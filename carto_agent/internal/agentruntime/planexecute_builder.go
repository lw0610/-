package agentruntime

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func (f *Factory) buildPlanComponents(ctx context.Context, activeConfigName string, activeConfig *config.AgentConfigFile) (adk.ResumableAgent, error) {
	planCfg := activeConfig.Plan
	planAgentDef := selectPlanMainAgent(activeConfig)
	if planAgentDef == nil {
		return nil, fmt.Errorf("plan main agent not found")
	}

	plannerModel, err := f.resolvePlanChatModel(planProvider(planAgentDef))
	if err != nil {
		return nil, fmt.Errorf("resolve planner model: %w", err)
	}

	skillDirs := config.ResolveRuntimeSkillDirsWithPlugin(f.config.PluginSkillsDir, f.config.BuiltinSkillsDir, f.config.UserSkillsDir, f.config.ConfigBaseDir, activeConfigName)
	packageCatalog, err := subagents.LoadPackageCatalogMultiDir(skillDirs...)
	if err != nil {
		return nil, fmt.Errorf("load package tools: %w", err)
	}
	plannerRuntimeSkillConfig := buildPlanRuntimeSkillConfig(planCfg, planAgentDef, packageCatalog)
	if err := validatePlanConfigRuntime(ctx, planAgentDef, planCfg); err != nil {
		return nil, fmt.Errorf("validate plan runtime: %w", err)
	}

	selectedTools := selectPlanTools(ctx, f.config.BaseTools, planCfg, planAgentDef, packageCatalog)
	selectedTools = dedupeToolsByName(ctx, selectedTools)
	if len(selectedTools) == 0 {
		globalPackages := []string(nil)
		if planCfg != nil {
			globalPackages = append(globalPackages, planCfg.Global.ToolPackages...)
		}
		agentPackages := []string(nil)
		if planAgentDef != nil {
			agentPackages = append(agentPackages, planAgentDef.ToolPackages...)
		}
		return nil, fmt.Errorf("plan mode 未选择到任何可用工具: package_tools=%d global_skill_packages=%v agent_skill_packages=%v", len(packageCatalog.Tools), globalPackages, agentPackages)
	}
	instruction := planInstruction(ctx, planAgentDef, selectedTools)
	var middlewares []adk.AgentMiddleware
	if plannerRuntimeSkillConfig != nil {
		skillMw, err := subagents.LoadSkillMiddleware(ctx, planRuntimeAgentName, skillDirs, skillDirs, plannerRuntimeSkillConfig)
		if err != nil {
			return nil, fmt.Errorf("load planner skill middleware: %w", err)
		}
		if skillMw != nil {
			middlewares = append(middlewares, *skillMw)
		}
	}

	planAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "planner_runtime",
		Description: "plan mode runtime",
		Instruction: instruction,
		Model:       plannerModel,
		Middlewares: middlewares,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: selectedTools,
			},
		},
		GenModelInput: buildPlannerGenInputFn(planAgentDef),
		MaxIterations: planMaxIterations(planCfg, f.maxIterations()),
	})
	if err != nil {
		return nil, fmt.Errorf("build plan runtime: %w", err)
	}

	return planAgent, nil
}

func planProvider(planAgent *config.PlanAgentDefData) string {
	if planAgent == nil {
		return ""
	}
	return strings.TrimSpace(planAgent.LLM.Provider)
}

func planInstruction(ctx context.Context, planAgent *config.PlanAgentDefData, tools []tool.BaseTool) string {
	if planAgent == nil {
		return formatPlanToolCatalog(ctx, tools)
	}

	parts := make([]string, 0, 3)
	if text := strings.TrimSpace(planAgent.Prompts["static_instructions"]); text != "" {
		parts = append(parts, text)
	}
	if text := strings.TrimSpace(planAgent.Prompts["runtime_mode_instruction"]); text != "" {
		parts = append(parts, text)
	}
	parts = append(parts, strings.TrimSpace("## BUILD 能力感知\n调用 `query_build_capabilities` 工具可查询当前方案在 BUILD 模式下各个 agent 的可用技能包与工具列表。\n在输出执行计划前，先确认 BUILD 模式真实可用能力，避免规划不可执行步骤。"))
	if text := formatPlanToolCatalog(ctx, tools); text != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n")
}

func buildPlannerGenInputFn(planAgent *config.PlanAgentDefData) adk.GenModelInput {
	userTemplate := "{{ user_input }}"
	if planAgent != nil {
		if tpl := strings.TrimSpace(planAgent.Prompts["user_prompt_template"]); tpl != "" {
			userTemplate = tpl
		}
	}

	return func(_ context.Context, instruction string, input *adk.AgentInput) ([]adk.Message, error) {
		inputLen := 0
		if input != nil {
			inputLen = len(input.Messages)
		}
		msgs := make([]adk.Message, 0, inputLen+1)
		if trimmed := strings.TrimSpace(instruction); trimmed != "" {
			msgs = append(msgs, schema.SystemMessage(trimmed))
		}
		if input == nil || len(input.Messages) == 0 {
			return msgs, nil
		}

		history := clonePlanMessages(input.Messages)
		if lastUserIdx := findLastUserMessageIndex(history); lastUserIdx >= 0 {
			rendered := strings.TrimSpace(renderPlanUserPrompt(userTemplate, []adk.Message{history[lastUserIdx]}))
			if rendered != "" {
				rewritten := clonePlanMessage(history[lastUserIdx])
				rewritten.Content = rendered
				history[lastUserIdx] = rewritten
			}
		}

		msgs = append(msgs, history...)
		return msgs, nil
	}
}

func renderPlanUserPrompt(template string, userInput []adk.Message) string {
	rendered := strings.TrimSpace(template)
	if rendered == "" {
		rendered = "{{ user_input }}"
	}

	userText := formatPlanUserInput(userInput)
	rendered = strings.ReplaceAll(rendered, "{{ user_input }}", userText)
	rendered = strings.ReplaceAll(rendered, "{{user_input}}", userText)
	return rendered
}

func formatPlanUserInput(input []adk.Message) string {
	var lines []string
	for _, msg := range input {
		if msg == nil {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		lines = append(lines, content)
	}
	return strings.Join(lines, "\n")
}

func formatPlanToolCatalog(ctx context.Context, tools []tool.BaseTool) string {
	if len(tools) == 0 {
		return "## 当前可用规划工具\n- 当前未接入可用工具，请基于已有上下文完成结构化规划。"
	}

	lines := make([]string, 0, len(tools)+1)
	for _, item := range tools {
		info, err := item.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		name := strings.TrimSpace(info.Name)
		if name == "" {
			continue
		}
		desc := strings.TrimSpace(info.Desc)
		if desc == "" {
			lines = append(lines, fmt.Sprintf("- `%s`", name))
			continue
		}
		lines = append(lines, fmt.Sprintf("- `%s`: %s", name, desc))
	}
	if len(lines) == 0 {
		return "## 当前可用规划工具\n- 当前未接入可用工具，请基于已有上下文完成结构化规划。"
	}

	sort.Strings(lines)
	return "## 当前可用规划工具\n" + strings.Join(lines, "\n")
}

func planMaxIterations(planCfg *config.PlanConfig, fallback int) int {
	if planCfg != nil && planCfg.MaxIterations > 0 {
		return planCfg.MaxIterations
	}
	if fallback > 0 {
		return fallback
	}
	return 20
}

func appendToolIfMissing(ctx context.Context, tools []tool.BaseTool, extra tool.BaseTool) []tool.BaseTool {
	if extra == nil {
		return tools
	}
	extraInfo, err := extra.Info(ctx)
	if err != nil || extraInfo == nil {
		return tools
	}
	extraName := normalizeToolName(extraInfo.Name)
	if extraName == "" {
		return tools
	}
	for idx, item := range tools {
		if item == nil {
			continue
		}
		info, err := item.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		if normalizeToolName(info.Name) == extraName {
			replaced := append([]tool.BaseTool(nil), tools...)
			replaced[idx] = extra
			return replaced
		}
	}
	return append(tools, extra)
}

func dedupeToolsByName(ctx context.Context, tools []tool.BaseTool) []tool.BaseTool {
	if len(tools) <= 1 {
		return tools
	}
	seen := make(map[string]struct{}, len(tools))
	result := make([]tool.BaseTool, 0, len(tools))
	for _, item := range tools {
		if item == nil {
			continue
		}
		info, err := item.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		name := normalizeToolName(info.Name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, item)
	}
	return result
}

func clonePlanMessages(messages []adk.Message) []adk.Message {
	cloned := make([]adk.Message, len(messages))
	for idx, msg := range messages {
		cloned[idx] = clonePlanMessage(msg)
	}
	return cloned
}

func clonePlanMessage(msg adk.Message) adk.Message {
	if msg == nil {
		return nil
	}
	cp := *msg
	return &cp
}

func findLastUserMessageIndex(messages []adk.Message) int {
	for idx := len(messages) - 1; idx >= 0; idx-- {
		msg := messages[idx]
		if msg == nil {
			continue
		}
		if msg.Role == schema.User {
			return idx
		}
	}
	return -1
}
