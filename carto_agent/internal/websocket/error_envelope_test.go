package websocket

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
)

func TestHandleConfigMessageReturnsStructuredEnvelopeWhenConfigServiceUnavailable(t *testing.T) {
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	server := &DeepServer{}
	if !server.routeMessage(serverConn, Message{
		Type: MsgTypeConfigGetAll,
		Data: map[string]any{"request_id": "cfg-1"},
	}) {
		t.Fatal("config message was not routed")
	}

	resp := readSingleMessage(t, clientConn)
	assertStructuredEnvelopeResponse(t, resp, MsgTypeConfigResponse, "cfg-1", "CONFIG_SERVICE_UNAVAILABLE", "config")
	if got := envelopeField(t, resp, "stage"); got != "settings_mutation" {
		t.Fatalf("unexpected envelope stage: %q", got)
	}
}

func TestHandleSkillMessageReturnsStructuredEnvelopeWhenSkillCatalogUnavailable(t *testing.T) {
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	server := &DeepServer{}
	if !server.routeMessage(serverConn, Message{
		Type: MsgTypeSkillList,
		Data: map[string]any{"request_id": "skill-1"},
	}) {
		t.Fatal("skill message was not routed")
	}

	resp := readSingleMessage(t, clientConn)
	assertStructuredEnvelopeResponse(t, resp, MsgTypeSkillResponse, "skill-1", "SKILL_CATALOG_UNAVAILABLE", "catalog")
	if got := envelopeField(t, resp, "stage"); got != "asset_catalog" {
		t.Fatalf("unexpected envelope stage: %q", got)
	}
}

func TestHandleSessionMessageReturnsStructuredEnvelopeForInvalidSessionID(t *testing.T) {
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	server := &DeepServer{}
	if !server.routeMessage(serverConn, Message{
		Type: MsgTypeSessionDirInfo,
		Data: map[string]any{
			"request_id":   "session-1",
			"session_id":   "../bad-session",
			"project_path": "/tmp/project",
		},
	}) {
		t.Fatal("session message was not routed")
	}

	resp := readSingleMessage(t, clientConn)
	assertStructuredEnvelopeResponse(t, resp, MsgTypeSessionResponse, "session-1", "INVALID_SESSION_REQUEST", "websocket")
	if got := envelopeField(t, resp, "stage"); got != "session_dir_info" {
		t.Fatalf("unexpected envelope stage: %q", got)
	}
	if got := envelopeField(t, resp, "user_action"); got == "" {
		t.Fatal("expected user_action to be present for invalid session request")
	}
	if got := envelopeDebugContextField(t, resp, "action"); got != "dir_info" {
		t.Fatalf("unexpected debug_context.action: %q", got)
	}
}

func TestHandleAgentConfigMessageToolCatalogReturnsStructuredEnvelopeOnFailure(t *testing.T) {
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	server := &DeepServer{}
	server.agentConfigManager = &config.AgentConfigManager{}

	if !server.routeMessage(serverConn, Message{
		Type: MsgTypeToolCatalogGet,
		Data: map[string]any{
			"request_id": "catalog-1",
			"data":       map[string]any{"scope": "effective"},
		},
	}) {
		t.Fatal("tool catalog message was not routed")
	}

	resp := readSingleMessage(t, clientConn)
	assertStructuredEnvelopeResponse(t, resp, MsgTypeToolCatalogResp, "catalog-1", "AGENT_CONFIG_UNAVAILABLE", "config")
	if got := envelopeField(t, resp, "stage"); got != "asset_catalog" {
		t.Fatalf("unexpected envelope stage: %q", got)
	}
}

func readSingleMessage(t *testing.T, conn interface {
	ReadMessage() (int, []byte, error)
	SetReadDeadline(time.Time) error
}) Message {
	t.Helper()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket response failed: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("unmarshal websocket response failed: %v payload=%s", err, string(payload))
	}
	return msg
}

func assertStructuredEnvelopeResponse(t *testing.T, msg Message, expectedType, expectedRequestID, expectedCode, expectedOrigin string) {
	t.Helper()

	if msg.Type != expectedType {
		t.Fatalf("unexpected response type: %q", msg.Type)
	}
	if got, _ := msg.Data["request_id"].(string); got != expectedRequestID {
		t.Fatalf("unexpected request_id: %q", got)
	}
	if got, _ := msg.Data["success"].(bool); got {
		t.Fatalf("expected success=false, got %#v", msg.Data["success"])
	}
	if got, _ := msg.Data["code"].(string); got != expectedCode {
		t.Fatalf("unexpected top-level code: %q", got)
	}

	errorPayload, ok := msg.Data["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured error envelope, got %#v", msg.Data["error"])
	}
	if got, _ := errorPayload["code"].(string); got != expectedCode {
		t.Fatalf("unexpected envelope code: %q", got)
	}
	if got, _ := errorPayload["origin"].(string); got != expectedOrigin {
		t.Fatalf("unexpected envelope origin: %q", got)
	}
	if got, _ := errorPayload["message"].(string); got == "" {
		t.Fatal("expected envelope message to be present")
	}
	if got, _ := errorPayload["correlation_id"].(string); got == "" {
		t.Fatal("expected correlation_id to be present")
	}
	if got, _ := errorPayload["evidence_id"].(string); got == "" {
		t.Fatal("expected evidence_id to be present")
	}
	debugContext, ok := errorPayload["debug_context"].(map[string]any)
	if !ok {
		t.Fatalf("expected debug_context object, got %#v", errorPayload["debug_context"])
	}
	if len(debugContext) == 0 {
		t.Fatal("expected debug_context to contain fields")
	}
}

func envelopeField(t *testing.T, msg Message, field string) string {
	t.Helper()
	errorPayload, ok := msg.Data["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured error envelope, got %#v", msg.Data["error"])
	}
	value, _ := errorPayload[field].(string)
	return value
}

func envelopeDebugContextField(t *testing.T, msg Message, field string) string {
	t.Helper()
	errorPayload, ok := msg.Data["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured error envelope, got %#v", msg.Data["error"])
	}
	debugContext, ok := errorPayload["debug_context"].(map[string]any)
	if !ok {
		t.Fatalf("expected debug_context object, got %#v", errorPayload["debug_context"])
	}
	value, _ := debugContext[field].(string)
	return value
}
