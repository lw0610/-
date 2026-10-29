package flowrun

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
)

// CheckRevision proves that every completed step and each input it consumed is
// unchanged. A new model plan cannot invalidate results that remain visible.
func CheckRevision(run *RunState, next *plan.Plan, executable *flow.FlowPlan) error {
	if run == nil || run.CartoPlan == nil {
		return fmt.Errorf("旧流程记录只读，不能继续执行")
	}
	if run.Status != "needs_replan" && run.Status != "waiting_user" && run.Status != "interrupted" {
		return fmt.Errorf("运行中的计划必须先暂停才能修订")
	}
	if run.Revisions >= plan.MaxRevisions {
		return fmt.Errorf("PLAN_REVISION_LIMIT: 已达到三次修订上限")
	}
	if next == nil || next.ID != run.CartoPlan.ID || next.Version != run.CartoPlan.Version+1 {
		return plan.ErrVersionConflict
	}
	if executable == nil || len(executable.Steps) == 0 {
		return fmt.Errorf("PLAN_SCHEMA_INVALID: 缺少可执行步骤")
	}
	for i, old := range run.Steps {
		if !preservedStep(old.Status) {
			break
		}
		if i >= len(executable.Steps) || old.ID != executable.Steps[i].ID {
			return fmt.Errorf("已完成的步骤不能删除或调换顺序：%s", old.Title)
		}
		before, known := run.CartoPlan.StepByID(old.ID)
		after, exists := next.StepByID(old.ID)
		if known && (!exists || !reflect.DeepEqual(before, after)) {
			return fmt.Errorf("已完成的步骤不能改写：%s", old.Title)
		}
		// Executable snapshots also contain implicit acquire steps absent from
		// the model's visible plan. Their consumed/resolved data is just as
		// immutable as data used by an explicitly declared completed step.
		consumed := append(append(append([]string{}, before.Uses...), before.Data...), before.Inputs...)
		if run.Plan != nil && i < len(run.Plan.Steps) {
			consumed = append(consumed, run.Plan.Steps[i].DataIDs...)
			consumed = append(consumed, run.Plan.Steps[i].DataUses...)
		}
		for _, id := range consumed {
			a, external := run.CartoPlan.DataByID(id)
			if !external {
				continue
			}
			b, found := next.DataByID(id)
			if !found || !samePlanData(a, b) {
				return fmt.Errorf("已完成步骤使用的数据不能改写：%s", a.Name)
			}
		}
	}
	return nil
}
func (c *Controller) Revise(ctx context.Context, sessionID string, next *plan.Plan, executable *flow.FlowPlan, inputs map[string]any, selections map[string]json.RawMessage) (*RunState, error) {
	c.mu.Lock()
	id := c.latest[sessionID]
	e := c.runs[id]
	c.mu.Unlock()
	if e == nil {
		return nil, fmt.Errorf("计划运行不存在")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	run := e.state
	if err := CheckRevision(run, next, executable); err != nil {
		if run.Revisions >= plan.MaxRevisions {
			_ = c.finish(ctx, e, "failed", err.Error())
		}
		return nil, err
	}
	frozen, err := plan.Clone(next)
	if err != nil {
		return nil, err
	}
	completed := 0
	for _, step := range run.Steps {
		if !preservedStep(step.Status) {
			break
		}
		completed++
	}
	states := append([]StepState{}, run.Steps[:completed]...)
	for _, s := range executable.Steps[completed:] {
		state := StepState{ID: s.ID, Index: s.Index, Title: s.Title, Role: s.Role, Status: "pending", Outputs: map[string]any{}, Attempts: []Attempt{}}
		for _, previous := range run.Steps[completed:] {
			if previous.ID == s.ID {
				state.Attempts = previous.Attempts
				state.LastFailure = previous.LastFailure
				state.UserGuidance = previous.UserGuidance
				break
			}
		}
		states = append(states, state)
	}
	for _, d := range run.CartoPlan.Data {
		n, ok := frozen.DataByID(d.ID)
		if !ok || !samePlanData(d, n) {
			delete(run.AcquiredInputs, d.ID)
			delete(run.DataNeeds, d.ID)
			delete(run.InputDetails, d.ID)
			for _, r := range run.Plan.Resources {
				if r.BaseDirFromInput == d.ID {
					delete(run.EntryOverrides, r.Key)
					delete(run.LayerOverrides, r.Key)
				}
			}
		}
	}
	run.CartoPlan = frozen
	run.PlanHistory = append(run.PlanHistory, frozen)
	run.Revisions++
	run.Plan = executable
	run.Steps = states
	run.Cursor = completed
	run.Inputs = inputs
	run.Selections = selections
	run.Status = "running"
	run.FinalVerification = nil
	processCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	// A waiting expert belongs to the previous version. Its delayed completion
	// must never be applied to the revised plan's current cursor.
	for runID, owner := range c.waiting {
		if owner == run.ID {
			delete(c.waiting, runID)
		}
	}
	if e.cancel != nil {
		e.cancel()
	}
	e.cancel = cancel
	c.mu.Unlock()
	if err = c.save(processCtx, e); err != nil {
		return nil, err
	}
	c.emit(processCtx, sessionID, run.ID, "flow.plan_revised", map[string]any{"carto_plan": frozen, "revisions": run.Revisions, "steps": states})
	err = c.advance(processCtx, e)
	snapshot, cloneErr := cloneRun(run)
	if err == nil {
		err = cloneErr
	}
	return snapshot, err
}
func (c *Controller) SkipOptional(ctx context.Context, sessionID string) (*RunState, error) {
	c.mu.Lock()
	e := c.runs[c.latest[sessionID]]
	c.mu.Unlock()
	if e == nil {
		return nil, fmt.Errorf("计划运行不存在")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	run := e.state
	if run.CartoPlan == nil || run.Status != "needs_replan" || run.Cursor >= len(run.Steps) || !run.Plan.Steps[run.Cursor].Optional {
		return nil, fmt.Errorf("只有失败的可选步骤可以跳过")
	}
	run.Steps[run.Cursor].Status = "skipped_after_failure"
	run.Result.Issues = append(run.Result.Issues, run.Steps[run.Cursor].Title+"：用户选择跳过")
	run.Cursor++
	run.Status = "running"
	processCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	e.cancel = cancel
	c.mu.Unlock()
	if err := c.save(processCtx, e); err != nil {
		return nil, err
	}
	err := c.advance(processCtx, e)
	snapshot, cloneErr := cloneRun(run)
	if err == nil {
		err = cloneErr
	}
	return snapshot, err
}

func preservedStep(status string) bool {
	return status == "completed" || status == "skipped" || status == "skipped_after_failure"
}

// Legacy snapshots identify a step output by Name. Making that implicit
// reference explicit during normalization does not change completed inputs.
func samePlanData(a, b plan.Data) bool {
	if a.From == "step" {
		a.Output = a.OutputLabel()
	}
	if b.From == "step" {
		b.Output = b.OutputLabel()
	}
	return reflect.DeepEqual(a, b)
}

// PreserveCompleted is applied before validating a new model proposal. Completed
// work is execution state, not text the model must reproduce byte for byte.
func PreserveCompleted(run *RunState, next *plan.Plan) {
	if run == nil || run.CartoPlan == nil || next == nil {
		return
	}
	completed := map[string]bool{}
	consumed := map[string]bool{}
	steps := []plan.Step{}
	for _, state := range run.Steps {
		if !preservedStep(state.Status) {
			break
		}
		if old, ok := run.CartoPlan.StepByID(state.ID); ok {
			steps = append(steps, old)
			completed[old.ID] = true
			for _, id := range old.Inputs {
				consumed[id] = true
			}
		}
	}
	for _, step := range next.Steps {
		if !completed[step.ID] {
			steps = append(steps, step)
		}
	}
	next.Steps = steps
	data := []plan.Data{}
	for _, d := range run.CartoPlan.Data {
		if consumed[d.ID] {
			data = append(data, d)
		}
	}
	for _, d := range next.Data {
		if !consumed[d.ID] {
			data = append(data, d)
		}
	}
	next.Data = data
}
