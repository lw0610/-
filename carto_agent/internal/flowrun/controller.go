package flowrun

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/flow"
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
	if options.Compiler == nil || options.Runner == nil || options.Store == nil {
		return nil, fmt.Errorf("流程控制器缺少编译器、步骤运行器或存储")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Executors == nil {
		options.Executors = NewExecutorRegistry()
	}
	return &Controller{options: options, runs: map[string]*execution{}, sessions: map[string]string{}, waiting: map[string]string{}, latest: map[string]string{}}, nil
}

func (c *Controller) Start(ctx context.Context, request StartRequest) (*RunState, error) {
	if request.SessionID == "" || !filepath.IsAbs(request.SessionDir) {
		return nil, fmt.Errorf("会话编号和绝对会话目录不能为空")
	}
	plan, err := c.options.Compiler.EnsureRunnable(ctx, request.FlowID, request.Scheme)
	if err != nil || plan == nil || plan.HasErrors() {
		payload := map[string]any{"flow_id": request.FlowID, "reasons": []StartupIssue{{Code: "FLOW_COMPILE_FAILED", Message: fmt.Sprint(err)}}}
		var invalid *flow.ValidationError
		if errors.As(err, &invalid) {
			payload["reasons"] = invalid.Diagnostics
		} else if plan != nil && plan.HasErrors() {
			payload["reasons"] = plan.Diagnostics
			err = &flow.ValidationError{Plan: plan, Diagnostics: plan.Diagnostics}
		}
		c.emit(ctx, request.SessionID, "", "flow.blocked", payload)
		if err == nil {
			err = fmt.Errorf("流程计划为空")
		}
		return nil, err
	}
	return c.startPlan(ctx, request, plan)
}

// startPlan uses the exact plan already validated by Start or Restart. In
// particular, a restart must not resolve a third resource revision after the
// user's fingerprint has been checked and the previous run cancelled.
func (c *Controller) startPlan(ctx context.Context, request StartRequest, plan *flow.FlowPlan) (*RunState, error) {
	if request.ProjectRoot == "" {
		request.ProjectRoot = request.SessionDir
	}
	plan, inputs, err := c.preparePlan(ctx, request, plan)
	if err != nil {
		return nil, err
	}
	return c.startPreparedPlan(ctx, request, plan, inputs)
}

func (c *Controller) preparePlan(ctx context.Context, request StartRequest, plan *flow.FlowPlan) (*flow.FlowPlan, map[string]any, error) {
	inputs, err := ResolveInputs(plan, request.Inputs, request.OriginalSource, request.ProjectRoot)
	if err != nil {
		var invalid *InputError
		payload := map[string]any{"flow_id": request.FlowID}
		if errors.As(err, &invalid) {
			payload["reasons"] = invalid.Reasons
		}
		c.emit(ctx, request.SessionID, "", "flow.blocked", payload)
		return nil, nil, err
	}
	linked, problems := validateInputPlan(plan, inputs, request.ProjectRoot)
	reasons := []StartupIssue{}
	for _, p := range problems {
		if p.Level == "error" {
			reasons = append(reasons, StartupIssue{Code: p.Code, Input: p.Label, Message: p.Message})
		}
	}
	if len(reasons) > 0 {
		invalid := &InputError{Reasons: reasons}
		c.emit(ctx, request.SessionID, "", "flow.blocked", map[string]any{"flow_id": request.FlowID, "reasons": reasons})
		return nil, nil, invalid
	}
	plan = linked
	if plan.HasErrors() {
		invalid := &flow.ValidationError{Plan: plan, Diagnostics: plan.Diagnostics}
		c.emit(ctx, request.SessionID, "", "flow.blocked", map[string]any{"flow_id": request.FlowID, "reasons": plan.Diagnostics})
		return nil, nil, invalid
	}
	return plan, inputs, nil
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
	state := &RunState{APIVersion: APIVersion, ID: id, SessionID: request.SessionID, SessionDir: request.SessionDir, ProjectRoot: request.ProjectRoot, FlowID: plan.FlowID, FlowVersion: plan.FlowVersion, PlanCacheKey: compileKey(plan), Scheme: plan.Scheme, Origin: origin, Status: "running", StartedAt: c.options.Now(), Inputs: inputs, Steps: []StepState{}, Plan: plan, RunJSONPath: path, Result: RunResult{Deliverables: map[string]any{}, Issues: []string{}}}
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
	memory, memoryErr := flow.DefaultInputMemory()
	if memoryErr == nil {
		memoryErr = memory.Remember(plan.FlowID, plan.Inputs, inputs)
	}
	if memoryErr != nil {
		c.emit(processCtx, state.SessionID, id, "flow.input_memory_failed", map[string]any{"code": "FLOW_INPUT_MEMORY_FAILED", "message": "无法记住本次输入，流程继续运行"})
	}
	steps := []map[string]any{}
	for _, step := range plan.Steps {
		steps = append(steps, map[string]any{"id": step.ID, "index": step.Index, "title": step.Title, "role_label": step.RoleLabel, "kind": step.Kind})
	}
	c.emit(processCtx, state.SessionID, id, "flow.started", map[string]any{"flow_run_id": id, "flow_id": state.FlowID, "name": plan.Name, "steps": steps})
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
		seq := c.options.Events.Emit(context.WithoutCancel(ctx), Event{Event: event, SessionID: session, FlowRunID: id, Payload: payload})
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
		state := &run.Steps[run.Cursor]
		if !state.Condition.Evaluated && !state.AwaitingChoice {
			condition, err := evaluateCondition(ctx, step, run, c.options.Model)
			if err != nil {
				state.LastFailure = "条件判断失败：" + err.Error()
				return c.finish(ctx, e, "failed", state.LastFailure)
			}
			state.Condition = condition
			if !condition.Result {
				state.Status = "skipped"
				run.Cursor++
				if err = c.save(ctx, e); err != nil {
					return err
				}
				c.emit(ctx, run.SessionID, run.ID, "flow.step_skipped", map[string]any{"flow_run_id": run.ID, "step_id": step.ID, "reason": condition.Reason})
				continue
			}
		}
		mode := "step"
		if state.AwaitingChoice {
			mode = "failure_choice"
		} else if step.Kind == "confirm" {
			mode = "confirm"
		}
		request, err := c.buildRequest(step, run, e.source, mode)
		if err != nil {
			return c.finish(ctx, e, "failed", err.Error())
		}
		now := c.options.Now()
		manual := state.PendingManual
		state.PendingManual = false
		state.Attempts = append(state.Attempts, Attempt{RunID: request.RunID, Mode: mode, StartedAt: now, UserInitiated: manual, Verification: Verification{Errors: []string{}}})
		state.Status = "running"
		run.Status = "running"
		if err = c.save(ctx, e); err != nil {
			return err
		}
		c.emit(ctx, run.SessionID, run.ID, "flow.step_started", map[string]any{"flow_run_id": run.ID, "step_id": step.ID, "attempt": len(state.Attempts), "run_id": request.RunID, "role": request.Role.ID, "role_label": request.Role.DisplayName})
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
			if err = c.save(ctx, e); err != nil {
				return err
			}
			kind := "interrupt"
			if mode != "step" {
				kind = mode
			}
			c.emit(ctx, run.SessionID, run.ID, "flow.step_waiting_user", map[string]any{"flow_run_id": run.ID, "step_id": step.ID, "kind": kind})
			return nil
		}
		if err = c.handleResult(ctx, e, result, runErr); err != nil {
			return err
		}
		if IsTerminal(run.Status) {
			return nil
		}
	}
	return c.finish(ctx, e, "completed", "")
}

func (c *Controller) buildRequest(step flow.StepPlan, run *RunState, source, mode string) (StepRequest, error) {
	state := &run.Steps[run.Cursor]
	request := StepRequest{FlowID: run.FlowID, FlowDir: run.Plan.FlowDir, Lock: run.Plan.Lock, SessionID: run.SessionID, SessionDir: run.SessionDir, FlowRunID: run.ID, RunID: fmt.Sprintf("flow-%s-s%02d-a%d", run.ID, step.Index, len(state.Attempts)+1), Step: step, OriginalSource: source, Mode: mode}
	for _, resolved := range run.Plan.Resources {
		request.Resources = append(request.Resources, resolved.ResolvedResource())
	}
	if mode != "step" {
		description := step.Instructions
		if mode == "failure_choice" {
			description = state.LastFailure
		}
		confirmation := confirmationForStep(description, step.Title, mode == "failure_choice")
		request.Confirmation = &confirmation
		return request, nil
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
		if !role.Enabled || role.Kind == "executor" || role.FlowOnly {
			continue
		}
		if len(step.SuggestedTools) == 0 && role.IsMain {
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
	now := c.options.Now()
	attempt.FinishedAt = &now
	if attempt.Mode == "failure_choice" {
		if runErr != nil {
			return c.finish(ctx, e, "failed", runErr.Error())
		}
		choice, err := parseChoice(result.LastContent)
		if err != nil {
			return c.finish(ctx, e, "failed", err.Error())
		}
		attempt.UserChoice = &choice
		state.AwaitingChoice = false
		switch choice.Choice {
		case "retry":
			if state.UserRetries >= 5 {
				return c.finish(ctx, e, "failed", "本步骤已达到5次人工重试上限")
			}
			state.UserRetries++
			state.UserGuidance = choice.Guidance
			state.PendingManual = true
			state.Status = "pending"
		case "skip":
			state.Status = "skipped_after_failure"
			run.Result.Issues = append(run.Result.Issues, step.Title+"："+state.LastFailure)
			run.Cursor++
		case "stop":
			return c.finish(ctx, e, "failed", "用户终止流程")
		default:
			return c.finish(ctx, e, "failed", "用户选择无效")
		}
		return c.save(ctx, e)
	}
	var receipt Receipt
	var err error
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
		attempt.Repair = repair
		attempt.ReceiptRepaired = repair != nil && err == nil
	}
	if err == nil {
		attempt.Receipt = &receipt
		attempt.Verification = VerifyOutputs(ctx, step, receipt, run.ProjectRoot, c.options.Tools)
		if receipt.Status == "completed" && attempt.Verification.OK {
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
	if !attempt.UserInitiated {
		state.AutomaticFailures++
	}
	next := step.OnFailure
	if next == "" {
		next = "retry_then_ask"
	}
	if next == "retry_then_ask" {
		if state.AutomaticFailures < 2 && !attempt.UserInitiated {
			next = "retry"
		} else {
			next = "ask"
		}
	}
	c.emit(ctx, run.SessionID, run.ID, "flow.step_failed", map[string]any{"flow_run_id": run.ID, "step_id": step.ID, "attempt": len(state.Attempts), "errors": strings.Split(state.LastFailure, "；"), "next": next})
	switch next {
	case "retry":
		state.Status = "pending"
	case "ask":
		state.AwaitingChoice = true
	case "skip":
		state.Status = "skipped_after_failure"
		run.Cursor++
		run.Result.Issues = append(run.Result.Issues, step.Title+"："+state.LastFailure)
	case "stop":
		return c.finish(ctx, e, "failed", state.LastFailure)
	default:
		return c.finish(ctx, e, "failed", "无效的失败策略 "+next)
	}
	return c.save(ctx, e)
}

// OnStepRunFinished only accepts a registered interrupted run; repeated delivery is ignored.
func (c *Controller) OnStepRunFinished(ctx context.Context, runID string, result StepResult) (*RunState, error) {
	c.mu.Lock()
	id := c.waiting[runID]
	e := c.runs[id]
	if !result.Interrupted {
		delete(c.waiting, runID)
	}
	c.mu.Unlock()
	if e == nil {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if IsTerminal(e.state.Status) {
		return cloneRun(e.state)
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
	if !IsTerminal(e.state.Status) {
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
	if request.ResourceChoice == "restart" {
		return c.Restart(ctx, request)
	}
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
	c.emit(ctx, run.SessionID, run.ID, "flow.finished", map[string]any{"flow_run_id": run.ID, "status": run.Status, "deliverables": run.Result.Deliverables, "issues": run.Result.Issues, "summary": run.Result.Summary, "run_json_path": run.RunJSONPath})
	return nil
}
