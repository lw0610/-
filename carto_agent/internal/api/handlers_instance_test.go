package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/discovery"
	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/settings"
)

type stubRuntimeCoordinator struct {
	snapshot middlewareclient.RuntimeSnapshot
}

func (s *stubRuntimeCoordinator) Snapshot(ctx context.Context) middlewareclient.RuntimeSnapshot {
	_ = ctx
	return s.snapshot
}

func (s *stubRuntimeCoordinator) Services(ctx context.Context) control.RuntimeServicesView {
	_ = ctx
	return control.RuntimeServicesView{}
}

func buildInstanceTestRouter(t *testing.T) *mux.Router {
	t.Helper()

	registry := discovery.NewInstanceRegistry()
	handler := NewAPI(nil, nil, nil, nil, "", registry)
	router := mux.NewRouter()
	handler.SetupRoutes(router)
	return router
}
func buildInstanceTestRouterWithLocalBridge(t *testing.T, registry *discovery.InstanceRegistry, provider *bridgeprovider.Provider) *mux.Router {
	t.Helper()

	handler := NewAPI(nil, nil, nil, nil, "", registry)
	handler.SetLocalBridgeProvider(provider)
	router := mux.NewRouter()
	handler.SetupRoutes(router)
	return router
}

func buildInstanceTestRouterWithLocalBridgeAndAssetCatalog(t *testing.T, registry *discovery.InstanceRegistry, provider *bridgeprovider.Provider, assetCatalog control.AssetCatalogService) *mux.Router {
	t.Helper()

	handler := NewAPIWithServices(nil, nil, registry, nil, nil, nil, assetCatalog, nil)
	handler.SetLocalBridgeProvider(provider)
	router := mux.NewRouter()
	handler.SetupRoutes(router)
	return router
}

type stubInstanceAssetCatalogService struct {
	visibility string
	scopes     []string
	known      bool
	err        error
}

func (s stubInstanceAssetCatalogService) SettingsWorkspace() map[string]any { return nil }
func (s stubInstanceAssetCatalogService) ListAgentConfigs(string) ([]config.StoredAgentConfig, error) {
	return nil, nil
}
func (s stubInstanceAssetCatalogService) GetAgentConfig(string, string) (*config.StoredAgentConfig, error) {
	return nil, nil
}
func (s stubInstanceAssetCatalogService) GetAgentProfile(string) (settings.AgentProfileView, error) {
	return settings.AgentProfileView{}, nil
}
func (s stubInstanceAssetCatalogService) GetAgent(string, string) (config.AgentDefData, error) {
	return config.AgentDefData{}, nil
}
func (s stubInstanceAssetCatalogService) GetToolCatalog(context.Context, string) (control.ToolCatalogView, error) {
	return control.ToolCatalogView{}, nil
}
func (s stubInstanceAssetCatalogService) ListSkillPackages() ([]settings.SkillPackageSummary, error) {
	return nil, nil
}
func (s stubInstanceAssetCatalogService) GetSkillPackage(string) (settings.SkillPackageDetail, error) {
	return settings.SkillPackageDetail{}, nil
}
func (s stubInstanceAssetCatalogService) GetSkillPackageReferences(string) []config.SkillReference {
	return nil
}
func (s stubInstanceAssetCatalogService) ListSkills() ([]map[string]any, error) { return nil, nil }
func (s stubInstanceAssetCatalogService) GetSkillContent(string) (map[string]any, error) {
	return nil, nil
}
func (s stubInstanceAssetCatalogService) GetSkillReferences(string) []config.SkillReference {
	return nil
}
func (s stubInstanceAssetCatalogService) ResolveToolAccessPolicy(context.Context, string) (string, []string, bool, error) {
	return s.visibility, append([]string(nil), s.scopes...), s.known, s.err
}

func decodeAPIResponse(t *testing.T, rr *httptest.ResponseRecorder) Response {
	t.Helper()

	var resp Response
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	return resp
}

type instanceActionWSMessage struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

func setupInstanceActionBridge(t *testing.T, instanceID string, tools []map[string]any) (*discovery.InstanceRegistry, *bridgeprovider.Provider, *websocket.Conn, func()) {
	t.Helper()
	t.Setenv("AUTOCARTO_HOME", t.TempDir())

	registry := discovery.NewInstanceRegistry()
	provider := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, registry)
	provider.SetDefaultInstanceSource(discovery.SourceAgentReverseWS)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", provider.HandleWebSocket)
	httpServer := httptest.NewServer(mux)
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		httpServer.Close()
		provider.Close()
		registry.Close()
		t.Fatalf("dial bridge websocket failed: %v", err)
	}

	sendInstanceActionWSMessage(t, conn, instanceActionWSMessage{
		Type: "instance_connect",
		Data: map[string]any{
			"instance_id":  instanceID,
			"pid":          1001,
			"qgis_version": "3.40",
			"project_path": "/tmp/original.qgz",
			"tools":        tools,
		},
	})

	waitForInstanceActionCondition(t, func() bool {
		return provider.HasInstanceConnected(instanceID)
	})

	cleanup := func() {
		_ = conn.Close()
		_ = provider.Close()
		registry.Close()
		httpServer.Close()
	}
	return registry, provider, conn, cleanup
}

func sendInstanceActionWSMessage(t *testing.T, conn *websocket.Conn, msg instanceActionWSMessage) {
	t.Helper()
	payload, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal websocket message failed: %v", err)
	}
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write websocket message failed: %v", err)
	}
}

func readInstanceActionWSMessage(t *testing.T, conn *websocket.Conn) instanceActionWSMessage {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket message failed: %v", err)
	}
	var msg instanceActionWSMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode websocket message failed: %v", err)
	}
	return msg
}

func waitForInstanceActionCondition(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not satisfied before timeout")
}

func TestInstanceRegisterEndpoints(t *testing.T) {
	router := buildInstanceTestRouter(t)

	body1 := `{"instance_id":"qgis_1001","pid":1001,"qgis_version":"3.34"}`
	req1 := httptest.NewRequest(http.MethodPost, "/api/instances/register", bytes.NewBufferString(body1))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("POST /api/instances/register status=%d body=%s", rr1.Code, rr1.Body.String())
	}

	body2 := `{"pid":2002,"qgis_version":"3.34"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/instances/register", bytes.NewBufferString(body2))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/instances/register (invalid body) status=%d body=%s", rr2.Code, rr2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("GET /api/instances status=%d body=%s", rr3.Code, rr3.Body.String())
	}

	resp := decodeAPIResponse(t, rr3)
	if !resp.Success {
		t.Fatalf("GET /api/instances success=false: %+v", resp)
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected data type: %T", resp.Data)
	}
	if got, ok := data["instance_count"].(float64); !ok || int(got) != 1 {
		t.Fatalf("unexpected instance_count: %#v", data["instance_count"])
	}
	if got, ok := data["qgis_available"].(bool); !ok || !got {
		t.Fatalf("unexpected qgis_available: %#v", data["qgis_available"])
	}
}

func TestInstanceRegisterRejectsMissingInstanceID(t *testing.T) {
	router := buildInstanceTestRouter(t)

	body := `{"pid":1001}`
	req := httptest.NewRequest(http.MethodPost, "/api/instances/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	if resp.Code != errCodeInstanceRegisterInvalid {
		t.Fatalf("unexpected code: %s", resp.Code)
	}
}

func TestCapabilitiesReflectInstanceAvailability(t *testing.T) {
	router := buildInstanceTestRouter(t)

	req1 := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("GET /api/capabilities status=%d body=%s", rr1.Code, rr1.Body.String())
	}

	resp1 := decodeAPIResponse(t, rr1)
	data1 := resp1.Data.(map[string]any)
	if got, ok := data1["qgis_available"].(bool); !ok || got {
		t.Fatalf("expected qgis_available=false, got=%#v", data1["qgis_available"])
	}

	body := `{"instance_id":"qgis_1001","pid":1001,"qgis_version":"3.34"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/instances/register", bytes.NewBufferString(body))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rr2.Code, rr2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("GET /api/capabilities status=%d body=%s", rr3.Code, rr3.Body.String())
	}

	resp3 := decodeAPIResponse(t, rr3)
	data3 := resp3.Data.(map[string]any)
	if got, ok := data3["qgis_available"].(bool); !ok || !got {
		t.Fatalf("expected qgis_available=true, got=%#v", data3["qgis_available"])
	}
}

func TestListInstancesFallsBackToRuntimeSnapshotWhenProxyListEmpty(t *testing.T) {
	registry := discovery.NewInstanceRegistry()
	runtimeCoordinator := &stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "qgis_runtime_1",
			InstanceCount: 1,
			RunningCount:  1,
			Items: []*discovery.QGISInstance{{
				ID:                 "qgis_runtime_1",
				PID:                3001,
				QGISVersion:        "3.34",
				ProjectPath:        "/tmp/runtime.qgz",
				Status:             "running",
				Services:           map[string]discovery.ServiceInfo{},
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
				LastHeart:          time.Now(),
				MachineFingerprint: "runtime-host",
			}},
		},
		Stale: true,
	}}
	handler := NewAPIWithServices(nil, nil, registry, nil, runtimeCoordinator, nil, nil, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/instances status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected response type: %T", resp.Data)
	}
	instances, ok := data["instances"].([]any)
	if !ok || len(instances) != 1 {
		t.Fatalf("unexpected instances payload: %#v", data["instances"])
	}
	if got, _ := data["active_id"].(string); got != "qgis_runtime_1" {
		t.Fatalf("unexpected active_id: %q", got)
	}
	if got, _ := data["qgis_available"].(bool); !got {
		t.Fatalf("expected qgis_available=true, got %#v", data["qgis_available"])
	}
}

func TestCloseInstanceEndpointRequiresLocalBridgeProvider(t *testing.T) {
	router := buildInstanceTestRouter(t)

	registerBody := `{"instance_id":"qgis_close_1","pid":1001,"qgis_version":"3.34"}`
	registerReq := httptest.NewRequest(http.MethodPost, "/api/instances/register", bytes.NewBufferString(registerBody))
	registerReq.Header.Set("Content-Type", "application/json")
	registerRR := httptest.NewRecorder()
	router.ServeHTTP(registerRR, registerReq)
	if registerRR.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", registerRR.Code, registerRR.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/instances/qgis_close_1/close", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /api/instances/{id}/close status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	if resp.Code != errCodeInstanceCloseUnavailable {
		t.Fatalf("unexpected code: %s", resp.Code)
	}
}

func TestCloseInstanceEndpointUsesLocalBridgeProvider(t *testing.T) {
	registry, provider, conn, cleanup := setupInstanceActionBridge(t, "qgis_close_1", []map[string]any{{
		"name":        "close_instance",
		"inputSchema": map[string]any{"type": "object"},
	}})
	defer cleanup()

	router := buildInstanceTestRouterWithLocalBridge(t, registry, provider)
	errCh := make(chan string, 1)
	go func() {
		msg := readInstanceActionWSMessage(t, conn)
		if msg.Type != "tool_invoke" {
			errCh <- "unexpected message type: " + msg.Type
			return
		}
		if got, _ := msg.Data["tool_name"].(string); got != "close_instance" {
			errCh <- "unexpected tool name: " + got
			return
		}
		params, _ := msg.Data["params"].(map[string]any)
		if got, ok := params["delay_ms"].(float64); !ok || int(got) != 200 {
			errCh <- "unexpected delay_ms"
			return
		}
		requestID, _ := msg.Data["request_id"].(string)
		sendInstanceActionWSMessage(t, conn, instanceActionWSMessage{
			Type: "tool_result",
			Data: map[string]any{
				"request_id": requestID,
				"success":    true,
				"output":     map[string]any{"closed": true},
			},
		})
		errCh <- ""
	}()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/qgis_close_1/close", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/instances/{id}/close status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	if !resp.Success {
		t.Fatalf("response should succeed: %+v", resp)
	}
	data := resp.Data.(map[string]any)
	if got, ok := data["status"].(string); !ok || got != "closing" {
		t.Fatalf("unexpected status: %#v", data["status"])
	}
	if got, ok := data["instance_id"].(string); !ok || got != "qgis_close_1" {
		t.Fatalf("unexpected instance_id: %#v", data["instance_id"])
	}
	if errMsg := <-errCh; errMsg != "" {
		t.Fatal(errMsg)
	}
}

func TestOpenInstanceProjectEndpointUsesLocalBridgeProvider(t *testing.T) {
	registry, provider, conn, cleanup := setupInstanceActionBridge(t, "qgis_open_1", []map[string]any{{
		"name":        "open_project",
		"inputSchema": map[string]any{"type": "object"},
	}})
	defer cleanup()

	router := buildInstanceTestRouterWithLocalBridge(t, registry, provider)
	errCh := make(chan string, 1)
	go func() {
		msg := readInstanceActionWSMessage(t, conn)
		if got, _ := msg.Data["tool_name"].(string); got != "open_project" {
			errCh <- "unexpected tool name"
			return
		}
		params, _ := msg.Data["params"].(map[string]any)
		if got, _ := params["path"].(string); got != "/tmp/demo.qgz" {
			errCh <- "unexpected path"
			return
		}
		requestID, _ := msg.Data["request_id"].(string)
		sendInstanceActionWSMessage(t, conn, instanceActionWSMessage{
			Type: "tool_result",
			Data: map[string]any{
				"request_id": requestID,
				"success":    true,
				"output":     map[string]any{"success": true},
			},
		})
		errCh <- ""
	}()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/qgis_open_1/open-project", bytes.NewBufferString(`{"path":"/tmp/demo.qgz"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/instances/{id}/open-project status=%d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeAPIResponse(t, rr)
	if !resp.Success {
		t.Fatalf("response should succeed: %+v", resp)
	}
	if errMsg := <-errCh; errMsg != "" {
		t.Fatal(errMsg)
	}
}

func TestCreateInstanceProjectEndpointUsesLocalBridgeProvider(t *testing.T) {
	registry, provider, conn, cleanup := setupInstanceActionBridge(t, "qgis_create_1", []map[string]any{{
		"name":        "create_project",
		"inputSchema": map[string]any{"type": "object"},
	}})
	defer cleanup()

	router := buildInstanceTestRouterWithLocalBridge(t, registry, provider)
	errCh := make(chan string, 1)
	go func() {
		msg := readInstanceActionWSMessage(t, conn)
		if got, _ := msg.Data["tool_name"].(string); got != "create_project" {
			errCh <- "unexpected tool name"
			return
		}
		params, _ := msg.Data["params"].(map[string]any)
		if got, _ := params["path"].(string); got != "/tmp/new.qgz" {
			errCh <- "unexpected path"
			return
		}
		requestID, _ := msg.Data["request_id"].(string)
		sendInstanceActionWSMessage(t, conn, instanceActionWSMessage{
			Type: "tool_result",
			Data: map[string]any{
				"request_id": requestID,
				"success":    true,
				"output":     map[string]any{"success": true},
			},
		})
		errCh <- ""
	}()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/qgis_create_1/create-project", bytes.NewBufferString(`{"path":"/tmp/new.qgz"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/instances/{id}/create-project status=%d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeAPIResponse(t, rr)
	if !resp.Success {
		t.Fatalf("response should succeed: %+v", resp)
	}
	if errMsg := <-errCh; errMsg != "" {
		t.Fatal(errMsg)
	}
}

func TestOpenInstanceProjectEndpointReturnsBridgeErrorDetails(t *testing.T) {
	registry, provider, conn, cleanup := setupInstanceActionBridge(t, "qgis_open_err_1", []map[string]any{{
		"name":        "open_project",
		"inputSchema": map[string]any{"type": "object"},
	}})
	defer cleanup()

	router := buildInstanceTestRouterWithLocalBridge(t, registry, provider)
	go func() {
		msg := readInstanceActionWSMessage(t, conn)
		requestID, _ := msg.Data["request_id"].(string)
		sendInstanceActionWSMessage(t, conn, instanceActionWSMessage{
			Type: "tool_result",
			Data: map[string]any{
				"request_id": requestID,
				"success":    false,
				"error":      "实例桥接尚未就绪",
				"error_code": bridgeprovider.ToolErrorCodeNotReady,
				"provider_diagnostics": map[string]any{
					"provider_id":            "qgis_bridge",
					"unavailable_reason":     "interactive_instance_disconnected",
					"connected_instance_ids": []any{},
				},
			},
		})
	}()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/qgis_open_err_1/open-project", bytes.NewBufferString(`{"path":"/tmp/demo.qgz"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected bridge failure status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	if resp.Success {
		t.Fatalf("expected response failure: %+v", resp)
	}
	details, ok := resp.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected error details map, got %#v", resp.Details)
	}
	if got, _ := details["error_code"].(string); got != bridgeprovider.ToolErrorCodeNotReady {
		t.Fatalf("unexpected error_code details: %#v", details)
	}
	diagnostics, ok := details["provider_diagnostics"].(map[string]any)
	if !ok || diagnostics["provider_id"] != "qgis_bridge" {
		t.Fatalf("unexpected provider diagnostics: %#v", details["provider_diagnostics"])
	}
}

func TestHiddenToolDirectInvokeRejected(t *testing.T) {
	registry, provider, conn, cleanup := setupInstanceActionBridge(t, "qgis_hidden_1", []map[string]any{{
		"name":        "open_project",
		"inputSchema": map[string]any{"type": "object"},
	}})
	defer cleanup()

	router := buildInstanceTestRouterWithLocalBridgeAndAssetCatalog(t, registry, provider, stubInstanceAssetCatalogService{
		visibility: "hidden",
		scopes:     []string{"tool.invoke.internal"},
		known:      true,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/instances/qgis_hidden_1/open-project", bytes.NewBufferString(`{"path":"/tmp/demo.qgz"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected hidden tool invoke rejection status=%d body=%s", rr.Code, rr.Body.String())
	}

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected hidden tool invoke to be rejected before bridge dispatch")
	}
}

func TestInstanceProjectEndpointRejectsToolWithoutInvokeScope(t *testing.T) {
	registry, provider, conn, cleanup := setupInstanceActionBridge(t, "qgis_scope_1", []map[string]any{{
		"name":        "open_project",
		"inputSchema": map[string]any{"type": "object"},
	}})
	defer cleanup()

	router := buildInstanceTestRouterWithLocalBridgeAndAssetCatalog(t, registry, provider, stubInstanceAssetCatalogService{
		visibility: "public",
		scopes:     []string{"catalog.read"},
		known:      true,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/instances/qgis_scope_1/open-project", bytes.NewBufferString(`{"path":"/tmp/demo.qgz"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected missing scope rejection status=%d body=%s", rr.Code, rr.Body.String())
	}

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected missing invoke scope to be rejected before bridge dispatch")
	}
}

func TestAuthorizeInstanceToolInvokeRejectsUnknownPolicy(t *testing.T) {
	api := &API{assetCatalogService: stubInstanceAssetCatalogService{known: false}}

	err := api.authorizeInstanceToolInvoke(context.Background(), "create_project", "instance_project_action", "qgis_unknown_1")
	if err == nil {
		t.Fatal("expected unknown-policy tool invoke to be rejected")
	}
	var denied *instanceToolInvokeDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected instanceToolInvokeDeniedError, got %T", err)
	}
}
