package planexec

import (
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
	"reflect"
	"strings"
	"testing"
)

func TestImplicitAcquisitionCannotCollideWithVisibleStep(t *testing.T) {
	p := &plan.Plan{APIVersion: plan.APIVersion, ID: "cp-collision", Version: 1, Goal: "load", Data: []plan.Data{{ID: "d1", Name: "roads", From: "acquire", FindID: "find", Choice: "auto"}}, Steps: []plan.Step{{ID: "acquire-d1", Title: "load", Capabilities: []string{"load"}, Uses: []string{"d1"}, Instructions: "load verified data", Outputs: []flow.OutputDeclaration{{Label: "done", Type: "text"}}}}}
	env := flow.Environment{Roles: []flow.RoleSpec{{ID: "worker", Enabled: true, ToolPackages: []string{"load"}}}, Packages: []flow.PackageSpec{{ID: "load", Enabled: true, Kind: "category"}}}
	executable, _, err := Build(p, BuildOptions{Environment: env})
	if err != nil {
		t.Fatal(err)
	}
	if len(executable.Steps) != 2 || executable.Steps[0].Kind != "acquire" || executable.Steps[0].ID == executable.Steps[1].ID || executable.Steps[1].ID != "acquire-d1" {
		t.Fatal(executable.Steps)
	}
}

func TestBuildChecksExecutorSourceBindingBeforeExecution(t *testing.T) {
	for _, scenario := range []string{"missing", "fact-only", "unreferenced", "wrong-type", "missing-presets", "bound"} {
		t.Run(scenario, func(t *testing.T) {
			p := &plan.Plan{APIVersion: plan.APIVersion, ID: "cp-extract", Version: 1, Goal: "从史料抽取路线",
				Steps: []plan.Step{{ID: "extract", Title: "抽取路线", Capabilities: []string{"route_knowledge"}, Instructions: "抽取原文", Parameters: map[string]string{"route_style": "红蓝对比", "symbols": "军标"},
					Outputs: []flow.OutputDeclaration{{Label: "路线数据集", Type: "dataset"}}}},
			}
			switch scenario {
			case "fact-only":
				p.Facts = []plan.Fact{{Name: "史料来源", From: "conversation", Value: "用户已提供史料"}}
			case "unreferenced", "wrong-type", "missing-presets", "bound":
				p.Data = []plan.Data{{ID: "history", Name: "用户史料", From: "conversation"}}
				if scenario != "unreferenced" {
					p.Steps[0].Uses = []string{"history"}
				}
				if scenario == "wrong-type" {
					p.Data[0].From, p.Data[0].FileID = "session", "existing-document"
				}
				if scenario == "missing-presets" {
					p.Steps[0].Parameters = map[string]string{"route_style": "  "}
				}
			}
			executable, inputs, err := Build(p, BuildOptions{OriginalSource: "从甲地前往乙地"})
			if scenario == "bound" {
				if err != nil || executable.Steps[0].Executor != "route_knowledge" || !reflect.DeepEqual(executable.Steps[0].Refs.Inputs, []string{"history"}) || inputs["history"] == nil {
					t.Fatalf("valid source binding did not reach executor: plan=%+v inputs=%v err=%v", executable, inputs, err)
				}
				return
			}
			invalid, ok := err.(*plan.ValidationError)
			if !ok || executable != nil || len(invalid.Diagnostics) != 1 {
				t.Fatalf("invalid executor input reached execution: plan=%+v err=%v", executable, err)
			}
			diagnostic := invalid.Diagnostics[0]
			if scenario == "missing-presets" {
				if diagnostic.Code != "PLAN_EXECUTOR_PARAMETER_MISSING" || diagnostic.Step != "extract" || !strings.Contains(diagnostic.Message, "route_style") || !strings.Contains(diagnostic.Message, "symbols") {
					t.Fatalf("missing drawing presets were not identified before execution: %+v", diagnostic)
				}
				return
			}
			if diagnostic.Code != "PLAN_EXECUTOR_INPUT_MISSING" || diagnostic.Step != "extract" || !strings.Contains(diagnostic.Message, "from=conversation") || !strings.Contains(diagnostic.Message, "uses") {
				t.Fatalf("missing correctable source diagnostic: %+v", diagnostic)
			}
		})
	}
}

func TestBuildUsesOutputBindingAndNormalizesUnambiguousReferences(t *testing.T) {
	for _, form := range []string{"explicit", "legacy", "sole-output", "step-reference"} {
		t.Run(form, func(t *testing.T) {
			p := &plan.Plan{APIVersion: plan.APIVersion, ID: "cp-output", Version: 1, Goal: "加工并导出",
				Data: []plan.Data{{ID: "derived", Name: "中文显示名", From: "step", Step: "produce", Output: "data_result"}},
				Steps: []plan.Step{
					{ID: "produce", Title: "生成", Capabilities: []string{"load"}, Instructions: "生成数据", Outputs: []flow.OutputDeclaration{{Label: "data_result", Type: "dataset"}}},
					{ID: "consume", Title: "使用", Capabilities: []string{"load"}, Uses: []string{"derived"}, Instructions: "使用上游数据", Outputs: []flow.OutputDeclaration{{Label: "final", Type: "file"}}},
				},
			}
			switch form {
			case "legacy":
				p.Data[0].Name, p.Data[0].Output = "data_result", ""
			case "sole-output":
				p.Data[0].Output = ""
			case "step-reference":
				p.Data, p.Steps[1].Uses = nil, []string{"produce"}
			}
			before, err := plan.Clone(p)
			if err != nil {
				t.Fatal(err)
			}
			env := flow.Environment{Roles: []flow.RoleSpec{{ID: "worker", Enabled: true, ToolPackages: []string{"load"}}}, Packages: []flow.PackageSpec{{ID: "load", Enabled: true, Kind: "category"}}}
			executable, _, err := Build(p, BuildOptions{Environment: env})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(executable.Steps[1].Refs.Outputs, []flow.OutputRef{{Step: "produce", Label: "data_result"}}) || len(executable.Steps[1].DataUses) != 1 {
				t.Fatalf("compiled reference used a display name or omitted the binding: %+v", executable.Steps[1])
			}
			if !reflect.DeepEqual(before, p) {
				t.Fatal("Build changed the caller's visible plan")
			}
		})
	}
}
