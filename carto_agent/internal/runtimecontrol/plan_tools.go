package runtimecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/adk"
)

const ToolPlanPropose = "plan_propose"
const ToolPlanRevise = "plan_revise"

type PlanService interface {
	ProposePlan(context.Context, json.RawMessage) (map[string]any, error)
	RevisePlan(context.Context, json.RawMessage) (map[string]any, error)
}
type planServiceKey struct{}

const planFailuresKey = "runtime-control.plan-validation-failures"

// ADK owns the counter and checkpoint lifecycle. This lock only makes its
// separate Get/Set calls atomic when a model submits tools in parallel; no run
// state is retained outside the current agent invocation.
var planFailuresMu sync.Mutex

func WithPlanService(ctx context.Context, service PlanService) context.Context {
	return context.WithValue(ctx, planServiceKey{}, service)
}
func invokePlanTool(ctx context.Context, name string, args map[string]any) (any, error) {
	service, ok := ctx.Value(planServiceKey{}).(PlanService)
	if !ok || service == nil {
		return nil, fmt.Errorf("当前会话未配置制图计划服务")
	}
	raw, ok := args["plan"]
	if !ok {
		return nil, boundPlanCorrection(ctx, fmt.Errorf("PLAN_SCHEMA_INVALID: 缺少 plan 对象"))
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return nil, boundPlanCorrection(ctx, fmt.Errorf("PLAN_SCHEMA_INVALID: plan 必须是 JSON 对象: %w", err))
	}
	var result map[string]any
	if name == ToolPlanPropose {
		result, err = service.ProposePlan(ctx, body)
	} else {
		result, err = service.RevisePlan(ctx, body)
	}
	if err != nil {
		return result, boundPlanCorrection(ctx, err)
	}
	if result["accepted"] == true {
		planFailuresMu.Lock()
		_ = adk.DeleteRunLocalValue(ctx, planFailuresKey)
		planFailuresMu.Unlock()
		// Stop the planning turn only after successful validation/persistence.
		// Direct service callers have no ADK state and do not need this action.
		_ = adk.SendToolGenAction(ctx, name, adk.NewExitAction())
	}
	return result, err
}

type planCorrectionError struct {
	cause    error
	attempts int
}

func (e *planCorrectionError) Error() string {
	if e.FatalToolError() {
		return fmt.Sprintf("计划输入连续 %d 次未通过校验，已停止本轮自动修正；请根据诊断解决阻塞后再继续：%s", e.attempts, e.cause)
	}
	return e.cause.Error()
}

func (e *planCorrectionError) Unwrap() error        { return e.cause }
func (e *planCorrectionError) FatalToolError() bool { return e.attempts >= plan.MaxRevisions }
func (e *planCorrectionError) ToolRetryable() bool  { return !e.FatalToolError() }
func (e *planCorrectionError) ToolErrorCode() string {
	if e.FatalToolError() {
		return "PLAN_CORRECTION_EXHAUSTED"
	}
	var coded interface{ ToolErrorCode() string }
	if errors.As(e.cause, &coded) {
		return coded.ToolErrorCode()
	}
	return "PLAN_SCHEMA_INVALID"
}

func (e *planCorrectionError) ToolProviderDiagnostics() map[string]any {
	details := map[string]any{}
	var coded interface{ ToolProviderDiagnostics() map[string]any }
	if errors.As(e.cause, &coded) {
		for key, value := range coded.ToolProviderDiagnostics() {
			details[key] = value
		}
	} else {
		details["path"] = "plan"
		details["message"] = e.cause.Error()
	}
	remaining := plan.MaxRevisions - e.attempts
	if remaining < 0 {
		remaining = 0
	}
	details["attempts"], details["limit"], details["remaining"] = e.attempts, plan.MaxRevisions, remaining
	details["next_action"] = "correct_plan"
	if e.FatalToolError() {
		details["next_action"] = "stop"
		details["stop_reason"] = "plan_correction_budget_exhausted"
	}
	return details
}

func boundPlanCorrection(ctx context.Context, err error) error {
	// Only model-correctable input failures consume this budget. Transport,
	// persistence, cancellation and interruption errors keep their own semantics.
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var invalid *plan.ValidationError
	if !errors.As(err, &invalid) && !strings.HasPrefix(err.Error(), "PLAN_SCHEMA_INVALID:") {
		return err
	}
	planFailuresMu.Lock()
	defer planFailuresMu.Unlock()
	value, _, stateErr := adk.GetRunLocalValue(ctx, planFailuresKey)
	if stateErr != nil {
		// Direct API/service callers have no agent-local state and no repair loop.
		return err
	}
	attempts, _ := value.(int)
	attempts++
	if stateErr := adk.SetRunLocalValue(ctx, planFailuresKey, attempts); stateErr != nil {
		return fmt.Errorf("保存当前规划修正状态失败: %w", stateErr)
	}
	return &planCorrectionError{cause: err, attempts: attempts}
}
