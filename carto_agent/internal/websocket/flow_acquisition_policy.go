package websocket

import (
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
)

func acquisitionPolicyRules(policy string) string {
	switch policy {
	case "allow":
		return "[flow_auto_acquire:allow] 可获取且当前可用的输入缺失时，直接传 {$acquire:true}；启动后说明获取条目与来源。用户给了不完整文件夹时传 {$fill:true,path:真实文件夹}。"
	case "deny":
		return "[flow_auto_acquire:deny] 不得传 $acquire 或 $fill。缺少必填数据时请用户提供。"
	default:
		return "[flow_auto_acquire:ask] 可获取的输入缺失且用户未表态时，只询问一次由用户提供还是自动获取；用户表示没有数据、请系统寻找或无所谓时，传 {$acquire:true}；用户提供不完整文件夹时传 {$fill:true,path:真实文件夹}。"
	}
}
func validateOrchestratorAcquisition(plan *flow.FlowPlan, inputs map[string]any) error {
	if err := runtimecontrol.ValidateFlowToolInputs(inputs); err != nil {
		return err
	}
	for _, input := range plan.Inputs {
		_, marked, err := flow.ParseInputChoice(inputs[input.Key])
		if !marked || err != nil {
			continue
		}
		if plan.AutoAcquire == "deny" {
			return fmt.Errorf("FLOW_ACQUIRE_DENIED: 当前方案不自动获取数据，请提供%s", input.Label)
		}
		if !input.Acquirable || !input.AcquireAvailable {
			return fmt.Errorf("FLOW_INPUT_ACQUIRE_UNAVAILABLE: %s 当前没有可用获取方式，请提供数据或配置数据源", input.Label)
		}
	}
	return nil
}
func checkedAcquisitionPreview(ctx context.Context, compiler flowrun.Compiler, plan *flow.FlowPlan, inputs map[string]any, root string) (*flow.FlowPlan, []flowrun.InputProblem, error) {
	if plan.Acquisition == nil {
		return plan, nil, nil
	}
	loader, ok := compiler.(interface {
		AcquisitionEnvironment(context.Context, string) (datasource.PlanningEnvironment, error)
	})
	if !ok {
		return nil, nil, fmt.Errorf("数据获取目录服务尚未就绪")
	}
	env, err := loader.AcquisitionEnvironment(ctx, plan.Scheme)
	if err != nil {
		return nil, nil, err
	}
	linked, problems := flowrun.ValidateInputsWithAcquisition(ctx, plan, inputs, root, env, nil)
	return linked, problems, nil
}
func plannedAcquisitionSummary(plan *flow.FlowPlan) []flow.AcquisitionItem {
	items := []flow.AcquisitionItem{}
	if plan.Acquisition != nil {
		for _, item := range plan.Acquisition.Items {
			if item.Mode == "auto" || item.Mode == "fill" {
				items = append(items, item)
			}
		}
	}
	return items
}
func flowInputSummaries(inputs []flow.InputPlan) []map[string]any {
	result := []map[string]any{}
	for _, input := range inputs {
		modes := input.Modes
		if modes == nil {
			modes = []string{"user"}
		}
		result = append(result, map[string]any{"key": input.Key, "label": input.Label, "type": input.Type, "required": input.Required, "default": input.Default, "options": input.Options, "description": input.Description, "remember": input.Remember, "acquirable": input.Acquirable, "acquire_available": input.AcquireAvailable, "modes": modes})
	}
	return result
}
