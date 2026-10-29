package websocket

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/autocarto/carto_agent/internal/config"
)

type fakeContextCompressionModel struct {
	summary   string
	lastInput []*schema.Message
}

func (f *fakeContextCompressionModel) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	f.lastInput = append([]*schema.Message(nil), input...)
	return schema.AssistantMessage(f.summary, nil), nil
}

func (f *fakeContextCompressionModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (f *fakeContextCompressionModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func TestCompressContextMessagesUsesSummarizationMiddleware(t *testing.T) {
	chatModel := &fakeContextCompressionModel{summary: "保留当前目标与已完成工作"}
	raw := []any{
		map[string]any{"role": "user", "content": "先分析现有实现"},
		map[string]any{"role": "assistant", "content": "已经完成第一轮代码审查"},
		map[string]any{"role": "user", "content": "继续补齐 runtime middleware"},
		map[string]any{"role": "assistant", "content": "已接入 patchtoolcalls"},
		map[string]any{"role": "user", "content": "现在继续替换旧的 context.compress"},
	}

	result, err := compressContextMessages(context.Background(), chatModel, raw, &config.LLMProviderConfig{
		CompressStrategy: config.CompressStrategySummary,
	})
	if err != nil {
		t.Fatalf("compressContextMessages() error = %v", err)
	}

	if result.Summary != "保留当前目标与已完成工作" {
		t.Fatalf("unexpected summary: %q", result.Summary)
	}
	if result.OriginalCount != 5 {
		t.Fatalf("unexpected original count: %d", result.OriginalCount)
	}
	if result.CompressedCount != 5 {
		t.Fatalf("unexpected compressed count: %d", result.CompressedCount)
	}
	if len(result.CompressedContext) != 5 {
		t.Fatalf("unexpected compressed context len: %d", len(result.CompressedContext))
	}
	if role, _ := result.CompressedContext[0]["role"].(string); role != "system" {
		t.Fatalf("expected summary role system, got %q", role)
	}
	if content, _ := result.CompressedContext[0]["content"].(string); !strings.Contains(content, "保留当前目标与已完成工作") {
		t.Fatalf("unexpected summary content: %q", content)
	}
	if content, _ := result.CompressedContext[len(result.CompressedContext)-1]["content"].(string); content != "现在继续替换旧的 context.compress" {
		t.Fatalf("unexpected tail content: %q", content)
	}
	if len(chatModel.lastInput) == 0 {
		t.Fatalf("expected summarization model to receive input")
	}
}

func TestCompressContextMessagesPatchesDanglingToolCalls(t *testing.T) {
	chatModel := &fakeContextCompressionModel{summary: "修复了中断的工具调用"}
	raw := []any{
		map[string]any{
			"role": "assistant",
			"tool_calls": []any{
				map[string]any{
					"id":   "call-1",
					"type": "function",
					"function": map[string]any{
						"name":      "write_todos",
						"arguments": "{}",
					},
				},
			},
		},
		map[string]any{"role": "user", "content": "继续执行"},
	}

	result, err := compressContextMessages(context.Background(), chatModel, raw, &config.LLMProviderConfig{
		CompressStrategy: config.CompressStrategySummary,
	})
	if err != nil {
		t.Fatalf("compressContextMessages() error = %v", err)
	}

	if len(result.CompressedContext) != 4 {
		t.Fatalf("unexpected compressed context len: %d", len(result.CompressedContext))
	}
	foundPatchedToolMessage := false
	for _, item := range result.CompressedContext {
		role, _ := item["role"].(string)
		content, _ := item["content"].(string)
		if role == "tool" && strings.Contains(content, "write_todos") &&
			(strings.Contains(content, "已被取消") || strings.Contains(strings.ToLower(content), "cancelled")) {
			foundPatchedToolMessage = true
			break
		}
	}
	if !foundPatchedToolMessage {
		t.Fatalf("expected patched tool message in compressed context: %#v", result.CompressedContext)
	}
}

func TestCompressContextMessagesPreservesStructuredToolResult(t *testing.T) {
	chatModel := &fakeContextCompressionModel{summary: "保留 tool result"}
	raw := []any{
		map[string]any{"role": "user", "content": "生成预览"},
		map[string]any{
			"role":         "tool",
			"tool_name":    "preview_map",
			"tool_call_id": "call-1",
			"user_input_multi_content": []any{
				map[string]any{
					"type": "text",
					"text": `{"success":true,"image":"preview.png"}`,
				},
				map[string]any{
					"type": "file_url",
					"file": map[string]any{
						"name": "preview.png",
						"url":  "file:///tmp/preview.png",
					},
				},
			},
		},
		map[string]any{"role": "assistant", "content": "继续处理"},
	}

	result, err := compressContextMessages(context.Background(), chatModel, raw, &config.LLMProviderConfig{
		CompressStrategy: config.CompressStrategySummary,
	})
	if err != nil {
		t.Fatalf("compressContextMessages() error = %v", err)
	}

	foundStructured := false
	for _, item := range result.CompressedContext {
		role, _ := item["role"].(string)
		if role != "tool" {
			continue
		}
		// Recent tool results remain structured in durable history. Provider
		// conversion must happen at the model boundary, not while restoring or
		// compressing messages, otherwise attachments disappear on continuation.
		parts, ok := item["user_input_multi_content"].([]any)
		if ok && len(parts) == 2 {
			text, _ := parts[0].(map[string]any)
			file, _ := parts[1].(map[string]any)
			meta, _ := file["file"].(map[string]any)
			foundStructured = text["text"] == `{"success":true,"image":"preview.png"}` && meta["url"] == "file:///tmp/preview.png" && item["tool_call_id"] == "call-1"
		}
	}
	if !foundStructured {
		t.Fatalf("expected original structured tool result to survive compression: %#v", result.CompressedContext)
	}
}

func TestCompressContextMessagesWithTruncateStrategy(t *testing.T) {
	chatModel := &fakeContextCompressionModel{summary: "不应调用模型"}
	raw := []any{
		map[string]any{"role": "user", "content": strings.Repeat("A", 60)},
		map[string]any{"role": "assistant", "content": strings.Repeat("B", 60)},
		map[string]any{"role": "user", "content": strings.Repeat("C", 60)},
		map[string]any{"role": "assistant", "content": strings.Repeat("D", 60)},
		map[string]any{"role": "user", "content": "最后一条必须保留"},
	}

	result, err := compressContextMessages(context.Background(), chatModel, raw, &config.LLMProviderConfig{
		MaxContextTokens:      30,
		MaxContextUsage:       100,
		AutoCompressThreshold: 50,
		CompressStrategy:      config.CompressStrategyTruncate,
	})
	if err != nil {
		t.Fatalf("compressContextMessages() error = %v", err)
	}

	if len(chatModel.lastInput) != 0 {
		t.Fatalf("truncate strategy should not call summary model")
	}
	if !strings.Contains(result.Summary, "截断") {
		t.Fatalf("unexpected summary: %q", result.Summary)
	}
	if len(result.CompressedContext) == 0 {
		t.Fatalf("expected compressed context")
	}
	if role, _ := result.CompressedContext[0]["role"].(string); role != "system" {
		t.Fatalf("expected system notice, got %q", role)
	}
	last := result.CompressedContext[len(result.CompressedContext)-1]
	if content, _ := last["content"].(string); content != "最后一条必须保留" {
		t.Fatalf("unexpected tail content: %q", content)
	}
}
