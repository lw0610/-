package skill

import (
	"testing"

	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
)

func TestConvertBridgeToolDefinitions(t *testing.T) {
	t.Parallel()

	defs := ConvertBridgeToolDefinitions([]bridgepkg.ToolDefinition{
		{
			Name:        "data_load_vector",
			Description: "Load vector data.",
			Category:    "data",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []any{"path"},
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Layer path.",
					},
				},
			},
		},
	}, "instance-1")

	if len(defs) != 1 {
		t.Fatalf("expected one converted definition, got %d", len(defs))
	}
	def := defs[0]
	if def.Metadata.Name != "data_load_vector" {
		t.Fatalf("unexpected name: %s", def.Metadata.Name)
	}
	if def.Metadata.Runtime != RuntimeIDQGIS {
		t.Fatalf("unexpected runtime: %s", def.Metadata.Runtime)
	}
	if def.Metadata.Backend != RuntimeIDQGIS {
		t.Fatalf("unexpected backend: %s", def.Metadata.Backend)
	}
	if def.Metadata.Domain != "data" {
		t.Fatalf("unexpected domain: %s", def.Metadata.Domain)
	}
	if def.Metadata.Source != "bridge" || !def.Metadata.Dynamic {
		t.Fatalf("unexpected source/dynamic metadata: %#v", def.Metadata)
	}
	if len(def.Metadata.InstanceIDs) != 1 || def.Metadata.InstanceIDs[0] != "instance-1" {
		t.Fatalf("unexpected instance ids: %#v", def.Metadata.InstanceIDs)
	}
	if def.Execution.BackendTool != "data_load_vector" {
		t.Fatalf("unexpected backend tool: %s", def.Execution.BackendTool)
	}
	if len(def.Spec.Parameters) != 1 || def.Spec.Parameters[0].Name != "path" || !def.Spec.Parameters[0].Required {
		t.Fatalf("unexpected parameters: %#v", def.Spec.Parameters)
	}
}
