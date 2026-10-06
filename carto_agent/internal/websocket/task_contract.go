package websocket

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gorilla/websocket"
)

type taskRequestOperation string

const (
	taskRequestOperationCreate taskRequestOperation = "create"
	taskRequestOperationCancel taskRequestOperation = "cancel"
	taskRequestOperationRetry  taskRequestOperation = "retry"
)

type taskRequestAck struct {
	SessionID      string
	RequestID      string
	IdempotencyKey string
	Operation      string
	Accepted       bool
	Deduped        bool
}

var errTaskRequestSessionIDRequired = errors.New("task requests require session_id")
var errTaskRequestIdempotencyKeyRequired = errors.New("task requests require idempotency_key")
var errTaskRequestSessionOwnerRequired = errors.New("task requests require current session owner")

func normalizeTaskRequestOperation(msgType string) (taskRequestOperation, bool) {
	switch strings.TrimSpace(msgType) {
	case MsgTypeTaskCreate:
		return taskRequestOperationCreate, true
	case MsgTypeTaskCancel:
		return taskRequestOperationCancel, true
	case MsgTypeTaskRetry:
		return taskRequestOperationRetry, true
	default:
		return "", false
	}
}

func extractTaskRequestID(msg Message) string {
	if trimmed := strings.TrimSpace(msg.RequestID); trimmed != "" {
		return trimmed
	}
	if msg.Data == nil {
		return ""
	}
	requestID, _ := msg.Data["request_id"].(string)
	return strings.TrimSpace(requestID)
}

func extractTaskIdempotencyKey(msg Message) string {
	if trimmed := strings.TrimSpace(msg.IdempotencyKey); trimmed != "" {
		return trimmed
	}
	if msg.Data == nil {
		return ""
	}
	value, _ := msg.Data["idempotency_key"].(string)
	return strings.TrimSpace(value)
}

func (s *DeepServer) normalizeTaskRequestSessionID(_ *websocket.Conn, msg Message) string {
	if trimmed := strings.TrimSpace(msg.SessionID); trimmed != "" {
		return trimmed
	}
	if msg.Data != nil {
		if sessionID, _ := msg.Data["session_id"].(string); strings.TrimSpace(sessionID) != "" {
			return strings.TrimSpace(sessionID)
		}
	}
	return ""
}

func taskRequestLedgerKey(sessionID string, operation taskRequestOperation, idempotencyKey string, requestID string) string {
	return fmt.Sprintf("%s|%s|%s|%s",
		strings.TrimSpace(sessionID),
		strings.TrimSpace(string(operation)),
		strings.TrimSpace(idempotencyKey),
		strings.TrimSpace(requestID),
	)
}

func (s *DeepServer) claimTaskRequest(conn *websocket.Conn, msg Message) (taskRequestAck, error) {
	operation, ok := normalizeTaskRequestOperation(msg.Type)
	if !ok {
		return taskRequestAck{}, nil
	}

	ack := taskRequestAck{
		SessionID:      s.normalizeTaskRequestSessionID(conn, msg),
		RequestID:      extractTaskRequestID(msg),
		IdempotencyKey: extractTaskIdempotencyKey(msg),
		Operation:      string(operation),
		Accepted:       true,
	}
	if ack.SessionID == "" {
		return taskRequestAck{}, errTaskRequestSessionIDRequired
	}
	if ack.IdempotencyKey == "" {
		return taskRequestAck{}, errTaskRequestIdempotencyKeyRequired
	}

	ledgerKey := taskRequestLedgerKey(ack.SessionID, operation, ack.IdempotencyKey, ack.RequestID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.taskRequestLedger == nil {
		s.taskRequestLedger = make(map[string]taskRequestAck)
	}
	if previous, exists := s.taskRequestLedger[ledgerKey]; exists {
		previous.Deduped = true
		previous.Accepted = true
		return previous, nil
	}
	s.taskRequestLedger[ledgerKey] = ack
	return ack, nil
}

func (s *DeepServer) authorizeTaskRequestOwner(conn *websocket.Conn, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return errTaskRequestSessionIDRequired
	}
	s.mu.Lock()
	owner := s.sessionOwners[strings.TrimSpace(sessionID)]
	s.mu.Unlock()
	if owner == nil {
		return nil
	}
	if !s.isSessionOwner(conn, sessionID) {
		return errTaskRequestSessionOwnerRequired
	}
	return nil
}

func (s *DeepServer) sendTaskAck(conn *websocket.Conn, ack taskRequestAck) {
	if s == nil {
		return
	}
	_ = s.sendMessage(conn, Message{
		Type:      MsgTypeTaskAck,
		SessionID: strings.TrimSpace(ack.SessionID),
		Data: map[string]any{
			"session_id":       strings.TrimSpace(ack.SessionID),
			"request_id":       strings.TrimSpace(ack.RequestID),
			"idempotency_key":  strings.TrimSpace(ack.IdempotencyKey),
			"operation":        strings.TrimSpace(ack.Operation),
			"accepted":         ack.Accepted,
			"deduped":          ack.Deduped,
			"acknowledged":     true,
			"request_contract": "t9",
		},
	})
}

func (s *DeepServer) enforceTaskRequestContract(conn *websocket.Conn, msg Message) (taskRequestAck, bool) {
	if _, err := resolveMessageProtocolVersion(msg); err != nil {
		s.sendSessionErrorWithCode(conn, s.normalizeTaskRequestSessionID(conn, msg), "WebSocket 协议版本不受支持。", ErrorCodeUnsupportedProtocolVersion)
		return taskRequestAck{}, false
	}
	sessionID := s.normalizeTaskRequestSessionID(conn, msg)
	if err := s.authorizeTaskRequestOwner(conn, sessionID); err != nil {
		switch {
		case errors.Is(err, errTaskRequestSessionIDRequired):
			s.sendSessionErrorWithCode(conn, sessionID, "任务请求缺少 session_id，已被拒绝。", ErrorCodeSessionIDRequired)
		case errors.Is(err, errTaskRequestSessionOwnerRequired):
			s.sendSessionErrorWithCode(conn, sessionID, "当前连接不是该会话的 owner，任务请求已被拒绝。", ErrorCodeSessionOwnerRequired)
		default:
			s.sendSessionErrorWithCode(conn, sessionID, err.Error(), ErrorCodePlanProtocolViolation)
		}
		return taskRequestAck{}, false
	}
	ack, err := s.claimTaskRequest(conn, msg)
	if err != nil {
		switch {
		case errors.Is(err, errTaskRequestSessionIDRequired):
			s.sendSessionErrorWithCode(conn, sessionID, "任务请求缺少 session_id，已被拒绝。", ErrorCodeSessionIDRequired)
		case errors.Is(err, errTaskRequestIdempotencyKeyRequired):
			s.sendSessionErrorWithCode(conn, sessionID, "任务请求缺少 idempotency_key，已被拒绝。", ErrorCodeIdempotencyKeyRequired)
		default:
			s.sendSessionErrorWithCode(conn, sessionID, err.Error(), ErrorCodePlanProtocolViolation)
		}
		return taskRequestAck{}, false
	}
	if ack.Operation == "" {
		return taskRequestAck{}, true
	}
	s.sendTaskAck(conn, ack)
	if ack.Deduped {
		return ack, false
	}
	return ack, true
}
