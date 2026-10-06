package deepagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

func TestConfirmedExtractionReuseStartsFromAcceptedSourceGraph(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", root)
	source := "甲地出发到乙地，丙地单独标记。"
	sum := sha256.Sum256([]byte(source))
	contract := knowledge.Contract{ContractID: "source-reuse", SourceSHA256: hex.EncodeToString(sum[:]), JSONSchema: json.RawMessage(`{}`), Instructions: "extract", ReviewProtocolVersion: 1}
	place := func(id string) any {
		return map[string]any{"entity_id": id, "name": id, "lon": 110.0, "lat": 33.0, "coordinate_source": "manual"}
	}
	node := func(id string) any { return map[string]any{"ref_type": "spatial_entity", "ref_id": id} }
	graph := knowledge.Graph{"spatial_entities": []any{place("a"), place("b"), place("c")}, "routes": []any{
		map[string]any{"route_id": "line", "paths": []any{map[string]any{"path_id": "line-path", "role": "main", "nodes": []any{node("a"), node("b")}}}},
		map[string]any{"route_id": "point", "paths": []any{map[string]any{"path_id": "point-path", "role": "main", "nodes": []any{node("c")}}}},
	}}
	store := knowledge.NewReviewStore(filepath.Join(root, "reviews"))
	project := filepath.Join(root, "project.qgz")
	original := &knowledge.Result{Graph: graph}
	r, err := store.Create(knowledge.ReviewCreateInput{SessionID: "first", ProjectPath: project, SourceText: source, Contract: contract, OriginalResult: original, Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	prepared, issues := knowledge.PrepareReviewCompilation(r)
	if len(issues) > 0 || len(prepared["routes"].([]any)) != 1 {
		t.Fatalf("first projection: %v", issues)
	}
	if _, _, err = store.Claim(r.SessionID, r.ReviewID, r.Revision, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Complete(r.SessionID, r.ReviewID, r.Revision, prepared, map[string]any{"dataset_dir": t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	contractTool := utils.NewEnhancedTool(&schema.ToolInfo{Name: knowledgeContractTool}, func(_ context.Context, args map[string]any) (*schema.ToolResult, error) {
		if args["source_text"] != source {
			t.Fatal("reuse changed source")
		}
		raw, _ := json.Marshal(map[string]any{"success": true, "data": contract})
		return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: string(raw)}}}, nil
	})
	agent := &knowledgeSubAgent{tools: map[string]tool.BaseTool{knowledgeContractTool: contractTool}, resolveCoordinates: func(context.Context, map[string]any, string) error {
		t.Fatal("already complete accepted coordinates were re-estimated")
		return nil
	}}
	ctx := WithKnowledgeReview(toolsession.WithContext(context.Background(), "second", project, ""), store)
	_, err = agent.extractAndCompile(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage(source)}}, nil)
	var paused *knowledgeReviewPause
	if !errors.As(err, &paused) {
		t.Fatalf("confirmed reuse failed: %v", err)
	}
	if !reflect.DeepEqual(paused.review.Graph, graph) || paused.review.OriginalCacheInfo["status"] != "confirmed_hit" {
		t.Fatalf("reused drawing projection lost accepted source: %#v", paused.review.Graph)
	}
	projectedAgain, issues := knowledge.PrepareReviewCompilation(paused.review)
	if len(issues) > 0 || !reflect.DeepEqual(projectedAgain, prepared) {
		t.Fatalf("reprojection differs: %v", issues)
	}
}
