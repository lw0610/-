package websocket

import (
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
)

// The main agent delegates to configured subagents. Their full tool schemas
// and prompts stay with the selected role, rather than being rebuilt in a plan.
func planningCatalog(env flow.Environment, guides []flow.GuideSummary) map[string]any {
	roles := []map[string]any{}
	executors := []map[string]any{}
	for _, role := range env.Roles {
		if !role.Enabled || role.IsMain {
			continue
		}
		entry := map[string]any{"id": role.ID, "name": role.DisplayName, "description": role.Description, "kind": role.Kind, "tool_packages": role.ToolPackages}
		if role.Executor != "" {
			entry["executor"] = role.Executor
			if _, ok := flow.FindExecutorSpec(role.Executor); ok {
				executors = append(executors, map[string]any{"role": role.ID, "executor": role.Executor})
			}
		}
		provider := role.Provider
		if provider == "" {
			provider = env.MainProvider
		}
		for _, visionProvider := range env.VisionProviders {
			if provider == visionProvider {
				entry["supports_images"] = true
				break
			}
		}
		roles = append(roles, entry)
	}
	categories := []map[string]any{}
	if env.DataSources != nil {
		for _, category := range env.DataSources.Categories {
			entry := map[string]any{"id": category.ID, "name": category.Name, "geometry": category.Geometry}
			if len(category.Params) > 0 {
				entry["params"] = category.Params
			}
			if len(category.Aliases) > 0 {
				entry["aliases"] = append([]datasource.Alias{}, category.Aliases...)
			}
			categories = append(categories, entry)
		}
	}
	return map[string]any{"guides": guides, "roles": roles, "executors": executors, "data_categories": categories}
}
