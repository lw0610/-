package acquire

import (
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func plannerFixture(t *testing.T) (*flow.FlowPlan, datasource.PlanningEnvironment) {
	t.Helper()
	catalog, err := datasource.Load("../../../autocarto/tools/system/data-acquisition/assets", "")
	if err != nil {
		t.Fatal(err)
	}
	plan := &flow.FlowPlan{Inputs: []flow.InputPlan{{Key: "数据", Type: "file"}, {Key: "范围", Type: "text"}, {Key: "纸张", Type: "choice"}}, Acquisition: &flow.AcquisitionPlan{Items: []flow.AcquisitionItem{{Key: "铁路", Requirement: "铁路", Input: "数据", InputType: "file", Category: "transport.railway", Geometry: "line", Acquirable: true, Required: true, ExtentExpression: "「范围」", ScaleExpression: "自动", Params: map[string]string{}, Fields: map[string]string{}}}}}
	env := datasource.PlanningEnvironment{Catalog: catalog, Integrations: map[string]bool{"web": true, "osm": true}, MaxDownloadMB: 2048}
	return plan, env
}
func TestV4PlannerDeterministicRegionalAndNationwide(t *testing.T) {
	p, e := plannerFixture(t)
	inputs := map[string]any{"数据": map[string]any{"$acquire": true}, "范围": "郑州市", "纸张": "A1 横向"}
	first, issues := Plan(context.Background(), p, inputs, e, nil)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	if first.Items[0].Chosen == nil || first.Items[0].Chosen.SourceID != "source.osm.overpass" {
		t.Fatalf("%+v", first.Items[0])
	}
	second, _ := Plan(context.Background(), p, inputs, e, nil)
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatal("non deterministic")
	}
	inputs["范围"] = "全国"
	delete(inputs, "纸张")
	nation, issues := Plan(context.Background(), p, inputs, e, nil)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	if nation.Items[0].Chosen == nil || nation.Items[0].Chosen.SourceID != "source.osm.geofabrik-china" {
		t.Fatalf("%+v", nation.Items[0])
	}
	e.Integrations["osm"] = false
	inputs["范围"] = "郑州市"
	disabled, issues := Plan(context.Background(), p, inputs, e, nil)
	if len(issues) == 0 || disabled.Items[0].Available {
		t.Fatalf("%+v %+v", disabled, issues)
	}
}
func TestV4PlannerScaleAndFillUserPrecedence(t *testing.T) {
	extent := &flow.ExtentPlan{BBox: []float64{100, 0, 100 + 2000000.0/111320, 1}}
	scale := EstimateScale(extent, map[string]any{"纸张": "A1 横向"})
	if math.Abs(float64(scale)-3000000) > 450000 {
		t.Fatal(scale)
	}
	p, e := plannerFixture(t)
	root := t.TempDir()
	file := filepath.Join(root, "rail.geojson")
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	p.Acquisition.Items[0].Entry = "铁路"
	p.Acquisition.Items[0].Filename = "rail.geojson"
	plan, issues := Plan(context.Background(), p, map[string]any{"数据": map[string]any{"$fill": true, "path": root}}, e, nil)
	if len(issues) > 0 || plan.Items[0].Mode != "user" || plan.Items[0].UserPath != file {
		t.Fatalf("%+v %+v", plan, issues)
	}
}
func TestV4PlannerRejectSourceAndMissingExtent(t *testing.T) {
	p, e := plannerFixture(t)
	_, issues := Plan(context.Background(), p, map[string]any{"数据": map[string]any{"$acquire": true, "source": "source.invalid"}}, e, nil)
	seen := map[string]bool{}
	for _, issue := range issues {
		seen[issue.Code] = true
	}
	if !seen["FLOW_INPUT_REQUIRED_FOR_ACQUIRE"] || !seen["FLOW_ACQUIRE_SOURCE_INVALID"] {
		t.Fatal(issues)
	}
}

func TestInputOrProducedExtentPrefersProvidedValueAndOtherwiseDefers(t *testing.T) {
	p, e := plannerFixture(t)
	p.Steps = []flow.StepPlan{{ID: "s1", ExpectedOutputs: []flow.OutputDeclaration{{Label: "范围"}}}}
	inputs := map[string]any{"数据": map[string]any{"$acquire": true}}
	deferred, issues := Plan(context.Background(), p, inputs, e, nil)
	if len(issues) > 0 || !deferred.Items[0].Extent.Pending || deferred.Items[0].Extent.DependencyStep != "s1" {
		t.Fatalf("%+v %+v", deferred, issues)
	}
	inputs["范围"] = "郑州市"
	ready, issues := Plan(context.Background(), p, inputs, e, nil)
	if len(issues) > 0 || ready.Items[0].Extent.Pending || ready.Items[0].Extent.DependencyStep != "" || len(ready.Items[0].Extent.BBox) != 4 {
		t.Fatalf("%+v %+v", ready, issues)
	}
}
