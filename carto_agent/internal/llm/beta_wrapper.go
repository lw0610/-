package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// SanitizingModelWrapper 包装 ChatModel，在模型调用前统一清洗消息。
type SanitizingModelWrapper struct {
	underlying model.ToolCallingChatModel
}

var _ model.ToolCallingChatModel = (*SanitizingModelWrapper)(nil)

// NewSanitizingModelWrapper 创建消息清洗包装器。
func NewSanitizingModelWrapper(underlying model.ToolCallingChatModel) *SanitizingModelWrapper {
	return &SanitizingModelWrapper{underlying: underlying}
}

// SanitizeMessages 清洗发往模型的消息，兼容更严格的 OpenAI-compatible 后端。
//
// Tool 消息必须继续作为 tool role 回复对应的 tool_call；其中的图片会被移到
// 紧随连续 tool 消息块之后的 user 多模态消息，避免严格后端拒绝 tool role
// 上的多模态内容。返回新切片，不改写调用方持有的会话历史。
func SanitizeMessages(msgs []*schema.Message) []*schema.Message {
	prepared := cloneMessagesForSanitization(msgs)
	prepared = SanitizeToolMultiContent(prepared)
	EnsureNonNilOpenAIContent(prepared)
	return prepared
}

// SanitizeToolMultiContent 清洗 tool 消息上的 UserInputMultiContent，
// 避免 eino-ext/openai 对 tool role 多模态内容的严格校验报错。为兼容会话恢复等
// 旧调用方，该函数会原地清洗 tool 消息，并返回可能追加 user 图片消息的新切片。
// 模型调用应使用 SanitizeMessages，由其先复制会话历史。
func SanitizeToolMultiContent(msgs []*schema.Message) []*schema.Message {
	if len(msgs) == 0 {
		return msgs
	}

	prepared := make([]*schema.Message, 0, len(msgs))
	for index := 0; index < len(msgs); {
		msg := msgs[index]
		if msg == nil || msg.Role != schema.Tool {
			prepared = append(prepared, msg)
			index++
			continue
		}

		// Preserve a contiguous block of tool responses before adding a user
		// message. This keeps parallel OpenAI tool-call responses protocol-valid.
		var imageParts []schema.MessageInputPart
		for index < len(msgs) {
			toolMsg := msgs[index]
			if toolMsg == nil || toolMsg.Role != schema.Tool {
				break
			}

			sanitized := toolMsg
			if len(sanitized.UserInputMultiContent) > 0 {
				if sanitized.Content == "" {
					sanitized.Content = toolTextContent(sanitized.UserInputMultiContent)
				}
				mediaContext := toolImageContextParts(sanitized.UserInputMultiContent)
				if len(mediaContext) > 0 {
					imageParts = append(imageParts, schema.MessageInputPart{
						Type: schema.ChatMessagePartTypeText,
						Text: toolImageContextLabel(sanitized),
					})
					imageParts = append(imageParts, mediaContext...)
				}
				sanitized.UserInputMultiContent = nil
			}
			prepared = append(prepared, sanitized)
			index++
		}

		if len(imageParts) > 0 {
			prepared = append(prepared, &schema.Message{
				Role:                  schema.User,
				UserInputMultiContent: imageParts,
			})
		}
	}
	return prepared
}

func cloneMessagesForSanitization(msgs []*schema.Message) []*schema.Message {
	if len(msgs) == 0 {
		return msgs
	}
	cloned := make([]*schema.Message, len(msgs))
	for index, msg := range msgs {
		cloned[index] = cloneMessageForSanitization(msg)
	}
	return cloned
}

func cloneMessageForSanitization(msg *schema.Message) *schema.Message {
	if msg == nil {
		return nil
	}
	cloned := *msg
	if len(msg.UserInputMultiContent) > 0 {
		cloned.UserInputMultiContent = append([]schema.MessageInputPart(nil), msg.UserInputMultiContent...)
	}
	return &cloned
}

func toolTextContent(parts []schema.MessageInputPart) string {
	textParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Type == schema.ChatMessagePartTypeText && strings.TrimSpace(part.Text) != "" {
			textParts = append(textParts, part.Text)
		}
	}
	return strings.Join(textParts, "\n")
}

func toolImageContextParts(parts []schema.MessageInputPart) []schema.MessageInputPart {
	hasImage := false
	for _, part := range parts {
		if part.Type == schema.ChatMessagePartTypeImageURL && part.Image != nil {
			hasImage = true
			break
		}
	}
	if !hasImage {
		return nil
	}

	// Keep text next to its image so a result such as
	// "original" + image + "candidate" + image retains that association.
	contextParts := make([]schema.MessageInputPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case schema.ChatMessagePartTypeText:
			if strings.TrimSpace(part.Text) != "" {
				contextParts = append(contextParts, part)
			}
		case schema.ChatMessagePartTypeImageURL:
			if part.Image != nil {
				contextParts = append(contextParts, part)
			}
		}
	}
	return contextParts
}

func toolImageContextLabel(msg *schema.Message) string {
	if msg != nil && strings.TrimSpace(msg.ToolName) != "" {
		return fmt.Sprintf("Image output returned by tool %q. Use it as visual input for the next analysis.", strings.TrimSpace(msg.ToolName))
	}
	return "Image output returned by the preceding tool. Use it as visual input for the next analysis."
}

// EnsureNonNilOpenAIContent 避免 assistant/tool-call 消息被 OpenAI 适配器编码为 content:null。
// 部分本地 OpenAI-compatible 后端（例如 Ollama /v1/chat/completions）会拒绝 null content。
func EnsureNonNilOpenAIContent(msgs []*schema.Message) {
	for _, msg := range msgs {
		if msg == nil || msg.Content != "" || hasStructuredMessageContent(msg) {
			continue
		}
		msg.Content = " "
	}
}

func hasStructuredMessageContent(msg *schema.Message) bool {
	return len(msg.UserInputMultiContent) > 0 || len(msg.AssistantGenMultiContent) > 0
}

// Generate 生成响应前统一清洗消息。
func (w *SanitizingModelWrapper) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	prepared := SanitizeMessages(input)
	return w.underlying.Generate(ctx, prepared, opts...)
}

// Stream 流式生成前统一清洗消息。
func (w *SanitizingModelWrapper) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	prepared := SanitizeMessages(input)
	return w.underlying.Stream(ctx, prepared, opts...)
}

// WithTools 返回带工具的新模型实例，并保留消息清洗行为。
func (w *SanitizingModelWrapper) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	newUnderlying, err := w.underlying.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &SanitizingModelWrapper{underlying: newUnderlying}, nil
}
