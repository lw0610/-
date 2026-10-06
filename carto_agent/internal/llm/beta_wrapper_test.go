package llm

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestSanitizeMessages_ToolTextPreservedAndImageRehomed(t *testing.T) {
	msg := &schema.Message{
		Role:       schema.Tool,
		ToolCallID: "call-1",
		ToolName:   "generate_candidate",
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "first"},
			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{}},
			{Type: schema.ChatMessagePartTypeText, Text: "second"},
		},
	}

	prepared := SanitizeMessages([]*schema.Message{msg})

	if len(prepared) != 2 {
		t.Fatalf("expected tool and synthetic user messages, got %d", len(prepared))
	}
	toolMsg := prepared[0]
	if toolMsg.Role != schema.Tool || toolMsg.ToolCallID != "call-1" || toolMsg.ToolName != "generate_candidate" {
		t.Fatalf("tool protocol fields were not preserved: %#v", toolMsg)
	}
	if toolMsg.Content != "first\nsecond" {
		t.Fatalf("expected degraded content %q, got %q", "first\nsecond", toolMsg.Content)
	}
	if toolMsg.UserInputMultiContent != nil {
		t.Fatalf("expected tool UserInputMultiContent to be nil, got %#v", toolMsg.UserInputMultiContent)
	}

	mediaMsg := prepared[1]
	if mediaMsg.Role != schema.User || len(mediaMsg.UserInputMultiContent) != 4 {
		t.Fatalf("unexpected synthetic media message: %#v", mediaMsg)
	}
	if mediaMsg.UserInputMultiContent[0].Type != schema.ChatMessagePartTypeText {
		t.Fatalf("expected a context label before the image, got %#v", mediaMsg.UserInputMultiContent[0])
	}
	if mediaMsg.UserInputMultiContent[1].Text != "first" || mediaMsg.UserInputMultiContent[2].Type != schema.ChatMessagePartTypeImageURL || mediaMsg.UserInputMultiContent[2].Image == nil || mediaMsg.UserInputMultiContent[3].Text != "second" {
		t.Fatalf("expected text/image association to be preserved, got %#v", mediaMsg.UserInputMultiContent)
	}

	if msg.Content != "" || len(msg.UserInputMultiContent) != 3 {
		t.Fatalf("expected source history to remain unchanged, got %#v", msg)
	}
}

func TestSanitizeToolMultiContent_UserMessageUntouched(t *testing.T) {
	msg := &schema.Message{
		Role: schema.User,
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "hello"},
			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{}},
		},
	}

	prepared := SanitizeToolMultiContent([]*schema.Message{msg})

	if len(prepared) != 1 || prepared[0].Role != schema.User {
		t.Fatalf("unexpected prepared messages: %#v", prepared)
	}
	if prepared[0].Content != "" || len(prepared[0].UserInputMultiContent) != 2 {
		t.Fatalf("expected user multimodal content to remain intact: %#v", prepared[0])
	}
	if prepared[0].UserInputMultiContent[0].Type != schema.ChatMessagePartTypeText || prepared[0].UserInputMultiContent[0].Text != "hello" {
		t.Fatalf("unexpected first part: %#v", prepared[0].UserInputMultiContent[0])
	}
	if prepared[0].UserInputMultiContent[1].Type != schema.ChatMessagePartTypeImageURL || prepared[0].UserInputMultiContent[1].Image == nil {
		t.Fatalf("unexpected second part: %#v", prepared[0].UserInputMultiContent[1])
	}
}

func TestSanitizeToolMultiContent_EmptyAndNil(t *testing.T) {
	if got := SanitizeToolMultiContent(nil); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
	if got := SanitizeToolMultiContent([]*schema.Message{}); len(got) != 0 {
		t.Fatalf("expected empty result, got %#v", got)
	}
	got := SanitizeToolMultiContent([]*schema.Message{nil})
	if len(got) != 1 || got[0] != nil {
		t.Fatalf("expected nil message to be preserved, got %#v", got)
	}
}

func TestSanitizeToolMultiContent_ToolContentPreserved(t *testing.T) {
	msg := &schema.Message{
		Role:    schema.Tool,
		Content: "existing",
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "should not override"},
		},
	}

	prepared := SanitizeToolMultiContent([]*schema.Message{msg})

	if len(prepared) != 1 || prepared[0].Content != "existing" {
		t.Fatalf("expected content to remain %q, got %#v", "existing", prepared)
	}
	if prepared[0].UserInputMultiContent != nil {
		t.Fatalf("expected UserInputMultiContent to be nil, got %#v", prepared[0].UserInputMultiContent)
	}
}

func TestSanitizeToolMultiContent_DirectCallStillSanitizesInPlace(t *testing.T) {
	msg := &schema.Message{
		Role: schema.Tool,
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "stored result"},
			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{}},
		},
	}

	SanitizeToolMultiContent([]*schema.Message{msg})

	if msg.Content != "stored result" || msg.UserInputMultiContent != nil {
		t.Fatalf("expected direct compatibility call to sanitize in place, got %#v", msg)
	}
}

func TestSanitizeToolMultiContent_ParallelToolBlockStaysContiguous(t *testing.T) {
	first := &schema.Message{
		Role:       schema.Tool,
		ToolCallID: "call-1",
		ToolName:   "first_tool",
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "first result"},
			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{}},
		},
	}
	second := &schema.Message{
		Role:       schema.Tool,
		ToolCallID: "call-2",
		ToolName:   "second_tool",
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "second result"},
			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{}},
		},
	}

	prepared := SanitizeToolMultiContent([]*schema.Message{first, second})

	if len(prepared) != 3 {
		t.Fatalf("expected two tool messages followed by one user media message, got %d", len(prepared))
	}
	if prepared[0].Role != schema.Tool || prepared[1].Role != schema.Tool || prepared[2].Role != schema.User {
		t.Fatalf("parallel tool response order was broken: %#v", prepared)
	}
	if prepared[0].ToolCallID != "call-1" || prepared[1].ToolCallID != "call-2" {
		t.Fatalf("tool call IDs were not preserved: %#v", prepared[:2])
	}
	if len(prepared[2].UserInputMultiContent) != 6 {
		t.Fatalf("expected label+image for each tool, got %#v", prepared[2].UserInputMultiContent)
	}
}

func TestEnsureNonNilOpenAIContent_AddsPlaceholderForEmptyToolCallMessage(t *testing.T) {
	msg := &schema.Message{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{
			{ID: "call-1", Function: schema.FunctionCall{Name: "demo_tool", Arguments: "{}"}},
		},
	}

	EnsureNonNilOpenAIContent([]*schema.Message{msg})

	if msg.Content != " " {
		t.Fatalf("expected placeholder content, got %q", msg.Content)
	}
}

func TestSanitizeMessages_ImageOnlyToolGetsPlaceholderAndUserImage(t *testing.T) {
	msg := &schema.Message{
		Role: schema.Tool,
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{}},
		},
	}

	prepared := SanitizeMessages([]*schema.Message{msg})

	if len(prepared) != 2 {
		t.Fatalf("expected tool and user messages, got %d", len(prepared))
	}
	if prepared[0].UserInputMultiContent != nil || prepared[0].Content != " " {
		t.Fatalf("expected protocol-safe tool placeholder, got %#v", prepared[0])
	}
	if prepared[1].Role != schema.User || len(prepared[1].UserInputMultiContent) != 2 {
		t.Fatalf("expected user image message, got %#v", prepared[1])
	}
}

func TestSanitizingModelWrapperGenerateUsesPreparedMessagesWithoutMutatingHistory(t *testing.T) {
	underlying := &recordingSanitizerModel{}
	wrapper := NewSanitizingModelWrapper(underlying)
	input := []*schema.Message{{
		Role:       schema.Tool,
		ToolCallID: "call-1",
		ToolName:   "generate_candidate",
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "candidate ready"},
			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{}},
		},
	}}

	if _, err := wrapper.Generate(context.Background(), input); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(underlying.input) != 2 || underlying.input[0].Role != schema.Tool || underlying.input[1].Role != schema.User {
		t.Fatalf("underlying model did not receive prepared messages: %#v", underlying.input)
	}
	if input[0].Content != "" || len(input[0].UserInputMultiContent) != 2 {
		t.Fatalf("wrapper mutated source history: %#v", input[0])
	}
}

type recordingSanitizerModel struct {
	input []*schema.Message
}

func (m *recordingSanitizerModel) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.input = input
	return schema.AssistantMessage("ok", nil), nil
}

func (m *recordingSanitizerModel) Stream(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.input = input
	return nil, nil
}

func (m *recordingSanitizerModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
