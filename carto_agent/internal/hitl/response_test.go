package hitl

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestSingleResponseNormalizesFrontendAndLegacyChoices(t *testing.T) {
	info := normalizeRequestInfo(RequestInput{Type: "confirmation", SelectMode: "single"})
	for _, raw := range []any{
		map[string]any{"option": "allow"},
		map[string]any{"selected": "allow"},
		map[string]any{"selected": []any{"allow"}},
		map[string]any{"value": "allow"},
		map[string]any{"options": []string{"allow"}},
		"allow",
	} {
		out := normalizeResponse(info, true, raw)
		if !out.IsSelected("allow") || out.IsConfirmed() || len(out.Decisions) != 0 {
			t.Fatalf("valid choice did not normalize: %#v -> %+v", raw, out)
		}
		if !reflect.DeepEqual(out.UserData, raw) {
			t.Fatal("original payload changed")
		}
	}
}

func TestSingleResponseNeverInfersPermission(t *testing.T) {
	info := normalizeRequestInfo(RequestInput{Type: "confirmation", SelectMode: "single"})
	for _, raw := range []any{
		nil, map[string]any{}, map[string]any{"option": "deny"},
		map[string]any{"text": "allow"}, map[string]any{"input": "allow"},
		map[string]any{"confirmed": false}, map[string]any{"approved": true},
		map[string]any{"approved": false, "option": "allow"},
		map[string]any{"cancelled": true, "option": "allow"},
		map[string]any{"canceled": true, "confirmed": true},
		map[string]any{"confirmed": false, "option": "allow"},
		map[string]any{"selected": []string{"allow", "deny"}},
		map[string]any{"selected": []any{"allow", 123}},
		map[string]any{"selected": "allow", "option": "deny"},
		map[string]any{"decisions": map[string]any{"other": map[string]any{"option": "allow"}}},
	} {
		out := normalizeResponse(info, true, raw)
		if out.IsSelected("allow") || out.IsConfirmed() {
			t.Fatalf("ambiguous or negative response authorized work: %#v -> %+v", raw, out)
		}
	}
	if out := normalizeResponse(info, false, map[string]any{"option": "allow"}); out.Approved || out.IsSelected("allow") {
		t.Fatal("missing resume data authorized work")
	}
}

func TestResponsePreservesMultipleTextAndFileAnswers(t *testing.T) {
	info := normalizeRequestInfo(RequestInput{SelectMode: "multiple"})
	raw := map[string]any{"options": []any{"a", "b", "a"}, "text": "其他说明", "path": "/tmp/source.csv"}
	out := normalizeResponse(info, true, raw)
	answer, ok := out.Decision("")
	if !ok || !reflect.DeepEqual(answer.Selected, []string{"a", "b"}) || answer.Text != "其他说明" || out.IsSelected("a") || !reflect.DeepEqual(out.UserData, raw) {
		t.Fatalf("multi-answer was lost or became single permission: %+v", out)
	}
	for _, raw := range []any{map[string]any{"input": "使用用户数据"}, map[string]any{"path": "/tmp/user.csv", "guidance": "保留原文件"}} {
		out := normalizeResponse(info, true, raw)
		if !out.Approved || !reflect.DeepEqual(out.UserData, raw) || out.IsConfirmed() || out.IsSelected("allow") {
			t.Fatal("text/file response was lost or treated as authorization")
		}
	}
}

func TestBatchResponseAddressesOnlyDeclaredDecision(t *testing.T) {
	info := normalizeRequestInfo(RequestInput{Decisions: []DecisionItem{{ID: "year"}, {ID: "permission"}}})
	raw := map[string]any{"decisions": map[string]any{
		"year":       map[string]any{"selected": []any{"2020"}},
		"permission": map[string]any{"option": "deny", "note": "仅本地"},
		"other":      map[string]any{"option": "allow"},
	}}
	out := normalizeResponse(info, true, raw)
	if out.Answer != nil || len(out.Decisions) != 2 || out.IsSelected("allow") {
		t.Fatalf("batch answer leaked to single permission: %+v", out)
	}
	decision, ok := out.Decision("permission")
	if !ok || !decision.IsSelected("deny") || decision.Note != "仅本地" {
		t.Fatal("explicitly addressed answer was lost")
	}
	if _, ok := out.Decision("other"); ok {
		t.Fatal("undeclared question was admitted")
	}
	if _, ok := out.Decision(""); ok {
		t.Fatal("unspecified question selected an arbitrary answer")
	}
}

type permissionProbeTool struct {
	name       string
	permission bool
	responses  int
}

func (p *permissionProbeTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: p.name, Desc: "test explicit permission"}, nil
}

func (p *permissionProbeTool) InvokableRun(ctx context.Context, _ string, _ ...einotool.Option) (string, error) {
	out, err := Invoke(ctx, map[string]any{
		"type": "confirmation", "title": p.name, "select_mode": "single",
		"options": []map[string]any{{"label": "允许", "value": "allow"}, {"label": "拒绝", "value": "deny"}},
	})
	if err != nil {
		return "", err
	}
	p.responses++
	p.permission = out.IsSelected("allow")
	raw, err := json.Marshal(out)
	return string(raw), err
}

func TestConcurrentHITLResumeKeepsTargetAnswersSeparate(t *testing.T) {
	ctx := context.Background()
	first, second := &permissionProbeTool{name: "first"}, &permissionProbeTool{name: "second"}
	m := &sequenceModel{messages: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{
			{ID: "first-call", Function: schema.FunctionCall{Name: "first", Arguments: `{}`}},
			{ID: "second-call", Function: schema.FunctionCall{Name: "second", Arguments: `{}`}},
		}),
		schema.AssistantMessage("完成", nil),
	}}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "probe", Description: "probe", Model: m,
		ToolsConfig: responseTools(first, second),
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, CheckPointStore: runtimepersist.NewMemoryCheckpointStore(nil)})
	event := collectInterruptEvent(t, runner.Query(ctx, "开始", adk.WithCheckPointID("parallel-permissions")))
	ids := map[string]string{}
	for _, ic := range event.Action.Interrupted.InterruptContexts {
		if payload, ok := ic.Info.(InterruptPayload); ok && ic.IsRootCause {
			ids[payload.Title] = ic.ID
		}
	}
	if len(ids) != 2 || first.responses != 0 || second.responses != 0 {
		t.Fatalf("expected two independent paused tools: %+v", ids)
	}
	// Only the first target receives a response; the other must stay paused.
	iter, err := runner.ResumeWithParams(ctx, "parallel-permissions", &adk.ResumeParams{Targets: map[string]any{
		ids["first"]: map[string]any{"option": "allow"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	remaining := collectInterruptEvent(t, iter)
	if remaining == nil || first.responses != 1 || !first.permission || second.responses != 0 {
		t.Fatal("partial resume granted or reran an unrelated target")
	}
	secondID := ""
	for _, ic := range remaining.Action.Interrupted.InterruptContexts {
		if payload, ok := ic.Info.(InterruptPayload); ok && payload.Title == "second" && ic.IsRootCause {
			secondID = ic.ID
		}
	}
	if secondID == "" {
		t.Fatal("unanswered target disappeared")
	}
	iter, err = runner.ResumeWithParams(ctx, "parallel-permissions", &adk.ResumeParams{Targets: map[string]any{
		secondID: map[string]any{"option": "deny"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	collectFinalOutput(t, iter)
	if first.responses != 1 || !first.permission || second.responses != 1 || second.permission {
		t.Fatal("denial was treated as approval or completed permission was repeated")
	}
}

func TestGuardRejectsMissingAndNegativeConfirmation(t *testing.T) {
	for _, payload := range []any{nil, map[string]any{}, map[string]any{"confirmed": false}, map[string]any{"option": "allow"}, map[string]any{"decisions": map[string]any{"unrelated": true}}} {
		for _, enhanced := range []bool{false, true} {
			ctx := context.Background()
			plain := &countingInvokableTool{name: "layout_delete"}
			rich := &countingEnhancedTool{name: "layout_delete"}
			var base einotool.BaseTool = plain
			if enhanced {
				base = rich
			}
			m := &sequenceModel{messages: []*schema.Message{schema.AssistantMessage("", []schema.ToolCall{{ID: "delete", Function: schema.FunctionCall{Name: "layout_delete", Arguments: `{}`}}})}}
			agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
				Name: "guard-probe", Description: "guard probe", Model: m,
				ToolsConfig: responseTools(WrapHighRiskBaseTools([]einotool.BaseTool{base})...),
			})
			if err != nil {
				t.Fatal(err)
			}
			runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, CheckPointStore: runtimepersist.NewMemoryCheckpointStore(nil)})
			event := collectInterruptEvent(t, runner.Query(ctx, "删除", adk.WithCheckPointID("guard-negative")))
			iter, err := runner.ResumeWithParams(ctx, "guard-negative", &adk.ResumeParams{Targets: map[string]any{event.Action.Interrupted.InterruptContexts[0].ID: payload}})
			if err != nil {
				t.Fatal(err)
			}
			denied := false
			for {
				e, ok := iter.Next()
				if !ok {
					break
				}
				if e.Err != nil && strings.Contains(e.Err.Error(), "HITL_NOT_CONFIRMED") {
					denied = true
				}
			}
			if !denied || plain.calls != 0 || rich.calls != 0 {
				t.Fatalf("missing/negative response executed a guarded tool: enhanced=%v payload=%#v", enhanced, payload)
			}
		}
	}
}

func responseTools(tools ...einotool.BaseTool) adk.ToolsConfig {
	var cfg adk.ToolsConfig
	cfg.Tools = tools
	return cfg
}
