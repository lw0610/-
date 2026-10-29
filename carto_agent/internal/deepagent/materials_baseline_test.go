package deepagent

import (
	"context"
	"testing"

	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/taskmaterials"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

func TestMaterialBaselineKeepsEnhancedReaderAndDoesNotExpandOtherTools(t *testing.T) {
	ctx := context.Background()
	reader := utils.NewEnhancedTool(&schema.ToolInfo{Name: "file_read"}, func(context.Context, map[string]any) (*schema.ToolResult, error) { return nil, nil })
	writer := utils.NewTool(&schema.ToolInfo{Name: "file_write"}, func(context.Context, map[string]any) (string, error) { return "", nil })
	shell := utils.NewTool(&schema.ToolInfo{Name: "shell_execute"}, func(context.Context, map[string]any) (string, error) { return "", nil })
	selected := subagents.WithAttachmentReadTool(ctx, nil, []tool.BaseTool{writer, shell, reader})
	if len(selected) != 1 || selected[0] != reader {
		t.Fatalf("only the existing enhanced reader belongs in the baseline: %v", selected)
	}
	if len(subagents.WithAttachmentReadTool(ctx, selected, []tool.BaseTool{reader})) != 1 {
		t.Fatal("read baseline was duplicated")
	}
	handlers := withTaskMaterials(withTaskMaterials(nil))
	if len(handlers) != 1 {
		t.Fatal("task material middleware was duplicated")
	}
	prepared, run, err := handlers[0].BeforeAgent(ctx, &adk.ChatModelAgentContext{Instruction: "task rules"})
	if err != nil || run.Instruction != "task rules" {
		t.Fatalf("material context altered instructions: %v %v", run, err)
	}
	stale := schema.UserMessage("old material")
	stale.Extra = map[string]any{taskmaterials.MessageMarker: true}
	request := []*schema.Message{schema.SystemMessage("rules"), stale, schema.UserMessage("real task")}
	input, err := materialModelInput(prepared, request)
	if err != nil || len(input) != 2 || input[1].Content != "real task" || len(request) != 3 || request[1] != stale {
		t.Fatalf("model input must remove stale material without rewriting history: %v, %v", input, err)
	}
}
