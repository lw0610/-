package deepagent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/autocarto/carto_agent/internal/knowledge"
)

func TestCoordinatesEnrichDisplayedAndSemanticGraph(t *testing.T) {
	graph := knowledge.Graph{"spatial_entities": []any{map[string]any{"entity_id": "p", "name": "古地名"}}}
	result := &knowledge.Result{Graph: graph, SemanticGraph: cloneKnowledgeGraph(graph)}
	agent := &knowledgeSubAgent{resolveCoordinates: func(_ context.Context, g map[string]any, source string) error {
		if source != "原文" {
			t.Fatal("missing source context")
		}
		p := g["spatial_entities"].([]any)[0].(map[string]any)
		p["lon"], p["lat"], p["coordinate_source"] = 110.2, 33.4, "gazetteer"
		p["geocoding"] = map[string]any{"status": "resolved", "modern_name": "现代检索名"}
		return nil
	}}
	if err := agent.enrichCoordinates(context.Background(), result, "原文", nil); err != nil {
		t.Fatal(err)
	}
	for _, g := range []knowledge.Graph{result.Graph, result.SemanticGraph} {
		p := g["spatial_entities"].([]any)[0].(map[string]any)
		if p["lon"] != 110.2 || p["lat"] != 33.4 || p["name"] != "古地名" || p["geocoding"] == nil {
			t.Fatalf("missing merged coordinate: %#v", p)
		}
	}
}

func TestUnavailableGeocoderReportsFailureWithoutPublishingPartialCoordinates(t *testing.T) {
	manual := map[string]any{"entity_id": "manual", "lon": 110.0, "lat": 33.0, "coordinate_source": "manual"}
	estimate := map[string]any{"entity_id": "guess", "lon": 110.0, "lat": 33.0, "coordinate_source": "llm_estimated", "coordinate_confidence": 0.8}
	result := &knowledge.Result{Graph: knowledge.Graph{"spatial_entities": []any{manual, estimate}}}
	before := cloneKnowledgeGraph(result.Graph)
	agent := &knowledgeSubAgent{resolveCoordinates: func(_ context.Context, graph map[string]any, _ string) error {
		graph["spatial_entities"].([]any)[0].(map[string]any)["lon"] = 99.0
		return errors.New("服务不可用")
	}}
	if err := agent.enrichCoordinates(context.Background(), result, "原文", nil); err == nil {
		t.Fatal("model outage incorrectly treated as successful coordinate completion")
	}
	if manual["lon"] != 110.0 || manual["geocoding"] != nil {
		t.Fatal("manual coordinate overwritten")
	}
	if !reflect.DeepEqual(before, result.Graph) {
		t.Fatal("failed lookup changed the original graph")
	}
}

func TestSuccessfulResolverCannotLeaveCandidateOnlyEntities(t *testing.T) {
	result := &knowledge.Result{Graph: knowledge.Graph{"spatial_entities": []any{map[string]any{"entity_id": "p", "name": "甲地"}}}}
	agent := &knowledgeSubAgent{resolveCoordinates: func(_ context.Context, graph map[string]any, _ string) error {
		graph["spatial_entities"].([]any)[0].(map[string]any)["geocoding"] = map[string]any{"status": "needs_review"}
		return nil
	}}
	if err := agent.enrichCoordinates(context.Background(), result, "原文", nil); err == nil {
		t.Fatal("candidate-only entity was published")
	}
}

func TestEstimatedCoordinatesAreDeliveredWithoutAnotherConfirmation(t *testing.T) {
	graph := knowledge.Graph{"spatial_entities": []any{map[string]any{"entity_id": "p", "name": "某地以北", "entity_kind": "region"}}}
	result := &knowledge.Result{Graph: graph, SemanticGraph: cloneKnowledgeGraph(graph)}
	agent := &knowledgeSubAgent{resolveCoordinates: func(_ context.Context, g map[string]any, _ string) error {
		p := g["spatial_entities"].([]any)[0].(map[string]any)
		p["lon"], p["lat"], p["coordinate_source"], p["coordinate_confidence"] = 109.4, 33.8, "llm_estimated", 0.4
		p["geocoding"] = map[string]any{"status": "estimated", "selection_method": "model_estimate"}
		return nil
	}}
	if err := agent.enrichCoordinates(context.Background(), result, "原文", nil); err != nil {
		t.Fatal(err)
	}
	if err := knowledge.ValidateResolvedCoordinates(result.Graph); err != nil {
		t.Fatal(err)
	}
	if result.Graph["spatial_entities"].([]any)[0].(map[string]any)["coordinate_source"] != "llm_estimated" {
		t.Fatal("estimate relabeled as a verified coordinate")
	}
}
