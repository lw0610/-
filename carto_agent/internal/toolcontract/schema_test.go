package toolcontract

import (
	"encoding/json"
	"testing"
)

func TestMaterializedSchemaPreservesEmptyObjectsAcrossExports(t *testing.T) {
	original := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"value": map[string]any{},
			"alternatives": map[string]any{
				"anyOf": []any{map[string]any{}, map[string]any{"type": "string"}},
			},
		},
		"additionalProperties": false,
	}
	contract := Contract{
		APIVersion: ContractAPIVersionV1,
		Kind:       KindToolContract,
		Metadata:   Metadata{Name: "schema_probe", Runtime: "internal", Backend: "internal"},
		Spec: Spec{
			Description: "Retain arbitrary JSON values in the tool contract.",
			Parameters:  []Parameter{{Name: "payload", Type: "object", Schema: original}},
		},
		Policy:    Policy{Visibility: VisibilityPublic},
		Execution: Execution{Binding: ExecutionBindingBackendTool, BackendTool: "schema_probe"},
	}
	tools, err := Materialize([]Contract{contract})
	if err != nil {
		t.Fatal(err)
	}
	catalog, ok := tools[0].PublicCatalogEntry(RuntimeState{Available: true})
	if !ok {
		t.Fatal("public tool missing from catalog")
	}
	manifest := tools[0].ExecutionManifestEntry()
	want, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	for name, params := range map[string][]Parameter{
		"materialized": tools[0].InputSchema.Parameters,
		"catalog":      catalog.InputSchema.Parameters,
		"manifest":     manifest.Parameters,
	} {
		t.Run(name, func(t *testing.T) {
			actual, err := json.Marshal(params[0].Schema)
			if err != nil {
				t.Fatal(err)
			}
			if string(actual) != string(want) {
				t.Fatalf("schema changed during export:\n got %s\nwant %s", actual, want)
			}
			// Each consumer may modify its exported schema without changing authority.
			props := params[0].Schema["properties"].(map[string]any)
			props["value"].(map[string]any)["type"] = "string"
			if len(original["properties"].(map[string]any)["value"].(map[string]any)) != 0 {
				t.Fatal("export shares schema objects with the source contract")
			}
		})
	}
}
