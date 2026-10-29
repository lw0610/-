package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/toolresult"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

var mediaPNG = []byte("\x89PNG\r\n\x1a\nfixture-image")
var mediaPDF = []byte("%PDF-1.7\nfixture-document\n%%EOF")

func mediaToolHistory(t *testing.T, legacy bool) []*schema.Message {
	t.Helper()
	result := toolresult.AppendImageData(toolresult.Text("真实来源：fixture"), mediaPNG, "image/png")
	result = toolresult.AppendFileData(result, mediaPDF, "application/pdf", map[string]any{"filename": "table.pdf"})
	parts, err := result.ToMessageInputParts()
	if err != nil {
		t.Fatal(err)
	}
	tool := &schema.Message{Role: schema.Tool, ToolCallID: "call-media", ToolName: "file_read", Content: "附件已保存", UserInputMultiContent: parts}
	if legacy {
		tool.UserInputMultiContent = nil
		tool.MultiContent = []schema.ChatMessagePart{
			{Type: schema.ChatMessagePartTypeText, Text: "真实来源：fixture"},
			{Type: schema.ChatMessagePartTypeImageURL, ImageURL: &schema.ChatMessageImageURL{URL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(mediaPNG)}},
			{Type: schema.ChatMessagePartTypeFileURL, FileURL: &schema.ChatMessageFileURL{URL: "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(mediaPDF), Name: "table.pdf"}},
		}
	}
	return []*schema.Message{
		schema.UserMessage("读取附件"),
		schema.AssistantMessage("", []schema.ToolCall{{ID: "call-media", Type: "function", Function: schema.FunctionCall{Name: "file_read", Arguments: "{}"}}}),
		tool,
	}
}

func TestMediaCapabilityWrappersPreserveProtocolAndHistory(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, outerCapabilities := range []bool{false, true} {
			for _, cap := range []MediaCapabilities{{Model: "text-only"}, {Model: "image-only", Image: true}, {Model: "multimodal", Image: true, PDF: true}} {
				for _, stream := range []bool{false, true} {
					t.Run(fmt.Sprintf("legacy=%t/capsOuter=%t/%s/stream=%t", legacy, outerCapabilities, cap.Model, stream), func(t *testing.T) {
						input := mediaToolHistory(t, legacy)
						before, _ := json.Marshal(input)
						recorder := &recordingSanitizerModel{}
						var wrapped model.ToolCallingChatModel
						if outerCapabilities {
							wrapped = NewMediaCapabilityModel(NewSanitizingModelWrapper(recorder), cap)
						} else {
							wrapped = NewSanitizingModelWrapper(NewMediaCapabilityModel(recorder, cap))
						}
						wrapped, err := wrapped.WithTools([]*schema.ToolInfo{{Name: "file_read"}})
						if err != nil {
							t.Fatal(err)
						}
						if stream {
							_, err = wrapped.Stream(context.Background(), input)
						} else {
							_, err = wrapped.Generate(context.Background(), input)
						}
						if err != nil {
							t.Fatal(err)
						}
						after, _ := json.Marshal(input)
						if string(before) != string(after) {
							t.Fatal("input history changed")
						}
						got := recorder.input
						if len(got) != 4 || got[2].Role != schema.Tool || got[2].ToolCallID != "call-media" || got[2].Content != "附件已保存" || got[3].Role != schema.User {
							t.Fatalf("tool protocol was not preserved: %#v", got)
						}
						var images, pdfs, rejections int
						for _, m := range got {
							if len(m.MultiContent) != 0 {
								t.Fatal("legacy media bypassed normalization")
							}
							for _, p := range m.UserInputMultiContent {
								switch p.Type {
								case schema.ChatMessagePartTypeImageURL:
									images++
								case schema.ChatMessagePartTypeFileURL:
									pdfs++
								case schema.ChatMessagePartTypeText:
									if strings.Contains(p.Text, "MODEL_MEDIA_UNSUPPORTED") {
										rejections++
									}
								}
							}
						}
						wantImages, wantPDFs := 0, 0
						if cap.Image {
							wantImages = 1
						}
						if cap.PDF {
							wantPDFs = 1
						}
						if images != wantImages || pdfs != wantPDFs || rejections != 2-wantImages-wantPDFs {
							t.Fatalf("capability filtering: image=%d PDF=%d diagnostics=%d", images, pdfs, rejections)
						}
					})
				}
			}
		}
	}
}

func TestMediaCapabilityReflectsInstalledAdapters(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic", "ollama", "google"} {
		for _, vision := range []bool{false, true} {
			got := MediaCapabilitiesForProvider(provider, "test", vision)
			if got.Image != vision || got.PDF != (provider == "google" && vision) {
				t.Fatalf("incorrect adapter capability for %s: %#v", provider, got)
			}
		}
	}
}

func TestSanitizerPreservesParallelToolPDFsAndIsIdempotent(t *testing.T) {
	first := mediaToolHistory(t, false)[2]
	second := mediaToolHistory(t, true)[2]
	second.ToolCallID = "call-second"
	input := []*schema.Message{first, second}
	before, _ := json.Marshal(input)
	got := SanitizeMessages(input)
	if len(got) != 3 || got[0].Role != schema.Tool || got[1].Role != schema.Tool || got[2].Role != schema.User {
		t.Fatalf("parallel call ordering changed: %#v", got)
	}
	var documents int
	for _, p := range got[2].UserInputMultiContent {
		if p.Type == schema.ChatMessagePartTypeFileURL {
			documents++
			if p.File.Base64Data == nil || *p.File.Base64Data != base64.StdEncoding.EncodeToString(mediaPDF) {
				t.Fatal("PDF bytes lost")
			}
		}
	}
	if documents != 2 {
		t.Fatalf("expected both PDFs, got %d", documents)
	}
	if !reflect.DeepEqual(SanitizeMessages(got), got) {
		t.Fatal("sanitizer duplicates or drops media on repeated wrapping")
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("source tool results mutated")
	}
}

func TestMediaCapabilityDoesNotReactivateSupersededLegacyParts(t *testing.T) {
	for _, role := range []schema.RoleType{schema.User, schema.Tool} {
		t.Run(string(role), func(t *testing.T) {
			input := mediaToolHistory(t, false)[2]
			input.Role = role
			input.MultiContent = mediaToolHistory(t, true)[2].MultiContent
			before, _ := json.Marshal(input)
			recorder := &recordingSanitizerModel{}
			cm := NewMediaCapabilityModel(recorder, MediaCapabilities{Model: "text-only"})
			if _, err := cm.Generate(context.Background(), []*schema.Message{input}); err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(recorder.input)
			if strings.Contains(string(encoded), base64.StdEncoding.EncodeToString(mediaPNG)) || strings.Contains(string(encoded), base64.StdEncoding.EncodeToString(mediaPDF)) {
				t.Fatal("superseded legacy media bypassed capability filtering")
			}
			after, _ := json.Marshal(input)
			if string(before) != string(after) {
				t.Fatal("source history mutated")
			}
		})
	}
}

func TestMediaCapabilityHandlesPDFURL(t *testing.T) {
	url := "https://example.test/source.pdf"
	for _, pdf := range []bool{false, true} {
		input := &schema.Message{Role: schema.Tool, ToolCallID: "file-call", Content: "source", UserInputMultiContent: []schema.MessageInputPart{{
			Type: schema.ChatMessagePartTypeFileURL,
			File: &schema.MessageInputFile{MessagePartCommon: schema.MessagePartCommon{URL: &url, MIMEType: "application/pdf"}},
		}}}
		recorder := &recordingSanitizerModel{}
		cm := NewMediaCapabilityModel(recorder, MediaCapabilities{Model: "pdf-model", PDF: pdf})
		if _, err := cm.Generate(context.Background(), []*schema.Message{input}); err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(recorder.input)
		if strings.Contains(string(encoded), url) != pdf || strings.Contains(string(encoded), "MODEL_MEDIA_UNSUPPORTED") == pdf {
			t.Fatalf("URL document lost or bypassed capabilities: %s", encoded)
		}
	}
}

func TestOpenAIMediaReachesActualHTTPRequest(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		requests <- body
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"media\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"media\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"media","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	// An IPv4-mapped loopback address still reaches this local fixture, but
	// avoids the localhost text-tool adapter that intentionally implements
	// Stream via Generate. Exercise the native OpenAI SSE request here.
	baseURL := strings.Replace(server.URL, "127.0.0.1", "[::ffff:127.0.0.1]", 1) + "/v1"
	for _, vision := range []bool{false, true} {
		for _, legacy := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("vision=%t/legacy=%t/stream=%t", vision, legacy, stream), func(t *testing.T) {
					cm, err := NewChatModel(&Config{Provider: "openai", Model: "fixture-model", Vision: vision, APIKey: "fixture-only", BaseURL: baseURL})
					if err != nil {
						t.Fatal(err)
					}
					cm, err = cm.WithTools([]*schema.ToolInfo{{Name: "file_read", Desc: "Read file"}})
					if err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					input := mediaToolHistory(t, legacy)
					before, _ := json.Marshal(input)
					if stream {
						reader, err := cm.Stream(ctx, input)
						if err != nil {
							t.Fatal(err)
						}
						defer reader.Close()
						for {
							_, err := reader.Recv()
							if err == io.EOF {
								break
							}
							if err != nil {
								t.Fatal(err)
							}
						}
					} else {
						if _, err := cm.Generate(ctx, input); err != nil {
							t.Fatal(err)
						}
					}
					body := <-requests
					if (body["stream"] == true) != stream {
						t.Fatal("request did not use the expected native Generate/Stream mode")
					}
					raw, _ := json.Marshal(body)
					imageURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(mediaPNG)
					if strings.Contains(string(raw), imageURL) != vision {
						t.Fatal("image data URI did not follow selected capability")
					}
					if strings.Contains(string(raw), base64.StdEncoding.EncodeToString(mediaPDF)) || !strings.Contains(string(raw), "MODEL_MEDIA_UNSUPPORTED") {
						t.Fatal("unsupported PDF was silently encoded or lost its diagnostic")
					}
					messages := body["messages"].([]any)
					for _, item := range messages {
						m := item.(map[string]any)
						if m["role"] == "tool" {
							if m["tool_call_id"] != "call-media" {
								t.Fatal("tool_call_id changed")
							}
							if _, ok := m["content"].(string); !ok {
								t.Fatal("non-text media sent in tool response")
							}
						}
					}
					if _, exists := body["tools"]; !exists {
						t.Fatal("WithTools lost bindings")
					}
					after, _ := json.Marshal(input)
					if string(before) != string(after) {
						t.Fatal("HTTP serialization mutated history")
					}
				})
			}
		}
	}
}

func TestGeminiPDFReachesActualHTTPRequest(t *testing.T) {
	type request struct {
		body   map[string]any
		stream bool
	}
	requests := make(chan request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		stream := strings.Contains(r.URL.Path, "streamGenerateContent")
		requests <- request{body: body, stream: stream}
		response := `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\n", response)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, response)
	}))
	defer server.Close()
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			cm, err := NewChatModel(&Config{Provider: "google", Model: "fixture-model", Vision: true, APIKey: "fixture-only", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			cm, err = cm.WithTools([]*schema.ToolInfo{{Name: "file_read", Desc: "Read file"}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			input := mediaToolHistory(t, false)
			before, _ := json.Marshal(input)
			if stream {
				reader, err := cm.Stream(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				for {
					_, err := reader.Recv()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
				}
			} else if _, err := cm.Generate(ctx, input); err != nil {
				t.Fatal(err)
			}
			received := <-requests
			if received.stream != stream {
				t.Fatal("request did not use the expected native Generate/Stream mode")
			}
			body := received.body
			var gotImage, gotPDF bool
			for _, content := range body["contents"].([]any) {
				for _, item := range content.(map[string]any)["parts"].([]any) {
					inline, ok := item.(map[string]any)["inlineData"].(map[string]any)
					if !ok {
						continue
					}
					switch inline["mimeType"] {
					case "image/png":
						gotImage = inline["data"] == base64.StdEncoding.EncodeToString(mediaPNG)
					case "application/pdf":
						gotPDF = inline["data"] == base64.StdEncoding.EncodeToString(mediaPDF)
					}
				}
			}
			if !gotImage || !gotPDF {
				t.Fatalf("Gemini inline image/PDF bytes lost: image=%t PDF=%t", gotImage, gotPDF)
			}
			if _, exists := body["tools"]; !exists {
				t.Fatal("Gemini WithTools lost bindings")
			}
			after, _ := json.Marshal(input)
			if string(before) != string(after) {
				t.Fatal("Gemini serialization mutated history")
			}
		})
	}
}
