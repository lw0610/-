package flowrun

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/autocarto/carto_agent/internal/flow"
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
	result, err := c.restoreStepRequest(state, request.OriginalSource, attempt)
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

func (c *Controller) restoreStepRequest(state *RunState, source string, attempt Attempt) (StepRequest, error) {
	step := state.Plan.Steps[state.Cursor]
	if attempt.Mode != "acquire" && attempt.Mode != "prepare" {
		return c.buildRequest(step, state, source, attempt.Mode)
	}
	// The ADK checkpoint owns the expert's messages and tool state. Reconstruct
	// only its trusted identity; DataGet here could fetch again or return a
	// completed registration instead of the interrupted expert's preparation.
	active := state.Steps[state.Cursor]
	roleID := attempt.Role
	if roleID == "" {
		if attempt.Mode == "prepare" {
			return StepRequest{}, fmt.Errorf("旧取数断点未记录实际执行角色，不能用消费步骤角色替代恢复；请修订当前计划后继续，已登记的数据仍可复用")
		}
		roleID = active.Role
	}
	matched := false
	dataIDs := step.DataIDs
	query := state.Selections[attempt.DataID]
	if attempt.Mode == "prepare" {
		// Preparation also includes an input's upstream extent dependencies.
		// Its persisted attempt and frozen need identify that handoff directly.
		matched = true
		query = state.DataNeeds[attempt.DataID]
	}
	for _, id := range dataIDs {
		matched = matched || id == attempt.DataID
	}
	_, exists := state.CartoPlan.DataByID(attempt.DataID)
	if (attempt.Mode == "acquire" && step.Kind != "acquire") || !matched || !exists || roleID == "" || !json.Valid(query) {
		return StepRequest{}, fmt.Errorf("数据获取断点缺少匹配的执行者、数据或冻结查询")
	}
	result := newStepRequest(step, state, source, attempt.Mode)
	role, err := SelectRole(flow.StepPlan{Role: roleID}, state.Plan.Roles)
	if err != nil {
		return StepRequest{}, fmt.Errorf("取数断点角色不在方案快照中，不能替换执行者恢复旧断点，请修订当前计划后继续：%w", err)
	}
	result.Role = role
	result.Step.Role = roleID
	result.Acquisition = &DataPreparation{DataID: attempt.DataID, Role: result.Role}
	if attempt.Mode == "prepare" {
		result.Acquisition.Need = append(json.RawMessage{}, query...)
	} else {
		result.Acquisition.Selection = append(json.RawMessage{}, query...)
	}
	return result, nil
}
