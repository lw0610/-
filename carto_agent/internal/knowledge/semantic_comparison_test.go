package knowledge

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// These fixtures reduce the observed c0002__a1 -> c0001__actor_1 merge,
// absent event.actor_refs -> [], and new r10/r10_p01 explanation failures.
func semanticComparisonFixture(t *testing.T) (Graph, []EvidenceSource) {
	t.Helper()
	sources := sourceSentences(Chunk{ID: "c1", Text: "红二十五军到达甲地。该军随后到达乙地。红军追至乙地。"})
	g := Graph{
		"schema_version": "1.0", "task": map[string]any{"input_crs": "EPSG:4326"},
		"actors": []any{
			map[string]any{"actor_id": "a1", "name": "红二十五军", "evidence_refs": []any{"c1:s001"}},
			map[string]any{"actor_id": "a2", "name": "该军", "evidence_refs": []any{"c1:s002"}},
		},
		"spatial_entities": []any{
			map[string]any{"entity_id": "s1", "name": "甲地", "evidence_refs": []any{"c1:s001"}},
			map[string]any{"entity_id": "s2", "name": "乙地", "evidence_refs": []any{"c1:s002"}},
		},
		"events": []any{map[string]any{"event_id": "e1", "name": "到达乙地", "location_ref": "s2", "evidence_refs": []any{"c1:s002"}}},
		"routes": []any{map[string]any{"route_id": "r1", "name": "行军", "evidence_refs": []any{"c1:s002"}, "paths": []any{
			map[string]any{"path_id": "p1", "role": "main", "actor_refs": []any{"a2"}, "evidence_refs": []any{"c1:s002"}, "nodes": []any{
				map[string]any{"ref_id": "s1", "ref_type": "spatial_entity"}, map[string]any{"ref_id": "s2", "ref_type": "spatial_entity"},
			}},
		}}},
	}
	if err := materializeEvidence(g, sources); err != nil {
		t.Fatal(err)
	}
	return g, sources
}

func semanticComparisonOutput(t *testing.T, before Graph, sources []EvidenceSource, edit func(Graph)) modelResponse {
	t.Helper()
	view, err := graphModelView(before, sources)
	if err != nil {
		t.Fatal(err)
	}
	edit(view)
	if err := materializeEvidence(view, sources); err != nil {
		t.Fatal(err)
	}
	return modelResponse{Graph: view, Aliases: map[string]string{}}
}

func mergeComparisonActors(t *testing.T, before Graph, sources []EvidenceSource) modelResponse {
	t.Helper()
	out := semanticComparisonOutput(t, before, sources, func(g Graph) {
		a1 := array(g["actors"])[0].(map[string]any)
		a1["evidence_refs"] = []any{"c1:s001", "c1:s002"}
		g["actors"] = []any{a1}
		if err := rewriteGraphReferences(g, func(id string) (string, error) {
			if id == "a2" {
				return "a1", nil
			}
			return id, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	out.Aliases = map[string]string{"a2": "a1"}
	out.Changes = []SemanticChange{{SubjectRefs: []string{"a2", "a1"}, SourceRefs: []string{"c1:s001", "c1:s002"}, Explanation: "归并同一部队，更新相关事件和路线引用。"}}
	return out
}

func TestSemanticChangeAcceptsEvidencedAliasReferencePropagation(t *testing.T) {
	before, sources := semanticComparisonFixture(t)
	event := array(before["events"])[0].(map[string]any)
	event["actor_refs"] = []any{"a2"}
	out := mergeComparisonActors(t, before, sources)
	original, _ := json.Marshal(before)
	if _, err := validateSemanticChange(before, out, nil, sources); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(before)
	if string(after) != string(original) {
		t.Fatal("comparison changed source graph")
	}
	r := &Result{Graph: before, Aliases: map[string]string{}}
	if err := applySemanticScope(r, semanticScope{ids: GraphIDs(before)}, out, nil); err != nil {
		t.Fatal(err)
	}
	index, _ := indexGraph(r.Graph)
	if !reflect.DeepEqual(index["p1"].data["actor_refs"], []any{"a1"}) || !reflect.DeepEqual(index["e1"].data["actor_refs"], []any{"a1"}) {
		t.Fatal("accepted alias propagation was not applied")
	}
}

func TestSemanticChangeAliasProofDoesNotCoverOtherFacts(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(modelResponse)
		want string
	}{
		{"node order", func(out modelResponse) {
			index, _ := indexGraph(out.Graph)
			nodes := array(index["p1"].data["nodes"])
			nodes[0], nodes[1] = nodes[1], nodes[0]
		}, "p1"},
		{"free text", func(out modelResponse) {
			array(out.Graph["events"])[0].(map[string]any)["name"] = "a1"
		}, "e1"},
		{"added evidence", func(out modelResponse) {
			array(out.Graph["events"])[0].(map[string]any)["evidence_refs"] = []any{"c1:s001", "c1:s002"}
		}, "e1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, sources := semanticComparisonFixture(t)
			array(before["events"])[0].(map[string]any)["name"] = "a2"
			out := mergeComparisonActors(t, before, sources)
			tc.edit(out)
			if _, err := validateSemanticChange(before, out, nil, sources); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("substantive change escaped explanation: %v", err)
			}
		})
	}
	before, sources := semanticComparisonFixture(t)
	out := semanticComparisonOutput(t, before, sources, func(g Graph) {
		index, _ := indexGraph(g)
		index["p1"].data["actor_refs"] = []any{"a1"}
	})
	if _, err := validateSemanticChange(before, out, nil, sources); err == nil {
		t.Fatal("identity change without a declared alias was accepted")
	}
	out = mergeComparisonActors(t, before, sources)
	out.Changes = nil
	if _, err := validateSemanticChange(before, out, nil, sources); err == nil {
		t.Fatal("alias without evidence-backed change was accepted")
	}
}

func TestSemanticChangeOptionalEmptyArraysAreEquivalent(t *testing.T) {
	before, sources := semanticComparisonFixture(t)
	out := semanticComparisonOutput(t, before, sources, func(g Graph) {
		array(g["actors"])[0].(map[string]any)["aliases"] = []any{}
		array(g["events"])[0].(map[string]any)["actor_refs"] = []any{}
	})
	if _, err := validateSemanticChange(before, out, nil, sources); err != nil {
		t.Fatal(err)
	}
	if _, err := validateSemanticChange(out.Graph, modelResponse{Graph: before}, nil, sources); err != nil {
		t.Fatal(err)
	}
	array(out.Graph["events"])[0].(map[string]any)["actor_refs"] = []any{"a1"}
	if _, err := validateSemanticChange(before, out, nil, sources); err == nil {
		t.Fatal("nonempty optional references were ignored")
	}
	view, _ := graphModelView(before, sources)
	index, _ := indexGraph(view)
	index["p1"].data["actor_refs"] = []any{}
	index["p1"].data["nodes"] = []any{}
	normalizeSemanticComparison(view)
	if _, exists := index["p1"].data["actor_refs"]; !exists {
		t.Fatal("required path actor_refs erased")
	}
	if _, exists := index["p1"].data["nodes"]; !exists {
		t.Fatal("required path nodes erased")
	}
}

func newComparisonRoute(t *testing.T, before Graph, sources []EvidenceSource) modelResponse {
	t.Helper()
	out := semanticComparisonOutput(t, before, sources, func(g Graph) {
		g["routes"] = append(array(g["routes"]), map[string]any{"route_id": "r10", "name": "追击路线", "evidence_refs": []any{"c1:s003"}, "paths": []any{
			map[string]any{"path_id": "r10_p01", "role": "main", "actor_refs": []any{"a1"}, "evidence_refs": []any{"c1:s003"}, "nodes": []any{
				map[string]any{"ref_id": "s1", "ref_type": "spatial_entity"}, map[string]any{"ref_id": "s2", "ref_type": "spatial_entity"},
			}},
		}})
	})
	out.Changes = []SemanticChange{{SubjectRefs: []string{"r10"}, SourceRefs: []string{"c1:s003"}, Explanation: "依据原文补充追击路线及其路径。"}}
	return out
}

func TestSemanticChangeNewRouteProofCoversOnlyNewEvidencedPaths(t *testing.T) {
	before, sources := semanticComparisonFixture(t)
	out := newComparisonRoute(t, before, sources)
	if _, err := validateSemanticChange(before, out, nil, sources); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*modelResponse)
	}{
		{"missing proof", func(out *modelResponse) { out.Changes = nil }},
		{"unrelated source", func(out *modelResponse) { out.Changes[0].SourceRefs = []string{"c1:s001"} }},
		{"unknown source", func(out *modelResponse) { out.Changes[0].SourceRefs = []string{"c1:s999"} }},
		{"missing path evidence", func(out *modelResponse) {
			index, _ := indexGraph(out.Graph)
			delete(index["r10_p01"].data, "evidence_refs")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := newComparisonRoute(t, before, sources)
			tc.edit(&out)
			if _, err := validateSemanticChange(before, out, nil, sources); err == nil {
				t.Fatal("new path without matching proof was accepted")
			}
		})
	}
	// A route-level explanation cannot cover an existing path's node reorder.
	out = semanticComparisonOutput(t, before, sources, func(g Graph) {
		index, _ := indexGraph(g)
		nodes := array(index["p1"].data["nodes"])
		nodes[0], nodes[1] = nodes[1], nodes[0]
	})
	out.Changes = []SemanticChange{{SubjectRefs: []string{"r1"}, SourceRefs: []string{"c1:s002"}, Explanation: "核对路线。"}}
	if _, err := validateSemanticChange(before, out, nil, sources); err == nil || !strings.Contains(err.Error(), "p1") {
		t.Fatalf("existing path change was hidden by parent: %v", err)
	}
}

func TestSemanticChangeFeedbackListsAllUnexplainedObjects(t *testing.T) {
	before, sources := semanticComparisonFixture(t)
	out := semanticComparisonOutput(t, before, sources, func(g Graph) {
		array(g["events"])[0].(map[string]any)["name"] = "新事件名"
		array(g["spatial_entities"])[0].(map[string]any)["name"] = "新地点名"
	})
	_, err := validateSemanticChange(before, out, nil, sources)
	if err == nil || !strings.Contains(err.Error(), "对象 e1 ") || !strings.Contains(err.Error(), "对象 s1 ") {
		t.Fatalf("feedback omitted a failed object: %v", err)
	}
	if strings.Index(err.Error(), "对象 e1 ") > strings.Index(err.Error(), "对象 s1 ") {
		t.Fatal("feedback order is nondeterministic")
	}
}

func TestSemanticChangePathProofDoesNotNeedDuplicateParentProof(t *testing.T) {
	before, sources := semanticComparisonFixture(t)
	out := semanticComparisonOutput(t, before, sources, func(g Graph) {
		index, _ := indexGraph(g)
		nodes := array(index["p1"].data["nodes"])
		nodes[0], nodes[1] = nodes[1], nodes[0]
	})
	out.Changes = []SemanticChange{{SubjectRefs: []string{"p1"}, SourceRefs: []string{"c1:s002"}, Explanation: "依据原文修正该路径节点顺序。"}}
	if _, err := validateSemanticChange(before, out, nil, sources); err != nil {
		t.Fatalf("path change required duplicate container proof: %v", err)
	}
	// An explained node change cannot also hide unrelated parent metadata edits.
	array(out.Graph["routes"])[0].(map[string]any)["name"] = "未经说明的新路线名"
	if _, err := validateSemanticChange(before, out, nil, sources); err == nil || !strings.Contains(err.Error(), "对象 r1 ") {
		t.Fatalf("parent metadata change escaped explanation: %v", err)
	}
}

func TestSemanticChangeRouteMembershipAndOrderStillRequireProof(t *testing.T) {
	for _, scenario := range []string{"order", "add", "delete", "move"} {
		t.Run(scenario, func(t *testing.T) {
			before, sources := semanticComparisonFixture(t)
			route := array(before["routes"])[0].(map[string]any)
			copyGraph, _ := cloneGraph(before)
			otherPath := array(array(copyGraph["routes"])[0].(map[string]any)["paths"])[0].(map[string]any)
			otherPath["path_id"] = "p2"
			route["paths"] = append(array(route["paths"]), otherPath)
			if scenario == "move" {
				before["routes"] = append(array(before["routes"]), map[string]any{"route_id": "r2", "name": "另一条路线", "evidence_refs": []any{"c1:s002"}, "paths": []any{}})
			}
			out := semanticComparisonOutput(t, before, sources, func(g Graph) {
				r1 := array(g["routes"])[0].(map[string]any)
				paths := array(r1["paths"])
				switch scenario {
				case "order":
					paths[0], paths[1] = paths[1], paths[0]
				case "add":
					added := map[string]any{}
					for key, value := range paths[0].(map[string]any) {
						added[key] = value
					}
					added["path_id"] = "p3"
					r1["paths"] = append(paths, added)
				case "delete":
					r1["paths"] = paths[:1]
				case "move":
					r1["paths"] = paths[:1]
					array(g["routes"])[1].(map[string]any)["paths"] = []any{paths[1]}
				}
			})
			// Even a path-specific explanation does not silently explain a route's
			// membership/order or a move between two existing route containers.
			refs := []string{"p1", "p2"}
			if scenario == "add" {
				refs = append(refs, "p3")
			}
			out.Changes = []SemanticChange{{SubjectRefs: refs, SourceRefs: []string{"c1:s002"}, Explanation: "说明路径本身。"}}
			_, err := validateSemanticChange(before, out, nil, sources)
			if err == nil || !strings.Contains(err.Error(), "对象 r1 ") {
				t.Fatalf("route membership/order was silently accepted: %v", err)
			}
			if scenario == "move" && !strings.Contains(err.Error(), "对象 r2 ") {
				t.Fatalf("destination route membership escaped explanation: %v", err)
			}
		})
	}
}
