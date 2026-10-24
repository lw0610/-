package subagents

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

// commonBehaviorNorms 通用行为规范，注入到所有智能体提示词中
const commonBehaviorNorms = `## 通用行为规范
- 使用中文响应，专业术语保留英文并附中文说明。
- 每次操作前，先用 get_project_info 或 get_layers_info 确认当前工程状态，避免操作不存在的图层。
- 工具调用失败时：报告错误信息 → 分析原因 → 尝试一次修正重试 → 仍失败则向上报告。
- 操作完成后汇报【操作】【结果】【输出位置】三要素。
- 涉及文件写入时，先确认输出路径存在且不会覆盖重要数据。

`

// SubAgentInfo 子智能体信息（用于提示词渲染）
type SubAgentInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	UseCases    string `json:"use_cases"`
	Priority    int    `json:"priority"`
	ToolCount   int    `json:"tool_count"`
}

// TemplateEngine 提示词模板引擎
type TemplateEngine struct {
	runtimeConfig *RuntimeSkillConfig
	toolInfoCache map[string]ToolInfo
}

// NewTemplateEngine 创建模板引擎
func NewTemplateEngine(runtimeConfig *RuntimeSkillConfig) *TemplateEngine {
	return &TemplateEngine{
		runtimeConfig: runtimeConfig,
		toolInfoCache: make(map[string]ToolInfo),
	}
}

// CacheToolInfos 缓存所有工具信息
func (e *TemplateEngine) CacheToolInfos(ctx context.Context, tools []tool.BaseTool) error {
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil {
			continue
		}

		e.toolInfoCache[info.Name] = ToolInfo{
			Name:        info.Name,
			Desc:        info.Desc,
			Description: info.Desc, // 别名
		}
	}
	return nil
}

// GetToolInfo 获取工具信息
func (e *TemplateEngine) GetToolInfo(toolName string) (ToolInfo, bool) {
	info, ok := e.toolInfoCache[toolName]
	return info, ok
}

// BuildOrchestratorPrompt 构建 Orchestrator 的完整提示词
func (e *TemplateEngine) BuildOrchestratorPrompt(
	ctx context.Context,
	tools []tool.BaseTool,
) (string, error) {
	// 确保工具信息已缓存
	if len(e.toolInfoCache) == 0 {
		if err := e.CacheToolInfos(ctx, tools); err != nil {
			return "", err
		}
	}

	agentConfig, ok := e.runtimeConfig.Agents["orchestrator"]
	if !ok {
		return "", fmt.Errorf("orchestrator 配置不存在")
	}

	var sb strings.Builder

	// 1. 写入介绍
	sb.WriteString("你是AutoCarto AI的编排智能体，是整个多智能体系统的核心调度中枢。\n\n")

	// 2. 动态生成子智能体表格
	sb.WriteString("## 可调度的专业智能体\n\n")
	sb.WriteString(e.generateSubAgentsTable(ctx, tools))
	sb.WriteString("\n")

	// 3. 动态生成智能体能力详情
	sb.WriteString("## 智能体能力详情\n\n")
	sb.WriteString(e.generateSubAgentsDetails(ctx, tools))
	sb.WriteString("\n")

	// 4. 注入通用行为规范
	sb.WriteString(commonBehaviorNorms)

	// 5. 写入静态指令
	if agentConfig.Prompts.StaticInstructions != "" {
		sb.WriteString(agentConfig.Prompts.StaticInstructions)
	}

	return sb.String(), nil
}

// BuildSubAgentPrompt 构建子智能体的完整提示词
func (e *TemplateEngine) BuildSubAgentPrompt(
	ctx context.Context,
	agentName string,
	filteredTools []tool.BaseTool,
) (string, error) {
	// 确保工具信息已缓存
	if len(e.toolInfoCache) == 0 {
		if err := e.CacheToolInfos(ctx, filteredTools); err != nil {
			return "", err
		}
	}

	agentConfig, ok := e.runtimeConfig.Agents[agentName]
	if !ok {
		return "", fmt.Errorf("智能体 %s 配置不存在", agentName)
	}

	var sb strings.Builder

	// 1. 写入介绍
	sb.WriteString(fmt.Sprintf("你是%s，专注于%s。\n\n",
		agentConfig.DisplayName, agentConfig.Description))

	sb.WriteString("## 核心能力\n\n")
	sb.WriteString(e.generateToolListSection(ctx, agentName, filteredTools))
	sb.WriteString("\n")

	// 3. 注入通用行为规范
	sb.WriteString(commonBehaviorNorms)

	// 4. 写入静态指令
	if agentConfig.Prompts.StaticInstructions != "" {
		sb.WriteString(agentConfig.Prompts.StaticInstructions)
	}

	return sb.String(), nil
}

// generateSubAgentsTable 生成子智能体表格
func (e *TemplateEngine) generateSubAgentsTable(
	ctx context.Context,
	tools []tool.BaseTool,
) string {
	var sb strings.Builder

	// 收集并排序子智能体
	agents := e.collectSubAgents(ctx, tools)

	// 生成表格
	sb.WriteString("| 智能体 | 职能 | 适用场景 | 工具数量 |\n")
	sb.WriteString("|--------|------|----------|----------|\n")

	for _, agent := range agents {
		useCases := agent.UseCases
		if useCases == "" {
			useCases = agent.Description
		}
		// 截断过长的描述
		if len(useCases) > 40 {
			useCases = useCases[:37] + "..."
		}

		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %d |\n",
			agent.Name,
			agent.DisplayName,
			useCases,
			agent.ToolCount))
	}

	return sb.String()
}

// generateSubAgentsDetails 生成子智能体详情
func (e *TemplateEngine) generateSubAgentsDetails(
	ctx context.Context,
	tools []tool.BaseTool,
) string {
	var sb strings.Builder

	agents := e.collectSubAgents(ctx, tools)

	for _, agent := range agents {
		sb.WriteString(fmt.Sprintf("### %s (%s)\n", agent.DisplayName, agent.Name))
		sb.WriteString(fmt.Sprintf("- **职能**: %s\n", agent.Description))

		if agent.UseCases != "" {
			sb.WriteString(fmt.Sprintf("- **适用场景**: %s\n", agent.UseCases))
		}

		sb.WriteString(fmt.Sprintf("- **可用工具数**: %d\n", agent.ToolCount))

		sb.WriteString("\n")
	}

	return sb.String()
}

func (e *TemplateEngine) generateToolListSection(
	ctx context.Context,
	agentName string,
	filteredTools []tool.BaseTool,
) string {
	var sb strings.Builder
	_ = agentName

	toolInfos := make([]ToolInfo, 0, len(filteredTools))
	for _, item := range filteredTools {
		info, err := item.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		toolInfo, ok := e.toolInfoCache[info.Name]
		if !ok {
			toolInfo = ToolInfo{Name: info.Name, Desc: info.Desc, Description: info.Desc}
		}
		toolInfos = append(toolInfos, toolInfo)
	}
	if len(toolInfos) == 0 {
		return "- 当前未配置可用工具。\n"
	}

	sort.Slice(toolInfos, func(i, j int) bool {
		return toolInfos[i].Name < toolInfos[j].Name
	})
	for _, item := range toolInfos {
		desc := item.Desc
		if len(desc) > 80 {
			desc = desc[:77] + "..."
		}
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", item.Name, desc))
	}

	return sb.String()
}

// collectSubAgents 收集子智能体信息
func (e *TemplateEngine) collectSubAgents(
	ctx context.Context,
	tools []tool.BaseTool,
) []SubAgentInfo {
	var agents []SubAgentInfo

	for agentName, agentConfig := range e.runtimeConfig.Agents {
		if agentConfig.IsMain || agentName == "orchestrator" || !agentConfig.Enabled || agentConfig.FlowOnly {
			continue
		}

		// 计算工具数量
		filter := CreateRuntimeSkillToolFilter(agentName, e.runtimeConfig)
		filteredTools := filter.Filter(ctx, tools)

		info := SubAgentInfo{
			Name:        agentName,
			DisplayName: agentConfig.DisplayName,
			Description: agentConfig.Description,
			ToolCount:   len(filteredTools),
		}

		agents = append(agents, info)
	}

	// 按优先级排序
	sort.Slice(agents, func(i, j int) bool {
		if agents[i].Priority != agents[j].Priority {
			return agents[i].Priority < agents[j].Priority
		}
		return agents[i].Name < agents[j].Name
	})

	return agents
}

// GenerateToolListPrompt 生成工具列表提示词（简单格式）
func (e *TemplateEngine) GenerateToolListPrompt(ctx context.Context, tools []tool.BaseTool) string {
	var sb strings.Builder
	sb.WriteString("\n## 可用工具\n\n")

	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil {
			continue
		}
		sb.WriteString("- **")
		sb.WriteString(info.Name)
		sb.WriteString("**: ")
		sb.WriteString(info.Desc)
		sb.WriteString("\n")
	}

	return sb.String()
}
