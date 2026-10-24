package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
	"github.com/autocarto/carto_agent/internal/toolrouter"
)

// RuntimeCache 保存 middleware 运行时快照缓存。
type RuntimeCache struct {
	mu               sync.RWMutex
	snapshot         *middlewareclient.RuntimeSnapshot
	lastSnapshotTime time.Time
	toolCatalogCache []middlewareclient.ToolCatalogItem
}

type runtimeInstanceDeltaSummary struct {
	Source             string
	PrevActiveID       string
	NextActiveID       string
	PrevRunningCount   int
	NextRunningCount   int
	RemovedInstanceIDs []string
}

const (
	instanceStatusReasonLeaseExpired = "lease_expired"
	instanceStatusReasonInstanceStop = "instance_stopped"
	instanceStatusReasonNonRunning   = "non_running"
)

func NewRuntimeCache() *RuntimeCache {
	return &RuntimeCache{}
}

func (c *RuntimeCache) GetState() middlewareclient.RuntimeState {
	if c == nil {
		return middlewareclient.RuntimeState{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	state := middlewareclient.RuntimeState{
		LastSnapshotTime: c.lastSnapshotTime,
	}
	if c.snapshot != nil {
		snapshot := cloneMiddlewareRuntimeSnapshot(*c.snapshot)
		state.Snapshot = &snapshot
	}
	return state
}

func (c *RuntimeCache) GetSnapshot() *middlewareclient.RuntimeSnapshot {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.snapshot == nil {
		return nil
	}
	snapshot := cloneMiddlewareRuntimeSnapshot(*c.snapshot)
	return &snapshot
}

func (c *RuntimeCache) ApplySnapshot(snapshot middlewareclient.RuntimeSnapshot) (*middlewareclient.RuntimeSnapshot, bool) {
	if c == nil {
		return nil, false
	}
	fingerprint := runtimeSnapshotFingerprint(snapshot)
	c.mu.Lock()
	defer c.mu.Unlock()

	var prev *middlewareclient.RuntimeSnapshot
	if c.snapshot != nil {
		clonedPrev := cloneMiddlewareRuntimeSnapshot(*c.snapshot)
		prev = &clonedPrev
	}

	changed := prev == nil || runtimeSnapshotFingerprint(*prev) != fingerprint
	cloned := cloneMiddlewareRuntimeSnapshot(snapshot)
	c.snapshot = &cloned
	c.lastSnapshotTime = time.Now()
	return prev, changed
}
func (c *RuntimeCache) ToolCatalogCache() []middlewareclient.ToolCatalogItem {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]middlewareclient.ToolCatalogItem, len(c.toolCatalogCache))
	copy(out, c.toolCatalogCache)
	return out
}

func (c *RuntimeCache) SetToolCatalogCache(tools []middlewareclient.ToolCatalogItem) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolCatalogCache = make([]middlewareclient.ToolCatalogItem, len(tools))
	copy(c.toolCatalogCache, tools)
}

func runtimeSnapshotFingerprint(snapshot middlewareclient.RuntimeSnapshot) string {
	raw, err := json.Marshal(struct {
		Version             uint64                                    `json:"version"`
		ConfigRevision      string                                    `json:"config_revision,omitempty"`
		DiscoveryRevision   string                                    `json:"discovery_revision,omitempty"`
		RuntimeRevision     string                                    `json:"runtime_revision,omitempty"`
		ServicePhase        string                                    `json:"service_phase,omitempty"`
		ToolCatalogRevision string                                    `json:"tool_catalog_revision,omitempty"`
		ToolSchemaRevision  string                                    `json:"tool_schema_revision,omitempty"`
		ToolAvailability    string                                    `json:"tool_availability_revision,omitempty"`
		ToolRevisions       middlewareclient.ToolRevisions            `json:"tool_revisions,omitempty"`
		LastRebuildReason   string                                    `json:"last_rebuild_reason,omitempty"`
		LastRebuildAt       time.Time                                 `json:"last_rebuild_at,omitempty"`
		ModeIssues          map[string]middlewareclient.ModeIssue     `json:"mode_issues,omitempty"`
		ModeReadiness       map[string]middlewareclient.ModeReadiness `json:"mode_readiness,omitempty"`
		Stale               bool                                      `json:"stale,omitempty"`
		Services            map[string]middlewareclient.ServiceStatus `json:"services"`
		Instances           middlewareclient.InstancesSnapshot        `json:"instances"`
	}{
		Version:             snapshot.Version,
		ConfigRevision:      snapshot.ConfigRevision,
		DiscoveryRevision:   snapshot.DiscoveryRevision,
		RuntimeRevision:     snapshot.RuntimeRevision,
		ServicePhase:        snapshot.ServicePhase,
		ToolCatalogRevision: snapshot.ToolCatalogRevision,
		ToolSchemaRevision:  snapshot.ToolSchemaRevision,
		ToolAvailability:    snapshot.ToolAvailabilityRevision,
		ToolRevisions:       snapshot.ToolRevisions,
		LastRebuildReason:   snapshot.LastRebuildReason,
		LastRebuildAt:       snapshot.LastRebuildAt,
		ModeIssues:          snapshot.ModeIssues,
		ModeReadiness:       snapshot.ModeReadiness,
		Stale:               snapshot.Stale,
		Services:            snapshot.Services,
		Instances:           snapshot.Instances,
	})
	if err != nil {
		return fmt.Sprintf(
			"version=%d config=%s discovery=%s runtime=%s catalog=%s schema=%s availability=%s stale=%t",
			snapshot.Version,
			snapshot.ConfigRevision,
			snapshot.DiscoveryRevision,
			snapshot.RuntimeRevision,
			snapshot.ToolCatalogRevision,
			snapshot.ToolSchemaRevision,
			snapshot.ToolAvailabilityRevision,
			snapshot.Stale,
		)
	}
	return string(raw)
}

func cloneMiddlewareRuntimeSnapshot(snapshot middlewareclient.RuntimeSnapshot) middlewareclient.RuntimeSnapshot {
	cloned := snapshot
	if snapshot.Services != nil {
		cloned.Services = make(map[string]middlewareclient.ServiceStatus, len(snapshot.Services))
		for key, value := range snapshot.Services {
			cloned.Services[key] = value
		}
	}
	if snapshot.Instances.Items != nil {
		cloned.Instances.Items = make([]*discovery.QGISInstance, 0, len(snapshot.Instances.Items))
		for _, item := range snapshot.Instances.Items {
			if item == nil {
				continue
			}
			cp := *item
			if item.Services != nil {
				cp.Services = make(map[string]discovery.ServiceInfo, len(item.Services))
				for svcName, svc := range item.Services {
					cp.Services[svcName] = svc
				}
			}
			cloned.Instances.Items = append(cloned.Instances.Items, &cp)
		}
	}
	if snapshot.ModeIssues != nil {
		cloned.ModeIssues = make(map[string]middlewareclient.ModeIssue, len(snapshot.ModeIssues))
		for key, value := range snapshot.ModeIssues {
			cloned.ModeIssues[key] = value
		}
	}
	if snapshot.ModeReadiness != nil {
		cloned.ModeReadiness = make(map[string]middlewareclient.ModeReadiness, len(snapshot.ModeReadiness))
		for key, value := range snapshot.ModeReadiness {
			cloned.ModeReadiness[key] = value
		}
	}
	return cloned
}

func cloneDiscoveryInstance(instance *discovery.QGISInstance) *discovery.QGISInstance {
	if instance == nil {
		return nil
	}
	cp := *instance
	if instance.Services != nil {
		cp.Services = make(map[string]discovery.ServiceInfo, len(instance.Services))
		for name, svc := range instance.Services {
			cp.Services[name] = svc
		}
	}
	return &cp
}

func summarizeInstanceAvailability(instances []*discovery.QGISInstance) (count int, running int) {
	count = len(instances)
	for _, instance := range instances {
		if instance != nil && strings.EqualFold(strings.TrimSpace(instance.Status), "running") {
			running++
		}
	}
	return count, running
}

func mergeRuntimeInstances(base, overlay []*discovery.QGISInstance) []*discovery.QGISInstance {
	merged := make([]*discovery.QGISInstance, 0, len(base)+len(overlay))
	indexByID := make(map[string]int, len(base)+len(overlay))
	appendOrReplace := func(instance *discovery.QGISInstance) {
		if instance == nil {
			return
		}
		instanceID := strings.TrimSpace(instance.ID)
		if instanceID == "" {
			merged = append(merged, cloneDiscoveryInstance(instance))
			return
		}
		if idx, ok := indexByID[instanceID]; ok {
			merged[idx] = cloneDiscoveryInstance(instance)
			return
		}
		indexByID[instanceID] = len(merged)
		merged = append(merged, cloneDiscoveryInstance(instance))
	}
	for _, instance := range base {
		appendOrReplace(instance)
	}
	for _, instance := range overlay {
		appendOrReplace(instance)
	}
	return merged
}

func explainNonRunningInstance(instance *discovery.QGISInstance) *discovery.QGISInstance {
	retained := cloneDiscoveryInstance(instance)
	if retained == nil {
		return nil
	}
	status := strings.ToLower(strings.TrimSpace(retained.Status))
	if status == "running" || status == "" {
		return retained
	}
	if strings.TrimSpace(retained.StatusReason) == "" {
		switch status {
		case "dead":
			retained.StatusReason = instanceStatusReasonLeaseExpired
		case "stopped":
			retained.StatusReason = instanceStatusReasonInstanceStop
		default:
			retained.StatusReason = instanceStatusReasonNonRunning
		}
	}
	if strings.TrimSpace(retained.StatusDetail) == "" {
		switch retained.StatusReason {
		case instanceStatusReasonLeaseExpired:
			retained.StatusDetail = "实例心跳已过期或进程已退出，等待重新注册恢复。"
		case instanceStatusReasonInstanceStop:
			retained.StatusDetail = "实例已停止运行，如需恢复请重新启动或重新注册。"
		default:
			retained.StatusDetail = "实例当前不在运行态，请检查状态后再恢复使用。"
		}
	}
	return retained
}

func retainNonRunningInstanceForExplainability(prev middlewareclient.RuntimeSnapshot, next middlewareclient.RuntimeSnapshot, event *discovery.InstanceEvent) middlewareclient.RuntimeSnapshot {
	retainedByID := make(map[string]*discovery.QGISInstance)
	collectRetained := func(instance *discovery.QGISInstance) {
		if instance == nil {
			return
		}
		instanceID := strings.TrimSpace(instance.ID)
		if instanceID == "" {
			return
		}
		status := strings.ToLower(strings.TrimSpace(instance.Status))
		if status == "running" || status == "" {
			return
		}
		if current, ok := findRunningInstanceByID(next.Instances.Items, instanceID); ok && current != nil {
			return
		}
		retained := explainNonRunningInstance(instance)
		if retained == nil {
			return
		}
		retainedByID[instanceID] = retained
	}

	for _, item := range prev.Instances.Items {
		collectRetained(item)
	}
	if event != nil {
		collectRetained(event.Instance)
	}
	if len(retainedByID) == 0 {
		return next
	}

	retainedItems := make([]*discovery.QGISInstance, 0, len(retainedByID))
	for _, item := range retainedByID {
		retainedItems = append(retainedItems, item)
	}
	next = cloneMiddlewareRuntimeSnapshot(next)
	next.Instances.Items = mergeRuntimeInstances(next.Instances.Items, retainedItems)
	next.Instances.InstanceCount, next.Instances.RunningCount = summarizeInstanceAvailability(next.Instances.Items)
	if activeID := strings.TrimSpace(next.Instances.ActiveID); activeID != "" {
		if current, ok := findRunningInstanceByID(next.Instances.Items, activeID); !ok || current == nil {
			next.Instances.ActiveID = ""
		}
	}
	return next
}

func buildAggregateBridgeServiceStatus(existing middlewareclient.ServiceStatus, instanceCount, runningCount int) middlewareclient.ServiceStatus {
	status := existing
	status.ID = skill.RuntimeIDQGIS
	if strings.TrimSpace(status.Source) == "" {
		status.Source = discovery.SourceReverseWS
	}
	status.Enabled = true
	status.InstanceCount = instanceCount
	status.RunningCount = runningCount
	if runningCount > 0 {
		status.Status = "running"
		status.Ready = true
		return status
	}
	status.Ready = false
	switch strings.ToLower(strings.TrimSpace(status.Status)) {
	case "failed", "starting", "restarting", "syncing", "unknown":
		return status
	default:
		status.Status = "stopped"
		return status
	}
}

func applyLocalBridgeProjection(base, projection middlewareclient.RuntimeSnapshot) middlewareclient.RuntimeSnapshot {
	merged := cloneMiddlewareRuntimeSnapshot(base)
	if len(projection.Services) == 0 && len(projection.Instances.Items) == 0 && strings.TrimSpace(projection.Instances.ActiveID) == "" {
		return merged
	}
	if merged.Services == nil {
		merged.Services = map[string]middlewareclient.ServiceStatus{}
	}
	for key, value := range projection.Services {
		normalizedKey := normalizePublicRuntimeServiceKey(key)
		if strings.TrimSpace(value.ID) == "" {
			value.ID = normalizedKey
		} else {
			value.ID = normalizePublicRuntimeServiceKey(value.ID)
		}
		merged.Services[normalizedKey] = value
	}
	merged.Instances.Items = mergeRuntimeInstances(merged.Instances.Items, projection.Instances.Items)
	merged.Instances.InstanceCount, merged.Instances.RunningCount = summarizeInstanceAvailability(merged.Instances.Items)
	if activeID := strings.TrimSpace(projection.Instances.ActiveID); activeID != "" {
		merged.Instances.ActiveID = activeID
	}
	if merged.Timestamp.IsZero() && !projection.Timestamp.IsZero() {
		merged.Timestamp = projection.Timestamp
	}
	qgisServiceKey := normalizePublicRuntimeServiceKey(discovery.ServiceNameQGISBridge)
	for key := range merged.Services {
		if key != qgisServiceKey && normalizePublicRuntimeServiceKey(key) == qgisServiceKey {
			delete(merged.Services, key)
		}
	}
	if _, ok := merged.Services[qgisServiceKey]; ok || merged.Instances.InstanceCount > 0 {
		merged.Services[qgisServiceKey] = buildAggregateBridgeServiceStatus(
			merged.Services[qgisServiceKey],
			merged.Instances.InstanceCount,
			merged.Instances.RunningCount,
		)
	}
	return merged
}

func (s *DeepServer) SetMiddlewareToolReloader(reloader func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.middlewareToolReloader = reloader
}

func (s *DeepServer) SetToolCatalogProvider(provider func(context.Context) ([]middlewareclient.ToolCatalogItem, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolCatalogProvider = provider
}

func (s *DeepServer) GetRuntimeState() middlewareclient.RuntimeState {
	if s == nil || s.runtimeCache == nil {
		return middlewareclient.RuntimeState{}
	}
	state := s.runtimeCache.GetState()
	if state.Snapshot != nil {
		snapshot := s.augmentRuntimeSnapshot(*state.Snapshot)
		state.Snapshot = &snapshot
	}
	return state
}

func (s *DeepServer) GetRuntimeSnapshot() middlewareclient.RuntimeSnapshot {
	if s == nil || s.runtimeCache == nil {
		return s.augmentRuntimeSnapshot(middlewareclient.RuntimeSnapshot{})
	}
	if snapshot := s.runtimeCache.GetSnapshot(); snapshot != nil {
		return s.augmentRuntimeSnapshot(*snapshot)
	}
	return s.augmentRuntimeSnapshot(middlewareclient.RuntimeSnapshot{})
}

func (s *DeepServer) augmentRuntimeSnapshot(snapshot middlewareclient.RuntimeSnapshot) middlewareclient.RuntimeSnapshot {
	state := middlewareclient.RuntimeState{}
	if s != nil && s.runtimeCache != nil {
		state = s.runtimeCache.GetState()
	}
	cloned := cloneMiddlewareRuntimeSnapshot(snapshot)
	cloned = applyLocalRuntimeProjection(cloned, s.getLocalRuntimeProjection())
	cloned = applyLocalBridgeProjection(cloned, s.getLocalBridgeProjection())
	if cloned.Services == nil {
		cloned.Services = map[string]middlewareclient.ServiceStatus{}
	}
	cloned.Services["agent"] = middlewareclient.ServiceStatus{
		ID:      "agent",
		Status:  "running",
		Source:  "sidecar",
		Enabled: true,
	}
	cloned.Services = control.NormalizePublicRuntimeServices(cloned.Services)
	planEnabled := s.planEnabledForSnapshot()
	activeConfig := strings.TrimSpace(cloned.ActiveConfig)
	if activeConfig == "" {
		activeConfig = strings.TrimSpace(s.activeConfigNameForSnapshot())
	}
	cloned.ActiveConfig = activeConfig
	runtimeRevision := strings.TrimSpace(s.GetRuntimeRevision())
	if runtimeRevision != "" {
		cloned.RuntimeRevision = runtimeRevision
		if strings.TrimSpace(cloned.ConfigRevision) == "" {
			cloned.ConfigRevision = runtimeRevision
		}
	}
	if strings.TrimSpace(cloned.DiscoveryRevision) == "" {
		cloned.DiscoveryRevision = strings.TrimSpace(control.ResolveDiscoveryRevision(cloned))
	}
	if strings.TrimSpace(cloned.ServicePhase) == "" {
		cloned.ServicePhase = strings.TrimSpace(s.runtimeServicePhase(cloned))
	}
	if schemaRevision := strings.TrimSpace(s.GetToolSchemaRevision()); schemaRevision != "" && strings.TrimSpace(cloned.ToolSchemaRevision) == "" {
		cloned.ToolSchemaRevision = schemaRevision
	}
	if availabilityRevision := strings.TrimSpace(s.GetToolAvailabilityRevision()); availabilityRevision != "" && strings.TrimSpace(cloned.ToolAvailabilityRevision) == "" {
		cloned.ToolAvailabilityRevision = availabilityRevision
	}
	cloned.ToolRevisions = middlewareclient.ToolRevisions{
		Catalog:      strings.TrimSpace(cloned.ToolCatalogRevision),
		Schema:       strings.TrimSpace(cloned.ToolSchemaRevision),
		Availability: strings.TrimSpace(cloned.ToolAvailabilityRevision),
	}
	if strings.TrimSpace(cloned.LastRebuildReason) == "" {
		cloned.LastRebuildReason = strings.TrimSpace(s.GetLastRuntimeRebuildReason())
	}
	if cloned.LastRebuildAt.IsZero() {
		cloned.LastRebuildAt = s.GetLastRuntimeRebuildAt()
	}
	if strings.TrimSpace(cloned.ActiveAgentConfig) == "" {
		cloned.ActiveAgentConfig = strings.TrimSpace(cloned.ActiveConfig)
	}
	if runtimeActiveConfig := strings.TrimSpace(s.GetRuntimeActiveConfigName()); runtimeActiveConfig != "" {
		cloned.RuntimeActiveAgentConfig = runtimeActiveConfig
	} else if strings.TrimSpace(cloned.RuntimeActiveAgentConfig) == "" {
		cloned.RuntimeActiveAgentConfig = strings.TrimSpace(cloned.ActiveConfig)
	}
	cloned.PlanEnabled = planEnabled
	if len(cloned.ModeIssues) == 0 {
		cloned.ModeIssues = s.runtimeModeIssuesSnapshot()
	}
	if len(cloned.ModeReadiness) == 0 {
		cloned.ModeReadiness = s.runtimeModeReadinessSnapshot(planEnabled)
	}
	if readiness, ok := cloned.ModeReadiness["plan"]; ok {
		cloned.PlanReady = readiness.Ready
	}
	cloned.Stale = cloned.Stale || state.Stale
	return cloned
}

func applyLocalRuntimeProjection(base, projection middlewareclient.RuntimeSnapshot) middlewareclient.RuntimeSnapshot {
	merged := cloneMiddlewareRuntimeSnapshot(base)
	if len(projection.Services) == 0 {
		return merged
	}
	if merged.Services == nil {
		merged.Services = map[string]middlewareclient.ServiceStatus{}
	}
	for key, value := range projection.Services {
		normalizedKey := normalizePublicRuntimeServiceKey(key)
		if strings.TrimSpace(value.ID) == "" {
			value.ID = normalizedKey
		} else {
			value.ID = normalizePublicRuntimeServiceKey(value.ID)
		}
		merged.Services[normalizedKey] = value
	}
	if merged.Timestamp.IsZero() && !projection.Timestamp.IsZero() {
		merged.Timestamp = projection.Timestamp
	}
	return merged
}

func (s *DeepServer) snapshotLocalBridgeProjection() middlewareclient.RuntimeSnapshot {
	if s == nil {
		return middlewareclient.RuntimeSnapshot{}
	}
	s.mu.Lock()
	provider := s.localBridgeProvider
	port := s.port
	s.mu.Unlock()
	if provider == nil {
		return middlewareclient.RuntimeSnapshot{}
	}
	registry := provider.InstanceRegistry()
	instances := []*discovery.QGISInstance{}
	if registry != nil {
		instances = registry.GetAll()
	}
	instanceCount, runningCount := summarizeInstanceAvailability(instances)
	activeID := strings.TrimSpace(provider.ActiveInstance())
	if activeID != "" {
		if _, ok := findRunningInstanceByID(instances, activeID); !ok {
			activeID = ""
		}
	}
	services := map[string]middlewareclient.ServiceStatus{}
	if instanceCount > 0 || runningCount > 0 || activeID != "" {
		services[discovery.ServiceNameAgentQGISBridge] = middlewareclient.ServiceStatus{
			ID:            discovery.ServiceNameAgentQGISBridge,
			Status:        ternaryRuntimeStatus(runningCount > 0, "running", "stopped"),
			Host:          "127.0.0.1",
			Port:          port,
			Source:        "sidecar_reverse_ws",
			Enabled:       true,
			Ready:         runningCount > 0,
			InstanceCount: instanceCount,
			RunningCount:  runningCount,
		}
	}
	return middlewareclient.RuntimeSnapshot{
		Timestamp: time.Now(),
		Services:  services,
		Instances: middlewareclient.InstancesSnapshot{
			Items:         instances,
			ActiveID:      activeID,
			InstanceCount: instanceCount,
			RunningCount:  runningCount,
		},
	}
}

func ternaryRuntimeStatus(condition bool, whenTrue, whenFalse string) string {
	if condition {
		return whenTrue
	}
	return whenFalse
}

func (s *DeepServer) SetLocalRuntimeProjection(snapshot middlewareclient.RuntimeSnapshot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.localRuntimeProjection = cloneMiddlewareRuntimeSnapshot(snapshot)
	s.mu.Unlock()
}

func (s *DeepServer) getLocalRuntimeProjection() middlewareclient.RuntimeSnapshot {
	if s == nil {
		return middlewareclient.RuntimeSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneMiddlewareRuntimeSnapshot(s.localRuntimeProjection)
}

func (s *DeepServer) getLocalBridgeProjection() middlewareclient.RuntimeSnapshot {
	if s == nil {
		return middlewareclient.RuntimeSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneMiddlewareRuntimeSnapshot(s.localBridgeProjection)
}

func (s *DeepServer) hasLocalBridgeRunningInstances() bool {
	projection := s.getLocalBridgeProjection()
	return projection.Instances.RunningCount > 0
}

func (s *DeepServer) refreshLocalBridgeProjection(event *discovery.InstanceEvent) (middlewareclient.RuntimeSnapshot, middlewareclient.RuntimeSnapshot, bool) {
	if s == nil {
		return middlewareclient.RuntimeSnapshot{}, middlewareclient.RuntimeSnapshot{}, false
	}
	next := s.snapshotLocalBridgeProjection()
	fallbackActiveID := ""
	if s.runtimeCache != nil {
		if snapshot := s.runtimeCache.GetSnapshot(); snapshot != nil {
			fallbackActiveID = strings.TrimSpace(snapshot.Instances.ActiveID)
		}
	}
	s.mu.Lock()
	prev := cloneMiddlewareRuntimeSnapshot(s.localBridgeProjection)
	next = retainNonRunningInstanceForExplainability(prev, next, event)
	nextFingerprint := runtimeSnapshotFingerprint(next)
	changed := runtimeSnapshotFingerprint(prev) != nextFingerprint
	s.localBridgeProjection = cloneMiddlewareRuntimeSnapshot(next)
	if activeID := strings.TrimSpace(next.Instances.ActiveID); activeID != "" {
		s.activeInstanceID = activeID
	} else if next.Instances.RunningCount > 0 {
		s.activeInstanceID = fallbackActiveID
	} else {
		s.activeInstanceID = ""
	}
	s.mu.Unlock()
	return prev, next, changed
}

func (s *DeepServer) handleLocalBridgeStateChangedForEvent(event *discovery.InstanceEvent) {
	prevProjection, nextProjection, changed := s.refreshLocalBridgeProjection(event)
	if !changed {
		return
	}
	base := middlewareclient.RuntimeSnapshot{}
	if s != nil && s.runtimeCache != nil {
		if snapshot := s.runtimeCache.GetSnapshot(); snapshot != nil {
			base = *snapshot
		}
	}
	prev := applyLocalBridgeProjection(base, prevProjection)
	next := applyLocalBridgeProjection(base, nextProjection)
	if summary, ok := summarizeRuntimeInstanceDelta(&prev, next, "local_bridge"); ok {
		logger.Info("local bridge 实例变化: source=%s prev_active_id=%s next_active_id=%s prev_running_count=%d next_running_count=%d removed_instance_ids=%v", summary.Source, summary.PrevActiveID, summary.NextActiveID, summary.PrevRunningCount, summary.NextRunningCount, summary.RemovedInstanceIDs)
	}
	s.handleBoundInstanceDisconnects(&prev, next)
	s.mu.Lock()
	hasCatalogProvider := s.toolCatalogProvider != nil
	s.mu.Unlock()
	if hasCatalogProvider {
		s.refreshMiddlewareToolCatalog(next.ToolCatalogRevision, "local_bridge")
		return
	}
	s.realtimeService().BroadcastRuntimeSnapshot(next)
}

func (s *DeepServer) handleLocalBridgeStateChanged() {
	s.handleLocalBridgeStateChangedForEvent(nil)
}

func (s *DeepServer) HandleLocalBridgeStateChanged() {
	s.handleLocalBridgeStateChanged()
}

func (s *DeepServer) consumeLocalBridgeRegistryEvents(events <-chan discovery.InstanceEvent, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			switch strings.TrimSpace(event.Type) {
			case "removed", "changed":
			default:
				continue
			}
			s.handleLocalBridgeStateChangedForEvent(&event)
		}
	}
}

func (s *DeepServer) PrimeRuntimeState(snapshot middlewareclient.RuntimeSnapshot, tools []middlewareclient.ToolCatalogItem) {
	if s == nil || s.runtimeCache == nil {
		return
	}
	if snapshot.Services == nil {
		snapshot.Services = map[string]middlewareclient.ServiceStatus{}
	}
	if snapshot.Instances.Items == nil {
		snapshot.Instances.Items = []*discovery.QGISInstance{}
	}
	prev, changed := s.runtimeCache.ApplySnapshot(snapshot)
	s.mu.Lock()
	s.activeInstanceID = strings.TrimSpace(snapshot.Instances.ActiveID)
	s.mu.Unlock()
	if changed {
		s.handleBoundInstanceDisconnects(prev, snapshot)
	}
	if len(tools) == 0 {
		tools = []middlewareclient.ToolCatalogItem{}
	}
	schemaRevision, availabilityRevision := s.resolveToolCatalogRevisions(tools)
	s.setToolCatalogDiagnostics(schemaRevision, availabilityRevision)
	s.runtimeCache.SetToolCatalogCache(tools)
	s.updateToolAggregatorFromCatalog(tools)
	if changed {
		s.realtimeService().BroadcastRuntimeSnapshot(s.GetRuntimeSnapshot())
	}
}

func summarizeRuntimeInstanceDelta(prev *middlewareclient.RuntimeSnapshot, next middlewareclient.RuntimeSnapshot, source string) (runtimeInstanceDeltaSummary, bool) {
	if prev == nil {
		return runtimeInstanceDeltaSummary{}, false
	}
	summary := runtimeInstanceDeltaSummary{
		Source:           strings.TrimSpace(source),
		PrevActiveID:     strings.TrimSpace(prev.Instances.ActiveID),
		NextActiveID:     strings.TrimSpace(next.Instances.ActiveID),
		PrevRunningCount: prev.Instances.RunningCount,
		NextRunningCount: next.Instances.RunningCount,
	}
	prevRunning := collectRunningInstances(*prev)
	nextRunning := collectRunningInstances(next)
	removed := make([]string, 0)
	for instanceID := range prevRunning {
		if _, ok := nextRunning[instanceID]; ok {
			continue
		}
		removed = append(removed, instanceID)
	}
	sort.Strings(removed)
	summary.RemovedInstanceIDs = removed
	if len(summary.RemovedInstanceIDs) == 0 && summary.PrevActiveID == summary.NextActiveID && summary.PrevRunningCount == summary.NextRunningCount {
		return runtimeInstanceDeltaSummary{}, false
	}
	return summary, true
}

func (s *DeepServer) refreshMiddlewareToolCatalog(expectedRevision, source string) {
	if s == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var (
		newTools []middlewareclient.ToolCatalogItem
		err      error
	)
	s.mu.Lock()
	provider := s.toolCatalogProvider
	s.mu.Unlock()
	if provider == nil {
		return
	}
	newTools, err = provider(ctx)
	if err != nil {
		logger.Warn("加载本地工具目录失败: source=%s err=%v", source, err)
		return
	}
	oldTools := s.runtimeCache.ToolCatalogCache()
	if len(newTools) == 0 {
		newTools = []middlewareclient.ToolCatalogItem{}
	}
	diff := toolcatalog.DiffTools(oldTools, newTools)
	schemaRevision, availabilityRevision := s.resolveToolCatalogRevisions(newTools)
	s.setToolCatalogDiagnostics(schemaRevision, availabilityRevision)
	s.runtimeCache.SetToolCatalogCache(newTools)
	s.updateToolAggregatorFromCatalog(newTools)
	s.requestCapabilityRefresh(fmt.Sprintf("runtime_tool_catalog:%s", source))
	s.realtimeService().BroadcastRuntimeSnapshot(s.GetRuntimeSnapshot())
	if !diff.Changed {
		logger.Info("middleware 工具目录 revision 变化但 schema / availability 无变更，跳过 Agent 重建: revision=%s source=%s", expectedRevision, source)
		return
	}
	if !diff.SchemaChanged {
		logger.Info("middleware 工具目录仅 availability 变化，跳过 Agent 重建: source=%s availability_changed=%v revision=%s", source, diff.AvailabilityOnly, availabilityRevision)
		return
	}
	if schemaRevision != "" && schemaRevision == s.lastAppliedToolSchemaRevision() {
		logger.Info("middleware 工具目录 schema revision 已应用，跳过重复重建: source=%s revision=%s", source, schemaRevision)
		return
	}
	logger.Info("middleware 工具目录 schema 发生变化，准备重建 Agent: source=%s revision=%s added=%v removed=%v modified=%v", source, schemaRevision, diff.Added, diff.Removed, diff.Modified)
	s.setPendingRuntimeRebuildReason(fmt.Sprintf("tool_schema_changed:%s", schemaRevision))
	s.mu.Lock()
	reloader := s.middlewareToolReloader
	s.mu.Unlock()
	if reloader == nil {
		return
	}
	if err := reloader(ctx); err != nil {
		logger.Warn("根据 middleware 工具目录重建 Agent 失败: %v", err)
	}
}

func (s *DeepServer) resolveToolCatalogRevisions(tools []middlewareclient.ToolCatalogItem) (string, string) {
	if s != nil && s.capabilitySnapshotStore != nil {
		if snapshot := s.capabilitySnapshotStore.Get(); snapshot != nil {
			schemaRevision := strings.TrimSpace(snapshot.SchemaRevision)
			availabilityRevision := strings.TrimSpace(snapshot.AvailabilityRevision)
			if schemaRevision != "" || availabilityRevision != "" {
				return schemaRevision, availabilityRevision
			}
		}
	}
	return toolcatalog.ToolSchemaRevision(tools), toolcatalog.ToolAvailabilityRevision(tools)
}

func (s *DeepServer) updateToolAggregatorFromCatalog(tools []middlewareclient.ToolCatalogItem) {
	if s == nil || s.aggregator == nil {
		return
	}
	interactive := make([]toolrouter.InteractiveToolDefinition, 0)
	computeDefs := make([]compute.ToolDefinition, 0)
	for _, item := range tools {
		runtimeID := skill.CatalogItemRuntime(item)
		if strings.TrimSpace(runtimeID) == "" {
			continue
		}
		isCompute := strings.EqualFold(runtimeID, skill.RuntimeIDCLI)
		if isCompute {
			computeDefs = append(computeDefs, compute.ToolDefinition{
				Name:              item.Name,
				DisplayName:       item.DisplayName,
				Keywords:          append([]string(nil), item.Keywords...),
				Description:       item.Description,
				Category:          item.Category,
				InputSchema:       cloneSchemaMap(item.InputSchema),
				Available:         item.Available,
				UnavailableReason: strings.TrimSpace(item.UnavailableReason),
			})
			continue
		}
		interactive = append(interactive, toolrouter.InteractiveToolDefinition{
			Name:              item.Name,
			DisplayName:       item.DisplayName,
			Keywords:          append([]string(nil), item.Keywords...),
			Description:       item.Description,
			Category:          item.Category,
			InputSchema:       cloneSchemaMap(item.InputSchema),
			Available:         item.Available,
			UnavailableReason: strings.TrimSpace(item.UnavailableReason),
			InstanceIDs:       append([]string(nil), item.InstanceIDs...),
		})
	}
	s.aggregator.UpdateInteractive(interactive)
	s.aggregator.UpdateCompute(computeDefs)
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

func cloneSchemaMap(src map[string]any) map[string]interface{} {
	if len(src) == 0 {
		return map[string]interface{}{}
	}
	raw, err := json.Marshal(src)
	if err != nil {
		out := make(map[string]interface{}, len(src))
		for key, value := range src {
			out[key] = value
		}
		return out
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		out = make(map[string]interface{}, len(src))
		for key, value := range src {
			out[key] = value
		}
	}
	return out
}

func (s *DeepServer) broadcastRuntimeSnapshot(snapshot middlewareclient.RuntimeSnapshot) {
	msg := Message{Type: MsgTypeRuntimeSnapshot, Data: s.runtimeSnapshotMessageData(s.augmentRuntimeSnapshot(snapshot))}
	for _, conn := range s.listClientConnections() {
		if err := s.sendMessage(conn, msg); err != nil {
			logger.Warn("广播 runtime_snapshot 失败: %v", err)
		}
	}
}

func (s *DeepServer) sendCurrentRuntimeSnapshot(conn *websocket.Conn) {
	if conn == nil {
		return
	}
	snapshot := s.GetRuntimeSnapshot()
	if snapshot.Version == 0 && len(snapshot.Services) == 0 && snapshot.Instances.InstanceCount == 0 {
		return
	}
	if err := s.sendMessage(conn, Message{Type: MsgTypeRuntimeSnapshot, Data: s.runtimeSnapshotMessageData(snapshot)}); err != nil {
		logger.Warn("发送当前 runtime_snapshot 失败: %v", err)
	}
}

func (s *DeepServer) runtimeSnapshotMessageData(snapshot middlewareclient.RuntimeSnapshot) map[string]any {
	data := runtimeSnapshotToMessageData(snapshot)
	s.mu.Lock()
	service := s.configService
	s.mu.Unlock()
	if service != nil {
		data["runtime_paths"] = service.GetRuntimeDirectoryDiagnostics()
	}
	return data
}

func runtimeSnapshotToMessageData(snapshot middlewareclient.RuntimeSnapshot) map[string]any {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}
