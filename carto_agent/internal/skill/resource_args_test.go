package skill

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

func resourceEngine(t *testing.T, typ, fallback string) (*Engine, *fakeComputeInvoker, *resource.Library, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_HOME", filepath.Join(root, "runtime"))
	registry := loadRegistryForTest(t, map[string]string{"resource_tool.yaml": `apiVersion: tool/v1
kind: Tool
metadata:
  name: resource_tool
  domain: test
  runtime: qgis
spec:
  description: resource behavior
  parameters:
    - name: selected
      type: resource
      resource_type: ` + typ + `
      required: true
      default_resource: ` + fallback + `
    - name: base_dir
      type: string
execution:
  backend_tool: apply
  param_mapping:
    selected: "{{ selected }}"
`})
	backend := &fakeComputeInvoker{}
	library := resource.NewLibrary(filepath.Join(root, "builtin"), filepath.Join(root, "user"), bindings.New(filepath.Join(root, "bindings.yaml")))
	engine := NewEngine(registry, NewComputeBackend(backend))
	engine.SetResourceResolver(library)
	return engine, backend, library, root
}
func writeResource(t *testing.T, root, owner, id, name, override string) string {
	t.Helper()
	dir := filepath.Join(root, owner, "styles")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".qml")
	if err := os.WriteFile(path, []byte(`<qgis><layerGeometryType>2</layerGeometryType></qgis>`), 0644); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: resource/v1\nid: " + id + "\nname: " + name + "\ntype: style\nversion: 1.0.0\nentry: " + name + ".qml\n"
	if override != "" {
		manifest += "overrides: " + override + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name+".resource.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestResourceArgumentsResolveNameIDPathDefaultAndOverride(t *testing.T) {
	engine, backend, _, root := resourceEngine(t, "style", "style.sample")
	path := writeResource(t, root, "builtin", "style.sample", "示例样式", "")
	for _, value := range []any{"示例样式", "「示例样式」", "style.sample", path, nil} {
		args := map[string]any{}
		if value != nil {
			args["selected"] = value
		}
		result, err := engine.Execute(context.Background(), "resource_tool", args)
		if err != nil {
			t.Fatal(err)
		}
		handle, ok := backend.lastReq.Params["selected"].(map[string]any)
		if !ok || handle["api_version"] != "resource-handle/v1" || handle["id"] != "style.sample" {
			t.Fatalf("handle: %#v", backend.lastReq.Params)
		}
		raw, _ := json.Marshal(result.Params)
		if strings.Contains(string(raw), "resource-handle") || strings.Contains(string(raw), "entries") {
			t.Fatalf("expanded handle leaked: %s", raw)
		}
		if value != nil && args["selected"] != value {
			t.Fatal("caller arguments changed")
		}
	}
	writeResource(t, root, "user", "style.user", "用户样式", "style.sample")
	_, err := engine.Execute(context.Background(), "resource_tool", nil)
	if err != nil {
		t.Fatal(err)
	}
	if backend.lastReq.Params["selected"].(map[string]any)["id"] != "style.user" {
		t.Fatal("explicit override was ignored")
	}
}
func TestResourceArgumentsRejectInvalidAndUntrustedHandle(t *testing.T) {
	engine, backend, _, root := resourceEngine(t, "style", "")
	writeResource(t, root, "builtin", "style.sample", "示例样式", "")
	for _, value := range []any{"不存在", 42, map[string]any{"api_version": "resource-handle/v1", "id": "style.sample"}, nil} {
		_, err := engine.Execute(context.Background(), "resource_tool", map[string]any{"selected": value})
		if err == nil {
			t.Fatalf("accepted %v", value)
		}
		if value == "不存在" && !strings.Contains(err.Error(), "示例样式") {
			t.Fatalf("missing candidates: %v", err)
		}
	}
	if backend.callCount != 0 {
		t.Fatal("invalid resource reached backend")
	}
	engine.SetResourceResolver(nil)
	_, err := engine.Execute(context.Background(), "resource_tool", map[string]any{"selected": "示例样式"})
	if err == nil || !strings.Contains(err.Error(), "资源服务未就绪") {
		t.Fatalf("got %v", err)
	}
}
func TestRetiredResourceTypesDoNotReachRuntime(t *testing.T) {
	for _, kind := range []string{"data_list", "data_folder", "style_set", "route_style", "symbols", "doc"} {
		engine, backend, _, _ := resourceEngine(t, kind, "")
		_, err := engine.Execute(context.Background(), "resource_tool", map[string]any{"selected": "old"})
		if err == nil || backend.callCount != 0 {
			t.Fatalf("retired type %s reached backend: %v", kind, err)
		}
	}
}

func TestNestedStyleResourcesUseScopedResolver(t *testing.T) {
	engine, _, _, root := resourceEngine(t, "style", "")
	writeResource(t, root, "builtin", "style.sample", "示例样式", "")
	schema := map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"style": map[string]any{"type": "string", "x-resource-type": "style"}}}}
	value := []any{map[string]any{"layer": "roads", "style": "示例样式"}}
	out, err := engine.expandNestedResourceArgs(context.Background(), SkillDefinition{}, schema, value)
	if err != nil {
		t.Fatal(err)
	}
	nested := out.([]any)[0].(map[string]any)["style"].(map[string]any)
	if nested["id"] != "style.sample" {
		t.Fatal(nested)
	}
	if value[0].(map[string]any)["style"] != "示例样式" {
		t.Fatal("mutated trace arguments")
	}
}
