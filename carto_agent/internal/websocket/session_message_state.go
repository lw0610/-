package websocket

import (
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/autocarto/carto_agent/internal/llm"
	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/autocarto/carto_agent/internal/session"
)

const defaultSessionHydrateMessageLimit = 40

func normalizeSessionMessageRole(role string) schema.RoleType {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		return schema.User
	case "assistant":
		return schema.Assistant
	case "tool":
		return schema.Tool
	case "system":
		return schema.System
	default:
		return schema.System
	}
}

func decodeSessionMessages(raw any, limit int) []*schema.Message {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	if limit == 0 {
		limit = defaultSessionHydrateMessageLimit
	}

	decoded := make([]*schema.Message, 0, len(items))
	for _, item := range items {
		payload, ok := item.(map[string]any)
		if !ok {
			continue
		}

		if msg := decodeStructuredSessionMessage(payload); msg != nil {
			decoded = append(decoded, msg)
			continue
		}

		content, _ := payload["content"].(string)
		if strings.TrimSpace(content) == "" {
			continue
		}
		role, _ := payload["role"].(string)
		decoded = append(decoded, &schema.Message{
			Role:    normalizeSessionMessageRole(role),
			Content: content,
		})
	}

	if limit > 0 && len(decoded) > limit {
		decoded = decoded[len(decoded)-limit:]
	}
	return decoded
}

func decodeStructuredSessionMessage(payload map[string]any) *schema.Message {
	if len(payload) == 0 {
		return nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	var msg schema.Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil
	}

	msg.Role = normalizeSessionMessageRole(string(msg.Role))
	if payload["msg_type"] == "interrupt" {
		if data, ok := payload["data"].(map[string]any); ok {
			raw, _ := json.Marshal(data)
			var card struct {
				Contexts []struct {
					Info struct {
						Type string `json:"type"`
					} `json:"info"`
				} `json:"interrupt_contexts"`
			}
			if json.Unmarshal(raw, &card) == nil {
				for _, item := range card.Contexts {
					if item.Info.Type == "knowledge_review" {
						if msg.Extra == nil {
							msg.Extra = map[string]any{}
						}
						msg.Extra["knowledge_review_card"] = data
						break
					}
				}
			}
		}
	}
	if !hasMeaningfulSessionMessageContent(&msg) {
		return nil
	}

	// 清洗 tool 消息的 UserInputMultiContent，避免 OpenAI 兼容后端 role 校验报错。
	llm.SanitizeToolMultiContent([]*schema.Message{&msg})

	return &msg
}

func hasMeaningfulSessionMessageContent(msg *schema.Message) bool {
	if msg == nil {
		return false
	}
	if strings.TrimSpace(msg.Content) != "" || strings.TrimSpace(msg.Name) != "" ||
		strings.TrimSpace(msg.ToolCallID) != "" || strings.TrimSpace(msg.ToolName) != "" ||
		strings.TrimSpace(msg.ReasoningContent) != "" {
		return true
	}
	return len(msg.ToolCalls) > 0 || len(msg.MultiContent) > 0 || len(msg.UserInputMultiContent) > 0 ||
		len(msg.AssistantGenMultiContent) > 0 || msg.ResponseMeta != nil || len(msg.Extra) > 0
}

func (s *DeepServer) loadSessionMessages(sessionID, projectPath string) []*schema.Message {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	sessionData, err := s.LoadSession(sessionID, projectPath)
	if err != nil {
		return nil
	}
	return decodeSessionMessages(sessionData["messages"], defaultSessionHydrateMessageLimit)
}

func (s *DeepServer) hydrateSessionMessages(sess *session.Session, sessionID, projectPath string) int {
	if sess == nil {
		return 0
	}
	if len(sess.GetMessages()) > 0 {
		return 0
	}

	loaded := s.loadSessionMessages(sessionID, projectPath)
	if len(loaded) == 0 {
		return 0
	}

	sess.AddMessages(loaded)
	logger.Info("会话消息回灌完成: session_id=%s, count=%d", strings.TrimSpace(sessionID), len(loaded))
	return len(loaded)
}
