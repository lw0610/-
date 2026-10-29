package deepagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

type extractionOnceModel struct {
	t     *testing.T
	calls int
}

func (m *extractionOnceModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.calls++
	if m.calls != 1 || len(messages) != 2 || !strings.Contains(messages[1].Content, "source_evidence") {
		m.t.Fatalf("unexpected additional model request: %d", m.calls)
	}
	return schema.AssistantMessage(`{"graph":{"schema_version":"1.0","task":{"task_id":"test","title":"行军路线","input_crs":"EPSG:4326"},"actors":[{"actor_id":"army","name":"红军","evidence_refs":["c0001:s001"]}],"spatial_entities":[{"entity_id":"a","name":"甲地","entity_kind":"place","evidence_refs":["c0001:s001"]},{"entity_id":"b","name":"乙地","entity_kind":"place","evidence_refs":["c0001:s001"]}],"events":[],"routes":[{"route_id":"route","name":"行军","side":"friendly","evidence_refs":["c0001:s001"],"paths":[{"path_id":"path","role":"main","actor_refs":["army"],"evidence_refs":["c0001:s001"],"nodes":[{"ref_type":"spatial_entity","ref_id":"a"},{"ref_type":"spatial_entity","ref_id":"b"}]}]}],"callouts":[],"point_presentations":[]},"unresolved":[]}`, nil), nil
}

func (m *extractionOnceModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.t.Fatal("unexpected streaming request")
	return nil, nil
}

func TestExtractionOnlyReachesEditableResultCompilesAndCachesWithoutReviewModel(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", root)
	if err := os.MkdirAll(filepath.Join(root, "data", "session"), 0700); err != nil {
		t.Fatal(err)
	}
	source := "红军从甲地行军到乙地。"
	sum := sha256.Sum256([]byte(source))
	contract := knowledge.Contract{ContractID: "extraction-only", SourceSHA256: hex.EncodeToString(sum[:]), JSONSchema: json.RawMessage(`{}`), Instructions: "从原文提取事实。", ReviewProtocolVersion: 1, RuntimeSessionDir: filepath.Join(root, "data", "session")}
	resultTool := func(name string, handler func(map[string]any) map[string]any) tool.BaseTool {
		return utils.NewEnhancedTool(&schema.ToolInfo{Name: name}, func(_ context.Context, args map[string]any) (*schema.ToolResult, error) {
			raw, _ := json.Marshal(map[string]any{"success": true, "data": handler(args)})
			return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: string(raw)}}}, nil
		})
	}
	contractTool := resultTool(knowledgeContractTool, func(args map[string]any) map[string]any {
		if args["source_text"] != source {
			t.Fatal("source changed")
		}
		raw, _ := json.Marshal(contract)
		var data map[string]any
		_ = json.Unmarshal(raw, &data)
		return data
	})
	compiles := 0
	compiler := resultTool(knowledgeCompileTool, func(args map[string]any) map[string]any {
		compiles++
		if args["review_context"] == nil || args["knowledge_json"] == nil {
			t.Fatal("missing accepted graph or confirmation context")
		}
		return map[string]any{"dataset_dir": filepath.Join(root, "dataset")}
	})
	cm := &extractionOnceModel{t: t}
	coordinateCalls := 0
	agent := &knowledgeSubAgent{
		chatModel: cm, modelIdentity: &knowledgeModelIdentity{Provider: "test", Model: "extract-once"}, config: knowledge.DefaultConfig(),
		tools: map[string]tool.BaseTool{knowledgeContractTool: contractTool, knowledgeCompileTool: compiler},
		resolveCoordinates: func(_ context.Context, graph map[string]any, text string) error {
			coordinateCalls++
			if text != source || cm.calls != 1 {
				t.Fatal("coordinates did not follow the first extraction")
			}
			for i, raw := range graph["spatial_entities"].([]any) {
				p := raw.(map[string]any)
				p["lon"], p["lat"], p["coordinate_source"] = 110.0+float64(i), 33.0, "gazetteer"
			}
			return nil
		},
	}
	store := knowledge.NewReviewStore(filepath.Join(root, "reviews"))
	ctx := WithKnowledgeReview(toolsession.WithContext(context.Background(), "session", filepath.Join(root, "project.qgz"), ""), store)
	request, _ := json.Marshal(map[string]any{"source_text": source, "route_style": "红蓝对比", "symbols": "军标"})
	_, err := agent.extractAndCompile(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage(string(request))}}, func(p knowledge.Progress) {
		if p.Stage == "semantic_review" {
			t.Fatal("semantic review progress emitted")
		}
	})
	var paused *knowledgeReviewPause
	if !errors.As(err, &paused) {
		t.Fatalf("no editable result: %v", err)
	}
	r := paused.review
	if cm.calls != 1 || coordinateCalls != 1 || compiles != 0 || r.Status != knowledge.ReviewAwaiting {
		t.Fatalf("unexpected workflow: model=%d coordinates=%d compile=%d status=%s", cm.calls, coordinateCalls, compiles, r.Status)
	}
	if err := knowledge.ValidateExtractionCompletion(r.OriginalResult); err != nil {
		t.Fatal(err)
	}
	if !r.OriginalResult.Completion.SemanticSkipped || r.OriginalResult.Completion.SemanticComplete || len(r.OriginalResult.SemanticReviews) != 0 {
		t.Fatal("skipped review falsely marked as completed")
	}
	claimed, _, err := store.Claim(r.SessionID, r.ReviewID, r.Revision, "start-map")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = agent.compileReviewedKnowledge(ctx, store, claimed); err != nil {
		t.Fatal(err)
	}
	if compiles != 1 || cm.calls != 1 {
		t.Fatal("confirmation reran extraction/review")
	}
	cache, _ := newKnowledgeCache(agent.modelIdentity, agent.config, contract)
	if cache == nil {
		t.Fatal("cache unavailable")
	}
	saved, _, err := cache.load()
	if err != nil {
		t.Fatalf("extraction-only result was not cached: %v", err)
	}
	if !saved.Completion.SemanticSkipped || saved.Status != "completed" {
		t.Fatal("incorrect cached completion")
	}
}
