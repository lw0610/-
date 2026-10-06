package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func coordinateReview(t *testing.T) (*ReviewStore, *Review) {
	t.Helper()
	store := NewReviewStore(t.TempDir())
	source := "部队到达甲地。"
	graph := Graph{"spatial_entities": []any{map[string]any{"entity_id": "p1", "name": "甲地", "entity_kind": "place"}}, "routes": []any{map[string]any{"route_id": "r1", "name": "原路线"}}}
	review, err := store.Create(ReviewCreateInput{SessionID: "s1", SourceText: source, Contract: Contract{ContractID: "v1", SourceSHA256: reviewHash([]byte(source)), JSONSchema: json.RawMessage(`{}`)}, OriginalResult: &Result{Graph: graph}, Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	return store, review
}

func fillCoordinate(_ context.Context, graph map[string]any, _ string) error {
	place := graph["spatial_entities"].([]any)[0].(map[string]any)
	place["lon"], place["lat"] = 110.12, 33.45
	place["coordinate_source"] = "gazetteer"
	place["geocoding"] = map[string]any{"status": "resolved", "provider": "tianditu"}
	return nil
}

func TestCoordinateLookupPersistsIntoReviewGraph(t *testing.T) {
	s, before := coordinateReview(t)
	after, err := s.ResolveDraftCoordinates(context.Background(), before.SessionID, before.ReviewID, before.Revision, fillCoordinate)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.Get(before.SessionID, before.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	place := reloaded.View().Graph["spatial_entities"].([]any)[0].(map[string]any)
	if place["lon"] != 110.12 || place["lat"] != 33.45 || place["name"] != "甲地" || place["coordinate_source"] != "gazetteer" || after.Revision != before.Revision+1 {
		t.Fatalf("coordinate not written to displayed graph: %#v", place)
	}
	if !reflect.DeepEqual(before.Graph["routes"], reloaded.Graph["routes"]) {
		t.Fatal("route changed")
	}
	if _, exists := reloaded.OriginalResult.Graph["spatial_entities"].([]any)[0].(map[string]any)["lon"]; exists {
		t.Fatal("immutable extraction baseline changed")
	}
	if records := reviewManualRecords(reloaded, reloaded.Graph); len(records) != 0 {
		t.Fatalf("automatic lookup claimed human edits: %#v", records)
	}
	claimed, _, err := s.Claim(after.SessionID, after.ReviewID, after.Revision, "confirm")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(claimed.Graph, reloaded.Graph) {
		t.Fatal("confirmation did not use enriched graph")
	}
}

func TestManualEditAfterLookupStillHasManualProvenance(t *testing.T) {
	s, before := coordinateReview(t)
	auto, err := s.ResolveDraftCoordinates(context.Background(), before.SessionID, before.ReviewID, before.Revision, fillCoordinate)
	if err != nil {
		t.Fatal(err)
	}
	place := auto.Graph["spatial_entities"].([]any)[0].(map[string]any)
	place["name"] = "人工修正"
	manual, err := s.SaveDraft(auto.SessionID, auto.ReviewID, auto.Revision, auto.Graph)
	if err != nil {
		t.Fatal(err)
	}
	if records := reviewManualRecords(manual, manual.Graph); len(records) != 1 || records[0].Origin != "manual_edited" {
		t.Fatalf("human edit lost: %#v", records)
	}
}

func TestClientLookupMetadataCannotForgeAutomaticProvenance(t *testing.T) {
	s, before := coordinateReview(t)
	_ = fillCoordinate(context.Background(), before.Graph, before.SourceText)
	manual, err := s.SaveDraft(before.SessionID, before.ReviewID, before.Revision, before.Graph)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewManualRecords(manual, manual.Graph)) != 1 {
		t.Fatal("client geocoding metadata bypassed manual provenance")
	}
}

func TestCoordinateLookupDoesNotOverwriteConcurrentEdit(t *testing.T) {
	s, before := coordinateReview(t)
	_, err := s.ResolveDraftCoordinates(context.Background(), before.SessionID, before.ReviewID, before.Revision, func(ctx context.Context, graph map[string]any, source string) error {
		manual, _ := cloneGraph(before.Graph)
		manual["spatial_entities"].([]any)[0].(map[string]any)["name"] = "人工修正"
		if _, err := s.SaveDraft(before.SessionID, before.ReviewID, before.Revision, manual); err != nil {
			t.Fatal(err)
		}
		return fillCoordinate(ctx, graph, source)
	})
	if !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("expected conflict: %v", err)
	}
	latest, _ := s.Get(before.SessionID, before.ReviewID)
	place := latest.Graph["spatial_entities"].([]any)[0].(map[string]any)
	if place["name"] != "人工修正" || place["lon"] != nil {
		t.Fatalf("new edit overwritten: %#v", place)
	}
}

func TestCoordinateLookupFailureAndCancellationDoNotSave(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		s, before := coordinateReview(t)
		ctx, cancel := context.WithCancel(context.Background())
		_, err := s.ResolveDraftCoordinates(ctx, before.SessionID, before.ReviewID, before.Revision, func(ctx context.Context, graph map[string]any, source string) error {
			_ = fillCoordinate(ctx, graph, source)
			if canceled {
				cancel()
				return nil
			}
			return errors.New("lookup failed")
		})
		cancel()
		if err == nil {
			t.Fatal("expected failure")
		}
		latest, _ := s.Get(before.SessionID, before.ReviewID)
		if latest.Revision != before.Revision || !reflect.DeepEqual(latest.Graph, before.Graph) {
			t.Fatal("failed lookup was saved")
		}
	}
}

func TestCandidateOnlyCoordinateCompletionCannotSave(t *testing.T) {
	s, before := coordinateReview(t)
	_, err := s.ResolveDraftCoordinates(context.Background(), before.SessionID, before.ReviewID, before.Revision, func(_ context.Context, graph map[string]any, _ string) error {
		graph["spatial_entities"].([]any)[0].(map[string]any)["geocoding"] = map[string]any{"status": "ambiguous", "candidates": []any{}}
		return nil
	})
	if err == nil {
		t.Fatal("coordinate-only completion saved an empty candidate row")
	}
	latest, _ := s.Get(before.SessionID, before.ReviewID)
	if latest.Revision != before.Revision || !reflect.DeepEqual(latest.Graph, before.Graph) {
		t.Fatal("incomplete lookup changed saved graph")
	}
}

func TestResolvedCoordinateCompletenessIncludesRegionsAndRejectsInvalidNumbers(t *testing.T) {
	for _, coords := range [][]any{{nil, 33.0}, {110.0, nil}, {"110", "33"}, {181.0, 33.0}, {110.0, 91.0}, {math.NaN(), 33.0}, {110.0, math.Inf(1)}} {
		graph := Graph{"spatial_entities": []any{map[string]any{"entity_id": "region", "entity_kind": "region", "name": "终南山区", "lon": coords[0], "lat": coords[1]}}}
		if ValidateResolvedCoordinates(graph) == nil {
			t.Fatalf("invalid coordinate accepted: %v", coords)
		}
	}
	graph := Graph{"spatial_entities": []any{map[string]any{"entity_id": "region", "entity_kind": "region", "name": "终南山区", "lon": 109.0, "lat": 33.9, "coordinate_source": "llm_estimated", "coordinate_confidence": 0.3}}}
	if err := ValidateResolvedCoordinates(graph); err != nil {
		t.Fatal(err)
	}
}

func TestResolvedCoordinatesRequireAccurateSourceAndEstimateConfidence(t *testing.T) {
	for _, input := range []map[string]any{{}, {"coordinate_source": "unknown"}, {"coordinate_source": "llm_estimated"}, {"coordinate_source": "llm_estimated", "coordinate_confidence": 2.0}} {
		input["lon"], input["lat"] = 110.0, 33.0
		if ValidateResolvedCoordinates(Graph{"spatial_entities": []any{input}}) == nil {
			t.Fatalf("invalid coordinate provenance accepted: %v", input)
		}
	}
}

func TestCoordinateSaveCanceledWhileWaitingForLockDoesNotCommit(t *testing.T) {
	s, before := coordinateReview(t)
	graph, _ := cloneGraph(before.Graph)
	_ = fillCoordinate(context.Background(), graph, before.SourceText)
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := s.saveDraftContext(ctx, before.SessionID, before.ReviewID, before.Revision, graph, true)
		done <- err
	}()
	cancel()
	s.mu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation ignored: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("save did not return after cancellation")
	}
	latest, _ := s.Get(before.SessionID, before.ReviewID)
	if latest.Revision != before.Revision {
		t.Fatal("canceled save committed")
	}
}

func TestConfirmedGraphCompletionOnlyAddsMissingCoordinates(t *testing.T) {
	before := Graph{"spatial_entities": []any{map[string]any{"entity_id": "p", "name": "甲地"}}, "routes": []any{map[string]any{"route_id": "r", "name": "原路线"}}}
	after, _ := cloneGraph(before)
	_ = fillCoordinate(context.Background(), after, "")
	if !isCoordinateCompletion(before, after) {
		t.Fatal("missing coordinates could not be completed")
	}
	changedRoute, _ := cloneGraph(after)
	changedRoute["routes"].([]any)[0].(map[string]any)["name"] = "不同路线"
	if isCoordinateCompletion(before, changedRoute) {
		t.Fatal("route change accepted as coordinate completion")
	}
	changedName, _ := cloneGraph(after)
	changedName["spatial_entities"].([]any)[0].(map[string]any)["name"] = "不同地点"
	if isCoordinateCompletion(before, changedName) {
		t.Fatal("identity change accepted as coordinate completion")
	}
	changedPoint, _ := cloneGraph(after)
	changedPoint["spatial_entities"].([]any)[0].(map[string]any)["lon"] = 99.0
	if isCoordinateCompletion(after, changedPoint) {
		t.Fatal("approved point overwritten as coordinate completion")
	}
}

func TestReusedConfirmedGraphCanFillBackgroundWithoutLosingHumanHistory(t *testing.T) {
	s, before := coordinateReview(t)
	manualGraph, _ := cloneGraph(before.Graph)
	manualGraph["spatial_entities"].([]any)[0].(map[string]any)["name"] = "人工修正的背景地点"
	manual, err := s.SaveDraft(before.SessionID, before.ReviewID, before.Revision, manualGraph)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Claim(manual.SessionID, manual.ReviewID, manual.Revision, "legacy-confirmed")
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.Complete(manual.SessionID, manual.ReviewID, manual.Revision, manual.Graph, map[string]any{"dataset_dir": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	completed, _ := cloneGraph(published.ApprovedGraph)
	_ = fillCoordinate(context.Background(), completed, before.SourceText)
	newDraft, err := s.Create(ReviewCreateInput{SessionID: "new-session", SourceText: before.SourceText, Contract: before.Contract, OriginalResult: before.OriginalResult, Graph: completed, ReusedFrom: published})
	if err != nil {
		t.Fatal(err)
	}
	if len(newDraft.InheritedProvenance) != 1 || len(newDraft.InheritedProvenance[0].ManualRecords) != 1 {
		t.Fatal("approved human changes were lost during coordinate completion")
	}
	stored, _ := s.Get(before.SessionID, before.ReviewID)
	if stored.ApprovedGraph["spatial_entities"].([]any)[0].(map[string]any)["lon"] != nil {
		t.Fatal("old approved graph mutated")
	}
	if newDraft.Graph["spatial_entities"].([]any)[0].(map[string]any)["lon"] != 110.12 {
		t.Fatal("new graph has no coordinate")
	}
}
