package skill

import (
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/middlewareclient"
)

const MissingReasonInteractiveInstanceDisconnected = "interactive_instance_disconnected"

// CatalogItemRuntime 按 runtime-first 规则解析目录项运行时。
func CatalogItemRuntime(item middlewareclient.ToolCatalogItem) string {
	if runtimeID := NormalizeRuntime(item.Backend); runtimeID != "" {
		return runtimeID
	}
	switch strings.ToLower(strings.TrimSpace(item.Source)) {
	case RuntimeIDQGIS, "bridge", "interactive", "qgis_bridge", "pyqgis":
		return RuntimeIDQGIS
	case RuntimeIDCLI, "compute":
		return RuntimeIDCLI
	case RuntimeIDInternal:
		return RuntimeIDInternal
	}
	switch strings.ToLower(strings.TrimSpace(item.ProviderID)) {
	case "qgis_bridge":
		return RuntimeIDQGIS
	}
	return ""
}

func ApplyBridgeRuntimeAvailability(items []middlewareclient.ToolCatalogItem, snapshot middlewareclient.RuntimeSnapshot) []middlewareclient.ToolCatalogItem {
	if len(items) == 0 {
		return nil
	}
	runningInstanceIDs := collectRunningInstanceIDs(snapshot)
	cloned := make([]middlewareclient.ToolCatalogItem, 0, len(items))
	for _, item := range items {
		cp := item
		cp.InstanceIDs = append([]string(nil), item.InstanceIDs...)
		if !isBridgeCatalogItem(item) {
			cloned = append(cloned, cp)
			continue
		}
		if len(cp.InstanceIDs) > 0 {
			cp.InstanceIDs = intersectSortedInstanceIDs(cp.InstanceIDs, runningInstanceIDs)
		} else {
			cp.InstanceIDs = append([]string(nil), runningInstanceIDs...)
		}
		if len(cp.InstanceIDs) == 0 {
			cp.Available = false
			cp.UnavailableReason = MissingReasonInteractiveInstanceDisconnected
		} else {
			cp.Available = true
			cp.UnavailableReason = ""
		}
		cloned = append(cloned, cp)
	}
	return cloned
}

func collectRunningInstanceIDs(snapshot middlewareclient.RuntimeSnapshot) []string {
	ids := make([]string, 0, len(snapshot.Instances.Items))
	for _, instance := range snapshot.Instances.Items {
		if instance == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(instance.Status), "running") {
			continue
		}
		instanceID := strings.TrimSpace(instance.ID)
		if instanceID == "" {
			continue
		}
		ids = append(ids, instanceID)
	}
	sort.Strings(ids)
	return ids
}

func isBridgeCatalogItem(item middlewareclient.ToolCatalogItem) bool {
	return strings.EqualFold(CatalogItemRuntime(item), RuntimeIDQGIS)
}

func intersectSortedInstanceIDs(explicit []string, running []string) []string {
	if len(explicit) == 0 || len(running) == 0 {
		return nil
	}
	runningSet := make(map[string]struct{}, len(running))
	for _, instanceID := range running {
		runningSet[strings.TrimSpace(instanceID)] = struct{}{}
	}
	matched := make([]string, 0, len(explicit))
	for _, instanceID := range explicit {
		trimmed := strings.TrimSpace(instanceID)
		if trimmed == "" {
			continue
		}
		if _, ok := runningSet[trimmed]; ok {
			matched = append(matched, trimmed)
		}
	}
	sort.Strings(matched)
	return matched
}
