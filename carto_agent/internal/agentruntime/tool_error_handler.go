package agentruntime

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func wrapToolWithErrorHandler(t tool.BaseTool, controlPlane *capability.ControlPlane) tool.BaseTool {
	plane := capability.NormalizeControlPlane(controlPlane)
	if et, ok := t.(tool.EnhancedInvokableTool); ok {
		info, _ := et.Info(context.Background())
		return &enhancedErrorWrapper{inner: et, toolName: toolNameFromInfo(info), controlPlane: plane}
	}
	if it, ok := t.(tool.InvokableTool); ok {
		info, _ := it.Info(context.Background())
		return &invokableErrorWrapper{inner: it, toolName: toolNameFromInfo(info), controlPlane: plane}
	}
	return t
}

type invokableErrorWrapper struct {
	inner        tool.InvokableTool
	toolName     string
	controlPlane *capability.ControlPlane
}

func (w *invokableErrorWrapper) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return w.inner.Info(ctx)
}

func (w *invokableErrorWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	start := time.Now()
	w.controlPlane.Observer.OnToolStart(ctx, w.toolName)
	result, err := w.inner.InvokableRun(ctx, argumentsInJSON, opts...)
	if err == nil {
		w.controlPlane.Observer.OnToolEnd(ctx, newToolObservation(w.toolName, start, true, nil, false))
		return result, nil
	}
	var terminal interface{ FatalToolError() bool }
	if errors.As(err, &terminal) && terminal.FatalToolError() {
		w.controlPlane.Observer.OnToolEnd(ctx, newToolObservation(w.toolName, start, false, err, false))
		return result, err
	}
	if ctx.Err() != nil {
		w.controlPlane.Observer.OnToolEnd(ctx, newToolObservation(w.toolName, start, false, err, true))
		return result, err
	}
	if _, ok := compose.IsInterruptRerunError(err); ok {
		w.controlPlane.Observer.OnToolEnd(ctx, newToolObservation(w.toolName, start, false, err, false))
		return result, err
	}
	mapped, mapErr := w.controlPlane.ErrorMapper.MapToolError(ctx, w.toolName, err)
	if mapErr != nil {
		w.controlPlane.Observer.OnToolEnd(ctx, newToolObservation(w.toolName, start, false, mapErr, false))
		return result, mapErr
	}
	w.controlPlane.Observer.OnToolEnd(ctx, newToolObservation(w.toolName, start, false, err, false))
	return firstToolResultText(mapped), nil
}

type enhancedErrorWrapper struct {
	inner        tool.EnhancedInvokableTool
	toolName     string
	controlPlane *capability.ControlPlane
}

func (w *enhancedErrorWrapper) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return w.inner.Info(ctx)
}

func (w *enhancedErrorWrapper) InvokableRun(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
	start := time.Now()
	w.controlPlane.Observer.OnToolStart(ctx, w.toolName)
	result, err := w.inner.InvokableRun(ctx, toolArgument, opts...)
	if err == nil {
		w.controlPlane.Observer.OnToolEnd(ctx, newToolObservation(w.toolName, start, true, nil, false))
		return result, nil
	}
	var terminal interface{ FatalToolError() bool }
	if errors.As(err, &terminal) && terminal.FatalToolError() {
		w.controlPlane.Observer.OnToolEnd(ctx, newToolObservation(w.toolName, start, false, err, false))
		return result, err
	}
	if ctx.Err() != nil {
		w.controlPlane.Observer.OnToolEnd(ctx, newToolObservation(w.toolName, start, false, err, true))
		return result, err
	}
	if _, ok := compose.IsInterruptRerunError(err); ok {
		w.controlPlane.Observer.OnToolEnd(ctx, newToolObservation(w.toolName, start, false, err, false))
		return result, err
	}
	mapped, mapErr := w.controlPlane.ErrorMapper.MapToolError(ctx, w.toolName, err)
	if mapErr != nil {
		w.controlPlane.Observer.OnToolEnd(ctx, newToolObservation(w.toolName, start, false, mapErr, false))
		return result, mapErr
	}
	w.controlPlane.Observer.OnToolEnd(ctx, newToolObservation(w.toolName, start, false, err, false))
	return mapped, nil
}

func toolNameFromInfo(info *schema.ToolInfo) string {
	if info == nil {
		return ""
	}
	return strings.TrimSpace(info.Name)
}

func newToolObservation(toolName string, startedAt time.Time, success bool, err error, cancelled bool) capability.ToolObservation {
	observation := capability.ToolObservation{
		ToolName:  strings.TrimSpace(toolName),
		Duration:  time.Since(startedAt),
		Success:   success,
		Cancelled: cancelled,
	}
	if err != nil {
		observation.Error = strings.TrimSpace(err.Error())
	}
	return observation
}

func firstToolResultText(result *schema.ToolResult) string {
	if result == nil {
		return ""
	}
	for _, part := range result.Parts {
		if part.Type == schema.ToolPartTypeText {
			return part.Text
		}
	}
	return ""
}
