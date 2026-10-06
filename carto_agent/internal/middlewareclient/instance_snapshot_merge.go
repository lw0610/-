package middlewareclient

import (
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/discovery"
)

func HasInstanceSnapshotData(snapshot InstanceSnapshot) bool {
	return snapshot.InstanceCount > 0 || snapshot.RunningCount > 0 || len(snapshot.Instances) > 0 || strings.TrimSpace(snapshot.ActiveID) != ""
}

func InstanceSnapshotFromRuntime(snapshot RuntimeSnapshot) InstanceSnapshot {
	items := snapshot.Instances.Items
	if items == nil {
		items = []*discovery.QGISInstance{}
	}
	return InstanceSnapshot{
		Instances:     items,
		ActiveID:      strings.TrimSpace(snapshot.Instances.ActiveID),
		QGISAvailable: snapshot.Instances.RunningCount > 0,
		InstanceCount: snapshot.Instances.InstanceCount,
		RunningCount:  snapshot.Instances.RunningCount,
		Stale:         snapshot.Stale,
	}
}

func MergeInstanceSnapshots(preferred, fallback InstanceSnapshot) InstanceSnapshot {
	instances := mergeInstanceList(preferred.Instances, fallback.Instances)
	activeID := chooseMergedActiveID(instances, preferred.ActiveID, fallback.ActiveID)
	runningCount := countRunningInstances(instances)
	instanceCount := len(instances)
	middlewareMode := strings.TrimSpace(preferred.MiddlewareMode)
	if middlewareMode == "" {
		middlewareMode = strings.TrimSpace(fallback.MiddlewareMode)
	}

	stale := preferred.Stale
	switch {
	case HasInstanceSnapshotData(preferred) && HasInstanceSnapshotData(fallback):
		stale = preferred.Stale && fallback.Stale
	case !HasInstanceSnapshotData(preferred) && HasInstanceSnapshotData(fallback):
		stale = fallback.Stale
	}

	return InstanceSnapshot{
		Instances:      instances,
		ActiveID:       activeID,
		QGISAvailable:  runningCount > 0,
		InstanceCount:  instanceCount,
		RunningCount:   runningCount,
		MiddlewareMode: middlewareMode,
		Stale:          stale,
	}
}

func mergeInstanceList(preferred, fallback []*discovery.QGISInstance) []*discovery.QGISInstance {
	fallbackByID := make(map[string]*discovery.QGISInstance, len(fallback))
	for _, inst := range fallback {
		if inst == nil || strings.TrimSpace(inst.ID) == "" {
			continue
		}
		fallbackByID[strings.TrimSpace(inst.ID)] = inst
	}

	result := make([]*discovery.QGISInstance, 0, len(preferred)+len(fallback))
	seen := make(map[string]struct{}, len(preferred)+len(fallback))
	for _, inst := range preferred {
		if inst == nil || strings.TrimSpace(inst.ID) == "" {
			continue
		}
		id := strings.TrimSpace(inst.ID)
		merged := cloneInstance(inst)
		if fallbackInst, ok := fallbackByID[id]; ok {
			enrichInstance(merged, fallbackInst)
		}
		result = append(result, merged)
		seen[id] = struct{}{}
	}

	for _, inst := range fallback {
		if inst == nil || strings.TrimSpace(inst.ID) == "" {
			continue
		}
		id := strings.TrimSpace(inst.ID)
		if _, ok := seen[id]; ok {
			continue
		}
		result = append(result, cloneInstance(inst))
	}
	return result
}

func chooseMergedActiveID(instances []*discovery.QGISInstance, candidateIDs ...string) string {
	if len(instances) == 0 {
		return ""
	}
	exists := make(map[string]struct{}, len(instances))
	for _, inst := range instances {
		if inst == nil || strings.TrimSpace(inst.ID) == "" {
			continue
		}
		exists[strings.TrimSpace(inst.ID)] = struct{}{}
	}
	for _, candidate := range candidateIDs {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if _, ok := exists[trimmed]; ok {
			return trimmed
		}
	}
	for _, inst := range instances {
		if inst != nil && strings.EqualFold(strings.TrimSpace(inst.Status), "running") && strings.TrimSpace(inst.ID) != "" {
			return strings.TrimSpace(inst.ID)
		}
	}
	return ""
}

func countRunningInstances(instances []*discovery.QGISInstance) int {
	count := 0
	for _, inst := range instances {
		if inst != nil && strings.EqualFold(strings.TrimSpace(inst.Status), "running") {
			count++
		}
	}
	return count
}

func cloneInstance(inst *discovery.QGISInstance) *discovery.QGISInstance {
	if inst == nil {
		return nil
	}
	cloned := *inst
	cloned.Services = cloneServices(inst.Services)
	return &cloned
}

func cloneServices(input map[string]discovery.ServiceInfo) map[string]discovery.ServiceInfo {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]discovery.ServiceInfo, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func enrichInstance(target *discovery.QGISInstance, fallback *discovery.QGISInstance) {
	if target == nil || fallback == nil {
		return
	}
	if strings.TrimSpace(target.MachineFingerprint) == "" {
		target.MachineFingerprint = fallback.MachineFingerprint
	}
	if strings.TrimSpace(target.StartTime) == "" {
		target.StartTime = fallback.StartTime
	}
	if target.PID == 0 {
		target.PID = fallback.PID
	}
	if strings.TrimSpace(target.QGISVersion) == "" {
		target.QGISVersion = fallback.QGISVersion
	}
	if shouldPreferFallbackProjectPath(target, fallback) {
		target.ProjectPath = fallback.ProjectPath
	}
	if shouldPreferFallbackStatus(target.Status, fallback.Status) {
		target.Status = fallback.Status
	}
	if target.Services == nil {
		target.Services = cloneServices(fallback.Services)
	} else {
		for name, svc := range fallback.Services {
			if current, ok := target.Services[name]; ok {
				if strings.TrimSpace(current.Status) == "" {
					current.Status = svc.Status
				}
				if current.Port == 0 {
					current.Port = svc.Port
				}
				if current.LastHeartbeat.IsZero() || (!svc.LastHeartbeat.IsZero() && svc.LastHeartbeat.After(current.LastHeartbeat)) {
					current.LastHeartbeat = svc.LastHeartbeat
				}
				target.Services[name] = current
				continue
			}
			target.Services[name] = svc
		}
	}
	if target.CreatedAt.IsZero() || earlierNonZero(fallback.CreatedAt, target.CreatedAt) {
		target.CreatedAt = fallback.CreatedAt
	}
	if target.UpdatedAt.IsZero() || laterNonZero(fallback.UpdatedAt, target.UpdatedAt) {
		target.UpdatedAt = fallback.UpdatedAt
	}
	if target.LastHeart.IsZero() || laterNonZero(fallback.LastHeart, target.LastHeart) {
		target.LastHeart = fallback.LastHeart
	}
	if strings.TrimSpace(target.Source) == "" {
		target.Source = fallback.Source
	}
}

func shouldPreferFallbackProjectPath(target *discovery.QGISInstance, fallback *discovery.QGISInstance) bool {
	if target == nil || fallback == nil {
		return false
	}
	fallbackPath := strings.TrimSpace(fallback.ProjectPath)
	if fallbackPath == "" {
		return false
	}
	targetPath := strings.TrimSpace(target.ProjectPath)
	if targetPath == "" {
		return true
	}
	return laterNonZero(fallback.UpdatedAt, target.UpdatedAt)
}

func shouldPreferFallbackStatus(current, fallback string) bool {
	current = strings.ToLower(strings.TrimSpace(current))
	fallback = strings.ToLower(strings.TrimSpace(fallback))
	if current == "" {
		return fallback != ""
	}
	return current != "running" && fallback == "running"
}

func earlierNonZero(left, right time.Time) bool {
	if left.IsZero() {
		return false
	}
	if right.IsZero() {
		return true
	}
	return left.Before(right)
}

func laterNonZero(left, right time.Time) bool {
	if left.IsZero() {
		return false
	}
	if right.IsZero() {
		return true
	}
	return left.After(right)
}
