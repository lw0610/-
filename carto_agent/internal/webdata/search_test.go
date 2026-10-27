package webdata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

func TestSearchUsesProviderMCPRequestsAndPreservesSourceText(t *testing.T) {
	t.Setenv("EXA_API_KEY", "exa-private+/key")
	t.Setenv("PARALLEL_API_KEY", "parallel-private-key")
	for _, provider := range []string{"exa", "parallel"} {
		t.Run(provider, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || r.Header.Get("Accept") != "application/json, text/event-stream" || r.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("invalid MCP request: %s %+v", r.Method, r.Header)
				}
				var payload struct {
					JSONRPC string `json:"jsonrpc"`
					Method  string `json:"method"`
					ID      int    `json:"id"`
					Params  struct {
						Name      string         `json:"name"`
						Arguments map[string]any `json:"arguments"`
					} `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload.JSONRPC != "2.0" || payload.Method != "tools/call" || payload.ID != 1 {
					t.Fatalf("wrong envelope: %+v", payload)
				}
				if provider == "exa" {
					if r.URL.Host != "mcp.exa.ai" || r.URL.Query().Get("exaApiKey") != "exa-private+/key" || payload.Params.Name != "web_search_exa" {
						t.Fatal("wrong Exa endpoint or tool")
					}
					if payload.Params.Arguments["query"] != "全国人口 2025 官方" || payload.Params.Arguments["numResults"] != float64(5) || payload.Params.Arguments["contextMaxCharacters"] != float64(10000) || payload.Params.Arguments["type"] != "deep" || payload.Params.Arguments["livecrawl"] != "preferred" {
						t.Fatalf("wrong Exa args: %+v", payload.Params.Arguments)
					}
					if r.Header.Get("Authorization") != "" {
						t.Fatal("Parallel credentials sent to Exa")
					}
				} else {
					if r.URL.Host != "search.parallel.ai" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer parallel-private-key" || payload.Params.Name != "web_search" {
						t.Fatal("wrong Parallel request")
					}
					if payload.Params.Arguments["objective"] != "全国人口 2025 官方" || len(payload.Params.Arguments) != 2 {
						t.Fatalf("wrong Parallel args: %+v", payload.Params.Arguments)
					}
					queries, ok := payload.Params.Arguments["search_queries"].([]any)
					if !ok || len(queries) != 1 || queries[0] != "全国人口 2025 官方" {
						t.Fatalf("wrong search_queries: %+v", queries)
					}
				}
				body := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"统计公报 https://www.stats.gov.cn/"}]}}`
				if provider == "parallel" {
					body = "event: message\r\ndata: " + body + "\r\n\r\n"
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			value, err := invoke(context.Background(), "web_search", map[string]any{"query": "全国人口 2025 官方", "num_results": 5, "type": "deep", "livecrawl": "preferred"}, Options{Enabled: true, SearchProvider: provider}, client)
			if err != nil {
				t.Fatal(err)
			}
			result := value.(map[string]any)
			if calls != 1 || result["provider"] != provider || result["text"] != "统计公报 https://www.stats.gov.cn/" || result["has_content"] != true || result["retrieved_at"] == "" {
				t.Fatalf("invalid result: %+v", result)
			}
		})
	}
}

func TestSearchResponseParsing(t *testing.T) {
	for _, tc := range []struct {
		name, body, text string
		wantError        bool
	}{
		{"json", `{"id":1,"result":{"content":[{"type":"text","text":"one"},{"type":"text","text":"two"}]}}`, "one\n\ntwo", false},
		{"sse-notification", "data: {\"method\":\"notifications/progress\"}\n\ndata:{\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"answer\"}]}}\n\n", "answer", false},
		{"sse-multiline", "data: {\"id\":1,\n" + "data: \"result\":{\"content\":[]}}\n\n", "", false},
		{"empty-results", `{"id":1,"result":{"content":[]}}`, "", false},
		{"rpc-error", `{"id":1,"error":{"code":-32603,"message":"secret"}}`, "", true},
		{"tool-error", `{"id":1,"result":{"isError":true,"content":[{"type":"text","text":"secret"}]}}`, "", true},
		{"wrong-id", `{"id":2,"result":{"content":[]}}`, "", true},
		{"missing-content", `{"id":1,"result":{}}`, "", true},
		{"invalid-json", `{"result":`, "", true},
		{"html-error", "<html>denied</html>", "", true},
		{"empty-body", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := parseSearchResponse([]byte(tc.body))
			if (err != nil) != tc.wantError || text != tc.text {
				t.Fatalf("text=%q err=%v", text, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("raw provider error leaked")
			}
		})
	}
}

func TestSearchFailuresNeverFallbackOrLeakCredentials(t *testing.T) {
	t.Setenv("EXA_API_KEY", "secret-key")
	for _, tc := range []struct {
		name         string
		status       int
		transportErr error
		body         string
	}{
		{"unauthorized", 401, nil, "secret-key"},
		{"forbidden", 403, nil, "secret-key"},
		{"rate-limit", 429, nil, "secret-key"},
		{"server-error", 500, nil, "secret-key"},
		{"network", 0, errors.New("Post https://mcp.exa.ai/mcp?exaApiKey=secret-key failed"), ""},
		{"large-body", 200, nil, strings.Repeat("x", searchResponseLimit+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if tc.transportErr != nil {
					return nil, tc.transportErr
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			value, err := invoke(context.Background(), "web_search", map[string]any{"query": "test"}, Options{Enabled: true}, client)
			if err == nil || value != nil || calls != 1 {
				t.Fatalf("unexpected success/retry: value=%v err=%v calls=%d", value, err, calls)
			}
			if strings.Contains(err.Error(), "secret-key") || strings.Contains(err.Error(), "exaApiKey") {
				t.Fatal("credential leaked in error")
			}
		})
	}
}

func TestSearchRedirectAndBodyEchoCannotExposeCredentials(t *testing.T) {
	t.Setenv("EXA_API_KEY", "secret+/key")
	calls := 0
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.example/"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	if _, err := invoke(context.Background(), "web_search", map[string]any{"query": "test"}, Options{Enabled: true}, client); err == nil || calls != 1 {
		t.Fatalf("redirect followed: calls=%d, %v", calls, err)
	}
	value, err := invoke(context.Background(), "web_search", map[string]any{"query": "test"}, Options{Enabled: true}, fixtureClient(`{"id":1,"result":{"content":[{"type":"text","text":"secret+/key secret%2B%2Fkey"}]}}`, "application/json", 200))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(value.(map[string]any)["text"].(string), "secret") {
		t.Fatal("response echoed credential")
	}
}

func TestSearchValidatesBeforeNetworkingAndHonorsDisabled(t *testing.T) {
	client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network request"); return nil, nil })}
	for _, args := range []map[string]any{
		{}, {"query": " "}, {"query": "x", "num_results": 0}, {"query": "x", "num_results": 1.5}, {"query": "x", "num_results": math.NaN()},
		{"query": "x", "type": "invalid"}, {"query": "x", "livecrawl": true}, {"query": "x", "context_max_characters": 100001},
	} {
		if _, err := invoke(context.Background(), "web_search", args, Options{Enabled: true}, client); err == nil {
			t.Fatalf("accepted invalid input: %+v", args)
		}
	}
	for _, opts := range []Options{{}, {Enabled: true, SearchProvider: "unknown"}} {
		if _, err := invoke(context.Background(), "web_search", map[string]any{"query": "test"}, opts, client); err == nil {
			t.Fatal("accepted disabled or invalid provider")
		}
	}
}

func TestSearchReportsEmptyResultsAndBoundsUnicodeText(t *testing.T) {
	t.Setenv("EXA_API_KEY", "")
	for _, text := range []string{"", strings.Repeat("地", 1001)} {
		body, _ := json.Marshal(map[string]any{"id": 1, "result": map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}})
		value, err := invoke(context.Background(), "web_search", map[string]any{"query": "test", "context_max_characters": 1000}, Options{Enabled: true}, fixtureClient(string(body), "application/json", 200))
		if err != nil {
			t.Fatal(err)
		}
		result := value.(map[string]any)
		if text == "" {
			if result["has_content"] != false || result["message"] == nil {
				t.Fatalf("empty result presented as evidence: %+v", result)
			}
		} else if len([]rune(result["text"].(string))) != 1000 || result["truncated"] != true {
			t.Fatalf("not bounded: %+v", result)
		}
	}
}
