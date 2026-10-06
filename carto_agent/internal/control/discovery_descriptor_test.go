package control

import (
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/protocol"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
)

func TestDiscoveryDescriptor(t *testing.T) {
	now := time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC)
	snapshot := middlewareclient.RuntimeSnapshot{
		Version:         7,
		Timestamp:       now.Add(-5 * time.Second),
		RuntimeRevision: "runtime-rev-7",
		ServicePhase:    "ready",
		ActiveConfig:    "default",
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "qgis-1",
			InstanceCount: 1,
			RunningCount:  1,
		},
	}
	entry := discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        50321,
		Protocol:    "websocket",
		Status:      "running",
	}

	descriptor, err := BuildDiscoveryDescriptor(snapshot, entry, now, 30*time.Second)
	if err != nil {
		t.Fatalf("BuildDiscoveryDescriptor returned error: %v", err)
	}
	if descriptor.Version != protocol.ContractVersionV1 {
		t.Fatalf("unexpected descriptor version: %q", descriptor.Version)
	}
	if descriptor.Revision != 7 {
		t.Fatalf("unexpected revision: %d", descriptor.Revision)
	}
	if descriptor.Port != 50321 {
		t.Fatalf("unexpected descriptor port: %d", descriptor.Port)
	}
	if descriptor.BackendURL != "http://127.0.0.1:50321" {
		t.Fatalf("unexpected backend_url: %q", descriptor.BackendURL)
	}
	if descriptor.WSURL != "ws://127.0.0.1:50321/ws" {
		t.Fatalf("unexpected ws_url: %q", descriptor.WSURL)
	}
	if descriptor.Readiness != DiscoveryReadinessReady {
		t.Fatalf("unexpected readiness: %q", descriptor.Readiness)
	}
	if descriptor.Degraded {
		t.Fatal("expected descriptor degraded=false")
	}
	if descriptor.RetryHints.Retryable {
		t.Fatalf("expected ready descriptor retryable=false, got %#v", descriptor.RetryHints)
	}
	if descriptor.RuntimeRevision != "runtime-rev-7" {
		t.Fatalf("unexpected runtime_revision: %q", descriptor.RuntimeRevision)
	}
	if descriptor.ActiveInstanceID != "qgis-1" {
		t.Fatalf("unexpected active_instance_id: %q", descriptor.ActiveInstanceID)
	}
	if descriptor.RestartSequence != 7 {
		t.Fatalf("unexpected restart_sequence: %d", descriptor.RestartSequence)
	}
	if descriptor.ExpiresAt != now.Add(25*time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("unexpected expires_at: %q", descriptor.ExpiresAt)
	}
}

func TestDiscoveryDescriptor_SupportedContractVersionsAuthority(t *testing.T) {
	supported := protocol.SupportedContractVersions()
	if len(supported) != 1 {
		t.Fatalf("expected exactly one supported contract version, got %#v", supported)
	}
	if supported[0] != protocol.ContractVersionV1 {
		t.Fatalf("unexpected supported contract version: %#v", supported)
	}
}

func TestDiscoveryDescriptorReturnsBlockReadinessDuringStartup(t *testing.T) {
	now := time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC)
	descriptor, err := BuildDiscoveryDescriptor(middlewareclient.RuntimeSnapshot{
		Version:         3,
		Timestamp:       now.Add(-2 * time.Second),
		RuntimeRevision: "runtime-rev-3",
		ServicePhase:    "starting",
		ActiveConfig:    "default",
		Instances: middlewareclient.InstancesSnapshot{
			InstanceCount: 0,
			RunningCount:  0,
		},
	}, discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        3000,
		Protocol:    "http",
		Status:      "running",
	}, now, 30*time.Second)
	if err != nil {
		t.Fatalf("BuildDiscoveryDescriptor returned error: %v", err)
	}
	if descriptor.Readiness != DiscoveryReadinessBlock {
		t.Fatalf("unexpected readiness: %q", descriptor.Readiness)
	}
	if descriptor.Degraded {
		t.Fatal("expected startup block to avoid degraded flag")
	}
	if !descriptor.RetryHints.Retryable {
		t.Fatalf("expected retryable startup descriptor, got %#v", descriptor.RetryHints)
	}
	if descriptor.RetryHints.AfterMS <= 0 {
		t.Fatalf("expected retry hint after_ms > 0, got %#v", descriptor.RetryHints)
	}
	if descriptor.ReasonCode != "BACKEND_STARTING" {
		t.Fatalf("unexpected reason_code: %q", descriptor.ReasonCode)
	}
	if descriptor.ReasonMessage != "等待后端监听器完成启动后再重试。" {
		t.Fatalf("unexpected reason_message: %q", descriptor.ReasonMessage)
	}
}

func TestDiscoveryDescriptor_WaitingPluginIsDegradedButRetryable(t *testing.T) {
	now := time.Date(2026, time.April, 8, 5, 0, 0, 0, time.UTC)
	descriptor, err := BuildDiscoveryDescriptor(middlewareclient.RuntimeSnapshot{
		Version:         13,
		Timestamp:       now.Add(-2 * time.Second),
		RuntimeRevision: "runtime-rev-13",
		ServicePhase:    "waiting_plugin",
		Instances: middlewareclient.InstancesSnapshot{
			InstanceCount: 0,
			RunningCount:  0,
		},
	}, discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        50303,
		Protocol:    "http",
		Status:      "running",
	}, now, 30*time.Second)
	if err != nil {
		t.Fatalf("BuildDiscoveryDescriptor returned error: %v", err)
	}
	if descriptor.Readiness != DiscoveryReadinessDegraded {
		t.Fatalf("unexpected readiness: %q", descriptor.Readiness)
	}
	if !descriptor.Degraded {
		t.Fatal("expected waiting_plugin descriptor to be degraded")
	}
	if descriptor.ReasonCode != "PLUGIN_REGISTRATION_PENDING" {
		t.Fatalf("unexpected reason_code: %q", descriptor.ReasonCode)
	}
	if descriptor.ReasonMessage != "后端已就绪，等待 plugin 完成注册后恢复完整运行态连接。" {
		t.Fatalf("unexpected reason_message: %q", descriptor.ReasonMessage)
	}
	if !descriptor.RetryHints.Retryable {
		t.Fatalf("expected retryable waiting_plugin descriptor, got %#v", descriptor.RetryHints)
	}
	if descriptor.RetryHints.AfterMS <= 0 {
		t.Fatalf("expected waiting_plugin retry hint after_ms > 0, got %#v", descriptor.RetryHints)
	}
}

func TestDiscoveryDescriptor_ReadyDoesNotRequirePluginRegistration(t *testing.T) {
	now := time.Date(2026, time.April, 9, 2, 0, 0, 0, time.UTC)
	descriptor, err := BuildDiscoveryDescriptor(middlewareclient.RuntimeSnapshot{
		Version:         14,
		Timestamp:       now.Add(-2 * time.Second),
		RuntimeRevision: "runtime-rev-14",
		ServicePhase:    "ready",
		ActiveConfig:    "default",
		Instances: middlewareclient.InstancesSnapshot{
			InstanceCount: 0,
			RunningCount:  0,
		},
	}, discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        50304,
		Protocol:    "http",
		Status:      "running",
	}, now, 30*time.Second)
	if err != nil {
		t.Fatalf("BuildDiscoveryDescriptor returned error: %v", err)
	}
	if descriptor.Readiness != DiscoveryReadinessReady {
		t.Fatalf("unexpected readiness: %q", descriptor.Readiness)
	}
	if descriptor.Degraded {
		t.Fatal("expected ready descriptor degraded=false without plugin registration")
	}
	if descriptor.ReasonCode != "" {
		t.Fatalf("unexpected reason_code: %q", descriptor.ReasonCode)
	}
	if descriptor.RetryHints.Retryable {
		t.Fatalf("expected non-retryable ready descriptor, got %#v", descriptor.RetryHints)
	}
	if descriptor.ServicePhase != "ready" {
		t.Fatalf("unexpected service_phase: %q", descriptor.ServicePhase)
	}
}

func TestDiscoveryDescriptor_ReadyIgnoresInteractiveInstanceDisconnectedCatalogGap(t *testing.T) {
	now := time.Date(2026, time.April, 9, 6, 0, 0, 0, time.UTC)
	descriptor, err := BuildDiscoveryDescriptorWithCatalog(middlewareclient.RuntimeSnapshot{
		Version:         15,
		Timestamp:       now.Add(-2 * time.Second),
		RuntimeRevision: "runtime-rev-15",
		ServicePhase:    "ready",
		ActiveConfig:    "default",
		Instances: middlewareclient.InstancesSnapshot{
			InstanceCount: 0,
			RunningCount:  0,
		},
	}, discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        50305,
		Protocol:    "http",
		Status:      "running",
	}, now, 30*time.Second, toolcatalog.Contract{
		ID:                      "tool_catalog/global",
		Version:                 toolcatalog.ContractVersion,
		Source:                  "go_control_plane",
		CapabilityType:          toolcatalog.CapabilityTypeToolCatalog,
		RuntimeTarget:           "qgis",
		Availability:            toolcatalog.AvailabilityDegraded,
		MissingDependencyReason: toolcatalog.MissingReasonInteractiveDisconnected,
		DegradationStatus:       toolcatalog.DegradationStatusMissingDependency,
		Policy:                  &toolcatalog.Policy{Scope: "global", ConfigName: "default"},
		Items: []toolcatalog.Capability{{
			ID:                      "pyqgis_get_context",
			CapabilityType:          toolcatalog.CapabilityTypeTool,
			RuntimeTarget:           "qgis",
			Availability:            toolcatalog.AvailabilityUnavailable,
			MissingDependencyReason: toolcatalog.MissingReasonInteractiveDisconnected,
			DegradationStatus:       toolcatalog.DegradationStatusMissingDependency,
		}},
	})
	if err != nil {
		t.Fatalf("BuildDiscoveryDescriptorWithCatalog returned error: %v", err)
	}
	if descriptor.Readiness != DiscoveryReadinessReady {
		t.Fatalf("expected ready descriptor when only interactive instance is absent, got %q", descriptor.Readiness)
	}
	if descriptor.Degraded {
		t.Fatalf("expected degraded=false when only interactive instance is absent, got %#v", descriptor)
	}
	if descriptor.ReasonCode != "" {
		t.Fatalf("expected empty reason_code, got %q", descriptor.ReasonCode)
	}
	if descriptor.RetryHints.Retryable {
		t.Fatalf("expected non-retryable ready descriptor, got %#v", descriptor.RetryHints)
	}
}

func TestBackendRestartReconnectOrder(t *testing.T) {
	now := time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC)
	entry := discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        3000,
		Protocol:    "http",
		Status:      "running",
	}
	older, err := BuildDiscoveryDescriptor(middlewareclient.RuntimeSnapshot{
		Version:   3,
		Timestamp: now.Add(-4 * time.Second),
		Instances: middlewareclient.InstancesSnapshot{ActiveID: "qgis-old"},
	}, entry, now, 30*time.Second)
	if err != nil {
		t.Fatalf("older descriptor build failed: %v", err)
	}
	newer, err := BuildDiscoveryDescriptor(middlewareclient.RuntimeSnapshot{
		Version:   4,
		Timestamp: now.Add(-1 * time.Second),
		Instances: middlewareclient.InstancesSnapshot{ActiveID: "qgis-new"},
	}, entry, now, 30*time.Second)
	if err != nil {
		t.Fatalf("newer descriptor build failed: %v", err)
	}
	if newer.RestartSequence <= older.RestartSequence {
		t.Fatalf("expected newer restart_sequence > older, got newer=%d older=%d", newer.RestartSequence, older.RestartSequence)
	}
	if newer.ObservedAt <= older.ObservedAt {
		t.Fatalf("expected newer observed_at > older, got newer=%q older=%q", newer.ObservedAt, older.ObservedAt)
	}
}

func TestDescriptorExpiry(t *testing.T) {
	now := time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC)
	_, err := BuildDiscoveryDescriptor(middlewareclient.RuntimeSnapshot{
		Version:   2,
		Timestamp: now.Add(-31 * time.Second),
	}, discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        3000,
		Protocol:    "http",
		Status:      "running",
	}, now, 30*time.Second)
	if err == nil {
		t.Fatal("expected expired discovery descriptor error")
	}
}

func TestDiscoveryDescriptor_PublishesRestartSequence(t *testing.T) {
	now := time.Date(2026, time.April, 8, 4, 30, 0, 0, time.UTC)
	descriptor, err := BuildDiscoveryDescriptor(middlewareclient.RuntimeSnapshot{
		Version:         11,
		Timestamp:       now.Add(-3 * time.Second),
		RuntimeRevision: "runtime-rev-11",
		ServicePhase:    "ready",
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "qgis-main",
			InstanceCount: 1,
			RunningCount:  1,
		},
	}, discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        50301,
		Protocol:    "http",
		Status:      "running",
	}, now, 30*time.Second)
	if err != nil {
		t.Fatalf("BuildDiscoveryDescriptor returned error: %v", err)
	}
	if descriptor.RestartSequence != 11 {
		t.Fatalf("unexpected restart_sequence: %d", descriptor.RestartSequence)
	}
	if descriptor.RuntimeRevision != "runtime-rev-11" {
		t.Fatalf("unexpected runtime_revision: %q", descriptor.RuntimeRevision)
	}
	if descriptor.Readiness != DiscoveryReadinessReady {
		t.Fatalf("unexpected readiness: %q", descriptor.Readiness)
	}
}

func TestDiscoveryDescriptor_StalePortForcesRefresh(t *testing.T) {
	now := time.Date(2026, time.April, 8, 4, 45, 0, 0, time.UTC)
	descriptor, err := BuildDiscoveryDescriptor(middlewareclient.RuntimeSnapshot{
		Version:         12,
		Timestamp:       now.Add(-1 * time.Second),
		RuntimeRevision: "runtime-rev-12",
		ServicePhase:    "running",
		Stale:           true,
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "qgis-stale",
			InstanceCount: 1,
			RunningCount:  1,
		},
	}, discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        50302,
		Protocol:    "http",
		Status:      "running",
	}, now, 30*time.Second)
	if err != nil {
		t.Fatalf("BuildDiscoveryDescriptor returned error: %v", err)
	}
	if descriptor.Readiness != DiscoveryReadinessDegraded {
		t.Fatalf("unexpected readiness: %q", descriptor.Readiness)
	}
	if !descriptor.Degraded {
		t.Fatal("expected stale descriptor to be degraded")
	}
	if descriptor.ReasonCode != "RUNTIME_STALE" {
		t.Fatalf("unexpected reason_code: %q", descriptor.ReasonCode)
	}
	if !descriptor.RetryHints.Retryable {
		t.Fatalf("expected stale descriptor retryable=true, got %#v", descriptor.RetryHints)
	}
	if descriptor.RetryHints.AfterMS <= 0 {
		t.Fatalf("expected stale descriptor after_ms > 0, got %#v", descriptor.RetryHints)
	}
}
