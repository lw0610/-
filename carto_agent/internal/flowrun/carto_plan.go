package flowrun

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/llm"
	"github.com/autocarto/carto_agent/internal/plan"
)

// StartFromPlan skips every legacy form/compiler path. The caller has validated
// a concrete cartographic plan, bound its inputs and selected actual roles.
func (c *Controller) StartFromPlan(ctx context.Context, request StartRequest, executionPlan *flow.FlowPlan) (*RunState, error) {
	if request.SessionID == "" || !filepath.IsAbs(request.SessionDir) || request.CartoPlan == nil || executionPlan == nil || executionPlan.HasErrors() {
		return nil, fmt.Errorf("PLAN_SCHEMA_INVALID: 缺少已校验计划或会话目录")
	}
	frozen, err := plan.Clone(request.CartoPlan)
	if err != nil {
		return nil, err
	}
	request.CartoPlan = frozen
	return c.startPreparedPlan(ctx, request, executionPlan, request.Inputs)
}
func (c *Controller) needsReplan(ctx context.Context, e *execution, reason string) error {
	run := e.state
	if run.Revisions >= plan.MaxRevisions {
		return c.finish(ctx, e, "failed", "已达到三次计划修订上限："+reason)
	}
	run.Status = "needs_replan"
	if err := c.save(ctx, e); err != nil {
		return err
	}
	stepID := ""
	optional := false
	if run.Cursor < len(run.Steps) {
		stepID = run.Steps[run.Cursor].ID
		optional = run.Plan.Steps[run.Cursor].Optional
	}
	c.emit(ctx, run.SessionID, run.ID, "flow.step_failed", map[string]any{"step_id": stepID, "errors": []string{reason}, "next": "replan", "status": "needs_replan", "optional": optional, "revisions": run.Revisions})
	return nil
}
func (c *Controller) advancePlanData(ctx context.Context, e *execution, step flow.StepPlan) error {
	run := e.state
	state := &run.Steps[run.Cursor]
	for state.AutomaticFailures < 2 {
		request := newStepRequest(step, run, e.source, "acquire")
		if err := c.beginStep(ctx, e, request); err != nil {
			return err
		}
		var failure error
		for _, id := range step.DataIDs {
			d, ok := run.CartoPlan.DataByID(id)
			if !ok {
				failure = fmt.Errorf("计划数据不存在：%s", id)
				break
			}
			if value, ok := run.AcquiredInputs[id]; ok && provided(value) {
				continue
			}
			if c.options.DataGet == nil {
				failure = fmt.Errorf("数据服务未就绪")
				break
			}
			result, err := c.options.DataGet(ctx, run, d)
			if err != nil {
				failure = err
				break
			}
			if result.Preparation != nil {
				prepared := result.Preparation
				if prepared.DataID != id || prepared.Role.ID == "" || len(prepared.Messages) == 0 || len(prepared.Selection) == 0 {
					failure = fmt.Errorf("数据获取交接缺少已确认的需求、来源或执行者：%s", d.Name)
					break
				}
				request.Acquisition = prepared
				request.Role = prepared.Role
				request.Step.Role = prepared.Role.ID
				request.Messages = prepared.Messages
				state.Role = prepared.Role.ID
				state.Attempts[len(state.Attempts)-1].DataID = id
				if err := c.save(ctx, e); err != nil {
					return err
				}
				return c.runStepRequest(ctx, e, request)
			}
			if !provided(result.Value) {
				failure = fmt.Errorf("数据服务没有返回可用数据：%s", d.Name)
				break
			}
			bindPlanData(run, d.ID, result)
		}
		if ctx.Err() != nil {
			return c.finish(ctx, e, "cancelled", ctx.Err().Error())
		}
		if failure == nil {
			return c.completePlanData(ctx, e)
		}
		if err := c.failPlanData(ctx, e, failure, true); err != nil {
			return err
		}
		if IsTerminal(run.Status) {
			return nil
		}
	}
	return c.needsReplan(ctx, e, state.LastFailure)
}

// Completion never trusts model text and never silently launches the expert
// again. It collects the exact query's registered, revalidated data instead.
func (c *Controller) finishDataPreparation(ctx context.Context, e *execution, result StepResult, runErr error) error {
	if ctx.Err() != nil {
		return c.finish(ctx, e, "cancelled", ctx.Err().Error())
	}
	run := e.state
	state := &run.Steps[run.Cursor]
	attempt := &state.Attempts[len(state.Attempts)-1]
	if runErr == nil {
		runErr = result.Err
	}
	if runErr != nil {
		return c.failPlanData(ctx, e, runErr, false)
	}
	d, ok := run.CartoPlan.DataByID(attempt.DataID)
	if !ok || c.options.DataGet == nil {
		return c.failPlanData(ctx, e, fmt.Errorf("待核验的数据获取交接不存在：%s", attempt.DataID), false)
	}
	registered, err := c.options.DataGet(ctx, run, d)
	if ctx.Err() != nil {
		return c.finish(ctx, e, "cancelled", ctx.Err().Error())
	}
	if err != nil {
		return c.failPlanData(ctx, e, err, false)
	}
	if registered.Preparation != nil || !provided(registered.Value) {
		return c.failPlanData(ctx, e, fmt.Errorf("DATA_DISCOVERY_INCOMPLETE: 数据获取未登记可核验的结果：%s", d.Name), false)
	}
	bindPlanData(run, d.ID, registered)
	finished := c.options.Now()
	attempt.FinishedAt = &finished
	attempt.Receipt = &Receipt{Status: "completed", Summary: "数据已登记并核验", Outputs: map[string]any{d.Name: registered.Value}}
	attempt.Verification = Verification{OK: true, Errors: []string{}}
	for _, id := range run.Plan.Steps[run.Cursor].DataIDs {
		if !provided(run.AcquiredInputs[id]) {
			state.Status = "pending"
			return c.save(ctx, e)
		}
	}
	return c.completePlanData(ctx, e)
}

func bindPlanData(run *RunState, id string, result DataResult) {
	if run.AcquiredInputs == nil {
		run.AcquiredInputs = map[string]any{}
	}
	run.AcquiredInputs[id] = result.Value
	run.SourceNotes = append(run.SourceNotes, result.Notes...)
	run.ResourceFingerprint, _ = flow.ResourceSnapshotFingerprint(run.Plan)
}

func (c *Controller) completePlanData(ctx context.Context, e *execution) error {
	run := e.state
	step := run.Plan.Steps[run.Cursor]
	state := &run.Steps[run.Cursor]
	outputs := map[string]any{}
	for _, id := range step.DataIDs {
		d, ok := run.CartoPlan.DataByID(id)
		if !ok || !provided(run.AcquiredInputs[id]) {
			return c.failPlanData(ctx, e, fmt.Errorf("获取步骤尚缺少核验结果：%s", id), false)
		}
		outputs[d.Name] = run.AcquiredInputs[id]
	}
	last := &state.Attempts[len(state.Attempts)-1]
	finished := c.options.Now()
	last.FinishedAt = &finished
	last.Receipt = &Receipt{Status: "completed", Summary: step.Title, Outputs: outputs}
	last.Verification = Verification{OK: true, Errors: []string{}}
	state.Status = "completed"
	state.Outputs = outputs
	run.Cursor++
	if err := c.save(ctx, e); err != nil {
		return err
	}
	c.emit(ctx, run.SessionID, run.ID, "flow.step_completed", map[string]any{"step_id": step.ID, "outputs": outputs, "verification": last.Verification})
	return nil
}

func (c *Controller) failPlanData(ctx context.Context, e *execution, failure error, retry bool) error {
	run := e.state
	state := &run.Steps[run.Cursor]
	last := &state.Attempts[len(state.Attempts)-1]
	finished := c.options.Now()
	last.FinishedAt = &finished
	state.AutomaticFailures++
	state.LastFailure = failure.Error()
	state.Status = "failed"
	last.Verification = Verification{Errors: []string{failure.Error()}}
	if llm.IsNonRecoverableServiceError(failure) {
		return c.finish(ctx, e, "failed", failure.Error())
	}
	if err := c.save(ctx, e); err != nil {
		return err
	}
	if !retry {
		return c.needsReplan(ctx, e, state.LastFailure)
	}
	return nil
}

func planStepContext(step flow.StepPlan, run *RunState) string {
	if run.CartoPlan == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("【已知信息】\n")
	for _, f := range run.CartoPlan.Facts {
		value, _ := json.Marshal(f.Value)
		fmt.Fprintf(&b, "- %s：%s（%s）\n", f.Name, value, f.From)
	}
	b.WriteString("【可用数据】\n")
	values := EffectiveInputs(run)
	for _, id := range step.DataUses {
		d, ok := run.CartoPlan.DataByID(id)
		if !ok {
			continue
		}
		value := values[id]
		if d.From == "step" {
			for _, s := range run.Steps {
				if s.ID == d.Step && s.Status == "completed" {
					value = s.Outputs[d.OutputLabel()]
				}
			}
		}
		raw, _ := json.Marshal(value)
		source := d.From
		if d.Kind != "" {
			source += "，" + d.Kind
		}
		fmt.Fprintf(&b, "- %s：%s（%s）\n", d.Name, raw, source)
	}
	if len(run.SourceNotes) > 0 {
		b.WriteString("数据来源说明：" + strings.Join(run.SourceNotes, "；") + "\n")
	}
	b.WriteString("【本步图层】\n")
	for _, d := range run.CartoPlan.Data {
		for _, id := range step.DataUses {
			if id == d.ID {
				fmt.Fprintf(&b, "- %s；样式：%s\n", d.LayerName, d.Style)
			}
		}
	}
	if run.Guide != nil {
		b.WriteString("【流程全文】\n" + run.Guide.Body + "\n")
	}

	return b.String() + "\n"
}

func (c *Controller) checkCartographicResume(ctx context.Context, state *RunState, request ResumeRequest) (*RunState, error) {
	if state.Status != "interrupted" && state.Status != "waiting_user" && state.Status != "needs_replan" {
		return nil, fmt.Errorf("仅可继续暂停的计划")
	}
	if state.Cursor < 0 || state.Cursor >= len(state.Steps) {
		return nil, fmt.Errorf("没有待执行步骤")
	}
	if err := checkResumeSource(state, request.OriginalSource); err != nil {
		return nil, err
	}
	if c.options.ValidatePlanResume != nil {
		if err := c.options.ValidatePlanResume(ctx, state); err != nil {
			return nil, err
		}
	}
	if request.ResourceChoice != "" && request.ResourceChoice != "continue_snapshot" {
		return nil, fmt.Errorf("资源已变化时请选择继续使用保存的引用，或停止并创建新计划")
	}
	fingerprint, err := flow.ResourceSnapshotFingerprint(state.Plan)
	if err != nil {
		return nil, err
	}
	if fingerprint != state.ResourceFingerprint || state.ResourceChange != nil {
		if request.ResourceChoice != "continue_snapshot" || request.ResourceFingerprint != fingerprint {
			change := &ResourceChange{Fingerprint: fingerprint, Message: ResourceChangeMessage}
			state.ResourceChange = change
			if err := c.options.Store.Save(ctx, state); err != nil {
				return nil, err
			}
			return state, &ResourceChangeError{Change: *change}
		}
		state.ResourceFingerprint = fingerprint
		state.ResourceChange = nil
		if err := c.options.Store.Save(ctx, state); err != nil {
			return nil, err
		}
	}
	return state, nil
}

// AttachPaused reconstructs controller bookkeeping without executing a step.
func (c *Controller) AttachPaused(ctx context.Context, state *RunState, source string) error {
	if state == nil || state.CartoPlan == nil {
		return fmt.Errorf("旧流程运行记录只读")
	}
	if state.Status != "needs_replan" && state.Status != "interrupted" && state.Status != "waiting_user" {
		return fmt.Errorf("运行记录未暂停")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing := c.runs[state.ID]; existing != nil {
		return nil
	}
	if c.sessions[state.SessionID] != "" {
		return fmt.Errorf("会话已有执行中的计划")
	}
	saved, err := cloneRun(state)
	if err != nil {
		return err
	}
	_, cancel := context.WithCancel(ctx)
	published, err := cloneRun(saved)
	if err != nil {
		cancel()
		return err
	}
	e := &execution{state: saved, source: source, cancel: cancel, published: published}
	c.runs[state.ID] = e
	c.latest[state.SessionID] = state.ID
	c.sessions[state.SessionID] = state.ID
	return nil
}
