package websocket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/discovery"
	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
)

type localBridgeWSMessage struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

func TestHandleConnectionRoutesPluginRoleToLocalBridgeProvider(t *testing.T) {
	provider, wsServer, wsURL, cleanup := setupLocalBridgeWebsocketServer(t)
	defer cleanup()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"?role=plugin", nil)
	if err != nil {
		t.Fatalf("dial plugin websocket failed: %v", err)
	}
	defer conn.Close()

	sendLocalBridgeWSMessage(t, conn, localBridgeWSMessage{
		Type: "instance_connect",
		Data: map[string]any{
			"instance_id":  "inst-role",
			"pid":          1001,
			"qgis_version": "3.40",
			"project_path": "/tmp/role.qgz",
			"tools":        []map[string]any{{"name": "open_project", "inputSchema": map[string]any{"type": "object"}}},
		},
	})

	waitForLocalBridgeCondition(t, func() bool {
		return provider.HasInstanceConnected("inst-role") && wsServer.GetRuntimeSnapshot().Instances.RunningCount == 1
	})
	snapshot := wsServer.GetRuntimeSnapshot()
	if snapshot.Instances.RunningCount != 1 {
		t.Fatalf("unexpected running count: %d", snapshot.Instances.RunningCount)
	}
}

func TestHandleConnectionStoresPluginSkillsDirFromInstanceConnect(t *testing.T) {
	provider, _, wsURL, cleanup := setupLocalBridgeWebsocketServer(t)
	defer cleanup()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"?role=plugin", nil)
	if err != nil {
		t.Fatalf("dial plugin websocket failed: %v", err)
	}
	defer conn.Close()

	sendLocalBridgeWSMessage(t, conn, localBridgeWSMessage{
		Type: "instance_connect",
		Data: map[string]any{
			"instance_id":  "inst-skills",
			"pid":          1004,
			"qgis_version": "3.40",
			"project_path": "/tmp/skills.qgz",
			"plugin_path":  "/tmp/plugin",
			"skills_dir":   "/tmp/plugin/skills",
			"tools":        []map[string]any{{"name": "open_project", "inputSchema": map[string]any{"type": "object"}}},
		},
	})

	waitForLocalBridgeCondition(t, func() bool {
		return provider.HasInstanceConnected("inst-skills")
	})

	if got := provider.PluginSkillsDir(); got != "/tmp/plugin/skills" {
		t.Fatalf("unexpected plugin skills dir: %q", got)
	}
}

func TestCurrentPluginSkillsDirPrefersLiveLocalBridgeValue(t *testing.T) {
	provider, _, wsURL, cleanup := setupLocalBridgeWebsocketServer(t)
	defer cleanup()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"?role=plugin", nil)
	if err != nil {
		t.Fatalf("dial plugin websocket failed: %v", err)
	}
	defer conn.Close()

	sendLocalBridgeWSMessage(t, conn, localBridgeWSMessage{
		Type: "instance_connect",
		Data: map[string]any{
			"instance_id":  "inst-live-plugin-dir",
			"pid":          1005,
			"qgis_version": "3.40",
			"project_path": "/tmp/live.qgz",
			"skills_dir":   "/tmp/live-plugin/skills",
			"tools":        []map[string]any{{"name": "open_project", "inputSchema": map[string]any{"type": "object"}}},
		},
	})

	waitForLocalBridgeCondition(t, func() bool {
		return provider.HasInstanceConnected("inst-live-plugin-dir")
	})

	server := NewDeepServer(0, nil)
	server.SetLocalBridgeProvider(provider)
	server.SetAgentRebuildConfig(&AgentRebuildConfig{PluginSkillsDir: "/tmp/stale-plugin/skills"})

	if got := server.currentPluginSkillsDir(); got != "/tmp/live-plugin/skills" {
		t.Fatalf("expected live plugin skills dir, got %q", got)
	}
}

func TestGetRuntimeSnapshotMergesLocalBridgeProjection(t *testing.T) {
	registry := discovery.NewInstanceRegistry()
	defer registry.Close()
	provider := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, registry)
	provider.SetDefaultInstanceSource(discovery.SourceAgentReverseWS)
	if err := registry.Register(discovery.RegisterRequest{
		InstanceID:  "inst-local",
		PID:         2001,
		QGISVersion: "3.40",
		ProjectPath: "/tmp/local.qgz",
		Source:      discovery.SourceAgentReverseWS,
	}); err != nil {
		t.Fatalf("register local instance failed: %v", err)
	}
	provider.SetActiveInstance("inst-local")

	server := NewDeepServer(0, nil)
	server.SetLocalBridgeProvider(provider)
	server.PrimeRuntimeState(middlewareclient.RuntimeSnapshot{
		Services: map[string]middlewareclient.ServiceStatus{
			discovery.ServiceNameQGISBridge: {
				ID:            discovery.ServiceNameQGISBridge,
				Status:        "running",
				Source:        discovery.SourceReverseWS,
				InstanceCount: 1,
				RunningCount:  1,
			},
		},
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "inst-middleware",
			InstanceCount: 1,
			RunningCount:  1,
			Items: []*discovery.QGISInstance{{
				ID:          "inst-middleware",
				ProjectPath: "/tmp/middleware.qgz",
				Status:      "running",
				Source:      discovery.SourceReverseWS,
				Services: map[string]discovery.ServiceInfo{
					discovery.ServiceNameQGISBridge: {Status: "running"},
				},
			}},
		},
	}, nil)

	snapshot := server.GetRuntimeSnapshot()
	if snapshot.Instances.ActiveID != "inst-local" {
		t.Fatalf("unexpected merged active id: %q", snapshot.Instances.ActiveID)
	}
	if snapshot.Instances.RunningCount != 2 {
		t.Fatalf("unexpected merged running count: %d", snapshot.Instances.RunningCount)
	}
	if len(snapshot.Instances.Items) != 2 {
		t.Fatalf("unexpected merged instance count: %d", len(snapshot.Instances.Items))
	}
	localService, ok := snapshot.Services["qgis"]
	if !ok {
		t.Fatalf("expected qgis service in snapshot")
	}
	if localService.RunningCount != 2 {
		t.Fatalf("unexpected aggregated qgis running count: %d", localService.RunningCount)
	}
}

func TestBroadcastSessionLogResetDoesNotForwardIgnoredPluginSideChannel(t *testing.T) {
	provider, wsServer, wsURL, cleanup := setupLocalBridgeWebsocketServer(t)
	defer cleanup()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"?role=plugin", nil)
	if err != nil {
		t.Fatalf("dial plugin websocket failed: %v", err)
	}
	defer conn.Close()

	sendLocalBridgeWSMessage(t, conn, localBridgeWSMessage{
		Type: "instance_connect",
		Data: map[string]any{
			"instance_id":  "inst-log",
			"pid":          1003,
			"qgis_version": "3.40",
			"project_path": "/tmp/log.qgz",
			"tools":        []map[string]any{{"name": "open_project", "inputSchema": map[string]any{"type": "object"}}},
		},
	})

	waitForLocalBridgeCondition(t, func() bool {
		return provider.HasInstanceConnected("inst-log") && wsServer.hasLocalBridgeRunningInstances()
	})

	wsServer.broadcastSessionLogReset("sid-log", "/tmp/log.qgz", "/tmp/session-log")
	assertNoLocalBridgeWSMessageWithin(t, conn, 200*time.Millisecond)
}

func TestInstanceLeaseExpiry(t *testing.T) {
	registry := discovery.NewInstanceRegistry()
	defer registry.Close()
	provider := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, registry)
	defer provider.Close()

	server := NewDeepServer(0, nil)
	server.localBridgeCleanupInterval = 10 * time.Millisecond
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	server.mu.Lock()
	server.clients[serverConn] = true
	server.connWriteMu[serverConn] = &sync.Mutex{}
	server.mu.Unlock()

	if err := registry.Register(discovery.RegisterRequest{
		InstanceID:  "inst-expire",
		PID:         999999,
		QGISVersion: "3.40",
		ProjectPath: "/tmp/expire.qgz",
		Source:      discovery.SourceAgentReverseWS,
	}); err != nil {
		t.Fatalf("register instance failed: %v", err)
	}

	server.SetLocalBridgeProvider(provider)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if registry.Get("inst-expire") == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if inst := registry.Get("inst-expire"); inst != nil {
		t.Fatal("expected cleanup to remove expired instance")
	}

	msg := readSingleMessage(t, clientConn)
	assertRuntimeSnapshotCounts(t, msg, 0, 1)
	instances, _ := msg.Data["instances"].(map[string]any)
	items, _ := instances["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one explainable expired instance, got %#v", items)
	}
	instance, _ := items[0].(map[string]any)
	if got := strings.TrimSpace(testJSONString(instance["status"])); got != "dead" {
		t.Fatalf("expected expired instance status dead, got %q", got)
	}
	if got := strings.TrimSpace(testJSONString(instance["status_reason"])); got != "lease_expired" {
		t.Fatalf("expected status_reason lease_expired, got %q", got)
	}
	if got := strings.TrimSpace(testJSONString(instance["status_detail"])); got == "" {
		t.Fatal("expected expired instance status_detail to be populated")
	}
	if got := strings.TrimSpace(testJSONString(instances["active_id"])); got != "" {
		t.Fatalf("expected no active instance after expiry, got %q", got)
	}
}

func TestInstanceExpiryBroadcastsRuntimeSnapshot(t *testing.T) {
	TestInstanceLeaseExpiry(t)
}

func TestRuntimeSnapshotPublishesExpiredInstanceReason(t *testing.T) {
	registry := discovery.NewInstanceRegistry()
	defer registry.Close()
	provider := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, registry)
	defer provider.Close()

	server := NewDeepServer(0, nil)
	server.localBridgeCleanupInterval = 10 * time.Millisecond
	server.SetLocalBridgeProvider(provider)

	if err := registry.Register(discovery.RegisterRequest{
		InstanceID:  "inst-expired-reason",
		PID:         999999,
		QGISVersion: "3.40",
		ProjectPath: "/tmp/expired-reason.qgz",
		Source:      discovery.SourceAgentReverseWS,
	}); err != nil {
		t.Fatalf("register instance failed: %v", err)
	}

	waitForLocalBridgeCondition(t, func() bool {
		return registry.Get("inst-expired-reason") == nil && server.GetRuntimeSnapshot().Instances.RunningCount == 0
	})

	snapshot := server.GetRuntimeSnapshot()
	if snapshot.Instances.InstanceCount != 1 {
		t.Fatalf("expected expired instance to stay explainable in snapshot, got count=%d", snapshot.Instances.InstanceCount)
	}
	if len(snapshot.Instances.Items) != 1 {
		t.Fatalf("expected one explainable instance item, got=%d", len(snapshot.Instances.Items))
	}
	if snapshot.Instances.Items[0] == nil {
		t.Fatal("expected explainable instance item")
	}
	if snapshot.Instances.Items[0].Status != "dead" {
		t.Fatalf("expected expired instance status dead, got=%q", snapshot.Instances.Items[0].Status)
	}

	raw, err := json.Marshal(snapshot.Instances.Items[0])
	if err != nil {
		t.Fatalf("marshal expired instance failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal expired instance failed: %v", err)
	}
	if got := strings.TrimSpace(testJSONString(payload["status_reason"])); got != "lease_expired" {
		t.Fatalf("expected status_reason=lease_expired, got=%q payload=%#v", got, payload)
	}
	if got := strings.TrimSpace(testJSONString(payload["status_detail"])); got == "" {
		t.Fatalf("expected status_detail for expired instance, got payload=%#v", payload)
	}
	if snapshot.Instances.ActiveID != "" {
		t.Fatalf("expected no active instance after expiry, got=%q", snapshot.Instances.ActiveID)
	}
}

func TestSnapshotKeepsNonRunningInstanceForExplainability(t *testing.T) {
	registry := discovery.NewInstanceRegistry()
	defer registry.Close()
	provider := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, registry)
	defer provider.Close()

	server := NewDeepServer(0, nil)
	server.SetLocalBridgeProvider(provider)

	if err := registry.Register(discovery.RegisterRequest{
		InstanceID:  "inst-non-running-kept",
		PID:         os.Getpid(),
		QGISVersion: "3.40",
		ProjectPath: "/tmp/non-running-kept.qgz",
		Source:      discovery.SourceAgentReverseWS,
	}); err != nil {
		t.Fatalf("register instance failed: %v", err)
	}
	provider.SetActiveInstance("inst-non-running-kept")
	server.HandleLocalBridgeStateChanged()

	if err := registry.Unregister("inst-non-running-kept"); err != nil {
		t.Fatalf("unregister instance failed: %v", err)
	}

	waitForLocalBridgeCondition(t, func() bool {
		return server.GetRuntimeSnapshot().Instances.RunningCount == 0
	})

	snapshot := server.GetRuntimeSnapshot()
	if snapshot.Instances.InstanceCount != 1 {
		t.Fatalf("expected stopped instance to remain explainable in snapshot, got count=%d", snapshot.Instances.InstanceCount)
	}
	if len(snapshot.Instances.Items) != 1 {
		t.Fatalf("expected one non-running instance item, got=%d", len(snapshot.Instances.Items))
	}
	if snapshot.Instances.Items[0] == nil {
		t.Fatal("expected non-running instance item")
	}
	if snapshot.Instances.Items[0].Status != "stopped" {
		t.Fatalf("expected stopped instance status, got=%q", snapshot.Instances.Items[0].Status)
	}
	if snapshot.Instances.ActiveID != "" {
		t.Fatalf("expected active instance to be cleared, got=%q", snapshot.Instances.ActiveID)
	}
}

func TestHeartbeatRecovery(t *testing.T) {
	registry := discovery.NewInstanceRegistry()
	defer registry.Close()
	provider := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, registry)
	defer provider.Close()

	server := NewDeepServer(0, nil)
	server.localBridgeCleanupInterval = 10 * time.Millisecond
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	server.mu.Lock()
	server.clients[serverConn] = true
	server.connWriteMu[serverConn] = &sync.Mutex{}
	server.mu.Unlock()

	server.SetLocalBridgeProvider(provider)

	registerReq := discovery.RegisterRequest{
		InstanceID:  "inst-heartbeat",
		PID:         os.Getpid(),
		QGISVersion: "3.40",
		ProjectPath: "/tmp/heartbeat.qgz",
		Source:      discovery.SourceAgentReverseWS,
	}
	if err := registry.Register(registerReq); err != nil {
		t.Fatalf("register instance failed: %v", err)
	}
	provider.SetActiveInstance(registerReq.InstanceID)
	server.HandleLocalBridgeStateChanged()
	assertRuntimeSnapshotCounts(t, readSingleMessage(t, clientConn), 1, 1)

	if err := registry.Unregister(registerReq.InstanceID); err != nil {
		t.Fatalf("unregister instance failed: %v", err)
	}
	msg := readSingleMessage(t, clientConn)
	assertRuntimeSnapshotCounts(t, msg, 0, 1)
	instances, _ := msg.Data["instances"].(map[string]any)
	items, _ := instances["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected stopped instance to remain explainable, got %#v", items)
	}
	instance, _ := items[0].(map[string]any)
	if got := strings.TrimSpace(testJSONString(instance["status"])); got != "stopped" {
		t.Fatalf("expected stopped instance status, got %q", got)
	}
	if got := strings.TrimSpace(testJSONString(instance["status_reason"])); got != "instance_stopped" {
		t.Fatalf("expected status_reason instance_stopped, got %q", got)
	}
	if got := strings.TrimSpace(testJSONString(instances["active_id"])); got != "" {
		t.Fatalf("expected active instance to clear after stop, got %q", got)
	}

	if err := registry.Register(registerReq); err != nil {
		t.Fatalf("re-register instance failed: %v", err)
	}
	provider.SetActiveInstance(registerReq.InstanceID)
	if err := registry.Heartbeat(registerReq.InstanceID); err != nil {
		t.Fatalf("heartbeat instance failed: %v", err)
	}

	assertRuntimeSnapshotCounts(t, readSingleMessage(t, clientConn), 1, 1)
}

func TestSequentialNonRunningEventsRetainMultipleExplainableInstances(t *testing.T) {
	registry := discovery.NewInstanceRegistry()
	defer registry.Close()
	provider := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, registry)
	defer provider.Close()

	server := NewDeepServer(0, nil)
	server.localBridgeCleanupInterval = 10 * time.Millisecond
	server.SetLocalBridgeProvider(provider)

	expireReq := discovery.RegisterRequest{
		InstanceID:  "inst-expire-multi",
		PID:         999999,
		QGISVersion: "3.40",
		ProjectPath: "/tmp/expire-multi.qgz",
		Source:      discovery.SourceAgentReverseWS,
	}
	stopReq := discovery.RegisterRequest{
		InstanceID:  "inst-stop-multi",
		PID:         os.Getpid(),
		QGISVersion: "3.40",
		ProjectPath: "/tmp/stop-multi.qgz",
		Source:      discovery.SourceAgentReverseWS,
	}
	if err := registry.Register(expireReq); err != nil {
		t.Fatalf("register expire instance failed: %v", err)
	}
	if err := registry.Register(stopReq); err != nil {
		t.Fatalf("register stop instance failed: %v", err)
	}
	provider.SetActiveInstance(stopReq.InstanceID)
	server.HandleLocalBridgeStateChanged()

	waitForLocalBridgeCondition(t, func() bool {
		snapshot := server.GetRuntimeSnapshot()
		return snapshot.Instances.RunningCount == 1 && hasSnapshotInstanceStatus(snapshot, expireReq.InstanceID, "dead")
	})

	if err := registry.Unregister(stopReq.InstanceID); err != nil {
		t.Fatalf("unregister stop instance failed: %v", err)
	}

	waitForLocalBridgeCondition(t, func() bool {
		return server.GetRuntimeSnapshot().Instances.RunningCount == 0
	})

	snapshot := server.GetRuntimeSnapshot()
	if snapshot.Instances.InstanceCount != 2 {
		t.Fatalf("expected two explainable non-running instances, got count=%d snapshot=%#v", snapshot.Instances.InstanceCount, snapshot.Instances)
	}
	if !hasSnapshotInstanceStatus(snapshot, expireReq.InstanceID, "dead") {
		t.Fatalf("expected expired instance %q to remain explainable, snapshot=%#v", expireReq.InstanceID, snapshot.Instances.Items)
	}
	if !hasSnapshotInstanceStatus(snapshot, stopReq.InstanceID, "stopped") {
		t.Fatalf("expected stopped instance %q to remain explainable, snapshot=%#v", stopReq.InstanceID, snapshot.Instances.Items)
	}
	if snapshot.Instances.ActiveID != "" {
		t.Fatalf("expected active instance cleared after sequential non-running events, got=%q", snapshot.Instances.ActiveID)
	}
	if len(snapshot.Instances.Items) != 2 {
		t.Fatalf("expected two retained instance items, got=%d", len(snapshot.Instances.Items))
	}
	if findSnapshotInstanceCount(snapshot, expireReq.InstanceID) != 1 || findSnapshotInstanceCount(snapshot, stopReq.InstanceID) != 1 {
		t.Fatalf("expected retained items keyed by id without duplication, snapshot=%#v", snapshot.Instances.Items)
	}
}

func TestHeartbeatRecoveryRetainsOtherExplainableInstancesByID(t *testing.T) {
	registry := discovery.NewInstanceRegistry()
	defer registry.Close()
	provider := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, registry)
	defer provider.Close()

	server := NewDeepServer(0, nil)
	server.localBridgeCleanupInterval = 10 * time.Millisecond
	server.SetLocalBridgeProvider(provider)

	expireReq := discovery.RegisterRequest{
		InstanceID:  "inst-expire-retained",
		PID:         999999,
		QGISVersion: "3.40",
		ProjectPath: "/tmp/expire-retained.qgz",
		Source:      discovery.SourceAgentReverseWS,
	}
	recoverReq := discovery.RegisterRequest{
		InstanceID:  "inst-recover-retained",
		PID:         os.Getpid(),
		QGISVersion: "3.40",
		ProjectPath: "/tmp/recover-retained.qgz",
		Source:      discovery.SourceAgentReverseWS,
	}
	if err := registry.Register(expireReq); err != nil {
		t.Fatalf("register expire instance failed: %v", err)
	}
	if err := registry.Register(recoverReq); err != nil {
		t.Fatalf("register recover instance failed: %v", err)
	}
	provider.SetActiveInstance(recoverReq.InstanceID)
	server.HandleLocalBridgeStateChanged()

	waitForLocalBridgeCondition(t, func() bool {
		snapshot := server.GetRuntimeSnapshot()
		return snapshot.Instances.RunningCount == 1 && hasSnapshotInstanceStatus(snapshot, expireReq.InstanceID, "dead")
	})

	if err := registry.Unregister(recoverReq.InstanceID); err != nil {
		t.Fatalf("unregister recover instance failed: %v", err)
	}
	waitForLocalBridgeCondition(t, func() bool {
		snapshot := server.GetRuntimeSnapshot()
		return snapshot.Instances.RunningCount == 0 && hasSnapshotInstanceStatus(snapshot, recoverReq.InstanceID, "stopped")
	})

	if err := registry.Register(recoverReq); err != nil {
		t.Fatalf("re-register recover instance failed: %v", err)
	}
	provider.SetActiveInstance(recoverReq.InstanceID)
	if err := registry.Heartbeat(recoverReq.InstanceID); err != nil {
		t.Fatalf("heartbeat recover instance failed: %v", err)
	}

	waitForLocalBridgeCondition(t, func() bool {
		snapshot := server.GetRuntimeSnapshot()
		return snapshot.Instances.RunningCount == 1 && hasSnapshotInstanceStatus(snapshot, recoverReq.InstanceID, "running")
	})

	snapshot := server.GetRuntimeSnapshot()
	if snapshot.Instances.InstanceCount != 2 {
		t.Fatalf("expected one recovered running instance plus one retained expired instance, got count=%d snapshot=%#v", snapshot.Instances.InstanceCount, snapshot.Instances)
	}
	if !hasSnapshotInstanceStatus(snapshot, expireReq.InstanceID, "dead") {
		t.Fatalf("expected expired instance %q to remain after other instance recovery, snapshot=%#v", expireReq.InstanceID, snapshot.Instances.Items)
	}
	if !hasSnapshotInstanceStatus(snapshot, recoverReq.InstanceID, "running") {
		t.Fatalf("expected recovered instance %q to be running, snapshot=%#v", recoverReq.InstanceID, snapshot.Instances.Items)
	}
	if snapshot.Instances.ActiveID != recoverReq.InstanceID {
		t.Fatalf("expected recovered instance to become active, got=%q", snapshot.Instances.ActiveID)
	}
	if findSnapshotInstanceCount(snapshot, recoverReq.InstanceID) != 1 {
		t.Fatalf("expected recovered instance to replace retained stopped copy by id, snapshot=%#v", snapshot.Instances.Items)
	}
}

func hasSnapshotInstanceStatus(snapshot middlewareclient.RuntimeSnapshot, instanceID string, status string) bool {
	for _, item := range snapshot.Instances.Items {
		if item == nil {
			continue
		}
		if strings.TrimSpace(item.ID) != instanceID {
			continue
		}
		if strings.TrimSpace(item.Status) == status {
			return true
		}
	}
	return false
}

func findSnapshotInstanceCount(snapshot middlewareclient.RuntimeSnapshot, instanceID string) int {
	count := 0
	for _, item := range snapshot.Instances.Items {
		if item != nil && strings.TrimSpace(item.ID) == instanceID {
			count++
		}
	}
	return count
}

func testJSONString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func assertRuntimeSnapshotCounts(t *testing.T, msg Message, runningCount, instanceCount uint64) {
	t.Helper()
	if msg.Type != MsgTypeRuntimeSnapshot {
		t.Fatalf("expected %s message, got %#v", MsgTypeRuntimeSnapshot, msg)
	}
	instances, _ := msg.Data["instances"].(map[string]any)
	if parseUint64Like(instances["running_count"]) != runningCount {
		t.Fatalf("expected running_count=%d, got %#v", runningCount, instances["running_count"])
	}
	if parseUint64Like(instances["instance_count"]) != instanceCount {
		t.Fatalf("expected instance_count=%d, got %#v", instanceCount, instances["instance_count"])
	}
}

func setupLocalBridgeWebsocketServer(t *testing.T) (*bridgeprovider.Provider, *DeepServer, string, func()) {
	t.Helper()
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	registry := discovery.NewInstanceRegistry()
	provider := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, registry)
	provider.SetDefaultInstanceSource(discovery.SourceAgentReverseWS)
	wsServer := NewDeepServer(0, nil)
	wsServer.SetLocalBridgeProvider(provider)
	provider.SetCatalogChangedHook(func() {
		wsServer.HandleLocalBridgeStateChanged()
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsServer.HandleConnection)
	httpServer := httptest.NewServer(mux)
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	cleanup := func() {
		_ = provider.Close()
		registry.Close()
		httpServer.Close()
	}
	return provider, wsServer, wsURL, cleanup
}

func sendLocalBridgeWSMessage(t *testing.T, conn *websocket.Conn, msg localBridgeWSMessage) {
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

func readLocalBridgeWSMessage(t *testing.T, conn *websocket.Conn) localBridgeWSMessage {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket message failed: %v", err)
	}
	var msg localBridgeWSMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode websocket message failed: %v", err)
	}
	return msg
}

func assertNoLocalBridgeWSMessageWithin(t *testing.T, conn *websocket.Conn, timeout time.Duration) {
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

func waitForLocalBridgeCondition(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not satisfied before timeout")
}
