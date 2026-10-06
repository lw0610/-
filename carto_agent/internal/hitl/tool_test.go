package hitl

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestHitlRequestInterruptAndResume(t *testing.T) {
	ctx := context.Background()
	hitlTool, err := NewRequestTool()
	if err != nil {
		t.Fatalf("create hitl tool failed: %v", err)
	}

	chatModel := &sequenceModel{
		messages: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{
				{
					ID: "1",
					Function: schema.FunctionCall{
						Name:      requestToolName,
						Arguments: `{"type":"clarification","title":"请确认","description":"请选择方案","options":[{"label":"A","value":"a"}],"default":"a"}`,
					},
				},
			}),
			schema.AssistantMessage("完成", nil),
		},
		validator: func(round int, input []*schema.Message) error {
			if round != 1 {
				return nil
			}
			for _, msg := range input {
				if msg.Role == schema.Tool && strings.Contains(msg.Content, `"user_data":"a"`) {
					return nil
				}
			}
			return fmt.Errorf("resume data not found in tool message")
		},
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "orchestrator",
		Description: "test",
		Model:       chatModel,
		ToolsConfig: func() adk.ToolsConfig {
			cfg := adk.ToolsConfig{}
			cfg.Tools = []einotool.BaseTool{hitlTool}
			return cfg
		}(),
	})
	if err != nil {
		t.Fatalf("create chat model agent failed: %v", err)
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		CheckPointStore: runtimepersist.NewMemoryCheckpointStore(nil),
	})

	iter := runner.Query(ctx, "开始", adk.WithCheckPointID("cp-hitl"))
	interruptEvent := collectInterruptEvent(t, iter)
	if interruptEvent == nil {
		t.Fatalf("expected interrupt event")
	}

	rootCtx := interruptEvent.Action.Interrupted.InterruptContexts[0]
	info, ok := rootCtx.Info.(InterruptPayload)
	if !ok {
		t.Fatalf("interrupt info type mismatch: %T", rootCtx.Info)
	}
	if got := info.Type; got != "clarification" {
		t.Fatalf("unexpected interrupt type: %v", got)
	}

	iter, err = runner.ResumeWithParams(ctx, "cp-hitl", &adk.ResumeParams{
		Targets: map[string]any{
			rootCtx.ID: "a",
		},
	})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}

	last := collectFinalOutput(t, iter)
	if !strings.Contains(last, "完成") {
		t.Fatalf("unexpected final output: %q", last)
	}
}

func TestHighRiskGuardBlocksUntilConfirmed(t *testing.T) {
	ctx := context.Background()
	rawTool := &countingInvokableTool{name: "layout_delete"}
	guarded := WrapHighRiskTools([]einotool.InvokableTool{rawTool})
	if len(guarded) != 1 {
		t.Fatalf("unexpected wrapped tool count: %d", len(guarded))
	}

	chatModel := &sequenceModel{
		messages: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{
				{
					ID: "1",
					Function: schema.FunctionCall{
						Name:      "layout_delete",
						Arguments: `{"name":"layoutA"}`,
					},
				},
			}),
			schema.AssistantMessage("删除完成", nil),
		},
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "orchestrator",
		Description: "test",
		Model:       chatModel,
		ToolsConfig: func() adk.ToolsConfig {
			cfg := adk.ToolsConfig{}
			cfg.Tools = []einotool.BaseTool{guarded[0]}
			return cfg
		}(),
	})
	if err != nil {
		t.Fatalf("create chat model agent failed: %v", err)
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		CheckPointStore: runtimepersist.NewMemoryCheckpointStore(nil),
	})

	iter := runner.Query(ctx, "删除布局", adk.WithCheckPointID("cp-guard"))
	interruptEvent := collectInterruptEvent(t, iter)
	if interruptEvent == nil {
		t.Fatalf("expected interrupt event")
	}
	if rawTool.calls != 0 {
		t.Fatalf("tool should not run before confirmation, calls=%d", rawTool.calls)
	}

	rootCtx := interruptEvent.Action.Interrupted.InterruptContexts[0]
	iter, err = runner.ResumeWithParams(ctx, "cp-guard", &adk.ResumeParams{
		Targets: map[string]any{
			rootCtx.ID: nil,
		},
	})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}

	_ = collectFinalOutput(t, iter)
	if rawTool.calls != 1 {
		t.Fatalf("tool should run after confirmation, calls=%d", rawTool.calls)
	}
}

func TestNormalizeRequestInfoDecisionPayload(t *testing.T) {
	info := normalizeRequestInfo(RequestInput{
		Type:       "decision",
		SelectMode: "multiple",
		Options: []RequestOption{
			{Label: "方案 A", Value: "a"},
			{Label: "方案 B", Value: "b"},
		},
		Defaults: []string{"a", "a", ""},
		Decisions: []DecisionItem{
			{
				Title:      "路线选择",
				SelectMode: "single",
				Options: []RequestOption{
					{Label: "保守", Value: "safe"},
					{Label: "激进", Value: "fast"},
				},
			},
			{
				ID:         "risk_confirm",
				Title:      "风险确认",
				SelectMode: "confirm",
				Required:   true,
			},
		},
		Required: true,
		Category: "approach",
	})

	if info.Type != "decision" {
		t.Fatalf("unexpected type: %s", info.Type)
	}
	if info.SelectMode != "multiple" {
		t.Fatalf("unexpected select mode: %s", info.SelectMode)
	}
	if !reflect.DeepEqual(info.Defaults, []string{"a"}) {
		t.Fatalf("unexpected defaults: %#v", info.Defaults)
	}
	if !info.Required {
		t.Fatalf("required flag should be true")
	}
	if info.Category != "approach" {
		t.Fatalf("unexpected category: %s", info.Category)
	}
	if len(info.Decisions) != 2 {
		t.Fatalf("unexpected decision count: %d", len(info.Decisions))
	}
	if info.Decisions[0].ID != "decision_1" {
		t.Fatalf("unexpected auto decision id: %s", info.Decisions[0].ID)
	}
	if info.Decisions[0].SelectMode != "single" {
		t.Fatalf("unexpected decision select mode: %s", info.Decisions[0].SelectMode)
	}
	if info.Decisions[1].ID != "risk_confirm" {
		t.Fatalf("unexpected explicit decision id: %s", info.Decisions[1].ID)
	}
	if !info.Decisions[1].Required {
		t.Fatalf("decision required flag should be true")
	}
}

func TestExtractDecisionResultsFromResumeData(t *testing.T) {
	raw := map[string]any{
		"decisions": map[string]any{
			"decision_1": map[string]any{
				"selected":  []any{"safe", "fast"},
				"text":      "优先稳妥",
				"confirmed": true,
			},
			"decision_2": "fallback",
		},
	}

	results := extractDecisionResults(raw)
	if len(results) != 2 {
		t.Fatalf("unexpected result count: %d", len(results))
	}

	first, ok := results["decision_1"]
	if !ok {
		t.Fatalf("decision_1 missing")
	}
	if !reflect.DeepEqual(first.Selected, []string{"safe", "fast"}) {
		t.Fatalf("unexpected selected options: %#v", first.Selected)
	}
	if first.Text != "优先稳妥" {
		t.Fatalf("unexpected text: %s", first.Text)
	}
	if first.Confirmed == nil || !*first.Confirmed {
		t.Fatalf("confirmed should be true")
	}

	second, ok := results["decision_2"]
	if !ok {
		t.Fatalf("decision_2 missing")
	}
	if !reflect.DeepEqual(second.Selected, []string{"fallback"}) {
		t.Fatalf("unexpected second selected: %#v", second.Selected)
	}
}

func TestExtractDecisionResultsSkipsPlainPayload(t *testing.T) {
	results := extractDecisionResults(map[string]any{
		"option": "safe",
		"text":   "普通补充",
	})
	if len(results) != 0 {
		t.Fatalf("plain payload should not be treated as decisions: %#v", results)
	}
}

func TestLowRiskDeleteLikeToolSkipsGuard(t *testing.T) {
	ctx := context.Background()
	rawTool := &countingInvokableTool{name: "delete_duplicate_geometries"}
	wrapped := WrapHighRiskTools([]einotool.InvokableTool{rawTool})
	if len(wrapped) != 1 {
		t.Fatalf("unexpected wrapped tool count: %d", len(wrapped))
	}

	if _, guarded := wrapped[0].(*guardedInvokableTool); guarded {
		t.Fatalf("low-risk cleanup tool should not be guarded")
	}

	if _, err := wrapped[0].InvokableRun(ctx, `{}`); err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	if rawTool.calls != 1 {
		t.Fatalf("tool should run directly, calls=%d", rawTool.calls)
	}
}

func TestUnknownDeleteLikeToolStillGuarded(t *testing.T) {
	ctx := context.Background()
	rawTool := &countingInvokableTool{name: "delete_all_records"}
	wrapped := WrapHighRiskTools([]einotool.InvokableTool{rawTool})
	if len(wrapped) != 1 {
		t.Fatalf("unexpected wrapped tool count: %d", len(wrapped))
	}

	if _, guarded := wrapped[0].(*guardedInvokableTool); !guarded {
		t.Fatalf("unknown delete-like tool should remain guarded")
	}

	if _, err := wrapped[0].InvokableRun(ctx, `{}`); err == nil {
		t.Fatalf("expected interrupt error before confirmation")
	}
	if rawTool.calls != 0 {
		t.Fatalf("guarded tool should not execute before confirmation, calls=%d", rawTool.calls)
	}
}

func TestWrapHighRiskBaseToolsGuardsEnhancedTool(t *testing.T) {
	ctx := context.Background()
	rawTool := &countingEnhancedTool{name: "delete_all_records"}
	wrapped := WrapHighRiskBaseTools([]einotool.BaseTool{rawTool})
	if len(wrapped) != 1 {
		t.Fatalf("unexpected wrapped tool count: %d", len(wrapped))
	}

	enhanced, ok := wrapped[0].(einotool.EnhancedInvokableTool)
	if !ok {
		t.Fatalf("expected enhanced invokable tool, got %T", wrapped[0])
	}

	if _, err := enhanced.InvokableRun(ctx, &schema.ToolArgument{Text: `{}`}); err == nil {
		t.Fatalf("expected interrupt error before confirmation")
	}
	if rawTool.calls != 0 {
		t.Fatalf("guarded enhanced tool should not execute before confirmation, calls=%d", rawTool.calls)
	}
}

type sequenceModel struct {
	messages  []*schema.Message
	index     int
	validator func(round int, input []*schema.Message) error
}

func (m *sequenceModel) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if m.validator != nil {
		if err := m.validator(m.index, input); err != nil {
			return nil, err
		}
	}
	if m.index >= len(m.messages) {
		return nil, errors.New("no more messages")
	}
	msg := m.messages[m.index]
	m.index++
	return msg, nil
}

func (m *sequenceModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream is not implemented")
}

func (m *sequenceModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

type countingInvokableTool struct {
	name  string
	calls int
}

func (t *countingInvokableTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: "counting tool"}, nil
}

func (t *countingInvokableTool) InvokableRun(_ context.Context, _ string, _ ...einotool.Option) (string, error) {
	t.calls++
	return `{"success":true,"info":"ok"}`, nil
}

type countingEnhancedTool struct {
	name  string
	calls int
}

func (t *countingEnhancedTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: "counting enhanced tool"}, nil
}

func (t *countingEnhancedTool) InvokableRun(_ context.Context, _ *schema.ToolArgument, _ ...einotool.Option) (*schema.ToolResult, error) {
	t.calls++
	return &schema.ToolResult{
		Parts: []schema.ToolOutputPart{
			{
				Type: schema.ToolPartTypeText,
				Text: `{"success":true,"info":"ok"}`,
			},
		},
	}, nil
}

func collectInterruptEvent(t *testing.T, iter *adk.AsyncIterator[*adk.AgentEvent]) *adk.AgentEvent {
	t.Helper()
	for {
		event, ok := iter.Next()
		if !ok {
			return nil
		}
		if event.Err != nil {
			t.Fatalf("unexpected event error: %v", event.Err)
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			return event
		}
	}
}

func collectFinalOutput(t *testing.T, iter *adk.AsyncIterator[*adk.AgentEvent]) string {
	t.Helper()
	last := ""
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("unexpected event error: %v", event.Err)
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			t.Fatalf("unexpected interrupt during resume")
		}
		if event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.Message != nil {
			last = event.Output.MessageOutput.Message.Content
		}
	}
	return last
}
