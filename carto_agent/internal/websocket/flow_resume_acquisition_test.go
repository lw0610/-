package websocket

import (
	"context"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
)

type flowResumeGuardRunner func(context.Context, flowrun.StepRequest) (flowrun.StepResult, error)

func (f flowResumeGuardRunner) Run(ctx context.Context, request flowrun.StepRequest) (flowrun.StepResult, error) {
	return f(ctx, request)
}

func TestPendingFlowGuardRejectsOldCheckpointBeforeResumingTools(t *testing.T) {
	s, _, session, p := cartoRuntimeFixture(t)
	ctx := context.Background()
	prepared, err := s.prepareCartographicPlan(ctx, session, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := config.ResolveSessionDir(session.ProjectPath, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	controller, err := flowrun.NewController(flowrun.Options{Store: flowrun.FileRunStore{}, Runner: flowResumeGuardRunner(func(context.Context, flowrun.StepRequest) (flowrun.StepResult, error) {
		calls++
		return flowrun.StepResult{Interrupted: true}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	s.flowController = controller
	state, err := controller.StartFromPlan(ctx, flowrun.StartRequest{SessionID: session.SessionID, SessionDir: dir, OriginalSource: session.OriginalSource, CartoPlan: p, Inputs: prepared.Inputs}, prepared.Executable)
	if err != nil || state.Status != "waiting_user" {
		t.Fatal(state, err)
	}
	oldID := state.Steps[0].Attempts[0].RunID
	pending := &PendingInterrupt{SessionID: session.SessionID, ProjectPath: session.ProjectPath, CheckpointID: oldID, FlowOptions: consumeEventsOptions{flowRunID: state.ID, runID: oldID, originalSource: &session.OriginalSource}}
	if _, err = s.checkFlowPendingResources(ctx, pending, "", ""); err != nil {
		t.Fatal("current checkpoint was rejected", err)
	}
	next, _ := plan.Clone(p)
	next.Version++
	state, err = controller.Revise(ctx, session.SessionID, next, prepared.Executable, prepared.Inputs, nil)
	if err != nil || state.Status != "waiting_user" || calls != 2 {
		t.Fatal(state, err, calls)
	}
	if _, err = s.checkFlowPendingResources(ctx, pending, "", ""); err == nil {
		t.Fatal("old runner could resume tools under the new plan")
	}
	// Revising preserves previous attempts; the resumable checkpoint is the last.
	attempts := state.Steps[state.Cursor].Attempts
	pending.CheckpointID = attempts[len(attempts)-1].RunID
	pending.FlowOptions.runID = pending.CheckpointID
	if _, err = s.checkFlowPendingResources(ctx, pending, "", ""); err != nil || calls != 2 {
		t.Fatal("current runner could not resume or guard ran tools", err, calls)
	}
}
