package deepagent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/autocarto/carto_agent/internal/config"
)

type fakeToolCallingChatModel struct{}

func (f *fakeToolCallingChatModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage("ok", nil), nil
}

func (f *fakeToolCallingChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (f *fakeToolCallingChatModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func TestBuildRuntimeHandlersWithoutProviderConfig(t *testing.T) {
	handlers, err := buildRuntimeHandlers(context.Background(), "orchestrator", &fakeToolCallingChatModel{}, nil, runtimeHandlerOptions{})
	if err != nil {
		t.Fatalf("buildRuntimeHandlers() error = %v", err)
	}

	if len(handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(handlers))
	}
	if got := handlerTypeName(handlers[0]); !strings.Contains(got, "patchtoolcalls") {
		t.Fatalf("expected patchtoolcalls handler, got %s", got)
	}
}

func TestBuildRuntimeHandlersWithSummarization(t *testing.T) {
	handlers, err := buildRuntimeHandlers(context.Background(), "orchestrator", &fakeToolCallingChatModel{}, &config.LLMProviderConfig{
		MaxContextTokens:      128000,
		MaxContextUsage:       90,
		AutoCompressThreshold: 85,
		CompressStrategy:      "summary",
	}, runtimeHandlerOptions{})
	if err != nil {
		t.Fatalf("buildRuntimeHandlers() error = %v", err)
	}

	if len(handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(handlers))
	}
	if got := handlerTypeName(handlers[0]); !strings.Contains(got, "patchtoolcalls") {
		t.Fatalf("expected patchtoolcalls handler, got %s", got)
	}
	if got := handlerTypeName(handlers[1]); !strings.Contains(got, "summarization") {
		t.Fatalf("expected summarization handler, got %s", got)
	}
}

func TestBuildRuntimeHandlersWithUnsupportedStrategy(t *testing.T) {
	handlers, err := buildRuntimeHandlers(context.Background(), "orchestrator", &fakeToolCallingChatModel{}, &config.LLMProviderConfig{
		MaxContextTokens:      128000,
		MaxContextUsage:       90,
		AutoCompressThreshold: 85,
		CompressStrategy:      "custom",
	}, runtimeHandlerOptions{})
	if err != nil {
		t.Fatalf("buildRuntimeHandlers() error = %v", err)
	}

	if len(handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(handlers))
	}
	if got := handlerTypeName(handlers[0]); !strings.Contains(got, "patchtoolcalls") {
		t.Fatalf("expected patchtoolcalls handler, got %s", got)
	}
}

func TestBuildRuntimeHandlersWithReductionStrategies(t *testing.T) {
	tests := []struct {
		name     string
		strategy string
	}{
		{name: "truncate", strategy: config.CompressStrategyTruncate},
		{name: "sliding window", strategy: config.CompressStrategySlidingWindow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, err := buildRuntimeHandlers(context.Background(), "orchestrator", &fakeToolCallingChatModel{}, &config.LLMProviderConfig{
				MaxContextTokens:      128000,
				MaxContextUsage:       90,
				AutoCompressThreshold: 85,
				CompressStrategy:      tt.strategy,
			}, runtimeHandlerOptions{})
			if err != nil {
				t.Fatalf("buildRuntimeHandlers() error = %v", err)
			}
			if len(handlers) != 2 {
				t.Fatalf("expected 2 handlers, got %d", len(handlers))
			}
			if got := handlerTypeName(handlers[0]); !strings.Contains(got, "patchtoolcalls") {
				t.Fatalf("expected patchtoolcalls handler, got %s", got)
			}
			if got := handlerTypeName(handlers[1]); !strings.Contains(got, "reduction") {
				t.Fatalf("expected reduction handler, got %s", got)
			}
		})
	}
}

func TestBuildRuntimeHandlersWithReductionOffloadFilesystem(t *testing.T) {
	handlers, err := buildRuntimeHandlers(context.Background(), "researcher", &fakeToolCallingChatModel{}, &config.LLMProviderConfig{
		MaxContextTokens:      128000,
		MaxContextUsage:       90,
		AutoCompressThreshold: 85,
		CompressStrategy:      config.CompressStrategyTruncate,
	}, runtimeHandlerOptions{
		ActiveConfigName: "default",
		ConfigBaseDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("buildRuntimeHandlers() error = %v", err)
	}

	if len(handlers) != 3 {
		t.Fatalf("expected 3 handlers, got %d", len(handlers))
	}
	if got := handlerTypeName(handlers[1]); !strings.Contains(got, "filesystem") {
		t.Fatalf("expected filesystem handler, got %s", got)
	}
	if got := handlerTypeName(handlers[2]); !strings.Contains(got, "reduction") {
		t.Fatalf("expected reduction handler, got %s", got)
	}
}

func TestReductionRetentionSuffixLimit(t *testing.T) {
	if got := reductionRetentionSuffixLimit(config.CompressStrategyTruncate); got != 1 {
		t.Fatalf("truncate retention = %d, want 1", got)
	}
	if got := reductionRetentionSuffixLimit(config.CompressStrategySlidingWindow); got != 3 {
		t.Fatalf("sliding window retention = %d, want 3", got)
	}
}

func handlerTypeName(handler any) string {
	return fmt.Sprintf("%T", handler)
}
