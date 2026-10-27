package runtimecontrol

import (
	"context"
	"fmt"
	"strings"
)

const ToolFlowStart = "flow_start"
const ToolFlowPreview = "flow_preview"

// FlowService is scoped to a trusted session run, not a process-global callback.
type FlowService interface {
	StartFlow(context.Context, string, map[string]any) (map[string]any, error)
	PreviewFlow(context.Context, string, map[string]any) (map[string]any, error)
}
type flowServiceKey struct{}

func WithFlowService(ctx context.Context, service FlowService) context.Context {
	return context.WithValue(ctx, flowServiceKey{}, service)
}
func invokeFlowTool(ctx context.Context, name string, args map[string]any) (any, error) {
	service, ok := ctx.Value(flowServiceKey{}).(FlowService)
	if !ok || service == nil {
		return nil, fmt.Errorf("当前运行未配置流程服务")
	}
	id := strings.TrimSpace(stringArg(args, "flow_id"))
	if id == "" {
		return nil, fmt.Errorf("flow_id 不能为空")
	}
	inputs := map[string]any{}
	if raw, exists := args["inputs"]; exists && raw != nil {
		var ok bool
		inputs, ok = raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("inputs 必须是对象")
		}
	}
	if err := ValidateFlowToolInputs(inputs); err != nil {
		return nil, err
	}
	if name == ToolFlowStart {
		return service.StartFlow(ctx, id, inputs)
	}
	return service.PreviewFlow(ctx, id, inputs)
}

// Source overrides belong to the explicit flow form, not model-generated tool arguments.
func ValidateFlowToolInputs(inputs map[string]any) error {
	for key, value := range inputs {
		marker, ok := value.(map[string]any)
		if !ok {
			continue
		}
		_, acquire := marker["$acquire"]
		_, fill := marker["$fill"]
		if !acquire && !fill {
			continue
		}
		for _, field := range []string{"source", "sources"} {
			if _, exists := marker[field]; exists {
				return fmt.Errorf("FLOW_ORCHESTRATOR_SOURCE_FORBIDDEN: 输入 %s 的来源只能在流程表单中选择，编排工具不得传 %s", key, field)
			}
		}
	}
	return nil
}
