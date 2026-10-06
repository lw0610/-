package websocket

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/skill"
)

type stubCapabilityRefreshRequester struct {
	mu      sync.Mutex
	reasons []string
}

func (s *stubCapabilityRefreshRequester) RequestRefresh(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reasons = append(s.reasons, reason)
}

func (s *stubCapabilityRefreshRequester) Reasons() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reasons...)
}

func TestSummarizeRuntimeInstanceDeltaReportsRemovedInstancesAndActiveChange(t *testing.T) {
	prev := &middlewareclient.RuntimeSnapshot{
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "inst-a",
			InstanceCount: 2,
			RunningCount:  2,
			Items: []*discovery.QGISInstance{
				{ID: "inst-a", ProjectPath: "/tmp/a.qgz", Status: "running"},
				{ID: "inst-b", ProjectPath: "/tmp/b.qgz", Status: "running"},
			},
		},
	}
	next := middlewareclient.RuntimeSnapshot{
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "inst-b",
			InstanceCount: 1,
			RunningCount:  1,
			Items:         []*discovery.QGISInstance{{ID: "inst-b", ProjectPath: "/tmp/b.qgz", Status: "running"}},
		},
	}

	summary, ok := summarizeRuntimeInstanceDelta(prev, next, "runtime_sse")
	if !ok {
		t.Fatal("expected runtime instance delta summary")
	}
	if summary.Source != "runtime_sse" {
		t.Fatalf("unexpected source: %q", summary.Source)
	}
	if summary.PrevActiveID != "inst-a" || summary.NextActiveID != "inst-b" {
		t.Fatalf("unexpected active ids: %+v", summary)
	}
	if summary.PrevRunningCount != 2 || summary.NextRunningCount != 1 {
		t.Fatalf("unexpected running counts: %+v", summary)
	}
	if len(summary.RemovedInstanceIDs) != 1 || summary.RemovedInstanceIDs[0] != "inst-a" {
		t.Fatalf("unexpected removed instances: %v", summary.RemovedInstanceIDs)
	}
}

func TestRevisionBroadcast(t *testing.T) {
	cache := NewRuntimeCache()
	base := middlewareclient.RuntimeSnapshot{
		Version:                  1,
		ConfigRevision:           "config-rev-1",
		RuntimeRevision:          "runtime-rev-1",
		ToolCatalogRevision:      "catalog-rev-1",
		ToolSchemaRevision:       "schema-rev-1",
		ToolAvailabilityRevision: "availability-rev-1",
		ToolRevisions: middlewareclient.ToolRevisions{
			Catalog:      "catalog-rev-1",
			Schema:       "schema-rev-1",
			Availability: "availability-rev-1",
		},
		Services: map[string]middlewareclient.ServiceStatus{
			"agent": {Status: "running", Enabled: true},
		},
		Instances: middlewareclient.InstancesSnapshot{Items: []*discovery.QGISInstance{}},
	}

	if _, changed := cache.ApplySnapshot(base); !changed {
		t.Fatal("expected initial snapshot to seed runtime cache")
	}

	revisionJump := base
	revisionJump.RuntimeRevision = "runtime-rev-2"
	revisionJump.ConfigRevision = "config-rev-2"
	if _, changed := cache.ApplySnapshot(revisionJump); !changed {
		t.Fatal("expected runtime revision jump to trigger runtime snapshot broadcast")
	}
	current := cache.GetSnapshot()
	if current == nil {
		t.Fatal("expected runtime cache snapshot after revision jump")
	}
	if current.RuntimeRevision != "runtime-rev-2" {
		t.Fatalf("expected runtime cache to keep latest runtime revision, got %q", current.RuntimeRevision)
	}
	if current.ConfigRevision != "config-rev-2" {
		t.Fatalf("expected runtime cache to keep latest config revision, got %q", current.ConfigRevision)
	}

	staleSnapshot := revisionJump
	staleSnapshot.Stale = true
	if _, changed := cache.ApplySnapshot(staleSnapshot); !changed {
		t.Fatal("expected stale flag transition to trigger runtime snapshot broadcast")
	}

	toolSchemaJump := staleSnapshot
	toolSchemaJump.Stale = false
	toolSchemaJump.ToolSchemaRevision = "schema-rev-2"
	toolSchemaJump.ToolRevisions.Schema = "schema-rev-2"
	if _, changed := cache.ApplySnapshot(toolSchemaJump); !changed {
		t.Fatal("expected tool schema revision jump to trigger runtime snapshot broadcast")
	}
}

func TestSnapshotInvalidation(t *testing.T) {
	cache := NewRuntimeCache()
	base := middlewareclient.RuntimeSnapshot{
		Version:             1,
		RuntimeRevision:     "runtime-rev-1",
		ConfigRevision:      "config-rev-1",
		ToolCatalogRevision: "catalog-rev-1",
		Services: map[string]middlewareclient.ServiceStatus{
			"agent": {Status: "running", Enabled: true},
		},
		Instances: middlewareclient.InstancesSnapshot{Items: []*discovery.QGISInstance{}},
	}

	cache.ApplySnapshot(base)
	staleSnapshot := base
	staleSnapshot.Stale = true

	if _, changed := cache.ApplySnapshot(staleSnapshot); !changed {
		t.Fatal("expected stale snapshot transition to invalidate cached runtime snapshot")
	}
	current := cache.GetSnapshot()
	if current == nil {
		t.Fatal("expected runtime cache snapshot after stale invalidation")
	}
	if !current.Stale {
		t.Fatal("expected runtime cache to retain stale=true after invalidation snapshot")
	}
}

func TestForcedRefreshAfterRevisionJump(t *testing.T) {
	cache := NewRuntimeCache()
	base := middlewareclient.RuntimeSnapshot{
		Version:                  1,
		RuntimeRevision:          "runtime-rev-1",
		ConfigRevision:           "config-rev-1",
		ToolCatalogRevision:      "catalog-rev-1",
		ToolSchemaRevision:       "schema-rev-1",
		ToolAvailabilityRevision: "availability-rev-1",
		ToolRevisions: middlewareclient.ToolRevisions{
			Catalog:      "catalog-rev-1",
			Schema:       "schema-rev-1",
			Availability: "availability-rev-1",
		},
		Services: map[string]middlewareclient.ServiceStatus{
			"agent": {Status: "running", Enabled: true},
		},
		Instances: middlewareclient.InstancesSnapshot{Items: []*discovery.QGISInstance{}},
	}

	cache.ApplySnapshot(base)
	revisionJump := base
	revisionJump.ToolSchemaRevision = "schema-rev-2"
	revisionJump.ToolRevisions.Schema = "schema-rev-2"

	if _, changed := cache.ApplySnapshot(revisionJump); !changed {
		t.Fatal("expected tool schema revision jump to require forced refresh broadcast")
	}
	current := cache.GetSnapshot()
	if current == nil {
		t.Fatal("expected runtime cache snapshot after schema revision jump")
	}
	if current.ToolSchemaRevision != "schema-rev-2" {
		t.Fatalf("expected runtime cache to keep latest schema revision, got %q", current.ToolSchemaRevision)
	}
}

func TestRevisionBroadcastTracksAugmentedRuntimeState(t *testing.T) {
	cache := NewRuntimeCache()
	base := middlewareclient.RuntimeSnapshot{
		Version:             1,
		ConfigRevision:      "config-rev-1",
		DiscoveryRevision:   "discovery-rev-1",
		RuntimeRevision:     "runtime-rev-1",
		ServicePhase:        "ready",
		LastRebuildReason:   "baseline",
		LastRebuildAt:       time.Date(2026, time.April, 7, 10, 0, 0, 0, time.UTC),
		ModeIssues:          map[string]middlewareclient.ModeIssue{},
		ModeReadiness:       map[string]middlewareclient.ModeReadiness{"plan": {Enabled: true, Ready: true}},
		Services:            map[string]middlewareclient.ServiceStatus{"agent": {Status: "running", Enabled: true}},
		Instances:           middlewareclient.InstancesSnapshot{Items: []*discovery.QGISInstance{}},
		ToolCatalogRevision: "catalog-rev-1",
	}

	if _, changed := cache.ApplySnapshot(base); !changed {
		t.Fatal("expected initial snapshot to seed runtime cache")
	}

	rebuildChange := base
	rebuildChange.LastRebuildReason = "tool_schema_changed:schema-rev-2"
	rebuildChange.LastRebuildAt = base.LastRebuildAt.Add(5 * time.Minute)
	if _, changed := cache.ApplySnapshot(rebuildChange); !changed {
		t.Fatal("expected rebuild metadata change to trigger runtime snapshot broadcast")
	}

	phaseChange := rebuildChange
	phaseChange.ServicePhase = "degraded"
	if _, changed := cache.ApplySnapshot(phaseChange); !changed {
		t.Fatal("expected service phase change to trigger runtime snapshot broadcast")
	}

	modeChange := phaseChange
	modeChange.ModeIssues = map[string]middlewareclient.ModeIssue{"plan": {Code: "PLANNER_BLOCKED", Message: "planner blocked"}}
	modeChange.ModeReadiness = map[string]middlewareclient.ModeReadiness{"plan": {Enabled: true, Ready: false, Code: "PLANNER_BLOCKED", Message: "planner blocked"}}
	if _, changed := cache.ApplySnapshot(modeChange); !changed {
		t.Fatal("expected mode readiness change to trigger runtime snapshot broadcast")
	}

	if _, changed := cache.ApplySnapshot(modeChange); changed {
		t.Fatal("expected identical augmented runtime state not to rebroadcast")
	}
}

func TestBroadcastRuntimeSnapshotAugmentsRevisionFields(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.runtime = &agentruntime.Runtime{
		Revision: "runtime-rev-15",
		ModeIssues: map[string]agentruntime.ModeIssue{
			"plan": {Code: "PLANNER_BLOCKED", Message: "planner blocked"},
		},
	}
	server.toolSchemaRevision = "schema-rev-15"
	server.toolAvailabilityRevision = "availability-rev-15"
	server.lastRuntimeRebuildReason = "tool_schema_changed:schema-rev-15"
	server.lastRuntimeRebuildAt = time.Date(2026, time.April, 7, 12, 0, 0, 0, time.UTC)

	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	server.clients[serverConn] = true
	server.connWriteMu[serverConn] = &sync.Mutex{}

	server.broadcastRuntimeSnapshot(middlewareclient.RuntimeSnapshot{
		Version:             15,
		ToolCatalogRevision: "catalog-rev-15",
		Services: map[string]middlewareclient.ServiceStatus{
			"agent": {ID: "agent", Status: "running", Enabled: true},
		},
		Instances: middlewareclient.InstancesSnapshot{Items: []*discovery.QGISInstance{}},
	})

	messages := readMessagesUntilTimeout(t, clientConn)
	if len(messages) == 0 {
		t.Fatal("expected runtime snapshot broadcast message")
	}
	var payload map[string]any
	for _, message := range messages {
		if message.Type == MsgTypeRuntimeSnapshot {
			payload = message.Data
			break
		}
	}
	if payload == nil {
		t.Fatalf("expected runtime_snapshot payload, got %#v", messages)
	}
	if got, _ := payload["runtime_revision"].(string); got != "runtime-rev-15" {
		t.Fatalf("expected runtime_revision to be augmented, got %#v", payload["runtime_revision"])
	}
	if got, _ := payload["config_revision"].(string); got != "runtime-rev-15" {
		t.Fatalf("expected config_revision fallback from runtime revision, got %#v", payload["config_revision"])
	}
	if got, _ := payload["discovery_revision"].(string); got != "runtime-rev-15" {
		t.Fatalf("expected discovery_revision fallback from runtime revision, got %#v", payload["discovery_revision"])
	}
	if got, _ := payload["service_phase"].(string); got != "degraded" {
		t.Fatalf("expected service_phase derived from mode issues, got %#v", payload["service_phase"])
	}
	if got, _ := payload["last_rebuild_reason"].(string); got != "tool_schema_changed:schema-rev-15" {
		t.Fatalf("expected rebuild reason in payload, got %#v", payload["last_rebuild_reason"])
	}
	toolRevisions, _ := payload["tool_revisions"].(map[string]any)
	if got, _ := toolRevisions["catalog"].(string); got != "catalog-rev-15" {
		t.Fatalf("expected catalog revision in tool_revisions, got %#v", toolRevisions)
	}
	if got, _ := toolRevisions["schema"].(string); got != "schema-rev-15" {
		t.Fatalf("expected schema revision in tool_revisions, got %#v", toolRevisions)
	}
	if got, _ := toolRevisions["availability"].(string); got != "availability-rev-15" {
		t.Fatalf("expected availability revision in tool_revisions, got %#v", toolRevisions)
	}
	modeReadiness, _ := payload["mode_readiness"].(map[string]any)
	planReadiness, _ := modeReadiness["plan"].(map[string]any)
	if ready, _ := planReadiness["ready"].(bool); ready {
		t.Fatalf("expected plan readiness false in payload, got %#v", planReadiness)
	}
}

func TestSummarizeRuntimeInstanceDeltaSkipsNonInstanceChanges(t *testing.T) {
	prev := &middlewareclient.RuntimeSnapshot{
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "inst-a",
			InstanceCount: 1,
			RunningCount:  1,
			Items:         []*discovery.QGISInstance{{ID: "inst-a", ProjectPath: "/tmp/a.qgz", Status: "running"}},
		},
	}
	next := middlewareclient.RuntimeSnapshot{
		ToolCatalogRevision: "rev-2",
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "inst-a",
			InstanceCount: 1,
			RunningCount:  1,
			Items:         []*discovery.QGISInstance{{ID: "inst-a", ProjectPath: "/tmp/a.qgz", Status: "running"}},
		},
	}

	if _, ok := summarizeRuntimeInstanceDelta(prev, next, "runtime_poll"); ok {
		t.Fatal("expected no instance delta summary when only tool revisions change")
	}
}

func TestRefreshMiddlewareToolCatalogTriggersReloaderAndUpdatesAggregator(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.SetToolCatalogProvider(func(context.Context) ([]middlewareclient.ToolCatalogItem, error) {
		return []middlewareclient.ToolCatalogItem{
			{
				Name:        "buffer",
				ProviderID:  skill.RuntimeIDCLI,
				Source:      skill.RuntimeIDCLI,
				Backend:     skill.RuntimeIDCLI,
				Description: "buffer tool",
				InputSchema: map[string]any{"type": "object"},
			},
			{
				Name:        "open_project",
				ProviderID:  skill.RuntimeIDQGIS,
				Source:      skill.RuntimeIDQGIS,
				Backend:     skill.RuntimeIDQGIS,
				Description: "open project",
				InputSchema: map[string]any{"type": "object"},
				InstanceIDs: []string{"inst-a"},
			},
		}, nil
	})
	capabilityRefresher := &stubCapabilityRefreshRequester{}
	server.capabilityRefresher = capabilityRefresher

	reloaderCalls := 0
	server.SetMiddlewareToolReloader(func(context.Context) error {
		reloaderCalls++
		return nil
	})

	server.refreshMiddlewareToolCatalog("rev-2", "test")

	if reloaderCalls != 1 {
		t.Fatalf("expected reloader to be called once, got %d", reloaderCalls)
	}

	snapshot := server.GetToolAggregatorSnapshot()
	if len(snapshot.Compute) != 1 || snapshot.Compute[0].Name != "buffer" {
		t.Fatalf("unexpected compute snapshot: %+v", snapshot.Compute)
	}
	if len(snapshot.Interactive) != 1 || snapshot.Interactive[0].Name != "open_project" {
		t.Fatalf("unexpected interactive snapshot: %+v", snapshot.Interactive)
	}
	if got := server.runtimeCache.ToolCatalogCache(); len(got) != 2 {
		t.Fatalf("unexpected cached tool count: %d", len(got))
	}
	if got := server.GetToolSchemaRevision(); got == "" {
		t.Fatal("expected tool schema revision to be recorded")
	}
	if reasons := capabilityRefresher.Reasons(); len(reasons) != 1 || reasons[0] != "runtime_tool_catalog:test" {
		t.Fatalf("expected capability refresh request after catalog refresh, got %#v", reasons)
	}
}

func TestRefreshMiddlewareToolCatalogUsesLocalToolCatalogProvider(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.SetToolCatalogProvider(func(context.Context) ([]middlewareclient.ToolCatalogItem, error) {
		return []middlewareclient.ToolCatalogItem{
			{
				Name:        "buffer",
				ProviderID:  skill.RuntimeIDCLI,
				Source:      skill.RuntimeIDCLI,
				Backend:     skill.RuntimeIDCLI,
				InputSchema: map[string]any{"type": "object"},
			},
			{
				Name:        "open_project",
				ProviderID:  skill.RuntimeIDQGIS,
				Source:      skill.RuntimeIDQGIS,
				Backend:     skill.RuntimeIDQGIS,
				InputSchema: map[string]any{"type": "object"},
			},
		}, nil
	})

	reloaderCalls := 0
	server.SetMiddlewareToolReloader(func(context.Context) error {
		reloaderCalls++
		return nil
	})

	server.refreshMiddlewareToolCatalog("rev-local", "local")

	if reloaderCalls != 1 {
		t.Fatalf("expected reloader to be called once, got %d", reloaderCalls)
	}
	cache := server.runtimeCache.ToolCatalogCache()
	if len(cache) != 2 {
		t.Fatalf("unexpected cached tool count: %d", len(cache))
	}
	aggregator := server.GetToolAggregatorSnapshot()
	if len(aggregator.Compute) != 1 || aggregator.Compute[0].Name != "buffer" {
		t.Fatalf("unexpected compute snapshot: %+v", aggregator.Compute)
	}
	if len(aggregator.Interactive) != 1 || aggregator.Interactive[0].Name != "open_project" {
		t.Fatalf("unexpected interactive snapshot: %+v", aggregator.Interactive)
	}
}

func TestPrimeRuntimeStateSeedsSnapshotAndCatalog(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.PrimeRuntimeState(middlewareclient.RuntimeSnapshot{
		Version:             7,
		ToolCatalogRevision: "rev-7",
		Services: map[string]middlewareclient.ServiceStatus{
			"middleware": {Status: "running", Enabled: true},
		},
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "inst-a",
			InstanceCount: 1,
			RunningCount:  1,
			Items:         []*discovery.QGISInstance{{ID: "inst-a", Status: "running"}},
		},
	}, []middlewareclient.ToolCatalogItem{{
		Name:        "buffer",
		ProviderID:  skill.RuntimeIDCLI,
		Source:      skill.RuntimeIDCLI,
		Backend:     skill.RuntimeIDCLI,
		InputSchema: map[string]any{"type": "object"},
	}})

	snapshot := server.GetRuntimeSnapshot()
	if snapshot.Version != 7 {
		t.Fatalf("unexpected version: %d", snapshot.Version)
	}
	if snapshot.Instances.ActiveID != "inst-a" {
		t.Fatalf("unexpected active id: %s", snapshot.Instances.ActiveID)
	}
	if got := server.GetToolSchemaRevision(); got == "" {
		t.Fatal("expected schema revision after priming")
	}
	cache := server.runtimeCache.ToolCatalogCache()
	if len(cache) != 1 || cache[0].Name != "buffer" {
		t.Fatalf("unexpected tool cache: %#v", cache)
	}
	aggregator := server.GetToolAggregatorSnapshot()
	if len(aggregator.Compute) != 1 || aggregator.Compute[0].Name != "buffer" {
		t.Fatalf("unexpected compute aggregator: %#v", aggregator.Compute)
	}
}

func TestPrimeRuntimeStatePrefersCapabilitySnapshotRevisions(t *testing.T) {
	server := NewDeepServer(0, nil)
	store := capability.NewSnapshotStore()
	store.Set(&capability.CapabilitySnapshot{SchemaRevision: "schema-from-snapshot", AvailabilityRevision: "availability-from-snapshot"})
	server.SetCapabilitySnapshotRuntime(store, nil, "primary")
	server.PrimeRuntimeState(middlewareclient.RuntimeSnapshot{Version: 1, ToolCatalogRevision: "rev-1", Instances: middlewareclient.InstancesSnapshot{Items: []*discovery.QGISInstance{}}}, []middlewareclient.ToolCatalogItem{{Name: "buffer", ProviderID: skill.RuntimeIDCLI, Source: skill.RuntimeIDCLI, Backend: skill.RuntimeIDCLI, InputSchema: map[string]any{"type": "object"}}})
	if got := server.GetToolSchemaRevision(); got != "schema-from-snapshot" {
		t.Fatalf("expected snapshot schema revision, got %q", got)
	}
	if got := server.GetToolAvailabilityRevision(); got != "availability-from-snapshot" {
		t.Fatalf("expected snapshot availability revision, got %q", got)
	}
}

func TestUpdateToolAggregatorFromCatalogPrefersCanonicalSourceOverLegacyProvider(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.updateToolAggregatorFromCatalog([]middlewareclient.ToolCatalogItem{
		{
			Name:        "open_project",
			ProviderID:  "legacy_cli_provider",
			Source:      "qgis",
			InputSchema: map[string]any{"type": "object"},
		},
		{
			Name:        "buffer",
			ProviderID:  "qgis_bridge",
			Source:      "cli",
			InputSchema: map[string]any{"type": "object"},
		},
	})

	aggregator := server.GetToolAggregatorSnapshot()
	if len(aggregator.Interactive) != 1 || aggregator.Interactive[0].Name != "open_project" {
		t.Fatalf("expected canonical qgis source to stay interactive, got %+v", aggregator.Interactive)
	}
	if len(aggregator.Compute) != 1 || aggregator.Compute[0].Name != "buffer" {
		t.Fatalf("expected canonical cli source to stay compute, got %+v", aggregator.Compute)
	}
}

func TestUpdateToolAggregatorFromCatalogPrefersBackendOverSourceAndProvider(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.updateToolAggregatorFromCatalog([]middlewareclient.ToolCatalogItem{
		{
			Name:        "open_project",
			Backend:     skill.RuntimeIDQGIS,
			ProviderID:  "legacy_cli_provider",
			Source:      "compute",
			InputSchema: map[string]any{"type": "object"},
		},
		{
			Name:        "buffer",
			Backend:     skill.RuntimeIDCLI,
			ProviderID:  "qgis_bridge",
			Source:      "interactive",
			InputSchema: map[string]any{"type": "object"},
		},
	})

	aggregator := server.GetToolAggregatorSnapshot()
	if len(aggregator.Interactive) != 1 || aggregator.Interactive[0].Name != "open_project" {
		t.Fatalf("expected backend=qgis to win, got %+v", aggregator.Interactive)
	}
	if len(aggregator.Compute) != 1 || aggregator.Compute[0].Name != "buffer" {
		t.Fatalf("expected backend=cli to win, got %+v", aggregator.Compute)
	}
}

func TestUpdateToolAggregatorFromCatalogDoesNotInferCliFromLegacyProviderAlone(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.updateToolAggregatorFromCatalog([]middlewareclient.ToolCatalogItem{
		{
			Name:        "open_project",
			ProviderID:  "legacy_cli_provider",
			Source:      "interactive",
			InputSchema: map[string]any{"type": "object"},
		},
		{
			Name:        "buffer",
			ProviderID:  "qgis_bridge",
			Source:      "compute",
			InputSchema: map[string]any{"type": "object"},
		},
		{
			Name:        "clip",
			ProviderID:  "legacy_cli_provider",
			InputSchema: map[string]any{"type": "object"},
		},
	})

	aggregator := server.GetToolAggregatorSnapshot()
	if len(aggregator.Interactive) != 1 || aggregator.Interactive[0].Name != "open_project" {
		t.Fatalf("expected legacy source=interactive to resolve qgis runtime, got %+v", aggregator.Interactive)
	}
	if len(aggregator.Compute) != 1 {
		t.Fatalf("expected only explicit compute-source tool to resolve cli runtime, got %+v", aggregator.Compute)
	}
	if aggregator.Compute[0].Name != "buffer" {
		t.Fatalf("expected legacy source=compute to resolve cli runtime, got %+v", aggregator.Compute)
	}
	for _, item := range aggregator.Compute {
		if item.Name == "clip" {
			t.Fatalf("did not expect provider-only fallback to resolve cli runtime, got %+v", aggregator.Compute)
		}
	}
}

func TestUpdateToolAggregatorFromCatalogKeepsLegacyComputeAliasUnclassified(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.updateToolAggregatorFromCatalog([]middlewareclient.ToolCatalogItem{
		{
			Name:        "buffer",
			Source:      "legacy_compute",
			InputSchema: map[string]any{"type": "object"},
		},
		{
			Name:        "open_project",
			Source:      "qgis_bridge",
			InputSchema: map[string]any{"type": "object"},
		},
	})

	aggregator := server.GetToolAggregatorSnapshot()
	if len(aggregator.Compute) != 0 {
		t.Fatalf("did not expect legacy source=legacy_compute to normalize to compute, got %+v", aggregator.Compute)
	}
	if len(aggregator.Interactive) != 1 || aggregator.Interactive[0].Name != "open_project" {
		t.Fatalf("expected legacy source=qgis_bridge to normalize to interactive, got %+v", aggregator.Interactive)
	}
}

func TestRefreshMiddlewareToolCatalogSkipsReloaderForAvailabilityOnlyChanges(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.PrimeRuntimeState(middlewareclient.RuntimeSnapshot{}, []middlewareclient.ToolCatalogItem{{
		Name:        "buffer",
		ProviderID:  skill.RuntimeIDCLI,
		Source:      skill.RuntimeIDCLI,
		Backend:     skill.RuntimeIDCLI,
		Available:   true,
		InputSchema: map[string]any{"type": "object"},
	}})

	reloaderCalls := 0
	server.SetToolCatalogProvider(func(context.Context) ([]middlewareclient.ToolCatalogItem, error) {
		return []middlewareclient.ToolCatalogItem{{
			Name:              "buffer",
			ProviderID:        skill.RuntimeIDCLI,
			Source:            skill.RuntimeIDCLI,
			Backend:           skill.RuntimeIDCLI,
			Available:         false,
			UnavailableReason: "compute_catalog_pending",
			InputSchema:       map[string]any{"type": "object"},
		}}, nil
	})
	server.SetMiddlewareToolReloader(func(context.Context) error {
		reloaderCalls++
		return nil
	})

	server.refreshMiddlewareToolCatalog("rev-availability-only", "test")

	if reloaderCalls != 0 {
		t.Fatalf("expected availability-only change to skip reloader, got %d calls", reloaderCalls)
	}
	if got := server.GetToolAvailabilityRevision(); got == "" {
		t.Fatal("expected availability revision to be recorded")
	}
}

func TestGetRuntimeSnapshotMergesLocalRuntimeProjection(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.SetLocalRuntimeProjection(middlewareclient.RuntimeSnapshot{
		Services: map[string]middlewareclient.ServiceStatus{
			"cli": {
				ID:      "cli",
				Status:  "running",
				Source:  "sidecar_local_cli",
				Ready:   true,
				Enabled: true,
			},
		},
	})
	server.PrimeRuntimeState(middlewareclient.RuntimeSnapshot{
		Services: map[string]middlewareclient.ServiceStatus{
			"middleware": {Status: "running", Enabled: true},
		},
	}, nil)

	snapshot := server.GetRuntimeSnapshot()
	if service, ok := snapshot.Services["cli"]; !ok || service.Source != "sidecar_local_cli" || !service.Ready {
		t.Fatalf("unexpected cli service: %#v", service)
	}
}

func TestRemoveLegacyRuntimePaths(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.PrimeRuntimeState(middlewareclient.RuntimeSnapshot{
		Version:             12,
		RuntimeRevision:     "runtime-rev-12",
		ConfigRevision:      "config-rev-12",
		ToolCatalogRevision: "catalog-rev-12",
		Services: map[string]middlewareclient.ServiceStatus{
			"agent":            {ID: "agent", Status: "running", Enabled: true},
			"cli":              {ID: "cli", Status: "running", Enabled: true},
			"headless_compute": {ID: "headless_compute", Status: "failed", Enabled: true},
		},
		Instances: middlewareclient.InstancesSnapshot{Items: []*discovery.QGISInstance{}},
		Stale:     true,
	}, nil)

	snapshot := server.GetRuntimeSnapshot()
	if _, exists := snapshot.Services["headless_compute"]; exists {
		t.Fatalf("expected legacy headless_compute removed from websocket read model, got %#v", snapshot.Services)
	}
	if service, ok := snapshot.Services["cli"]; !ok || service.Status != "running" {
		t.Fatalf("expected cli runtime path preserved, got %#v", snapshot.Services["cli"])
	}
	if !snapshot.Stale {
		t.Fatal("expected websocket runtime snapshot to preserve stale flag from authoritative read model")
	}
	if got := snapshot.ConfigRevision; got != "config-rev-12" {
		t.Fatalf("unexpected config revision: %q", got)
	}
}
