package websocket

import (
	"fmt"
	"strings"
	"time"
)

type runEventOptions struct {
	Sequence        uint64
	TimestampMillis int64
}

func buildRunEventMessage(msg Message) (Message, bool) {
	return buildRunEventMessageWithOptions(msg, runEventOptions{})
}

func buildRunEventMessageWithOptions(msg Message, options runEventOptions) (Message, bool) {
	eventType := normalizeRunEventType(msg)
	if eventType == "" || msg.Type == MsgTypeRunEvent {
		return Message{}, false
	}

	payload := map[string]any{}
	for key, value := range msg.Data {
		payload[key] = value
	}
	if msg.Content != "" {
		payload["content"] = msg.Content
	}
	if msg.SessionID != "" {
		payload["session_id"] = msg.SessionID
	}
	if msg.Agent != "" {
		payload["agent"] = msg.Agent
	}
	if msg.AgentDisplayName != "" {
		payload["agent_display_name"] = msg.AgentDisplayName
	}
	if msg.Phase != "" {
		payload["phase"] = msg.Phase
	}

	timestamp := options.TimestampMillis
	if timestamp <= 0 {
		timestamp = time.Now().UnixMilli()
	}

	runEventData := map[string]any{
		"event":       eventType,
		"source_type": msg.Type,
		"timestamp":   timestamp,
		"payload":     payload,
	}
	if options.Sequence > 0 {
		runEventData["seq"] = options.Sequence
		if sessionID := strings.TrimSpace(msg.SessionID); sessionID != "" {
			eventID := fmt.Sprintf("%s:%d", sessionID, options.Sequence)
			runEventData["event_id"] = eventID
			runEventData["dedupe_key"] = eventID
		}
	}

	return Message{
		Type:             MsgTypeRunEvent,
		Content:          msg.Content,
		SessionID:        msg.SessionID,
		Agent:            msg.Agent,
		AgentDisplayName: msg.AgentDisplayName,
		Phase:            msg.Phase,
		Data:             runEventData,
	}, true
}

func normalizeRunEventType(msg Message) string {
	switch msg.Type {
	case MsgTypeFlowEvent:
		return stringValue(msg.Data["event"])
	case MsgTypeThinking:
		return "thinking"
	case MsgTypeResponse:
		return "result"
	case MsgTypeError:
		return "error"
	case MsgTypeCancelled:
		return "cancelled"
	case MsgTypeTaskRunningChange:
		return "running_state"
	case MsgTypeTaskListUpdate:
		return "todo_update"
	case MsgTypeInterruptRequest:
		return "interrupt"
	case MsgTypeModeState:
		return "mode_state"
	case MsgTypePhaseTransition:
		return "phase_transition"
	case MsgTypeStatus:
		return "status"
	case MsgTypeSessionState:
		return "session_state"
	case MsgTypeContextResponse:
		return "context_response"
	case "agent_workflow":
		return "workflow"
	case MsgTypeTool:
		return "tool"
	case MsgTypeAgent:
		return "agent"
	default:
		return ""
	}
}
