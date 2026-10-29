package plan

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
)

func TestOutputBindingSeparatesDisplayNameAndSupportsLegacyPlans(t *testing.T) {
	for _, name := range []string{"数据集", "中文显示名称"} {
		p := fixture()
		p.Data[1].Name = name
		before, _ := json.Marshal(p)
		if ds := Validate(p, environment()); len(ds) != 0 {
			t.Fatal(ds)
		}
		after, _ := json.Marshal(p)
		if string(before) != string(after) {
			t.Fatal("Validate must not mutate the input plan")
		}
		if ds := NormalizeReferences(p); len(ds) != 0 || p.Data[1].Output != "数据集" || p.Data[1].Name != name {
			t.Fatalf("unambiguous output not bound independently of display: %+v %+v", p.Data[1], ds)
		}
		p.Data[1].Output = "not-declared"
		if ds := Validate(p, environment()); len(ds) != 1 || ds[0].Code != "PLAN_OUTPUT_INVALID" {
			t.Fatalf("wrong explicit output was silently replaced: %+v", ds)
		}
	}
	if (Data{Name: "legacy"}).OutputLabel() != "legacy" || (Data{Name: "display", Output: "actual"}).OutputLabel() != "actual" {
		t.Fatal("saved-plan compatibility helper lost precedence")
	}
}

func TestMultiOutputRequiresAChoiceAndProvidesExactAlternatives(t *testing.T) {
	p := fixture()
	p.Steps[0].Outputs = append(p.Steps[0].Outputs, flow.OutputDeclaration{Label: "诊断报告", Type: "text"})
	p.Data[1].Name = "显示名称"
	ds := Validate(p, environment())
	if len(ds) != 1 || ds[0].Fix["data_id"] != "d2" || ds[0].Fix["next_action"] != "select_output" || !reflect.DeepEqual(ds[0].Fix["valid_outputs"], p.Steps[0].Outputs) {
		t.Fatalf("ambiguous output lacked concrete choices: %+v", ds)
	}
	p.Data[1].Output = "诊断报告"
	if ds := Validate(p, environment()); len(ds) != 0 {
		t.Fatal(ds)
	}
	// Old multi-output plans still bind by an exact legacy name.
	p.Data[1].Output, p.Data[1].Name = "", "数据集"
	if ds := NormalizeReferences(p); len(ds) != 0 || p.Data[1].Output != "数据集" {
		t.Fatal(p.Data[1], ds)
	}
}

func TestUsesAcquireStepExpandsDeclaredDataWithoutInventingOutputs(t *testing.T) {
	p := fixture()
	p.Data = []Data{{ID: "roads", Name: "路网", From: "acquire", FindID: "find-roads"}, {ID: "boundary", Name: "市界", From: "acquire", FindID: "find-boundary"}}
	p.Steps[0] = Step{ID: "fetch", Title: "获取底图", Kind: "acquire", Data: []string{"roads", "boundary"}}
	p.Steps[1].Uses, p.Steps[1].DependsOn = []string{"fetch", "roads"}, []string{"fetch"}
	if ds := NormalizeReferences(p); len(ds) != 0 || !reflect.DeepEqual(p.Steps[1].Uses, []string{"roads", "boundary"}) || len(p.Data) != 2 {
		t.Fatalf("acquire reference was not expanded deterministically: %+v %+v", p, ds)
	}
	env := Environment{Capabilities: map[string]bool{"export": true}, ValidateSource: func(string, any) error { return nil }}
	if ds := Validate(p, env); len(ds) != 0 {
		t.Fatal(ds)
	}
	// Explicit data IDs do not allow a later acquire to become a cycle.
	p.Steps[0].Uses = []string{"roads"}
	if ds := Validate(p, env); !hasDiagnostic(ds, "PLAN_REF_INVALID") {
		t.Fatalf("acquire consumed its own future data: %+v", ds)
	}
}

func TestUsesSingleOutputCreatesStableReusableDataReference(t *testing.T) {
	p := fixture()
	p.Data = p.Data[:1]
	p.Steps[1].Uses = []string{"s1", "s1"}
	if ds := NormalizeReferences(p); len(ds) != 0 {
		t.Fatal(ds)
	}
	generated := p.Data[1]
	if !Identifier.MatchString(generated.ID) || generated.From != "step" || generated.Step != "s1" || generated.Output != "数据集" || !reflect.DeepEqual(p.Steps[1].Uses, []string{generated.ID}) {
		t.Fatalf("invalid normalized reference: %+v %+v", generated, p.Steps[1].Uses)
	}
	before, _ := json.Marshal(p)
	if ds := NormalizeReferences(p); len(ds) != 0 {
		t.Fatal(ds)
	}
	after, _ := json.Marshal(p)
	if string(before) != string(after) {
		t.Fatal("reference normalization is not idempotent")
	}
	copy := fixture()
	copy.Data, copy.Steps[1].Uses = copy.Data[:1], []string{"s1"}
	NormalizeReferences(copy)
	if copy.Data[1].ID != generated.ID {
		t.Fatal("same producer/output generated an unstable data ID")
	}
	if ds := Validate(p, environment()); len(ds) != 0 {
		t.Fatal(ds)
	}
}

func TestUsesReusesDeclaredBindingsAndExplicitDataIDsWinCollisions(t *testing.T) {
	p := fixture()
	p.Data[1].Name, p.Data[1].Output = "派生显示名", "数据集"
	p.Steps[1].Uses = []string{"s1"}
	if ds := NormalizeReferences(p); len(ds) != 0 || len(p.Data) != 2 || !reflect.DeepEqual(p.Steps[1].Uses, []string{"d2"}) {
		t.Fatal(p, ds)
	}
	p = fixture()
	p.Data[0].ID, p.Steps[0].Uses, p.Steps[1].Uses = "s1", []string{"s1"}, []string{"s1"}
	if ds := NormalizeReferences(p); len(ds) != 0 || !reflect.DeepEqual(p.Steps[1].Uses, []string{"s1"}) || len(p.Data) != 2 {
		t.Fatal("step ID overrode an explicit data ID", p, ds)
	}
	if ds := Validate(p, environment()); len(ds) != 0 {
		t.Fatal(ds)
	}
}

func TestAmbiguousAndUnknownUsesCarryRepairableReferences(t *testing.T) {
	p := fixture()
	p.Steps[0].Outputs = append(p.Steps[0].Outputs, flow.OutputDeclaration{Label: "诊断报告", Type: "text"})
	p.Steps[1].Uses = []string{"s1"}
	ds := Validate(p, environment())
	if len(ds) != 1 || ds[0].Code != "PLAN_OUTPUT_AMBIGUOUS" || !reflect.DeepEqual(ds[0].Fix["valid_outputs"], p.Steps[0].Outputs) || !reflect.DeepEqual(ds[0].Fix["valid_data_ids"], []string{"d1", "d2"}) {
		t.Fatalf("multi-output use must not choose a result: %+v", ds)
	}
	p.Steps[1].Uses = []string{"does-not-exist"}
	ds = Validate(p, environment())
	if len(ds) != 1 || ds[0].Code != "PLAN_REF_INVALID" || !strings.Contains(ds[0].Message, "data.id") || !reflect.DeepEqual(ds[0].Fix["valid_data_ids"], []string{"d1", "d2"}) {
		t.Fatalf("unknown use lacked actual valid IDs: %+v", ds)
	}
	if p.Steps[1].Uses[0] != "does-not-exist" {
		t.Fatal("unresolved use was removed instead of remaining correctable")
	}
}

func TestReferenceNormalizationNeverAllowsForwardOrCyclicDependencies(t *testing.T) {
	for _, mutate := range []func(*Plan){
		func(p *Plan) { p.Steps[0].Uses = []string{"s2"} },
		func(p *Plan) { p.Steps[0].Uses = []string{"d2"} },
		func(p *Plan) { p.Steps[0].DependsOn = []string{"s2"} },
	} {
		p := fixture()
		mutate(p)
		if ds := Validate(p, environment()); !hasDiagnostic(ds, "PLAN_REF_INVALID") {
			t.Fatalf("forward/cyclic dependency accepted: %+v", ds)
		}
	}
}

func hasDiagnostic(ds []flow.Diagnostic, code string) bool {
	for _, diagnostic := range ds {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
