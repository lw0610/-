package websocket

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/logger"
)

func TestSessionRunEventReplayFiltersBySequence(t *testing.T) {
	server := NewDeepServer(0, nil)

	first, ok := buildRunEventMessageWithOptions(Message{
		Type:      MsgTypeStatus,
		SessionID: "sid-1",
		Content:   "first",
	}, runEventOptions{Sequence: 1, TimestampMillis: 1000})
	if !ok {
		t.Fatal("expected first run_event to be built")
	}
	second, ok := buildRunEventMessageWithOptions(Message{
		Type:      MsgTypeStatus,
		SessionID: "sid-1",
		Content:   "second",
	}, runEventOptions{Sequence: 2, TimestampMillis: 2000})
	if !ok {
		t.Fatal("expected second run_event to be built")
	}

	server.recordSessionRunEvent("sid-1", first)
	server.recordSessionRunEvent("sid-1", second)

	replay := server.replaySessionRunEvents("sid-1", 1)
	if len(replay) != 1 {
		t.Fatalf("expected 1 replayed event, got %d", len(replay))
	}
	if got := parseUint64Like(replay[0].Data["seq"]); got != 2 {
		t.Fatalf("expected replay seq=2, got %d", got)
	}
	if replay[0].Content != "second" {
		t.Fatalf("expected replayed content to be second, got %q", replay[0].Content)
	}
}

func TestSessionAttachAndDetachSubscriptionLifecycle(t *testing.T) {
	server := NewDeepServer(0, nil)
	conn := &websocket.Conn{}

	if got := server.attachSessionSubscription(conn, "sid-attach"); got != "sid-attach" {
		t.Fatalf("expected normalized session id sid-attach, got %q", got)
	}
	if subscribers := server.listSessionSubscribers("sid-attach"); len(subscribers) != 1 {
		t.Fatalf("expected 1 subscriber after attach, got %d", len(subscribers))
	}

	server.detachSessionSubscription(conn, "sid-attach")
	if subscribers := server.listSessionSubscribers("sid-attach"); len(subscribers) != 0 {
		t.Fatalf("expected 0 subscribers after detach, got %d", len(subscribers))
	}
}

func TestSessionOwnerTransfersWhenCurrentOwnerDetaches(t *testing.T) {
	server := NewDeepServer(0, nil)
	ownerConn := &websocket.Conn{}
	observerConn := &websocket.Conn{}

	server.attachSessionSubscription(ownerConn, "sid-owner-transfer")
	server.attachSessionSubscription(observerConn, "sid-owner-transfer")
	if !server.isSessionOwner(ownerConn, "sid-owner-transfer") {
		t.Fatal("expected first attached connection to own session lease")
	}

	server.detachSessionSubscription(ownerConn, "sid-owner-transfer")
	if !server.isSessionOwner(observerConn, "sid-owner-transfer") {
		t.Fatal("expected remaining subscriber to inherit session lease")
	}
}

func TestBuildSessionAttachmentBundleTracksObservabilityFlags(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "sid-attach-summary"

	if err := server.saveSessionToFile(map[string]any{
		"session_id": sessionID,
		"name":       "Attach 摘要",
		"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
		"created_at": "2026-03-13T10:00:00Z",
		"updated_at": "2026-03-13T10:01:00Z",
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	server.markSessionRuntimeRunning(sessionID, "run-attach-summary", executionModeBuild, "building", projectPath, "inst-a")
	first, ok := buildRunEventMessageWithOptions(Message{Type: MsgTypeStatus, SessionID: sessionID, Content: "first"}, runEventOptions{Sequence: 1, TimestampMillis: 1000})
	if !ok {
		t.Fatal("expected first run_event")
	}
	second, ok := buildRunEventMessageWithOptions(Message{Type: MsgTypeStatus, SessionID: sessionID, Content: "second"}, runEventOptions{Sequence: 2, TimestampMillis: 2000})
	if !ok {
		t.Fatal("expected second run_event")
	}
	server.recordSessionRunEvent(sessionID, first)
	server.recordSessionRunEvent(sessionID, second)
	conn := &websocket.Conn{}
	server.attachSessionSubscription(conn, sessionID)

	bundle := server.buildSessionAttachmentBundle(sessionID, 1)
	if !bundle.SnapshotHit {
		t.Fatal("expected snapshot hit")
	}
	if !bundle.RuntimeStatePushed {
		t.Fatal("expected runtime_state pushed")
	}
	if bundle.ReplayCount != 1 {
		t.Fatalf("expected replay count 1, got %d", bundle.ReplayCount)
	}
	if bundle.SubscriberCount != 1 {
		t.Fatalf("expected subscriber count 1, got %d", bundle.SubscriberCount)
	}
	if len(bundle.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(bundle.Messages))
	}
	runtimeStateFound := false
	for _, message := range bundle.Messages {
		if message.Type != MsgTypeSessionRuntimeState {
			continue
		}
		runtimeStateFound = true
		if got := message.Data["bound_instance_id"]; got != "inst-a" {
			t.Fatalf("unexpected bound_instance_id in runtime_state bundle: %#v", got)
		}
	}
	if !runtimeStateFound {
		t.Fatal("expected runtime_state message in attachment bundle")
	}
}

func TestDeliverSessionAttachmentBundleTracksSendResults(t *testing.T) {
	bundle := sessionAttachmentBundle{
		Messages: []Message{
			{Type: MsgTypeSessionState, SessionID: "sid-log"},
			{Type: MsgTypeSessionRuntimeState, SessionID: "sid-log"},
			{Type: MsgTypeRunEvent, SessionID: "sid-log"},
		},
		SnapshotHit:        true,
		RuntimeStatePushed: true,
		ReplayCount:        1,
		SubscriberCount:    2,
	}

	attempts := 0
	report := deliverSessionAttachmentBundle("sid-log", 7, bundle, func(msg Message) error {
		attempts++
		if attempts == 2 {
			return errors.New("boom")
		}
		return nil
	})

	if report.SessionID != "sid-log" {
		t.Fatalf("unexpected session id: %s", report.SessionID)
	}
	if report.LastSeenSeq != 7 {
		t.Fatalf("unexpected last seen seq: %d", report.LastSeenSeq)
	}
	if report.SentCount != 2 {
		t.Fatalf("expected sent count 2, got %d", report.SentCount)
	}
	if report.SendErrorCount != 1 {
		t.Fatalf("expected send error count 1, got %d", report.SendErrorCount)
	}
	if report.LogLevel != logger.WARN {
		t.Fatalf("expected warn log level, got %v", report.LogLevel)
	}
	if report.SnapshotHit != bundle.SnapshotHit || report.RuntimeStatePushed != bundle.RuntimeStatePushed || report.ReplayCount != bundle.ReplayCount || report.SubscriberCount != bundle.SubscriberCount {
		t.Fatalf("expected report to preserve bundle fields: %+v", report)
	}
	if report.LastSendError == "" {
		t.Fatal("expected last send error recorded")
	}
}

func TestBuildSessionAttachmentBundleKeepsPerWindowReplayIndependent(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "shared.qgz")
	sessionID := "sid-shared-window"

	if err := server.saveSessionToFile(map[string]any{
		"session_id": sessionID,
		"name":       "Shared Window",
		"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
		"created_at": "2026-03-13T10:00:00Z",
		"updated_at": "2026-03-13T10:01:00Z",
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	server.markSessionRuntimeRunning(sessionID, "run-shared-window", executionModeBuild, "building", projectPath, "inst-a")
	for idx, item := range []struct {
		content   string
		timestamp int64
	}{
		{content: "first", timestamp: 1000},
		{content: "second", timestamp: 2000},
		{content: "third", timestamp: 3000},
	} {
		event, ok := buildRunEventMessageWithOptions(Message{
			Type:      MsgTypeStatus,
			SessionID: sessionID,
			Content:   item.content,
		}, runEventOptions{Sequence: uint64(idx + 1), TimestampMillis: item.timestamp})
		if !ok {
			t.Fatalf("expected run_event %d", idx+1)
		}
		server.recordSessionRunEvent(sessionID, event)
	}

	connA := &websocket.Conn{}
	connB := &websocket.Conn{}
	server.attachSessionSubscription(connA, sessionID)
	server.attachSessionSubscription(connB, sessionID)

	bundleA := server.buildSessionAttachmentBundle(sessionID, 1)
	bundleB := server.buildSessionAttachmentBundle(sessionID, 2)

	if subscribers := len(server.listSessionSubscribers(sessionID)); subscribers != 2 {
		t.Fatalf("expected 2 subscribers, got %d", subscribers)
	}
	if bundleA.SubscriberCount != 2 {
		t.Fatalf("expected window A subscriber_count=2, got %d", bundleA.SubscriberCount)
	}
	if bundleB.SubscriberCount != 2 {
		t.Fatalf("expected window B subscriber_count=2, got %d", bundleB.SubscriberCount)
	}
	if bundleA.ReplayCount != 2 {
		t.Fatalf("expected window A replay count 2, got %d", bundleA.ReplayCount)
	}
	if bundleB.ReplayCount != 1 {
		t.Fatalf("expected window B replay count 1, got %d", bundleB.ReplayCount)
	}

	extractReplaySeqs := func(messages []Message) []uint64 {
		seqs := make([]uint64, 0, len(messages))
		for _, message := range messages {
			if message.Type != MsgTypeRunEvent {
				continue
			}
			seq := parseUint64Like(message.Data["seq"])
			if seq == 0 {
				continue
			}
			seqs = append(seqs, seq)
		}
		return seqs
	}

	seqsA := extractReplaySeqs(bundleA.Messages)
	if len(seqsA) != 2 || seqsA[0] != 2 || seqsA[1] != 3 {
		t.Fatalf("expected window A replay seqs [2 3], got %v", seqsA)
	}
	seqsB := extractReplaySeqs(bundleB.Messages)
	if len(seqsB) != 1 || seqsB[0] != 3 {
		t.Fatalf("expected window B replay seqs [3], got %v", seqsB)
	}
}

func TestBuildSessionAttachmentBundleSkipsReplayAtZeroAndIncludesInterruptRequest(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "interrupt.qgz")
	sessionID := "sid-interrupt-window"

	server.markSessionRuntimeRunning(sessionID, "run-interrupt", executionModeBuild, "building", projectPath, "inst-a")
	server.markSessionRuntimeInterrupted(sessionID, "cp-1")
	server.setPendingInterrupt(nil, sessionID, &PendingInterrupt{SessionID: sessionID, ProjectPath: projectPath, CheckpointID: "cp-1"})
	for idx := 1; idx <= sessionRunEventBufferLimit+4; idx++ {
		event, ok := buildRunEventMessageWithOptions(Message{Type: MsgTypeStatus, SessionID: sessionID, Content: "step"}, runEventOptions{Sequence: uint64(idx), TimestampMillis: int64(idx)})
		if !ok {
			t.Fatalf("expected run_event %d", idx)
		}
		server.recordSessionRunEvent(sessionID, event)
	}

	bundle := server.buildSessionAttachmentBundle(sessionID, 0)
	if bundle.ReplayCount != 0 {
		t.Fatalf("expected no replay when after_seq=0, got %d", bundle.ReplayCount)
	}
	interruptCount := 0
	for _, message := range bundle.Messages {
		if message.Type == MsgTypeRunEvent {
			if event, _ := message.Data["event"].(string); event == "interrupt" {
				interruptCount++
			}
			if seq := parseUint64Like(message.Data["seq"]); seq != 0 {
				t.Fatalf("expected no replayed run_event when after_seq=0, got seq=%d", seq)
			}
		}
	}
	if interruptCount != 1 {
		t.Fatalf("expected exactly one interrupt_request, got %d", interruptCount)
	}

	replay := server.replaySessionRunEvents(sessionID, 4)
	if len(replay) != sessionRunEventBufferLimit {
		t.Fatalf("expected replay to keep buffer cap %d, got %d", sessionRunEventBufferLimit, len(replay))
	}
	if firstSeq := parseUint64Like(replay[0].Data["seq"]); firstSeq != 5 {
		t.Fatalf("unexpected first capped replay seq: %d", firstSeq)
	}
}

func TestBuildSessionAttachmentBundleMarksReplayRequiredWhenCursorFallsBehindBuffer(t *testing.T) {
	server := NewDeepServer(0, nil)
	sessionID := "sid-stale-cursor"

	for idx := 1; idx <= sessionRunEventBufferLimit+4; idx++ {
		event, ok := buildRunEventMessageWithOptions(Message{Type: MsgTypeStatus, SessionID: sessionID, Content: "step"}, runEventOptions{Sequence: uint64(idx), TimestampMillis: int64(idx)})
		if !ok {
			t.Fatalf("expected run_event %d", idx)
		}
		server.recordSessionRunEvent(sessionID, event)
	}

	bundle := server.buildSessionAttachmentBundle(sessionID, 1)
	if !bundle.ReplayRequired {
		t.Fatal("expected replay_required when cursor falls behind buffer")
	}
	if bundle.OldestAvailableSeq == 0 {
		t.Fatal("expected oldest_available_seq to be populated")
	}
	if bundle.LatestAvailableSeq <= bundle.OldestAvailableSeq {
		t.Fatalf("expected latest_available_seq > oldest_available_seq, got oldest=%d latest=%d", bundle.OldestAvailableSeq, bundle.LatestAvailableSeq)
	}
	if bundle.ReplayCount != 0 {
		t.Fatalf("expected stale cursor attach to skip replay payloads, got replay_count=%d", bundle.ReplayCount)
	}
}

func TestSessionAttachReturnsReplayRequiredWhenCursorFallsBehindBuffer(t *testing.T) {
	server := NewDeepServer(0, nil)
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	server.connWriteMu[serverConn] = &sync.Mutex{}

	sessionID := "sid-stale-cursor-response"
	for idx := 1; idx <= sessionRunEventBufferLimit+4; idx++ {
		event, ok := buildRunEventMessageWithOptions(Message{Type: MsgTypeStatus, SessionID: sessionID, Content: "step"}, runEventOptions{Sequence: uint64(idx), TimestampMillis: int64(idx)})
		if !ok {
			t.Fatalf("expected run_event %d", idx)
		}
		server.recordSessionRunEvent(sessionID, event)
	}

	handled := server.handleSessionMessage(serverConn, Message{
		Type: MsgTypeSessionAttach,
		Data: map[string]any{
			"request_id":    "req-stale-attach",
			"session_id":    sessionID,
			"last_seen_seq": 1,
		},
	})
	if !handled {
		t.Fatal("expected session attach to be handled")
	}

	messages := readMessagesUntilTimeout(t, clientConn)
	if len(messages) == 0 {
		t.Fatal("expected stale cursor attach to emit websocket response")
	}

	foundReplayRequired := false
	for _, message := range messages {
		if message.Type == MsgTypeRunEvent {
			if seq := parseUint64Like(message.Data["seq"]); seq != 0 {
				t.Fatalf("expected stale cursor attach to avoid replay payloads, got run_event seq=%d", seq)
			}
		}
		if message.Type != MsgTypeSessionResponse {
			continue
		}
		if message.Data["action"] != "attach" {
			continue
		}
		foundReplayRequired = true
		t.Logf("stale attach response: %#v", message.Data)
		if message.Data["success"] != false {
			t.Fatalf("expected attach response success=false, got %#v", message.Data["success"])
		}
		if message.Data["code"] != "REPLAY_REQUIRED" {
			t.Fatalf("expected attach response code REPLAY_REQUIRED, got %#v", message.Data["code"])
		}
	}
	if !foundReplayRequired {
		t.Fatal("expected attach response with replay_required semantics")
	}
}

func TestSessionAttachReplayRequiredDoesNotDeliverSnapshotOrRuntimeState(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "stale-terminal.qgz")
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	server.connWriteMu[serverConn] = &sync.Mutex{}

	sessionID := "sid-stale-terminal"
	if err := server.saveSessionToFile(map[string]any{
		"session_id": sessionID,
		"name":       "Stale Terminal",
		"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
		"created_at": "2026-03-13T10:00:00Z",
		"updated_at": "2026-03-13T10:01:00Z",
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}
	server.markSessionRuntimeRunning(sessionID, "run-stale-terminal", executionModeBuild, "building", projectPath, "inst-terminal")

	for idx := 1; idx <= sessionRunEventBufferLimit+4; idx++ {
		event, ok := buildRunEventMessageWithOptions(Message{Type: MsgTypeStatus, SessionID: sessionID, Content: "step"}, runEventOptions{Sequence: uint64(idx), TimestampMillis: int64(idx)})
		if !ok {
			t.Fatalf("expected run_event %d", idx)
		}
		server.recordSessionRunEvent(sessionID, event)
	}

	handled := server.handleSessionMessage(serverConn, Message{
		Type: MsgTypeSessionAttach,
		Data: map[string]any{
			"request_id":    "req-stale-terminal",
			"session_id":    sessionID,
			"last_seen_seq": 1,
		},
	})
	if !handled {
		t.Fatal("expected session attach to be handled")
	}

	messages := readMessagesUntilTimeout(t, clientConn)
	if len(messages) == 0 {
		t.Fatal("expected stale cursor attach to emit websocket response")
	}

	if len(messages) != 1 {
		t.Fatalf("expected replay_required attach to stop before snapshot/runtime delivery, got %+v", messages)
	}
	message := messages[0]
	if message.Type != MsgTypeSessionResponse {
		t.Fatalf("expected only session.response on stale attach, got %+v", message)
	}
	if message.Data["code"] != "REPLAY_REQUIRED" {
		t.Fatalf("expected REPLAY_REQUIRED code, got %#v", message.Data["code"])
	}
}

func TestSessionReplayAfterReconnect(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "reconnect.qgz")
	sessionID := "sid-reconnect"

	if err := server.saveSessionToFile(map[string]any{
		"session_id": sessionID,
		"name":       "Reconnect Replay",
		"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
		"created_at": "2026-03-13T10:00:00Z",
		"updated_at": "2026-03-13T10:01:00Z",
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	server.markSessionRuntimeRunning(sessionID, "run-reconnect", executionModeBuild, "building", projectPath, "inst-a")
	for idx, content := range []string{"first", "second", "third"} {
		event, ok := buildRunEventMessageWithOptions(Message{
			Type:      MsgTypeStatus,
			SessionID: sessionID,
			Content:   content,
		}, runEventOptions{Sequence: uint64(idx + 1), TimestampMillis: int64(1000 * (idx + 1))})
		if !ok {
			t.Fatalf("expected run_event %d", idx+1)
		}
		server.recordSessionRunEvent(sessionID, event)
	}

	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	server.connWriteMu[serverConn] = &sync.Mutex{}

	handled := server.handleSessionMessage(serverConn, Message{
		Type: MsgTypeSessionAttach,
		Data: map[string]any{
			"request_id":    "req-reconnect",
			"session_id":    sessionID,
			"last_seen_seq": 1,
		},
	})
	if !handled {
		t.Fatal("expected session attach to be handled")
	}

	messages := readMessagesUntilTimeout(t, clientConn)
	replayedSeqs := make([]uint64, 0, 2)
	for _, message := range messages {
		if message.Type != MsgTypeRunEvent {
			continue
		}
		seq := parseUint64Like(message.Data["seq"])
		if seq == 0 {
			continue
		}
		replayedSeqs = append(replayedSeqs, seq)
		if replayed, _ := message.Data["replayed"].(bool); !replayed {
			t.Fatalf("expected replayed run_event seq=%d to be marked replayed, got %#v", seq, message.Data["replayed"])
		}
		expectedKey := sessionID + ":" + map[uint64]string{2: "2", 3: "3"}[seq]
		if got := message.Data["dedupe_key"]; got != expectedKey {
			t.Fatalf("expected replayed run_event dedupe_key=%s, got %#v", expectedKey, got)
		}
	}
	if len(replayedSeqs) != 2 || replayedSeqs[0] != 2 || replayedSeqs[1] != 3 {
		t.Fatalf("expected replayed seqs [2 3], got %v", replayedSeqs)
	}
}

func TestSessionAttach_ReplaysFromCursor(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "attach-cursor.qgz")
	sessionID := "sid-attach-cursor"

	if err := server.saveSessionToFile(map[string]any{
		"session_id": sessionID,
		"name":       "Attach Cursor Replay",
		"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
		"created_at": "2026-03-13T10:00:00Z",
		"updated_at": "2026-03-13T10:01:00Z",
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	server.markSessionRuntimeRunning(sessionID, "run-attach-cursor", executionModeBuild, "building", projectPath, "inst-a")
	for idx, content := range []string{"first", "second", "third"} {
		event, ok := buildRunEventMessageWithOptions(Message{
			Type:      MsgTypeStatus,
			SessionID: sessionID,
			Content:   content,
		}, runEventOptions{Sequence: uint64(idx + 1), TimestampMillis: int64(1000 * (idx + 1))})
		if !ok {
			t.Fatalf("expected run_event %d", idx+1)
		}
		server.recordSessionRunEvent(sessionID, event)
	}

	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	server.connWriteMu[serverConn] = &sync.Mutex{}

	handled := server.handleSessionMessage(serverConn, Message{
		Type: MsgTypeSessionAttach,
		Data: map[string]any{
			"request_id":    "req-attach-cursor",
			"session_id":    sessionID,
			"last_seen_seq": 1,
		},
	})
	if !handled {
		t.Fatal("expected session attach to be handled")
	}

	messages := readMessagesUntilTimeout(t, clientConn)
	replayedSeqs := make([]uint64, 0, 2)
	for _, message := range messages {
		if message.Type != MsgTypeRunEvent {
			continue
		}
		seq := parseUint64Like(message.Data["seq"])
		if seq == 0 {
			continue
		}
		replayedSeqs = append(replayedSeqs, seq)
	}
	if len(replayedSeqs) != 2 || replayedSeqs[0] != 2 || replayedSeqs[1] != 3 {
		t.Fatalf("expected replayed seqs [2 3], got %v", replayedSeqs)
	}
}

func TestSessionAttach_FallsBackWhenReplayGapDetected(t *testing.T) {
	server := NewDeepServer(0, nil)
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	server.connWriteMu[serverConn] = &sync.Mutex{}

	sessionID := "sid-gap-detected"
	for idx := 1; idx <= sessionRunEventBufferLimit+4; idx++ {
		event, ok := buildRunEventMessageWithOptions(Message{Type: MsgTypeStatus, SessionID: sessionID, Content: "step"}, runEventOptions{Sequence: uint64(idx), TimestampMillis: int64(idx)})
		if !ok {
			t.Fatalf("expected run_event %d", idx)
		}
		server.recordSessionRunEvent(sessionID, event)
	}

	handled := server.handleSessionMessage(serverConn, Message{
		Type: MsgTypeSessionAttach,
		Data: map[string]any{
			"request_id":    "req-gap-detected",
			"session_id":    sessionID,
			"last_seen_seq": 1,
		},
	})
	if !handled {
		t.Fatal("expected session attach to be handled")
	}

	messages := readMessagesUntilTimeout(t, clientConn)
	if len(messages) != 1 {
		t.Fatalf("expected replay gap attach to stop at response, got %+v", messages)
	}
	message := messages[0]
	if message.Type != MsgTypeSessionResponse {
		t.Fatalf("expected only session.response, got %+v", message)
	}
	if message.Data["code"] != "REPLAY_REQUIRED" {
		t.Fatalf("expected REPLAY_REQUIRED code, got %#v", message.Data["code"])
	}
}

func TestSessionAttach_IsIdempotentAcrossReconnect(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "attach-reconnect.qgz")
	sessionID := "sid-attach-reconnect"

	if err := server.saveSessionToFile(map[string]any{
		"session_id": sessionID,
		"name":       "Attach Reconnect",
		"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
		"created_at": "2026-03-13T10:00:00Z",
		"updated_at": "2026-03-13T10:01:00Z",
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	server.markSessionRuntimeRunning(sessionID, "run-attach-reconnect", executionModeBuild, "building", projectPath, "inst-a")
	for idx, content := range []string{"first", "second", "third"} {
		event, ok := buildRunEventMessageWithOptions(Message{
			Type:      MsgTypeStatus,
			SessionID: sessionID,
			Content:   content,
		}, runEventOptions{Sequence: uint64(idx + 1), TimestampMillis: int64(1000 * (idx + 1))})
		if !ok {
			t.Fatalf("expected run_event %d", idx+1)
		}
		server.recordSessionRunEvent(sessionID, event)
	}

	attachAndCollect := func(requestID string) []Message {
		serverConn, clientConn, cleanup := newTestWebSocketPair(t)
		defer cleanup()
		server.connWriteMu[serverConn] = &sync.Mutex{}

		handled := server.handleSessionMessage(serverConn, Message{
			Type: MsgTypeSessionAttach,
			Data: map[string]any{
				"request_id":    requestID,
				"session_id":    sessionID,
				"last_seen_seq": 2,
			},
		})
		if !handled {
			t.Fatal("expected session attach to be handled")
		}
		messages := readMessagesUntilTimeout(t, clientConn)
		server.detachAllSessionSubscriptions(serverConn)
		return messages
	}

	extractReplaySeqs := func(messages []Message) []uint64 {
		seqs := make([]uint64, 0, len(messages))
		for _, message := range messages {
			if message.Type != MsgTypeRunEvent {
				continue
			}
			seq := parseUint64Like(message.Data["seq"])
			if seq == 0 {
				continue
			}
			seqs = append(seqs, seq)
		}
		return seqs
	}

	firstReplay := extractReplaySeqs(attachAndCollect("req-attach-reconnect-1"))
	secondReplay := extractReplaySeqs(attachAndCollect("req-attach-reconnect-2"))

	if len(firstReplay) != 1 || firstReplay[0] != 3 {
		t.Fatalf("expected first reconnect replay seq [3], got %v", firstReplay)
	}
	if len(secondReplay) != 1 || secondReplay[0] != 3 {
		t.Fatalf("expected second reconnect replay seq [3], got %v", secondReplay)
	}
}

func TestSessionAttachDeliversSnapshotThenRuntimeThenReplayInOrder(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "attach-order.qgz")
	sessionID := "sid-attach-order"

	if err := server.saveSessionToFile(map[string]any{
		"session_id": sessionID,
		"name":       "Attach Ordering",
		"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
		"created_at": "2026-03-13T10:00:00Z",
		"updated_at": "2026-03-13T10:01:00Z",
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}
	server.markSessionRuntimeRunning(sessionID, "run-attach-order", executionModeBuild, "building", projectPath, "inst-order")
	for idx, content := range []string{"first", "second", "third"} {
		event, ok := buildRunEventMessageWithOptions(Message{Type: MsgTypeStatus, SessionID: sessionID, Content: content}, runEventOptions{Sequence: uint64(idx + 1), TimestampMillis: int64(1000 * (idx + 1))})
		if !ok {
			t.Fatalf("expected run_event %d", idx+1)
		}
		server.recordSessionRunEvent(sessionID, event)
	}

	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	server.connWriteMu[serverConn] = &sync.Mutex{}

	handled := server.handleSessionMessage(serverConn, Message{
		Type: MsgTypeSessionAttach,
		Data: map[string]any{
			"request_id":    "req-attach-order",
			"session_id":    sessionID,
			"last_seen_seq": 1,
		},
	})
	if !handled {
		t.Fatal("expected session attach to be handled")
	}

	messages := readMessagesUntilTimeout(t, clientConn)
	if len(messages) < 4 {
		t.Fatalf("expected snapshot/runtime/replay messages, got %+v", messages)
	}
	if messages[0].Type != MsgTypeRunEvent {
		t.Fatalf("expected snapshot run_event first, got %+v", messages[0])
	}
	if seq := parseUint64Like(messages[0].Data["seq"]); seq != 0 {
		t.Fatalf("expected snapshot run_event seq=0, got %d", seq)
	}
	if messages[1].Type != MsgTypeSessionRuntimeState {
		t.Fatalf("expected session_runtime_state second, got %+v", messages[1])
	}
	var replayedSeqs []uint64
	for _, message := range messages[2:] {
		if message.Type != MsgTypeRunEvent {
			continue
		}
		replayedSeqs = append(replayedSeqs, parseUint64Like(message.Data["seq"]))
	}
	if len(replayedSeqs) != 2 || replayedSeqs[0] != 2 || replayedSeqs[1] != 3 {
		t.Fatalf("expected replayed seqs [2 3] after snapshot/runtime frames, got %v", replayedSeqs)
	}
}

func TestReplayCursorRecovery(t *testing.T) {
	const protocolVersion = 1

	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "reconnect-v1.qgz")
	sessionID := "sid-reconnect-v1"

	if err := server.saveSessionToFile(map[string]any{
		"session_id": sessionID,
		"name":       "Reconnect Replay V1",
		"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
		"created_at": "2026-03-13T10:00:00Z",
		"updated_at": "2026-03-13T10:01:00Z",
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	server.markSessionRuntimeRunning(sessionID, "run-reconnect-v1", executionModeBuild, "building", projectPath, "inst-a")
	for idx, content := range []string{"first", "second", "third"} {
		event, ok := buildRunEventMessageWithOptions(Message{
			Type:      MsgTypeStatus,
			SessionID: sessionID,
			Content:   content,
		}, runEventOptions{Sequence: uint64(idx + 1), TimestampMillis: int64(1000 * (idx + 1))})
		if !ok {
			t.Fatalf("expected run_event %d", idx+1)
		}
		server.recordSessionRunEvent(sessionID, event)
	}

	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	server.connWriteMu[serverConn] = &sync.Mutex{}

	handled := server.handleSessionMessage(serverConn, Message{
		Type: MsgTypeSessionAttach,
		Data: map[string]any{
			"request_id":       "req-reconnect-v1",
			"session_id":       sessionID,
			"last_seen_seq":    1,
			"protocol_version": protocolVersion,
		},
	})
	if !handled {
		t.Fatal("expected session attach to be handled")
	}

	messages := readMessagesUntilTimeout(t, clientConn)
	replayedSeqs := make([]uint64, 0, 2)
	for _, message := range messages {
		if got := parseUint64Like(message.Data["protocol_version"]); got != protocolVersion {
			t.Fatalf("expected outbound protocol_version=%d, got %d for %+v", protocolVersion, got, message)
		}
		if message.Type != MsgTypeRunEvent {
			continue
		}
		seq := parseUint64Like(message.Data["seq"])
		if seq == 0 {
			continue
		}
		replayedSeqs = append(replayedSeqs, seq)
		if replayed, _ := message.Data["replayed"].(bool); !replayed {
			t.Fatalf("expected replayed run_event seq=%d to be marked replayed, got %#v", seq, message.Data["replayed"])
		}
		expectedKey := sessionID + ":" + map[uint64]string{2: "2", 3: "3"}[seq]
		if got := message.Data["dedupe_key"]; got != expectedKey {
			t.Fatalf("expected replayed run_event dedupe_key=%s, got %#v", expectedKey, got)
		}
	}
	if len(replayedSeqs) != 2 || replayedSeqs[0] != 2 || replayedSeqs[1] != 3 {
		t.Fatalf("expected replayed seqs [2 3], got %v", replayedSeqs)
	}
}

func TestRejectInvalidCursor(t *testing.T) {
	const protocolVersion = 1

	server := NewDeepServer(0, nil)
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	server.connWriteMu[serverConn] = &sync.Mutex{}

	handled := server.handleSessionMessage(serverConn, Message{
		Type: MsgTypeSessionAttach,
		Data: map[string]any{
			"request_id":       "req-invalid-cursor",
			"session_id":       "sid-invalid-cursor",
			"last_seen_seq":    "bogus",
			"protocol_version": protocolVersion,
		},
	})
	if !handled {
		t.Fatal("expected session attach to be handled")
	}

	messages := readMessagesUntilTimeout(t, clientConn)
	if len(messages) == 0 {
		t.Fatal("expected invalid cursor refusal response")
	}

	found := false
	for _, message := range messages {
		if message.Type == MsgTypeRunEvent {
			t.Fatalf("expected invalid cursor refusal to avoid replay delivery, got %+v", message)
		}
		if message.Type != MsgTypeSessionResponse {
			continue
		}
		if message.Data["action"] != "attach" {
			continue
		}
		found = true
		if got := parseUint64Like(message.Data["protocol_version"]); got != protocolVersion {
			t.Fatalf("expected response protocol_version=%d, got %d", protocolVersion, got)
		}
		if message.Data["success"] != false {
			t.Fatalf("expected attach response success=false, got %#v", message.Data["success"])
		}
		if message.Data["code"] != "INVALID_CURSOR" {
			t.Fatalf("expected code %s, got %#v", "INVALID_CURSOR", message.Data["code"])
		}
	}
	if !found {
		t.Fatalf("expected invalid cursor attach response, got %+v", messages)
	}
}

func TestSendSessionMessageOnlyBroadcastsToMatchingSessionSubscribers(t *testing.T) {
	server := NewDeepServer(0, nil)
	serverConnA, clientConnA, cleanupA := newTestWebSocketPair(t)
	defer cleanupA()
	serverConnB, clientConnB, cleanupB := newTestWebSocketPair(t)
	defer cleanupB()

	server.connWriteMu[serverConnA] = &sync.Mutex{}
	server.connWriteMu[serverConnB] = &sync.Mutex{}
	server.attachSessionSubscription(serverConnA, "sid-a")
	server.attachSessionSubscription(serverConnB, "sid-b")

	if err := server.sendSessionMessage(nil, Message{Type: MsgTypeStatus, SessionID: "sid-a", Content: "only-a"}); err != nil {
		t.Fatalf("sendSessionMessage failed: %v", err)
	}

	messagesA := readMessagesUntilTimeout(t, clientConnA)
	messagesB := readMessagesUntilTimeout(t, clientConnB)
	if len(messagesA) == 0 {
		t.Fatal("expected sid-a subscriber to receive messages")
	}
	if len(messagesB) != 0 {
		t.Fatalf("expected sid-b subscriber to receive no messages, got %d", len(messagesB))
	}
	for _, message := range messagesA {
		if message.SessionID != "sid-a" {
			t.Fatalf("unexpected message session_id on sid-a subscriber: %+v", message)
		}
	}
}

func TestSendSessionMessageFallsBackToOriginConnectionInsteadOfBroadcastingAllClients(t *testing.T) {
	server := NewDeepServer(0, nil)
	serverConnOrigin, clientConnOrigin, cleanupOrigin := newTestWebSocketPair(t)
	defer cleanupOrigin()
	serverConnOther, clientConnOther, cleanupOther := newTestWebSocketPair(t)
	defer cleanupOther()

	server.connWriteMu[serverConnOrigin] = &sync.Mutex{}
	server.connWriteMu[serverConnOther] = &sync.Mutex{}
	server.clients[serverConnOrigin] = true
	server.clients[serverConnOther] = true

	if err := server.sendSessionMessage(serverConnOrigin, Message{Type: MsgTypeStatus, SessionID: "sid-origin-only", Content: "origin-only"}); err != nil {
		t.Fatalf("sendSessionMessage failed: %v", err)
	}

	messagesOrigin := readMessagesUntilTimeout(t, clientConnOrigin)
	messagesOther := readMessagesUntilTimeout(t, clientConnOther)
	if len(messagesOrigin) == 0 {
		t.Fatal("expected origin connection to receive session-scoped message")
	}
	if len(messagesOther) != 0 {
		t.Fatalf("expected unrelated client to receive no messages, got %d", len(messagesOther))
	}
	for _, message := range messagesOrigin {
		if message.SessionID != "sid-origin-only" {
			t.Fatalf("unexpected message session_id on origin connection: %+v", message)
		}
	}
}
