package knowledge

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"
)

func acceptedPath(id, role, attach string, places ...string) map[string]any {
	nodes := []any{}
	for _, place := range places {
		nodes = append(nodes, map[string]any{"ref_type": "spatial_entity", "ref_id": place})
	}
	path := map[string]any{"path_id": id, "role": role, "nodes": nodes, "actor_refs": []any{"actor"}, "evidence_refs": []any{"evidence"}}
	if attach != "" {
		path["attach_ref"] = attach
	}
	return path
}

func acceptedGraph(routes ...any) Graph {
	places := []any{}
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		places = append(places, map[string]any{"entity_id": id, "name": id, "lon": 110.0, "lat": 33.0, "map_role": "route_node"})
	}
	return Graph{"actors": []any{map[string]any{"actor_id": "actor"}}, "evidence": []any{map[string]any{"evidence_id": "evidence", "quote": "原文"}}, "spatial_entities": places, "routes": routes}
}

func acceptedRoute(id string, paths ...any) map[string]any {
	return map[string]any{"route_id": id, "name": id, "side": "enemy", "paths": paths, "evidence_refs": []any{"evidence"}}
}

func drawingPlaces(g Graph, path map[string]any) []string {
	result := []string{}
	for _, node := range array(path["nodes"]) {
		result = append(result, resolvedNode(g, node))
	}
	return result
}

func TestAcceptedMultiMainRoutesKeepEveryIndependentPath(t *testing.T) {
	routes := []any{}
	wantPaths := map[string][]string{}
	for r, count := range []int{2, 3, 2, 2} {
		paths := []any{}
		for p := 0; p < count; p++ {
			id := fmt.Sprintf("path_%d_%d", r, p)
			path := acceptedPath(id, "main", "", "a", "b")
			if p%2 == 1 {
				path = acceptedPath(id, "main", "", "c", "d")
			}
			paths = append(paths, path)
			wantPaths[id] = drawingPlaces(nil, path)
		}
		routes = append(routes, acceptedRoute(fmt.Sprintf("route_%d", r), paths...))
	}
	source := acceptedGraph(routes...)
	before, _ := cloneGraph(source)
	prepared, issues := PrepareReviewCompilation(&Review{Graph: source})
	if len(issues) > 0 || len(array(prepared["routes"])) != 9 {
		t.Fatalf("multiple mains rejected or lost: %v %#v", issues, prepared)
	}
	seen := map[string]bool{}
	for _, raw := range array(prepared["routes"]) {
		route := raw.(map[string]any)
		id := stringValue(route["route_id"])
		if seen[id] || route["topology"] != "standard" || len(array(route["paths"])) != 1 {
			t.Fatalf("invalid standalone route: %#v", route)
		}
		seen[id] = true
		path := array(route["paths"])[0].(map[string]any)
		if !reflect.DeepEqual(drawingPlaces(prepared, path), wantPaths[stringValue(path["path_id"])]) || !reflect.DeepEqual(path["actor_refs"], []any{"actor"}) || !reflect.DeepEqual(path["evidence_refs"], []any{"evidence"}) {
			t.Fatalf("path movement or provenance changed: %#v", path)
		}
	}
	if !reflect.DeepEqual(source, before) {
		t.Fatal("accepted source graph changed")
	}
	second, _ := PrepareReviewCompilation(&Review{Graph: source})
	if !reflect.DeepEqual(prepared, second) {
		t.Fatal("render projection is nondeterministic")
	}
}

func TestAcceptedMixedRoutesUseExplicitAttachmentsAndPreserveNodeOrder(t *testing.T) {
	source := acceptedGraph(acceptedRoute("mixed", acceptedPath("main", "main", "", "a", "b", "c"), acceptedPath("branch", "branch", "b", "b", "d", "d", "e"), acceptedPath("incoming", "incoming", "b", "f", "b")))
	before, _ := cloneGraph(source)
	prepared, issues := PrepareReviewCompilation(&Review{Graph: source})
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	want := map[string][]string{"main": {"a", "b", "c"}, "branch": {"b", "d", "e"}, "incoming": {"f", "b"}}
	for _, raw := range array(prepared["routes"]) {
		route := raw.(map[string]any)
		path := array(route["paths"])[0].(map[string]any)
		if !reflect.DeepEqual(drawingPlaces(prepared, path), want[stringValue(path["path_id"])]) || path["attach_ref"] != nil {
			t.Fatalf("wrong junction or direction: %#v", path)
		}
	}
	if len(array(prepared["routes"])) != 3 || !reflect.DeepEqual(before, source) {
		t.Fatal("paths lost or source changed")
	}
}

func TestAcceptedNativeBranchAndMultiStartStayNative(t *testing.T) {
	for _, role := range []string{"branch", "incoming"} {
		t.Run(role, func(t *testing.T) {
			g := acceptedGraph(acceptedRoute("route", acceptedPath("main", "main", "", "a", "b", "c"), acceptedPath("side", role, "b", "d")))
			prepared, issues := PrepareReviewCompilation(&Review{Graph: g})
			if len(issues) > 0 || len(array(prepared["routes"])) != 1 {
				t.Fatalf("supported topology flattened: %v", issues)
			}
			route := array(prepared["routes"])[0].(map[string]any)
			wantTopology := map[string]string{"branch": "branch", "incoming": "multi_start"}[role]
			if route["route_id"] != "route" || route["topology"] != wantTopology || array(route["paths"])[1].(map[string]any)["role"] != role {
				t.Fatalf("native topology lost: %#v", route)
			}
		})
	}
}

func TestAcceptedSplitIDsAvoidExistingDeclarations(t *testing.T) {
	graph := acceptedGraph(acceptedRoute("r", acceptedPath("p1", "main", "", "a", "b"), acceptedPath("p2", "main", "", "c", "d")))
	reserved := "r__render_" + reviewHash([]byte("r\x00p1"))[:16]
	graph["actors"] = append(array(graph["actors"]), map[string]any{"actor_id": reserved})
	prepared, issues := PrepareReviewCompilation(&Review{Graph: graph})
	if len(issues) != 0 || array(prepared["routes"])[0].(map[string]any)["route_id"] != reserved+"_2" {
		t.Fatalf("generated route ID collided with existing metadata: %v %#v", issues, prepared)
	}
	if _, err := indexGraph(prepared); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptedGraphIgnoresSemanticOpinionsAndCompletionHistory(t *testing.T) {
	graph := acceptedGraph(acceptedRoute("r", acceptedPath("p", "main", "", "a", "b")))
	array(graph["spatial_entities"])[0].(map[string]any)["map_role"] = "background"
	for _, original := range []*Result{nil, {Graph: graph}, {Graph: graph, Completion: &ExtractionCompletion{Version: 1}}, {Graph: graph, Completion: &ExtractionCompletion{Version: 2, SemanticComplete: false}}} {
		review := &Review{Graph: graph, OriginalResult: original, ExtractionStatus: "needs_review", PendingIssues: []ReviewPendingIssue{{ID: "old", Phase: "graph_consistency", Message: "old blocker"}}}
		prepared, issues := PrepareReviewCompilation(review)
		if len(issues) != 0 || len(ValidateReviewPending(review, graph)) != 0 {
			t.Fatalf("accepted graph blocked by history: %v", issues)
		}
		if array(prepared["spatial_entities"])[0].(map[string]any)["map_role"] != "background" {
			t.Fatal("semantic contradiction silently rewritten")
		}
	}
}

func TestAcceptedGraphStillReportsUninterpretableReferencesAndJSON(t *testing.T) {
	for _, broken := range []any{map[string]any{"ref_type": "spatial_entity", "ref_id": "missing"}, map[string]any{"ref_type": "event", "ref_id": "a"}, "not a node"} {
		graph := acceptedGraph(acceptedRoute("r", acceptedPath("p", "main", "", "a", "b")))
		path := array(array(graph["routes"])[0].(map[string]any)["paths"])[0].(map[string]any)
		path["nodes"] = []any{broken}
		if _, issues := PrepareReviewCompilation(&Review{Graph: graph}); len(issues) == 0 {
			t.Fatalf("uninterpretable node accepted: %#v", broken)
		}
	}
	if _, issues := PrepareReviewCompilation(&Review{Graph: Graph{"invalid": math.NaN()}}); len(issues) == 0 {
		t.Fatal("non-JSON data accepted")
	}
}

func TestAcceptedMetadataAndSinglePointPathsDoNotBlockOtherRoutes(t *testing.T) {
	graph := acceptedGraph(acceptedRoute("drawable", acceptedPath("line", "main", "", "a", "b")), acceptedRoute("isolated", acceptedPath("point", "main", "", "c", "c")))
	array(graph["routes"])[0].(map[string]any)["evidence_refs"] = []any{"missing-evidence"}
	array(array(graph["routes"])[0].(map[string]any)["paths"])[0].(map[string]any)["actor_refs"] = []any{"missing-actor"}
	prepared, issues := PrepareReviewCompilation(&Review{Graph: graph})
	if len(issues) != 0 || len(array(prepared["routes"])) != 1 {
		t.Fatalf("non-geometric data blocked drawable route: %v", issues)
	}
	if len(array(prepared["spatial_entities"])) != 6 {
		t.Fatal("single point entity was removed")
	}
	adaptations := reviewRenderingAdaptations(graph, prepared)
	if len(adaptations) != 2 || adaptations[1].PathID != "point" || adaptations[1].Operation != "point_only" || adaptations[1].RenderedRouteID != "" {
		t.Fatalf("isolated movement audit missing: %#v", adaptations)
	}
}

func TestAcceptedProjectionSigningIsStableAndNotHumanCorrection(t *testing.T) {
	graph := acceptedGraph(acceptedRoute("r", acceptedPath("p1", "main", "", "a", "b"), acceptedPath("p2", "main", "", "c", "d")))
	store := NewReviewStore(t.TempDir())
	source := "原文"
	r, err := store.Create(ReviewCreateInput{SessionID: "accept-sign", SourceText: source, Contract: Contract{ContractID: "v1", SourceSHA256: reviewHash([]byte(source)), JSONSchema: json.RawMessage(`{}`)}, OriginalResult: &Result{Graph: graph}, Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.SignContext(r)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SignContext(r)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated signatures differ: %v", err)
	}
	var payload struct {
		GraphJSON       string                      `json:"graph_json"`
		SourceGraphJSON string                      `json:"source_graph_json"`
		ManualRecords   []ReviewManualRecord        `json:"manual_records"`
		Adaptations     []ReviewRenderingAdaptation `json:"rendering_adaptations"`
	}
	if err = json.Unmarshal([]byte(first["payload"].(string)), &payload); err != nil {
		t.Fatal(err)
	}
	var signedSource Graph
	_ = json.Unmarshal([]byte(payload.SourceGraphJSON), &signedSource)
	if !reflect.DeepEqual(signedSource, graph) || len(payload.ManualRecords) != 0 || len(payload.Adaptations) != 2 || payload.GraphJSON == payload.SourceGraphJSON {
		t.Fatalf("projection provenance incorrect: %#v", payload)
	}
	claimed, fresh, err := store.Claim(r.SessionID, r.ReviewID, r.Revision, "start")
	if err != nil || !fresh || claimed.Status != ReviewCompiling {
		t.Fatalf("acceptance failed: %v", err)
	}
	prepared, _ := PrepareReviewCompilation(claimed)
	if _, err = store.Complete(r.SessionID, r.ReviewID, r.Revision, prepared, map[string]any{"dataset_dir": t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.Get(r.SessionID, r.ReviewID)
	if !reflect.DeepEqual(stored.Graph, graph) || !reflect.DeepEqual(stored.OriginalResult.Graph, graph) || stored.SourceText != source {
		t.Fatal("publication overwrote original accepted data")
	}
}

func TestConfirmedReuseRetainsPointOnlySourceAndHumanHistory(t *testing.T) {
	graph := acceptedGraph(acceptedRoute("split", acceptedPath("p1", "main", "", "a", "b"), acceptedPath("p2", "main", "", "c", "d")), acceptedRoute("isolated", acceptedPath("point", "main", "", "e")))
	store := NewReviewStore(t.TempDir())
	source := "原文"
	contract := Contract{ContractID: "v1", SourceSHA256: reviewHash([]byte(source)), JSONSchema: json.RawMessage(`{}`)}
	original := &Result{Graph: graph}
	r, err := store.Create(ReviewCreateInput{SessionID: "source-history", SourceText: source, Contract: contract, OriginalResult: original, Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range array(r.Graph["routes"]) {
		raw.(map[string]any)["name"] = "人工修改"
	}
	r, err = store.SaveDraft(r.SessionID, r.ReviewID, r.Revision, r.Graph)
	if err != nil {
		t.Fatal(err)
	}
	prepared, issues := PrepareReviewCompilation(r)
	if len(issues) != 0 || len(array(prepared["routes"])) != 2 {
		t.Fatalf("unexpected first projection: %v", issues)
	}
	_, _, err = store.Claim(r.SessionID, r.ReviewID, r.Revision, "first-map")
	if err != nil {
		t.Fatal(err)
	}
	published, err := store.Complete(r.SessionID, r.ReviewID, r.Revision, prepared, map[string]any{"dataset_dir": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		next, err := store.Create(ReviewCreateInput{SessionID: fmt.Sprintf("reused-history-%d", run), SourceText: source, Contract: contract, OriginalResult: original, Graph: published.Graph, ReusedFrom: published})
		if err != nil {
			t.Fatalf("accepted source graph rejected on reuse: %v", err)
		}
		if !reflect.DeepEqual(next.Graph, r.Graph) || !reflect.DeepEqual(next.OriginalResult.Graph, original.Graph) || len(next.InheritedProvenance) != 1 {
			t.Fatal("reuse lost source paths or duplicated history")
		}
		manual := next.InheritedProvenance[0].ManualRecords
		if len(manual) != 2 || manual[0].ID != "isolated" || manual[1].ID != "split" || manual[0].Origin != "manual_edited" {
			t.Fatalf("source edits replaced by drawing IDs or point edit lost: %#v", manual)
		}
		token, err := store.SignContext(next)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			SourceGraphJSON string                      `json:"source_graph_json"`
			ManualRecords   []ReviewManualRecord        `json:"manual_records"`
			Adaptations     []ReviewRenderingAdaptation `json:"rendering_adaptations"`
		}
		if err = json.Unmarshal([]byte(token["payload"].(string)), &payload); err != nil {
			t.Fatal(err)
		}
		var signedSource Graph
		_ = json.Unmarshal([]byte(payload.SourceGraphJSON), &signedSource)
		if !reflect.DeepEqual(signedSource, r.Graph) || len(payload.ManualRecords) != 2 || len(payload.Adaptations) != 3 || payload.Adaptations[2].Operation != "point_only" {
			t.Fatal("reused signature lost isolated path, rendered provenance, or source graph")
		}
		nextPrepared, issues := PrepareReviewCompilation(next)
		if len(issues) > 0 || !reflect.DeepEqual(nextPrepared, prepared) {
			t.Fatal("repeated projection changed accepted movement")
		}
		_, _, err = store.Claim(next.SessionID, next.ReviewID, next.Revision, "map-again")
		if err != nil {
			t.Fatal(err)
		}
		published, err = store.Complete(next.SessionID, next.ReviewID, next.Revision, nextPrepared, map[string]any{"dataset_dir": t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.Create(ReviewCreateInput{SessionID: "projected-reuse", SourceText: source, Contract: contract, OriginalResult: original, Graph: published.ApprovedGraph, ReusedFrom: published}); err == nil {
		t.Fatal("lossy rendered graph accepted as source-preserving reuse")
	}
}
