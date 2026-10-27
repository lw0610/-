package flowrun

import (
	"context"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"os"
	"path/filepath"
	"testing"
)

func inputAcquisitionFixture(t *testing.T) (*flow.FlowPlan, datasource.PlanningEnvironment) {
	t.Helper()
	catalog, err := datasource.Load("../../../autocarto/tools/system/data-acquisition/assets", "")
	if err != nil {
		t.Fatal(err)
	}
	p := testPlan(1)
	p.Inputs = []flow.InputPlan{{Key: "数据", Label: "数据", Type: "file", Required: true, Acquirable: true, Remember: true}, {Key: "范围", Label: "范围", Type: "text"}}
	p.Acquisition = &flow.AcquisitionPlan{Items: []flow.AcquisitionItem{{Key: "铁路", Requirement: "铁路", Input: "数据", InputType: "file", Category: "transport.railway", Acquirable: true, Required: true, ExtentExpression: "「范围」", ScaleExpression: "自动", Params: map[string]string{}}}}
	return p, datasource.PlanningEnvironment{Catalog: catalog, Integrations: map[string]bool{"osm": true, "web": true}, MaxDownloadMB: 2048}
}
func TestV4InputAcquireValidationAndPlan(t *testing.T) {
	p, env := inputAcquisitionFixture(t)
	values := map[string]any{"数据": map[string]any{"$acquire": true}, "范围": "郑州市"}
	linked, problems := ValidateInputsWithAcquisition(context.Background(), p, values, t.TempDir(), env, nil)
	if len(problems) != 0 || linked.Acquisition.Items[0].Chosen == nil {
		t.Fatal(problems)
	}
	if p.Acquisition.Items[0].Chosen != nil {
		t.Fatal("shared preview mutated")
	}
	_, problems = ValidateInputsWithAcquisition(context.Background(), p, map[string]any{"数据": map[string]any{"$acquire": true, "source": "missing"}, "范围": "郑州市"}, "", env, nil)
	found := false
	for _, d := range problems {
		found = found || d.Code == "FLOW_INPUT_SOURCE_INVALID"
	}
	if !found {
		t.Fatal(problems)
	}
	_, err := ResolveInputs(p, map[string]any{}, "", "")
	invalid := err.(*InputError)
	if !invalid.Reasons[0].Acquirable {
		t.Fatal(invalid)
	}
	p.Acquisition = nil
	_, err = ResolveInputs(p, values, "", "")
	if err.(*InputError).Reasons[0].Code != "FLOW_INPUT_ACQUIRE_UNSUPPORTED" {
		t.Fatal(err)
	}
}
func TestV4InputMarkerShapeAndFiles(t *testing.T) {
	p, _ := inputAcquisitionFixture(t)
	for _, value := range []any{map[string]any{"$acquire": false}, map[string]any{"$acquire": true, "$fill": true}, map[string]any{"$acquire": true, "unexpected": 1}, map[string]any{"$fill": true, "path": "/missing"}} {
		if _, err := ResolveInputs(p, map[string]any{"数据": value}, "", ""); err == nil {
			t.Fatalf("accepted %+v", value)
		}
	}
	p.Inputs[0].Type = "folder"
	p.Acquisition.Items[0].Entry = "铁路"
	p.Acquisition.Items[0].Filename = "rail.geojson"
	root := t.TempDir()
	file := filepath.Join(root, "rail.geojson")
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	values, err := ResolveInputs(p, map[string]any{"数据": map[string]any{"$fill": true, "path": root, "files": map[string]any{"铁路": file}}}, "", root)
	if err != nil {
		t.Fatal(err)
	}
	if flow.InputModes(values)["数据"] != "fill" {
		t.Fatal(values)
	}
	if _, err = ResolveInputs(p, map[string]any{"数据": map[string]any{"$acquire": true, "files": map[string]any{"不存在": file}}}, "", root); err == nil {
		t.Fatal("unknown entry accepted")
	}
}
func TestV4AcquisitionSuppressionKeepsUnrelatedErrors(t *testing.T) {
	missing := flow.Diagnostic{Level: "error", Code: "FLOW_DATA_FILE_MISSING", Message: "数据“铁路”的文件不存在"}
	invalid := flow.Diagnostic{Level: "error", Code: "FLOW_RESOURCE_INVALID", Message: "无效清单"}
	p := &flow.FlowPlan{Resources: []flow.ResourcePlan{{Key: "list", Problems: []flow.Diagnostic{missing, invalid}}}, LinkDiagnostics: []flow.Diagnostic{missing, invalid}}
	a := flow.AcquisitionPlan{Items: []flow.AcquisitionItem{{Resource: "list", Entry: "铁路", Mode: "fill", Acquirable: true, Available: true}}}
	problems := filterAcquisitionResourceProblems(p, []InputProblem{{Code: missing.Code, Message: missing.Message}, {Code: invalid.Code, Message: invalid.Message}}, a)
	if len(problems) != 1 || problems[0].Code != invalid.Code || !p.HasErrors() {
		t.Fatalf("%+v %+v", problems, p.Diagnostics)
	}
}

type acquisitionCompilerStub struct {
	compilerStub
	env datasource.PlanningEnvironment
}

func (s acquisitionCompilerStub) AcquisitionEnvironment(context.Context, string) (datasource.PlanningEnvironment, error) {
	return s.env, nil
}
func TestV4StartStoresSameAcquisitionPlanAsCheck(t *testing.T) {
	p, env := inputAcquisitionFixture(t)
	values := map[string]any{"数据": map[string]any{"$acquire": true}, "范围": "郑州市"}
	checked, problems := ValidateInputsWithAcquisition(context.Background(), p, values, "", env, nil)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	c, req, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) { return completed(r.Step), nil }, func(o *Options) { o.Compiler = acquisitionCompilerStub{compilerStub{plan: p}, env} })
	req.Inputs = values
	run, err := c.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if run.Plan.Acquisition.Items[0].Chosen.SourceID != checked.Acquisition.Items[0].Chosen.SourceID || p.Acquisition.Items[0].Chosen != nil {
		t.Fatal("startup/check disagreement")
	}
}
