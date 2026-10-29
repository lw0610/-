package plan

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCapabilityDiagnosticsUseCurrentPackagesAndGroupByStep(t *testing.T) {
	p := fixture()
	p.Steps[0].Capabilities = []string{"imagined_join_tool", "imagined_calculator_tool"}
	p.Steps[1].Capabilities = []string{"local-export"}
	env := Environment{Capabilities: map[string]bool{"local-fields": true, "local-export": true, "disabled-package": false}}
	err := RequireValid(p, env)
	var invalid *ValidationError
	if !errors.As(err, &invalid) || len(invalid.Diagnostics) != 1 {
		t.Fatalf("expected one actionable diagnostic per invalid step: %v", err)
	}
	d := invalid.Diagnostics[0]
	if d.Code != "PLAN_CAPABILITY_UNKNOWN" || d.Step != "s1" || !strings.Contains(d.Message, "工具包 ID") || !strings.Contains(d.Message, "imagined_join_tool") || !strings.Contains(d.Message, "imagined_calculator_tool") {
		t.Fatalf("missing package-level explanation: %+v", d)
	}
	details := invalid.ToolProviderDiagnostics()
	if details["next_action"] != "correct_plan" || !reflect.DeepEqual(details["allowed_capabilities"], []string{"local-export", "local-fields"}) {
		t.Fatalf("constraints were not derived from current capabilities: %+v", details)
	}
	if invalid.ToolErrorCode() != "PLAN_VALIDATION_FAILED" || !invalid.ToolRetryable() || strings.HasPrefix(invalid.Error(), "[") {
		t.Fatal("validation is still exposed as an opaque JSON string")
	}
	p.Steps[0].Capabilities = []string{"local-fields"}
	if err := RequireValid(p, env); err != nil {
		t.Fatalf("correcting only capability IDs did not resolve validation: %v", err)
	}
}

func TestOnlyExplicitlyReferencedStylesAreValidated(t *testing.T) {
	p := fixture()
	p.Guide = &GuideRef{ID: "guide-with-alternative-styles", Version: "1"}
	p.Data[0].Style = "市域面"
	env := environment()
	lookups := []string{}
	env.StyleExists = func(name string) bool {
		lookups = append(lookups, name)
		return name == "市域面"
	}
	if ds := Validate(p, env); len(ds) != 0 || !reflect.DeepEqual(lookups, []string{"市域面"}) {
		t.Fatalf("validation must check only the chosen style: diagnostics=%+v, lookups=%v", ds, lookups)
	}
	p.Data[0].Style = "不存在的样式"
	if ds := Validate(p, env); len(ds) != 1 || ds[0].Code != "PLAN_STYLE_NOT_FOUND" {
		t.Fatalf("a genuinely missing selected style was accepted: %+v", ds)
	}
	p.Data[0].Style = ""
	lookups = nil
	if ds := Validate(p, env); len(ds) != 0 || len(lookups) != 0 {
		t.Fatalf("an unselected style was treated as required: %+v", ds)
	}
}

func TestOutputReferenceDiagnosticsIdentifyTheContract(t *testing.T) {
	p := fixture()
	p.Data[1].Name = "可自由显示的派生数据名称"
	p.Data[1].Output = "错误的产出标识"
	p.Deliverables[0].Format = "png"
	ds := Validate(p, environment())
	if len(ds) != 2 {
		t.Fatalf("expected producer and deliverable mismatches: %+v", ds)
	}
	if ds[0].Step != "s1" || !strings.Contains(ds[0].Message, "outputs[].label") || !strings.Contains(ds[0].Message, "数据集(type=dataset,format=route_dataset)") {
		t.Fatalf("missing producer correction: %+v", ds[0])
	}
	if !strings.Contains(ds[1].Message, "label、type") || !strings.Contains(ds[1].Message, "PDF(type=file,format=pdf)") {
		t.Fatalf("missing deliverable correction: %+v", ds[1])
	}
	p.Data[1].Output = "数据集"
	p.Deliverables[0].Format = "pdf"
	if err := RequireValid(p, environment()); err != nil {
		t.Fatal(err)
	}
}
