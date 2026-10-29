package websocket

import (
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestDecodeSessionMessagesPreservesToolMediaAcrossRestore(t *testing.T) {
	image, pdf := "aW1hZ2U=", "JVBERi0xLjcK"
	original := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{
			{ID: "read-image", Type: "function", Function: schema.FunctionCall{Name: "file_read", Arguments: `{}`}},
			{ID: "read-pdf", Type: "function", Function: schema.FunctionCall{Name: "file_read", Arguments: `{}`}},
		}),
		{Role: schema.Tool, ToolCallID: "read-image", ToolName: "file_read", UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "original source image"},
			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{MIMEType: "image/png", Base64Data: &image}}},
		}},
		{Role: schema.Tool, ToolCallID: "read-pdf", ToolName: "file_read", UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeFileURL, File: &schema.MessageInputFile{MessagePartCommon: schema.MessagePartCommon{MIMEType: "application/pdf", Base64Data: &pdf}}},
		}},
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var stored []any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	restored := decodeSessionMessages(stored, -1)
	if len(restored) != 3 || len(restored[1].UserInputMultiContent) != 2 || len(restored[2].UserInputMultiContent) != 1 {
		t.Fatalf("tool media lost during restoration: %#v", restored)
	}
	if *restored[1].UserInputMultiContent[1].Image.Base64Data != image || *restored[2].UserInputMultiContent[0].File.Base64Data != pdf {
		t.Fatal("stored media bytes changed")
	}
	if restored[1].ToolCallID != "read-image" || restored[2].ToolCallID != "read-pdf" {
		t.Fatal("parallel tool response identities changed")
	}
}

func TestDecodeSessionMessagesLimitAndRole(t *testing.T) {
	raw := []any{
		map[string]any{"role": "user", "content": "u1"},
		map[string]any{"role": "assistant", "content": "a1"},
		map[string]any{"role": "system", "content": "s1"},
	}

	msgs := decodeSessionMessages(raw, 2)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != schema.Assistant || msgs[0].Content != "a1" {
		t.Fatalf("unexpected first message: role=%s content=%q", msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != schema.System || msgs[1].Content != "s1" {
		t.Fatalf("unexpected second message: role=%s content=%q", msgs[1].Role, msgs[1].Content)
	}
}

func TestDecodeSessionMessagesSkipInvalid(t *testing.T) {
	raw := []any{
		map[string]any{"role": "user", "content": ""},
		map[string]any{"role": "assistant", "content": "   "},
		map[string]any{"role": "unknown", "content": "ok"},
		"invalid",
	}

	msgs := decodeSessionMessages(raw, 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != schema.System {
		t.Fatalf("expected unknown role mapped to system, got %s", msgs[0].Role)
	}
	if msgs[0].Content != "ok" {
		t.Fatalf("expected content ok, got %q", msgs[0].Content)
	}
}

func TestDecodeSessionMessagesPreservesStructuredFields(t *testing.T) {
	raw := []any{
		map[string]any{
			"role":              "assistant",
			"reasoning_content": "先拆解任务",
			"tool_calls": []any{
				map[string]any{
					"id":   "call-1",
					"type": "function",
					"function": map[string]any{
						"name":      "write_todos",
						"arguments": `{"items":[{"content":"补测试"}]}`,
					},
				},
			},
			"extra": map[string]any{"source": "runtime_snapshot"},
		},
	}

	msgs := decodeSessionMessages(raw, -1)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != schema.Assistant {
		t.Fatalf("expected assistant role, got %s", msgs[0].Role)
	}
	if msgs[0].ReasoningContent != "先拆解任务" {
		t.Fatalf("unexpected reasoning content: %q", msgs[0].ReasoningContent)
	}
	if len(msgs[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msgs[0].ToolCalls))
	}
	if msgs[0].ToolCalls[0].Function.Name != "write_todos" {
		t.Fatalf("unexpected tool call name: %s", msgs[0].ToolCalls[0].Function.Name)
	}
	if source, _ := msgs[0].Extra["source"].(string); source != "runtime_snapshot" {
		t.Fatalf("unexpected extra source: %#v", msgs[0].Extra)
	}
}

func TestDecodeSessionMessagesPreservesToolRole(t *testing.T) {
	raw := []any{
		map[string]any{"role": "tool", "content": "工具执行完成"},
	}

	msgs := decodeSessionMessages(raw, -1)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != schema.Tool {
		t.Fatalf("expected tool role, got %s", msgs[0].Role)
	}
	if msgs[0].Content != "工具执行完成" {
		t.Fatalf("unexpected content: %q", msgs[0].Content)
	}
}
