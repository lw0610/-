package runtimecontrol_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/hitl"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

// These tests use the production Factory's error wrappers and real ADK agent
// and checkpoint state, rather than calling the counter directly.
type correctionModel struct {
	actions []schema.FunctionCall
	calls   int
	inputs  [][]*schema.Message
}

func (m *correctionModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func (m *correctionModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.calls++
	m.inputs = append(m.inputs, messages)
	if m.calls > len(m.actions) {
		return nil, fmt.Errorf("unexpected model round %d after planning boundary", m.calls)
	}
	return schema.AssistantMessage("", []schema.ToolCall{{ID: fmt.Sprint(m.calls), Type: "function", Function: m.actions[m.calls-1]}}), nil
}
func (m *correctionModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	r, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{r}), nil
}

type validatingPlanService struct {
	calls  int
	errors map[int]error
}

func (s *validatingPlanService) ProposePlan(_ context.Context, raw json.RawMessage) (map[string]any, error) {
	s.calls++
	if err := s.errors[s.calls]; err != nil {
		return nil, err
	}
	p, err := plan.Decode(raw)
	if err == nil {
		err = plan.RequireValid(p, plan.Environment{Capabilities: map[string]bool{"local-export": true}})
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"accepted": true}, nil
}
func (s *validatingPlanService) RevisePlan(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	return s.ProposePlan(ctx, raw)
}

type correctionCapture struct {
	errors         []error
	successCleared bool
}

func newCorrectionRuntime(t *testing.T, m *correctionModel, service *validatingPlanService) (context.Context, *agentruntime.Runtime, *correctionCapture) {
	t.Helper()
	ctx := runtimecontrol.WithPlanService(context.Background(), service)
	capture := &correctionCapture{}
	var tools []tool.BaseTool
	names := []string{runtimecontrol.ToolPlanPropose, runtimecontrol.ToolPlanRevise, runtimecontrol.ToolHITLRequest}
	for _, name := range names {
		name := name
		tools = append(tools, utils.NewTool(&schema.ToolInfo{Name: name, Desc: "plan test operation"}, func(c context.Context, args map[string]any) (string, error) {
			result, err := runtimecontrol.Invoke(c, name, args)
			if name != runtimecontrol.ToolHITLRequest {
				capture.errors = append(capture.errors, err)
				if err == nil {
					_, exists, stateErr := adk.GetRunLocalValue(c, "runtime-control.plan-validation-failures")
					capture.successCleared = !exists && stateErr == nil
				}
			}
			if err != nil {
				return "", err
			}
			body, err := json.Marshal(result)
			return string(body), err
		}))
	}
	root := t.TempDir()
	dir := filepath.Join(root, "planning-tools")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: planning-tools\ndescription: test planning contract\nkind: category\ntools: [" + strings.Join(names, ",") + "]\n---\nPlan tools."
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	factory, err := agentruntime.NewFactory(&agentruntime.FactoryConfig{
		DefaultChatModel: runtimecontrol.WithPlanSubmissionBoundary(m), BaseTools: tools, MaxIterations: 12,
		PluginSkillsDir: root, BuiltinSkillsDir: t.TempDir(), UserSkillsDir: t.TempDir(), ConfigBaseDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := factory.Build(ctx, "correction-fixture", &config.AgentConfigFile{Runtime: &config.RuntimeConfig{
		Agents: map[string]config.AgentDefData{"main": {Kind: "agent", Enabled: true, IsMain: true, ToolPackages: []string{"planning-tools"}, Prompts: map[string]string{"static_instructions": "Submit a valid plan and stop after acceptance."}}},
	}})
	if err != nil || runtime == nil || runtime.PlanAgent == nil {
		t.Fatalf("build runtime: %v %+v", err, runtime)
	}
	return ctx, runtime, capture
}

func planAction(name string, valid bool) schema.FunctionCall {
	var inputs []string
	if !valid {
		inputs = []string{"missing_input"} // A semantic reference error that v2 cannot normalize.
	}
	p := plan.Plan{APIVersion: plan.APIVersion, ID: "cp-budget", Version: 1, Goal: "导出项目",
		Steps: []plan.Step{{ID: "export", Title: "导出", Capabilities: []string{"local-export"}, Inputs: inputs, Instructions: "导出当前项目",
			Produces: []plan.Product{{ID: "export_pdf", Kind: "file", Format: "pdf"}}}},
		Deliverables: []flow.OutputDeclaration{{Label: "export_pdf"}},
	}
	args, _ := json.Marshal(map[string]any{"plan": p})
	return schema.FunctionCall{Name: name, Arguments: string(args)}
}

func drainCorrection(iter *adk.AsyncIterator[*adk.AgentEvent]) (terminal error, exit bool, interrupt *adk.AgentEvent) {
	for {
		event, ok := iter.Next()
		if !ok {
			return
		}
		if event.Err != nil {
			terminal = event.Err
		}
		if event.Action != nil {
			exit = exit || event.Action.Exit
			if event.Action.Interrupted != nil {
				interrupt = event
			}
		}
	}
}

func correctionDetails(t *testing.T, err error) map[string]any {
	t.Helper()
	var coded interface {
		ToolErrorCode() string
		ToolRetryable() bool
		ToolProviderDiagnostics() map[string]any
	}
	if !errors.As(err, &coded) {
		t.Fatalf("missing structured correction diagnostics: %v", err)
	}
	return coded.ToolProviderDiagnostics()
}

func TestPlanCorrectionBudgetStopsRealAgentAndKeepsDiagnostics(t *testing.T) {
	m := &correctionModel{actions: []schema.FunctionCall{
		planAction(runtimecontrol.ToolPlanPropose, false), planAction(runtimecontrol.ToolPlanRevise, false), planAction(runtimecontrol.ToolPlanPropose, false),
	}}
	service := &validatingPlanService{}
	ctx, runtime, capture := newCorrectionRuntime(t, m, service)
	err, exited, _ := drainCorrection(adk.NewRunner(ctx, adk.RunnerConfig{Agent: runtime.PlanAgent}).Query(ctx, "制定计划"))
	if err == nil || exited || m.calls != plan.MaxRevisions || service.calls != plan.MaxRevisions || len(capture.errors) != plan.MaxRevisions {
		t.Fatalf("unbounded plan repair: error=%v exit=%v model=%d service=%d tools=%d", err, exited, m.calls, service.calls, len(capture.errors))
	}
	for i, toolErr := range capture.errors {
		details := correctionDetails(t, toolErr)
		if details["attempts"] != i+1 || details["remaining"] != plan.MaxRevisions-i-1 || !reflect.DeepEqual(details["allowed_capabilities"], []string{"local-export"}) {
			t.Fatalf("lost current constraint/remaining budget: %+v", details)
		}
	}
	last := capture.errors[len(capture.errors)-1]
	var fatal interface{ FatalToolError() bool }
	if !errors.As(last, &fatal) || !fatal.FatalToolError() || correctionDetails(t, last)["next_action"] != "stop" {
		t.Fatalf("last validation error did not stop the real tool wrapper: %v", last)
	}
	mapped, mapErr := (capability.DefaultErrorMapper{}).MapToolError(ctx, runtimecontrol.ToolPlanPropose, last)
	if mapErr != nil || !strings.Contains(mapped.Parts[0].Text, `"code":"PLAN_CORRECTION_EXHAUSTED"`) || !strings.Contains(mapped.Parts[0].Text, `"stop_reason":"plan_correction_budget_exhausted"`) || !strings.Contains(mapped.Parts[0].Text, `"diagnostics"`) {
		t.Fatalf("terminal diagnostics lost mapping: %+v %v", mapped, mapErr)
	}
	// The second model call must receive normal structured, actionable feedback.
	found := false
	for _, message := range m.inputs[1] {
		if message.Role == schema.Tool && strings.Contains(message.Content, `"allowed_capabilities":["local-export"]`) && strings.Contains(message.Content, `"next_action":"correct_plan"`) {
			found = true
		}
	}
	if !found {
		t.Fatal("production tool wrapper hid structured feedback from the model")
	}
}

func TestPlanCorrectionSuccessExitsAndNewRunsStartFresh(t *testing.T) {
	m := &correctionModel{actions: []schema.FunctionCall{
		planAction(runtimecontrol.ToolPlanPropose, false), planAction(runtimecontrol.ToolPlanRevise, false), planAction(runtimecontrol.ToolPlanRevise, false),
	}}
	service := &validatingPlanService{}
	ctx, runtime, capture := newCorrectionRuntime(t, m, service)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: runtime.PlanAgent})
	if err, _, _ := drainCorrection(runner.Query(ctx, "首轮失败")); err == nil {
		t.Fatal("first run did not exhaust correction budget")
	}
	for _, agent := range []adk.Agent{runtime.PlanAgent, runtime.BuildAgent} {
		m.calls, m.inputs, capture.errors = 0, nil, nil
		m.actions[2] = planAction(runtimecontrol.ToolPlanRevise, true)
		err, exit, _ := drainCorrection(adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent}).Query(ctx, "新一轮修正后成功"))
		if err != nil || !exit || m.calls != 3 || !capture.successCleared || len(capture.errors) != 3 || capture.errors[2] != nil {
			t.Fatalf("success did not clear/exit or another run leaked: err=%v exit=%v model=%d capture=%+v", err, exit, m.calls, capture)
		}
		if correctionDetails(t, capture.errors[0])["attempts"] != 1 || correctionDetails(t, capture.errors[1])["attempts"] != 2 {
			t.Fatal("new agent invocation inherited an exhausted budget")
		}
	}
}

func TestPlanCorrectionBudgetSurvivesRealHITLResume(t *testing.T) {
	m := &correctionModel{actions: []schema.FunctionCall{
		planAction(runtimecontrol.ToolPlanPropose, false), planAction(runtimecontrol.ToolPlanRevise, false),
		{Name: runtimecontrol.ToolHITLRequest, Arguments: `{"type":"clarification","title":"补充条件","select_mode":"text"}`},
		planAction(runtimecontrol.ToolPlanPropose, false),
	}}
	service := &validatingPlanService{}
	ctx, runtime, capture := newCorrectionRuntime(t, m, service)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: runtime.PlanAgent, CheckPointStore: runtimepersist.NewMemoryCheckpointStore(nil)})
	err, _, event := drainCorrection(runner.Query(ctx, "制定计划", adk.WithCheckPointID("plan-correction-resume")))
	if err != nil || event == nil || len(capture.errors) != 2 {
		t.Fatalf("expected HITL after two errors: %v %+v", err, event)
	}
	id := ""
	for _, target := range event.Action.Interrupted.InterruptContexts {
		if payload, ok := target.Info.(hitl.InterruptPayload); ok && payload.Title == "补充条件" && target.IsRootCause {
			id = target.ID
		}
	}
	if id == "" {
		t.Fatal("missing HITL resume target")
	}
	iter, err := runner.ResumeWithParams(ctx, "plan-correction-resume", &adk.ResumeParams{Targets: map[string]any{id: map[string]any{"input": "继续原目标"}}})
	if err != nil {
		t.Fatal(err)
	}
	err, exit, _ := drainCorrection(iter)
	if err == nil || exit || m.calls != 4 || service.calls != plan.MaxRevisions || len(capture.errors) != plan.MaxRevisions || correctionDetails(t, capture.errors[2])["attempts"] != plan.MaxRevisions {
		t.Fatalf("HITL reset correction budget: error=%v exit=%v model=%d service=%d errors=%v", err, exit, m.calls, service.calls, capture.errors)
	}
}

func TestPlanCorrectionCountsOnlyInputFailures(t *testing.T) {
	m := &correctionModel{actions: []schema.FunctionCall{
		{Name: runtimecontrol.ToolPlanPropose, Arguments: `{}`},
		planAction(runtimecontrol.ToolPlanPropose, false), // storage error
		planAction(runtimecontrol.ToolPlanRevise, false),  // transport error
		planAction(runtimecontrol.ToolPlanRevise, false),
		planAction(runtimecontrol.ToolPlanPropose, true),
	}}
	storageErr, transportErr := errors.New("cannot save plan"), errors.New("provider connection reset")
	service := &validatingPlanService{errors: map[int]error{1: storageErr, 2: transportErr}}
	ctx, runtime, capture := newCorrectionRuntime(t, m, service)
	err, exit, _ := drainCorrection(adk.NewRunner(ctx, adk.RunnerConfig{Agent: runtime.PlanAgent}).Query(ctx, "制定计划"))
	if err != nil || !exit || m.calls != 5 || len(capture.errors) != 5 {
		t.Fatalf("non-input errors consumed repair budget: %v exit=%v calls=%d errors=%v", err, exit, m.calls, capture.errors)
	}
	if correctionDetails(t, capture.errors[0])["attempts"] != 1 || capture.errors[1] != storageErr || capture.errors[2] != transportErr || correctionDetails(t, capture.errors[3])["attempts"] != 2 {
		t.Fatal("wrong errors counted as plan corrections", capture.errors)
	}
}

func TestPlanCorrectionDoesNotCountDirectCalls(t *testing.T) {
	service := &validatingPlanService{}
	ctx := runtimecontrol.WithPlanService(context.Background(), service)
	var args map[string]any
	_ = json.Unmarshal([]byte(planAction(runtimecontrol.ToolPlanPropose, false).Arguments), &args)
	for i := 0; i < plan.MaxRevisions+2; i++ {
		_, err := runtimecontrol.Invoke(ctx, runtimecontrol.ToolPlanPropose, args)
		var invalid *plan.ValidationError
		var fatal interface{ FatalToolError() bool }
		if !errors.As(err, &invalid) || errors.As(err, &fatal) {
			t.Fatalf("direct caller polluted agent budget: %v", err)
		}
	}
}
