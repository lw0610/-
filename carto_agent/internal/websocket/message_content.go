package websocket

import (
	"encoding/json"
	"net/url"
	"path"
	"strings"

	"github.com/cloudwego/eino/schema"
)

func summarizeSchemaMessageContent(msg *schema.Message) string {
	if msg == nil {
		return ""
	}
	if text := strings.TrimSpace(msg.Content); text != "" {
		return text
	}
	if text := summarizeMessageInputParts(msg.UserInputMultiContent); text != "" {
		return text
	}
	if text := summarizeMessageOutputParts(msg.AssistantGenMultiContent); text != "" {
		return text
	}
	if text := strings.TrimSpace(msg.ReasoningContent); text != "" {
		return text
	}
	return ""
}

func summarizeMessageInputParts(parts []schema.MessageInputPart) string {
	if len(parts) == 0 {
		return ""
	}

	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case schema.ChatMessagePartTypeText:
			if text := strings.TrimSpace(part.Text); text != "" {
				lines = append(lines, text)
			}
		case schema.ChatMessagePartTypeImageURL:
			lines = append(lines, "[图片]")
		case schema.ChatMessagePartTypeAudioURL:
			lines = append(lines, "[音频]")
		case schema.ChatMessagePartTypeVideoURL:
			lines = append(lines, "[视频]")
		case schema.ChatMessagePartTypeFileURL:
			lines = append(lines, summarizeInputFilePart(part.File))
		default:
			lines = append(lines, "[多模态内容]")
		}
	}
	return strings.Join(lines, "\n")
}

func summarizeMessageOutputParts(parts []schema.MessageOutputPart) string {
	if len(parts) == 0 {
		return ""
	}

	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case schema.ChatMessagePartTypeText:
			if text := strings.TrimSpace(part.Text); text != "" {
				lines = append(lines, text)
			}
		case schema.ChatMessagePartTypeImageURL:
			lines = append(lines, "[图片]")
		case schema.ChatMessagePartTypeAudioURL:
			lines = append(lines, "[音频]")
		case schema.ChatMessagePartTypeVideoURL:
			lines = append(lines, "[视频]")
		case schema.ChatMessagePartTypeReasoning:
			if part.Reasoning != nil && strings.TrimSpace(part.Reasoning.Text) != "" {
				lines = append(lines, part.Reasoning.Text)
			}
		default:
			lines = append(lines, "[多模态输出]")
		}
	}
	return strings.Join(lines, "\n")
}

func summarizeInputFilePart(file *schema.MessageInputFile) string {
	if file == nil {
		return "[文件]"
	}
	if name := strings.TrimSpace(file.Name); name != "" {
		return "[文件] " + name
	}
	if file.URL != nil {
		if name := summarizeURLBasename(*file.URL); name != "" {
			return "[文件] " + name
		}
	}
	return "[文件]"
}

func summarizeURLBasename(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err == nil {
		name := path.Base(parsed.Path)
		if name != "." && name != "/" {
			return name
		}
	}
	return path.Base(trimmed)
}

func messageInputPartsPayload(parts []schema.MessageInputPart) []map[string]any {
	if len(parts) == 0 {
		return nil
	}
	raw, err := json.Marshal(parts)
	if err != nil {
		return nil
	}
	var payload []map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	return payload
}
