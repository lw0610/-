package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource"
)

func TestBuiltinLayerDefaultAndExplicitStyleUseResourceResolver(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	assets := filepath.Join(root, "autocarto")
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", assets)
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	raw, err := os.ReadFile(filepath.Join(assets, "tools/system/data-loading/layer_load_builtin.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	registry := loadRegistryForTest(t, map[string]string{"layer_load_builtin.yaml": string(raw)})
	backend := &fakeComputeInvoker{}
	engine := NewEngine(registry, NewComputeBackend(backend))
	engine.SetResourceResolver(resource.NewAssetLibrary(assets))
	for _, selected := range []string{"", "县域面"} {
		args := map[string]any{"name": "省域面"}
		if selected != "" {
			args["style"] = selected
		}
		result, err := engine.Execute(context.Background(), "layer_load_builtin", args)
		if err != nil {
			t.Fatal(err)
		}
		handle, ok := backend.lastReq.Params["style"].(map[string]any)
		want := selected
		if want == "" {
			want = "省域面"
		}
		if !ok || handle["api_version"] != "resource-handle/v1" || handle["name"] != want || handle["type"] != "style" {
			t.Fatalf("default/explicit style did not reach the backend as a resolved handle: %+v", backend.lastReq.Params)
		}
		if result.Params["style"] != want {
			t.Fatalf("resource internals leaked into trace: %+v", result.Params)
		}
	}
}
