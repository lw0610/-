package flowrun

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
)

func TestLayerVerificationNeverAcceptsFieldOrStyleNames(t *testing.T) {
	step := flow.StepPlan{ExpectedOutputs: []flow.OutputDeclaration{{Label: "图层", Type: "layers", Required: true}}}
	tools := toolFunc(func(context.Context, string, map[string]any) (any, error) {
		return map[string]any{"success": true, "info": map[string]any{"layers": []any{map[string]any{"id": "roads-1", "name": "roads", "fields": []any{map[string]any{"name": "population"}}, "renderer": map[string]any{"name": "red"}}}}}, nil
	})
	for _, bad := range []string{"population", "red"} {
		result := VerifyOutputs(context.Background(), step, Receipt{Outputs: map[string]any{"图层": []any{bad}}}, t.TempDir(), tools)
		if result.OK {
			t.Fatalf("nested metadata accepted as layer: %s", bad)
		}
	}
	if result := VerifyOutputs(context.Background(), step, Receipt{Outputs: map[string]any{"图层": []any{"roads-1", "roads"}}}, t.TempDir(), tools); !result.OK {
		t.Fatal(result)
	}
}
func TestKnowledgeExecutorManualGuidanceChangesEffectiveRules(t *testing.T) {
	input := flow.InputPlan{Key: "source", Label: "原文", Type: "conversation_text", Required: true}
	inputs := map[string]any{"source": ConversationReference("原始材料")}
	step := flow.StepPlan{Instructions: "保留路线", Refs: flow.PlanRefs{Inputs: []string{"source"}}}
	run := &RunState{Plan: &flow.FlowPlan{Inputs: []flow.InputPlan{input}}, Inputs: inputs, Steps: []StepState{{UserGuidance: "重新抽取并增加地点"}}}
	messages, err := NewRouteKnowledgeExecutor().BuildInput(step, run)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if json.Unmarshal([]byte(messages[0].Content), &payload) != nil {
		t.Fatal(messages[0].Content)
	}
	if rules, _ := payload["rules"].(string); !strings.Contains(rules, "保留路线") || !strings.Contains(rules, "重新抽取并增加地点") {
		t.Fatal("manual guidance absent", payload)
	}
	if strings.Contains(messages[0].Content, "原始材料") {
		t.Fatal("source copied into message")
	}
}
