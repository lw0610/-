package flowrun

import (
	"context"
	"github.com/autocarto/carto_agent/internal/flow"
	"testing"
)

func TestV4FlowRoleOverlayAndUnresolvedBranch(t *testing.T) {
	p := testPlan(1)
	p.FlowRoles = []flow.FlowRoleSpec{{ID: "flow_role_test", DisplayName: "临时角色", Kind: "agent", ToolPackages: []string{"data"}}}
	p.Steps[0].Role = "flow_role_test"
	p.Steps[0].GrantedPackages = []string{"extra"}
	c, request, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Overlay == nil || r.Overlay.FlowRole == nil || r.Role.ID != "flow_role_test" || r.Overlay.ExtraPackages[0] != "extra" {
			t.Fatalf("%+v", r)
		}
		return completed(r.Step), nil
	}, nil)
	run, err := c.Start(context.Background(), request)
	if err != nil || run.Status != "completed" {
		t.Fatal(run, err)
	}
	p = testPlan(2)
	p.Steps[1].Unresolved = "FLOW_ROLE_NOT_FOUND"
	p.Steps[1].Condition = &flow.ConditionDoc{Text: "前步完成", Rule: &flow.ConditionRule{Op: "step_status", Step: "s1", Status: "completed"}}
	stepCalls, choiceCalls := 0, 0
	c, request, _ = newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Mode == "failure_choice" {
			choiceCalls++
			return StepResult{Interrupted: true}, nil
		}
		stepCalls++
		return completed(r.Step), nil
	}, nil)
	run, err = c.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if stepCalls != 1 || choiceCalls != 1 || run.Status != "waiting_user" || len(run.Steps[1].Attempts) != 2 || run.Steps[1].Attempts[0].Receipt.Status != "failed" {
		t.Fatalf("calls %d/%d run %+v", stepCalls, choiceCalls, run)
	}
}
func TestV4InputRoleBlockIdentifiesStep(t *testing.T) {
	p := testPlan(1)
	p.Inputs = []flow.InputPlan{{Key: "run", Type: "boolean"}}
	p.Steps[0].Condition = &flow.ConditionDoc{Rule: &flow.ConditionRule{Op: "truthy", Input: "run"}}
	p.Steps[0].Unresolved = "FLOW_ROLE_DISABLED"
	c, req, _ := newTestController(t, p, func(context.Context, StepRequest) (StepResult, error) {
		t.Fatal("must not execute")
		return StepResult{}, nil
	}, nil)
	req.Inputs = map[string]any{"run": true}
	_, err := c.Start(context.Background(), req)
	invalid, ok := err.(*InputError)
	if !ok || len(invalid.Reasons) != 1 || invalid.Reasons[0].Step != "s1" {
		t.Fatalf("%T %v", err, err)
	}
}
func TestV4RestoreRequiresCurrentOverlayAuthority(t *testing.T) {
	p := testPlan(1)
	p.Steps[0].GrantedPackages = []string{"extra"}
	c, _, _ := newTestController(t, p, func(context.Context, StepRequest) (StepResult, error) { return StepResult{}, nil }, nil)
	if c.validateResumeOverlays(context.Background(), p, "test") == nil {
		t.Fatal("resume without authority accepted")
	}
	p.Steps[0].GrantedPackages = nil
	if err := c.validateResumeOverlays(context.Background(), p, "test"); err != nil {
		t.Fatal(err)
	}
}
