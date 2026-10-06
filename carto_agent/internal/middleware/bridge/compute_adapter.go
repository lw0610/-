package bridge

import (
	"context"

	"github.com/autocarto/carto_agent/internal/compute"
)

type ComputeToolInvoker struct {
	Provider *Provider
}

func NewComputeToolInvoker(provider *Provider) ComputeToolInvoker {
	return ComputeToolInvoker{Provider: provider}
}

func (i ComputeToolInvoker) ListInstanceTools(ctx context.Context, instanceID string) ([]map[string]any, error) {
	if i.Provider == nil {
		return nil, nil
	}
	return i.Provider.ListInstanceTools(ctx, instanceID)
}

func (i ComputeToolInvoker) InvokeTool(ctx context.Context, toolName string, args map[string]any) (*compute.InvokeResponse, error) {
	if i.Provider == nil {
		return nil, nil
	}
	result, err := i.Provider.InvokeTool(ctx, toolName, args)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	outputLayers := make([]compute.OutputLayer, 0, len(result.OutputLayers))
	for _, layer := range result.OutputLayers {
		outputLayers = append(outputLayers, compute.OutputLayer{Path: layer.Path, AutoLoad: layer.AutoLoad})
	}
	return &compute.InvokeResponse{
		Success:             result.Success,
		Output:              result.Output,
		Error:               result.Error,
		ErrorCode:           result.ErrorCode,
		OutputLayers:        outputLayers,
		ProviderDiagnostics: cloneDiagnosticsMap(result.ProviderDiagnostics),
	}, nil
}
