package flowrun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/planexec"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

func knowledgeRunFixture() (*RunState, flow.StepPlan) {
	step := flow.StepPlan{ID: "extract", Kind: "executor", Executor: "route_knowledge", Role: "renamed", Instructions: "本次业务规则", Refs: flow.PlanRefs{Inputs: []string{"source", "refresh"}}, ExpectedOutputs: []flow.OutputDeclaration{{Label: "路线数据集", Type: "dataset", Format: "route_dataset", Required: true}}}
	run := &RunState{Inputs: map[string]any{"source": map[string]any{"$ref": "current_user_message", "sha256": "reference-only", "length": 12}, "refresh": true}, Plan: &flow.FlowPlan{Inputs: []flow.InputPlan{{Key: "source", Type: "conversation_text"}, {Key: "refresh", Label: "重新抽取", Type: "boolean"}}}}
	return run, step
}

func TestRouteKnowledgePlanPassesExplicitPresetsToExecutor(t *testing.T) {
	p := &plan.Plan{APIVersion: plan.APIVersion, ID: "cp-route-presets", Version: 1, Goal: "根据史料生成路线数据",
		Data: []plan.Data{{ID: "history", Name: "史料", From: "conversation"}},
		Steps: []plan.Step{{ID: "extract", Title: "抽取路线", Capabilities: []string{"route_knowledge"}, Uses: []string{"history"},
			Instructions: "从原文提取路线", Parameters: map[string]string{"route_style": "用户路线预设", "symbols": "用户符号预设"},
			Outputs: []flow.OutputDeclaration{{Label: "路线数据集", Type: "dataset"}}}},
	}
	executable, inputs, err := planexec.Build(p, planexec.BuildOptions{OriginalSource: "甲地出发，乙地抵达。"})
	if err != nil {
		t.Fatal(err)
	}
	run := &RunState{Plan: executable, Inputs: inputs}
	step := executable.Steps[0]
	for _, withOldResources := range []bool{false, true} {
		if withOldResources {
			// Explicit presets do not require the legacy linked resource to be
			// available and must not be overwritten by an old snapshot.
			step.Refs.Resources = []string{"old-style"}
			executable.Resources = []flow.ResourcePlan{{Key: "old-style", Type: "route_style", Entry: "/retired/style.csv"}}
		}
		messages, err := NewRouteKnowledgeExecutor().BuildInput(step, run)
		if err != nil {
			t.Fatal(err)
		}
		var request map[string]any
		if err := json.Unmarshal([]byte(messages[0].Content), &request); err != nil {
			t.Fatal(err)
		}
		if request["source_ref"] != "current_task_input" || request["route_style"] != "用户路线预设" || request["symbols"] != "用户符号预设" || request["rules"] != p.Steps[0].Instructions {
			t.Fatalf("plan parameters did not reach the shared knowledge expert: %v", request)
		}
	}
}

func TestRouteKnowledgeBuildInputReferencesSourceAndOnlyReferencedResources(t *testing.T) {
	run, step := knowledgeRunFixture()
	dir := t.TempDir()
	builtin, err := filepath.Abs("../../../autocarto/resources/system")
	if err != nil {
		t.Fatal(err)
	}
	library := resource.NewLibrary(builtin, "", bindings.New(filepath.Join(dir, "bindings.yaml")))
	style := library.Resolve(context.Background(), resource.Ref{Type: "route_style", Location: "assets/resources/styles/路线样式-红蓝对比.csv"}, resource.Scope{FlowID: "marching-route", FlowDir: filepath.Join(builtin, "..", "..", "tools", "system", "route-cartography")})
	symbols := library.Resolve(context.Background(), resource.Ref{Type: "symbols", Location: "assets/resources/symbols/军标"}, resource.Scope{FlowID: "marching-route", FlowDir: filepath.Join(builtin, "..", "..", "tools", "system", "route-cartography")})
	if style.Handle() == nil || symbols.Handle() == nil {
		t.Fatal(style.Problems, symbols.Problems)
	}
	stylePlan, symbolsPlan := flow.ResourcePlanFromResolved(style), flow.ResourcePlanFromResolved(symbols)
	stylePlan.Key, symbolsPlan.Key = "style", "symbols"
	run.Plan.Resources = []flow.ResourcePlan{symbolsPlan, stylePlan, {Key: "ignored", Type: "route_style", Entry: "/not-used"}}
	step.Refs.Resources = []string{"symbols", "style"}
	before, _ := json.Marshal(run)
	adapter := NewRouteKnowledgeExecutor()
	registry := NewExecutorRegistry()
	if err := registry.Register(adapter); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("route_knowledge"); !ok {
		t.Fatal("executor not registered")
	}
	messages, err := adapter.BuildInput(step, run)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if len(messages) != 1 || json.Unmarshal([]byte(messages[0].Content), &payload) != nil {
		t.Fatal(messages)
	}
	if payload["source_ref"] != "current_task_input" || payload["rules"] != step.Instructions || payload["force_refresh"] != true || payload["symbols"].(map[string]any)["id"] != symbols.ID || payload["route_style"].(map[string]any)["id"] != style.ID {
		t.Fatal(payload)
	}
	for _, key := range []string{"symbol_table", "symbol_assets_dir", "route_style_table"} {
		if _, exists := payload[key]; exists {
			t.Fatal("legacy resource argument", key)
		}
	}

	if _, leaked := payload["source_text"]; leaked || strings.Contains(messages[0].Content, "reference-only") {
		t.Fatal("source metadata copied into model message")
	}
	after, _ := json.Marshal(run)
	if string(before) != string(after) {
		t.Fatal("adapter mutated run snapshot")
	}

}

func TestRouteKnowledgeBuildInputFallbackAndErrors(t *testing.T) {
	run, step := knowledgeRunFixture()
	adapter := NewRouteKnowledgeExecutor()
	step.Instructions = ""
	step.Refs.Inputs = []string{"source"}
	messages, err := adapter.BuildInput(step, run)
	if err != nil || strings.Contains(messages[0].Content, "rules") || !strings.Contains(messages[0].Content, `"force_refresh":false`) {
		t.Fatal(messages, err)
	}
	step.Refs.Inputs = nil
	if _, err := adapter.BuildInput(step, run); err == nil {
		t.Fatal("missing source reference accepted")
	}
	step.Refs.Inputs = []string{"source", "refresh"}
	run.Inputs["refresh"] = "true"
	if _, err := adapter.BuildInput(step, run); err == nil {
		t.Fatal("untyped refresh accepted")
	}
	run.Inputs["refresh"] = false
	step.Refs.Resources = []string{"unknown"}
	if _, err := adapter.BuildInput(step, run); err == nil {
		t.Fatal("unknown resource accepted")
	}
	if _, err := adapter.BuildInput(step, nil); err == nil {
		t.Fatal("nil run accepted")
	}
}

func TestRouteKnowledgeReceiptMapsDatasetAndPreservesMetadata(t *testing.T) {
	_, step := knowledgeRunFixture()
	data := map[string]any{"dataset_dir": "/output", "artifact_id": "artifact", "task_id": "task", "task_graph_file": "/graph", "counts": map[string]any{"points.csv": float64(2)}, "validation": map[string]any{"is_valid": true}, "warnings": []any{"保留提示"}}
	raw, _ := json.Marshal(map[string]any{"success": true, "data": data})
	receipt, err := NewRouteKnowledgeExecutor().ToReceipt(step, string(raw))
	if err != nil || receipt.Status != "completed" || receipt.Outputs["路线数据集"] != "/output" {
		t.Fatal(receipt, err)
	}
	for key, value := range data {
		if key != "dataset_dir" && !reflect.DeepEqual(receipt.Outputs[key], value) {
			t.Fatal(key)
		}
	}
	for _, input := range []string{`{}`, `{"success":true,"data":{}}`, `not json`} {
		if _, err := NewRouteKnowledgeExecutor().ToReceipt(step, input); err == nil {
			t.Fatal("invalid success accepted", input)
		}
	}
	failed, err := NewRouteKnowledgeExecutor().ToReceipt(step, `{"success":false,"info":"编译失败"}`)
	if err != nil || failed.Status != "failed" || len(failed.Issues) != 1 {
		t.Fatal(failed, err)
	}
}

func TestControllerConsumesKnowledgeReceiptWithoutModelRepair(t *testing.T) {
	_, step := knowledgeRunFixture()
	p := testPlan(1)
	step.Index, step.OnFailure = 1, "stop"
	p.Steps = []flow.StepPlan{step}
	p.Roles = []flow.RoleSpec{{ID: "renamed", Kind: "executor", Executor: "route_knowledge", FlowOnly: true, Enabled: true}}
	p.Inputs = []flow.InputPlan{{Key: "source", Type: "conversation_text", Required: true}}
	step.Refs.Inputs = []string{"source"}
	p.Steps[0] = step
	c, request, events := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.OriginalSource != "原文逐字保留" || strings.Contains(r.Messages[0].Content, r.OriginalSource) {
			t.Fatal("source context lost or copied")
		}
		dir := filepath.Join(r.SessionDir, "dataset")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"places.csv", "routes.csv", "route_paths.csv", "points.csv", "symbol_mapping.csv", "callouts.csv"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("header\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		raw, _ := json.Marshal(map[string]any{"success": true, "data": map[string]any{"dataset_dir": dir, "artifact_id": "actual-handoff"}})
		return StepResult{LastContent: string(raw)}, nil
	}, func(options *Options) {
		options.Executors = NewExecutorRegistry()
		if err := options.Executors.Register(NewRouteKnowledgeExecutor()); err != nil {
			t.Fatal(err)
		}
		options.Model = modelFunc(func(context.Context, string) (string, error) {
			t.Fatal("deterministic receipt called model repair")
			return "", nil
		})
	})
	request.OriginalSource = "原文逐字保留"
	run, err := c.Start(context.Background(), request)
	if err != nil || run.Status != "completed" || run.Steps[0].Outputs["artifact_id"] != "actual-handoff" || !events.has("flow.step_completed") {
		t.Fatal(run, err)
	}
	raw, err := os.ReadFile(run.RunJSONPath)
	if err != nil || strings.Contains(string(raw), request.OriginalSource) {
		t.Fatal("raw source persisted in flow state", err)
	}
}
