package skill

import (
	"context"
	"fmt"
	"strings"

	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
)

type QGISBackend struct {
	responseInvoker  QGISResponseInvoker
	legacyInvoker    QGISInvoker
	runtimeInspector QGISRuntimeInspector
	argsMutator      ArgsMutator
}

func NewQGISBackend(
	responseInvoker QGISResponseInvoker,
	legacyInvoker QGISInvoker,
	runtimeInspector QGISRuntimeInspector,
	argsMutators ...ArgsMutator,
) *QGISBackend {
	var argsMutator ArgsMutator
	if len(argsMutators) > 0 {
		argsMutator = argsMutators[0]
	}
	return &QGISBackend{
		responseInvoker:  responseInvoker,
		legacyInvoker:    legacyInvoker,
		runtimeInspector: runtimeInspector,
		argsMutator:      argsMutator,
	}
}

func (b *QGISBackend) ID() string {
	return RuntimeIDQGIS
}

func (b *QGISBackend) Execute(ctx context.Context, toolName string, args map[string]any) (*BackendResult, error) {
	params := cloneMapAny(args)
	if params == nil {
		params = make(map[string]any)
	}
	if b.argsMutator != nil {
		b.argsMutator(ctx, params)
	}
	if err := b.preflight(params); err != nil {
		return nil, err
	}

	if b.responseInvoker != nil {
		resp, err := b.responseInvoker(ctx, strings.TrimSpace(toolName), params)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, fmt.Errorf("qgis backend returned nil response")
		}
		return &BackendResult{
			Success:      resp.Success,
			Output:       cloneAny(resp.Output),
			Error:        strings.TrimSpace(resp.Error),
			OutputLayers: toBackendOutputLayersFromCompute(resp.OutputLayers),
		}, nil
	}
	if b.legacyInvoker == nil {
		return nil, newQGISToolInvokeError(
			bridgepkg.ToolErrorCodeProviderMissing,
			"qgis invoker unavailable",
			true,
			b.notReadyDiagnostics(),
		)
	}
	output, err := b.legacyInvoker(ctx, strings.TrimSpace(toolName), params)
	if err != nil {
		return nil, err
	}
	return &BackendResult{Success: true, Output: cloneAny(output)}, nil
}

func (b *QGISBackend) preflight(args map[string]any) error {
	if b.responseInvoker == nil && b.legacyInvoker == nil {
		return newQGISToolInvokeError(
			bridgepkg.ToolErrorCodeProviderMissing,
			"qgis invoker unavailable",
			true,
			b.notReadyDiagnostics(),
		)
	}
	if b.runtimeInspector == nil {
		return nil
	}
	if explicitInstanceTarget(args) != "" {
		return nil
	}
	connected := b.runtimeInspector.ConnectedInstanceIDs()
	if len(connected) > 0 {
		return nil
	}
	return newQGISToolInvokeError(
		bridgepkg.ToolErrorCodeNotReady,
		"QGIS 工具当前未就绪：没有可用实例连接",
		true,
		b.notReadyDiagnostics(),
	)
}

func (b *QGISBackend) notReadyDiagnostics() map[string]any {
	diagnostics := map[string]any{
		"unavailable_reason": MissingReasonInteractiveInstanceDisconnected,
	}
	if b.runtimeInspector == nil {
		return diagnostics
	}
	if providerID := strings.TrimSpace(b.runtimeInspector.ProviderID()); providerID != "" {
		diagnostics["provider_id"] = providerID
	}
	if activeID := strings.TrimSpace(b.runtimeInspector.ActiveInstance()); activeID != "" {
		diagnostics["active_instance_id"] = activeID
	}
	diagnostics["connected_instance_ids"] = b.runtimeInspector.ConnectedInstanceIDs()
	return diagnostics
}

func explicitInstanceTarget(args map[string]any) string {
	for _, key := range []string{"target_instance_id", "instance_id"} {
		value, _ := args[key].(string)
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func newQGISToolInvokeError(code, message string, retryable bool, diagnostics map[string]any) error {
	return &bridgepkg.ToolInvokeError{
		Code:                strings.TrimSpace(code),
		Message:             strings.TrimSpace(message),
		Retryable:           retryable,
		ProviderDiagnostics: cloneComputeDiagnostics(diagnostics),
	}
}

func cloneComputeDiagnostics(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

var _ ToolBackend = (*QGISBackend)(nil)
