package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

// Exercise the production constructor and SDK serialization, including WithTools.
// All endpoints are intercepted at transport level; no credentials or requests
// leave the local test server. The original hostname still selects the protocol.
func TestOpenAIProtocolControlsOnWire(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- body
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
			fmt.Fprint(w, "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	previous := http.DefaultTransport
	transport := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	http.DefaultTransport = transport
	t.Cleanup(func() {
		http.DefaultTransport = previous
		transport.CloseIdleConnections()
	})

	cases := []struct {
		name      string
		baseURL   string
		thinking  bool
		maxTokens int
		deepSeek  bool
	}{
		{"deepseek-disabled", "http://api.deepseek.com/v1", false, 4096, true},
		{"deepseek-enabled", "http://api.deepseek.com/v1", true, 4096, true},
		{"deepseek-beta-port", "http://api.deepseek.com:8080/beta", false, 1024, true},
		{"deepseek-unspecified-limit", "http://api.deepseek.com", false, 0, true},
		{"openai-disabled", "http://api.openai.com/v1", false, 4096, false},
		{"openai-enabled", "http://api.openai.com/v1", true, 4096, false},
		{"unknown-compatible", "http://compat.example/v1", false, 4096, false},
		{"hostname-lookalike", "http://api.deepseek.com.example/v1", false, 4096, false},
	}
	for _, tc := range cases {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				cfg := &Config{Provider: "openai", Model: "deepseek-v4-pro", APIKey: "local-fixture-only", BaseURL: tc.baseURL, MaxTokens: tc.maxTokens, Temperature: 0.7, ThinkingEnabled: tc.thinking, ThinkingBudget: 10240}
				before := *cfg
				cm, err := NewChatModel(cfg)
				if err != nil {
					t.Fatal(err)
				}
				cm, err = cm.WithTools([]*schema.ToolInfo{{Name: "read_probe", Desc: "Read-only test tool"}})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				messages := []*schema.Message{schema.UserMessage("Reply ok")}
				if stream {
					reader, err := cm.Stream(ctx, messages)
					if err != nil {
						t.Fatal(err)
					}
					defer reader.Close()
					content := ""
					for {
						message, err := reader.Recv()
						if err == io.EOF {
							break
						}
						if err != nil {
							t.Fatal(err)
						}
						content += message.Content
					}
					if content != "ok" {
						t.Fatalf("unexpected stream content: %q", content)
					}
				} else {
					response, err := cm.Generate(ctx, messages)
					if err != nil || response.Content != "ok" {
						t.Fatalf("Generate: response=%v error=%v", response, err)
					}
				}
				body := <-requests
				if *cfg != before {
					t.Fatal("constructor mutated user configuration")
				}
				if body["model"] != cfg.Model || body["temperature"] != 0.7 {
					t.Fatal("model choice or temperature changed", body)
				}
				if _, exists := body["tools"]; !exists {
					t.Fatal("WithTools discarded the tool definition")
				}
				if _, exists := body["extra_body"]; exists {
					t.Fatal("wire controls must be top-level JSON fields")
				}
				limitKey, otherLimit := "max_completion_tokens", "max_tokens"
				if tc.deepSeek {
					limitKey, otherLimit = otherLimit, limitKey
					thinking, ok := body["thinking"].(map[string]any)
					want := "disabled"
					if tc.thinking {
						want = "enabled"
					}
					if !ok || thinking["type"] != want {
						t.Fatalf("thinking preference was not explicit: %+v", body)
					}
				} else if _, exists := body["thinking"]; exists {
					t.Fatal("DeepSeek controls leaked into another endpoint")
				}
				if _, exists := body[otherLimit]; exists {
					t.Fatalf("conflicting token parameter %s: %+v", otherLimit, body)
				}
				if tc.maxTokens > 0 {
					if body[limitKey] != float64(tc.maxTokens) {
						t.Fatalf("configured output limit was not sent as %s: %+v", limitKey, body)
					}
				} else if _, exists := body[limitKey]; exists {
					t.Fatal("unspecified output limit was invented")
				}
				if tc.thinking {
					if body["reasoning_effort"] != "high" {
						t.Fatal("enabled thinking budget was not preserved", body)
					}
				} else if _, exists := body["reasoning_effort"]; exists {
					t.Fatal("disabled thinking conflicts with reasoning_effort", body)
				}
			})
		}
	}
}
