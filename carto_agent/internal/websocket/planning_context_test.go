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
	env.Roles[0].Description = "读取并处理用户数据"
	env.Roles = append(env.Roles, flow.RoleSpec{ID: "knowledge", Description: "抽取路线知识", Kind: "executor", Executor: "route_knowledge", Enabled: true, FlowOnly: true})
	env.Packages[0].Tools = []string{"execution_only_tool"}
	env.Packages = append(env.Packages, flow.PackageSpec{ID: "hidden-control", Hidden: true, Enabled: true}, flow.PackageSpec{ID: "disabled-control", Enabled: false})
	env.DataSources = &datasource.Catalog{Categories: []datasource.Category{{ID: "boundary", Name: "行政区", Geometry: "polygon", Params: map[string]datasource.Parameter{"level": {Required: true, Values: []string{"city", "county"}}}}}}
	s.cartoRuntime.options.LoadEnvironment = func(context.Context, string) (flow.Environment, error) { return env, nil }
	messages, err := s.injectTaskContext(context.Background(), []*schema.Message{schema.UserMessage("制作专题图")}, run.Scheme, run.SessionID, executionModePlan)
	if err != nil {
		t.Fatal(err)
	}
	text := messages[len(messages)-1].Content
	for _, expected := range []string{env.PlanningInstructions, `"roles":`, `"id":"worker"`, `"description":"读取并处理用户数据"`, `"tool_packages":["read"]`, `"level":{"values":["city","county"],"required":true}`, `"role":"knowledge"`, `"executor":"route_knowledge"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("planning context lacks %q: %s", expected, text)
		}
	}
	for _, excluded := range []string{"execution_only_tool", "hidden-control", "disabled-control", `"capabilities":`, `"required_parameters":`} {
		if strings.Contains(text, excluded) {
			t.Fatalf("execution-only catalog leaked into planning context: %s", excluded)
		}
	}
}

func TestPlanSubmissionAutomaticallyBindsExtractionSource(t *testing.T) {
	s, _, run, p := cartoRuntimeFixture(t)
	run.Mode = executionModePlan
	env, err := s.cartoRuntime.options.LoadEnvironment(context.Background(), run.Scheme)
	if err != nil {
		t.Fatal(err)
	}
	env.Roles = append(env.Roles, flow.RoleSpec{ID: "knowledge", Kind: "executor", Executor: "route_knowledge", Enabled: true})
	s.cartoRuntime.options.LoadEnvironment = func(context.Context, string) (flow.Environment, error) { return env, nil }
	p.Data = nil
	p.Steps[0].RoleHint = "knowledge"
	p.Steps[0].Capabilities = nil
	p.Steps[0].Inputs = nil
	p.Steps[0].Produces = []plan.Product{{ID: "route", Kind: "dataset"}}
	p.Steps[0].Parameters = nil
	body, _ := json.Marshal(p)
	result, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), body)
	if err != nil || result["accepted"] != true {
		t.Fatal(result, err)
	}
	stored, err := s.ListPlans(context.Background(), run.SessionID, run.ProjectPath)
	if err != nil || len(stored) != 1 || len(stored[0].Plan.Steps[0].Inputs) != 1 || stored[0].Plan.Data[0].Ref != "消息原文" {
		t.Fatal(stored, err)
	}
}

func TestPlanningContextFailsBeforeModelOnMissingContract(t *testing.T) {
	s, _, run, _ := cartoRuntimeFixture(t)
	for _, failure := range []error{nil, fmt.Errorf("catalog unavailable")} {
		s.cartoRuntime.options.LoadEnvironment = func(context.Context, string) (flow.Environment, error) { return flow.Environment{}, failure }
		messages, err := s.injectTaskContext(context.Background(), []*schema.Message{schema.UserMessage("制图")}, run.Scheme, run.SessionID, executionModePlan)
		if err == nil || len(messages) != 0 {
			t.Fatalf("missing contract continued planning: %v, %v", messages, err)
		}
	}
}

func TestBuildContextReadsFullGuideWithoutPlanningContract(t *testing.T) {
	s, _, run, _ := cartoRuntimeFixture(t)
	s.cartoRuntime.options.LoadEnvironment = func(context.Context, string) (flow.Environment, error) {
		t.Fatal("Build must not load the plan controller's contract")
		return flow.Environment{}, nil
	}
	u := schema.UserMessage("按选择的流程完成制图")
	u.Extra = map[string]any{"guide_id": "marching-route"}
	messages, err := s.injectTaskContext(context.Background(), []*schema.Message{u}, run.Scheme, run.SessionID, executionModeBuild)
	if err != nil {
		t.Fatal(err)
	}
	body, err := s.cartoRuntime.options.Guides.ReadContext(context.Background(), "marching-route", s.cartoRuntime.options.Resources)
	if err != nil {
		t.Fatal(err)
	}
	var contextText strings.Builder
	for _, message := range messages {
		contextText.WriteString(message.Content)
	}
	if !strings.Contains(contextText.String(), body) {
		t.Fatal("Build lost the selected flow's full text")
	}
	for _, contract := range []string{"制图计划契约", "carto-plan/v2", `"active_plan"`} {
		if strings.Contains(contextText.String(), contract) {
			t.Fatalf("Build received plan-only instructions: %s", contract)
		}
	}
}

func TestPlanCapabilityDisplayAliasIsStoredAsCanonicalID(t *testing.T) {
	s, _, run, p := cartoRuntimeFixture(t)
	run.Mode = executionModePlan
	p.Steps[0].RoleHint = "worker"
	p.Steps[0].Capabilities = []string{"read"}
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
	if err != nil {
		t.Fatalf("configured role should supply its own tools: %v", err)
	}
}

func TestPlanSubmissionPersistsResolvedOutputReferences(t *testing.T) {
	s, _, run, p := cartoRuntimeFixture(t)
	run.Mode = executionModePlan
	p.Steps = append(p.Steps, plan.Step{ID: "consume", Title: "使用上游产出", Capabilities: []string{"read"}, Inputs: []string{"work"}, Instructions: "检查真实上游产出", Produces: []plan.Product{{ID: "review", Kind: "file", Format: "txt"}}})
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
	if len(canonical.Data) != 0 || len(canonical.Steps[1].Inputs) != 1 || canonical.Steps[1].Inputs[0] != "result" {
		t.Fatalf("v2 did not directly bind producer ID: %+v", canonical)
	}
}
