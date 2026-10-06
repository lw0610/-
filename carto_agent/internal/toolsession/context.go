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
)

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
