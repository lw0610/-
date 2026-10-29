package planexec

import (
	"context"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
	"testing"
)

type checkTool func(context.Context, string, map[string]any) (any, error)

func (f checkTool) Invoke(c context.Context, n string, a map[string]any) (any, error) {
	return f(c, n, a)
}
func TestFinalChecksUseObservedLayoutAndLayers(t *testing.T) {
	p := &plan.Plan{Steps: []plan.Step{{Outputs: []flow.OutputDeclaration{{Label: "成图版面", Type: "layout"}}}}, Checks: []string{"版面元素包含：主图、标题、比例尺、网格", "计划中的图层都已在工程中", "说明栏包含「数据来源说明」", "主观质量由人工判断"}}
	g := &flow.GuideDoc{Body: "绘制铁路图层"}
	p.Data = []plan.Data{{ID: "rail", LayerName: "铁路"}}
	missing := false
	calls := 0
	tool := checkTool(func(_ context.Context, n string, _ map[string]any) (any, error) {
		calls++
		if n == "get_layers_info" {
			return map[string]any{"success": true, "layers": []any{map[string]any{"name": "铁路"}}}, nil
		}
		items := []any{map[string]any{"type": "QgsLayoutItemMap", "item_id": "main_map", "grids": []any{map[string]any{"enabled": true}}}, map[string]any{"type": "QgsLayoutItemLabel", "item_id": "title"}, map[string]any{"type": "QgsLayoutItemLabel", "item_id": "notes", "text": "来源 OSM；许可 ODbL"}}
		if !missing {
			items = append(items, map[string]any{"type": "QgsLayoutItemScaleBar", "item_id": "scale"})
		}
		return map[string]any{"success": true, "layout": map[string]any{"items": items}}, nil
	})
	in := CheckInput{Plan: p, Guide: g, Outputs: map[string]any{"成图版面": "成图"}, SourceNotes: []string{"来源 OSM；许可 ODbL"}}
	r := Check(context.Background(), in, tool)
	if !r.OK || len(r.Manual) != 1 || calls != 2 {
		t.Fatal(r, calls)
	}
	missing = true
	r = Check(context.Background(), in, tool)
	if r.OK || len(r.Errors) != 1 {
		t.Fatal(r)
	}
}
func TestConfirmationPoliciesPreserveDirectDownloads(t *testing.T) {
	p := &plan.Plan{}
	if ConfirmationFor(p, "when_needed", "direct", true).Required {
		t.Fatal("direct downloads unexpectedly require confirmation")
	}
	if !ConfirmationFor(p, "when_needed", "confirm", true).Required {
		t.Fatal("network policy ignored")
	}
	p.Assumptions = []plan.Assumption{{Text: "范围尚未核实", Impact: "high"}}
	if !ConfirmationFor(p, "when_needed", "direct", false).Required {
		t.Fatal("high impact ignored")
	}
	if ConfirmationFor(p, "never", "direct", false).Required {
		t.Fatal("never ignored")
	}
}

func TestConfirmationStepCountAndReview(t *testing.T) {
	p := &plan.Plan{}
	if !ConfirmationFor(p, "always", "direct", false).Required {
		t.Fatal("always ignored")
	}
	p.Steps = make([]plan.Step, 10)
	if ConfirmationFor(p, "when_needed", "direct", false).Required {
		t.Fatal("ten steps should not trigger")
	}
	p.Steps = append(p.Steps, plan.Step{})
	if !ConfirmationFor(p, "when_needed", "direct", false).Required {
		t.Fatal("eleven steps ignored")
	}
	p.Steps = []plan.Step{{Capabilities: []string{"quality-feedback"}}}
	if !ConfirmationFor(p, "when_needed", "direct", false).Required {
		t.Fatal("image review ignored")
	}
}
