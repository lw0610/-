package websocket

import (
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
)

// Planning needs package identifiers and category constraints, not the full
// tool schemas of every execution role. Those are loaded by the selected role.
func planningCatalog(env flow.Environment, guides []flow.GuideSummary) map[string]any {
	packages := []map[string]string{}
	for _, pkg := range env.Packages {
		if flow.FlowPackageGrantable(pkg.ID, env.Packages) {
			packages = append(packages, map[string]string{"id": pkg.ID, "name": pkg.DisplayName})
		}
	}
	executors := []map[string]any{}
	for _, spec := range flow.ExecutorSpecs() {
		packages = append(packages, map[string]string{"id": spec.Type, "name": spec.DisplayName})
		executors = append(executors, map[string]any{"capability": spec.Type, "required_input_types": spec.RequiredInputTypes, "required_parameters": spec.RequiredParameters})
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
	return map[string]any{"guides": guides, "capabilities": packages, "executors": executors, "data_categories": categories}
}
