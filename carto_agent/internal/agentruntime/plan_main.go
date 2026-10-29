package agentruntime

import (
	"context"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

// Plan uses the same profile, prompt, main model and guide backend as Build.
// Only its reachable tools and delegates change. Legacy planner configuration
// is deliberately never consulted.
func (f *Factory) buildPlanComponents(ctx context.Context, name string, profile *config.AgentConfigFile) (adk.ResumableAgent, error) {
	cfg, err := f.deepAgentConfig(name, profile)
	if err != nil {
		return nil, err
	}
	cfg.SkipSubAgents = true
	cfg.PlanningOnly = true
	cfg.Tools = selectCartographicPlanTools(ctx, cfg.Tools)
	return deepagent.New(ctx, cfg)
}
func selectCartographicPlanTools(ctx context.Context, all []tool.BaseTool) []tool.BaseTool {
	out := []tool.BaseTool{}
	for _, t := range all {
		info, err := t.Info(ctx)
		if err == nil && info != nil && CartographicPlanToolAllowed(info.Name) {
			out = append(out, t)
		}
	}
	return out
}

// Explicit allowlist: a name such as osm_get_features is a download despite
// containing "get", and must not accidentally become a planning operation.
func CartographicPlanToolAllowed(name string) bool {
	switch name {
	case "plan_propose", "plan_revise", "hitl_request", "query_build_capabilities",
		"get_layers_info", "get_project_info", "layout_info", "layout_list", "geodata_inspect",
		"resource_get", "reference_read", "output_get_info", "raster_info", "raster_statistics",
		"file_read", "file_list", "pyqgis_get_context", "pyqgis_get_task_status":
		return true
	}
	return false
}
