package skill

import (
	"reflect"
	"sort"
	"strings"
	"sync"

	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

type BridgeDynamicRegistry struct {
	mu             sync.RWMutex
	defsByInstance map[string][]toolpkg.ToolDefinition
}

func NewBridgeDynamicRegistry() *BridgeDynamicRegistry {
	return &BridgeDynamicRegistry{defsByInstance: make(map[string][]toolpkg.ToolDefinition)}
}

func (r *BridgeDynamicRegistry) RegisterInstanceTools(instanceID string, defs []bridgepkg.ToolDefinition) {
	if r == nil {
		return
	}
	trimmedID := strings.TrimSpace(instanceID)
	if trimmedID == "" {
		return
	}
	converted := ConvertBridgeToolDefinitions(defs, trimmedID)
	r.mu.Lock()
	if len(converted) == 0 {
		delete(r.defsByInstance, trimmedID)
	} else {
		r.defsByInstance[trimmedID] = converted
	}
	r.mu.Unlock()
}

func (r *BridgeDynamicRegistry) UnregisterInstanceTools(instanceID string, toolNames []string) {
	if r == nil {
		return
	}
	trimmedID := strings.TrimSpace(instanceID)
	if trimmedID == "" {
		return
	}
	r.mu.Lock()
	delete(r.defsByInstance, trimmedID)
	r.mu.Unlock()
}

func (r *BridgeDynamicRegistry) ApplyToRegistry(registry *toolpkg.Registry) {
	if r == nil || registry == nil {
		return
	}
	registry.RegisterDynamic(r.AllDefinitions())
}

func (r *BridgeDynamicRegistry) AllDefinitions() []toolpkg.ToolDefinition {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	mergedByName := make(map[string]toolpkg.ToolDefinition)
	conflicts := make(map[string]struct{})
	for _, defs := range r.defsByInstance {
		for _, def := range defs {
			name := strings.TrimSpace(def.Metadata.Name)
			if name == "" {
				continue
			}
			if _, conflicted := conflicts[name]; conflicted {
				continue
			}
			existing, ok := mergedByName[name]
			if !ok {
				mergedByName[name] = def.Clone()
				continue
			}
			if !reflect.DeepEqual(buildToolSchemaMap(existing), buildToolSchemaMap(def)) {
				delete(mergedByName, name)
				conflicts[name] = struct{}{}
				continue
			}
			existing.Metadata.InstanceIDs = append(existing.Metadata.InstanceIDs, def.Metadata.InstanceIDs...)
			existing.Metadata.InstanceIDs = normalizeStringSlice(existing.Metadata.InstanceIDs)
			mergedByName[name] = existing
		}
	}
	if len(mergedByName) == 0 {
		return nil
	}
	names := make([]string, 0, len(mergedByName))
	for name := range mergedByName {
		names = append(names, name)
	}
	sort.Strings(names)
	defs := make([]toolpkg.ToolDefinition, 0, len(names))
	for _, name := range names {
		cloned := mergedByName[name].Clone()
		sort.Strings(cloned.Metadata.InstanceIDs)
		defs = append(defs, cloned)
	}
	return defs
}

func buildToolSchemaMap(def toolpkg.ToolDefinition) map[string]any {
	properties := make(map[string]any, len(def.Spec.Parameters))
	required := make([]string, 0, len(def.Spec.Parameters))
	for _, param := range def.Spec.Parameters {
		if strings.TrimSpace(param.Name) == "" {
			continue
		}
		property := map[string]any{
			"type": param.Type,
		}
		if description := strings.TrimSpace(param.Description); description != "" {
			property["description"] = description
		}
		if param.Default != nil {
			property["default"] = cloneAny(param.Default)
		}
		if len(param.Values) > 0 {
			property["enum"] = cloneAny(param.Values)
		}
		properties[param.Name] = property
		if param.Required {
			required = append(required, param.Name)
		}
	}
	sort.Strings(required)
	schemaMap := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schemaMap["required"] = required
	}
	return schemaMap
}
