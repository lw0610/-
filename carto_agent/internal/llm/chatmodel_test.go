package llm

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	openaiModel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestMapThinkingBudgetToReasoningEffort(t *testing.T) {
	tests := []struct {
		name   string
		budget int
		want   openaiModel.ReasoningEffortLevel
	}{
		{name: "low when zero", budget: 0, want: openaiModel.ReasoningEffortLevelLow},
		{name: "low below medium threshold", budget: 4095, want: openaiModel.ReasoningEffortLevelLow},
		{name: "medium at threshold", budget: 4096, want: openaiModel.ReasoningEffortLevelMedium},
		{name: "medium below high threshold", budget: 8191, want: openaiModel.ReasoningEffortLevelMedium},
		{name: "high at threshold", budget: 8192, want: openaiModel.ReasoningEffortLevelHigh},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapThinkingBudgetToReasoningEffort(tt.budget)
			if got != tt.want {
				t.Fatalf("expected reasoning effort %q, got %q", tt.want, got)
			}
		})
	}
}

func TestShouldDisableHTTP2ForOpenAIBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{name: "deepseek v1", baseURL: "https://api.deepseek.com/v1", want: true},
		{name: "deepseek beta", baseURL: "https://api.deepseek.com/beta", want: true},
		{name: "deepseek with port", baseURL: "https://api.deepseek.com:443/v1", want: true},
		{name: "openai official", baseURL: "https://api.openai.com/v1", want: false},
		{name: "local ollama openai compatible", baseURL: "http://127.0.0.1:11434/v1", want: false},
		{name: "empty", baseURL: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldDisableHTTP2ForOpenAIBaseURL(tt.baseURL); got != tt.want {
				t.Fatalf("expected %v for %q, got %v", tt.want, tt.baseURL, got)
			}
		})
	}
}

func TestBuildOpenAIHTTPClient_DeepSeekDisablesHTTP2(t *testing.T) {
	client := buildOpenAIHTTPClient("https://api.deepseek.com/v1")
	if client == nil {
		t.Fatal("expected deepseek client override")
	}
	if client.Timeout != 5*time.Minute {
		t.Fatalf("expected timeout %s, got %s", 5*time.Minute, client.Timeout)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.ForceAttemptHTTP2 {
		t.Fatal("expected HTTP/2 to be disabled for deepseek")
	}
	if transport.TLSNextProto == nil {
		t.Fatal("expected TLSNextProto override to disable HTTP/2")
	}
}

func TestBuildOpenAIHTTPClient_NonDeepSeekUsesDefaultClient(t *testing.T) {
	if client := buildOpenAIHTTPClient("https://api.openai.com/v1"); client != nil {
		t.Fatalf("expected nil client override for non-deepseek base url, got %#v", client)
	}
}

func TestIsLocalOpenAICompatibleBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{name: "ollama localhost", baseURL: "http://127.0.0.1:11434/v1", want: true},
		{name: "ollama hostname", baseURL: "http://localhost:11434/v1", want: true},
		{name: "ollama ipv6", baseURL: "http://[::1]:11434/v1", want: true},
		{name: "deepseek", baseURL: "https://api.deepseek.com/v1", want: false},
		{name: "openai official", baseURL: "https://api.openai.com/v1", want: false},
		{name: "empty", baseURL: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLocalOpenAICompatibleBaseURL(tt.baseURL); got != tt.want {
				t.Fatalf("expected %v for %q, got %v", tt.want, tt.baseURL, got)
			}
		})
	}
}

func TestWrapOpenAICompatibleModelAddsTextToolAdapterForLocalBaseURL(t *testing.T) {
	wrapped := wrapOpenAICompatibleModel("http://127.0.0.1:11434/v1", &fakeToolCallingModel{})
	if _, ok := wrapped.(*TextToolCallModelWrapper); !ok {
		t.Fatalf("expected local OpenAI-compatible model to use TextToolCallModelWrapper, got %T", wrapped)
	}
}

func TestWrapOpenAICompatibleModelLeavesRemoteOpenAIUnchanged(t *testing.T) {
	wrapped := wrapOpenAICompatibleModel("https://api.deepseek.com/v1", &fakeToolCallingModel{})
	if _, ok := wrapped.(*TextToolCallModelWrapper); ok {
		t.Fatalf("expected remote OpenAI-compatible model not to use TextToolCallModelWrapper")
	}
	if _, ok := wrapped.(*SanitizingModelWrapper); !ok {
		t.Fatalf("expected remote OpenAI-compatible model to keep SanitizingModelWrapper, got %T", wrapped)
	}
}

type fakeToolCallingModel struct {
	generateCalls int
	generateErrs  []error
}

func (f *fakeToolCallingModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	idx := f.generateCalls
	f.generateCalls++
	if idx < len(f.generateErrs) && f.generateErrs[idx] != nil {
		return nil, f.generateErrs[idx]
	}
	return schema.AssistantMessage("ok", nil), nil
}

func (f *fakeToolCallingModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("not implemented")
}

func (f *fakeToolCallingModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func TestRetryingModelWrapper_RetriesTransientGenerateErrors(t *testing.T) {
	fake := &fakeToolCallingModel{generateErrs: []error{
		errors.New("read tcp 1.2.3.4:443: read: connection reset by peer"),
		errors.New("stream error: stream ID 7; INTERNAL_ERROR; received from peer"),
		nil,
	}}

	wrapped := NewRetryingModelWrapper(fake)
	msg, err := wrapped.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if msg == nil || msg.Content != "ok" {
		t.Fatalf("unexpected message: %#v", msg)
	}
	if fake.generateCalls != 3 {
		t.Fatalf("expected 3 generate calls, got %d", fake.generateCalls)
	}
}

func TestRetryingModelWrapper_DoesNotRetryNonTransientErrors(t *testing.T) {
	fake := &fakeToolCallingModel{generateErrs: []error{errors.New("status code: 401 invalid api key")}}

	wrapped := NewRetryingModelWrapper(fake)
	_, err := wrapped.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")})
	if err == nil {
		t.Fatal("expected generate error")
	}
	if fake.generateCalls != 1 {
		t.Fatalf("expected 1 generate call, got %d", fake.generateCalls)
	}
}
