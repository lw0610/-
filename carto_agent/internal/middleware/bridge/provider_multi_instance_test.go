package bridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/discovery"
)

func TestProviderAutoSwitchesActiveInstanceWhenConnectionCloses(t *testing.T) {
	bridgeProvider, wsURL, cleanup := setupBridgeProviderServer(t)
	defer cleanup()

	connA := dialBridgeProvider(t, wsURL)
	defer connA.Close()
	connB := dialBridgeProvider(t, wsURL)
	defer connB.Close()

	sendInstanceConnectWithTools(t, connA, "inst-a", []string{"data_load_vector"})
	sendInstanceConnectWithTools(t, connB, "inst-b", []string{"layout_create"})
	waitForConnectedInstanceCount(t, bridgeProvider, 2)
	bridgeProvider.SetActiveInstance("inst-a")
	waitForActiveInstance(t, bridgeProvider, "inst-a")

	if err := connA.Close(); err != nil {
		t.Fatalf("close connA failed: %v", err)
	}
	waitForActiveInstance(t, bridgeProvider, "inst-b")
}

func TestProviderListToolsIgnoresPluginCatalogTruthAcrossInstances(t *testing.T) {
	bridgeProvider, wsURL, cleanup := setupBridgeProviderServer(t)
	defer cleanup()

	connA := dialBridgeProvider(t, wsURL)
	defer connA.Close()
	connB := dialBridgeProvider(t, wsURL)
	defer connB.Close()

	sendInstanceConnectWithTools(t, connA, "inst-a", []string{"data_load_vector"})
	sendInstanceConnectWithTools(t, connB, "inst-b", []string{"layout_create", "data_load_vector"})
	waitForConnectedInstanceCount(t, bridgeProvider, 2)

	tools, err := bridgeProvider.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("expected adapter-only bridge to expose no plugin-sent tools, got %#v", tools)
	}
}

func TestProviderListInstanceToolsReturnsEmptyWithoutRequestingPluginCatalogTruth(t *testing.T) {
	bridgeProvider, wsURL, cleanup := setupBridgeProviderServer(t)
	defer cleanup()

	connA := dialBridgeProvider(t, wsURL)
	defer connA.Close()
	connB := dialBridgeProvider(t, wsURL)
	defer connB.Close()

	sendInstanceConnectWithToolsAndExtras(t, connA, "inst-a", []string{"data_load_vector"}, map[string]any{"python_path": "/opt/qgis/bin/python"})
	sendInstanceConnectWithToolsAndExtras(t, connB, "inst-b", []string{"data_load_vector"}, map[string]any{"python_path": "/opt/qgis/bin/python"})
	waitForConnectedInstanceCount(t, bridgeProvider, 2)
	bridgeProvider.SetActiveInstance("inst-a")
	waitForActiveInstance(t, bridgeProvider, "inst-a")

	tools, err := bridgeProvider.ListInstanceTools(context.Background(), "inst-b")
	if err != nil {
		t.Fatalf("ListInstanceTools failed: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("unexpected tools: %#v", tools)
	}
	assertNoWSMessageWithin(t, connA, 200*time.Millisecond)
	assertNoWSMessageWithin(t, connB, 200*time.Millisecond)
}

func TestProviderResolveTargetPluginRejectsMissingExplicitInstanceWithoutFallback(t *testing.T) {
	bridgeProvider, wsURL, cleanup := setupBridgeProviderServer(t)
	defer cleanup()

	connA := dialBridgeProvider(t, wsURL)
	defer connA.Close()
	connB := dialBridgeProvider(t, wsURL)
	defer connB.Close()

	sendInstanceConnectWithTools(t, connA, "inst-a", []string{"data_load_vector"})
	sendInstanceConnectWithTools(t, connB, "inst-b", []string{"layout_create"})
	waitForConnectedInstanceCount(t, bridgeProvider, 2)
	bridgeProvider.SetActiveInstance("inst-a")
	waitForActiveInstance(t, bridgeProvider, "inst-a")

	resolvedID, pluginState, err := bridgeProvider.resolveTargetPlugin(map[string]any{"instance_id": "inst-missing"})
	if err == nil {
		t.Fatalf("expected explicit missing instance to be rejected, got instance=%q state=%#v", resolvedID, pluginState)
	}
	if !strings.Contains(err.Error(), "inst-missing") {
		t.Fatalf("expected error to mention explicit target, got %v", err)
	}
	if strings.TrimSpace(resolvedID) != "" || pluginState != nil {
		t.Fatalf("expected no fallback plugin resolution, got instance=%q state=%#v", resolvedID, pluginState)
	}
}

func setupBridgeProviderServer(t *testing.T) (*Provider, string, func()) {
	t.Helper()

	instanceRegistry := discovery.NewInstanceRegistry()
	bridgeProvider := NewProvider("qgis_bridge", instanceRegistry)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", bridgeProvider.HandleWebSocket)
	httpServer := httptest.NewServer(mux)
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"

	cleanup := func() {
		_ = bridgeProvider.Close()
		instanceRegistry.Close()
		httpServer.Close()
	}
	return bridgeProvider, wsURL, cleanup
}

func dialBridgeProvider(t *testing.T, wsURL string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial bridge websocket failed: %v", err)
	}
	return conn
}

func sendInstanceConnectWithTools(t *testing.T, conn *websocket.Conn, instanceID string, toolNames []string) {
	t.Helper()
	sendInstanceConnectWithToolsAndExtras(t, conn, instanceID, toolNames, nil)
}

func sendInstanceConnectWithToolsAndExtras(t *testing.T, conn *websocket.Conn, instanceID string, toolNames []string, extras map[string]any) {
	t.Helper()
	tools := make([]map[string]any, 0, len(toolNames))
	for _, name := range toolNames {
		tools = append(tools, map[string]any{
			"name":        name,
			"description": name + " desc",
			"category":    "test",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			},
		})
	}
	data := map[string]any{
		"instance_id":  instanceID,
		"pid":          99999,
		"qgis_version": "3.34.0",
		"project_path": "/tmp/demo.qgz",
		"tools":        tools,
	}
	for key, value := range extras {
		data[key] = value
	}
	sendWSMessage(t, conn, testWSMessage{
		Type: msgTypeInstanceConnect,
		Data: data,
	})
}

func waitForConnectedInstanceCount(t *testing.T, bridgeProvider *Provider, expected int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		bridgeProvider.mu.RLock()
		count := len(bridgeProvider.instancePlugins)
		bridgeProvider.mu.RUnlock()
		if count == expected {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	bridgeProvider.mu.RLock()
	count := len(bridgeProvider.instancePlugins)
	bridgeProvider.mu.RUnlock()
	t.Fatalf("waitForConnectedInstanceCount timeout: expected=%d got=%d", expected, count)
}

func waitForActiveInstance(t *testing.T, bridgeProvider *Provider, expected string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := bridgeProvider.ActiveInstance(); got == expected {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waitForActiveInstance timeout: expected=%s got=%s", expected, bridgeProvider.ActiveInstance())
}

func assertNoWSMessageWithin(t *testing.T, conn *websocket.Conn, timeout time.Duration) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	_, _, err := conn.ReadMessage()
	if err == nil {
		t.Fatal("expected no websocket message")
	}
	if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
		return
	}
	if strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return
	}
	t.Fatalf("expected timeout waiting for websocket message, got %v", err)
}
