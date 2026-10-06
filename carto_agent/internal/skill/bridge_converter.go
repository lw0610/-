package skill

import (
	"sort"
	"strings"

	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

func ConvertBridgeToolDefinitions(defs []bridgepkg.ToolDefinition, instanceID string) []toolpkg.ToolDefinition {
	if len(defs) == 0 {
		return nil
	}
	converted := make([]toolpkg.ToolDefinition, 0, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		converted = append(converted, toolpkg.ToolDefinition{
			APIVersion: toolpkg.ToolAPIVersionV1,
			Kind:       toolpkg.ToolKindDefinition,
			Metadata: toolpkg.ToolMetadata{
				Name:        name,
				Domain:      strings.TrimSpace(def.Category),
				Runtime:     toolpkg.RuntimeIDQGIS,
				Backend:     toolpkg.RuntimeIDQGIS,
				Source:      "bridge",
				Dynamic:     true,
				InstanceIDs: bridgeInstanceIDs(def.InstanceIDs, instanceID),
			},
			Spec: toolpkg.ToolSpec{
				Description: strings.TrimSpace(def.Description),
				Parameters:  bridgeSchemaParameters(def.InputSchema),
			},
			Execution: toolpkg.ToolExecution{
				BackendTool:  name,
				ParamMapping: bridgeParamMapping(def.InputSchema),
			},
		})
	}
	return converted
}

func bridgeInstanceIDs(existing []string, fallback string) []string {
	ids := normalizeStringSlice(existing)
	if len(ids) > 0 {
		return ids
	}
	if trimmed := strings.TrimSpace(fallback); trimmed != "" {
		return []string{trimmed}
	}
	return nil
}

func bridgeSchemaParameters(schema map[string]any) []toolpkg.ToolParameter {
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) == 0 {
		return nil
	}
	requiredSet := make(map[string]struct{})
	switch rawRequired := schema["required"].(type) {
	case []string:
		for _, item := range rawRequired {
			requiredSet[strings.TrimSpace(item)] = struct{}{}
		}
	case []any:
		for _, item := range rawRequired {
			requiredSet[strings.TrimSpace(stringifyResolvedValue(item))] = struct{}{}
		}
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	sort.Strings(keys)
	params := make([]toolpkg.ToolParameter, 0, len(keys))
	for _, key := range keys {
		property, _ := properties[key].(map[string]any)
		paramType := "string"
		if rawType, ok := property["type"].(string); ok && strings.TrimSpace(rawType) != "" {
			paramType = strings.TrimSpace(rawType)
		}
		_, required := requiredSet[key]
		params = append(params, toolpkg.ToolParameter{
			Name:        key,
			Type:        paramType,
			Required:    required,
			Description: strings.TrimSpace(toolString(property["description"])),
		})
	}
	return params
}

func bridgeParamMapping(schema map[string]any) map[string]any {
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) == 0 {
		return map[string]any{}
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	sort.Strings(keys)
	paramMapping := make(map[string]any, len(keys))
	for _, key := range keys {
		paramMapping[key] = "{{ " + key + " }}"
	}
	return paramMapping
}

func toolString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
