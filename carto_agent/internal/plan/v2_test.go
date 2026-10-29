package plan

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestV2NormalizesWithoutDuplicatingProducedData(t *testing.T) {
	p, err := Decode([]byte(`{"goal":"成图","guide":"marching-route","data":[{"id":"原文 1","name":"史料","source":"user","ref":"消息原文"}],"steps":[{"id":"extract","title":"抽取","inputs":["史料"],"produces":[{"id":"route","kind":"dataset","format":"route_dataset"}]},{"id":"draw","title":"导出PDF","inputs":["extract"],"capabilities":["imagined"],"produces":[{"id":"export.pdf","kind":"file","format":"pdf"}]}],"deliverables":["export.pdf"]}`))
	if err != nil {
		t.Fatal(err)
	}
	notes := NormalizeV2(p, map[string]bool{"route_knowledge": true, "print-layout": true, "file-operations": true})
	if len(notes) == 0 || len(p.Data) != 1 || p.Steps[0].Inputs[0] != p.Data[0].ID || p.Steps[1].Inputs[0] != "route" {
		t.Fatalf("normalization failed: %+v", p)
	}
	if p.Steps[0].Capabilities[0] != "route_knowledge" || len(ValidateV2(p)) > 0 {
		t.Fatal(p, ValidateV2(p))
	}
	p.Guide.Version = "6.0.0"
	copy, err := Clone(p)
	if err != nil || copy.Guide.Version != p.Guide.Version {
		t.Fatal(copy, err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err = json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["guide"] != "marching-route" || wire["deliverables"].([]any)[0] != "export.pdf" {
		t.Fatal(string(raw))
	}
	var persisted Plan
	if err = json.Unmarshal(raw, &persisted); err != nil || len(ValidateV2(&persisted)) != 0 {
		t.Fatal(err, ValidateV2(&persisted))
	}
}
func TestV2ReportsAllSemanticProblemsInOnePass(t *testing.T) {
	p := &Plan{Goal: "test", Data: []Data{{ID: "rivers", Name: "河流", Source: "retrieve", Extent: &Extent{From: "route"}}, {ID: "cycle_a", Extent: &Extent{From: "cycle_b"}}, {ID: "cycle_b", Extent: &Extent{From: "cycle_a"}}}, Steps: []Step{{ID: "load", Inputs: []string{"rivers", "missing"}, Produces: []Product{{ID: "map", Kind: "layer"}}}, {ID: "extract", Produces: []Product{{ID: "route", Kind: "dataset"}}}}}
	codes := map[string]bool{}
	for _, d := range ValidateV2(p) {
		codes[d.Code] = true
	}
	for _, code := range []string{"PLAN_NEED_MISSING", "PLAN_EXTENT_INVALID", "PLAN_REF_INVALID", "PLAN_EXTENT_CYCLE"} {
		if !codes[code] {
			t.Fatalf("missing %s: %v", code, codes)
		}
	}
}
func TestV1ReadableButCannotBeProposed(t *testing.T) {
	raw := []byte(`{"api_version":"carto-plan/v1","plan_id":"old","version":1,"goal":"old","guide":{"id":"old","version":"1"},"steps":[],"deliverables":[]}`)
	var saved Plan
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Guide.Version != "1" {
		t.Fatal(saved)
	}
	if _, err := Decode(raw); err == nil {
		t.Fatal("legacy plan accepted for new execution")
	}
}

func TestV2PreservesDeclaredCapabilitiesWithoutInferringFromProse(t *testing.T) {
	available := map[string]bool{"data-loading": true, "project-state": true, "cartography-styling": true, "vector-fields": true, "statistical-data-tools": true, "vector-geometry": true}
	for _, instructions := range []string{"按人口分级设色并标注城市名称", "合并300米内同名站点并保留对应关系"} {
		for _, declared := range [][]string{{"data-loading", "project-state"}, nil, {"imagined"}} {
			p := &Plan{Steps: []Step{{Title: "处理图层", RoleHint: "cartographer", Instructions: instructions, Capabilities: declared, Produces: []Product{{Kind: "layer"}}}}}
			NormalizeV2(p, available)
			if p.Steps[0].RoleHint != "cartographer" {
				t.Fatal("configured role choice changed", p.Steps[0])
			}
			if len(declared) > 0 && declared[0] != "imagined" {
				if !reflect.DeepEqual(p.Steps[0].Capabilities, declared) {
					t.Fatalf("valid declared responsibility changed: %+v", p.Steps[0])
				}
				continue
			}
			if len(p.Steps[0].Capabilities) != 0 {
				t.Fatalf("tool packages inferred from prose: %+v", p.Steps[0])
			}
		}
	}
}
func TestV2NormalizesGeoPackageProductFormat(t *testing.T) {
	p := &Plan{Steps: []Step{{Produces: []Product{{ID: "result", Kind: "file", Format: "GeoPackage"}}}}}
	NormalizeV2(p, nil)
	if p.Steps[0].Produces[0].Format != "gpkg" {
		t.Fatal(p.Steps[0])
	}
}

func TestV2RouteDatasetKeepsDedicatedExecutorDespiteDeclaredPackages(t *testing.T) {
	p := &Plan{Steps: []Step{{Capabilities: []string{"data-loading"}, Produces: []Product{{ID: "route", Kind: "dataset", Format: "route_dataset"}}}}}
	NormalizeV2(p, map[string]bool{"data-loading": true, "route_knowledge": true})
	if !reflect.DeepEqual(p.Steps[0].Capabilities, []string{"route_knowledge"}) {
		t.Fatalf("route dataset lost its dedicated executor: %+v", p.Steps[0])
	}
}
