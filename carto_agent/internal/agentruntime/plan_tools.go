package agentruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/logger"
)

const planRuntimeAgentName = "planner_runtime"

func buildPlanRuntimeSkillConfig(planCfg *config.PlanConfig, planAgent *config.PlanAgentDefData, packageCatalog subagents.PackageCatalog) *subagents.RuntimeSkillConfig {
	if planCfg == nil || planAgent == nil {
		return nil
	}
	return &subagents.RuntimeSkillConfig{
		Mode: "plan",
		Global: subagents.RuntimeSkillGlobalConfig{
			SkillPackages:   append([]string(nil), planCfg.Global.ToolPackages...),
			PackageTools:    packageCatalog.Tools,
			PackageMetadata: packageCatalog.Metadata,
		},
		Agents: map[string]subagents.RuntimeSkillAgentConfig{
			planRuntimeAgentName: {
				Enabled:      true,
				IsMain:       true,
				ToolPackages: append([]string{}, planAgent.ToolPackages...),
			},
		},
	}
}

func resolvePlanCategorySkillTools(planCfg *config.PlanConfig, planAgent *config.PlanAgentDefData, packageCatalog subagents.PackageCatalog) []string {
	if len(packageCatalog.Tools) == 0 {
		return nil
	}
	runtimeConfig := buildPlanRuntimeSkillConfig(planCfg, planAgent, packageCatalog)
	if runtimeConfig == nil {
		return nil
	}
	return runtimeConfig.GetAgentAllowedTools(planRuntimeAgentName)
}

func validatePlanConfigRuntime(
	ctx context.Context,
	planAgent *config.PlanAgentDefData,
	planCfg *config.PlanConfig,
) error {
	_ = ctx
	if planCfg == nil {
		return fmt.Errorf("plan 配置为空")
	}
	if planAgent == nil {
		return fmt.Errorf("plan 主智能体为空")
	}

	tpl := strings.TrimSpace(planAgent.Prompts["user_prompt_template"])
	if tpl == "" {
		return fmt.Errorf("plan.agents.main.prompts.user_prompt_template 不能为空")
	}
	if !strings.Contains(tpl, "{{ user_input }}") {
		return fmt.Errorf("plan.agents.main.prompts.user_prompt_template 必须包含 {{ user_input }}")
	}

	return nil
}

func selectPlanTools(
	ctx context.Context,
	baseTools []tool.BaseTool,
	planCfg *config.PlanConfig,
	planAgent *config.PlanAgentDefData,
	packageCatalog subagents.PackageCatalog,
) []tool.BaseTool {
	if len(baseTools) == 0 || planAgent == nil {
		return nil
	}

	toolNames := make([]string, 0, len(baseTools))
	nameToTool := make(map[string]tool.BaseTool, len(baseTools))
	for _, item := range baseTools {
		info, err := item.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		name := normalizeToolName(info.Name)
		if name == "" {
			continue
		}
		if _, exists := nameToTool[name]; exists {
			continue
		}
		nameToTool[name] = item
		toolNames = append(toolNames, name)
	}

	requested := make(map[string]bool)
	for _, toolName := range resolvePlanCategorySkillTools(planCfg, planAgent, packageCatalog) {
		if toolName != "" {
			requested[toolName] = true
		}
	}
	if len(requested) == 0 {
		globalPackages := []string(nil)
		if planCfg != nil {
			globalPackages = append(globalPackages, planCfg.Global.ToolPackages...)
		}
		agentPackages := []string(nil)
		if planAgent != nil {
			agentPackages = append(agentPackages, planAgent.ToolPackages...)
		}
		logger.Warn("plan tools 选择结果为空: package_tools=%d global_skill_packages=%v agent_skill_packages=%v", len(packageCatalog.Tools), globalPackages, agentPackages)
		return nil
	}

	allowed := make(map[string]bool, len(requested))
	switch strings.ToLower(strings.TrimSpace(planAgent.Mode)) {
	case config.PlanToolStrategyAll:
		for name := range requested {
			allowed[name] = true
		}
	case config.PlanToolStrategyReadonlyOnly:
		for name := range requested {
			if isReadonlyToolName(name) {
				allowed[name] = true
			}
		}
	default:
		for name := range requested {
			if isReadonlyToolName(name) || isLightweightValidationToolName(name) {
				allowed[name] = true
			}
		}
	}

	selected := make([]tool.BaseTool, 0, len(allowed))
	for _, name := range toolNames {
		if name == "flow_start" {
			continue // Starting execution is unavailable in Plan even with mode=all.
		}
		if !allowed[name] {
			continue
		}
		item, ok := nameToTool[name]
		if !ok {
			continue
		}
		selected = append(selected, item)
	}

	return selected
}
func normalizeToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func isReadonlyToolName(name string) bool {
	normalized := normalizeToolName(name)
	if normalized == "" {
		return false
	}

	readonlyPrefixes := []string{
		"get_",
		"list_",
		"show_",
		"describe_",
		"query_",
	}
	for _, prefix := range readonlyPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}

	readonlySuffixes := []string{
		"_info",
		"_list",
		"_status",
		"_statistics",
	}
	for _, suffix := range readonlySuffixes {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}

	readonlyTools := map[string]bool{
		"flow_preview":           true,
		"hitl_request":           true,
		"output_get_info":        true,
		"pyqgis_get_context":     true,
		"pyqgis_get_task_status": true,
		"layout_info":            true,
		"layout_list":            true,
		"raster_info":            true,
		"raster_statistics":      true,
	}

	return readonlyTools[normalized]
}

func isLightweightValidationToolName(name string) bool {
	normalized := normalizeToolName(name)
	if normalized == "" {
		return false
	}

	lightweightTools := map[string]bool{
		"check_validity": true,
	}

	return lightweightTools[normalized]
}
