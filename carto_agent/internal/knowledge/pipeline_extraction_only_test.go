package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type extractionOnlyModel struct {
	inputs []map[string]any
	change func(Graph, string) []modelIssue
}

func (m *extractionOnlyModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	var input map[string]any
	if err := json.Unmarshal([]byte(messages[1].Content), &input); err != nil {
		return nil, err
	}
	m.inputs = append(m.inputs, input)
	if input["graph"] != nil || input["chunk_id"] == nil {
		return nil, errors.New("unexpected model request after fragment extraction")
	}
	ref := array(input["source_evidence"])[0].(map[string]any)["ref"].(string)
	graph := extractionOnlyGraph(ref)
	issues := []modelIssue{}
	if m.change != nil {
		issues = m.change(graph, ref)
	}
	raw, err := json.Marshal(map[string]any{"graph": graph, "unresolved": issues})
	return schema.AssistantMessage(string(raw), nil), err
}

func (*extractionOnlyModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unexpected stream request")
}

func extractionOnlyGraph(sourceRef string) Graph {
	refs := []any{sourceRef}
	return Graph{
		"schema_version": "1.0", "task": map[string]any{"input_crs": "EPSG:4326"},
		"actors": []any{map[string]any{"actor_id": "army", "name": "部队", "evidence_refs": refs}},
		"spatial_entities": []any{
			map[string]any{"entity_id": "a", "name": "甲地", "map_role": "route_node", "evidence_refs": refs},
			map[string]any{"entity_id": "b", "name": "乙地", "map_role": "route_node", "evidence_refs": refs},
		},
		"routes": []any{map[string]any{
			"route_id": "route", "name": "行军路线", "side": "friendly", "evidence_refs": refs,
			"paths": []any{map[string]any{
				"path_id": "main", "role": "main", "actor_refs": []any{"army"}, "evidence_refs": refs,
				"nodes": []any{
					map[string]any{"ref_type": "spatial_entity", "ref_id": "a"},
					map[string]any{"ref_type": "spatial_entity", "ref_id": "b"},
					map[string]any{"ref_type": "spatial_entity", "ref_id": "a"},
				},
			}},
		}},
	}
}

func extractionOnlyContract(source string) Contract {
	return Contract{ContractID: "test", Instructions: "Extract the supplied source.", JSONSchema: json.RawMessage(`{}`), SourceSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}
}

func TestPipelineRunsOnlyExtractionModelCalls(t *testing.T) {
	for _, count := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d_chunks", count), func(t *testing.T) {
			sentence := "部队由甲地到乙地，再返回甲地。"
			source := strings.Repeat(sentence, count)
			cm := &extractionOnlyModel{}
			statuses := []string{}
			cfg := Config{TargetChars: len([]rune(sentence)), MaxChars: len([]rune(sentence)), ReviewAllRoutes: true,
				Checkpoint: func(r *Result) error { statuses = append(statuses, r.Status); return nil }}
			r, err := NewPipeline(cm, cfg).Run(context.Background(), source, extractionOnlyContract(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(cm.inputs) != count || len(r.Attempts) != count || len(r.Chunks) != count || r.Status != "ready_to_compile" {
				t.Fatalf("unexpected additional pass or incomplete result: calls=%d attempts=%d chunks=%d status=%s", len(cm.inputs), len(r.Attempts), len(r.Chunks), r.Status)
			}
			for _, status := range statuses {
				if status == "semantic_review" {
					t.Fatal("persisted a semantic review that was never performed")
				}
			}
			if err := ValidateExtractionCompletion(r); err != nil {
				t.Fatal(err)
			}
			if r.Completion.AllRoutesRequired || r.Completion.SemanticAttempted || r.Completion.SemanticComplete || !r.Completion.SemanticSkipped || len(r.SemanticReviews) != 0 {
				t.Fatalf("misleading completion history: %#v", r.Completion)
			}
			if len(array(r.Graph["spatial_entities"])) != 2*count || len(array(r.Graph["routes"])) != count {
				t.Fatal("same-name entities or independent fragment routes were merged without evidence")
			}
			for i, raw := range array(r.Graph["routes"]) {
				prefix := fmt.Sprintf("c%04d__", i+1)
				route := raw.(map[string]any)
				path := array(route["paths"])[0].(map[string]any)
				if route["route_id"] != prefix+"route" || !reflect.DeepEqual(drawingPlaces(r.Graph, path), []string{prefix + "a", prefix + "b", prefix + "a"}) {
					t.Fatalf("fragment or repeated-visit order changed: %#v", route)
				}
			}
			if err := checkReviewReferences(r.Graph); err != nil {
				t.Fatalf("local reference preparation was lost: %v", err)
			}
		})
	}
}

func TestExtractionOnlyDraftKeepsUnresolvedFactsEditable(t *testing.T) {
	source := "部队由甲地到乙地，再返回甲地。"
	cm := &extractionOnlyModel{change: func(g Graph, ref string) []modelIssue {
		return []modelIssue{{ID: "uncertain", Kind: "route", Reason: "context", Field: "nodes", SubjectRefs: []string{"main"}, Message: "原文次序仍有歧义", SourceRefs: []string{ref}}}
	}}
	r, err := NewPipeline(cm, DefaultConfig()).Run(context.Background(), source, extractionOnlyContract(source), nil)
	var draftErr *ExtractionNeedsReviewError
	if !errors.As(err, &draftErr) || len(cm.inputs) != 1 {
		t.Fatalf("expected editable extraction without a review request: %v, calls=%d", err, len(cm.inputs))
	}
	if err := ReviewableExtraction(r, source); err != nil {
		t.Fatalf("skipped model review blocked the results page: %v", err)
	}
	issues := ExtractionPendingIssues(r)
	if len(issues) != 1 || issues[0].Phase != "extraction" {
		t.Fatalf("extraction finding was misrepresented as a model review: %#v", issues)
	}
	if _, issues := PrepareReviewCompilation(&Review{Graph: r.Graph, OriginalResult: r}); len(issues) != 0 {
		t.Fatalf("unresolved extraction finding blocked accepted drawing: %#v", issues)
	}
}

func TestExtractionOnlyPartialDraftRemainsReviewable(t *testing.T) {
	source := "部队由甲地到乙地，再返回甲地。"
	cm := &extractionOnlyModel{change: func(g Graph, _ string) []modelIssue {
		path := array(array(g["routes"])[0].(map[string]any)["paths"])[0].(map[string]any)
		array(path["nodes"])[1].(map[string]any)["ref_id"] = "unknown"
		return nil
	}}
	r, err := NewPipeline(cm, DefaultConfig()).Run(context.Background(), source, extractionOnlyContract(source), nil)
	var draftErr *ExtractionNeedsReviewError
	if !errors.As(err, &draftErr) || !HasPartialExtraction(r) || len(cm.inputs) != 2 {
		t.Fatalf("format retry/recovery changed: %v, calls=%d", err, len(cm.inputs))
	}
	if err := ReviewableExtraction(r, source); err != nil {
		t.Fatalf("partial extraction hidden after removal of semantic pass: %v", err)
	}
	if !r.Completion.SemanticSkipped || r.Completion.SemanticAttempted {
		t.Fatalf("incorrect partial draft completion: %#v", r.Completion)
	}
}

func TestEmptyExtractionPreservesReasonWithoutReportingReadyToDraw(t *testing.T) {
	source := "读取史料文件内容"
	message := "原文未提供任何行军路线、地点或主体信息，无法构建知识图谱。"
	cm := &extractionOnlyModel{change: func(g Graph, ref string) []modelIssue {
		for _, collection := range graphCollections {
			if collection.key != "evidence" {
				g[collection.key] = []any{}
			}
		}
		return []modelIssue{{ID: "no_content", Kind: "route", Reason: "not_stated", Field: "paths", Message: message, SourceRefs: []string{ref}}}
	}}
	r, err := NewPipeline(cm, DefaultConfig()).Run(context.Background(), source, extractionOnlyContract(source), nil)
	var reviewErr *ExtractionNeedsReviewError
	if !errors.As(err, &reviewErr) || r.Status != "needs_review" || len(cm.inputs) != 1 || len(r.Issues) != 1 || r.Issues[0].Message != message {
		t.Fatalf("empty extraction was hidden or retried: result=%+v err=%v calls=%d", r, err, len(cm.inputs))
	}
	if err := ReviewableExtraction(r, source); err != nil {
		t.Fatalf("empty result and its explanation should remain inspectable: %v", err)
	}
	review := &Review{Graph: r.Graph, OriginalResult: r, PendingIssues: ExtractionPendingIssues(r)}
	if _, problems := PrepareReviewCompilation(review); len(problems) != 1 || problems[0].Code != "empty_extraction" || !strings.Contains(problems[0].Message, message) {
		t.Fatalf("empty result allowed into compiler: %#v", problems)
	}
	if problems := ValidateReviewPending(review, r.Graph); len(problems) != 1 || problems[0].Code != "empty_extraction" {
		t.Fatalf("empty result can be claimed for drawing: %#v", problems)
	}
	if view := review.View(); len(view.Errors) != 1 || !strings.Contains(view.Errors[0].Message, message) {
		t.Fatalf("source failure not exposed in review: %#v", view.Errors)
	}
}

func TestPartialFactsDoNotRequireEveryCollectionToBeNonempty(t *testing.T) {
	source := "红军当时驻扎在甲地。"
	cm := &extractionOnlyModel{change: func(g Graph, ref string) []modelIssue {
		g["routes"] = []any{}
		return []modelIssue{{ID: "no_route", Kind: "route", Reason: "not_stated", Field: "paths", Message: "原文没有交代移动路线。", SourceRefs: []string{ref}}}
	}}
	r, err := NewPipeline(cm, DefaultConfig()).Run(context.Background(), source, extractionOnlyContract(source), nil)
	if err != nil || !HasExtractedContent(r.Graph) || r.Status != "ready_to_compile" || len(cm.inputs) != 1 || len(r.Issues) != 1 {
		t.Fatalf("partial factual content was blocked: %+v %v", r, err)
	}
	if _, problems := PrepareReviewCompilation(&Review{Graph: r.Graph}); len(problems) != 0 {
		t.Fatalf("missing routes blocked otherwise valid saved content: %#v", problems)
	}
}

func TestExtractionCompletionRejectsAmbiguousOrIncompleteProtocol(t *testing.T) {
	complete := ExtractionCompletion{Version: ExtractionProtocolVersion, ExtractionComplete: true, AdaptationComplete: true, SemanticSkipped: true}
	for _, field := range []string{"extraction", "adaptation", "skipped", "attempted", "semantic_complete", "version"} {
		t.Run(field, func(t *testing.T) {
			c := complete
			switch field {
			case "extraction":
				c.ExtractionComplete = false
			case "adaptation":
				c.AdaptationComplete = false
			case "skipped":
				c.SemanticSkipped = false
			case "attempted":
				c.SemanticAttempted = true
			case "semantic_complete":
				c.SemanticComplete = true
			case "version":
				c.Version = 1
			}
			if err := ValidateExtractionCompletion(&Result{Completion: &c}); err == nil {
				t.Fatal("incomplete or conflicting protocol accepted")
			}
		})
	}
	if err := ValidateExtractionCompletion(&Result{Completion: &complete, SemanticReviews: []SemanticReview{{Applied: true}}}); err == nil {
		t.Fatal("v3 accepted a fabricated semantic review record")
	}
	legacy := &Result{Completion: &ExtractionCompletion{Version: 2, ExtractionComplete: true, AdaptationComplete: true, SemanticAttempted: true, SemanticComplete: true}, Chunks: []Fragment{{Chunk: Chunk{ID: "c1"}}}, SemanticReviews: []SemanticReview{{Applied: true, ChunkIDs: []string{"c1"}}}}
	if err := ValidateExtractionCompletion(legacy); err != nil {
		t.Fatalf("valid historical extraction no longer readable: %v", err)
	}
	legacy.SemanticReviews = nil
	if err := ValidateExtractionCompletion(legacy); err == nil {
		t.Fatal("legacy extraction without required historical coverage accepted")
	}
}
