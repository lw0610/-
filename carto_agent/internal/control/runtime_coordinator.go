package control

import (
	"context"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
)

// RuntimeCoordinatorDeps 表示 RuntimeCoordinator 的依赖。
type RuntimeCoordinatorDeps struct {
	AgentConfigManager       *config.AgentConfigManager
	RuntimeSource            RuntimeSource
	RuntimeDiagnostics       settings.RuntimeDiagnosticsService
	InstanceRegistryProvider func() *discovery.InstanceRegistry
}

type runtimeCoordinatorService struct {
	agentConfigManager       *config.AgentConfigManager
	runtimeSource            RuntimeSource
	runtimeDiagnostics       settings.RuntimeDiagnosticsService
	instanceRegistryProvider func() *discovery.InstanceRegistry
}

// NewRuntimeCoordinator 创建统一运行态协调器。
func NewRuntimeCoordinator(deps RuntimeCoordinatorDeps) RuntimeCoordinator {
	diagnostics := deps.RuntimeDiagnostics
	if diagnostics == nil {
		if provider, ok := deps.RuntimeSource.(settings.RuntimeDiagnosticsService); ok {
			diagnostics = provider
		}
	}
	return &runtimeCoordinatorService{
		agentConfigManager:       deps.AgentConfigManager,
		runtimeSource:            deps.RuntimeSource,
		runtimeDiagnostics:       diagnostics,
		instanceRegistryProvider: deps.InstanceRegistryProvider,
	}
}

func (c *runtimeCoordinatorService) Snapshot(ctx context.Context) middlewareclient.RuntimeSnapshot {
	snapshot := c.loadSnapshot(ctx)
	snapshot = c.mergeLiveInstances(ctx, snapshot)
	return c.decorate(snapshot)
}

func (c *runtimeCoordinatorService) Services(ctx context.Context) RuntimeServicesView {
	snapshot := c.Snapshot(ctx)
	return RuntimeServicesViewFromSnapshot(snapshot)
}

func RuntimeServicesViewFromSnapshot(snapshot middlewareclient.RuntimeSnapshot) RuntimeServicesView {
	view := RuntimeServicesView{
		Services:                 snapshot.Services,
		Instances:                RuntimeInstancesView{ActiveID: snapshot.Instances.ActiveID, InstanceCount: snapshot.Instances.InstanceCount, RunningCount: snapshot.Instances.RunningCount},
		ConfigRevision:           snapshot.ConfigRevision,
		DiscoveryRevision:        snapshot.DiscoveryRevision,
		ActiveConfig:             snapshot.ActiveConfig,
		ServicePhase:             snapshot.ServicePhase,
		RuntimeRevision:          snapshot.RuntimeRevision,
		RuntimeActiveAgentConfig: snapshot.RuntimeActiveAgentConfig,
		ActiveInstanceID:         strings.TrimSpace(snapshot.Instances.ActiveID),
		ModeIssues:               snapshot.ModeIssues,
		ModeReadiness:            snapshot.ModeReadiness,
		ToolCatalogRevision:      snapshot.ToolCatalogRevision,
		ToolSchemaRevision:       snapshot.ToolSchemaRevision,
		ToolAvailabilityRevision: snapshot.ToolAvailabilityRevision,
		ToolRevisions:            snapshot.ToolRevisions,
		LastRebuildReason:        snapshot.LastRebuildReason,
		PlannerReadyReason:       strings.TrimSpace(snapshot.ModeIssues["plan"].Message),
		Stale:                    snapshot.Stale,
		Version:                  snapshot.Version,
		Timestamp:                formatTime(snapshot.Timestamp),
	}
	if !snapshot.LastRebuildAt.IsZero() {
		view.LastRebuildAt = formatTime(snapshot.LastRebuildAt)
	}
	if view.Services == nil {
		view.Services = map[string]middlewareclient.ServiceStatus{}
	}
	return view
}

func (c *runtimeCoordinatorService) loadSnapshot(ctx context.Context) middlewareclient.RuntimeSnapshot {
	if c.runtimeSource != nil {
		snapshot := c.runtimeSource.GetRuntimeSnapshot()
		if hasRuntimeSnapshotData(snapshot) {
			snapshot.Timestamp = time.Now()
			snapshot.Services = NormalizePublicRuntimeServices(snapshot.Services)
			return snapshot
		}
		if state := c.runtimeSource.GetRuntimeState(); state.Snapshot != nil && hasRuntimeSnapshotData(*state.Snapshot) {
			snapshot := *state.Snapshot
			snapshot.Timestamp = time.Now()
			snapshot.Services = NormalizePublicRuntimeServices(snapshot.Services)
			return snapshot
		}
	}
	instanceSnapshot := c.loadInstanceSnapshot(ctx)
	services := map[string]middlewareclient.ServiceStatus{
		"agent": {
			ID:      "agent",
			Status:  "running",
			Source:  "sidecar",
			Enabled: true,
		},
	}
	if instanceSnapshot.RunningCount > 0 {
		services[skill.RuntimeIDQGIS] = middlewareclient.ServiceStatus{
			ID:            skill.RuntimeIDQGIS,
			Status:        "running",
			Source:        "reverse_ws",
			InstanceCount: instanceSnapshot.InstanceCount,
			RunningCount:  instanceSnapshot.RunningCount,
		}
	}
	items := instanceSnapshot.Instances
	if items == nil {
		items = []*discovery.QGISInstance{}
	}
	return middlewareclient.RuntimeSnapshot{
		Timestamp: time.Now(),
		Services:  services,
		Instances: middlewareclient.InstancesSnapshot{
			Items:         items,
			ActiveID:      instanceSnapshot.ActiveID,
			InstanceCount: instanceSnapshot.InstanceCount,
			RunningCount:  instanceSnapshot.RunningCount,
		},
		Stale: instanceSnapshot.Stale,
	}
}

func hasRuntimeSnapshotData(snapshot middlewareclient.RuntimeSnapshot) bool {
	if len(snapshot.Services) > 0 || snapshot.Version > 0 || !snapshot.Timestamp.IsZero() {
		return true
	}
	if snapshot.Instances.InstanceCount > 0 || snapshot.Instances.RunningCount > 0 || len(snapshot.Instances.Items) > 0 {
		return true
	}
	if strings.TrimSpace(snapshot.Instances.ActiveID) != "" {
		return true
	}
	if strings.TrimSpace(snapshot.ActiveConfig) != "" || strings.TrimSpace(snapshot.ActiveAgentConfig) != "" || strings.TrimSpace(snapshot.RuntimeActiveAgentConfig) != "" {
		return true
	}
	if strings.TrimSpace(snapshot.ConfigRevision) != "" || strings.TrimSpace(snapshot.DiscoveryRevision) != "" || strings.TrimSpace(snapshot.RuntimeRevision) != "" {
		return true
	}
	if strings.TrimSpace(snapshot.ToolCatalogRevision) != "" || strings.TrimSpace(snapshot.ToolSchemaRevision) != "" || strings.TrimSpace(snapshot.ToolAvailabilityRevision) != "" {
		return true
	}
	if strings.TrimSpace(snapshot.LastRebuildReason) != "" || !snapshot.LastRebuildAt.IsZero() {
		return true
	}
	if len(snapshot.ModeIssues) > 0 || len(snapshot.ModeReadiness) > 0 || snapshot.PlanReady || snapshot.Stale {
		return true
	}
	return false
}

func (c *runtimeCoordinatorService) loadInstanceSnapshot(ctx context.Context) middlewareclient.InstanceSnapshot {
	merged := middlewareclient.InstanceSnapshot{Instances: []*discovery.QGISInstance{}}

	registry := c.instanceRegistry()
	if registry == nil {
		if merged.Instances == nil {
			merged.Instances = []*discovery.QGISInstance{}
		}
		return merged
	}
	instances := registry.GetAll()
	runningCount := 0
	for _, inst := range instances {
		if inst != nil && strings.EqualFold(strings.TrimSpace(inst.Status), "running") {
			runningCount++
		}
	}
	local := middlewareclient.InstanceSnapshot{
		Instances:     instances,
		QGISAvailable: runningCount > 0,
		InstanceCount: len(instances),
		RunningCount:  runningCount,
	}
	return middlewareclient.MergeInstanceSnapshots(merged, local)
}

func (c *runtimeCoordinatorService) mergeLiveInstances(ctx context.Context, snapshot middlewareclient.RuntimeSnapshot) middlewareclient.RuntimeSnapshot {
	live := c.loadInstanceSnapshot(ctx)
	merged := middlewareclient.MergeInstanceSnapshots(
		middlewareclient.InstanceSnapshotFromRuntime(snapshot),
		live,
	)

	snapshot.Instances = middlewareclient.InstancesSnapshot{
		Items:         merged.Instances,
		ActiveID:      merged.ActiveID,
		InstanceCount: merged.InstanceCount,
		RunningCount:  merged.RunningCount,
	}
	if snapshot.Services == nil {
		snapshot.Services = map[string]middlewareclient.ServiceStatus{}
	}
	snapshot.Services = NormalizePublicRuntimeServices(snapshot.Services)
	if merged.RunningCount > 0 {
		snapshot.Services[skill.RuntimeIDQGIS] = middlewareclient.ServiceStatus{
			ID:            skill.RuntimeIDQGIS,
			Status:        "running",
			Source:        "reverse_ws",
			InstanceCount: merged.InstanceCount,
			RunningCount:  merged.RunningCount,
		}
	} else if service, ok := snapshot.Services[skill.RuntimeIDQGIS]; ok {
		service.Status = "stopped"
		service.InstanceCount = 0
		service.RunningCount = 0
		if strings.TrimSpace(service.Source) == "" {
			service.Source = "reverse_ws"
		}
		snapshot.Services[skill.RuntimeIDQGIS] = service
	}
	snapshot.ServicePhase = ""
	if middlewareclient.HasInstanceSnapshotData(live) {
		snapshot.Stale = snapshot.Stale && live.Stale
	}
	return snapshot
}

func (c *runtimeCoordinatorService) decorate(snapshot middlewareclient.RuntimeSnapshot) middlewareclient.RuntimeSnapshot {
	decorated := snapshot
	if decorated.Services == nil {
		decorated.Services = map[string]middlewareclient.ServiceStatus{}
	}

	activeConfig := strings.TrimSpace(decorated.ActiveConfig)
	if activeConfig == "" && c.agentConfigManager != nil {
		activeConfig = strings.TrimSpace(c.agentConfigManager.GetActiveConfigName())
	}
	planEnabled := decorated.PlanEnabled
	if c.agentConfigManager != nil {
		if activeConfig == "" {
			activeConfig = strings.TrimSpace(c.agentConfigManager.GetActiveConfigName())
		}
		if active := c.agentConfigManager.GetActiveConfig(); active != nil {
			planEnabled = active.Meta.PlanEnabled
		}
	}

	decorated.ActiveConfig = activeConfig
	if strings.TrimSpace(decorated.ActiveAgentConfig) == "" {
		decorated.ActiveAgentConfig = activeConfig
	}
	if strings.TrimSpace(decorated.RuntimeActiveAgentConfig) == "" {
		decorated.RuntimeActiveAgentConfig = activeConfig
	}
	decorated.PlanEnabled = planEnabled

	if c.runtimeDiagnostics != nil {
		if runtimeRevision := strings.TrimSpace(c.runtimeDiagnostics.GetRuntimeRevision()); runtimeRevision != "" {
			decorated.RuntimeRevision = runtimeRevision
			if strings.TrimSpace(decorated.ConfigRevision) == "" {
				decorated.ConfigRevision = runtimeRevision
			}
		}
		if runtimeConfigName := strings.TrimSpace(c.runtimeDiagnostics.GetRuntimeActiveConfigName()); runtimeConfigName != "" {
			decorated.RuntimeActiveAgentConfig = runtimeConfigName
			if strings.TrimSpace(decorated.ActiveConfig) == "" {
				decorated.ActiveConfig = runtimeConfigName
			}
		}
		if issues := c.runtimeDiagnostics.GetRuntimeModeIssues(); len(issues) > 0 && len(decorated.ModeIssues) == 0 {
			decorated.ModeIssues = convertSettingsModeIssues(issues)
		}
		if plannerReadyReason := strings.TrimSpace(c.runtimeDiagnostics.GetPlannerReadyReason()); plannerReadyReason != "" {
			if decorated.ModeIssues == nil {
				decorated.ModeIssues = map[string]middlewareclient.ModeIssue{}
			}
			issue := decorated.ModeIssues["plan"]
			if strings.TrimSpace(issue.Message) == "" {
				issue.Message = plannerReadyReason
			}
			decorated.ModeIssues["plan"] = issue
		}
		if activeInstanceID := strings.TrimSpace(c.runtimeDiagnostics.GetActiveInstanceID()); activeInstanceID != "" {
			decorated.Instances.ActiveID = activeInstanceID
		}
		if schemaRevision := strings.TrimSpace(c.runtimeDiagnostics.GetToolSchemaRevision()); schemaRevision != "" {
			if strings.TrimSpace(decorated.ToolSchemaRevision) == "" {
				decorated.ToolSchemaRevision = schemaRevision
			}
			if strings.TrimSpace(decorated.ToolRevisions.Schema) == "" {
				decorated.ToolRevisions.Schema = schemaRevision
			}
		}
		if availabilityRevision := strings.TrimSpace(c.runtimeDiagnostics.GetToolAvailabilityRevision()); availabilityRevision != "" {
			if strings.TrimSpace(decorated.ToolAvailabilityRevision) == "" {
				decorated.ToolAvailabilityRevision = availabilityRevision
			}
			if strings.TrimSpace(decorated.ToolRevisions.Availability) == "" {
				decorated.ToolRevisions.Availability = availabilityRevision
			}
		}
		if rebuildReason := strings.TrimSpace(c.runtimeDiagnostics.GetLastRuntimeRebuildReason()); rebuildReason != "" && strings.TrimSpace(decorated.LastRebuildReason) == "" {
			decorated.LastRebuildReason = rebuildReason
		}
		if rebuiltAt := c.runtimeDiagnostics.GetLastRuntimeRebuildAt(); !rebuiltAt.IsZero() && decorated.LastRebuildAt.IsZero() {
			decorated.LastRebuildAt = rebuiltAt
		}
	}

	if strings.TrimSpace(decorated.ToolRevisions.Catalog) == "" {
		decorated.ToolRevisions.Catalog = strings.TrimSpace(decorated.ToolCatalogRevision)
	}
	if strings.TrimSpace(decorated.ToolRevisions.Schema) == "" {
		decorated.ToolRevisions.Schema = strings.TrimSpace(decorated.ToolSchemaRevision)
	}
	if strings.TrimSpace(decorated.ToolRevisions.Availability) == "" {
		decorated.ToolRevisions.Availability = strings.TrimSpace(decorated.ToolAvailabilityRevision)
	}
	if len(decorated.ModeReadiness) == 0 {
		decorated.ModeReadiness = buildRuntimeModeReadiness(decorated.PlanEnabled, decorated.ModeIssues)
	}
	if planReadiness, ok := decorated.ModeReadiness["plan"]; ok {
		decorated.PlanReady = planReadiness.Ready
	}
	if strings.TrimSpace(decorated.ServicePhase) == "" {
		decorated.ServicePhase = deriveRuntimeServicePhase(decorated)
	}
	if strings.TrimSpace(decorated.ConfigRevision) == "" {
		decorated.ConfigRevision = strings.TrimSpace(decorated.RuntimeRevision)
	}
	if strings.TrimSpace(decorated.DiscoveryRevision) == "" {
		decorated.DiscoveryRevision = ResolveDiscoveryRevision(decorated)
	}
	return decorated
}

func (c *runtimeCoordinatorService) instanceRegistry() *discovery.InstanceRegistry {
	if c.instanceRegistryProvider == nil {
		return nil
	}
	return c.instanceRegistryProvider()
}

func convertSettingsModeIssues(issues map[string]settings.SettingsModeIssue) map[string]middlewareclient.ModeIssue {
	if len(issues) == 0 {
		return nil
	}
	result := make(map[string]middlewareclient.ModeIssue, len(issues))
	for mode, issue := range issues {
		trimmedMode := strings.TrimSpace(mode)
		if trimmedMode == "" {
			continue
		}
		result[trimmedMode] = middlewareclient.ModeIssue{
			Code:    strings.TrimSpace(issue.Code),
			Message: strings.TrimSpace(issue.Message),
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func NormalizePublicRuntimeServices(services map[string]middlewareclient.ServiceStatus) map[string]middlewareclient.ServiceStatus {
	if len(services) == 0 {
		return services
	}
	normalized := make(map[string]middlewareclient.ServiceStatus, len(services))
	for key, service := range services {
		normalizedKey := normalizePublicRuntimeServiceKey(key)
		if normalizedKey == "headless_compute" {
			continue
		}
		if strings.TrimSpace(service.ID) == "" {
			service.ID = normalizedKey
		} else {
			service.ID = normalizePublicRuntimeServiceKey(service.ID)
			if service.ID == "headless_compute" {
				continue
			}
		}
		normalized[normalizedKey] = service
	}
	return normalized
}

func normalizePublicRuntimeServiceKey(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case skill.RuntimeIDCLI:
		return skill.RuntimeIDCLI
	case discovery.ServiceNameQGISBridge, discovery.ServiceNameAgentQGISBridge, skill.RuntimeIDQGIS:
		return skill.RuntimeIDQGIS
	default:
		return strings.TrimSpace(raw)
	}
}

func buildRuntimeModeReadiness(planEnabled bool, issues map[string]middlewareclient.ModeIssue) map[string]middlewareclient.ModeReadiness {
	result := map[string]middlewareclient.ModeReadiness{
		"build": {
			Enabled: true,
			Ready:   true,
		},
		"plan": {
			Enabled: planEnabled,
			Ready:   planEnabled,
		},
	}
	if issue, ok := issues["build"]; ok {
		result["build"] = middlewareclient.ModeReadiness{Enabled: true, Ready: false, Code: strings.TrimSpace(issue.Code), Message: strings.TrimSpace(issue.Message)}
	}
	if issue, ok := issues["plan"]; ok {
		result["plan"] = middlewareclient.ModeReadiness{Enabled: planEnabled, Ready: false, Code: strings.TrimSpace(issue.Code), Message: strings.TrimSpace(issue.Message)}
	}
	return result
}

func deriveRuntimeServicePhase(snapshot middlewareclient.RuntimeSnapshot) string {
	if len(snapshot.Services) == 0 {
		return "bootstrap"
	}
	failed := false
	starting := false
	for _, service := range snapshot.Services {
		switch strings.ToLower(strings.TrimSpace(service.Status)) {
		case "failed":
			failed = true
		case "starting", "restarting", "syncing", "unknown":
			starting = true
		}
	}
	if failed {
		return "degraded"
	}
	if len(snapshot.ModeIssues) > 0 {
		return "degraded"
	}
	if starting {
		return "bootstrap"
	}
	return "ready"
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}
