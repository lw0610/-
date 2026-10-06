package websocket

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/gorilla/websocket"
)

func TestMessageRouterIncludesToolCatalogAliases(t *testing.T) {
	if _, ok := messageRouter[MsgTypeToolCatalogGet]; !ok {
		t.Fatalf("messageRouter missing %q handler", MsgTypeToolCatalogGet)
	}
}

func TestRouteMessageUnknownType(t *testing.T) {
	server := &DeepServer{}
	if server.routeMessage(nil, Message{Type: "unknown"}) {
		t.Fatal("unknown message type should not be routed")
	}
}

func TestRouteMessageKnownTypeWithoutManagerDoesNotPanic(t *testing.T) {
	server := &DeepServer{}
	if server.routeMessage(nil, Message{Type: MsgTypeToolCatalogGet, Data: map[string]any{}}) {
		t.Fatal("route should return false when handler preconditions are not met")
	}
}

func TestIdempotencyKeyEnforced(t *testing.T) {
	t.Run("task create requires explicit session id", func(t *testing.T) {
		server := NewDeepServer(0, nil)
		seedTestRunningInstance(server)
		serverConn, clientConn, cleanup := newTestWebSocketPair(t)
		defer cleanup()
		server.connWriteMu[serverConn] = &sync.Mutex{}

		payload, err := json.Marshal(map[string]any{
			"type":            MsgTypeTaskCreate,
			"idempotency_key": "create-explicit-session",
			"request_id":      "req-create-explicit-session",
			"content":         "请执行任务",
		})
		if err != nil {
			t.Fatalf("marshal payload failed: %v", err)
		}

		server.handleMessage(serverConn, payload)
		messages := readMessagesUntilTimeout(t, clientConn)
		if len(messages) == 0 {
			t.Fatal("expected websocket response for missing session id")
		}
		found := false
		for _, message := range messages {
			if message.Type != MsgTypeError {
				continue
			}
			if code, _ := message.Data["code"].(string); code == "SESSION_ID_REQUIRED" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected SESSION_ID_REQUIRED error, got %+v", messages)
		}
	})

	t.Run("task create requires idempotency key", func(t *testing.T) {
		server := NewDeepServer(0, nil)
		seedTestRunningInstance(server)
		serverConn, clientConn, cleanup := newTestWebSocketPair(t)
		defer cleanup()
		server.connWriteMu[serverConn] = &sync.Mutex{}

		payload, err := json.Marshal(map[string]any{
			"type":       "task",
			"session_id": "sid-create-idempotency",
			"content":    "请执行任务",
		})
		if err != nil {
			t.Fatalf("marshal payload failed: %v", err)
		}

		server.handleMessage(serverConn, payload)
		messages := readMessagesUntilTimeout(t, clientConn)
		if len(messages) == 0 {
			t.Fatal("expected websocket response for missing idempotency key")
		}
		found := false
		for _, message := range messages {
			if message.Type != MsgTypeError {
				continue
			}
			if code, _ := message.Data["code"].(string); code == "IDEMPOTENCY_KEY_REQUIRED" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected IDEMPOTENCY_KEY_REQUIRED error, got %+v", messages)
		}
	})

	t.Run("duplicate cancel request is acked as deduped", func(t *testing.T) {
		server := NewDeepServer(0, nil)
		serverConn, clientConn, cleanup := newTestWebSocketPair(t)
		defer cleanup()
		server.connWriteMu[serverConn] = &sync.Mutex{}

		firstAck, err := server.claimTaskRequest(serverConn, Message{
			Type:           MsgTypeTaskCancel,
			SessionID:      "sid-cancel-idempotency",
			IdempotencyKey: "cancel-1",
		})
		if err != nil {
			t.Fatalf("claim first cancel request failed: %v", err)
		}
		if firstAck.Deduped {
			t.Fatalf("expected first cancel request not deduped, got %+v", firstAck)
		}

		secondAck, err := server.claimTaskRequest(serverConn, Message{
			Type:           MsgTypeTaskCancel,
			SessionID:      "sid-cancel-idempotency",
			IdempotencyKey: "cancel-1",
		})
		if err != nil {
			t.Fatalf("claim second cancel request failed: %v", err)
		}
		if !secondAck.Deduped {
			t.Fatalf("expected second cancel request deduped, got %+v", secondAck)
		}
		server.sendTaskAck(serverConn, secondAck)
		messages := readMessagesUntilTimeout(t, clientConn)

		found := false
		for _, message := range messages {
			if message.Type != "task_ack" {
				continue
			}
			if deduped, _ := message.Data["deduped"].(bool); deduped {
				if operation, _ := message.Data["operation"].(string); operation == "cancel" {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("expected deduped cancel task_ack, got %+v", messages)
		}
	})

	t.Run("task retry requires idempotency key and emits ack", func(t *testing.T) {
		server := NewDeepServer(0, nil)
		serverConn, clientConn, cleanup := newTestWebSocketPair(t)
		defer cleanup()
		server.connWriteMu[serverConn] = &sync.Mutex{}

		missingKeyPayload, err := json.Marshal(map[string]any{
			"type":       "task_retry",
			"session_id": "sid-retry-idempotency",
			"content":    "请重试",
		})
		if err != nil {
			t.Fatalf("marshal retry payload failed: %v", err)
		}
		server.handleMessage(serverConn, missingKeyPayload)
		missingMessages := readMessagesUntilTimeout(t, clientConn)

		missingFound := false
		for _, message := range missingMessages {
			if message.Type != MsgTypeError {
				continue
			}
			if code, _ := message.Data["code"].(string); code == "IDEMPOTENCY_KEY_REQUIRED" {
				missingFound = true
			}
		}
		if !missingFound {
			t.Fatalf("expected retry to reject missing idempotency key, got %+v", missingMessages)
		}

		ack, err := server.claimTaskRequest(serverConn, Message{
			Type:           MsgTypeTaskRetry,
			SessionID:      "sid-retry-idempotency",
			IdempotencyKey: "retry-1",
		})
		if err != nil {
			t.Fatalf("claim retry request failed: %v", err)
		}
		if ack.Operation != "retry" || ack.Deduped {
			t.Fatalf("unexpected retry ack: %+v", ack)
		}

		ackServerConn, ackClientConn, ackCleanup := newTestWebSocketPair(t)
		defer ackCleanup()
		server.connWriteMu[ackServerConn] = &sync.Mutex{}
		server.sendTaskAck(ackServerConn, ack)
		messages := readMessagesUntilTimeout(t, ackClientConn)

		found := false
		for _, message := range messages {
			if message.Type != "task_ack" {
				continue
			}
			if operation, _ := message.Data["operation"].(string); operation != "retry" {
				continue
			}
			if accepted, _ := message.Data["accepted"].(bool); accepted {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected retry task_ack after providing idempotency key, got %+v", messages)
		}
	})

	t.Run("request id participates in task request dedupe identity", func(t *testing.T) {
		server := NewDeepServer(0, nil)
		conn := &websocket.Conn{}

		firstAck, err := server.claimTaskRequest(conn, Message{
			Type:           MsgTypeTaskCreate,
			SessionID:      "sid-request-id-dedupe",
			RequestID:      "req-1",
			IdempotencyKey: "dup-key",
		})
		if err != nil {
			t.Fatalf("claim first request failed: %v", err)
		}
		if firstAck.Deduped {
			t.Fatalf("expected first request not deduped, got %+v", firstAck)
		}

		secondAck, err := server.claimTaskRequest(conn, Message{
			Type:           MsgTypeTaskCreate,
			SessionID:      "sid-request-id-dedupe",
			RequestID:      "req-2",
			IdempotencyKey: "dup-key",
		})
		if err != nil {
			t.Fatalf("claim second request failed: %v", err)
		}
		if secondAck.Deduped {
			t.Fatalf("expected distinct request_id not to dedupe, got %+v", secondAck)
		}
	})

	t.Run("task mutate requires current session owner connection", func(t *testing.T) {
		server := NewDeepServer(0, nil)
		seedTestRunningInstance(server)
		ownerServerConn, ownerClientConn, ownerCleanup := newTestWebSocketPair(t)
		defer ownerCleanup()
		observerServerConn, observerClientConn, observerCleanup := newTestWebSocketPair(t)
		defer observerCleanup()
		server.connWriteMu[ownerServerConn] = &sync.Mutex{}
		server.connWriteMu[observerServerConn] = &sync.Mutex{}

		server.attachSessionSubscription(ownerServerConn, "sid-owner-gate")
		server.attachSessionSubscription(observerServerConn, "sid-owner-gate")

		payload, err := json.Marshal(map[string]any{
			"type":            MsgTypeTaskCreate,
			"session_id":      "sid-owner-gate",
			"idempotency_key": "owner-gate-1",
			"request_id":      "req-owner-gate-1",
			"content":         "请执行任务",
		})
		if err != nil {
			t.Fatalf("marshal payload failed: %v", err)
		}

		server.handleMessage(observerServerConn, payload)
		observerMessages := readMessagesUntilTimeout(t, observerClientConn)
		if len(observerMessages) == 0 {
			t.Fatal("expected non-owner task mutate to be refused")
		}
		foundOwnerError := false
		for _, message := range observerMessages {
			if message.Type == MsgTypeTaskAck {
				t.Fatalf("expected owner gate to reject before ack, got %+v", observerMessages)
			}
			if message.Type != MsgTypeError {
				continue
			}
			if code, _ := message.Data["code"].(string); code == "SESSION_OWNER_REQUIRED" {
				foundOwnerError = true
			}
		}
		if !foundOwnerError {
			t.Fatalf("expected SESSION_OWNER_REQUIRED error, got %+v", observerMessages)
		}

		server.handleMessage(ownerServerConn, payload)
		ownerMessages := readMessagesUntilTimeout(t, ownerClientConn)
		foundAck := false
		for _, message := range ownerMessages {
			if message.Type == MsgTypeTaskAck {
				foundAck = true
			}
		}
		if !foundAck {
			t.Fatalf("expected owner connection to receive task_ack, got %+v", ownerMessages)
		}
	})
}

func TestModelQuickSwitchReturnsMutationPayload(t *testing.T) {
	server := NewDeepServer(0, nil)
	tmpDir := t.TempDir()
	defaultPath := filepath.Join(tmpDir, "config.yaml")
	userPath := filepath.Join(tmpDir, "user.yaml")
	if err := os.WriteFile(defaultPath, []byte("llm:\n  active: provider-a\n  providers:\n    - name: provider-a\n      provider: openai\n      model: gpt-4o-mini\n    - name: provider-b\n      provider: openai\n      model: gpt-4.1-mini\n"), 0o644); err != nil {
		t.Fatalf("write default config failed: %v", err)
	}
	server.configService = config.NewConfigService(nil, &config.Config{
		LLM: config.LLMConfig{
			Active: "provider-a",
			Providers: []config.LLMProviderConfig{
				{Name: "provider-a", Provider: "openai", Model: "gpt-4o-mini"},
				{Name: "provider-b", Provider: "openai", Model: "gpt-4.1-mini"},
			},
		},
	}, defaultPath, userPath)
	server.SetControlServices(control.NewSettingsMutationService(control.SettingsMutationServiceDeps{
		ConfigService: server.configService,
		ActiveRuns:    server,
	}), nil, nil)

	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	server.connWriteMu[serverConn] = &sync.Mutex{}

	payload, err := json.Marshal(map[string]any{
		"type": MsgTypeModelQuickSwitch,
		"data": map[string]any{
			"request_id":       "req-quick-switch-success",
			"provider_name":    "provider-b",
			"protocol_version": 1,
		},
	})
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}

	server.handleMessage(serverConn, payload)
	messages := readMessagesUntilTimeout(t, clientConn)
	if len(messages) == 0 {
		t.Fatal("expected quick switch response messages")
	}

	foundSessionState := false
	foundMutation := false
	for _, message := range messages {
		switch message.Type {
		case MsgTypeSessionState:
			foundSessionState = true
			if got, _ := message.Data["active_model"].(string); got != "provider-b" {
				t.Fatalf("expected session_state active_model=provider-b, got %#v", got)
			}
		case MsgTypeLLMConfigResponse:
			if message.Data["success"] != true {
				t.Fatalf("expected quick switch success response, got %#v", message.Data)
			}
			mutation, ok := message.Data["mutation"].(map[string]any)
			if !ok {
				t.Fatalf("expected mutation payload, got %#v", message.Data["mutation"])
			}
			foundMutation = true
			if got, _ := mutation["config_revision"].(string); got == "" {
				t.Fatalf("expected mutation config_revision, got %#v", mutation)
			}
		}
	}
	if !foundSessionState {
		t.Fatalf("expected session_state message, got %+v", messages)
	}
	if !foundMutation {
		t.Fatalf("expected llm_config_response mutation payload, got %+v", messages)
	}
}

func TestWSVersionNegotiation(t *testing.T) {
	const protocolVersion = 1

	t.Run("session attach rejects unsupported protocol version", func(t *testing.T) {
		server := NewDeepServer(0, nil)
		serverConn, clientConn, cleanup := newTestWebSocketPair(t)
		defer cleanup()
		server.connWriteMu[serverConn] = &sync.Mutex{}

		handled := server.handleSessionMessage(serverConn, Message{
			Type: MsgTypeSessionAttach,
			Data: map[string]any{
				"request_id":       "req-version-attach",
				"session_id":       "sid-version-attach",
				"protocol_version": protocolVersion + 1,
			},
		})
		if !handled {
			t.Fatal("expected session attach to be handled")
		}

		messages := readMessagesUntilTimeout(t, clientConn)
		if len(messages) == 0 {
			t.Fatal("expected protocol version refusal response")
		}

		found := false
		for _, message := range messages {
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
				t.Fatalf("expected attach refusal success=false, got %#v", message.Data["success"])
			}
			if message.Data["code"] != "UNSUPPORTED_PROTOCOL_VERSION" {
				t.Fatalf("expected code %s, got %#v", "UNSUPPORTED_PROTOCOL_VERSION", message.Data["code"])
			}
		}
		if !found {
			t.Fatalf("expected attach refusal response, got %+v", messages)
		}
	})

	t.Run("task cancel rejects unsupported protocol version", func(t *testing.T) {
		server := NewDeepServer(0, nil)
		serverConn, clientConn, cleanup := newTestWebSocketPair(t)
		defer cleanup()
		server.connWriteMu[serverConn] = &sync.Mutex{}

		payload, err := json.Marshal(map[string]any{
			"type":            MsgTypeTaskCancel,
			"session_id":      "sid-version-cancel",
			"idempotency_key": "cancel-version-1",
			"data": map[string]any{
				"protocol_version": protocolVersion + 1,
			},
		})
		if err != nil {
			t.Fatalf("marshal payload failed: %v", err)
		}

		server.handleMessage(serverConn, payload)
		messages := readMessagesUntilTimeout(t, clientConn)
		if len(messages) == 0 {
			t.Fatal("expected websocket refusal for unsupported protocol version")
		}

		foundError := false
		for _, message := range messages {
			if message.Type == MsgTypeTaskAck || message.Type == MsgTypeCancelled {
				t.Fatalf("expected refusal before task handling, got %+v", message)
			}
			if message.Type != MsgTypeError {
				continue
			}
			foundError = true
			if got := parseUint64Like(message.Data["protocol_version"]); got != protocolVersion {
				t.Fatalf("expected error protocol_version=%d, got %d", protocolVersion, got)
			}
			if code, _ := message.Data["code"].(string); code != "UNSUPPORTED_PROTOCOL_VERSION" {
				t.Fatalf("expected code %s, got %#v", "UNSUPPORTED_PROTOCOL_VERSION", message.Data["code"])
			}
		}
		if !foundError {
			t.Fatalf("expected unsupported version error, got %+v", messages)
		}
	})

	t.Run("task create rejects unsupported protocol version before ack", func(t *testing.T) {
		server := NewDeepServer(0, nil)
		serverConn, clientConn, cleanup := newTestWebSocketPair(t)
		defer cleanup()
		server.connWriteMu[serverConn] = &sync.Mutex{}

		payload, err := json.Marshal(map[string]any{
			"type":            MsgTypeTaskCreate,
			"session_id":      "sid-version-create",
			"idempotency_key": "create-version-1",
			"request_id":      "req-version-create",
			"content":         "请执行任务",
			"data": map[string]any{
				"protocol_version": protocolVersion + 1,
			},
		})
		if err != nil {
			t.Fatalf("marshal payload failed: %v", err)
		}

		server.handleMessage(serverConn, payload)
		messages := readMessagesUntilTimeout(t, clientConn)
		if len(messages) == 0 {
			t.Fatal("expected websocket refusal for unsupported protocol version")
		}

		foundError := false
		for _, message := range messages {
			if message.Type == MsgTypeTaskAck {
				t.Fatalf("expected refusal before task_ack, got %+v", message)
			}
			if message.Type != MsgTypeError {
				continue
			}
			foundError = true
			if got := parseUint64Like(message.Data["protocol_version"]); got != protocolVersion {
				t.Fatalf("expected error protocol_version=%d, got %d", protocolVersion, got)
			}
			if code, _ := message.Data["code"].(string); code != ErrorCodeUnsupportedProtocolVersion {
				t.Fatalf("expected code %s, got %#v", ErrorCodeUnsupportedProtocolVersion, message.Data["code"])
			}
		}
		if !foundError {
			t.Fatalf("expected unsupported version error, got %+v", messages)
		}
	})

	t.Run("model quick switch rejects unsupported protocol version", func(t *testing.T) {
		server := NewDeepServer(0, nil)
		serverConn, clientConn, cleanup := newTestWebSocketPair(t)
		defer cleanup()
		server.connWriteMu[serverConn] = &sync.Mutex{}

		payload, err := json.Marshal(map[string]any{
			"type": MsgTypeModelQuickSwitch,
			"data": map[string]any{
				"request_id":       "req-version-quick-switch",
				"provider_name":    "provider-b",
				"protocol_version": protocolVersion + 1,
			},
		})
		if err != nil {
			t.Fatalf("marshal payload failed: %v", err)
		}

		server.handleMessage(serverConn, payload)
		messages := readMessagesUntilTimeout(t, clientConn)
		if len(messages) == 0 {
			t.Fatal("expected websocket refusal for unsupported protocol version")
		}

		foundResponse := false
		for _, message := range messages {
			if message.Type != MsgTypeLLMConfigResponse {
				continue
			}
			foundResponse = true
			if got := parseUint64Like(message.Data["protocol_version"]); got != protocolVersion {
				t.Fatalf("expected response protocol_version=%d, got %d", protocolVersion, got)
			}
			if message.Data["success"] != false {
				t.Fatalf("expected quick switch refusal success=false, got %#v", message.Data["success"])
			}
			if code, _ := message.Data["code"].(string); code != ErrorCodeUnsupportedProtocolVersion {
				t.Fatalf("expected code %s, got %#v", ErrorCodeUnsupportedProtocolVersion, message.Data["code"])
			}
		}
		if !foundResponse {
			t.Fatalf("expected llm config refusal response, got %+v", messages)
		}
	})
}
