package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/gorilla/mux"
)

func TestFlowResourceRepairAndExportAPI(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_HOME", root)
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	b := bindings.New(filepath.Join(root, "bindings.yaml"))
	library := resource.NewLibrary(filepath.Join(root, "builtin"), filepath.Join(root, "user"), b)
	for _, part := range []struct{ root, id string }{{library.BuiltinDir, "style.first"}, {library.UserDir, "style.second"}} {
		write(filepath.Join(part.root, "styles/说明.qml"), "<qgis><note>"+part.id+"</note></qgis>")
		write(filepath.Join(part.root, "styles/说明.resource.yaml"), "apiVersion: resource/v1\nid: "+part.id+"\ntype: style\nname: 说明\nentry: 说明.qml\nversion: 1.0.0\n")
	}
	store := flow.NewStore(filepath.Join(root, "builtin-flows"), filepath.Join(root, "flows"), b)
	raw := "---\n编号: repair-flow\n名称: 资源修复\n---\n## 使用的资源\n- 样式「说明」：公共资源库\n## 步骤\n### 阅读\n- 负责：用户\n- 做法：查看「说明」。\n"
	if _, err := store.Create(raw, "repair-flow"); err != nil {
		t.Fatal(err)
	}
	a := &API{}
	a.SetResourceLibrary(library)
	a.SetFlowStore(store)
	a.SetFlowCompiler(flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: filepath.Join(root, "cache"), LoadEnvironment: func(context.Context, string) (flow.Environment, error) {
		return flow.Environment{Scheme: "default"}, nil
	}}))
	router := mux.NewRouter()
	a.SetupRoutes(router)
	call := func(path string, body any, status int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", path, bytes.NewReader(raw))
		r.Header.Set("X-AutoCarto-Contract-Version", "v1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	endpoint := "/api/flows/repair-flow/resources/repair?scheme=default"
	call(endpoint, map[string]string{"action": "choose_resource", "resource_key": "unresolved:style:说明", "resource_id": "style.second"}, 200)
	item, err := store.Get("repair-flow")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := flow.ReadLock(item.Dir)
	if err != nil || lock.Resources[0].ID != "style.second" {
		t.Fatal(lock, err)
	}
	result := call(endpoint, map[string]string{"action": "update_lock", "resource_key": "library:style.second"}, 200)["data"].(map[string]any)
	if result["meta"] == nil || result["plan"] == nil || result["diagnostics"] == nil {
		t.Fatalf("not a FlowDetail: %+v", result)
	}
	call(endpoint, map[string]string{"action": "choose_resource", "resource_key": "library:style.second", "resource_id": "style.missing"}, 400)
	call(endpoint, map[string]string{"action": "unknown", "resource_key": "library:style.second"}, 400)
	export := filepath.Join(root, "export.zip")
	call("/api/flows/repair-flow/export", map[string]any{"target_path": export}, 200)
	// Default include_library_deps=true produces a portable package.
	empty := resource.NewLibrary(filepath.Join(root, "empty-builtin"), filepath.Join(root, "empty-user"), b)
	dest := flow.NewStore("", filepath.Join(root, "imported"), b)
	imported, err := dest.Import(export, flow.ImportOptions{}, empty)
	if err != nil {
		t.Fatal(err)
	}
	lock, err = flow.ReadLock(imported.Dir)
	if err != nil || lock.Resources[0].Source != "bundled" {
		t.Fatal(lock, err)
	}
	// A repository-owned flow can be inspected but all repair mutations are denied.
	write(filepath.Join(store.BuiltinDir, "builtin-flow/FLOW.md"), raw)
	call("/api/flows/builtin-flow/resources/repair", map[string]string{"action": "choose_resource", "resource_key": "unresolved:style:说明", "resource_id": "style.second"}, 403)
}
