package flowrun

import (
	"context"
	"errors"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
)

func resourcePlan(fingerprint, path string) *flow.FlowPlan {
	p := testPlan(1)
	p.CompileKey = "structure"
	p.LinkFingerprint = fingerprint
	p.Resources = []flow.ResourcePlan{{Key: "data_list.base", ID: "data_list.base", Type: "data_list", Entry: path}}
	return p
}

func interruptedResourceRun(t *testing.T) (*RunState, StartRequest) {
	t.Helper()
	original := resourcePlan("original", "/original/base.csv")
	c, request, _ := newTestController(t, original, func(context.Context, StepRequest) (StepResult, error) { return StepResult{Interrupted: true}, nil }, nil)
	state, err := c.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	state.Status = "interrupted"
	if err := (FileRunStore{}).Save(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	return state, request
}

func TestResumeResourceChangeRequiresChoiceAndKeepsSnapshot(t *testing.T) {
	old, start := interruptedResourceRun(t)
	current := resourcePlan("new", "/current/base.csv")
	calls := 0
	c, _, events := newTestController(t, current, func(_ context.Context, request StepRequest) (StepResult, error) {
		calls++
		if len(request.Resources) != 1 || request.Resources[0].Entry != "/original/base.csv" {
			t.Fatalf("silently adopted current resources: %+v", request.Resources)
		}
		return completed(request.Step), nil
	}, nil)
	request := ResumeRequest{SessionID: start.SessionID, SessionDir: start.SessionDir, FlowRunID: old.ID}
	paused, err := c.Resume(context.Background(), request)
	var changed *ResourceChangeError
	if !errors.As(err, &changed) || paused.ResourceChange == nil || calls != 0 || !events.has("flow.resources_changed") {
		t.Fatalf("%+v %v calls=%d", paused, err, calls)
	}
	request.ResourceChoice, request.ResourceFingerprint = "continue_snapshot", "outdated"
	if _, err := c.Resume(context.Background(), request); !errors.As(err, &changed) || calls != 0 {
		t.Fatal("stale choice resumed execution", err)
	}
	request.ResourceFingerprint = "new"
	resumed, err := c.Resume(context.Background(), request)
	if err != nil || resumed.Status != "completed" || resumed.ID != old.ID || resumed.Plan.LinkFingerprint != "original" || resumed.ResourceFingerprint != "new" || calls != 1 {
		t.Fatalf("%+v %v calls=%d", resumed, err, calls)
	}
}

func TestRestartAfterResourceChangeCreatesNewRun(t *testing.T) {
	old, start := interruptedResourceRun(t)
	current := resourcePlan("new", "/current/base.csv")
	c, _, _ := newTestController(t, current, func(_ context.Context, request StepRequest) (StepResult, error) {
		if request.Resources[0].Entry != "/current/base.csv" {
			t.Fatal(request.Resources)
		}
		return completed(request.Step), nil
	}, nil)
	request := ResumeRequest{SessionID: start.SessionID, SessionDir: start.SessionDir, FlowRunID: old.ID, ResourceChoice: "restart", ResourceFingerprint: "new"}
	restarted, err := c.Restart(context.Background(), request)
	if err != nil || restarted.ID == old.ID || restarted.Status != "completed" || restarted.Plan.LinkFingerprint != "new" {
		t.Fatalf("%+v %v", restarted, err)
	}
	stored, err := (FileRunStore{}).Load(context.Background(), start.SessionDir, old.ID)
	if err != nil || stored.Status != "cancelled" || stored.Plan.Resources[0].Entry != "/original/base.csv" {
		t.Fatalf("%+v %v", stored, err)
	}
	if _, err := c.Resume(context.Background(), request); err == nil {
		t.Fatal("cancelled snapshot replayed")
	}
}

func TestResumeStructureErrorsCannotBeAcknowledgedAsResourceChanges(t *testing.T) {
	old, start := interruptedResourceRun(t)
	for _, mutate := range []func(*flow.FlowPlan){func(p *flow.FlowPlan) { p.CompileKey = "changed-structure" }, func(p *flow.FlowPlan) {
		p.CompileDiagnostics = []flow.Diagnostic{{Level: "error", Message: "invalid role"}}
	}} {
		current := resourcePlan("new", "/current/base.csv")
		mutate(current)
		c, _, _ := newTestController(t, current, func(context.Context, StepRequest) (StepResult, error) {
			t.Fatal("structure error executed")
			return StepResult{}, nil
		}, nil)
		_, err := c.Resume(context.Background(), ResumeRequest{SessionID: start.SessionID, SessionDir: start.SessionDir, FlowRunID: old.ID, ResourceChoice: "continue_snapshot", ResourceFingerprint: "new"})
		var changed *ResourceChangeError
		if err == nil || errors.As(err, &changed) {
			t.Fatalf("expected structural rejection: %v", err)
		}
	}
}

func TestResumeResourceChoiceClearsPromptAfterResourcesRevert(t *testing.T) {
	old, start := interruptedResourceRun(t)
	current := resourcePlan("new", "/current/base.csv")
	c, _, _ := newTestController(t, current, func(_ context.Context, request StepRequest) (StepResult, error) { return completed(request.Step), nil }, nil)
	request := ResumeRequest{SessionID: start.SessionID, SessionDir: start.SessionDir, FlowRunID: old.ID}
	if _, err := c.Resume(context.Background(), request); err == nil {
		t.Fatal("missing initial change prompt")
	}
	current.LinkFingerprint, current.Resources[0].Entry = "original", "/original/base.csv"
	request.ResourceChoice, request.ResourceFingerprint = "continue_snapshot", "new"
	changed, err := c.Resume(context.Background(), request)
	if err == nil || changed.ResourceChange == nil || changed.ResourceChange.Fingerprint != "original" {
		t.Fatalf("%+v %v", changed, err)
	}
	request.ResourceFingerprint = "original"
	completed, err := c.Resume(context.Background(), request)
	if err != nil || completed.ResourceChange != nil || completed.Status != "completed" {
		t.Fatalf("%+v %v", completed, err)
	}
}

type resumeCompilerFunc func() (*flow.FlowPlan, error)

func (f resumeCompilerFunc) EnsureRunnable(context.Context, string, string) (*flow.FlowPlan, error) {
	return f()
}

func TestRestartResourceChangesDuringConfirmationPreserveOldRun(t *testing.T) {
	old, start := interruptedResourceRun(t)
	calls, executions := 0, 0
	c, _, events := newTestController(t, resourcePlan("unused", "/unused"), func(_ context.Context, step StepRequest) (StepResult, error) {
		executions++
		if step.Resources[0].Entry != "/third/base.csv" {
			t.Fatal(step.Resources)
		}
		return completed(step.Step), nil
	}, nil)
	c.options.Compiler = resumeCompilerFunc(func() (*flow.FlowPlan, error) {
		calls++
		if calls == 1 {
			return resourcePlan("second", "/second/base.csv"), nil
		}
		return resourcePlan("third", "/third/base.csv"), nil
	})
	request := ResumeRequest{SessionID: start.SessionID, SessionDir: start.SessionDir, FlowRunID: old.ID, ResourceChoice: "restart", ResourceFingerprint: "second"}
	paused, err := c.Restart(context.Background(), request)
	var changed *ResourceChangeError
	if !errors.As(err, &changed) || paused.ID != old.ID || paused.ResourceChange.Fingerprint != "third" || executions != 0 || !events.has("flow.resources_changed") {
		t.Fatalf("run restarted without confirming latest resource: %+v %v executions=%d", paused, err, executions)
	}
	stored, err := (FileRunStore{}).Load(context.Background(), start.SessionDir, old.ID)
	if err != nil || stored.Status != "interrupted" {
		t.Fatalf("old run ended before re-confirmation: %+v %v", stored, err)
	}
	request.ResourceFingerprint = "third"
	restarted, err := c.Restart(context.Background(), request)
	if err != nil || restarted.ID == old.ID || restarted.Plan.LinkFingerprint != "third" || executions != 1 || calls != 4 {
		t.Fatalf("restart must use the validated plan without a third resolution: %+v %v calls=%d executions=%d", restarted, err, calls, executions)
	}
}

func TestRestartStructureChangesDuringConfirmationPreserveOldRun(t *testing.T) {
	old, start := interruptedResourceRun(t)
	calls := 0
	c, _, _ := newTestController(t, resourcePlan("unused", "/unused"), func(context.Context, StepRequest) (StepResult, error) {
		t.Fatal("changed structure executed")
		return StepResult{}, nil
	}, nil)
	c.options.Compiler = resumeCompilerFunc(func() (*flow.FlowPlan, error) {
		calls++
		plan := resourcePlan("second", "/second/base.csv")
		if calls > 1 {
			plan.CompileKey = "changed-structure"
		}
		return plan, nil
	})
	_, err := c.Restart(context.Background(), ResumeRequest{SessionID: start.SessionID, SessionDir: start.SessionDir, FlowRunID: old.ID, ResourceChoice: "restart", ResourceFingerprint: "second"})
	if err == nil {
		t.Fatal("structure changed between validation and restart")
	}
	stored, err := (FileRunStore{}).Load(context.Background(), start.SessionDir, old.ID)
	if err != nil || stored.Status != "interrupted" {
		t.Fatalf("old run ended before structure validation: %+v %v", stored, err)
	}
}
