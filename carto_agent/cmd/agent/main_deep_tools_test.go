package main

import (
	"context"
	"testing"

	"github.com/autocarto/carto_agent/internal/hitl"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type fakeAgentTool struct {
	name string
}

func (t *fakeAgentTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: "fake"}, nil
}

func (t *fakeAgentTool) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return `{"ok":true}`, nil
}

func TestPrepareAgentToolsDeduplicatesToolNames(t *testing.T) {
	requestTool, err := hitl.NewRequestTool()
	if err != nil {
		t.Fatalf("new request tool: %v", err)
	}

	prepared := prepareAgentTools([]einotool.BaseTool{
		&fakeAgentTool{name: "buffer"},
		requestTool,
	})

	counts := make(map[string]int)
	for _, item := range prepared {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		counts[info.Name]++
	}

	if counts["buffer"] != 1 {
		t.Fatalf("expected buffer once, got %d", counts["buffer"])
	}
	if counts["hitl_request"] != 1 {
		t.Fatalf("expected hitl_request once, got %d", counts["hitl_request"])
	}
}
