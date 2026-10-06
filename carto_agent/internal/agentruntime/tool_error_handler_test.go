package agentruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/autocarto/carto_agent/internal/capability"
	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type recordingObserver struct {
	starts []string
	ends   []capability.ToolObservation
}

func (o *recordingObserver) OnToolStart(_ context.Context, toolName string) {
	o.starts = append(o.starts, toolName)
}

func (o *recordingObserver) OnToolEnd(_ context.Context, observation capability.ToolObservation) {
	o.ends = append(o.ends, observation)
}

type fakeEnhancedTool struct {
	info  *schema.ToolInfo
	runFn func(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error)
}

func (f *fakeEnhancedTool) Info(context.Context) (*schema.ToolInfo, error) {
	return f.info, nil
}

func (f *fakeEnhancedTool) InvokableRun(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
	return f.runFn(ctx, toolArgument, opts...)
}

func TestWrapToolWithErrorHandlerObservesSuccessfulEnhancedTool(t *testing.T) {
	observer := &recordingObserver{}
	wrapped := wrapToolWithErrorHandler(&fakeEnhancedTool{
		info: &schema.ToolInfo{Name: "buffer", Desc: "缓冲区分析"},
		runFn: func(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
			return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: "ok"}}}, nil
		},
	}, &capability.ControlPlane{Observer: observer, ErrorMapper: capability.DefaultErrorMapper{}})

	enhanced, ok := wrapped.(tool.EnhancedInvokableTool)
	if !ok {
		t.Fatalf("expected EnhancedInvokableTool, got %T", wrapped)
	}
	result, err := enhanced.InvokableRun(context.Background(), &schema.ToolArgument{Text: `{"distance":1}`})
	if err != nil {
		t.Fatalf("InvokableRun returned error: %v", err)
	}
	if result == nil || len(result.Parts) != 1 || result.Parts[0].Text != "ok" {
		t.Fatalf("unexpected tool result: %#v", result)
	}
	if len(observer.starts) != 1 || observer.starts[0] != "buffer" {
		t.Fatalf("unexpected observer starts: %#v", observer.starts)
	}
	if len(observer.ends) != 1 || !observer.ends[0].Success || observer.ends[0].Cancelled {
		t.Fatalf("unexpected observer ends: %#v", observer.ends)
	}
}

func TestWrapToolWithErrorHandlerMapsRegularErrorToStructuredToolResult(t *testing.T) {
	observer := &recordingObserver{}
	wrapped := wrapToolWithErrorHandler(&fakeEnhancedTool{
		info: &schema.ToolInfo{Name: "buffer", Desc: "缓冲区分析"},
		runFn: func(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
			return nil, errors.New("boom")
		},
	}, &capability.ControlPlane{Observer: observer, ErrorMapper: capability.DefaultErrorMapper{}})

	enhanced := wrapped.(tool.EnhancedInvokableTool)
	result, err := enhanced.InvokableRun(context.Background(), &schema.ToolArgument{Text: `{}`})
	if err != nil {
		t.Fatalf("expected mapped error result, got error: %v", err)
	}
	if result == nil || len(result.Parts) != 1 || result.Parts[0].Text == "" {
		t.Fatalf("expected structured tool result, got %#v", result)
	}
	if got := result.Parts[0].Text; got != `{"success":false,"error":"boom"}` {
		t.Fatalf("unexpected mapped result: %s", got)
	}
	if len(observer.ends) != 1 || observer.ends[0].Success || observer.ends[0].Error != "boom" {
		t.Fatalf("unexpected observer end: %#v", observer.ends)
	}
}

func TestWrapToolWithErrorHandlerMapsCodedToolErrorToStructuredToolResult(t *testing.T) {
	observer := &recordingObserver{}
	wrapped := wrapToolWithErrorHandler(&fakeEnhancedTool{
		info: &schema.ToolInfo{Name: "buffer", Desc: "缓冲区分析"},
		runFn: func(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
			return nil, &bridgeprovider.ToolInvokeError{
				Code:      bridgeprovider.ToolErrorCodeProviderMissing,
				Message:   "无可用 QGIS 插件实例",
				Retryable: true,
				ProviderDiagnostics: map[string]any{
					"provider_id": "qgis_bridge",
				},
			}
		},
	}, &capability.ControlPlane{Observer: observer, ErrorMapper: capability.DefaultErrorMapper{}})

	enhanced := wrapped.(tool.EnhancedInvokableTool)
	result, err := enhanced.InvokableRun(context.Background(), &schema.ToolArgument{Text: `{}`})
	if err != nil {
		t.Fatalf("expected mapped error result, got error: %v", err)
	}
	if result == nil || len(result.Parts) != 1 {
		t.Fatalf("expected structured tool result, got %#v", result)
	}
	want := `{"success":false,"error":"无可用 QGIS 插件实例","code":"TOOL_PROVIDER_MISSING","retryable":true,"provider_diagnostics":{"provider_id":"qgis_bridge"}}`
	if got := result.Parts[0].Text; got != want {
		t.Fatalf("unexpected mapped result: %s", got)
	}
}

func TestWrapToolWithErrorHandlerPassesThroughContextCancellation(t *testing.T) {
	observer := &recordingObserver{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wrapped := wrapToolWithErrorHandler(&fakeEnhancedTool{
		info: &schema.ToolInfo{Name: "buffer"},
		runFn: func(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
			return nil, ctx.Err()
		},
	}, &capability.ControlPlane{Observer: observer, ErrorMapper: capability.DefaultErrorMapper{}})

	enhanced := wrapped.(tool.EnhancedInvokableTool)
	result, err := enhanced.InvokableRun(ctx, &schema.ToolArgument{Text: `{}`})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation passthrough, got result=%#v err=%v", result, err)
	}
	if len(observer.ends) != 1 || !observer.ends[0].Cancelled {
		t.Fatalf("expected cancelled observation, got %#v", observer.ends)
	}
}

func TestWrapToolWithErrorHandlerPassesThroughInterruptRerun(t *testing.T) {
	observer := &recordingObserver{}
	interruptErr := compose.NewInterruptAndRerunErr("retry")
	wrapped := wrapToolWithErrorHandler(&fakeEnhancedTool{
		info: &schema.ToolInfo{Name: "buffer"},
		runFn: func(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
			return nil, interruptErr
		},
	}, &capability.ControlPlane{Observer: observer, ErrorMapper: capability.DefaultErrorMapper{}})

	enhanced := wrapped.(tool.EnhancedInvokableTool)
	result, err := enhanced.InvokableRun(context.Background(), &schema.ToolArgument{Text: `{}`})
	if !errors.Is(err, interruptErr) {
		t.Fatalf("expected interrupt rerun passthrough, got result=%#v err=%v", result, err)
	}
	if len(observer.ends) != 1 || observer.ends[0].Success || observer.ends[0].Cancelled {
		t.Fatalf("expected failed non-cancelled observation, got %#v", observer.ends)
	}
}
