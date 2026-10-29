package websocket

import (
	"context"
	"errors"
	"fmt"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/resource"
)

func resourceChangePending(err error) bool {
	var changed *flowrun.ResourceChangeError
	return errors.As(err, &changed)
}

func (s *DeepServer) checkFlowPendingResources(ctx context.Context, pending *PendingInterrupt, choice, fingerprint string) (*flowrun.RunState, error) {
	if pending.FlowOptions.flowRunID == "" {
		return nil, nil
	}
	_, controller := s.flowDependencies()
	if controller == nil {
		return nil, nil
	}
	dir, err := config.ResolveSessionDir(pending.ProjectPath, pending.SessionID)
	if err != nil {
		return nil, err
	}
	source := ""
	if pending.FlowOptions.originalSource != nil {
		source = *pending.FlowOptions.originalSource
	}
	state, err := controller.CheckResume(ctx, flowrun.ResumeRequest{SessionID: pending.SessionID, SessionDir: dir, FlowRunID: pending.FlowOptions.flowRunID, OriginalSource: source, ResourceChoice: choice, ResourceFingerprint: fingerprint})
	if err != nil {
		return state, err
	}
	if err = checkPendingFlowAttempt(state, pending.CheckpointID); err != nil {
		return state, err
	}
	return state, nil
}

// Reject a question from an earlier plan version before resuming its ADK
// runner, so an old checkpoint cannot perform tools under the new data scope.
func checkPendingFlowAttempt(state *flowrun.RunState, checkpointID string) error {
	if state == nil || state.Status != "waiting_user" || state.Cursor < 0 || state.Cursor >= len(state.Steps) {
		return fmt.Errorf("该确认断点已失效，请使用当前计划的待处理问题")
	}
	step := state.Steps[state.Cursor]
	if step.Status != "waiting_user" || len(step.Attempts) == 0 || checkpointID == "" || step.Attempts[len(step.Attempts)-1].RunID != checkpointID {
		return fmt.Errorf("确认断点与当前计划步骤不匹配，请使用当前计划的待处理问题")
	}
	return nil
}

func withRunResourceSnapshot(ctx context.Context, state *flowrun.RunState) context.Context {
	if state == nil || state.Plan == nil {
		return ctx
	}
	linked := make([]*resource.ResolvedResource, 0, len(state.Plan.Resources))
	for _, item := range state.Plan.Resources {
		linked = append(linked, item.ResolvedResource())
	}
	return resource.WithScope(ctx, resource.Scope{FlowID: state.FlowID, FlowDir: state.Plan.FlowDir, Lock: state.Plan.Lock, Linked: linked})
}
