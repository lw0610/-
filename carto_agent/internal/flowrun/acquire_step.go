package flowrun

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/hitl"
)

type acquisitionJob struct {
	done    chan struct{}
	result  acquire.ItemResult
	err     error
	applied bool
}
type acquisitionJobs struct {
	ctx    context.Context
	cancel context.CancelFunc
	engine *acquire.Engine
	env    datasource.PlanningEnvironment
	err    error
	jobs   map[string]*acquisitionJob
}

// Original choices are retained separately for form memory. Consumers see real
// files; a multi-source list remains a list of row paths, never a fictitious folder.
func EffectiveInputs(run *RunState) map[string]any {
	values := map[string]any{}
	for key, value := range run.Inputs {
		choice, marked, _ := flow.ParseInputChoice(value)
		if marked {
			if choice.Path != "" {
				values[key] = choice.Path
			}
			continue
		}
		values[key] = value
	}
	for key, value := range run.AcquiredInputs {
		values[key] = value
	}
	return values
}

func (c *Controller) startAcquisitionJobs(ctx context.Context, e *execution) {
	run := e.state
	if run.Plan.Acquisition == nil || len(run.Plan.Acquisition.Items) == 0 {
		return
	}
	if run.Acquisition == nil {
		run.Acquisition = &AcquisitionState{Plan: *run.Plan.Acquisition, Items: []acquire.ItemResult{}}
		for _, item := range run.Acquisition.Plan.Items {
			run.Acquisition.Items = append(run.Acquisition.Items, acquire.ItemResult{Key: item.Key, Status: "pending", Attempts: []acquire.AttemptRecord{}, Notes: []string{}, Output: acquire.OutputRecord{Paths: []string{}, Extent: []float64{}}})
		}
	}
	if e.acquisition == nil {
		c.emit(ctx, run.SessionID, run.ID, "flow.acquire_planned", map[string]any{"plan": run.Acquisition.Plan})
		jobCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		stop := context.AfterFunc(c.options.Lifecycle, cancel)
		jobs := &acquisitionJobs{ctx: jobCtx, cancel: func() { stop(); cancel() }, jobs: map[string]*acquisitionJob{}}
		e.acquisition = jobs
		if c.options.AcquisitionFactory == nil {
			jobs.err = fmt.Errorf("数据获取执行器不可用")
		} else {
			jobs.engine, jobs.err = c.options.AcquisitionFactory(jobCtx, run)
		}
		if loader, ok := c.options.Compiler.(interface {
			AcquisitionEnvironment(context.Context, string) (datasource.PlanningEnvironment, error)
		}); ok {
			var envErr error
			jobs.env, envErr = loader.AcquisitionEnvironment(ctx, run.Scheme)
			if jobs.err == nil {
				jobs.err = envErr
			}
		} else if jobs.engine != nil {
			jobs.env = datasource.PlanningEnvironment{Catalog: jobs.engine.Catalog, AutoAcquire: "allow", Integrations: map[string]bool{}, Secrets: map[string]bool{}}
		}
	}
	jobs := e.acquisition
	for i := range run.Acquisition.Plan.Items {
		item := run.Acquisition.Plan.Items[i]
		status := run.Acquisition.Items[i].Status
		if status == "completed" || status == "reused" || status == "user_provided" || status == "skipped" || status == "failed" || status == "waiting_user" {
			continue
		}
		if _, exists := jobs.jobs[item.Key]; exists {
			continue
		}
		var prepareErr error
		if (item.Mode == "auto" || item.Mode == "fill") && item.Extent != nil && item.Extent.Pending && item.Extent.DependencyStep != "" {
			value := ""
			ready := false
			for _, s := range run.Steps {
				if s.ID == item.Extent.DependencyStep {
					ready = s.Status == "completed" || s.Status == "skipped" || s.Status == "skipped_after_failure"
					value, _ = s.Outputs[item.Extent.DependencyOutput].(string)
					break
				}
			}
			if !ready {
				continue
			}
			if value == "" {
				prepareErr = fmt.Errorf("用于获取范围的步骤未产出 %s", item.Extent.DependencyOutput)
			} else {
				item, prepareErr = acquire.ResolveRuntimeExtent(ctx, item, value, EffectiveInputs(run), jobs.env, nil)
				run.Acquisition.Plan.Items[i] = item
			}
		}
		job := &acquisitionJob{done: make(chan struct{})}
		jobs.jobs[item.Key] = job
		run.Acquisition.Items[i].Status = "running"
		sessionID, runID := run.SessionID, run.ID
		c.emit(ctx, sessionID, runID, "flow.acquire_item_started", map[string]any{"key": item.Key, "source_id": func() string {
			if item.Chosen != nil {
				return item.Chosen.SourceID
			}
			return ""
		}()})
		go func(item flow.AcquisitionItem, job *acquisitionJob, prepareErr error) {
			defer close(job.done)
			if item.Mode == "user" || item.Mode == "none" {
				job.result, job.err = (&acquire.Engine{}).RunItem(jobs.ctx, item, nil)
				return
			}
			if prepareErr == nil {
				prepareErr = jobs.err
			}
			if prepareErr != nil {
				job.result = acquire.ItemResult{Key: item.Key, Status: "failed", Error: prepareErr.Error()}
				job.err = prepareErr
				return
			}
			if jobs.engine == nil {
				job.err = fmt.Errorf("数据获取执行器不可用")
				job.result = acquire.ItemResult{Key: item.Key, Status: "failed", Error: job.err.Error()}
				return
			}
			job.result, job.err = jobs.engine.RunItem(jobs.ctx, item, func(p acquire.Progress) {
				if c.options.Events != nil {
					c.options.Events.Emit(jobs.ctx, Event{Event: "flow.acquire_progress", SessionID: sessionID, FlowRunID: runID, Payload: map[string]any{"key": item.Key, "stage": p.Stage, "bytes": p.Bytes, "total": p.Total}})
				}
			})
			if job.err != nil {
				job.result.Status = "failed"
				job.result.Error = job.err.Error()
			}
		}(item, job, prepareErr)
	}
}
func (c *Controller) collectAcquisition(ctx context.Context, e *execution, index int, wait bool) (bool, error) {
	item := e.state.Acquisition.Plan.Items[index]
	job := e.acquisition.jobs[item.Key]
	if job == nil {
		return false, nil
	}
	if wait {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-job.done:
		}
	} else {
		select {
		case <-job.done:
		default:
			return false, nil
		}
	}
	if !job.applied {
		previous := e.state.Acquisition.Items[index].Attempts
		result := job.result
		result.Attempts = append(previous, result.Attempts...)
		e.state.Acquisition.Items[index] = result
		job.applied = true
		if job.err == nil && result.Status != "waiting_user" {
			c.applyAcquisitionResult(e.state, item, result)
		}
		if err := c.save(ctx, e); err != nil {
			return false, err
		}
		if job.err == nil {
			c.emit(ctx, e.state.SessionID, e.state.ID, "flow.acquire_item_done", map[string]any{"key": item.Key, "status": result.Status, "source_id": result.SourceID, "notes": result.Notes, "output": result.Output})
		}
	}
	return true, nil
}
func (c *Controller) advanceAcquisition(ctx context.Context, e *execution, step flow.StepPlan) error {
	run := e.state
	state := &run.Steps[run.Cursor]
	if run.Acquisition == nil {
		return c.finish(ctx, e, "failed", "数据获取步骤缺少计划")
	}
	if !state.AwaitingChoice {
		state.Status = "running"
		for i, item := range run.Acquisition.Plan.Items {
			if item.Requirement != step.AcquisitionRequirement {
				continue
			}
			status := run.Acquisition.Items[i].Status
			if status == "pending" {
				run.Acquisition.Items[i].Status = "failed"
				run.Acquisition.Items[i].Error = "范围依赖尚未完成"
			}
			if status == "running" {
				if _, err := c.collectAcquisition(ctx, e, i, true); err != nil {
					if ctx.Err() != nil {
						return c.finish(ctx, e, "cancelled", ctx.Err().Error())
					}
					return err
				}
			}
			result := run.Acquisition.Items[i]
			if result.Status == "failed" || result.Status == "waiting_user" {
				state.LastFailure = item.Key + "：" + result.Error
				state.AwaitingChoice = true
				state.Status = "failed"
				confirmation := acquisitionConfirmation(run, step)
				event := "flow.acquire_failed"
				if result.Status == "waiting_user" {
					event = "flow.acquire_confirmation"
				}
				c.emit(ctx, run.SessionID, run.ID, event, map[string]any{"key": item.Key, "error": result.Error, "choices": confirmation.Options, "required": item.Required})
				break
			}
			if result.Output.LayerID != "" {
				state.Outputs[item.Entry] = result.Output.LayerID
			} else {
				for _, path := range result.Output.Paths {
					state.Outputs[item.Entry] = path
					break
				}
			}
		}
		if !state.AwaitingChoice {
			state.Status = "completed"
			state.Condition = ConditionResult{Evaluated: true, Result: true, By: "system"}
			run.Cursor++
			if err := c.save(ctx, e); err != nil {
				return err
			}
			c.emit(ctx, run.SessionID, run.ID, "flow.step_completed", map[string]any{"step_id": step.ID, "outputs": state.Outputs, "summary": "所需数据已准备"})
			return nil
		}
	}
	request, err := c.buildRequest(step, run, e.source, "acquire_choice")
	if err != nil {
		return err
	}
	state.Attempts = append(state.Attempts, Attempt{RunID: request.RunID, Mode: "acquire_choice", StartedAt: c.options.Now()})
	if err = c.save(ctx, e); err != nil {
		return err
	}
	result, runErr := c.options.Runner.Run(ctx, request)
	if result.Interrupted && runErr == nil {
		c.mu.Lock()
		c.waiting[request.RunID] = run.ID
		c.mu.Unlock()
		run.Status = "waiting_user"
		state.Status = "waiting_user"
		if err = c.save(ctx, e); err != nil {
			return err
		}
		c.emit(ctx, run.SessionID, run.ID, "flow.step_waiting_user", map[string]any{"step_id": step.ID, "kind": "acquire_choice"})
		return nil
	}
	return c.handleAcquisitionChoice(ctx, e, result, runErr)
}
func failedAcquisition(run *RunState, step flow.StepPlan) int {
	if run.Acquisition != nil {
		for i, item := range run.Acquisition.Plan.Items {
			if item.Requirement == step.AcquisitionRequirement && (run.Acquisition.Items[i].Status == "failed" || run.Acquisition.Items[i].Status == "waiting_user") {
				return i
			}
		}
	}
	return -1
}
func acquisitionConfirmation(run *RunState, step flow.StepPlan) ConfirmationRequest {
	result := ConfirmationRequest{Category: "flow_acquisition", Title: "数据获取未完成", Default: "retry", Options: []hitl.RequestOption{{Label: "重试", Value: "retry"}}}
	i := failedAcquisition(run, step)
	if i < 0 {
		return result
	}
	item := run.Acquisition.Plan.Items[i]
	if run.Acquisition.Items[i].Status == "waiting_user" && run.Acquisition.Items[i].Output.LayerDefinition != nil {
		result.Title = "加载天地图在线底图"
		result.Description = "加载后，天地图密钥将保存在 QGIS 工程的图层地址中。请勿分享含密钥的工程文件；分享前应移除此在线图层。密钥不会写入对话、来源定义或数据仓。是否确认加载？"
		result.Default = "stop"
		result.Options = []hitl.RequestOption{{Label: "确认将密钥写入工程并加载", Value: "load_tiles"}, {Label: "我来提供本地栅格", Value: "provide"}}
		if !item.Required {
			result.Options = append(result.Options, hitl.RequestOption{Label: "跳过此项", Value: "skip"})
		}
		result.Options = append(result.Options, hitl.RequestOption{Label: "终止流程", Value: "stop"})
		return result
	}
	result.Description = item.Key + "：" + run.Acquisition.Items[i].Error
	for _, source := range item.Alternatives {
		result.Options = append(result.Options, hitl.RequestOption{Label: "换用 " + source.Name, Value: "source:" + source.SourceID})
	}
	result.Options = append(result.Options, hitl.RequestOption{Label: "我来提供文件", Value: "provide"})
	if !item.Required {
		result.Options = append(result.Options, hitl.RequestOption{Label: "跳过此项", Value: "skip"})
	}
	result.Options = append(result.Options, hitl.RequestOption{Label: "终止流程", Value: "stop"})
	return result
}
func (c *Controller) handleAcquisitionChoice(ctx context.Context, e *execution, result StepResult, runErr error) error {
	run := e.state
	state := &run.Steps[run.Cursor]
	step := run.Plan.Steps[run.Cursor]
	if runErr != nil {
		return c.finish(ctx, e, "failed", runErr.Error())
	}
	choice, err := parseChoice(result.LastContent)
	if err != nil {
		return c.finish(ctx, e, "failed", err.Error())
	}
	i := failedAcquisition(run, step)
	if i < 0 {
		return c.finish(ctx, e, "failed", "缺少待处理的获取项")
	}
	item := &run.Acquisition.Plan.Items[i]
	valid := false
	for _, option := range acquisitionConfirmation(run, step).Options {
		valid = valid || option.Value == choice.Choice
	}
	if !valid {
		return c.finish(ctx, e, "failed", "无效的数据获取选项")
	}
	if len(state.Attempts) > 0 {
		a := &state.Attempts[len(state.Attempts)-1]
		a.UserChoice = &choice
		now := c.options.Now()
		a.FinishedAt = &now
	}
	switch {
	case choice.Choice == "stop":
		return c.finish(ctx, e, "cancelled", "用户终止数据获取")
	case choice.Choice == "skip":
		run.Acquisition.Items[i].Status = "skipped"
		run.Result.Issues = append(run.Result.Issues, item.Key+"：用户跳过")
	case choice.Choice == "load_tiles":
		if e.acquisition == nil {
			c.startAcquisitionJobs(ctx, e)
		}
		current := &run.Acquisition.Items[i]
		if e.acquisition == nil || e.acquisition.engine == nil || len(current.Output.Paths) == 0 {
			current.Status = "failed"
			current.Error = "在线图层加载工具不可用"
			return c.save(ctx, e)
		}
		value, err := e.acquisition.engine.Invoke(ctx, "data_load_tiles", map[string]any{"definition_path": current.Output.Paths[0], "confirmed": true})
		var payload map[string]any
		if err == nil {
			payload, err = acquire.UnwrapToolResult(value)
		}
		if err != nil {
			current.Status = "failed"
			current.Error = err.Error()
			return c.save(ctx, e)
		}
		data := payload
		if nested, ok := payload["data"].(map[string]any); ok {
			data = nested
		}
		layerID, _ := data["layer_id"].(string)
		if layerID == "" {
			current.Status = "failed"
			current.Error = "加载器未返回图层编号"
			return c.save(ctx, e)
		}
		current.Status = "completed"
		current.Error = ""
		current.Output.LayerID = layerID
		current.Notes = append(current.Notes, "用户已确认密钥进入 QGIS 工程；分享工程前应移除在线图层")
		c.applyAcquisitionResult(run, *item, *current)
	case choice.Choice == "provide":
		path, err := convertInput(flow.InputPlan{Type: "file"}, strings.TrimSpace(choice.Guidance), run.ProjectRoot)
		if err != nil || path == nil {
			run.Acquisition.Items[i].Error = "提供的文件不可用，请重新选择"
			return c.save(ctx, e)
		}
		p := path.(string)
		if err = validateProvidedFile(*item, p); err != nil {
			run.Acquisition.Items[i].Error = err.Error()
			return c.save(ctx, e)
		}
		if e.acquisition == nil {
			c.startAcquisitionJobs(ctx, e)
		}
		if e.acquisition == nil || e.acquisition.engine == nil {
			run.Acquisition.Items[i].Error = "数据核验工具不可用，请检查连接后重试"
			return c.save(ctx, e)
		}
		acquired, err := e.acquisition.engine.ValidateProvided(ctx, *item, p)
		if err != nil {
			run.Acquisition.Items[i].Error = err.Error()
			return c.save(ctx, e)
		}
		run.Acquisition.Items[i] = acquired
		c.applyAcquisitionResult(run, *item, acquired)
	default:
		if strings.HasPrefix(choice.Choice, "source:") {
			id := strings.TrimPrefix(choice.Choice, "source:")
			for j, s := range item.Alternatives {
				if s.SourceID == id {
					old := item.Chosen
					item.Chosen = &s
					item.Alternatives = append(item.Alternatives[:j], item.Alternatives[j+1:]...)
					if old != nil {
						item.Alternatives = append(item.Alternatives, *old)
					}
					break
				}
			}
		}
		run.Acquisition.Items[i].Status = "pending"
		run.Acquisition.Items[i].Error = ""
	}
	if e.acquisition != nil {
		delete(e.acquisition.jobs, item.Key)
	}
	state.AwaitingChoice = false
	state.Status = "pending"
	run.Status = "running"
	return c.save(ctx, e)
}
func validateProvidedFile(item flow.AcquisitionItem, path string) error {
	if strings.EqualFold(filepath.Ext(path), ".shp") {
		stem := strings.TrimSuffix(path, filepath.Ext(path))
		for _, ext := range []string{".dbf", ".shx"} {
			if info, e := os.Stat(stem + ext); e != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("Shapefile 缺少 %s", ext)
			}
		}
	}
	return nil
}
