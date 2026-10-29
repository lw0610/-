package websocket

import (
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/resource"
)

// Planning and acquisition use the same selected layout dimensions, so their
// source ranking cannot differ merely because one forgot the map frame.
func planMapFrame(p *plan.Plan, resources []*resource.ResolvedResource) (string, float64, error) {
	paper, orientation, selected := "", "", ""
	width := 0.0
	for _, fact := range p.Facts {
		switch fact.Name {
		case "纸张", "纸张与方向":
			paper = fmt.Sprint(fact.Value)
		case "map_frame_width_mm":
			width, _ = fact.Value.(float64)
		}
	}
	for _, step := range p.Steps {
		if spec := step.Parameters["spec"]; spec != "" {
			selected = spec
		}
		if value := step.Parameters["paper"]; value != "" {
			paper = value
		}
		if value := step.Parameters["orientation"]; value != "" {
			orientation = value
		}
	}
	paperName := paper
	if fields := strings.Fields(paper); len(fields) > 0 {
		paperName = fields[0]
	}
	if orientation == "" {
		if strings.Contains(paper, "纵") || strings.Contains(paper, "竖") || strings.Contains(paper, "portrait") {
			orientation = "portrait"
		} else if strings.Contains(paper, "横") || strings.Contains(paper, "landscape") {
			orientation = "landscape"
		}
	}
	var layouts []*resource.ResolvedResource
	for _, r := range resources {
		if r.Type == "layout" && (selected == "" || selected == r.Name || selected == r.ID || selected == r.Entry) {
			layouts = append(layouts, r)
		}
	}
	if len(layouts) == 1 {
		actual, err := resource.LayoutMapWidthMM(layouts[0].Entry, paperName, orientation)
		if err != nil {
			return paper, 0, fmt.Errorf("读取版面「%s」主图框：%w", layouts[0].Name, err)
		}
		if actual > 0 {
			width = actual
		}
	}
	return paper, width, nil
}
