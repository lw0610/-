package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/schema"
)

func TestPlanningContextExposesConstraintsWithoutExecutionToolCatalog(t *testing.T) {
	s, _, run, _ := cartoRuntimeFixture(t)
	env, err := s.cartoRuntime.options.LoadEnvironment(context.Background(), run.Scheme)
	if err != nil {
		t.Fatal(err)
	}
	env.PlanningInstructions = "已安装能力手册的规划契约"
	env.Packages[0].Tools = []string{"execution_only_tool"}
	env.Packages = append(env.Packages, flow.PackageSpec{ID: "hidden-control", Hidden: true, Enabled: true}, flow.PackageSpec{ID: "disabled-control", Enabled: false})
	env.DataSources = &datasource.Catalog{Categories: []datasource.Category{{ID: "boundary", Name: "行政区", Geometry: "polygon", Params: map[string]datasource.Parameter{"level": {Required: true, Values: []string{"city", "county"}}}}}}
	s.cartoRuntime.options.LoadEnvironment = func(context.Context, string) (flow.Environment, error) { return env, nil }
	messages, err := s.injectCartographicRules(context.Background(), []*schema.Message{schema.UserMessage("制作专题图")}, run.Scheme, run.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	text := messages[len(messages)-1].Content
	for _, expected := range []string{env.PlanningInstructions, `"id":"read"`, `"level":{"values":["city","county"],"required":true}`, `"capability":"route_knowledge"`, `"required_input_types":["conversation_text"]`, `"required_parameters":["route_style","symbols"]`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("planning context lacks %q: %s", expected, text)
		}
	}
	for _, excluded := range []string{"execution_only_tool", "hidden-control", "disabled-control", `"roles":`} {
		if strings.Contains(text, excluded) {
			t.Fatalf("execution-only catalog leaked into planning context: %s", excluded)
		}
	}
}

func TestPlanSubmissionRejectsExecutorWithoutSourceBinding(t *testing.T) {
	s, _, run, p := cartoRuntimeFixture(t)
	run.Mode = executionModePlan
	p.Data = nil
	p.Steps[0].Capabilities, p.Steps[0].Uses = []string{"route_knowledge"}, nil
	p.Steps[0].Parameters = map[string]string{"route_style": "红蓝对比", "symbols": "军标"}
	p.Facts = []plan.Fact{{Name: "史料来源", From: "conversation", Value: "用户已提供史料"}}
	body, _ := json.Marshal(p)
	result, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), body)
	invalid, ok := err.(*plan.ValidationError)
	if !ok || result != nil || len(invalid.Diagnostics) != 1 || invalid.Diagnostics[0].Code != "PLAN_EXECUTOR_INPUT_MISSING" || invalid.Diagnostics[0].Step != p.Steps[0].ID {
		t.Fatalf("executor without actual source was accepted: result=%v err=%v", result, err)
	}
	stored, err := s.ListPlans(context.Background(), run.SessionID, run.ProjectPath)
	if err != nil || len(stored) != 0 {
		t.Fatalf("invalid proposal was persisted for execution: plans=%v err=%v", stored, err)
	}
}

func TestPlanningContextFailsBeforeModelOnMissingContract(t *testing.T) {
	s, _, run, _ := cartoRuntimeFixture(t)
	for _, failure := range []error{nil, fmt.Errorf("catalog unavailable")} {
		s.cartoRuntime.options.LoadEnvironment = func(context.Context, string) (flow.Environment, error) { return flow.Environment{}, failure }
		messages, err := s.injectCartographicRules(context.Background(), []*schema.Message{schema.UserMessage("制图")}, run.Scheme, run.SessionID)
		if err == nil || len(messages) != 0 {
			t.Fatalf("missing contract continued planning: %v, %v", messages, err)
		}
	}
}

func TestPlanCapabilityDisplayAliasIsStoredAsCanonicalID(t *testing.T) {
	s, _, run, p := cartoRuntimeFixture(t)
	run.Mode = executionModePlan
	p.Steps[0].Capabilities = []string{"读取数据"}
	body, _ := json.Marshal(p)
	result, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), body)
	if err != nil || result["accepted"] != true {
		t.Fatalf("valid display alias rejected: %v, %v", result, err)
	}
	stored, err := s.ListPlans(context.Background(), run.SessionID, run.ProjectPath)
	if err != nil || len(stored) != 1 || stored[0].Plan.Steps[0].Capabilities[0] != "read" {
		t.Fatalf("canonical capability not persisted: %#v, %v", stored, err)
	}
	p.ID = "unknown-tool"
	p.Steps[0].Capabilities = []string{"execution_only_tool"}
	body, _ = json.Marshal(p)
	_, err = (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), body)
	invalid, ok := err.(*plan.ValidationError)
	if !ok || invalid.ToolErrorCode() != "PLAN_VALIDATION_FAILED" {
		t.Fatalf("concrete tool should produce a correctable diagnostic: %v", err)
	}
}

func TestPlanSubmissionPersistsResolvedOutputReferences(t *testing.T) {
	s, _, run, p := cartoRuntimeFixture(t)
	run.Mode = executionModePlan
	p.Steps = append(p.Steps, plan.Step{ID: "consume", Title: "使用上游产出", Capabilities: []string{"read"}, Uses: []string{"work"}, Instructions: "检查真实上游产出", Outputs: []flow.OutputDeclaration{{Label: "检查结论", Type: "text"}}})
	body, _ := json.Marshal(p)
	result, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), body)
	if err != nil || result["accepted"] != true {
		t.Fatalf("unambiguous step reference rejected: %v, %v", result, err)
	}
	stored, err := s.ListPlans(context.Background(), run.SessionID, run.ProjectPath)
	if err != nil || len(stored) != 1 {
		t.Fatalf("plan not persisted: %v", err)
	}
	canonical := stored[0].Plan
	if len(canonical.Data) != 1 || canonical.Data[0].From != "step" || canonical.Data[0].Step != "work" || canonical.Data[0].OutputLabel() != "结果" || canonical.Steps[1].Uses[0] != canonical.Data[0].ID {
		t.Fatalf("canonical output reference missing: %+v", canonical)
	}
}
