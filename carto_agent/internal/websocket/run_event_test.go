package websocket

import "testing"

func TestBuildRunEventMessage(t *testing.T) {
	runEvent, ok := buildRunEventMessage(Message{
		Type:      MsgTypeTaskListUpdate,
		SessionID: "sid-1",
		Data: map[string]interface{}{
			"todos": []any{map[string]any{"content": "task"}},
		},
	})
	if !ok {
		t.Fatal("expected task list update to be mirrored as run_event")
	}
	if runEvent.Type != MsgTypeRunEvent {
		t.Fatalf("expected type %q, got %q", MsgTypeRunEvent, runEvent.Type)
	}
	if runEvent.SessionID != "sid-1" {
		t.Fatalf("expected session id to be preserved, got %q", runEvent.SessionID)
	}
	if event := runEvent.Data["event"]; event != "todo_update" {
		t.Fatalf("expected event todo_update, got %#v", event)
	}
	payload, _ := runEvent.Data["payload"].(map[string]any)
	if payload == nil {
		t.Fatal("expected payload map")
	}
	if payload["session_id"] != "sid-1" {
		t.Fatalf("expected payload session id, got %#v", payload["session_id"])
	}
}

func TestBuildRunEventMessagePreservesStructuredErrorMetadata(t *testing.T) {
	runEvent, ok := buildRunEventMessage(Message{
		Type:      MsgTypeError,
		SessionID: "sid-error",
		Content:   "QGIS 工具当前未就绪",
		Data: map[string]any{
			"code":       "TOOL_NOT_READY",
			"error_code": "TOOL_NOT_READY",
			"retryable":  true,
			"provider_diagnostics": map[string]any{
				"provider_id": "qgis_bridge",
			},
		},
	})
	if !ok {
		t.Fatal("expected error message to be mirrored as run_event")
	}
	payload, _ := runEvent.Data["payload"].(map[string]any)
	if payload == nil {
		t.Fatal("expected payload map")
	}
	if payload["error_code"] != "TOOL_NOT_READY" {
		t.Fatalf("expected error_code in run_event payload, got %#v", payload["error_code"])
	}
	diagnostics, _ := payload["provider_diagnostics"].(map[string]any)
	if diagnostics == nil || diagnostics["provider_id"] != "qgis_bridge" {
		t.Fatalf("expected provider diagnostics in payload, got %#v", payload["provider_diagnostics"])
	}
}

func TestBroadcastTaskListUpdateRecordsRunEvent(t *testing.T) {
	server := NewDeepServer(0, nil)
	err := server.BroadcastTaskListUpdate([]any{map[string]any{"content": "task-1", "status": "pending"}}, "sid-todo")
	if err != nil {
		t.Fatalf("BroadcastTaskListUpdate returned error: %v", err)
	}
	replay := server.sessionRunEventBuffers["sid-todo"]
	if len(replay) != 1 {
		t.Fatalf("expected exactly one buffered run_event, got %d", len(replay))
	}
	if replay[0].Type != MsgTypeRunEvent {
		t.Fatalf("expected recorded message type %q, got %q", MsgTypeRunEvent, replay[0].Type)
	}
	if got := replay[0].Data["event"]; got != "todo_update" {
		t.Fatalf("expected todo_update event, got %#v", got)
	}
	payload, _ := replay[0].Data["payload"].(map[string]any)
	if payload == nil {
		t.Fatal("expected payload map in replayed run_event")
	}
	todos, ok := payload["todos"].([]any)
	if !ok || len(todos) != 1 {
		t.Fatalf("expected todos payload, got %#v", payload["todos"])
	}
}

func TestTaskEventDedupe(t *testing.T) {
	server := NewDeepServer(0, nil)

	event, ok := buildRunEventMessageWithOptions(Message{
		Type:      MsgTypeTaskListUpdate,
		SessionID: "sid-dedupe",
		Data: map[string]any{
			"todos": []any{map[string]any{"content": "task-1", "status": "pending"}},
		},
	}, runEventOptions{Sequence: 7, TimestampMillis: 1700000000000})
	if !ok {
		t.Fatal("expected run_event to be built")
	}

	server.recordSessionRunEvent("sid-dedupe", event)
	server.recordSessionRunEvent("sid-dedupe", event)

	recorded := server.sessionRunEventBuffers["sid-dedupe"]
	if len(recorded) != 1 {
		t.Fatalf("expected duplicate event to be buffered once, got %d", len(recorded))
	}
	if got := recorded[0].Data["event_id"]; got != "sid-dedupe:7" {
		t.Fatalf("expected event_id sid-dedupe:7, got %#v", got)
	}
	if got := recorded[0].Data["dedupe_key"]; got != "sid-dedupe:7" {
		t.Fatalf("expected dedupe_key sid-dedupe:7, got %#v", got)
	}
}

func TestEventOrderingGuarantee(t *testing.T) {
	const protocolVersion = 1

	server := NewDeepServer(0, nil)
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	if err := server.sendSessionMessage(serverConn, Message{Type: MsgTypeStatus, SessionID: "sid-order", Content: "first"}); err != nil {
		t.Fatalf("sendSessionMessage first failed: %v", err)
	}
	if err := server.sendSessionMessage(serverConn, Message{Type: MsgTypeResponse, SessionID: "sid-order", Content: "second"}); err != nil {
		t.Fatalf("sendSessionMessage second failed: %v", err)
	}

	messages := readMessagesUntilTimeout(t, clientConn)
	if len(messages) != 4 {
		t.Fatalf("expected 4 websocket frames, got %d", len(messages))
	}

	wantTypes := []string{MsgTypeStatus, MsgTypeRunEvent, MsgTypeResponse, MsgTypeRunEvent}
	for idx, want := range wantTypes {
		if messages[idx].Type != want {
			t.Fatalf("expected frame %d type %s, got %s", idx, want, messages[idx].Type)
		}
		if got := parseUint64Like(messages[idx].Data["protocol_version"]); got != protocolVersion {
			t.Fatalf("expected frame %d protocol_version=%d, got %d", idx, protocolVersion, got)
		}
	}

	firstSeq := parseUint64Like(messages[1].Data["seq"])
	secondSeq := parseUint64Like(messages[3].Data["seq"])
	if firstSeq != 1 || secondSeq != 2 {
		t.Fatalf("expected ordered run_event seqs [1 2], got [%d %d]", firstSeq, secondSeq)
	}
	if got := messages[1].Data["event_id"]; got != "sid-order:1" {
		t.Fatalf("expected first run_event event_id sid-order:1, got %#v", got)
	}
	if got := messages[3].Data["event_id"]; got != "sid-order:2" {
		t.Fatalf("expected second run_event event_id sid-order:2, got %#v", got)
	}
}
