package capability

import (
	"context"
	"time"

	"github.com/cloudwego/eino/schema"
)

type ToolObservation struct {
	ToolName  string
	Backend   string
	Duration  time.Duration
	Success   bool
	Error     string
	Cancelled bool
}

type ToolObserver interface {
	OnToolStart(ctx context.Context, toolName string)
	OnToolEnd(ctx context.Context, observation ToolObservation)
}

type ErrorMapper interface {
	MapToolError(ctx context.Context, toolName string, err error) (*schema.ToolResult, error)
}

type ControlPlane struct {
	ErrorMapper ErrorMapper
	Observer    ToolObserver
}

type NoopObserver struct{}

func (NoopObserver) OnToolStart(context.Context, string) {}

func (NoopObserver) OnToolEnd(context.Context, ToolObservation) {}

func NormalizeControlPlane(controlPlane *ControlPlane) *ControlPlane {
	if controlPlane == nil {
		controlPlane = &ControlPlane{}
	}
	if controlPlane.ErrorMapper == nil {
		controlPlane.ErrorMapper = DefaultErrorMapper{}
	}
	if controlPlane.Observer == nil {
		controlPlane.Observer = NoopObserver{}
	}
	return controlPlane
}
