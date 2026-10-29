package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResourcePreviewOptionalLayerMapping(t *testing.T) {
	path := filepath.Join("..", "..", "..", "autocarto", "tools", "system", "print-layout", "resource_preview.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	registry := loadRegistryForTest(t, map[string]string{"print-layout/resource_preview.yaml": string(raw)})
	definition, ok := registry.Get("resource_preview")
	if !ok {
		t.Fatal("preview tool missing")
	}
	for _, layer := range []string{"", "selected-layer-id"} {
		args := map[string]any{"resource_path": "/tmp/style.qml", "resource_type": "style", "output_path": "/tmp/preview.png"}
		if layer != "" {
			args["layer_id"] = layer
		}
		resolved, err := resolveParamMapping(definition.Execution.ParamMapping, args, definition.Spec.Parameters, SessionContext{}, path)
		if err != nil {
			t.Fatal(err)
		}
		if actual, exists := resolved["layer_id"]; (layer == "" && exists && actual != "") || (layer != "" && actual != layer) {
			t.Fatalf("layer mapping must preserve omission or selected ID: %#v", resolved)
		}
	}
}
