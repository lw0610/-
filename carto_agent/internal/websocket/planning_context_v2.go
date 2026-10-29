package websocket

import (
	"encoding/json"
	"strings"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/layers"
)

// Bound optional catalog rows while retaining the scheme's subagent directory.
// Execution roles load their own full tool manuals when needed.
func boundedPlanningContext(env flow.Environment, guides []flow.GuideSummary, active map[string]any) string {
	catalog := planningCatalog(env, guides)
	summaries := make([]map[string]string, 0, len(guides))
	for _, guide := range guides {
		summaries = append(summaries, map[string]string{"id": guide.ID, "name": guide.Name, "description": guide.Description})
	}
	catalog["guides"] = summaries
	// Alias lists help the resolver, not the planner; retain category parameter
	// contracts and builtin layer names within the prompt budget.
	categories, _ := catalog["data_categories"].([]map[string]any)
	for _, category := range categories {
		delete(category, "aliases")
	}
	layerRows := []map[string]any{}
	if library, err := layers.NewDefaultLibrary(); err == nil {
		if items, e := library.List(); e == nil {
			for _, item := range items {
				if item.Status == "ready" {
					layerRows = append(layerRows, map[string]any{"name": item.Name, "pack": item.Pack, "category": item.Category, "geometry": item.Geometry})
				}
			}
		}
	}
	catalog["builtin_layers"] = layerRows
	catalog["active_plan"] = active
	prefix := "【制图计划契约，已由系统读取】\n" + strings.TrimSpace(env.PlanningInstructions) + "\n当前目录：\n"
	// Summaries are bounded by rows, so truncation never produces broken JSON.
	for {
		raw, _ := json.Marshal(catalog)
		if len(prefix)+len(raw) <= 8*1024 {
			return prefix + string(raw)
		}
		catalog["truncated"] = "目录摘要已截断；需要完整内置数据可在图层库查看"
		switch {
		case len(categories) > 0:
			categories = categories[:len(categories)-1]
			catalog["data_categories"] = categories
		case len(layerRows) > 0:
			layerRows = layerRows[:len(layerRows)-1]
			catalog["builtin_layers"] = layerRows
		case len(summaries) > 0:
			summaries = summaries[:len(summaries)-1]
			catalog["guides"] = summaries
		default:
			// Role choices are essential input, not optional catalog rows. An
			// oversized custom scheme must not silently lose its configured roles.
			return prefix + string(raw)
		}
	}
}
