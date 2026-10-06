package websocket

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
)

type stubToolCatalogAssetService struct {
	scope string
	view  control.ToolCatalogView
}

type toolCatalogWebSocketExpectation struct {
	requestID  string
	scope      string
	code       string
	message    string
	userAction string
	retryable  bool
}

func (s *stubToolCatalogAssetService) SettingsWorkspace() map[string]any { return nil }

func (s *stubToolCatalogAssetService) ListAgentConfigs(string) ([]config.StoredAgentConfig, error) {
	return nil, nil
}

func (s *stubToolCatalogAssetService) GetAgentConfig(string, string) (*config.StoredAgentConfig, error) {
	return nil, nil
}

func (s *stubToolCatalogAssetService) GetAgentProfile(string) (settings.AgentProfileView, error) {
	return settings.AgentProfileView{}, nil
}

func (s *stubToolCatalogAssetService) GetAgent(string, string) (config.AgentDefData, error) {
	return config.AgentDefData{}, nil
}

func (s *stubToolCatalogAssetService) GetToolCatalog(_ context.Context, scope string) (control.ToolCatalogView, error) {
	s.scope = scope
	return s.view, nil
}

func (s *stubToolCatalogAssetService) ListSkillPackages() ([]settings.SkillPackageSummary, error) {
	return nil, nil
}

func (s *stubToolCatalogAssetService) GetSkillPackage(string) (settings.SkillPackageDetail, error) {
	return settings.SkillPackageDetail{}, nil
}

func (s *stubToolCatalogAssetService) GetSkillPackageReferences(string) []config.SkillReference {
	return nil
}

func (s *stubToolCatalogAssetService) ListSkills() ([]map[string]any, error) { return nil, nil }

func (s *stubToolCatalogAssetService) GetSkillContent(string) (map[string]any, error) {
	return nil, nil
}

func (s *stubToolCatalogAssetService) GetSkillReferences(string) []config.SkillReference { return nil }

func runToolCatalogWebSocketMessage(t *testing.T, view control.ToolCatalogView, requestID, scope string) Message {
	t.Helper()
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	assetService := &stubToolCatalogAssetService{view: view}
	server := &DeepServer{}
	server.agentConfigManager = &config.AgentConfigManager{}
	server.SetControlServices(nil, assetService, nil)

	if !server.routeMessage(serverConn, Message{
		Type: MsgTypeToolCatalogGet,
		Data: map[string]any{
			"request_id": requestID,
			"data": map[string]any{
				"scope": scope,
			},
		},
	}) {
		t.Fatal("tool catalog message was not routed")
	}

	if got := assetService.scope; got != scope {
		t.Fatalf("expected scope to be forwarded, got %q", got)
	}

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket response failed: %v", err)
	}

	var resp Message
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("unmarshal websocket response failed: %v payload=%s", err, string(payload))
	}
	return resp
}

func assertToolCatalogWebSocketFailure(t *testing.T, resp Message, expected toolCatalogWebSocketExpectation) {
	t.Helper()
	if resp.Type != MsgTypeToolCatalogResp {
		t.Fatalf("unexpected response type: %q", resp.Type)
	}
	if got, _ := resp.Data["request_id"].(string); got != expected.requestID {
		t.Fatalf("unexpected request_id: %q payload=%#v", got, resp.Data)
	}
	if got, _ := resp.Data["success"].(bool); got {
		t.Fatalf("expected success=false, payload=%#v", resp.Data)
	}
	if got, _ := resp.Data["scope"].(string); got != expected.scope {
		t.Fatalf("unexpected scope: %q payload=%#v", got, resp.Data)
	}
	if _, ok := resp.Data["catalog"].(map[string]any); !ok {
		t.Fatalf("expected contract catalog payload, got %#v", resp.Data)
	}
	sharedError, ok := resp.Data["shared_error"].(map[string]any)
	if !ok {
		t.Fatalf("missing shared_error in websocket failure payload: %#v", resp.Data)
	}
	if got, _ := sharedError["code"].(string); got != expected.code {
		t.Fatalf("unexpected shared_error code: %q payload=%#v", got, resp.Data)
	}
	if got, _ := sharedError["message"].(string); got != expected.message {
		t.Fatalf("unexpected shared_error message: %q payload=%#v", got, resp.Data)
	}
	if got, _ := sharedError["user_action"].(string); got != expected.userAction {
		t.Fatalf("unexpected shared_error user_action: %q payload=%#v", got, resp.Data)
	}
	if got, _ := sharedError["retryable"].(bool); got != expected.retryable {
		t.Fatalf("unexpected shared_error retryable: %v payload=%#v", got, resp.Data)
	}
	errorEnvelope, ok := resp.Data["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing websocket error envelope: %#v", resp.Data)
	}
	if got, _ := errorEnvelope["message"].(string); got != expected.message {
		t.Fatalf("unexpected error message: %q payload=%#v", got, resp.Data)
	}
	if got, _ := errorEnvelope["user_action"].(string); got != expected.userAction {
		t.Fatalf("unexpected error user_action: %q payload=%#v", got, resp.Data)
	}
	if got, _ := errorEnvelope["retryable"].(bool); got != expected.retryable {
		t.Fatalf("unexpected error retryable: %v payload=%#v", got, resp.Data)
	}
	if got, _ := errorEnvelope["message"].(string); got != sharedError["message"] {
		t.Fatalf("error/shared_error message mismatch: error=%q shared=%q payload=%#v", got, sharedError["message"], resp.Data)
	}
	if got, _ := errorEnvelope["user_action"].(string); got != sharedError["user_action"] {
		t.Fatalf("error/shared_error user_action mismatch: error=%q shared=%q payload=%#v", got, sharedError["user_action"], resp.Data)
	}
	if got, _ := errorEnvelope["retryable"].(bool); got != sharedError["retryable"] {
		t.Fatalf("error/shared_error retryable mismatch: error=%v shared=%v payload=%#v", got, sharedError["retryable"], resp.Data)
	}
}

func TestHandleAgentConfigMessageToolCatalogReturnsHTTPContractPayload(t *testing.T) {
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	assetService := &stubToolCatalogAssetService{
		view: control.ToolCatalogView{
			Scope:           "effective",
			ConfigName:      "default",
			RuntimeRevision: "rev-123",
			Catalog: toolcatalog.Contract{
				ID:             "tool_catalog/effective",
				Version:        "1",
				Source:         "go_control_plane",
				CapabilityType: "tool_catalog",
				RuntimeTarget:  "mixed",
				Availability:   "available",
				Policy: &toolcatalog.Policy{
					Scope:      "effective",
					ConfigName: "default",
				},
				DegradationStatus: "none",
				Items: []toolcatalog.Capability{{
					ID:             "buffer",
					Source:         "cli",
					CapabilityType: "tool",
					RuntimeTarget:  "cli",
					Availability:   "available",
				}},
			},
			AvailableCount:  1,
			ConfiguredCount: 2,
			ToolCatalogStatus: map[string]any{
				"ready": true,
			},
			ToolInfos: []toolcatalog.Item{{Name: "buffer", Available: true}},
			Tools:     []string{"buffer", "flash_feature"},
		},
	}

	server := &DeepServer{}
	server.agentConfigManager = &config.AgentConfigManager{}
	server.SetControlServices(nil, assetService, nil)

	if !server.routeMessage(serverConn, Message{
		Type: MsgTypeToolCatalogGet,
		Data: map[string]any{
			"request_id": "req-1",
			"data": map[string]any{
				"scope": "effective",
			},
		},
	}) {
		t.Fatal("tool catalog message was not routed")
	}

	if got := assetService.scope; got != "effective" {
		t.Fatalf("expected scope to be forwarded, got %q", got)
	}

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket response failed: %v", err)
	}

	var resp Message
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("unmarshal websocket response failed: %v payload=%s", err, string(payload))
	}
	t.Logf("tool_catalog_get websocket body: %s", string(payload))
	if resp.Type != MsgTypeToolCatalogResp {
		t.Fatalf("unexpected response type: %q", resp.Type)
	}
	if got, _ := resp.Data["request_id"].(string); got != "req-1" {
		t.Fatalf("unexpected request_id: %q", got)
	}
	if got, _ := resp.Data["success"].(bool); !got {
		t.Fatalf("expected success=true, got %#v", resp.Data["success"])
	}
	if got, _ := resp.Data["error"].(string); got != "" {
		t.Fatalf("unexpected error: %q", got)
	}
	for _, key := range []string{"scope", "config_name", "runtime_revision", "catalog"} {
		if _, ok := resp.Data[key]; !ok {
			t.Fatalf("missing %q in websocket tool catalog response: %#v", key, resp.Data)
		}
	}
	for _, key := range []string{"tools", "tool_infos", "available_count", "configured_count", "tool_catalog_status"} {
		if _, ok := resp.Data[key]; ok {
			t.Fatalf("did not expect legacy %q in websocket tool catalog response: %#v", key, resp.Data)
		}
	}
	if got, _ := resp.Data["scope"].(string); got != "effective" {
		t.Fatalf("unexpected scope: %q", got)
	}
	if got, _ := resp.Data["config_name"].(string); got != "default" {
		t.Fatalf("unexpected config_name: %q", got)
	}
	if got, _ := resp.Data["runtime_revision"].(string); got != "rev-123" {
		t.Fatalf("unexpected runtime_revision: %q", got)
	}
	catalog, ok := resp.Data["catalog"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected catalog payload: %#v", resp.Data)
	}
	if got, _ := catalog["id"].(string); got != "tool_catalog/effective" {
		t.Fatalf("unexpected catalog id: %q payload=%#v", got, resp.Data)
	}
	if got, _ := catalog["availability"].(string); got != "available" {
		t.Fatalf("unexpected catalog availability: %q payload=%#v", got, resp.Data)
	}
	if _, ok := resp.Data["shared_error"]; ok {
		t.Fatalf("did not expect shared_error on non-degraded websocket payload: %#v", resp.Data)
	}
}

func TestHandleAgentConfigMessageToolCatalogProviderMissingCarriesSharedDictionary(t *testing.T) {
	resp := runToolCatalogWebSocketMessage(t, control.ToolCatalogView{
		Scope:           "global",
		ConfigName:      "default",
		RuntimeRevision: "rev-provider",
		Catalog: toolcatalog.Contract{
			ID:                      "tool_catalog/global",
			Version:                 "1",
			Source:                  "go_control_plane",
			CapabilityType:          "tool_catalog",
			RuntimeTarget:           "cli",
			Availability:            "degraded",
			MissingDependencyReason: "provider_missing",
			Policy: &toolcatalog.Policy{
				Scope:      "global",
				ConfigName: "default",
			},
			DegradationStatus: "provider_missing",
			Items: []toolcatalog.Capability{{
				ID:                      "output_init_dir",
				Source:                  "cli",
				CapabilityType:          "tool",
				RuntimeTarget:           "cli",
				Availability:            "unavailable",
				MissingDependencyReason: "provider_missing",
				DegradationStatus:       "provider_missing",
			}},
		},
	}, "req-provider", "global")
	if got, _ := resp.Data["success"].(bool); !got {
		t.Fatalf("expected success=true, got %#v payload=%#v", resp.Data["success"], resp.Data)
	}
	if _, ok := resp.Data["catalog"].(map[string]any); !ok {
		t.Fatalf("expected contract catalog payload, got %#v", resp.Data)
	}
	sharedError, ok := resp.Data["shared_error"].(map[string]any)
	if !ok {
		t.Fatalf("missing shared_error in degraded websocket payload: %#v", resp.Data)
	}
	if got, _ := sharedError["code"].(string); got != "provider_missing" {
		t.Fatalf("unexpected provider_missing shared_error code: %q payload=%#v", got, resp.Data)
	}
	if got, _ := sharedError["message"].(string); got != "Provider 未配置，当前能力已降级。" {
		t.Fatalf("unexpected provider_missing shared_error message: %q payload=%#v", got, resp.Data)
	}
	if got, _ := sharedError["user_action"].(string); got != "请先配置至少一个 Provider 后重试。" {
		t.Fatalf("unexpected provider_missing shared_error user_action: %q payload=%#v", got, resp.Data)
	}
	if got, _ := sharedError["retryable"].(bool); !got {
		t.Fatalf("expected provider_missing shared_error retryable=true payload=%#v", resp.Data)
	}
}

func TestHandleAgentConfigMessageToolCatalogUnsupportedRuntimeCarriesSharedDictionary(t *testing.T) {
	resp := runToolCatalogWebSocketMessage(t, control.ToolCatalogView{
		Scope:           "global",
		ConfigName:      "default",
		RuntimeRevision: "rev-unsupported",
		Catalog: toolcatalog.Contract{
			ID:                      "tool_catalog/global",
			Version:                 "1",
			Source:                  "go_control_plane",
			CapabilityType:          "tool_catalog",
			RuntimeTarget:           "mixed",
			Availability:            "unsupported",
			MissingDependencyReason: "unsupported_runtime",
			Policy: &toolcatalog.Policy{
				Scope:      "global",
				ConfigName: "default",
			},
			DegradationStatus: "unsupported_runtime",
			Items: []toolcatalog.Capability{{
				ID:                      "wasm_render_map",
				Source:                  "wasm-provider",
				CapabilityType:          "tool",
				RuntimeTarget:           "wasm",
				Availability:            "unsupported",
				MissingDependencyReason: "unsupported_runtime",
				DegradationStatus:       "unsupported_runtime",
			}},
		},
	}, "req-unsupported", "global")
	t.Logf("unsupported_runtime websocket body: %#v", resp.Data)
	assertToolCatalogWebSocketFailure(t, resp, toolCatalogWebSocketExpectation{
		requestID:  "req-unsupported",
		scope:      "global",
		code:       "unsupported_runtime",
		message:    "当前运行时不支持该能力。",
		userAction: "请切换到受支持的运行时或启动对应服务。",
		retryable:  false,
	})
}

func TestHandleAgentConfigMessageToolCatalogAbnormalEmptyCarriesSharedDictionary(t *testing.T) {
	resp := runToolCatalogWebSocketMessage(t, control.ToolCatalogView{
		Scope:           "global",
		ConfigName:      "default",
		RuntimeRevision: "rev-empty",
		Catalog: toolcatalog.Contract{
			ID:                      "tool_catalog/global",
			Version:                 "1",
			Source:                  "go_control_plane",
			CapabilityType:          "tool_catalog",
			RuntimeTarget:           "mixed",
			Availability:            "unavailable",
			MissingDependencyReason: "abnormal_empty",
			Policy: &toolcatalog.Policy{
				Scope:      "global",
				ConfigName: "default",
			},
			DegradationStatus: "abnormal_empty",
			Items:             []toolcatalog.Capability{},
		},
	}, "req-empty", "global")
	assertToolCatalogWebSocketFailure(t, resp, toolCatalogWebSocketExpectation{
		requestID:  "req-empty",
		scope:      "global",
		code:       "abnormal_empty",
		message:    "工具目录异常为空，当前能力已降级。",
		userAction: "请刷新工具目录或检查运行态注册是否完成。",
		retryable:  true,
	})
}
