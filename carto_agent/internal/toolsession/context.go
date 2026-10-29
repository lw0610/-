package toolsession

import (
	"context"
	"strings"
)

type contextKey string

const (
	sessionIDKey   contextKey = "autocarto_session_id"
	projectPathKey contextKey = "autocarto_project_path"
	instanceIDKey  contextKey = "autocarto_instance_id"
	flowStepKey    contextKey = "autocarto_flow_step"
)

type flowStep struct{ runID, stepID string }

func WithFlowStep(ctx context.Context, runID, stepID string) context.Context {
	return context.WithValue(ctx, flowStepKey, flowStep{strings.TrimSpace(runID), strings.TrimSpace(stepID)})
}

func OutputOwnerFromContext(ctx context.Context) map[string]string {
	sessionID := SessionIDFromContext(ctx)
	if sessionID == "" {
		return nil
	}
	step, _ := ctx.Value(flowStepKey).(flowStep)
	return map[string]string{"session_id": sessionID, "flow_run_id": step.runID, "step_id": step.stepID}
}

func WithContext(ctx context.Context, sessionID, projectPath, instanceID string) context.Context {
	ctx = context.WithValue(ctx, sessionIDKey, strings.TrimSpace(sessionID))
	if strings.TrimSpace(projectPath) != "" {
		ctx = context.WithValue(ctx, projectPathKey, strings.TrimSpace(projectPath))
	}
	if strings.TrimSpace(instanceID) != "" {
		ctx = context.WithValue(ctx, instanceIDKey, strings.TrimSpace(instanceID))
	}
	return ctx
}

func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value, ok := ctx.Value(sessionIDKey).(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func ProjectPathFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value, ok := ctx.Value(projectPathKey).(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func InstanceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value, ok := ctx.Value(instanceIDKey).(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}
