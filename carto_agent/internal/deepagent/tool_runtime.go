package deepagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// These helpers serve dedicated agents without introducing a second workflow runtime.
func invokeRuntimeTool(ctx context.Context, item tool.BaseTool, name string, arguments map[string]any) (*schema.ToolResult, error) {
	if item == nil {
		return nil, fmt.Errorf("tool %q not found", strings.TrimSpace(name))
	}
	raw, err := json.Marshal(arguments)
	if err != nil {
		return nil, err
	}
	if enhanced, ok := item.(tool.EnhancedInvokableTool); ok {
		return enhanced.InvokableRun(ctx, &schema.ToolArgument{Text: string(raw)})
	}
	if invokable, ok := item.(tool.InvokableTool); ok {
		text, err := invokable.InvokableRun(ctx, string(raw))
		if err != nil {
			return nil, err
		}
		return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: text}}}, nil
	}
	return nil, fmt.Errorf("tool %q is not invokable", strings.TrimSpace(name))
}

func runtimeToolResultText(result *schema.ToolResult) string {
	if result != nil {
		for _, part := range result.Parts {
			if part.Type == schema.ToolPartTypeText {
				return part.Text
			}
		}
	}
	return ""
}

func indexToolsByName(items []tool.BaseTool) map[string]tool.BaseTool {
	indexed := make(map[string]tool.BaseTool, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		info, err := item.Info(context.Background())
		if err == nil && info != nil && strings.TrimSpace(info.Name) != "" {
			indexed[strings.TrimSpace(info.Name)] = item
		}
	}
	return indexed
}

func namedEvent(name string, event *adk.AgentEvent) *adk.AgentEvent {
	if event == nil {
		return &adk.AgentEvent{AgentName: name}
	}
	event.AgentName = name
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
