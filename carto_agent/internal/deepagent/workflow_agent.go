package deepagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/autocarto/carto_agent/internal/workflow"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func buildWorkflowSubAgentConfig(agentName, displayName string, pkg skill.SkillPackage, baseTools []tool.BaseTool) (*subagents.GenericAgentConfig, error) {
	if pkg.Workflow == nil {
		return nil, fmt.Errorf("workflow package %q missing structured workflow definition", pkg.Name)
	}
	requiredToolNames := orderedWorkflowToolNames(pkg.Workflow.Spec.Steps)
	selectedTools := filterToolsByName(baseTools, requiredToolNames)
	return &subagents.GenericAgentConfig{
		Name:        agentName,
		DisplayName: displayName,
		Description: strings.TrimSpace(pkg.FrontMatter.Description),
		Instruction: buildWorkflowInstruction(pkg),
		Tools:       selectedTools,
	}, nil
}

func createWorkflowSubAgentFromPackage(_ context.Context, cfg *Config, agentName string, agentConfig subagents.RuntimeSkillAgentConfig, pkg skill.SkillPackage) (adk.Agent, error) {
	workflowCfg, err := buildWorkflowSubAgentConfig(agentName, agentConfig.DisplayName, pkg, cfg.Tools)
	if err != nil {
		return nil, err
	}
	return &workflowSubAgent{
		name:        agentName,
		description: firstNonEmpty(strings.TrimSpace(agentConfig.Description), strings.TrimSpace(workflowCfg.Description), strings.TrimSpace(agentConfig.DisplayName), agentName),
		workflow:    pkg.Workflow,
		tools:       indexToolsByName(workflowCfg.Tools),
	}, nil
}

type workflowSubAgent struct {
	name        string
	description string
	workflow    *workflow.Definition
	tools       map[string]tool.BaseTool
}

func (a *workflowSubAgent) Name(context.Context) string { return a.name }

func (a *workflowSubAgent) Description(context.Context) string { return a.description }

func (a *workflowSubAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		resolvedInputs, err := resolveWorkflowInputs(input)
		if err != nil {
			gen.Send(&adk.AgentEvent{AgentName: a.name, Err: err})
			return
		}
		plan, err := workflow.CompileSerialPlan(a.workflow, resolvedInputs)
		if err != nil {
			gen.Send(&adk.AgentEvent{AgentName: a.name, Err: err})
			return
		}
		for _, step := range plan.Steps {
			gen.Send(namedEvent(a.name, adk.EventFromMessage(schema.AssistantMessage(fmt.Sprintf("执行工作流步骤 [%s]: %s", step.ID, step.ToolName), nil), nil, schema.Assistant, "")))
			result, err := invokeWorkflowTool(ctx, a.tools[step.ToolName], step.ToolName, step.Arguments)
			if err != nil {
				gen.Send(&adk.AgentEvent{AgentName: a.name, Err: err})
				return
			}
			toolText := workflowToolResultText(result)
			gen.Send(namedEvent(a.name, adk.EventFromMessage(schema.ToolMessage(toolText, step.ID, schema.WithToolName(step.ToolName)), nil, schema.Tool, step.ToolName)))
			if failure := workflowToolFailure(result); failure != "" {
				gen.Send(&adk.AgentEvent{AgentName: a.name, Err: fmt.Errorf("workflow step %q failed: %s", step.ID, failure)})
				return
			}
		}
		gen.Send(namedEvent(a.name, adk.EventFromMessage(schema.AssistantMessage("工作流执行完成", nil), nil, schema.Assistant, "")))
	}()
	return iter
}

func orderedWorkflowToolNames(steps []workflow.Step) []string {
	seen := make(map[string]struct{}, len(steps))
	names := make([]string, 0, len(steps))
	for _, step := range steps {
		toolName := strings.TrimSpace(step.Tool)
		if toolName == "" {
			continue
		}
		if _, ok := seen[toolName]; ok {
			continue
		}
		seen[toolName] = struct{}{}
		names = append(names, toolName)
	}
	return names
}

func filterToolsByName(baseTools []tool.BaseTool, required []string) []tool.BaseTool {
	if len(baseTools) == 0 || len(required) == 0 {
		return nil
	}
	requiredSet := make(map[string]struct{}, len(required))
	for _, name := range required {
		requiredSet[strings.TrimSpace(name)] = struct{}{}
	}
	selected := make([]tool.BaseTool, 0, len(required))
	for _, item := range baseTools {
		info, err := item.Info(context.Background())
		if err != nil || info == nil {
			continue
		}
		if _, ok := requiredSet[strings.TrimSpace(info.Name)]; ok {
			selected = append(selected, item)
		}
	}
	return selected
}

func buildWorkflowInstruction(pkg skill.SkillPackage) string {
	parts := []string{strings.TrimSpace(pkg.FrontMatter.Description), "按固定串行步骤执行，不得跳步、并行或重排。"}
	for index, step := range pkg.Workflow.Spec.Steps {
		parts = append(parts, fmt.Sprintf("%d. [%s] 调用工具 %s", index+1, step.ID, step.Tool))
	}
	return strings.Join(parts, "\n")
}

func resolveWorkflowInputs(input *adk.AgentInput) (map[string]any, error) {
	if input == nil || len(input.Messages) == 0 {
		return nil, fmt.Errorf("workflow input payload is required as latest user JSON message")
	}
	for index := len(input.Messages) - 1; index >= 0; index-- {
		message := input.Messages[index]
		if message == nil || message.Role != schema.User {
			continue
		}
		payload := strings.TrimSpace(message.Content)
		if payload == "" {
			continue
		}
		var resolved map[string]any
		if err := json.Unmarshal([]byte(payload), &resolved); err != nil {
			return nil, fmt.Errorf("workflow inputs must be explicit JSON object in latest user message: %w", err)
		}
		return resolved, nil
	}
	return nil, fmt.Errorf("workflow input payload is required as latest user JSON message")
}

func invokeWorkflowTool(ctx context.Context, toolItem tool.BaseTool, toolName string, arguments map[string]any) (*schema.ToolResult, error) {
	if toolItem == nil {
		return nil, fmt.Errorf("workflow tool %q not found", strings.TrimSpace(toolName))
	}
	rawArgs, err := json.Marshal(arguments)
	if err != nil {
		return nil, err
	}
	if enhanced, ok := toolItem.(tool.EnhancedInvokableTool); ok {
		return enhanced.InvokableRun(ctx, &schema.ToolArgument{Text: string(rawArgs)})
	}
	if invokable, ok := toolItem.(tool.InvokableTool); ok {
		text, err := invokable.InvokableRun(ctx, string(rawArgs))
		if err != nil {
			return nil, err
		}
		return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: text}}}, nil
	}
	return nil, fmt.Errorf("workflow tool %q is not invokable", strings.TrimSpace(toolName))
}

func workflowToolResultText(result *schema.ToolResult) string {
	if result == nil {
		return ""
	}
	for _, part := range result.Parts {
		if part.Type == schema.ToolPartTypeText {
			return part.Text
		}
	}
	return ""
}

func workflowToolFailure(result *schema.ToolResult) string {
	text := strings.TrimSpace(workflowToolResultText(result))
	if text == "" {
		return ""
	}
	var payload struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return ""
	}
	if payload.Success {
		return ""
	}
	return strings.TrimSpace(payload.Error)
}

func indexToolsByName(baseTools []tool.BaseTool) map[string]tool.BaseTool {
	indexed := make(map[string]tool.BaseTool, len(baseTools))
	for _, item := range baseTools {
		if item == nil {
			continue
		}
		info, err := item.Info(context.Background())
		if err != nil || info == nil || strings.TrimSpace(info.Name) == "" {
			continue
		}
		indexed[strings.TrimSpace(info.Name)] = item
	}
	return indexed
}

func namedEvent(agentName string, event *adk.AgentEvent) *adk.AgentEvent {
	if event == nil {
		return &adk.AgentEvent{AgentName: agentName}
	}
	event.AgentName = agentName
	return event
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
