package flowrun

import (
	"context"
	"fmt"
)

// RestoreWaiting reattaches an existing ADK checkpoint without executing its step.
func (c *Controller) RestoreWaiting(ctx context.Context, request ResumeRequest) (StepRequest, error) {
	state, err := c.CheckResume(ctx, request)
	if err != nil {
		return StepRequest{}, err
	}
	if state.Status != "waiting_user" {
		return StepRequest{}, fmt.Errorf("流程没有可恢复的人工确认断点")
	}
	step := &state.Steps[state.Cursor]
	if len(step.Attempts) == 0 {
		return StepRequest{}, fmt.Errorf("缺少步骤断点记录")
	}
	attempt := step.Attempts[len(step.Attempts)-1]
	result, err := c.buildRequest(state.Plan.Steps[state.Cursor], state, request.OriginalSource, attempt.Mode)
	if err != nil {
		return StepRequest{}, err
	}
	result.RunID = attempt.RunID
	e := &execution{state: state, source: request.OriginalSource}
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing := c.sessions[state.SessionID]; existing != "" {
		if existing == state.ID && c.waiting[attempt.RunID] == state.ID {
			return result, nil
		}
		return StepRequest{}, fmt.Errorf("当前会话已有进行中的流程")
	}
	snapshot, err := cloneRun(state)
	if err != nil {
		return StepRequest{}, err
	}
	e.published = snapshot
	c.runs[state.ID] = e
	c.sessions[state.SessionID] = state.ID
	c.latest[state.SessionID] = state.ID
	c.waiting[attempt.RunID] = state.ID
	return result, nil
}
