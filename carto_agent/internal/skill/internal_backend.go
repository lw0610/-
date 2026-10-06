package skill

import (
	"context"
	"fmt"
	"strings"
)

type InternalBackend struct {
	invoker InternalInvoker
}

func NewInternalBackend(invoker InternalInvoker) *InternalBackend {
	return &InternalBackend{invoker: invoker}
}

func (b *InternalBackend) ID() string {
	return RuntimeIDInternal
}

func (b *InternalBackend) Execute(ctx context.Context, toolName string, args map[string]any) (*BackendResult, error) {
	if b == nil || b.invoker == nil {
		return nil, fmt.Errorf("internal backend invoker is nil")
	}
	output, err := b.invoker(ctx, strings.TrimSpace(toolName), cloneMapAny(args))
	if err != nil {
		return nil, err
	}
	return &BackendResult{Success: true, Output: cloneAny(output)}, nil
}
