package websocket

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type replanProbeAgent struct {
	calls     int
	input     string
	act       func(context.Context) error
	interrupt bool
}

func (*replanProbeAgent) Name(context.Context) string        { return "planning-probe" }
func (*replanProbeAgent) Description(context.Context) string { return "plan revision" }
func (a *replanProbeAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	a.calls++
	for _, m := range input.Messages {
		a.input += m.Content
	}
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		if a.interrupt {
			gen.Send(adk.Interrupt(ctx, map[string]any{"type": "clarification", "question": "是否保留零项文字说明？"}))
			return
		}
		if a.act != nil {
			if err := a.act(ctx); err != nil {
				gen.Send(&adk.AgentEvent{Err: err})
				return
			}
		}
		gen.Send(adk.EventFromMessage(schema.AssistantMessage("已检查修订要求", nil), nil, schema.Assistant, ""))
	}()
	return iter
}

func (a *replanProbeAgent) Resume(ctx context.Context, _ *adk.ResumeInfo, options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	return a.Run(ctx, &adk.AgentInput{}, options...)
}

func TestReplanningCannotDelegateExecutionOutsideController(t *testing.T) {
	s, _, run, p := cartoRuntimeFixture(t)
	planner, builder := &replanProbeAgent{}, &replanProbeAgent{}
	s.runtime.PlanAgent, s.runtime.BuildAgent = planner, builder
	state := &flowrun.RunState{ID: "fr-replan", CartoPlan: p, Status: "needs_replan", Cursor: 0,
		Steps: []flowrun.StepState{{Status: "failed", Attempts: []flowrun.Attempt{{Verification: flowrun.Verification{Errors: []string{"缺少制图预设"}}}}}}}
	if err := s.replanCartographic(context.Background(), run, state); err == nil || !strings.Contains(err.Error(), "未提交修订，已保留成果") {
		t.Fatalf("ordinary prose cannot complete replanning: %v", err)
	}
	if planner.calls != 1 || builder.calls != 0 || !strings.Contains(planner.input, "此阶段只改计划") {
		t.Fatalf("replanning used execution agent: planner=%d build=%d", planner.calls, builder.calls)
	}
	// A missing planner must not silently revive the same outside-plan execution path.
	s.runtime.PlanAgent = nil
	if err := s.replanCartographic(context.Background(), run, state); err == nil || builder.calls != 0 {
		t.Fatalf("missing planner fell back to build: %v, calls=%d", err, builder.calls)
	}
}

func TestRoutePlanUsesBuiltInDrawingWithoutStyleParameters(t *testing.T) {
	s, _, run, _ := cartoRuntimeFixture(t)
	env, err := s.cartoRuntime.options.LoadEnvironment(context.Background(), run.Scheme)
	if err != nil {
		t.Fatal(err)
	}
	env.Roles = append(env.Roles, flow.RoleSpec{ID: "knowledge", Kind: "executor", Executor: "route_knowledge", Enabled: true})
	s.cartoRuntime.options.LoadEnvironment = func(context.Context, string) (flow.Environment, error) { return env, nil }
	p := materialSourcePlan("消息原文")
	p.Steps[0].Parameters = nil
	p.Steps[0].RoleHint = "knowledge"
	raw, _ := json.Marshal(p)
	answer, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), raw)
	if err != nil || answer["status"] != "ready" || !s.hasPendingCartographicPlan(run.SessionID) {
		t.Fatalf("plan without an external style table must be executable: %v, %v", answer, err)
	}
	prepared := s.takeCartographicPlan(run.SessionID)
	if len(prepared.Executable.Steps[0].Parameters) != 0 {
		t.Fatal("planning injected an unrequested style table")
	}
}
