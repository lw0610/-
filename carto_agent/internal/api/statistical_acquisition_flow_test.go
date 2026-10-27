package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
)

func assertStatisticalAgentAcquisitionPlan(t *testing.T, plan *flow.FlowPlan) {
	t.Helper()
	if plan.Acquisition == nil || len(plan.Acquisition.Items) != 1 || plan.Acquisition.Items[0].Requirement != "行政区数据" || plan.Acquisition.Items[0].Category != "admin.division" {
		t.Fatalf("only administrative boundaries should use the source catalog: %+v", plan.Acquisition)
	}
	if len(plan.Requirements) != 1 || plan.Requirements[0].Name != "行政区数据" {
		t.Fatalf("statistics must not depend on an NBS portal adapter: %+v", plan.Requirements)
	}
	foundTable := false
	for _, input := range plan.Inputs {
		if input.Key == "统计表" {
			foundTable = true
			if input.Required || input.Acquirable || input.AcquireAvailable {
				t.Fatalf("statistics input should be optional with agent acquisition: %+v", input)
			}
		}
	}
	if !foundTable {
		t.Fatal("provided statistical tables must remain a reusable input")
	}
	steps := builtinBusinessSteps(plan)
	if len(steps) != 5 || steps[0].Kind != "agent" || steps[0].Condition == nil || steps[0].Condition.Rule == nil {
		t.Fatalf("missing ordinary conditional agent step: %+v", steps)
	}
	rule := steps[0].Condition.Rule
	if rule.Op != "and" || len(rule.Args) != 2 || rule.Args[0].Op != "not_provided" || rule.Args[0].Input != "统计表" || rule.Args[1].Op != "not_provided" || rule.Args[1].Input != "已有专题图层" {
		t.Fatalf("missing deterministic reuse condition: %+v", rule)
	}
	fileOutput := false
	for _, output := range steps[0].ExpectedOutputs {
		if output.Label == "联网统计表" {
			fileOutput = output.Type == "file" && output.Format == "csv" && output.Required
		}
	}
	if !fileOutput {
		t.Fatalf("acquisition must deliver a verified CSV file: %+v", steps[0].ExpectedOutputs)
	}
	for index, step := range steps {
		hasBrowser := false
		for _, capability := range step.RequiredPackages {
			hasBrowser = hasBrowser || capability == "browser-data-acquisition"
		}
		if hasBrowser != (index == 0) {
			t.Fatalf("browser capability must be required only in acquisition: step=%s packages=%v", step.ID, step.RequiredPackages)
		}
	}
	hasOutputRef := false
	for _, ref := range steps[1].Refs.Outputs {
		hasOutputRef = hasOutputRef || ref.Step == steps[0].ID && ref.Label == "联网统计表"
	}
	if !hasOutputRef {
		t.Fatalf("data preparation lost its acquisition output reference: %+v", steps[1].Refs)
	}
}

func TestBuiltinStatisticalMissingTableUsesAgentAcquisition(t *testing.T) {
	a, _, _, home := realFlowAPI(t)
	store, err := a.flowStore()
	if err != nil {
		t.Fatal(err)
	}
	library, err := a.resourceStore()
	if err != nil {
		t.Fatal(err)
	}
	compiler := flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: filepath.Join(home, "cache"), LoadEnvironment: a.loadFlowEnvironment})
	plan, err := compiler.BasePlan(context.Background(), "statistical-choropleth", "compile-scheme")
	if err != nil || plan.HasErrors() {
		t.Fatalf("real builtin flow failed to compile: plan=%+v error=%v", plan, err)
	}
	assertStatisticalAgentAcquisitionPlan(t, plan)
	env, err := a.loadFlowEnvironment(context.Background(), "compile-scheme")
	if err != nil {
		t.Fatal(err)
	}
	planning := env.AcquisitionEnvironment()
	planning.AutoAcquire = "allow"
	boundary := filepath.Join(home, "provided-boundary.geojson")
	if err := os.WriteFile(boundary, []byte(`{"type":"FeatureCollection","features":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	values := map[string]any{"行政区数据": boundary, "区域": "全国", "行政区级别": "省", "统计年份": 2024, "指标": "地区生产总值"}
	linked, problems := flowrun.ValidateInputsWithAcquisition(context.Background(), plan, values, home, planning, nil)
	for _, problem := range problems {
		if problem.Level == "error" {
			t.Fatalf("missing optional statistics table blocked startup: %+v", problems)
		}
	}
	if len(linked.Acquisition.Items) != 1 || linked.Acquisition.Items[0].Mode != "user" {
		t.Fatalf("provided boundary should be reused without adding NBS acquisition: %+v", linked.Acquisition)
	}

	values["行政区数据"] = map[string]any{"$acquire": true}
	acquisition, issues := acquire.Plan(context.Background(), plan, values, planning, nil)
	if len(issues) != 0 || len(acquisition.Items) != 1 || acquisition.Items[0].Chosen == nil || acquisition.Items[0].Chosen.SourceID != "source.builtin.cn-admin" {
		t.Fatalf("existing boundary auto acquisition changed: plan=%+v issues=%+v", acquisition, issues)
	}

	// An old form's source-catalog choice is not a real file and cannot be
	// treated as one; clients must discard obsolete acquisition defaults.
	values["统计表"] = map[string]any{"$acquire": true}
	_, err = flowrun.ResolveInputs(plan, values, "", home)
	var invalid *flowrun.InputError
	if !errors.As(err, &invalid) {
		t.Fatalf("obsolete acquisition marker should be rejected explicitly: %v", err)
	}
	for _, reason := range invalid.Reasons {
		if reason.Input == "统计表" && reason.Code == "FLOW_INPUT_ACQUIRE_UNSUPPORTED" {
			return
		}
	}
	t.Fatalf("missing explanation for obsolete table acquisition marker: %+v", invalid.Reasons)
}
