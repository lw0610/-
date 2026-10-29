package deepagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

// Uses the real task tool and checkpoint machinery. Only model responses and
// the QGIS compiler are scripted, so failures must travel back through delegation.
type knowledgeContinuationModel struct {
	calls           int
	reviewID        string
	reviewRevision  int64
	completedResult string
	withSource      bool
}

func TestKnowledgeContinuationExtractsChangedOrExplicitlyRefreshedMaterials(t *testing.T) {
	for _, scenario := range []string{"changed_source", "force_refresh"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("AUTOCARTO_HOME", root)
			store := knowledge.NewReviewStore(filepath.Join(root, "reviews"))
			original := "红军从甲地行军到乙地。"
			sum := sha256.Sum256([]byte(original))
			graph := knowledge.Graph{"task": map[string]any{"title": "已核对路线"}, "spatial_entities": []any{map[string]any{"entity_id": "a", "name": "甲地", "lon": 110.0, "lat": 33.0}}}
			review, err := store.Create(knowledge.ReviewCreateInput{SessionID: "session", SourceText: original,
				Contract: knowledge.Contract{ContractID: "continuation-source", SourceSHA256: hex.EncodeToString(sum[:]), JSONSchema: json.RawMessage(`{}`)}, OriginalResult: &knowledge.Result{Graph: graph}, Graph: graph})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.Claim("session", review.ReviewID, review.Revision, "start-mapping"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Fail("session", review.ReviewID, review.Revision, []knowledge.ReviewError{{Message: "本地生成失败"}}); err != nil {
				t.Fatal(err)
			}
			source := original
			if scenario == "changed_source" {
				source += "甲地的材料已经补充。"
			}
			contractCalls := 0
			contractTool := utils.NewEnhancedTool(&schema.ToolInfo{Name: knowledgeContractTool}, func(_ context.Context, args map[string]any) (*schema.ToolResult, error) {
				contractCalls++
				if args["source_text"] != source {
					return nil, fmt.Errorf("changed source was not preserved")
				}
				hash := sha256.Sum256([]byte(source))
				contract := knowledge.Contract{ContractID: "continuation-source", SourceSHA256: hex.EncodeToString(hash[:]), JSONSchema: json.RawMessage(`{}`), Instructions: "抽取路线事实", ReviewProtocolVersion: 1, RuntimeSessionDir: root}
				raw, _ := json.Marshal(map[string]any{"success": true, "data": contract})
				return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: string(raw)}}}, nil
			})
			extraction := &extractionOnceModel{t: t}
			agent := &knowledgeSubAgent{chatModel: extraction, modelIdentity: &knowledgeModelIdentity{Provider: "test", Model: "changed-source"}, config: knowledge.DefaultConfig(),
				tools: map[string]tool.BaseTool{knowledgeContractTool: contractTool},
				resolveCoordinates: func(_ context.Context, graph map[string]any, _ string) error {
					for i, raw := range graph["spatial_entities"].([]any) {
						place := raw.(map[string]any)
						place["lon"], place["lat"], place["coordinate_source"] = 110.0+float64(i), 33.0, "gazetteer"
					}
					return nil
				}}
			ctx := knowledge.WithOriginalSource(WithKnowledgeReview(toolsession.WithContext(context.Background(), "session", filepath.Join(root, "project.qgz"), ""), store), source)
			request := map[string]any{"review_id": review.ReviewID, "revision": review.Revision, "source_ref": "current_user_message", "rules": "本次只抽取已发生的行军。", "force_refresh": scenario == "force_refresh"}
			raw, _ := json.Marshal(request)
			_, err = agent.extractAndCompile(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage(string(raw))}}, nil)
			var paused *knowledgeReviewPause
			if !errors.As(err, &paused) {
				t.Fatalf("new materials did not produce an editable extraction: %v", err)
			}
			if extraction.calls != 1 || contractCalls != 1 || paused.review.ReviewID == review.ReviewID || paused.review.SourceText != source || !strings.Contains(paused.review.Contract.Instructions, request["rules"].(string)) {
				t.Fatalf("new extraction did not use current materials and instructions: %+v", paused.review)
			}
			previous, err := store.Get("session", review.ReviewID)
			if err != nil || previous.Status != knowledge.ReviewCompileFailed || previous.SourceText != original {
				t.Fatalf("new extraction overwrote the previous confirmed draft: %+v %v", previous, err)
			}
		})
	}
}

func (m *knowledgeContinuationModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func (m *knowledgeContinuationModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.calls++
	lastTask := ""
	for _, message := range messages {
		if message.Role == schema.Tool && message.ToolName == "task" {
			lastTask = message.Content
		}
	}
	var result struct {
		Success bool           `json:"success"`
		Error   string         `json:"error"`
		Data    map[string]any `json:"data"`
		Review  struct {
			ID       string `json:"review_id"`
			Revision int64  `json:"revision"`
			Status   string `json:"status"`
		} `json:"knowledge_review"`
	}
	if m.calls > 1 {
		if err := json.Unmarshal([]byte(lastTask), &result); err != nil {
			return nil, fmt.Errorf("delegation did not return a usable result: %s: %w", lastTask, err)
		}
	}
	request := map[string]any{"source_ref": "current_user_message"}
	subagent := "knowledge_engineer"
	switch m.calls {
	case 1:
		request["route_style"], request["symbols"] = "过期样式", "军标"
	case 2:
		if result.Success || !strings.Contains(result.Error, "过期样式") || result.Review.Status != knowledge.ReviewCompileFailed || result.Review.ID != m.reviewID || result.Review.Revision != m.reviewRevision {
			return nil, fmt.Errorf("confirmed compilation failure did not reach the main agent: %+v", result)
		}
		request = map[string]any{"review_id": result.Review.ID, "revision": result.Review.Revision}
		if m.withSource {
			request["source_ref"] = "current_user_message"
			request["rules"] = "修订后的步骤说明，继续生成已经确认的路线数据。"
		}
	case 3:
		if !result.Success || result.Data["dataset_dir"] == nil {
			return nil, fmt.Errorf("corrected delegation did not return the dataset: %+v", result)
		}
		m.completedResult = lastTask
		subagent = "cartographer"
		request = map[string]any{"dataset_dir": result.Data["dataset_dir"]}
	case 4:
		if !result.Success || result.Data["map"] != "finished-map" {
			return nil, fmt.Errorf("drawing specialist did not finish: %+v", result)
		}
		return schema.AssistantMessage("已使用核对后的路线数据集完成制图。", nil), nil
	default:
		return nil, fmt.Errorf("unexpected extra orchestration request %d", m.calls)
	}
	description, _ := json.Marshal(request)
	arguments, _ := json.Marshal(map[string]string{"subagent_type": subagent, "description": string(description)})
	return schema.AssistantMessage("", []schema.ToolCall{{ID: fmt.Sprintf("knowledge-task-%d", m.calls), Type: "function", Function: schema.FunctionCall{Name: "task", Arguments: string(arguments)}}}), nil
}

func (m *knowledgeContinuationModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

type continuationDrawingAgent struct {
	dataset string
	calls   int
}

func (*continuationDrawingAgent) Name(context.Context) string { return "cartographer" }
func (*continuationDrawingAgent) Description(context.Context) string {
	return "使用路线数据集制图"
}
func (a *continuationDrawingAgent) Run(_ context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	a.calls++
	var request map[string]string
	err := json.Unmarshal([]byte(input.Messages[len(input.Messages)-1].Content), &request)
	if err == nil && request["dataset_dir"] != a.dataset {
		err = fmt.Errorf("drawing specialist did not receive the confirmed dataset: %v", request)
	}
	if err != nil {
		gen.Send(&adk.AgentEvent{AgentName: "cartographer", Err: err})
	} else {
		gen.Send(namedEvent("cartographer", adk.EventFromMessage(schema.AssistantMessage(`{"success":true,"data":{"map":"finished-map"}}`, nil), nil, schema.Assistant, "")))
	}
	gen.Close()
	return iter
}

func TestKnowledgeDelegationUsesDefaultsToContinueConfirmedReview(t *testing.T) {
	for _, withSource := range []bool{false, true} {
		t.Run(fmt.Sprintf("source_reference_%t", withSource), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("AUTOCARTO_HOME", root)
			sessionDir := filepath.Join(root, "session")
			if err := os.MkdirAll(sessionDir, 0700); err != nil {
				t.Fatal(err)
			}
			source := "红军从甲地行军到乙地。"
			sum := sha256.Sum256([]byte(source))
			contract := knowledge.Contract{ContractID: "continuation", SourceSHA256: hex.EncodeToString(sum[:]), JSONSchema: json.RawMessage(`{}`), Instructions: "从原文提取路线事实。", ReviewProtocolVersion: 1, RuntimeSessionDir: sessionDir}
			contractCalls, compiles := 0, 0
			contractTool := utils.NewEnhancedTool(&schema.ToolInfo{Name: knowledgeContractTool}, func(_ context.Context, args map[string]any) (*schema.ToolResult, error) {
				contractCalls++
				if args["source_text"] != source {
					return nil, fmt.Errorf("original source changed")
				}
				raw, _ := json.Marshal(map[string]any{"success": true, "data": contract})
				return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: string(raw)}}}, nil
			})
			const editedTitle = "用户核对后的路线"
			compiler := utils.NewEnhancedTool(&schema.ToolInfo{Name: knowledgeCompileTool}, func(_ context.Context, args map[string]any) (*schema.ToolResult, error) {
				compiles++
				raw, _ := json.Marshal(args["knowledge_json"])
				var graph knowledge.Graph
				if err := json.Unmarshal(raw, &graph); err != nil {
					return nil, err
				}
				if graph["task"].(map[string]any)["title"] != editedTitle || args["source_text"] != source || args["review_context"] == nil {
					return nil, fmt.Errorf("compiler lost the confirmed user edit, source or signed review")
				}
				if compiles == 1 {
					if args["route_style"] != "过期样式" || args["symbols"] != "军标" {
						return nil, fmt.Errorf("initial preset was not preserved")
					}
					return nil, fmt.Errorf("route_style preset not found: 过期样式")
				}
				if compiles != 2 || args["route_style"] != nil || args["symbols"] != nil {
					return nil, fmt.Errorf("compiler retained old selections instead of using built-in drawing")
				}
				dataset := filepath.Join(root, "dataset")
				if err := os.MkdirAll(dataset, 0700); err != nil {
					return nil, err
				}
				raw, _ = json.Marshal(map[string]any{"success": true, "data": map[string]any{"dataset_dir": dataset, "artifact_id": "confirmed-route-dataset"}})
				return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: string(raw)}}}, nil
			})
			extraction := &extractionOnceModel{t: t}
			specialist := &knowledgeSubAgent{name: "knowledge_engineer", description: "抽取并核对路线知识", chatModel: extraction,
				modelIdentity: &knowledgeModelIdentity{Provider: "test", Model: "continuation"}, config: knowledge.DefaultConfig(),
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
			mainModel := &knowledgeContinuationModel{withSource: withSource}
			drawing := &continuationDrawingAgent{dataset: filepath.Join(root, "dataset")}
			agent, err := deep.New(ctx, &deep.Config{Name: "orchestrator", Description: "编排", Instruction: "完成用户任务。", ChatModel: mainModel,
				SubAgents: []adk.Agent{specialist, drawing}, WithoutGeneralSubAgent: true, MaxIteration: 6, ToolsConfig: adk.ToolsConfig{EmitInternalEvents: true}})
			if err != nil {
				t.Fatal(err)
			}
			runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, CheckPointStore: runtimepersist.NewMemoryCheckpointStore(nil)})
			iter := runner.Run(ctx, []*schema.Message{schema.UserMessage("根据史料制作行军路线图")}, adk.WithCheckPointID("review-continuation"))
			interruptID, reviewID := "", ""
			interrupts := 0
			for event, ok := iter.Next(); ok; event, ok = iter.Next() {
				if event.Err != nil {
					t.Fatal(event.Err)
				}
				if event.Action != nil && event.Action.Interrupted != nil {
					interrupts++
					for _, item := range event.Action.Interrupted.InterruptContexts {
						if info, ok := item.Info.(map[string]any); ok && info["type"] == "knowledge_review" {
							interruptID, reviewID = item.ID, info["review_id"].(string)
						}
					}
				}
			}
			if interrupts != 1 || interruptID == "" || mainModel.calls != 1 || extraction.calls != 1 || contractCalls != 1 || compiles != 0 {
				t.Fatalf("expected one extraction then review pause: interrupts=%d main=%d extraction=%d contract=%d compile=%d", interrupts, mainModel.calls, extraction.calls, contractCalls, compiles)
			}
			review, err := store.Get("session", reviewID)
			if err != nil {
				t.Fatal(err)
			}
			review.Graph["task"].(map[string]any)["title"] = editedTitle
			review, err = store.SaveDraft("session", reviewID, review.Revision, review.Graph)
			if err != nil {
				t.Fatal(err)
			}
			claimed, _, err := store.Claim("session", reviewID, review.Revision, "start-mapping")
			if err != nil {
				t.Fatal(err)
			}
			mainModel.reviewID, mainModel.reviewRevision = reviewID, claimed.Revision
			resumed, err := runner.ResumeWithParams(ctx, "review-continuation", &adk.ResumeParams{Targets: map[string]any{interruptID: map[string]any{"review_id": reviewID, "revision": claimed.Revision}}})
			if err != nil {
				t.Fatal(err)
			}
			statuses := map[string]bool{}
			for event, ok := resumed.Next(); ok; event, ok = resumed.Next() {
				if event.Err != nil {
					t.Fatal(event.Err)
				}
				if event.Action != nil && event.Action.Interrupted != nil {
					t.Fatal("a correctable compiler failure asked the user to confirm again")
				}
				if event.Output != nil && event.Output.MessageOutput != nil {
					message, err := event.Output.MessageOutput.GetMessage()
					if err != nil {
						t.Fatal(err)
					}
					if update, ok := message.Extra["knowledge_review_update"].(map[string]any); ok {
						statuses[fmt.Sprint(update["status"])] = true
					}
				}
			}
			finished, err := store.Get("session", reviewID)
			if err != nil {
				t.Fatal(err)
			}
			if mainModel.calls != 4 || mainModel.completedResult == "" || compiles != 2 || extraction.calls != 1 || contractCalls != 1 || drawing.calls != 1 {
				t.Fatalf("continuation restarted extraction or failed to delegate drawing: main=%d extraction=%d contract=%d compiles=%d drawing=%d", mainModel.calls, extraction.calls, contractCalls, compiles, drawing.calls)
			}
			if finished.Status != knowledge.ReviewCompiled || finished.Revision != claimed.Revision || finished.ClaimedAt != claimed.ClaimedAt || finished.Graph["task"].(map[string]any)["title"] != editedTitle || finished.ApprovedGraph["task"].(map[string]any)["title"] != editedTitle {
				t.Fatalf("continuation replaced the confirmed revision or edited graph: %+v", finished)
			}
			if !statuses[knowledge.ReviewCompileFailed] || !statuses[knowledge.ReviewCompiled] {
				t.Fatalf("review state transitions did not reach agent events: %v", statuses)
			}
		})
	}
}
