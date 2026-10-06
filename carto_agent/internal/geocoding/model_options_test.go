package geocoding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	openaiModel "github.com/cloudwego/eino-ext/components/model/openai"
)

func TestNonThinkingOptionsOnlyTargetHybridQwen3(t *testing.T) {
	for _, name := range []string{"qwen3.7-plus", "qwen3.6-plus", "qwen3-235b-a22b", "Qwen3.5-Plus"} {
		if len(NonThinkingOptions(name)) == 0 {
			t.Fatalf("missing local thinking override: %s", name)
		}
	}
	for _, name := range []string{"gpt-5", "deepseek-v4-pro", "claude-sonnet", "qwen-max", "qwen3-235b-a22b-thinking-2507", "qwen3-235b-a22b-instruct-2507", "qwen3-coder-plus", "qwen30"} {
		if len(NonThinkingOptions(name)) != 0 {
			t.Fatalf("vendor option leaked into unsupported model: %s", name)
		}
	}
}

func TestModelJSONSendsThinkingFalseAtTopLevelAndKeepsStageOutputBudget(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body = nil
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"{\"places\":[]}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()
	cm, err := openaiModel.NewChatModel(context.Background(), &openaiModel.ChatModelConfig{Model: "qwen3.7-plus", APIKey: "test-token", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	r := Resolver{Model: cm, ModelOptions: NonThinkingOptions("qwen3.7-plus")}
	var response map[string]any
	if err := r.modelJSON(context.Background(), estimationPrompt, map[string]any{"places": []any{}}, &response); err != nil {
		t.Fatal(err)
	}
	flag, exists := body["enable_thinking"]
	if !exists || flag != false {
		t.Fatalf("thinking not explicitly disabled at top level: %#v", body)
	}
	if _, nested := body["extra_body"]; nested {
		t.Fatal("incorrect nested extra_body sent")
	}
	if body["max_tokens"] != float64(1536) {
		t.Fatalf("stage budget lost: %#v", body["max_tokens"])
	}
	// The same shared model, called outside Resolver, must retain its settings.
	if _, err := cm.Generate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if _, changed := body["enable_thinking"]; changed {
		t.Fatal("resolver option changed shared model configuration")
	}
}
