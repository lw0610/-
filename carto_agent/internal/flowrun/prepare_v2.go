package flowrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/llm"
	"github.com/autocarto/carto_agent/internal/plan"
)

func TransientFailure(err error) bool {
	if err == nil {
		return false
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "timeout") || strings.Contains(text, "timed out") || strings.Contains(text, "429") || strings.Contains(text, "status code: 500") || strings.Contains(text, "status code: 502") || strings.Contains(text, "status code: 503") || strings.Contains(text, "status code: 504")
}
func (c *Controller) acquisitionConfig() config.AcquisitionConfig {
	if c.options.AcquisitionConfig != nil {
		return c.options.AcquisitionConfig().WithDefaults()
	}
	return (config.AcquisitionConfig{}).WithDefaults()
}
func (c *Controller) pausePlan(ctx context.Context, e *execution, reason string) error {
	run := e.state
	run.Status = "interrupted"
	if run.Cursor < len(run.Steps) {
		run.Steps[run.Cursor].Status = "interrupted"
		run.Steps[run.Cursor].LastFailure = reason
	}
	if err := c.save(ctx, e); err != nil {
		return err
	}
	c.release(e)
	c.emit(ctx, run.SessionID, run.ID, "flow.step_waiting_user", map[string]any{"step_id": run.Steps[run.Cursor].ID, "kind": "paused", "status": "interrupted", "reason": reason})
	return nil
}
func (c *Controller) preparationState(run *RunState, id, name string) *DataPreparationState {
	for i := range run.DataPreparation {
		if run.DataPreparation[i].DataID == id {
			return &run.DataPreparation[i]
		}
	}
	run.DataPreparation = append(run.DataPreparation, DataPreparationState{DataID: id, Name: name, Status: "pending", Ledger: []PreparationAttempt{}})
	return &run.DataPreparation[len(run.DataPreparation)-1]
}
func (c *Controller) publishPreparation(ctx context.Context, e *execution, state *DataPreparationState) error {
	run := e.state
	if err := SavePreparationLedger(filepath.Join(run.SessionDir, "data", "ledgers", state.DataID+".json"), *state); err != nil {
		return err
	}
	c.emit(ctx, run.SessionID, run.ID, "flow.data_preparation", map[string]any{"data_id": state.DataID, "name": state.Name, "status": state.Status, "reason": state.Reason, "ledger": state.Ledger, "started_at": state.StartedAt})
	return c.save(ctx, e)
}

// Deterministic independent acquisitions run concurrently. Agent discoveries
// enter the existing serialized interrupt lifecycle, so user review remains one
// coherent continuation of the consuming step.
func (c *Controller) prepareStepInputs(ctx context.Context, e *execution, step flow.StepPlan) error {
	run := e.state
	cfg := c.acquisitionConfig()
	pending := map[string]plan.Data{}
	var add func(string)
	add = func(id string) {
		if _, ok := pending[id]; ok {
			return
		}
		d, ok := run.CartoPlan.DataByID(id)
		if !ok || provided(run.AcquiredInputs[id]) {
			return
		}
		pending[id] = d
		if d.Extent != nil && d.Extent.From != "" {
			add(d.Extent.From)
		}
	}
	for _, id := range step.DataUses {
		add(id)
	}
	for len(pending) > 0 {
		ready := []plan.Data{}
		for _, d := range run.CartoPlan.Data {
			if _, ok := pending[d.ID]; !ok {
				continue
			}
			if d.Extent != nil {
				if _, waiting := pending[d.Extent.From]; waiting {
					continue
				}
			}
			ready = append(ready, d)
		}
		if len(ready) == 0 {
			return c.needsReplan(ctx, e, "数据准备存在无法解开的范围依赖")
		}
		type result struct {
			value DataResult
			err   error
		}
		results := make([]result, len(ready))
		var wg sync.WaitGroup
		slots := make(chan struct{}, cfg.Concurrency)
		for i, d := range ready {
			state := c.preparationState(run, d.ID, d.Name)
			state.Status = "running"
			started := time.Now()
			state.StartedAt = &started
			if err := c.publishPreparation(ctx, e, state); err != nil {
				return err
			}
			// Snapshot input maps before workers start; all writes happen after joining.
			snapshot, err := cloneRun(run)
			if err != nil {
				return err
			}
			wg.Add(1)
			go func(i int, d plan.Data, snapshot *RunState) {
				defer wg.Done()
				select {
				case slots <- struct{}{}:
				case <-ctx.Done():
					results[i].err = context.Cause(ctx)
					return
				}
				defer func() { <-slots }()
				if c.options.DataGet == nil {
					results[i].err = fmt.Errorf("数据服务未配置")
				} else {
					results[i].value, results[i].err = c.options.DataGet(ctx, snapshot, d)
				}
				if ctx.Err() != nil {
					results[i].err = context.Cause(ctx)
				}
			}(i, d, snapshot)
		}
		wg.Wait()
		discoveries := []DataResult{}
		var failures []error
		for i, d := range ready {
			r := results[i]
			state := c.preparationState(run, d.ID, d.Name)
			state.StartedAt = nil
			state.Ledger = append(state.Ledger, r.value.Ledger...)
			if r.err != nil {
				state.Status = "failed"
				state.Reason = r.err.Error()
				if err := c.publishPreparation(ctx, e, state); err != nil {
					return err
				}
				failures = append(failures, r.err)
				continue
			}
			if r.value.Preparation != nil {
				if run.DataNeeds == nil {
					run.DataNeeds = map[string]json.RawMessage{}
				}
				run.DataNeeds[d.ID] = r.value.Preparation.Need
				discoveries = append(discoveries, r.value)
				state.Status = "pending_agent"
			} else if provided(r.value.Value) {
				bindPlanData(run, d.ID, r.value)
				state.Status = "completed"
				delete(pending, d.ID)
			} else {
				state.Status = "failed"
				state.Reason = "数据服务没有提供可用内容：" + d.Name
				failures = append(failures, errors.New(state.Reason))
			}
			if err := c.publishPreparation(ctx, e, state); err != nil {
				return err
			}
		}
		// Persist all completed siblings before deciding whether this group can
		// continue. A failed river query must not discard a downloaded railway.
		if len(failures) > 0 {
			reason := errors.Join(failures...).Error()
			for _, failure := range failures {
				if llm.IsNonRecoverableServiceError(failure) {
					return c.pausePlan(ctx, e, reason)
				}
			}
			run.Steps[run.Cursor].LastFailure = reason
			return c.needsReplan(ctx, e, reason)
		}

		if len(discoveries) > 0 {
			prep := discoveries[0].Preparation
			request := newStepRequest(step, run, e.source, "prepare")
			request.Acquisition = prep
			request.Role = prep.Role
			request.Step.Role = prep.Role.ID
			request.Messages = prep.Messages
			run.Steps[run.Cursor].Role = prep.Role.ID
			if err := c.beginStep(ctx, e, request); err != nil {
				return err
			}
			run.Steps[run.Cursor].Attempts[len(run.Steps[run.Cursor].Attempts)-1].DataID = prep.DataID
			if err := c.runStepRequest(ctx, e, request); err != nil {
				return err
			}
			if run.Status != "running" {
				return nil
			}
			// Re-evaluate all missing inputs after registration. Previously successful
			// preparations remain bound and cannot be fetched again.
			return c.prepareStepInputs(ctx, e, step)
		}
	}
	return nil
}
func (c *Controller) finishPreparationV2(ctx context.Context, e *execution, result StepResult, runErr error) error {
	run := e.state
	step := &run.Steps[run.Cursor]
	attempt := &step.Attempts[len(step.Attempts)-1]
	now := c.options.Now()
	attempt.FinishedAt = &now
	state := c.preparationState(run, attempt.DataID, attempt.DataID)
	if runErr == nil {
		runErr = result.Err
	}
	if runErr != nil {
		state.Status = "failed"
		state.Reason = runErr.Error()
		if err := c.publishPreparation(ctx, e, state); err != nil {
			return err
		}
		if llm.IsNonRecoverableServiceError(runErr) {
			return c.pausePlan(ctx, e, runErr.Error())
		}
		return c.needsReplan(ctx, e, runErr.Error())
	}
	d, _ := run.CartoPlan.DataByID(attempt.DataID)
	got, err := c.options.DataGet(ctx, run, d)
	if err == nil && got.Preparation == nil && provided(got.Value) {
		bindPlanData(run, d.ID, got)
		state.Status = "completed"
		attempt.Verification = Verification{OK: true}
		step.Status = "pending"
		run.Status = "running"
		return c.publishPreparation(ctx, e, state)
	}
	reason := "数据准备未提交通过内容检查的数据"
	if err != nil {
		reason = err.Error()
	}
	state.Reason = reason
	state.Status = "failed"
	attempt.Verification = Verification{Errors: []string{reason}}
	failures := 0
	for _, a := range step.Attempts {
		if a.Mode == "prepare" && a.DataID == d.ID && !a.Verification.OK {
			failures++
		}
	}
	if failures < 2 {
		step.UserGuidance = "只修正取数结果并登记，不重做已成功的获取。" + reason
		step.Status = "pending"
		run.Status = "running"
		return c.publishPreparation(ctx, e, state)
	}
	if err := c.publishPreparation(ctx, e, state); err != nil {
		return err
	}
	return c.needsReplan(ctx, e, reason)
}
