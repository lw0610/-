package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
	"github.com/autocarto/carto_agent/internal/toolcontract"
)

const runtimeDescriptorTTL = 24 * time.Hour

// handleRuntimeSnapshot GET /api/runtime/snapshot
func (a *API) handleRuntimeSnapshot(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "runtime_snapshot") {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if a.runtimeCoordinator == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			"RUNTIME_COORDINATOR_UNAVAILABLE",
			"运行态协调器未初始化",
			"discovery",
			"runtime_snapshot",
			map[string]any{"dependency": "runtime_coordinator", "operation": "runtime_snapshot"},
		), nil)
		return
	}
	snapshot, snapshotErr := a.loadRuntimeSnapshotView(ctx)
	if errors.Is(snapshotErr, errRuntimeSnapshotReadTimeout) {
		snapshot = fallbackRuntimeSnapshot(time.Now().UTC(), snapshot, "RUNTIME_SNAPSHOT_TIMEOUT", "运行时快照读取超时，当前返回降级快照。")
	}
	publicCatalog, _, _ := a.loadPublicToolCatalogAuthority(ctx)
	alignRuntimeSnapshotToPublicToolCatalog(&snapshot, publicCatalog)
	result := map[string]any{
		"version":                     snapshot.Version,
		"timestamp":                   snapshot.Timestamp,
		"config_revision":             snapshot.ConfigRevision,
		"discovery_revision":          snapshot.DiscoveryRevision,
		"active_config":               snapshot.ActiveConfig,
		"service_phase":               snapshot.ServicePhase,
		"tool_catalog_revision":       snapshot.ToolCatalogRevision,
		"tool_schema_revision":        snapshot.ToolSchemaRevision,
		"tool_availability_revision":  snapshot.ToolAvailabilityRevision,
		"tool_revisions":              snapshot.ToolRevisions,
		"last_rebuild_reason":         snapshot.LastRebuildReason,
		"services":                    snapshot.Services,
		"instances":                   snapshot.Instances,
		"active_instance_id":          strings.TrimSpace(snapshot.Instances.ActiveID),
		"runtime_revision":            snapshot.RuntimeRevision,
		"active_agent_config":         snapshot.ActiveAgentConfig,
		"runtime_active_agent_config": snapshot.RuntimeActiveAgentConfig,
		"plan_enabled":                snapshot.PlanEnabled,
		"plan_ready":                  snapshot.PlanReady,
		"mode_issues":                 snapshot.ModeIssues,
		"mode_readiness":              snapshot.ModeReadiness,
		"stale":                       snapshot.Stale,
	}
	if a.configService != nil {
		result["runtime_paths"] = a.configService.GetRuntimeDirectoryDiagnostics()
	}
	if !snapshot.LastRebuildAt.IsZero() {
		result["last_rebuild_at"] = snapshot.LastRebuildAt
	}
	if planIssue := strings.TrimSpace(snapshot.ModeIssues["plan"].Message); planIssue != "" {
		result["planner_ready_reason"] = planIssue
	}
	Success(w, result)
}

// handleRuntimeServices GET /api/runtime/services
func (a *API) handleRuntimeServices(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "runtime_services") {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if a.runtimeCoordinator == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			"RUNTIME_COORDINATOR_UNAVAILABLE",
			"运行态协调器未初始化",
			"discovery",
			"runtime_services",
			map[string]any{"dependency": "runtime_coordinator", "operation": "runtime_services"},
		), nil)
		return
	}
	snapshot, snapshotErr := a.loadRuntimeSnapshotView(ctx)
	if errors.Is(snapshotErr, errRuntimeSnapshotReadTimeout) {
		snapshot = fallbackRuntimeSnapshot(time.Now().UTC(), snapshot, "RUNTIME_SNAPSHOT_TIMEOUT", "运行时服务快照读取超时，当前返回降级快照。")
	}
	publicCatalog, _, _ := a.loadPublicToolCatalogAuthority(ctx)
	alignRuntimeSnapshotToPublicToolCatalog(&snapshot, publicCatalog)
	services := control.RuntimeServicesViewFromSnapshot(snapshot)
	result := map[string]any{
		"services":                    services.Services,
		"instances":                   services.Instances,
		"config_revision":             services.ConfigRevision,
		"discovery_revision":          services.DiscoveryRevision,
		"active_config":               services.ActiveConfig,
		"service_phase":               services.ServicePhase,
		"runtime_revision":            services.RuntimeRevision,
		"runtime_active_agent_config": services.RuntimeActiveAgentConfig,
		"active_instance_id":          services.ActiveInstanceID,
		"mode_issues":                 services.ModeIssues,
		"mode_readiness":              services.ModeReadiness,
		"stale":                       services.Stale,
		"tool_catalog_revision":       services.ToolCatalogRevision,
		"tool_schema_revision":        services.ToolSchemaRevision,
		"tool_availability_revision":  services.ToolAvailabilityRevision,
		"tool_revisions":              services.ToolRevisions,
		"last_rebuild_reason":         services.LastRebuildReason,
		"version":                     services.Version,
		"timestamp":                   services.Timestamp,
	}
	if services.LastRebuildAt != "" {
		result["last_rebuild_at"] = services.LastRebuildAt
	}
	if services.PlannerReadyReason != "" {
		result["planner_ready_reason"] = services.PlannerReadyReason
	}
	Success(w, result)
}

func (a *API) handleRuntimeDescriptor(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "runtime_descriptor") {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	fallback := newAvailabilityEnvelope(
		"DISCOVERY_DESCRIPTOR_UNAVAILABLE",
		"运行时 discovery descriptor 不可用",
		"discovery",
		"runtime_descriptor",
		map[string]any{"service_name": discovery.ServiceNameAgentQGISBridge, "operation": "runtime_descriptor"},
	)
	if a.runtimeCoordinator == nil || a.serviceRegistry == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, fallback, nil)
		return
	}

	snapshot, snapshotErr := a.loadRuntimeSnapshotView(ctx)
	snapshot.Timestamp = time.Now().UTC()
	publicCatalog, executionManifest, publicToolCatalog := a.loadPublicToolCatalogAuthority(ctx)
	alignRuntimeSnapshotToPublicToolCatalog(&snapshot, publicCatalog)
	entry, entryErr := a.loadRuntimeServiceEntry(ctx, discovery.ServiceNameAgentQGISBridge)
	if entryErr != nil || entry.Port <= 0 {
		entry = runtimeDescriptorFallbackEntry(r)
	}

	descriptor, err := control.BuildDiscoveryDescriptorWithCatalog(snapshot, entry, snapshot.Timestamp, runtimeDescriptorTTL, publicCatalog)
	if err != nil {
		if entry.Port <= 0 {
			writeControlPlaneError(w, r, http.StatusServiceUnavailable, err, fallback, nil)
			return
		}
		descriptor = fallbackRuntimeDescriptor(snapshot.Timestamp, entry, "DISCOVERY_DESCRIPTOR_DEGRADED", "运行时 descriptor 降级发布，请等待后端恢复。", snapshot, executionManifest, publicToolCatalog)
	}
	if errors.Is(snapshotErr, errRuntimeSnapshotReadTimeout) {
		descriptor = fallbackRuntimeDescriptor(snapshot.Timestamp, entry, "RUNTIME_SNAPSHOT_TIMEOUT", "运行时快照读取超时，descriptor 已降级发布。", snapshot, executionManifest, publicToolCatalog)
	}
	if errors.Is(entryErr, errServiceRegistryReadTimeout) {
		descriptor = fallbackRuntimeDescriptor(snapshot.Timestamp, entry, "SERVICE_REGISTRY_TIMEOUT", "服务端口注册读取超时，descriptor 已降级发布。", snapshot, executionManifest, publicToolCatalog)
	}
	descriptor.ExecutionManifest = executionManifest
	descriptor.ToolCatalog = publicToolCatalog
	Success(w, descriptor)
}

func (a *API) loadRuntimeSnapshot(ctx context.Context) middlewareclient.RuntimeSnapshot {
	if a.runtimeCoordinator != nil {
		return a.runtimeCoordinator.Snapshot(ctx)
	}
	return middlewareclient.RuntimeSnapshot{}
}

func (a *API) loadPublicToolCatalogAuthority(ctx context.Context) (toolcatalog.Contract, []toolcontract.ExecutionManifestEntry, []toolcatalog.Item) {
	fallbackCatalog := degradedPublicCatalogContract()
	if a == nil || a.assetCatalogService == nil {
		return fallbackCatalog, nil, nil
	}
	view, err := a.loadToolCatalogView(ctx, "global")
	if err != nil {
		return fallbackCatalog, nil, nil
	}
	executionManifest, manifestErr := a.loadExecutionManifestContract(ctx)
	if manifestErr != nil {
		return degradedPublicCatalogContract(), nil, nil
	}
	return view.Catalog, executionManifest, descriptorToolCatalog(view)
}

func alignRuntimeSnapshotToPublicToolCatalog(snapshot *middlewareclient.RuntimeSnapshot, catalog toolcatalog.Contract) {
	if snapshot == nil {
		return
	}
	revisions := control.ResolvePublicToolRevisionsFromCatalog(catalog)
	snapshot.ToolCatalogRevision = revisions.Catalog
	snapshot.ToolSchemaRevision = revisions.Schema
	snapshot.ToolAvailabilityRevision = revisions.Availability
	snapshot.ToolRevisions = revisions
}

func loadExecutionManifestContract() ([]toolcontract.ExecutionManifestEntry, error) {
	definitions, err := toolpkg.LoadAuthorityDefinitions(config.ResolveBuiltinToolsDir(), config.ResolveUserToolsDir())
	if err != nil {
		return nil, err
	}
	return toolpkg.BuildExecutionManifestEntries(definitions), nil
}

func degradedPublicCatalogContract() toolcatalog.Contract {
	return toolcatalog.Contract{
		ID:                      "tool_catalog/global",
		Version:                 toolcatalog.ContractVersion,
		Source:                  "go_control_plane",
		CapabilityType:          toolcatalog.CapabilityTypeToolCatalog,
		RuntimeTarget:           "mixed",
		Availability:            toolcatalog.AvailabilityDegraded,
		MissingDependencyReason: toolcatalog.MissingReasonRuntimeCatalogMissing,
		DegradationStatus:       toolcatalog.DegradationStatusMissingDependency,
		Policy:                  &toolcatalog.Policy{Scope: "global"},
		Items:                   []toolcatalog.Capability{},
	}
}

func descriptorToolCatalog(view control.ToolCatalogView) []toolcatalog.Item {
	if len(view.ToolInfos) > 0 {
		return append([]toolcatalog.Item(nil), view.ToolInfos...)
	}
	items := make([]toolcatalog.Item, 0, len(view.Catalog.Items))
	for _, capability := range view.Catalog.Items {
		items = append(items, toolcatalog.Item{
			Name:          strings.TrimSpace(capability.ID),
			Backend:       strings.TrimSpace(capability.RuntimeTarget),
			Source:        strings.TrimSpace(capability.RuntimeTarget),
			Available:     strings.TrimSpace(capability.Availability) == toolcatalog.AvailabilityAvailable,
			MissingReason: strings.TrimSpace(capability.MissingDependencyReason),
			InstanceIDs:   append([]string(nil), capability.InstanceIDs...),
		})
	}
	return items
}
