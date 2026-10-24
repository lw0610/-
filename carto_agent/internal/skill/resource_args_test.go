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
func TestResourceArgumentsUseLinkedSnapshotAndBaseDirBeforeValidation(t *testing.T) {
	engine, backend, library, root := resourceEngine(t, "data_list", "")
	flowDir := filepath.Join(root, "flow")
	baseDir := filepath.Join(root, "data")
	for _, p := range []string{flowDir, baseDir} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	list := filepath.Join(flowDir, "list.csv")
	os.WriteFile(list, []byte("name,path,type\n测试,data.geojson,vector\n"), 0644)
	os.WriteFile(filepath.Join(baseDir, "data.geojson"), []byte(`{"type":"FeatureCollection","features":[]}`), 0644)
	r := library.Resolve(context.Background(), resource.Ref{Type: "data_list", Name: "底图", Location: list}, resource.Scope{FlowID: "f", FlowDir: flowDir})
	ctx := resource.WithScope(context.Background(), resource.Scope{FlowID: "f", FlowDir: flowDir, Linked: []*resource.ResolvedResource{r}})
	_, err := engine.Execute(ctx, "resource_tool", map[string]any{"selected": "底图", "base_dir": baseDir})
	if err != nil {
		t.Fatal(err)
	}
	handle := backend.lastReq.Params["selected"].(map[string]any)
	if handle["base_dir"] != baseDir {
		t.Fatalf("wrong base: %#v", handle)
	}
	entries := handle["entries"].([]map[string]any)
	if entries[0]["path"] != filepath.Join(baseDir, "data.geojson") {
		t.Fatal(entries)
	}
	// A deterministic executor may pass a handle, but it is resolved again against
	// the linked scope, never trusted as an arbitrary dictionary of file paths.
	r = library.Resolve(context.Background(), resource.Ref{Type: "data_list", Name: "底图", Location: list, BaseDir: baseDir}, resource.Scope{FlowID: "f", FlowDir: flowDir})
	ctx = resource.WithScope(resource.WithInternalHandles(context.Background()), resource.Scope{FlowID: "f", FlowDir: flowDir, Linked: []*resource.ResolvedResource{r}})
	forged := r.Handle()
	forged["entry"] = "/untrusted/file"
	_, err = engine.Execute(ctx, "resource_tool", map[string]any{"selected": forged})
	if err != nil {
		t.Fatal(err)
	}
	if backend.lastReq.Params["selected"].(map[string]any)["entry"] != list {
		t.Fatal("trusted marker bypassed resolver")
	}
}
