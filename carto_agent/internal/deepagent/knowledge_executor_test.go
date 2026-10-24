package deepagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

type rulesCaptureModel struct {
	base  extractionOnceModel
	rules string
}

func (m *rulesCaptureModel) Generate(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	found := false
	for _, message := range messages {
		found = found || strings.Contains(message.Content, m.rules)
	}
	if !found {
		m.base.t.Fatal("per-run business rules did not reach extraction request")
	}
	return m.base.Generate(ctx, messages, options...)
}
func (m *rulesCaptureModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.base.t.Fatal("unexpected stream")
	return nil, nil
}

func TestKnowledgeExecutorRulesResourcesCheckpointResumeAndFlowReceipt(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", root)
	sessionDir := filepath.Join(root, "session")
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		t.Fatal(err)
	}
	source := "红军从甲地行军到乙地。"
	sum := sha256.Sum256([]byte(source))
	contract := knowledge.Contract{ContractID: "executor-rules", SourceSHA256: hex.EncodeToString(sum[:]), JSONSchema: json.RawMessage(`{}`), Instructions: "服务器默认规则", ReviewProtocolVersion: 1, RuntimeSessionDir: sessionDir}
	responseTool := func(name string, handler func(map[string]any) map[string]any) tool.BaseTool {
		return utils.NewEnhancedTool(&schema.ToolInfo{Name: name}, func(callCtx context.Context, args map[string]any) (*schema.ToolResult, error) {
			if name == knowledgeCompileTool {
				scope, ok := resource.ScopeFromContext(callCtx)
				if !ok || scope.FlowID != "knowledge-flow" || len(scope.Linked) != 2 || !resource.InternalHandlesAllowed(callCtx) {
					t.Fatal("trusted resource scope was not restored", scope)
				}
			}
			raw, _ := json.Marshal(map[string]any{"success": true, "data": handler(args)})
			return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: string(raw)}}}, nil
		})
	}
	contractTool := responseTool(knowledgeContractTool, func(args map[string]any) map[string]any {
		if args["source_text"] != source {
			t.Fatal("original source changed")
		}
		raw, _ := json.Marshal(contract)
		data := map[string]any{}
		_ = json.Unmarshal(raw, &data)
		return data
	})
	scope := knowledgeLinkedResourceFixture(t, root)
	style, symbols := scope.Linked[0].Handle(), scope.Linked[1].Handle()
	compiles, validations := 0, 0
	compiler := responseTool(knowledgeCompileTool, func(args map[string]any) map[string]any {
		if args["route_style"].(map[string]any)["id"] != style["id"] || args["symbols"].(map[string]any)["id"] != symbols["id"] || args["rules"] != nil || args["source_text"] != source {
			t.Fatal("resource or rule/source handoff changed")
		}
		if args["validate_only"] == true {
			validations++
			return map[string]any{"review_protocol_version": 1, "validation": map[string]any{"valid": true, "errors": []any{}}}
		}
		compiles++
		if args["review_context"] == nil {
			t.Fatal("compilation bypassed signed review")
		}
		dir := filepath.Join(root, "dataset")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"places.csv", "routes.csv", "route_paths.csv", "points.csv", "symbol_mapping.csv", "callouts.csv"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("header\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return map[string]any{"dataset_dir": dir, "artifact_id": "reviewed-dataset", "task_id": "test", "counts": map[string]any{"places.csv": 2}, "warnings": []any{}}
	})
	cm := &rulesCaptureModel{base: extractionOnceModel{t: t}, rules: "流程覆盖规则"}
	agent := &knowledgeSubAgent{name: "renamed_executor", description: "路线知识抽取", instructions: "角色默认规则", chatModel: cm,
		modelIdentity: &knowledgeModelIdentity{Provider: "test", Model: "executor"}, config: knowledge.DefaultConfig(),
		tools: map[string]tool.BaseTool{knowledgeContractTool: contractTool, knowledgeCompileTool: compiler},
		resolveCoordinates: func(_ context.Context, graph map[string]any, _ string) error {
			for index, raw := range graph["spatial_entities"].([]any) {
				place := raw.(map[string]any)
				place["lon"], place["lat"], place["coordinate_source"] = 110.0+float64(index), 33.0, "gazetteer"
			}
			return nil
		}}
	store := knowledge.NewReviewStore(filepath.Join(root, "reviews"))
	ctx := knowledge.WithOriginalSource(WithKnowledgeReview(toolsession.WithContext(context.Background(), "session", filepath.Join(root, "project.qgz"), ""), store), source)
	resumeCtx := ctx
	ctx = resource.WithScope(ctx, scope)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, CheckPointStore: runtimepersist.NewMemoryCheckpointStore(nil)})
	rawInput, _ := json.Marshal(map[string]any{"source_ref": "current_user_message", "rules": cm.rules, "route_style": style, "symbols": symbols})
	iter := runner.Run(ctx, []*schema.Message{schema.UserMessage(string(rawInput))}, adk.WithCheckPointID("knowledge-flow"))
	var paused *adk.AgentEvent
	for event, ok := iter.Next(); ok; event, ok = iter.Next() {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			paused = event
		}
	}
	if paused == nil || len(paused.Action.Interrupted.InterruptContexts) == 0 || cm.base.calls != 1 || compiles != 0 {
		t.Fatal("missing actual review interrupt")
	}
	interrupt := paused.Action.Interrupted.InterruptContexts[0]
	info := interrupt.Info.(map[string]any)
	review, err := store.Get("session", info["review_id"].(string))
	if err != nil || review.Contract.Instructions != cm.rules || agent.instructions != "角色默认规则" {
		t.Fatal("rules did not remain local to this run", err)
	}
	if review.ResourceScope == nil || review.CompileArgs["symbols"].(map[string]any)["id"] != symbols["id"] {
		t.Fatal("resource lost at checkpoint")
	}
	if issues, err := ValidateKnowledgeReview(resumeCtx, store, review, compiler); err != nil || len(issues) != 0 || validations != 1 {
		t.Fatal(issues, err)
	}
	claimed, _, err := store.Claim(review.SessionID, review.ReviewID, review.Revision, "flow-review")
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := runner.ResumeWithParams(resumeCtx, "knowledge-flow", &adk.ResumeParams{Targets: map[string]any{interrupt.ID: map[string]any{"review_id": claimed.ReviewID, "revision": claimed.Revision}}})
	if err != nil {
		t.Fatal(err)
	}
	last := ""
	for event, ok := resumed.Next(); ok; event, ok = resumed.Next() {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.Output != nil && event.Output.MessageOutput != nil {
			message, err := event.Output.MessageOutput.GetMessage()
			if err != nil {
				t.Fatal(err)
			}
			last = message.Content
		}
	}
	if compiles != 1 || cm.base.calls != 1 {
		t.Fatal("resume repeated extraction")
	}
	step := flow.StepPlan{ExpectedOutputs: []flow.OutputDeclaration{{Label: "路线数据集", Type: "dataset", Format: "route_dataset", Required: true}}}
	receipt, err := flowrun.NewRouteKnowledgeExecutor().ToReceipt(step, last)
	if err != nil || receipt.Status != "completed" || receipt.Outputs["artifact_id"] != "reviewed-dataset" {
		t.Fatal(receipt, err)
	}
	if verification := flowrun.VerifyOutputs(ctx, step, receipt, root, nil); !verification.OK {
		t.Fatal(verification.Errors)
	}
	// Changing rules invalidates both extraction cache and approved-review reuse.
	newModel := &rulesCaptureModel{base: extractionOnceModel{t: t}, rules: "另一套本次规则"}
	agent.chatModel = newModel
	rawInput, _ = json.Marshal(map[string]any{"source_ref": "current_user_message", "rules": newModel.rules, "route_style": style, "symbols": symbols})
	_, err = agent.extractAndCompile(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage(string(rawInput))}}, nil)
	if _, ok := err.(*knowledgeReviewPause); !ok || newModel.base.calls != 1 {
		t.Fatal("changed rules reused incompatible extraction", err)
	}
	fallbackModel := &rulesCaptureModel{base: extractionOnceModel{t: t}, rules: "角色默认规则"}
	agent.chatModel = fallbackModel
	rawInput, _ = json.Marshal(map[string]any{"source_ref": "current_user_message", "symbols": symbols, "route_style": style})
	_, err = agent.extractAndCompile(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage(string(rawInput))}}, nil)
	if paused, ok := err.(*knowledgeReviewPause); !ok || paused.review.Contract.Instructions != fallbackModel.rules || fallbackModel.base.calls != 1 {
		t.Fatal("empty step rules did not fall back to role rules", err)
	}
	compatibleModel := &rulesCaptureModel{base: extractionOnceModel{t: t}, rules: "流程覆盖规则"}
	agent.chatModel = compatibleModel
	rawInput, _ = json.Marshal(map[string]any{"source_ref": "current_user_message", "rules": compatibleModel.rules, "route_style": style, "symbols": symbols})
	_, err = agent.extractAndCompile(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage(string(rawInput))}}, nil)
	if paused, ok := err.(*knowledgeReviewPause); !ok || compatibleModel.base.calls != 0 || paused.review.OriginalCacheInfo["status"] != "confirmed_hit" {
		t.Fatal("compatible reviewed extraction was not reused", err)
	}
}

func TestKnowledgeExecutorInputValidatesRulesAndVisualPaths(t *testing.T) {
	for _, value := range []string{`{"source_text":"原文","rules":false}`, `{"source_text":"原文","symbols":[]}`, `{"source_text":"原文","route_style":{}}`} {
		if _, _, err := resolveKnowledgeInput(&adk.AgentInput{Messages: []*schema.Message{schema.UserMessage(value)}}); err == nil {
			t.Fatal("untyped executor input accepted")
		}
	}
}

func knowledgeLinkedResourceFixture(t *testing.T, root string) resource.Scope {
	t.Helper()
	dir := filepath.Join(root, "flow")
	symbolDir := filepath.Join(dir, "resources", "symbols", "自定义符号")
	if err := os.MkdirAll(symbolDir, 0700); err != nil {
		t.Fatal(err)
	}
	stylePath := filepath.Join(dir, "resources", "route.csv")
	files := map[string]string{
		stylePath: "topology,side,route_type,draw_type,style_id\nstandard,friendly,main,bezier_arrow,custom\n",
		filepath.Join(symbolDir, "对照表.csv"):   "symbol_type,svg_path\nwaypoint,point.svg\n",
		filepath.Join(symbolDir, "point.svg"): `<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"><circle cx="4" cy="4" r="3"/></svg>`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	library := resource.NewLibrary("", "", bindings.New(filepath.Join(root, "bindings.yaml")))
	scope := resource.Scope{FlowID: "knowledge-flow", FlowDir: dir}
	for _, ref := range []resource.Ref{{Type: "route_style", Location: stylePath}, {Type: "symbols", Location: symbolDir}} {
		item := library.Resolve(context.Background(), ref, scope)
		if item.Handle() == nil {
			t.Fatal(item.Problems)
		}
		scope.Linked = append(scope.Linked, item)
	}
	return scope
}

func TestKnowledgeResourceInputUsesOnlyServerLinkedHandle(t *testing.T) {
	scope := knowledgeLinkedResourceFixture(t, t.TempDir())
	fake := scope.Linked[0].Handle()
	fake["entry"] = "/untrusted/model.csv"
	data, _ := json.Marshal(map[string]any{"source_text": "原文", "route_style": fake})
	input := &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage(string(data))}}
	if _, _, err := resolveKnowledgeInputWithContext(context.Background(), input); err == nil {
		t.Fatal("unscoped handle accepted")
	}
	_, args, err := resolveKnowledgeInputWithContext(resource.WithScope(context.Background(), scope), input)
	if err != nil || args["route_style"].(map[string]any)["entry"] != scope.Linked[0].Entry {
		t.Fatal(args, err)
	}
	fake["id"] = "route_style.unlinked"
	data, _ = json.Marshal(map[string]any{"source_text": "原文", "route_style": fake})
	input.Messages[0].Content = string(data)
	if _, _, err = resolveKnowledgeInputWithContext(resource.WithScope(context.Background(), scope), input); err == nil {
		t.Fatal("unlinked handle accepted")
	}
}

func TestKnowledgeReviewResourceScopeRequiresOriginSessionAndSavedSnapshot(t *testing.T) {
	scope := knowledgeLinkedResourceFixture(t, t.TempDir())
	review := &knowledge.Review{SessionID: "source", ResourceScope: &scope, CompileArgs: map[string]any{"route_style": scope.Linked[0].Handle()}}
	if _, err := knowledgeReviewResourceContext(toolsession.WithContext(context.Background(), "other", "", ""), review); err == nil {
		t.Fatal("cross-session scope accepted")
	}
	ctx := toolsession.WithContext(context.Background(), "source", "", "")
	restored, err := knowledgeReviewResourceContext(ctx, review)
	current, ok := resource.ScopeFromContext(restored)
	if err != nil || !ok || current.FlowDir != scope.FlowDir || !resource.InternalHandlesAllowed(restored) {
		t.Fatal(current, err)
	}
	empty := resource.Scope{FlowID: scope.FlowID, FlowDir: scope.FlowDir}
	review.ResourceScope = &empty
	if _, err = knowledgeReviewResourceContext(ctx, review); err == nil || !strings.Contains(err.Error(), "作用域缺少") {
		t.Fatal("empty saved scope silently recovered", err)
	}
	review.ResourceScope = nil
	if _, err = knowledgeReviewResourceContext(ctx, review); err == nil || !strings.Contains(err.Error(), "缺少已保存的资源作用域") {
		t.Fatal("missing scope silently recovered", err)
	}
}

// A resumed review invokes the real shipped definition and engine. The selected
// run snapshot remains frozen until the controller offers a resource restart.
func TestKnowledgeReviewRestoredScopeKeepsLinkedResourceSnapshot(t *testing.T) {
	root := t.TempDir()
	scope := knowledgeLinkedResourceFixture(t, root)
	args := map[string]any{"route_style": scope.Linked[0].Handle(), "symbols": scope.Linked[1].Handle(),
		"knowledge_json": map[string]any{}, "source_text": "source", "contract_id": "contract"}
	review := &knowledge.Review{SessionID: "source", ResourceScope: &scope, CompileArgs: args}
	ctx, err := knowledgeReviewResourceContext(toolsession.WithContext(context.Background(), "source", "", ""), review)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scope.Linked[0].Entry, []byte("topology,side,route_type,draw_type,style_id\nstandard,friendly,main,bezier_arrow,updated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f := loadGEESkillIntegrationFixture(t)
	calls := 0
	backend := skill.NewQGISBackend(nil, func(ctx context.Context, _ string, received map[string]any) (any, error) {
		calls++
		data, _ := json.Marshal(received["route_style"])
		if !strings.Contains(string(data), `"style_id":"custom"`) {
			t.Fatalf("linked snapshot changed: %s", data)
		}
		return map[string]any{"success": true}, nil
	}, nil)
	engine := skill.NewEngine(f.registry.skills, backend)
	engine.SetResourceResolver(resource.NewLibrary("", "", bindings.New(filepath.Join(root, "bindings.yaml"))))
	if _, err := engine.Execute(ctx, knowledgeCompileTool, args); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("compiler not invoked")
	}

}
