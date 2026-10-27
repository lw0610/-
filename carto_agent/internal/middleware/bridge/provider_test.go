package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/toolcontract"
)

type fakeDynamicToolRegistry struct {
	registerCalls   []dynamicRegisterCall
	unregisterCalls []dynamicUnregisterCall
}

type dynamicRegisterCall struct {
	instanceID string
	defs       []ToolDefinition
}

type dynamicUnregisterCall struct {
	instanceID string
	names      []string
}

func (f *fakeDynamicToolRegistry) RegisterInstanceTools(instanceID string, defs []ToolDefinition) {
	cloned := make([]ToolDefinition, len(defs))
	copy(cloned, defs)
	f.registerCalls = append(f.registerCalls, dynamicRegisterCall{instanceID: instanceID, defs: cloned})
}

func (f *fakeDynamicToolRegistry) UnregisterInstanceTools(instanceID string, toolNames []string) {
	f.unregisterCalls = append(f.unregisterCalls, dynamicUnregisterCall{instanceID: instanceID, names: append([]string(nil), toolNames...)})
}

func TestProviderUpgraderRejectsExternalOrigin(t *testing.T) {
	bridgeProvider := NewProvider("qgis_bridge", discovery.NewInstanceRegistry())
	defer bridgeProvider.Close()

	req := httptest.NewRequest("GET", "http://127.0.0.1/ws", nil)
	req.RemoteAddr = "127.0.0.1:50201"
	req.Header.Set("Origin", "https://example.com")
	if bridgeProvider.upgrader.CheckOrigin == nil {
		t.Fatal("expected bridge upgrader CheckOrigin to be configured")
	}
	if bridgeProvider.upgrader.CheckOrigin(req) {
		t.Fatal("expected bridge provider to reject external origin")
	}
}

type testWSMessage struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

func TestProviderInvokeToolRoundTripAndKeepCallerArgs(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()

	sendInstanceConnectWithExtras(t, conn, "inst-1", map[string]any{"python_path": "/opt/qgis/bin/python"})
	waitForInstanceConnected(t, bridgeProvider, "inst-1")

	callerArgs := map[string]any{"instance_id": "inst-1", "path": "/tmp/a.shp"}
	invokeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		msg := readWSMessage(t, conn)
		if msg.Type != "tool_invoke" {
			t.Errorf("unexpected message type: %s", msg.Type)
			return
		}
		if got, _ := msg.Data["tool_name"].(string); got != "data_load_vector" {
			t.Errorf("unexpected tool name: %q", got)
			return
		}
		params, _ := msg.Data["params"].(map[string]any)
		if got, _ := params["path"].(string); got != "/tmp/a.shp" {
			t.Errorf("unexpected path param: %#v", params)
			return
		}
		if got, _ := msg.Data["protocol_version"].(float64); got != 1 {
			t.Errorf("expected protocol_version=1, got %#v", msg.Data["protocol_version"])
			return
		}
		requestID, _ := msg.Data["request_id"].(string)
		sendWSMessage(t, conn, testWSMessage{
			Type: "tool_result",
			Data: map[string]any{
				"request_id": requestID,
				"success":    true,
				"output":     map[string]any{"result": "ok"},
				"error":      "",
				"output_layers": []map[string]any{{
					"path":      "/tmp/result.gpkg",
					"auto_load": true,
				}},
			},
		})
	}()

	result, err := bridgeProvider.InvokeTool(invokeCtx, "data_load_vector", callerArgs)
	if err != nil {
		t.Fatalf("InvokeTool failed: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected invoke result, got %#v", result)
	}
	output, ok := result.Output.(map[string]any)
	if !ok || output["result"] != "ok" {
		t.Fatalf("unexpected invoke output: %#v", result.Output)
	}
	if len(result.OutputLayers) != 1 || result.OutputLayers[0].Path != "/tmp/result.gpkg" || !result.OutputLayers[0].AutoLoad {
		t.Fatalf("unexpected output layers: %#v", result.OutputLayers)
	}

	if gotID, _ := callerArgs["instance_id"].(string); gotID != "inst-1" {
		t.Fatalf("caller args should keep instance_id, got: %#v", callerArgs)
	}
}

func TestProviderSendsToolEngineContractAfterInstanceConnect(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()

	bridgeProvider.SetToolContractProvider(func() ([]toolcontract.ExecutionManifestEntry, error) {
		return []toolcontract.ExecutionManifestEntry{{
			Name:        "get_project_info",
			Description: "Read QGIS project info",
			Runtime:     "qgis",
			Backend:     "qgis",
			Binding:     toolcontract.ExecutionBindingBackendTool,
			BackendTool: "get_project_info",
		}}, nil
	})

	sendInstanceConnect(t, conn, "inst-contract")
	waitForInstanceConnected(t, bridgeProvider, "inst-contract")

	msg := readWSMessage(t, conn)
	if msg.Type != msgTypeToolEngineContract {
		t.Fatalf("expected tool_engine_contract, got %s", msg.Type)
	}
	manifest, ok := msg.Data["execution_manifest"].([]any)
	if !ok || len(manifest) != 1 {
		t.Fatalf("expected one execution manifest entry, got %#v", msg.Data["execution_manifest"])
	}
	entry, ok := manifest[0].(map[string]any)
	if !ok {
		t.Fatalf("expected manifest entry object, got %#v", manifest[0])
	}
	if got, _ := entry["name"].(string); got != "get_project_info" {
		t.Fatalf("unexpected manifest tool name: %#v", entry)
	}
	if got, _ := entry["backend_tool"].(string); got != "get_project_info" {
		t.Fatalf("unexpected backend tool name: %#v", entry)
	}
	if catalog, ok := msg.Data["tool_catalog"].([]any); !ok || len(catalog) != 0 {
		t.Fatalf("expected empty tool_catalog placeholder, got %#v", msg.Data["tool_catalog"])
	}
}

func TestProviderInvokeToolRoundTripPreservesTerminalStateFields(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()

	sendInstanceConnect(t, conn, "inst-state")
	waitForInstanceConnected(t, bridgeProvider, "inst-state")

	invokeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		msg := readWSMessage(t, conn)
		requestID, _ := msg.Data["request_id"].(string)
		sendWSMessage(t, conn, testWSMessage{
			Type: msgTypeToolResult,
			Data: map[string]any{
				"request_id":     requestID,
				"success":        true,
				"output":         map[string]any{"result": "ok"},
				"error":          "",
				"status":         "succeeded",
				"terminal_state": "succeeded",
				"progress":       1.0,
				"message":        "执行完成",
			},
		})
	}()

	result, err := bridgeProvider.InvokeTool(invokeCtx, "data_load_vector", map[string]any{"instance_id": "inst-state"})
	if err != nil {
		t.Fatalf("InvokeTool failed: %v", err)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result failed: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal result failed: %v", err)
	}
	if got, _ := decoded["status"].(string); got != "succeeded" {
		t.Fatalf("expected status=succeeded, got %#v", decoded)
	}
	if got, _ := decoded["terminal_state"].(string); got != "succeeded" {
		t.Fatalf("expected terminal_state=succeeded, got %#v", decoded)
	}
	if got, _ := decoded["message"].(string); got != "执行完成" {
		t.Fatalf("expected message to be preserved, got %#v", decoded)
	}
	if got, _ := decoded["progress"].(float64); got != 1.0 {
		t.Fatalf("expected progress=1.0, got %#v", decoded)
	}
}

func TestProviderInvokeToolContextCanceledSendsBestEffortToolCancel(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()

	sendInstanceConnect(t, conn, "inst-cancel")
	waitForInstanceConnected(t, bridgeProvider, "inst-cancel")

	invokeCtx, cancel := context.WithCancel(context.Background())
	callerArgs := map[string]any{"instance_id": "inst-cancel", "path": "/tmp/cancel.shp"}
	errCh := make(chan error, 1)

	go func() {
		_, err := bridgeProvider.InvokeTool(invokeCtx, "data_load_vector", callerArgs)
		errCh <- err
	}()

	invokeMsg := readWSMessage(t, conn)
	if invokeMsg.Type != msgTypeToolInvoke {
		t.Fatalf("unexpected invoke message type: %s", invokeMsg.Type)
	}
	requestID, _ := invokeMsg.Data["request_id"].(string)
	if strings.TrimSpace(requestID) == "" {
		t.Fatalf("expected request_id in invoke message: %#v", invokeMsg)
	}

	cancel()

	cancelMsg := readWSMessage(t, conn)
	if cancelMsg.Type != "tool_cancel" {
		t.Fatalf("expected tool_cancel after ctx cancel, got %s", cancelMsg.Type)
	}
	if got, _ := cancelMsg.Data["protocol_version"].(float64); got != 1 {
		t.Fatalf("expected cancel protocol_version=1, got %#v", cancelMsg.Data["protocol_version"])
	}
	if got, _ := cancelMsg.Data["request_id"].(string); got != requestID {
		t.Fatalf("unexpected cancel request_id: got %q want %q", got, requestID)
	}
	if got, _ := cancelMsg.Data["tool_name"].(string); got != "data_load_vector" {
		t.Fatalf("unexpected cancel tool_name: %q", got)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("InvokeTool did not return after context cancellation")
	}
}

func TestProviderInvokeToolReturnsProviderMissingErrorWhenNoInstanceConnected(t *testing.T) {
	bridgeProvider := NewProvider("qgis_bridge", discovery.NewInstanceRegistry())
	defer bridgeProvider.Close()

	_, err := bridgeProvider.InvokeTool(context.Background(), "data_load_vector", map[string]any{})
	if err == nil {
		t.Fatal("expected provider missing error")
	}
	var invokeErr *ToolInvokeError
	if !errors.As(err, &invokeErr) {
		t.Fatalf("expected ToolInvokeError, got %T %v", err, err)
	}
	if invokeErr.Code != ToolErrorCodeProviderMissing {
		t.Fatalf("unexpected error code: %#v", invokeErr)
	}
	if got, _ := invokeErr.ProviderDiagnostics["provider_id"].(string); got != "qgis_bridge" {
		t.Fatalf("unexpected provider diagnostics: %#v", invokeErr.ProviderDiagnostics)
	}
}

func TestProviderInvokeToolReturnsInstanceRequiredErrorForMissingExplicitInstance(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()

	sendInstanceConnect(t, conn, "inst-connected")
	waitForInstanceConnected(t, bridgeProvider, "inst-connected")

	_, err := bridgeProvider.InvokeTool(context.Background(), "data_load_vector", map[string]any{"instance_id": "inst-missing"})
	if err == nil {
		t.Fatal("expected instance required error")
	}
	var invokeErr *ToolInvokeError
	if !errors.As(err, &invokeErr) {
		t.Fatalf("expected ToolInvokeError, got %T %v", err, err)
	}
	if invokeErr.Code != ToolErrorCodeInstanceRequired {
		t.Fatalf("unexpected error code: %#v", invokeErr)
	}
	if got, _ := invokeErr.ProviderDiagnostics["requested_instance_id"].(string); got != "inst-missing" {
		t.Fatalf("unexpected provider diagnostics: %#v", invokeErr.ProviderDiagnostics)
	}
}

func TestProviderInvokeToolRoundTripPreservesErrorCodeAndDiagnostics(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()

	sendInstanceConnect(t, conn, "inst-error")
	waitForInstanceConnected(t, bridgeProvider, "inst-error")

	invokeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		msg := readWSMessage(t, conn)
		requestID, _ := msg.Data["request_id"].(string)
		sendWSMessage(t, conn, testWSMessage{
			Type: msgTypeToolResult,
			Data: map[string]any{
				"request_id": requestID,
				"success":    false,
				"error":      "工具未就绪",
				"error_code": ToolErrorCodeNotReady,
				"provider_diagnostics": map[string]any{
					"instance_id": "inst-error",
				},
			},
		})
	}()

	result, err := bridgeProvider.InvokeTool(invokeCtx, "data_load_vector", map[string]any{"instance_id": "inst-error"})
	if err != nil {
		t.Fatalf("InvokeTool failed: %v", err)
	}
	if result == nil || result.ErrorCode != ToolErrorCodeNotReady {
		t.Fatalf("expected error code preserved, got %#v", result)
	}
	if got, _ := result.ProviderDiagnostics["instance_id"].(string); got != "inst-error" {
		t.Fatalf("unexpected provider diagnostics: %#v", result.ProviderDiagnostics)
	}
}

func TestProviderHandleInstanceDisconnectRemovesConnectedInstance(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()

	sendInstanceConnect(t, conn, "inst-2")
	waitForInstanceConnected(t, bridgeProvider, "inst-2")

	sendWSMessage(t, conn, testWSMessage{
		Type: msgTypeInstanceDisconnect,
		Data: map[string]any{"instance_id": "inst-2"},
	})
	waitForInstanceDisconnected(t, bridgeProvider, "inst-2")
}

func TestBridgeDisconnect_PreservesRuntimeIdentity(t *testing.T) {
	bridgeProvider, wsURL, cleanup := setupBridgeProviderServer(t)
	defer cleanup()

	conn := dialBridgeProvider(t, wsURL)
	defer conn.Close()

	sendInstanceConnect(t, conn, "inst-preserve")
	waitForInstanceConnected(t, bridgeProvider, "inst-preserve")
	before := waitForRegistryInstancePresent(t, bridgeProvider.InstanceRegistry(), "inst-preserve")

	if err := conn.Close(); err != nil {
		t.Fatalf("close websocket failed: %v", err)
	}
	waitForInstanceDisconnected(t, bridgeProvider, "inst-preserve")

	afterDisconnect := bridgeProvider.InstanceRegistry().Get("inst-preserve")
	if afterDisconnect == nil {
		t.Fatal("expected runtime identity to remain registered during disconnect grace period")
	}
	if afterDisconnect.Status != "running" {
		t.Fatalf("expected instance to remain running during grace period, got %q", afterDisconnect.Status)
	}
	if !afterDisconnect.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("expected runtime identity to be preserved, created_at changed from %v to %v", before.CreatedAt, afterDisconnect.CreatedAt)
	}
}

func TestBridgeReconnect_ReusesInstanceIdentity(t *testing.T) {
	bridgeProvider, wsURL, cleanup := setupBridgeProviderServer(t)
	defer cleanup()

	connA := dialBridgeProvider(t, wsURL)
	sendInstanceConnect(t, connA, "inst-reuse")
	waitForInstanceConnected(t, bridgeProvider, "inst-reuse")
	before := waitForRegistryInstancePresent(t, bridgeProvider.InstanceRegistry(), "inst-reuse")

	if err := connA.Close(); err != nil {
		t.Fatalf("close first websocket failed: %v", err)
	}
	waitForInstanceDisconnected(t, bridgeProvider, "inst-reuse")

	connB := dialBridgeProvider(t, wsURL)
	defer connB.Close()
	sendInstanceConnect(t, connB, "inst-reuse")
	waitForInstanceConnected(t, bridgeProvider, "inst-reuse")

	afterReconnect := waitForRegistryInstancePresent(t, bridgeProvider.InstanceRegistry(), "inst-reuse")
	if !afterReconnect.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("expected reconnect to reuse runtime identity, created_at changed from %v to %v", before.CreatedAt, afterReconnect.CreatedAt)
	}

	time.Sleep(1500 * time.Millisecond)
	stillRegistered := bridgeProvider.InstanceRegistry().Get("inst-reuse")
	if stillRegistered == nil {
		t.Fatal("expected reconnect to cancel pending unregister")
	}
	if !stillRegistered.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("expected identity to remain stable after grace window, created_at changed from %v to %v", before.CreatedAt, stillRegistered.CreatedAt)
	}
}

func TestBridgeGracefulTimeout_UnregistersAfterDeadline(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()

	sendInstanceConnect(t, conn, "inst-timeout")
	waitForInstanceConnected(t, bridgeProvider, "inst-timeout")
	waitForRegistryInstancePresent(t, bridgeProvider.InstanceRegistry(), "inst-timeout")

	sendWSMessage(t, conn, testWSMessage{
		Type: msgTypeInstanceDisconnect,
		Data: map[string]any{"instance_id": "inst-timeout"},
	})
	waitForInstanceDisconnected(t, bridgeProvider, "inst-timeout")

	if bridgeProvider.InstanceRegistry().Get("inst-timeout") == nil {
		t.Fatal("expected instance to remain registered until disconnect grace deadline expires")
	}
	waitForRegistryInstanceAbsent(t, bridgeProvider.InstanceRegistry(), "inst-timeout")
}

func TestProviderIgnoresPluginCatalogTruthOnConnectAndDisconnect(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()

	registry := &fakeDynamicToolRegistry{}
	bridgeProvider.SetDynamicToolRegistry(registry)

	sendInstanceConnectWithExtras(t, conn, "inst-sync", map[string]any{
		"skills_dir": "/tmp/plugin/skills",
		"tools": []map[string]any{{
			"name":        "data_load_vector",
			"description": "load vector",
			"category":    "data",
			"inputSchema": map[string]any{"type": "object"},
		}},
	})
	waitForInstanceConnected(t, bridgeProvider, "inst-sync")

	if len(registry.registerCalls) != 0 {
		t.Fatalf("expected no register calls, got %#v", registry.registerCalls)
	}

	sendWSMessage(t, conn, testWSMessage{
		Type: msgTypeInstanceDisconnect,
		Data: map[string]any{"instance_id": "inst-sync"},
	})
	waitForInstanceDisconnected(t, bridgeProvider, "inst-sync")

	if len(registry.unregisterCalls) != 0 {
		t.Fatalf("expected no unregister calls, got %#v", registry.unregisterCalls)
	}
}

func TestProviderPluginSkillsDirUsesPluginReportedSkillsDir(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()

	sendInstanceConnectWithExtras(t, conn, "inst-skills", map[string]any{"skills_dir": "/tmp/plugin/skills"})
	waitForInstanceConnected(t, bridgeProvider, "inst-skills")

	if got := bridgeProvider.PluginSkillsDir(); got != "/tmp/plugin/skills" {
		t.Fatalf("expected plugin skills dir from live plugin, got %q", got)
	}
}

func TestProviderListToolsIgnoresPluginSentCatalogTruth(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()

	sendInstanceConnectWithExtras(t, conn, "inst-tools", map[string]any{
		"tools": []map[string]any{{
			"name":        "data_load_vector",
			"description": "load vector",
			"category":    "data",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
		}},
	})
	waitForInstanceConnected(t, bridgeProvider, "inst-tools")

	tools, err := bridgeProvider.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("expected no tools from plugin-sent catalog truth, got %#v", tools)
	}
}

func TestProviderDefaultInstanceSourceCanBeOverridden(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()
	bridgeProvider.SetDefaultInstanceSource(discovery.SourceAgentReverseWS)

	sendInstanceConnect(t, conn, "inst-agent")
	waitForInstanceConnected(t, bridgeProvider, "inst-agent")

	instance := bridgeProvider.InstanceRegistry().Get("inst-agent")
	if instance == nil {
		t.Fatal("expected instance to be registered")
	}
	if instance.Source != discovery.SourceAgentReverseWS {
		t.Fatalf("unexpected source: %q", instance.Source)
	}
	if _, ok := instance.Services[discovery.ServiceNameAgentQGISBridge]; !ok {
		t.Fatalf("expected %s service, got %#v", discovery.ServiceNameAgentQGISBridge, instance.Services)
	}
}

func TestProviderProjectEventTriggersHookAndUpdatesProjectPath(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()
	var called atomic.Int32
	bridgeProvider.SetCatalogChangedHook(func() {
		called.Add(1)
	})

	sendInstanceConnect(t, conn, "inst-project")
	waitForInstanceConnected(t, bridgeProvider, "inst-project")

	tests := []struct {
		name         string
		eventType    string
		projectPath  string
		expectedPath string
	}{
		{
			name:         "project_opened updates project path",
			eventType:    "project_opened",
			projectPath:  "/tmp/opened.qgz",
			expectedPath: "/tmp/opened.qgz",
		},
		{
			name:         "project_saved updates project path",
			eventType:    "project_saved",
			projectPath:  "/tmp/saved.qgz",
			expectedPath: "/tmp/saved.qgz",
		},
		{
			name:         "project_closed clears project path",
			eventType:    "project_closed",
			projectPath:  "",
			expectedPath: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			beforeCalls := called.Load()
			payload := map[string]any{}
			if tc.projectPath != "" {
				payload["project_path"] = tc.projectPath
			}

			sendWSMessage(t, conn, testWSMessage{
				Type: msgTypeInstanceProjectEvent,
				Data: map[string]any{
					"instance_id": "inst-project",
					"event_type":  tc.eventType,
					"data":        payload,
				},
			})

			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				instance := bridgeProvider.InstanceRegistry().Get("inst-project")
				if instance != nil && instance.ProjectPath == tc.expectedPath && called.Load() > beforeCalls {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}

			instance := bridgeProvider.InstanceRegistry().Get("inst-project")
			if instance == nil {
				t.Fatal("expected instance to exist after project event")
			}
			if instance.ProjectPath != tc.expectedPath {
				t.Fatalf("unexpected updated project path for %s: %q", tc.eventType, instance.ProjectPath)
			}
			if called.Load() <= beforeCalls {
				t.Fatalf("expected project event %s to trigger catalog/state hook", tc.eventType)
			}
		})
	}
}

func TestProviderPrefersActiveInstancePythonPath(t *testing.T) {
	bridgeProvider, conn, cleanup := setupBridgeProviderTest(t)
	defer cleanup()

	sendWSMessage(t, conn, testWSMessage{
		Type: msgTypeInstanceConnect,
		Data: map[string]any{
			"instance_id":  "inst-python",
			"pid":          99999,
			"qgis_version": "3.34.0",
			"python_path":  "/opt/qgis/bin/python",
			"project_path": "/tmp/demo.qgz",
			"tools":        []map[string]any{},
		},
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if bridgeProvider.PluginPythonPath() == "/opt/qgis/bin/python" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if got := bridgeProvider.PluginPythonPath(); got != "/opt/qgis/bin/python" {
		t.Fatalf("unexpected python path: %q", got)
	}
}

func setupBridgeProviderTest(t *testing.T) (*Provider, *websocket.Conn, func()) {
	t.Helper()

	instanceRegistry := discovery.NewInstanceRegistry()
	bridgeProvider := NewProvider("qgis_bridge", instanceRegistry)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", bridgeProvider.HandleWebSocket)
	httpServer := httptest.NewServer(mux)

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		httpServer.Close()
		instanceRegistry.Close()
		t.Fatalf("dial bridge websocket failed: %v", err)
	}

	cleanup := func() {
		_ = conn.Close()
		_ = bridgeProvider.Close()
		instanceRegistry.Close()
		httpServer.Close()
	}
	return bridgeProvider, conn, cleanup
}

func sendInstanceConnect(t *testing.T, conn *websocket.Conn, instanceID string) {
	t.Helper()
	sendInstanceConnectWithExtras(t, conn, instanceID, nil)
}

func sendInstanceConnectWithExtras(t *testing.T, conn *websocket.Conn, instanceID string, extras map[string]any) {
	t.Helper()
	data := map[string]any{
		"instance_id":  instanceID,
		"pid":          99999,
		"qgis_version": "3.34.0",
		"project_path": "/tmp/demo.qgz",
	}
	for key, value := range extras {
		data[key] = value
	}
	sendWSMessage(t, conn, testWSMessage{
		Type: msgTypeInstanceConnect,
		Data: data,
	})
}

func sendWSMessage(t *testing.T, conn *websocket.Conn, msg testWSMessage) {
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

func readWSMessage(t *testing.T, conn *websocket.Conn) testWSMessage {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket message failed: %v", err)
	}
	var msg testWSMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode websocket message failed: %v", err)
	}
	return msg
}

func waitForInstanceConnected(t *testing.T, bridgeProvider *Provider, instanceID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if bridgeProvider.HasInstanceConnected(instanceID) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waitForInstanceConnected timeout: instance=%s", instanceID)
}

func waitForInstanceDisconnected(t *testing.T, bridgeProvider *Provider, instanceID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !bridgeProvider.HasInstanceConnected(instanceID) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waitForInstanceDisconnected timeout: instance=%s", instanceID)
}

func waitForRegistryInstancePresent(t *testing.T, registry *discovery.InstanceRegistry, instanceID string) *discovery.QGISInstance {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if inst := registry.Get(instanceID); inst != nil {
			return inst
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waitForRegistryInstancePresent timeout: instance=%s", instanceID)
	return nil
}

func waitForRegistryInstanceAbsent(t *testing.T, registry *discovery.InstanceRegistry, instanceID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if registry.Get(instanceID) == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waitForRegistryInstanceAbsent timeout: instance=%s", instanceID)
}
