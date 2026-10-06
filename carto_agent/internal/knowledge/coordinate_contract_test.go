package knowledge

import (
	"encoding/json"
	"os"
	"testing"
)

func TestModelCoordinateContractSeparatesLookupMetadata(t *testing.T) {
	raw, err := os.ReadFile("../../../autocarto/knowledge/schemas/task_knowledge_graph_v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := modelGraphSchema(Contract{JSONSchema: json.RawMessage(raw)})
	if err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	entitySchema := properties["spatial_entities"].(map[string]any)["items"].(map[string]any)
	entity := map[string]any{
		"entity_id": "place_a", "entity_kind": "place", "name": "甲地", "factual": true,
		"map_role": "route_node", "evidence_refs": []any{"s1"},
		"lon": 109.8, "lat": 33.5, "coordinate_source": "source_text",
	}
	if err := validateSchemaValue(entity, entitySchema, "entity"); err != nil {
		t.Fatalf("source coordinates rejected: %v", err)
	}
	for _, source := range []string{"llm_estimated", "gazetteer", "manual"} {
		entity["coordinate_source"] = source
		if err := validateSchemaValue(entity, entitySchema, "entity"); err == nil {
			t.Fatalf("model must not produce coordinate source %s", source)
		}
	}
	entity["coordinate_source"] = "source_text"
	entity["geocoding"] = map[string]any{"status": "resolved"}
	if err := validateSchemaValue(entity, entitySchema, "entity"); err == nil {
		t.Fatal("model-generated geocoding metadata accepted")
	}
}

func TestSourceCoordinatesRequireValuesInTheirOwnEvidence(t *testing.T) {
	tests := []struct {
		name, text string
		wantErr    bool
	}{
		{"explicit", "甲地东经109.800度，北纬33.50度。", false},
		{"text only", "一九三四年队伍途经甲地。", true},
		{"unrelated numeric suffix", "编号1109.8的地点位于33.5公里之外。", true},
		{"only longitude", "甲地东经109.8度。", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entity := map[string]any{
				"name": "甲地", "lon": 109.8, "lat": 33.5, "coordinate_source": "source_text", "evidence_refs": []any{"s1"},
			}
			graph := Graph{"spatial_entities": []any{entity}}
			sources := []EvidenceSource{{Ref: "s1", Text: tt.text}, {Ref: "other", Text: "乙地坐标109.8,33.5。"}}
			if err := validateSourceCoordinates(graph, sources); (err != nil) != tt.wantErr {
				t.Fatalf("got %v; want error=%v", err, tt.wantErr)
			}
		})
	}
}

func TestPendingSourceCoordinatesOmitAllCoordinateMetadata(t *testing.T) {
	entity := map[string]any{"name": "甲地", "evidence_refs": []any{"s1"}}
	graph := Graph{"spatial_entities": []any{entity}}
	if err := validateSourceCoordinates(graph, nil); err != nil {
		t.Fatal(err)
	}
	entity["coordinate_source"] = "source_text"
	if err := validateSourceCoordinates(graph, nil); err == nil {
		t.Fatal("coordinate metadata without coordinates accepted")
	}
}
