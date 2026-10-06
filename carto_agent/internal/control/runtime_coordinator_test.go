package control

import (
	"context"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/settings"
)

type stubCoordinatorRuntimeSource struct {
	snapshot middlewareclient.RuntimeSnapshot
}

type stubCoordinatorDiagnostics struct {
	runtimeRevision          string
	runtimeActiveConfigName  string
	runtimeModeIssues        map[string]settings.SettingsModeIssue
	toolSchemaRevision       string
	toolAvailabilityRevision string
	lastRuntimeRebuildReason string
	lastRuntimeRebuildAt     time.Time
	plannerReadyReason       string
	activeInstanceID         string
}

func (s stubCoordinatorRuntimeSource) GetRuntimeState() middlewareclient.RuntimeState {
	return middlewareclient.RuntimeState{Snapshot: &s.snapshot}
}

func (s stubCoordinatorRuntimeSource) GetRuntimeSnapshot() middlewareclient.RuntimeSnapshot {
	return s.snapshot
}

func (s stubCoordinatorDiagnostics) GetRuntimeRevision() string {
	return s.runtimeRevision
}

func (s stubCoordinatorDiagnostics) GetRuntimeActiveConfigName() string {
	return s.runtimeActiveConfigName
}

func (s stubCoordinatorDiagnostics) GetRuntimeModeIssues() map[string]settings.SettingsModeIssue {
	return s.runtimeModeIssues
}

func (s stubCoordinatorDiagnostics) GetToolSchemaRevision() string {
	return s.toolSchemaRevision
}

func (s stubCoordinatorDiagnostics) GetToolAvailabilityRevision() string {
	return s.toolAvailabilityRevision
}

func (s stubCoordinatorDiagnostics) GetLastRuntimeRebuildReason() string {
	return s.lastRuntimeRebuildReason
}

func (s stubCoordinatorDiagnostics) GetLastRuntimeRebuildAt() time.Time {
	return s.lastRuntimeRebuildAt
}

func (s stubCoordinatorDiagnostics) GetPlannerReadyReason() string {
	return s.plannerReadyReason
}

func (s stubCoordinatorDiagnostics) GetActiveInstanceID() string {
	return s.activeInstanceID
}

func TestRuntimeCoordinatorSnapshot(t *testing.T) {
	coordinator := NewRuntimeCoordinator(RuntimeCoordinatorDeps{
		RuntimeSource: stubCoordinatorRuntimeSource{snapshot: middlewareclient.RuntimeSnapshot{
			Version:             9,
			ToolCatalogRevision: "catalog-rev-9",
			Services: map[string]middlewareclient.ServiceStatus{
				"agent": {ID: "agent", Status: "running", Source: "sidecar", Enabled: true},
			},
			Instances: middlewareclient.InstancesSnapshot{
				Items: []*discovery.QGISInstance{{ID: "inst-read-model", Status: "running"}},
			},
			Stale: true,
		}},
		RuntimeDiagnostics: stubCoordinatorDiagnostics{
			runtimeRevision:          "runtime-rev-9",
			runtimeActiveConfigName:  "default",
			runtimeModeIssues:        map[string]settings.SettingsModeIssue{"plan": {Code: "PLANNER_BLOCKED", Message: "planner key missing"}},
			toolSchemaRevision:       "schema-rev-9",
			toolAvailabilityRevision: "availability-rev-9",
			lastRuntimeRebuildReason: "tool_schema_changed:schema-rev-9",
			lastRuntimeRebuildAt:     time.Date(2026, time.April, 4, 10, 0, 0, 0, time.UTC),
			activeInstanceID:         "inst-read-model",
		},
	})

	snapshot := coordinator.Snapshot(context.Background())
	if got := snapshot.RuntimeRevision; got != "runtime-rev-9" {
		t.Fatalf("unexpected runtime revision: %q", got)
	}
	if got := snapshot.ConfigRevision; got != "runtime-rev-9" {
		t.Fatalf("unexpected config revision: %q", got)
	}
	if got := snapshot.RuntimeActiveAgentConfig; got != "default" {
		t.Fatalf("unexpected runtime active config: %q", got)
	}
	if got := snapshot.ActiveConfig; got != "default" {
		t.Fatalf("unexpected active config: %q", got)
	}
	if got := snapshot.ToolRevisions.Schema; got != "schema-rev-9" {
		t.Fatalf("unexpected tool schema revision projection: %q", got)
	}
	if got := snapshot.ToolRevisions.Availability; got != "availability-rev-9" {
		t.Fatalf("unexpected tool availability revision projection: %q", got)
	}
	if got := snapshot.ToolRevisions.Catalog; got != "catalog-rev-9" {
		t.Fatalf("unexpected tool catalog revision projection: %q", got)
	}
	if got := snapshot.ModeReadiness["plan"]; got.Ready || got.Code != "PLANNER_BLOCKED" {
		t.Fatalf("unexpected plan readiness: %#v", got)
	}
	if got := snapshot.ServicePhase; got != "degraded" {
		t.Fatalf("unexpected service phase: %q", got)
	}
	if got := snapshot.Instances.ActiveID; got != "inst-read-model" {
		t.Fatalf("unexpected active instance id: %q", got)
	}
	if !snapshot.Stale {
		t.Fatal("expected stale flag to survive unified read model")
	}
	if got := snapshot.LastRebuildReason; got != "tool_schema_changed:schema-rev-9" {
		t.Fatalf("unexpected rebuild reason: %q", got)
	}
	if snapshot.LastRebuildAt.IsZero() {
		t.Fatal("expected rebuild timestamp from diagnostics")
	}
}

func TestMergeLiveInstances(t *testing.T) {
	registry := discovery.NewInstanceRegistry()
	if err := registry.Register(discovery.RegisterRequest{
		InstanceID:  "inst-merged",
		PID:         2003,
		QGISVersion: "3.40",
		ProjectPath: "",
		Source:      discovery.SourceReverseWS,
	}); err != nil {
		t.Fatalf("register instance failed: %v", err)
	}
	if err := registry.HandleEvent("inst-merged", discovery.EventRequest{
		EventType: "project_opened",
		Data:      map[string]any{"project_path": "/tmp/live.qgz"},
	}); err != nil {
		t.Fatalf("handle event failed: %v", err)
	}

	coordinator := NewRuntimeCoordinator(RuntimeCoordinatorDeps{
		RuntimeSource: stubCoordinatorRuntimeSource{snapshot: middlewareclient.RuntimeSnapshot{
			Version:      2,
			Timestamp:    time.Date(2026, time.March, 17, 22, 7, 28, 0, time.UTC),
			ServicePhase: "waiting_plugin",
			Services: map[string]middlewareclient.ServiceStatus{
				"agent": {ID: "agent", Status: "running", Source: "sidecar", Enabled: true},
			},
			Instances: middlewareclient.InstancesSnapshot{
				Items: []*discovery.QGISInstance{{
					ID:          "inst-merged",
					PID:         2003,
					QGISVersion: "3.40",
					ProjectPath: "",
					Status:      "running",
					Services:    map[string]discovery.ServiceInfo{},
					CreatedAt:   time.Now(),
					UpdatedAt:   time.Now(),
					LastHeart:   time.Now(),
					Source:      discovery.SourceReverseWS,
				}},
				ActiveID:      "inst-merged",
				InstanceCount: 1,
				RunningCount:  1,
			},
		}},
		InstanceRegistryProvider: func() *discovery.InstanceRegistry {
			return registry
		},
	})

	snapshot := coordinator.Snapshot(context.Background())
	if snapshot.Instances.InstanceCount != 1 {
		t.Fatalf("expected one merged instance, got %d", snapshot.Instances.InstanceCount)
	}
	if snapshot.Instances.RunningCount != 1 {
		t.Fatalf("expected one running instance, got %d", snapshot.Instances.RunningCount)
	}
	if got := snapshot.Instances.ActiveID; got != "inst-merged" {
		t.Fatalf("unexpected active instance id: %q", got)
	}
	if len(snapshot.Instances.Items) != 1 {
		t.Fatalf("unexpected runtime instances payload: %#v", snapshot.Instances.Items)
	}
	if got := snapshot.Instances.Items[0].ProjectPath; got != "/tmp/live.qgz" {
		t.Fatalf("expected merged project path, got %q", got)
	}
	service, ok := snapshot.Services["qgis"]
	if !ok {
		t.Fatalf("expected qgis service projection, got %#v", snapshot.Services)
	}
	if got := service.Status; got != "running" {
		t.Fatalf("unexpected qgis status: %q", got)
	}
}

func TestRuntimeCoordinatorSnapshotDoesNotWaitForPluginWhenAgentIsReady(t *testing.T) {
	coordinator := NewRuntimeCoordinator(RuntimeCoordinatorDeps{
		RuntimeSource: stubCoordinatorRuntimeSource{snapshot: middlewareclient.RuntimeSnapshot{
			Version: 5,
			Services: map[string]middlewareclient.ServiceStatus{
				"agent": {ID: "agent", Status: "running", Source: "sidecar", Enabled: true},
			},
			Instances: middlewareclient.InstancesSnapshot{
				InstanceCount: 0,
				RunningCount:  0,
			},
		}},
	})

	snapshot := coordinator.Snapshot(context.Background())
	if got := snapshot.ServicePhase; got != "ready" {
		t.Fatalf("unexpected service phase: %q", got)
	}
}

func TestRuntimeCoordinatorSnapshotRefreshesTimestampAndDropsLegacyHeadlessCompute(t *testing.T) {
	legacyTimestamp := time.Date(2026, time.March, 17, 22, 7, 28, 0, time.UTC)
	coordinator := NewRuntimeCoordinator(RuntimeCoordinatorDeps{
		RuntimeSource: stubCoordinatorRuntimeSource{snapshot: middlewareclient.RuntimeSnapshot{
			Timestamp: legacyTimestamp,
			Services: map[string]middlewareclient.ServiceStatus{
				"agent":            {ID: "agent", Status: "running", Source: "sidecar", Enabled: true},
				"cli":              {ID: "cli", Status: "running", Source: "local", Enabled: true},
				"headless_compute": {ID: "headless_compute", Status: "failed", Source: "legacy", Enabled: true},
			},
		}},
	})

	before := time.Now().Add(-2 * time.Second)
	snapshot := coordinator.Snapshot(context.Background())
	after := time.Now().Add(2 * time.Second)

	if !snapshot.Timestamp.After(legacyTimestamp) {
		t.Fatalf("expected refreshed timestamp after legacy value, got %s", snapshot.Timestamp)
	}
	if snapshot.Timestamp.Before(before) || snapshot.Timestamp.After(after) {
		t.Fatalf("expected refreshed timestamp near now, got %s (window %s - %s)", snapshot.Timestamp, before, after)
	}
	if _, exists := snapshot.Services["headless_compute"]; exists {
		t.Fatalf("expected legacy headless_compute removed from public snapshot, got %#v", snapshot.Services)
	}
	if service, ok := snapshot.Services["cli"]; !ok || service.Status != "running" {
		t.Fatalf("expected cli service preserved, got %#v", snapshot.Services["cli"])
	}
}

func TestRuntimeCoordinatorSnapshotBackfillsDiscoveryRevisionAndToolRevisions(t *testing.T) {
	coordinator := NewRuntimeCoordinator(RuntimeCoordinatorDeps{
		RuntimeSource: stubCoordinatorRuntimeSource{snapshot: middlewareclient.RuntimeSnapshot{
			Version:             4,
			ToolCatalogRevision: "catalog-rev-4",
			Services: map[string]middlewareclient.ServiceStatus{
				"agent": {ID: "agent", Status: "running", Source: "sidecar", Enabled: true},
			},
			Instances: middlewareclient.InstancesSnapshot{
				Items: []*discovery.QGISInstance{{ID: "inst-revision", Status: "running"}},
			},
		}},
		RuntimeDiagnostics: stubCoordinatorDiagnostics{
			runtimeRevision:          "runtime-rev-4",
			toolSchemaRevision:       "schema-rev-4",
			toolAvailabilityRevision: "availability-rev-4",
		},
	})

	snapshot := coordinator.Snapshot(context.Background())
	if got := snapshot.DiscoveryRevision; got != "runtime-rev-4" {
		t.Fatalf("expected discovery revision fallback from runtime revision, got %q", got)
	}
	if got := snapshot.ToolRevisions.Catalog; got != "catalog-rev-4" {
		t.Fatalf("expected catalog tool revision mirror, got %q", got)
	}
	if got := snapshot.ToolRevisions.Schema; got != "schema-rev-4" {
		t.Fatalf("expected schema tool revision mirror, got %q", got)
	}
	if got := snapshot.ToolRevisions.Availability; got != "availability-rev-4" {
		t.Fatalf("expected availability tool revision mirror, got %q", got)
	}
}
