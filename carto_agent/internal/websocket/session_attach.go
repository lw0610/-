package websocket

import (
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"math"
	"strings"

	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/logger"
)

const sessionRunEventBufferLimit = 256

type sessionAttachmentBundle struct {
	Messages           []Message
	SnapshotHit        bool
	RuntimeStatePushed bool
	ReplayCount        int
	ReplayRequired     bool
	OldestAvailableSeq uint64
	LatestAvailableSeq uint64
	SubscriberCount    int
}

type sessionAttachmentDeliveryReport struct {
	SessionID          string
	LastSeenSeq        uint64
	SnapshotHit        bool
	RuntimeStatePushed bool
	ReplayCount        int
	SubscriberCount    int
	SentCount          int
	SendErrorCount     int
	LastSendError      string
	LogLevel           logger.LogLevel
}

func parseUint64Like(raw any) uint64 {
	switch value := raw.(type) {
	case nil:
		return 0
	case uint64:
		return value
	case uint32:
		return uint64(value)
	case uint:
		return uint64(value)
	case int64:
		if value <= 0 {
			return 0
		}
		return uint64(value)
	case int:
		if value <= 0 {
			return 0
		}
		return uint64(value)
	case float64:
		if value <= 0 {
			return 0
		}
		if value > math.MaxUint64 {
			return math.MaxUint64
		}
		return uint64(value)
	case float32:
		if value <= 0 {
			return 0
		}
		return uint64(value)
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0
		}
		var parsed uint64
		for _, ch := range trimmed {
			if ch < '0' || ch > '9' {
				return 0
			}
			parsed = parsed*10 + uint64(ch-'0')
		}
		return parsed
	default:
		return 0
	}
}

func (s *DeepServer) attachSessionSubscription(conn *websocket.Conn, sessionID string) string {
	if conn == nil {
		return ""
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionSubscribers == nil {
		s.sessionSubscribers = make(map[string]map[*websocket.Conn]struct{})
	}
	if s.connSubscribedSessions == nil {
		s.connSubscribedSessions = make(map[*websocket.Conn]map[string]struct{})
	}
	connSessions := s.connSubscribedSessions[conn]
	if connSessions == nil {
		connSessions = make(map[string]struct{})
		s.connSubscribedSessions[conn] = connSessions
	}
	connSessions[sid] = struct{}{}
	subscribers := s.sessionSubscribers[sid]
	if subscribers == nil {
		subscribers = make(map[*websocket.Conn]struct{})
		s.sessionSubscribers[sid] = subscribers
	}
	subscribers[conn] = struct{}{}
	if s.sessionOwners == nil {
		s.sessionOwners = make(map[string]*websocket.Conn)
	}
	if s.sessionOwners[sid] == nil {
		s.sessionOwners[sid] = conn
	}
	return sid
}

func (s *DeepServer) detachSessionSubscription(conn *websocket.Conn, sessionID string) {
	if conn == nil {
		return
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if sessions, ok := s.connSubscribedSessions[conn]; ok {
		delete(sessions, sid)
		if len(sessions) == 0 {
			delete(s.connSubscribedSessions, conn)
		}
	}
	if subscribers, ok := s.sessionSubscribers[sid]; ok {
		delete(subscribers, conn)
		if len(subscribers) == 0 {
			delete(s.sessionSubscribers, sid)
			delete(s.sessionOwners, sid)
		} else if owner := s.sessionOwners[sid]; owner == conn {
			for nextConn := range subscribers {
				s.sessionOwners[sid] = nextConn
				break
			}
		}
	}
}

func (s *DeepServer) detachAllSessionSubscriptions(conn *websocket.Conn) {
	if conn == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	sessions := s.connSubscribedSessions[conn]
	if len(sessions) == 0 {
		delete(s.connSubscribedSessions, conn)
		return
	}
	for sid := range sessions {
		if subscribers, ok := s.sessionSubscribers[sid]; ok {
			delete(subscribers, conn)
			if len(subscribers) == 0 {
				delete(s.sessionSubscribers, sid)
				delete(s.sessionOwners, sid)
			} else if owner := s.sessionOwners[sid]; owner == conn {
				for nextConn := range subscribers {
					s.sessionOwners[sid] = nextConn
					break
				}
			}
		}
	}
	delete(s.connSubscribedSessions, conn)
}

func (s *DeepServer) isSessionOwner(conn *websocket.Conn, sessionID string) bool {
	if conn == nil {
		return false
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	owner := s.sessionOwners[sid]
	return owner == conn
}

func (s *DeepServer) listSessionSubscribers(sessionID string) []*websocket.Conn {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	subscribers := s.sessionSubscribers[sid]
	if len(subscribers) == 0 {
		return nil
	}
	connections := make([]*websocket.Conn, 0, len(subscribers))
	for conn := range subscribers {
		connections = append(connections, conn)
	}
	return connections
}

func (s *DeepServer) nextSessionRunEventSequence(sessionID string) uint64 {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return 0
	}
	s.eventSequenceMu.Lock()
	defer s.eventSequenceMu.Unlock()
	run, ok := s.flowContext(sid)
	path := ""
	if ok {
		path, _ = sequencePath(sid, run.ProjectPath)
	}
	s.mu.Lock()
	if s.sessionRunEventSeq == nil {
		s.sessionRunEventSeq = map[string]uint64{}
	}
	next := s.sessionRunEventSeq[sid] + 1
	s.sessionRunEventSeq[sid] = next
	s.mu.Unlock()
	if path != "" {
		if err := writeEventSequence(path, next); err != nil {
			logger.Warn("保存事件序号失败: session_id=%s err=%v", sid, err)
		}
	}
	return next
}

func (s *DeepServer) recordSessionRunEvent(sessionID string, runEvent Message) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || runEvent.Type != MsgTypeRunEvent {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionRunEventBuffers == nil {
		s.sessionRunEventBuffers = make(map[string][]Message)
	}
	if dedupeKey, _ := runEvent.Data["dedupe_key"].(string); strings.TrimSpace(dedupeKey) != "" {
		for _, existing := range s.sessionRunEventBuffers[sid] {
			existingKey, _ := existing.Data["dedupe_key"].(string)
			if strings.TrimSpace(existingKey) == strings.TrimSpace(dedupeKey) {
				return
			}
		}
	}
	buffer := append(s.sessionRunEventBuffers[sid], runEvent)
	if len(buffer) > sessionRunEventBufferLimit {
		buffer = append([]Message(nil), buffer[len(buffer)-sessionRunEventBufferLimit:]...)
	}
	s.sessionRunEventBuffers[sid] = buffer
}

func (s *DeepServer) replaySessionRunEvents(sessionID string, afterSeq uint64) []Message {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil
	}
	if afterSeq == 0 {
		logger.Debug("会话 run_event 回放跳过: session_id=%s after_seq=0", sid)
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	buffer := s.sessionRunEventBuffers[sid]
	if len(buffer) == 0 {
		logger.Debug("会话 run_event 回放为空: session_id=%s after_seq=%d buffer=0", sid, afterSeq)
		return nil
	}
	replay := make([]Message, 0, len(buffer))
	for _, event := range buffer {
		seq := parseUint64Like(event.Data["seq"])
		if seq <= afterSeq {
			continue
		}
		replay = append(replay, event)
	}
	logger.Debug("会话 run_event 回放计算完成: session_id=%s after_seq=%d replay=%d buffer=%d", sid, afterSeq, len(replay), len(buffer))
	if len(replay) == 0 {
		return nil
	}
	return replay
}

func (s *DeepServer) computeSessionReplayWindow(sessionID string, afterSeq uint64) ([]Message, uint64, uint64, bool) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, 0, 0, false
	}
	if afterSeq == 0 {
		return nil, 0, 0, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	buffer := append([]Message(nil), s.sessionRunEventBuffers[sid]...)
	if len(buffer) == 0 {
		return nil, 0, 0, false
	}

	oldest := parseUint64Like(buffer[0].Data["seq"])
	latest := parseUint64Like(buffer[len(buffer)-1].Data["seq"])
	if oldest > 0 && afterSeq < oldest-1 {
		return nil, oldest, latest, true
	}

	replay := make([]Message, 0, len(buffer))
	for _, event := range buffer {
		seq := parseUint64Like(event.Data["seq"])
		if seq <= afterSeq {
			continue
		}
		replay = append(replay, event)
	}
	return replay, oldest, latest, false
}

func (s *DeepServer) buildSessionAttachmentBundle(sessionID string, lastSeenSeq uint64) sessionAttachmentBundle {
	sid := strings.TrimSpace(sessionID)
	bundle := sessionAttachmentBundle{
		Messages: make([]Message, 0, 3),
	}
	if sid == "" {
		return bundle
	}
	boundProjectPath := ""
	if state, ok := s.activeSessionRuntime(sid); ok {
		boundProjectPath = strings.TrimSpace(state.BoundProjectPath)
	} else {
		boundProjectPath = strings.TrimSpace(s.sessionRuntimeProjectionSnapshot(sid, nil).RuntimeState.BoundProjectPath)
	}
	if sessionData, err := s.LoadSession(sid, boundProjectPath); err == nil {
		if state, ok := sessionData["flow_run"].(*flowrun.RunState); ok && state != nil && state.Status == "waiting_user" {
			_, _ = s.restoreFlowPending(context.Background(), sid, boundProjectPath, state.ID)
		}
		payload := buildAttachedSessionStatePayload(sessionData)
		if snapshotEvent, ok := buildRunEventMessage(Message{Type: MsgTypeSessionState, SessionID: sid, Data: payload}); ok {
			bundle.Messages = append(bundle.Messages, snapshotEvent)
			bundle.SnapshotHit = true
		}
	}
	if state, ok := s.activeSessionRuntime(sid); ok {
		bundle.Messages = append(bundle.Messages, Message{
			Type:      MsgTypeSessionRuntimeState,
			SessionID: sid,
			Data: map[string]any{
				"session_id":          strings.TrimSpace(state.SessionID),
				"run_id":              strings.TrimSpace(state.RunID),
				"status":              strings.TrimSpace(state.Status),
				"execution_mode":      strings.TrimSpace(state.ExecutionMode),
				"phase":               strings.TrimSpace(state.Phase),
				"bound_instance_id":   strings.TrimSpace(state.BoundInstanceID),
				"bound_project_path":  strings.TrimSpace(state.BoundProjectPath),
				"checkpoint_id":       strings.TrimSpace(state.CheckpointID),
				"runtime_revision":    strings.TrimSpace(state.RuntimeRevision),
				"active_agent_config": strings.TrimSpace(state.ActiveAgentConfig),
				"last_error":          strings.TrimSpace(state.LastError),
				"failure_code":        strings.TrimSpace(state.FailureCode),
				"failure_reason":      strings.TrimSpace(state.FailureReason),
				"updated_at":          strings.TrimSpace(state.UpdatedAt),
			},
		})
		bundle.RuntimeStatePushed = true
		if state.Status == sessionRuntimeStatusInterrupted {
			if pending, exists := s.getPendingInterrupt(sid); exists {
				if interruptEvent, ok := buildRunEventMessage(Message{
					Type:      MsgTypeInterruptRequest,
					SessionID: sid,
					Data:      pendingInterruptPayload(pending),
				}); ok {
					bundle.Messages = append(bundle.Messages, interruptEvent)
				}
			}
		}
	}
	replay, oldestSeq, latestSeq, replayRequired := s.computeSessionReplayWindow(sid, lastSeenSeq)
	bundle.ReplayRequired = replayRequired
	bundle.OldestAvailableSeq = oldestSeq
	bundle.LatestAvailableSeq = latestSeq
	replay = markReplayRunEvents(replay)
	bundle.ReplayCount = len(replay)
	bundle.Messages = append(bundle.Messages, replay...)
	bundle.SubscriberCount = len(s.listSessionSubscribers(sid))
	return bundle
}

func markReplayRunEvents(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]Message, 0, len(messages))
	for _, message := range messages {
		if message.Type != MsgTypeRunEvent {
			cloned = append(cloned, message)
			continue
		}
		copied := message
		data := make(map[string]any, len(message.Data)+1)
		for key, value := range message.Data {
			data[key] = value
		}
		data["replayed"] = true
		copied.Data = data
		cloned = append(cloned, copied)
	}
	return cloned
}

func (s *DeepServer) buildSessionAttachmentMessages(sessionID string, lastSeenSeq uint64) []Message {
	return s.buildSessionAttachmentBundle(sessionID, lastSeenSeq).Messages
}

func deliverSessionAttachmentBundle(sessionID string, lastSeenSeq uint64, bundle sessionAttachmentBundle, send func(Message) error) sessionAttachmentDeliveryReport {
	report := sessionAttachmentDeliveryReport{
		SessionID:          strings.TrimSpace(sessionID),
		LastSeenSeq:        lastSeenSeq,
		SnapshotHit:        bundle.SnapshotHit,
		RuntimeStatePushed: bundle.RuntimeStatePushed,
		ReplayCount:        bundle.ReplayCount,
		SubscriberCount:    bundle.SubscriberCount,
		LogLevel:           logger.INFO,
	}
	for _, message := range bundle.Messages {
		if send == nil {
			report.SendErrorCount++
			report.LastSendError = "missing sender"
			report.LogLevel = logger.WARN
			continue
		}
		if err := send(message); err != nil {
			report.SendErrorCount++
			report.LastSendError = err.Error()
			report.LogLevel = logger.WARN
			continue
		}
		report.SentCount++
	}
	return report
}

func (s *DeepServer) syncSessionAttachment(conn *websocket.Conn, requestID, sessionID string, lastSeenSeq uint64) {
	sid := s.attachSessionSubscription(conn, sessionID)
	if sid == "" {
		return
	}

	bundle := s.buildSessionAttachmentBundle(sid, lastSeenSeq)
	if bundle.ReplayRequired {
		s.sendSessionResponse(conn, requestID, "attach", false, map[string]any{
			"session_id":           sid,
			"last_seen_seq":        lastSeenSeq,
			"replay_required":      true,
			"oldest_available_seq": bundle.OldestAvailableSeq,
			"latest_available_seq": bundle.LatestAvailableSeq,
		}, fmt.Errorf("session replay cursor is stale"), newWebSocketRequestEnvelope(
			"REPLAY_REQUIRED",
			"会话回放窗口已过期，需要重新同步。",
			"websocket",
			"session_attach",
			"请重新加载会话快照后再重连。",
			map[string]any{
				"action":               "attach",
				"session_id":           sid,
				"last_seen_seq":        lastSeenSeq,
				"oldest_available_seq": bundle.OldestAvailableSeq,
				"latest_available_seq": bundle.LatestAvailableSeq,
			},
		))
		return
	}
	report := deliverSessionAttachmentBundle(sid, lastSeenSeq, bundle, func(message Message) error {
		return s.sendMessage(conn, message)
	})
	if report.LogLevel >= logger.WARN {
		logger.Warn("会话 attach 部分失败: session_id=%s last_seen_seq=%d snapshot_hit=%t runtime_state=%t replay=%d subscribers=%d sent=%d send_errors=%d last_error=%s", report.SessionID, report.LastSeenSeq, report.SnapshotHit, report.RuntimeStatePushed, report.ReplayCount, report.SubscriberCount, report.SentCount, report.SendErrorCount, report.LastSendError)
		return
	}
	logger.Info("会话 attach 完成: session_id=%s last_seen_seq=%d snapshot_hit=%t runtime_state=%t replay=%d subscribers=%d sent=%d send_errors=%d", report.SessionID, report.LastSeenSeq, report.SnapshotHit, report.RuntimeStatePushed, report.ReplayCount, report.SubscriberCount, report.SentCount, report.SendErrorCount)
}
