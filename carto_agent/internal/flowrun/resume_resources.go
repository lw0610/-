package flowrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/autocarto/carto_agent/internal/flow"
)

const ResourceChangeMessage = "资源在暂停期间发生变化。继续将使用本次运行保存的资源引用；已修改的文件内容不会恢复，文件缺失或内容变化仍可能导致执行失败。"

type ResourceChange struct {
	Fingerprint string `json:"fingerprint"`
	Message     string `json:"message"`
}

type ResourceChangeError struct{ Change ResourceChange }

func (e *ResourceChangeError) Error() string { return e.Change.Message }

func compileKey(plan *flow.FlowPlan) string {
	if plan == nil {
		return ""
	}
	return plan.CompileKey
}

func checkResumeSource(state *RunState, source string) error {
	if state.Plan == nil {
		return fmt.Errorf("流程运行记录缺少计划快照")
	}
	for _, input := range state.Plan.Inputs {
		if input.Type != "conversation_text" || !provided(state.Inputs[input.Key]) {
			continue
		}
		resolved, err := ResolveInputs(&flow.FlowPlan{Inputs: []flow.InputPlan{input}}, nil, source, state.ProjectRoot)
		if err != nil {
			return err
		}
		old, _ := json.Marshal(state.Inputs[input.Key])
		current, _ := json.Marshal(resolved[input.Key])
		if string(old) != string(current) {
			return fmt.Errorf("恢复所需原文与运行记录不一致")
		}
	}
	return nil
}

// CheckResume runs before either a recovered step or an ADK checkpoint executes.
// An acknowledgement is tied to the observed current fingerprint, while Plan
// remains the original run's resource-reference snapshot.
func (c *Controller) CheckResume(ctx context.Context, request ResumeRequest) (*RunState, error) {
	state, err := c.options.Store.Load(ctx, request.SessionDir, request.FlowRunID)
	if err != nil {
		return nil, err
	}
	if state.SessionID != request.SessionID {
		return nil, fmt.Errorf("流程与会话不匹配")
	}
	if state.Status != "interrupted" && state.Status != "waiting_user" {
		return nil, fmt.Errorf("仅可继续已中断或等待确认的流程")
	}
	if state.Cursor < 0 || state.Cursor >= len(state.Steps) {
		return nil, fmt.Errorf("中断记录没有待执行步骤")
	}
	if err := checkResumeSource(state, request.OriginalSource); err != nil {
		return nil, err
	}
	if request.ResourceChoice != "" && request.ResourceChoice != "continue_snapshot" && request.ResourceChoice != "restart" {
		return nil, fmt.Errorf("资源变化处理选择无效")
	}
	if err := c.validateResumeOverlays(ctx, state.Plan, state.Scheme); err != nil {
		return nil, err
	}
	current, linkErr := c.options.Compiler.EnsureRunnable(ctx, state.FlowID, state.Scheme)
	if current == nil {
		if linkErr != nil {
			return nil, linkErr
		}
		return nil, fmt.Errorf("恢复前流程校验未通过")
	}
	resumeInputs := resumePlanInputs(state.Plan, current, state.Inputs)
	for k, v := range state.AcquiredInputs {
		skipLayers := map[string]bool{}
		for _, resource := range state.Plan.Resources {
			if resource.BaseDirFromInput == k {
				for name := range state.LayerOverrides[resource.Key] {
					skipLayers[name] = true
				}
			}
		}

		if original, ok := v.(map[string]any); ok {
			files := map[string]any{}
			for name, path := range original {
				if !skipLayers[name] {
					files[name] = path
				}
			}
			choice, _, _ := flow.ParseInputChoice(resumeInputs[k])
			marker := map[string]any{"$acquire": true, "files": files}
			if choice.Path != "" {
				marker = map[string]any{"$fill": true, "path": choice.Path, "files": files}
			}
			resumeInputs[k] = marker
		} else if original, ok := v.(map[string]string); ok {
			files := map[string]string{}
			for name, path := range original {
				if !skipLayers[name] {
					files[name] = path
				}
			}
			choice, _, _ := flow.ParseInputChoice(resumeInputs[k])
			marker := map[string]any{"$acquire": true, "files": files}
			if choice.Path != "" {
				marker = map[string]any{"$fill": true, "path": choice.Path, "files": files}
			}
			resumeInputs[k] = marker
		} else {
			resumeInputs[k] = v
		}
	}
	if current.Acquisition != nil {
		var problems []InputProblem
		var checkErr error
		current, problems, checkErr = c.acquisitionInputPlan(ctx, current, resumeInputs, state.ProjectRoot)
		if checkErr != nil {
			return nil, checkErr
		}
		for _, p := range problems {
			if p.Level == "error" {
				return nil, &InputError{Reasons: []StartupIssue{{Code: p.Code, Input: p.Label, Step: p.Step, Item: p.Item, Acquirable: p.Acquirable, Candidates: p.Candidates, Message: p.Message}}}
			}
		}
	} else {
		current = flow.ResolveInputResources(current, resumeInputs)
	}
	current = flow.ResolveInputResourcesWithLayers(current, resumeInputs, state.EntryOverrides, state.LayerOverrides)
	var invalid *flow.ValidationError
	if linkErr != nil && !errors.As(linkErr, &invalid) {
		return nil, linkErr
	}
	if current.HasErrors() && len(current.LinkDiagnostics) == 0 {
		return nil, fmt.Errorf("恢复前流程结构校验未通过")
	}
	for _, d := range current.CompileDiagnostics {
		if d.Level == "error" {
			return nil, fmt.Errorf("恢复前流程结构校验未通过：%s", d.Message)
		}
	}
	snapshotKey := compileKey(state.Plan)
	if snapshotKey == "" {
		snapshotKey = state.PlanCacheKey
	}
	legacyCompatible := legacyResumeCompatible(state.Plan, current, state.Inputs, state.ProjectRoot)
	if legacyCompatible {
		current.LinkFingerprint = legacyResourceFingerprint(state.Plan, current)
	}
	if compileKey(current) != snapshotKey && !legacyCompatible {
		return nil, fmt.Errorf("流程或方案结构已改变，请新建一次运行")
	}
	changed := state.ResourceChange != nil || current.LinkFingerprint != state.Plan.LinkFingerprint && current.LinkFingerprint != state.ResourceFingerprint
	if request.ResourceChoice != "" && request.ResourceFingerprint != current.LinkFingerprint {
		changed = true
	}
	if changed {
		if request.ResourceChoice == "" || request.ResourceFingerprint != current.LinkFingerprint {
			change := &ResourceChange{Fingerprint: current.LinkFingerprint, Message: ResourceChangeMessage}
			state.ResourceChange = change
			if err := c.saveResumeState(ctx, state, "flow.resources_changed"); err != nil {
				return nil, err
			}
			return state, &ResourceChangeError{Change: *change}
		}
		if request.ResourceChoice == "restart" && (linkErr != nil || current.HasErrors()) {
			if linkErr != nil {
				return state, linkErr
			}
			return state, fmt.Errorf("当前资源校验未通过，无法重新开始")
		}
		state.ResourceFingerprint = current.LinkFingerprint
		state.ResourceChange = nil
		if err := c.saveResumeState(ctx, state, "flow.resources_confirmed"); err != nil {
			return nil, err
		}
	} else if request.ResourceChoice == "" && linkErr != nil && state.ResourceFingerprint == "" {
		return state, linkErr
	}
	return state, nil
}

func (c *Controller) saveResumeState(ctx context.Context, state *RunState, event string) error {
	c.mu.Lock()
	existing := c.runs[state.ID]
	c.mu.Unlock()
	if existing != nil {
		existing.mu.Lock()
		defer existing.mu.Unlock()
		if existing.state.Status != "interrupted" && existing.state.Status != "waiting_user" {
			return fmt.Errorf("流程状态已改变，请刷新后重试")
		}
		existing.state.ResourceChange = state.ResourceChange
		existing.state.ResourceFingerprint = state.ResourceFingerprint
		if err := c.save(ctx, existing); err != nil {
			return err
		}
	} else if err := c.options.Store.Save(ctx, state); err != nil {
		return err
	}
	payload := map[string]any{"flow_run_id": state.ID, "resource_change": state.ResourceChange, "status": state.Status, "flow_id": state.FlowID}
	c.emit(ctx, state.SessionID, state.ID, event, payload)
	return nil
}

// Restart preserves the old run record, ends its execution and starts a new run
// with a freshly linked plan. The websocket layer deletes the old ADK checkpoint.
func (c *Controller) Restart(ctx context.Context, request ResumeRequest) (*RunState, error) {
	if request.ResourceChoice != "restart" {
		return nil, fmt.Errorf("重新开始需要明确选择")
	}
	state, err := c.CheckResume(ctx, request)
	if err != nil {
		return state, err
	}
	current, err := c.options.Compiler.EnsureRunnable(ctx, state.FlowID, state.Scheme)
	if err != nil || current == nil || current.HasErrors() {
		if err == nil {
			err = fmt.Errorf("当前流程校验未通过，无法重新开始")
		}
		return state, err
	}
	inputs := resumePlanInputs(state.Plan, current, state.Inputs)
	for _, input := range current.Inputs {
		if input.Type == "conversation_text" {
			delete(inputs, input.Key)
		}
	}
	start := StartRequest{SessionID: state.SessionID, SessionDir: state.SessionDir, ProjectRoot: state.ProjectRoot, ProjectPath: state.ProjectPath, FlowID: state.FlowID, Scheme: state.Scheme, Inputs: inputs, OriginalSource: request.OriginalSource, Origin: state.Origin}
	if start.ProjectRoot == "" {
		start.ProjectRoot = start.SessionDir
	}
	current, inputs, err = c.preparePlan(ctx, start, current)
	if err != nil {
		return state, err
	}
	snapshotKey := compileKey(state.Plan)
	if snapshotKey == "" {
		snapshotKey = state.PlanCacheKey
	}
	legacyCompatible := legacyResumeCompatible(state.Plan, current, state.Inputs, state.ProjectRoot)
	if legacyCompatible {
		current.LinkFingerprint = legacyResourceFingerprint(state.Plan, current)
	}
	if compileKey(current) != snapshotKey && !legacyCompatible {
		return state, fmt.Errorf("流程或方案结构已改变，请新建一次运行")
	}
	if current.LinkFingerprint != request.ResourceFingerprint {
		change := &ResourceChange{Fingerprint: current.LinkFingerprint, Message: ResourceChangeMessage}
		state.ResourceChange = change
		if err := c.saveResumeState(ctx, state, "flow.resources_changed"); err != nil {
			return state, err
		}
		return state, &ResourceChangeError{Change: *change}
	}
	c.mu.Lock()
	existing := c.runs[state.ID]
	c.mu.Unlock()
	if existing != nil {
		existing.mu.Lock()
		err = c.finish(ctx, existing, "cancelled", "用户选择使用当前资源重新开始")
		existing.mu.Unlock()
	} else {
		e := &execution{state: state, source: request.OriginalSource}
		err = c.finish(ctx, e, "cancelled", "用户选择使用当前资源重新开始")
	}
	if err != nil {
		return nil, err
	}
	return c.startPreparedPlan(ctx, start, current, inputs)
}
