package flowrun

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/llm"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/cloudwego/eino/schema"
)

type execution struct {
	mu         sync.Mutex
	snapshotMu sync.RWMutex
	published  *RunState
	state      *RunState
	source     string
	cancel     context.CancelFunc
}
type Controller struct {
	options  Options
	mu       sync.Mutex
	runs     map[string]*execution
	sessions map[string]string
	waiting  map[string]string
	latest   map[string]string
}

func NewController(options Options) (*Controller, error) {
	if options.Runner == nil || options.Store == nil {
		return nil, fmt.Errorf("计划控制器缺少步骤运行器或存储")
	}
	if options.Lifecycle == nil {
		options.Lifecycle = context.Background()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Executors == nil {
		options.Executors = NewExecutorRegistry()
	}
	return &Controller{options: options, runs: map[string]*execution{}, sessions: map[string]string{}, waiting: map[string]string{}, latest: map[string]string{}}, nil
}

// The plan and inputs are already checked. In particular, restarting after a
// resource confirmation must not resolve a new revision after cancelling the old run.
func (c *Controller) startPreparedPlan(ctx context.Context, request StartRequest, plan *flow.FlowPlan, inputs map[string]any) (*RunState, error) {
	var err error
	id := request.FlowRunID
	if id == "" {
		id, err = c.NewRunID()
		if err != nil {
			return nil, err
		}
	}
	path, err := RunPath(request.SessionDir, id)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Lstat(path); statErr == nil {
		return nil, fmt.Errorf("流程运行记录已存在")
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	origin := request.Origin
	if origin == "" {
		origin = "direct"
	}
	if origin != "direct" && origin != "orchestrator" {
		return nil, fmt.Errorf("流程启动来源无效")
	}
	state := &RunState{CartoPlan: request.CartoPlan, Guide: request.Guide, Selections: request.Selections, APIVersion: APIVersion, ID: id, SessionID: request.SessionID, SessionDir: request.SessionDir, ProjectRoot: request.ProjectRoot, ProjectPath: request.ProjectPath, FlowID: plan.FlowID, FlowVersion: plan.FlowVersion, PlanCacheKey: compileKey(plan), Scheme: plan.Scheme, Origin: origin, Status: "running", StartedAt: c.options.Now(), Inputs: inputs, Steps: []StepState{}, Plan: plan, RunJSONPath: path, Result: RunResult{Deliverables: map[string]any{}, Issues: []string{}}}
	if state.CartoPlan != nil {
		state.PlanHistory = append(state.PlanHistory, state.CartoPlan)
		state.ResourceFingerprint, _ = flow.ResourceSnapshotFingerprint(plan)
	}
	for _, step := range plan.Steps {
		state.Steps = append(state.Steps, StepState{ID: step.ID, Index: step.Index, Title: step.Title, Role: step.Role, Status: "pending", Attempts: []Attempt{}, Outputs: map[string]any{}})
	}
	if len(state.Steps) == 0 {
		return nil, fmt.Errorf("流程没有步骤")
	}
	processCtx, cancel := context.WithCancel(ctx)
	e := &execution{state: state, source: request.OriginalSource, cancel: cancel}
	c.mu.Lock()
	if _, exists := c.runs[id]; exists {
		c.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("流程运行编号已存在")
	}
	if active := c.sessions[request.SessionID]; active != "" {
		c.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("当前会话已有进行中的流程 %s", active)
	}
	c.sessions[request.SessionID] = id
	c.runs[id] = e
	c.latest[request.SessionID] = id
	c.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if err = c.save(processCtx, e); err != nil {
		c.release(e)
		cancel()
		return nil, err
	}

	steps := []map[string]any{}
	for _, step := range plan.Steps {
		steps = append(steps, map[string]any{"id": step.ID, "index": step.Index, "title": step.Title, "role_label": step.RoleLabel, "kind": step.Kind})
	}
	c.emit(processCtx, state.SessionID, id, "flow.started", map[string]any{"flow_run_id": id, "flow_id": state.FlowID, "name": plan.Name, "steps": steps, "plan": plan})
	err = c.advance(processCtx, e)
	snapshot, cloneErr := cloneRun(state)
	if err == nil {
		err = cloneErr
	}
	return snapshot, err
}

func (c *Controller) NewRunID() (string, error) {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "fr-" + c.options.Now().Format("20060102-150405") + "-" + hex.EncodeToString(bytes), nil
}
func (c *Controller) save(ctx context.Context, e *execution) error {
	if err := c.options.Store.Save(context.WithoutCancel(ctx), e.state); err != nil {
		e.state.Status = "interrupted"
		if e.cancel != nil {
			e.cancel()
		}
		c.release(e)
		return fmt.Errorf("保存流程运行记录失败: %w", err)
	}
	snapshot, err := cloneRun(e.state)
	if err != nil {
		return err
	}
	e.snapshotMu.Lock()
	e.published = snapshot
	e.snapshotMu.Unlock()
	return nil
}
func (c *Controller) emit(ctx context.Context, session, id, event string, payload map[string]any) {
	if c.options.Events != nil {
		ev := Event{Event: event, SessionID: session, FlowRunID: id, Payload: payload}
		c.mu.Lock()
		active := c.runs[id]
		c.mu.Unlock()
		if active != nil && active.state.CartoPlan != nil {
			ev.PlanID = active.state.CartoPlan.ID
			ev.PlanVersion = active.state.CartoPlan.Version
			payload["plan_id"] = ev.PlanID
			payload["plan_version"] = ev.PlanVersion
		}
		seq := c.options.Events.Emit(context.WithoutCancel(ctx), ev)
		if seq > 0 && id != "" {
			c.mu.Lock()
			e := c.runs[id]
			c.mu.Unlock()
			if e != nil {
				e.state.EventSeq = seq
				_ = c.options.Store.Save(context.WithoutCancel(ctx), e.state)
				snapshot, err := cloneRun(e.state)
				if err == nil {
					e.snapshotMu.Lock()
					e.published = snapshot
					e.snapshotMu.Unlock()
				}
			}
		}
	}
}
func (c *Controller) release(e *execution) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions[e.state.SessionID] == e.state.ID {
		delete(c.sessions, e.state.SessionID)
	}
	for id, run := range c.waiting {
		if run == e.state.ID {
			delete(c.waiting, id)
		}
	}
}

// advance is serialized per flow, while Cancel can cancel its context immediately.
func (c *Controller) advance(ctx context.Context, e *execution) error {
	run := e.state
	for run.Cursor < len(run.Steps) {
		if ctx.Err() != nil {
			return c.finish(ctx, e, "cancelled", ctx.Err().Error())
		}
		step := run.Plan.Steps[run.Cursor]
		if run.CartoPlan != nil && run.CartoPlan.APIVersion == plan.APIVersion {
			if err := c.prepareStepInputs(ctx, e, step); err != nil {
				return err
			}
			if run.Status == "interrupted" || run.Status == "waiting_user" || run.Status == "needs_replan" || IsTerminal(run.Status) {
				return nil
			}
		}
		if step.Kind == "acquire" {
			if err := c.advancePlanData(ctx, e, step); err != nil {
				return err
			}
			if run.Status == "interrupted" || run.Status == "waiting_user" || run.Status == "needs_replan" || IsTerminal(run.Status) {
				return nil
			}
			continue
		}
		state := &run.Steps[run.Cursor]
		state.Condition = ConditionResult{Evaluated: true, Result: true, By: "plan"}
		if step.Unresolved != "" {
			state.LastFailure = step.Unresolved + "：本步所需执行者或工具包不可用，请修订计划。"
			state.Status = "failed"
			return c.needsReplan(ctx, e, state.LastFailure)
		}
		mode := "step"
		if step.Kind == "confirm" {
			mode = "confirm"
		}
		request, err := c.buildRequest(step, run, e.source, mode)
		if err != nil {
			return c.finish(ctx, e, "failed", err.Error())
		}
		if err := c.beginStep(ctx, e, request); err != nil {
			return c.pausePlan(ctx, e, err.Error())
		}
		if err := c.runStepRequest(ctx, e, request); err != nil {
			return err
		}
		if run.Status == "waiting_user" || run.Status == "interrupted" || IsTerminal(run.Status) || run.Status == "needs_replan" {
			return nil
		}
	}
	if c.options.FinalChecks != nil {
		check := c.options.FinalChecks(ctx, run)
		run.FinalVerification = &check
		if !check.OK {
			if len(run.Steps) > 0 {
				run.Cursor = len(run.Steps) - 1
				run.Steps[run.Cursor].Status = "failed"
				run.Steps[run.Cursor].LastFailure = strings.Join(check.Errors, "；")
			}
			return c.needsReplan(ctx, e, strings.Join(check.Errors, "；"))
		}
	}
	return c.finish(ctx, e, "completed", "")
}

// beginStep and runStepRequest are shared by ordinary work and acquisition
// experts. Both use one checkpoint/waiting map and one completion callback.
func (c *Controller) beginStep(ctx context.Context, e *execution, request StepRequest) error {
	run := e.state
	state := &run.Steps[run.Cursor]
	manual := state.PendingManual
	state.PendingManual = false
	state.Attempts = append(state.Attempts, Attempt{RunID: request.RunID, Mode: request.Mode, Role: request.Role.ID, StartedAt: c.options.Now(), UserInitiated: manual, Verification: Verification{Errors: []string{}}})
	if run.CartoPlan != nil && run.CartoPlan.APIVersion == plan.APIVersion && c.options.Tools != nil && request.Mode == "step" {
		var capabilities []string
		for _, planned := range run.CartoPlan.Steps {
			if planned.ID == request.Step.ID {
				capabilities = planned.Capabilities
				break
			}
		}
		if stepUsesQGIS(request.Step, capabilities) {
			observed, err := c.projectLayers(ctx)
			if err != nil {
				return err
			}
			attempt := &state.Attempts[len(state.Attempts)-1]
			attempt.LayersTracked = true
			for _, layer := range observed {
				attempt.BeforeLayers = append(attempt.BeforeLayers, layer.ID)
			}
		}
	}
	state.Status = "running"
	run.Status = "running"
	if err := c.save(ctx, e); err != nil {
		return err
	}
	c.emit(ctx, run.SessionID, run.ID, "flow.step_started", map[string]any{"flow_run_id": run.ID, "step_id": request.Step.ID, "title": request.Step.Title, "kind": request.Step.Kind, "attempt": len(state.Attempts), "run_id": request.RunID, "role": request.Role.ID, "role_label": request.Role.DisplayName})
	return nil
}

func (c *Controller) runStepRequest(ctx context.Context, e *execution, request StepRequest) error {
	run := e.state
	state := &run.Steps[run.Cursor]
	stepCtx := resource.WithScope(ctx, resource.Scope{FlowID: request.FlowID, FlowDir: request.FlowDir, Lock: request.Lock, Linked: request.Resources})
	result, runErr := c.options.Runner.Run(stepCtx, request)

	if ctx.Err() != nil {
		return c.finish(ctx, e, "cancelled", ctx.Err().Error())
	}
	if result.Interrupted && runErr == nil {
		c.mu.Lock()
		c.waiting[request.RunID] = run.ID
		c.mu.Unlock()
		state.Status = "waiting_user"
		run.Status = "waiting_user"
		if err := c.save(ctx, e); err != nil {
			return err
		}
		kind := "interrupt"
		if request.Mode == "confirm" {
			kind = request.Mode
		}
		c.emit(ctx, run.SessionID, run.ID, "flow.step_waiting_user", map[string]any{"flow_run_id": run.ID, "step_id": request.Step.ID, "kind": kind})
		return nil
	}
	return c.handleResult(ctx, e, result, runErr)
}

func newStepRequest(step flow.StepPlan, run *RunState, source, mode string) StepRequest {
	state := &run.Steps[run.Cursor]
	version := 0
	if run.CartoPlan != nil {
		version = run.CartoPlan.Version
	}
	request := StepRequest{PlanVersion: version, Repair: state.UserGuidance, FlowID: run.FlowID, FlowDir: run.Plan.FlowDir, Lock: run.Plan.Lock, SessionID: run.SessionID, SessionDir: run.SessionDir, FlowRunID: run.ID, RunID: fmt.Sprintf("flow-%s-s%02d-v%d-a%d", run.ID, step.Index, version, len(state.Attempts)+1), Step: step, OriginalSource: source, Mode: mode}
	for _, resolved := range run.Plan.Resources {
		request.Resources = append(request.Resources, resolved.ResolvedResource())
	}
	return request
}

func (c *Controller) buildRequest(step flow.StepPlan, run *RunState, source, mode string) (StepRequest, error) {
	state := &run.Steps[run.Cursor]
	request := newStepRequest(step, run, source, mode)
	if mode == "confirm" {
		confirmation := confirmationForStep(step.Instructions, step.Title, false)
		request.Confirmation = &confirmation
		return request, nil
	}
	if step.Unresolved != "" {
		return request, fmt.Errorf("%s：步骤负责角色不可用", step.Unresolved)
	}
	role, err := SelectRole(step, run.Plan.Roles)
	if err != nil {
		return request, err
	}
	request.Role = role
	request.Step.Role = role.ID
	state.Role = role.ID
	if step.Kind == "executor" {
		adapter, ok := c.options.Executors.Get(step.Executor)
		if !ok {
			return request, fmt.Errorf("专用执行器未注册：%s", step.Executor)
		}
		request.Messages, err = adapter.BuildInput(step, run)
		return request, err
	}
	message, truncated, err := BuildStepMessage(step, run)
	state.UpstreamTruncated = truncated
	request.Messages = []*schema.Message{schema.UserMessage(message)}
	return request, err
}

func SelectRole(step flow.StepPlan, roles []flow.RoleSpec) (flow.RoleSpec, error) {
	if step.Kind != "auto" {
		for _, role := range roles {
			if role.ID == step.Role && role.Enabled {
				return role, nil
			}
		}
		return flow.RoleSpec{}, fmt.Errorf("步骤负责角色不可用：%s", step.Role)
	}
	var selected *flow.RoleSpec
	best := -1
	for _, role := range roles {
		if !role.Enabled || role.IsMain || role.Kind == "executor" {
			continue
		}
		score := 0
		for _, tool := range step.SuggestedTools {
			for _, allowed := range role.Tools {
				if tool == allowed {
					score++
					break
				}
			}
		}
		if score > best {
			copy := role
			selected = &copy
			best = score
		}
	}
	if selected == nil {
		return flow.RoleSpec{}, fmt.Errorf("没有可执行本步的通用角色")
	}
	return *selected, nil
}

func (c *Controller) handleResult(ctx context.Context, e *execution, result StepResult, runErr error) error {
	run := e.state
	state := &run.Steps[run.Cursor]
	step := run.Plan.Steps[run.Cursor]
	attempt := &state.Attempts[len(state.Attempts)-1]
	if attempt.Mode == "prepare" {
		return c.finishPreparationV2(ctx, e, result, runErr)
	}
	if attempt.Mode == "acquire" {
		return c.finishDataPreparation(ctx, e, result, runErr)
	}
	now := c.options.Now()
	attempt.FinishedAt = &now
	var receipt Receipt
	var err error
	receiptFailed := false
	if runErr == nil {
		runErr = result.Err
	}
	if runErr != nil {
		err = runErr
	} else if attempt.Mode == "confirm" {
		choice, choiceErr := parseChoice(result.LastContent)
		attempt.UserChoice = &choice
		if choiceErr != nil {
			err = choiceErr
		} else if choice.Choice == "reject" || choice.Choice == "不确认" || choice.Choice == "否" {
			receipt = Receipt{Status: "blocked", Outputs: map[string]any{}, Issues: []string{"用户未确认"}}
		} else {
			outputs := map[string]any{}
			for _, output := range step.ExpectedOutputs {
				outputs[output.Label] = choice.Choice
			}
			receipt = Receipt{Status: "completed", Summary: "用户已确认", Outputs: outputs, Issues: []string{}}
		}
	} else if step.Kind == "executor" {
		adapter, ok := c.options.Executors.Get(step.Executor)
		if !ok {
			err = fmt.Errorf("专用执行器未注册")
		} else {
			receipt, err = adapter.ToReceipt(step, result.LastContent)
		}
	} else {
		var repair *ReceiptRepair
		receipt, repair, err = readReceipt(ctx, step, result, c.options.Model)
		receiptFailed = err != nil
		attempt.Repair = repair
		attempt.ReceiptRepaired = repair != nil && err == nil
	}
	if err == nil {
		attempt.Receipt = &receipt
		attempt.Verification = VerifyOutputs(ctx, step, receipt, run.ProjectRoot, c.options.Tools, result.StyleTools...)
		if receipt.Status == "completed" && attempt.Verification.OK {
			if err := c.cleanupAttemptLayers(ctx, attempt, step, &receipt); err != nil {
				return c.pausePlan(ctx, e, err.Error())
			}
			c.registerFlowOutputs(run, step.ID, receipt.Outputs)
			state.Outputs = receipt.Outputs
			state.Status = "completed"
			run.Cursor++
			run.Result.Issues = append(run.Result.Issues, receipt.Issues...)
			for _, output := range step.ExpectedOutputs {
				if output.Type == "review_final" {
					if review, reviewErr := reviewObject(receipt.Outputs[output.Label]); reviewErr == nil && review["final_passed"] == false {
						run.Result.Issues = append(run.Result.Issues, "质量终审未通过")
					}
				}
			}
			if err = c.save(ctx, e); err != nil {
				return err
			}
			c.emit(ctx, run.SessionID, run.ID, "flow.step_completed", map[string]any{"flow_run_id": run.ID, "step_id": step.ID, "summary": receipt.Summary, "outputs": receipt.Outputs, "verification": attempt.Verification})
			return nil
		}
		problems := append([]string{}, receipt.Issues...)
		problems = append(problems, attempt.Verification.Errors...)
		if receipt.Status != "completed" {
			problems = append(problems, "回执状态为 "+receipt.Status)
		}
		state.LastFailure = strings.Join(problems, "；")
	} else {
		state.LastFailure = err.Error()
		attempt.Verification = Verification{OK: false, Errors: []string{err.Error()}}
	}
	state.Status = "failed"
	if llm.IsNonRecoverableServiceError(err) {
		return c.pausePlan(ctx, e, state.LastFailure)
	}
	if err := c.cleanupAttemptLayers(ctx, attempt, step, nil); err != nil {
		return c.pausePlan(ctx, e, err.Error())
	}
	if receiptFailed {
		return c.finish(ctx, e, "failed", "本步结果未能整理为回执，已保留工具执行结果，未重新执行本步："+state.LastFailure)
	}
	state.AutomaticFailures++
	limit := 2
	if run.CartoPlan != nil && run.CartoPlan.APIVersion == plan.APIVersion {
		limit = 3
		if runErr != nil && !TransientFailure(runErr) {
			return c.needsReplan(ctx, e, state.LastFailure)
		}
		if step.Kind == "executor" {
			// Dedicated executors own their extraction/compilation retries. Feeding
			// the same fixed arguments back cannot repair a rejected request.
			return c.needsReplan(ctx, e, state.LastFailure)
		}
	}
	if state.AutomaticFailures >= limit {
		return c.needsReplan(ctx, e, state.LastFailure)
	}
	state.UserGuidance = "上次执行已有结果，仅修正以下核验问题，不重复整个步骤：" + state.LastFailure
	state.Status = "pending"
	c.emit(ctx, run.SessionID, run.ID, "flow.step_failed", map[string]any{"flow_run_id": run.ID, "step_id": step.ID, "attempt": len(state.Attempts), "errors": strings.Split(state.LastFailure, "；"), "next": "retry"})
	return c.save(ctx, e)
}

// OnStepRunFinished only accepts a registered interrupted run; repeated delivery is ignored.
func (c *Controller) OnStepRunFinished(ctx context.Context, runID string, result StepResult) (*RunState, error) {
	c.mu.Lock()
	id := c.waiting[runID]
	e := c.runs[id]
	c.mu.Unlock()
	if e == nil {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// A completion may have queued before a revision or cancellation acquired
	// this lock. Validate again against the current attempt, not the old lookup.
	c.mu.Lock()
	valid := c.waiting[runID] == id && c.runs[id] == e && e.state.Status == "waiting_user" && e.state.Cursor >= 0 && e.state.Cursor < len(e.state.Steps)
	if valid {
		step := e.state.Steps[e.state.Cursor]
		valid = step.Status == "waiting_user" && len(step.Attempts) > 0 && step.Attempts[len(step.Attempts)-1].RunID == runID
	}
	if !valid || !result.Interrupted {
		if c.waiting[runID] == id {
			delete(c.waiting, runID)
		}
	}
	c.mu.Unlock()
	if !valid {
		return nil, nil
	}
	if result.Interrupted {
		return cloneRun(e.state)
	}
	processCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	e.cancel = cancel
	c.mu.Unlock()
	e.state.Status = "running"
	if err := c.handleResult(processCtx, e, result, result.Err); err != nil {
		return nil, err
	}
	if !IsTerminal(e.state.Status) && e.state.Status != "needs_replan" {
		if err := c.advance(processCtx, e); err != nil {
			return nil, err
		}
	}
	return cloneRun(e.state)
}

func (c *Controller) Snapshot(sessionID string) (*RunState, error) {
	c.mu.Lock()
	e := c.runs[c.latest[sessionID]]
	c.mu.Unlock()
	if e == nil {
		return nil, nil
	}
	e.snapshotMu.RLock()
	defer e.snapshotMu.RUnlock()
	if e.published == nil {
		return nil, nil
	}
	return cloneRun(e.published)
}

func (c *Controller) Cancel(ctx context.Context, sessionID string) (*RunState, error) {
	c.mu.Lock()
	e := c.runs[c.sessions[sessionID]]
	if e == nil {
		e = c.runs[c.latest[sessionID]]
	}
	if e != nil && e.cancel != nil {
		e.cancel()
	}
	c.mu.Unlock()
	if e == nil {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !IsTerminal(e.state.Status) {
		if err := c.finish(ctx, e, "cancelled", "用户取消流程"); err != nil {
			return nil, err
		}
	}
	return cloneRun(e.state)
}

func (c *Controller) Resume(ctx context.Context, request ResumeRequest) (*RunState, error) {
	state, err := c.CheckResume(ctx, request)
	if err != nil {
		return state, err
	}
	if state.Status != "interrupted" {
		return nil, fmt.Errorf("仅可继续重启后中断的流程")
	}
	processCtx, cancel := context.WithCancel(ctx)
	e := &execution{state: state, source: request.OriginalSource, cancel: cancel}
	c.mu.Lock()
	if c.sessions[state.SessionID] != "" {
		c.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("当前会话已有进行中的流程")
	}
	c.sessions[state.SessionID] = state.ID
	c.runs[state.ID] = e
	c.latest[state.SessionID] = state.ID
	c.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	state.Status = "running"
	state.Steps[state.Cursor].Status = "pending"
	state.Steps[state.Cursor].PendingManual = true
	if err = c.advance(processCtx, e); err != nil {
		return nil, err
	}
	return cloneRun(state)
}

func (c *Controller) finish(ctx context.Context, e *execution, status, issue string) error {
	run := e.state
	if IsTerminal(run.Status) {
		return nil
	}
	if issue != "" {
		run.Result.Issues = append(run.Result.Issues, issue)
	}
	for _, step := range run.Steps {
		if step.Status == "completed" {
			for label, value := range step.Outputs {
				run.Result.Deliverables[label] = value
			}
		}
	}
	if status == "completed" && len(run.Result.Issues) > 0 {
		status = "completed_with_issues"
	}
	run.Status = status
	if (status == "failed" || status == "cancelled") && run.Cursor < len(run.Steps) {
		step := &run.Steps[run.Cursor]
		step.Status = "failed"
		step.LastFailure = issue
		if len(step.Attempts) > 0 && step.Attempts[len(step.Attempts)-1].FinishedAt == nil {
			now := c.options.Now()
			step.Attempts[len(step.Attempts)-1].FinishedAt = &now
		}
	}
	now := c.options.Now()
	run.FinishedAt = &now
	label := map[string]string{"completed": "已完成", "completed_with_issues": "已完成，但有待处理问题", "failed": "未完成", "cancelled": "已取消"}[status]
	lines := []string{fmt.Sprintf("流程「%s」%s。", run.Plan.Name, label)}
	if len(run.SourceNotes) > 0 {
		lines = append(lines, "数据来源说明：\n"+strings.Join(run.SourceNotes, "\n"))
	}
	if run.Plan.Deliverables != "" {
		lines = append(lines, run.Plan.Deliverables)
	}
	raw, _ := json.MarshalIndent(run.Result.Deliverables, "", "  ")
	if len(run.Result.Deliverables) > 0 {
		lines = append(lines, string(raw))
	}
	if len(run.Result.Issues) > 0 {
		lines = append(lines, strings.Join(run.Result.Issues, "；"))
	}
	run.Result.Summary = strings.Join(lines, "\n")
	if err := c.save(ctx, e); err != nil {
		return err
	}
	c.release(e)
	c.mu.Lock()
	if e.cancel != nil {
		e.cancel()
	}
	c.mu.Unlock()
	c.emit(ctx, run.SessionID, run.ID, "flow.finished", map[string]any{"flow_run_id": run.ID, "status": run.Status, "steps": run.Steps, "deliverables": run.Result.Deliverables, "issues": run.Result.Issues, "summary": run.Result.Summary, "run_json_path": run.RunJSONPath})
	return nil
}
