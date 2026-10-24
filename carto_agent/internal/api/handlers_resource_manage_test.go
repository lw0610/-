package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/gorilla/mux"
)

func TestResourceManagementAPI(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	builtin, user := filepath.Join(root, "builtin"), filepath.Join(root, "user")
	write(filepath.Join(builtin, "styles/说明.qml"), "<qgis><note>builtin text</note></qgis>")
	write(filepath.Join(builtin, "styles/说明.resource.yaml"), "apiVersion: resource/v1\nid: style.notes\ntype: style\nname: 说明\nentry: 说明.qml\nversion: 1.0.0\n")
	bindingStore := bindings.New(filepath.Join(root, "bindings.yaml"))
	library := resource.NewLibrary(builtin, user, bindingStore)
	flows := flow.NewStore(filepath.Join(root, "builtin-flows"), filepath.Join(root, "user-flows"), bindingStore)
	a := &API{}
	a.SetResourceLibrary(library)
	a.SetFlowStore(flows)
	router := mux.NewRouter()
	a.SetupRoutes(router)
	call := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		request := httptest.NewRequest(method, path, bytes.NewReader(raw))
		request.Header.Set("X-AutoCarto-Contract-Version", "v1")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, response.Code, response.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	copied := call("POST", "/api/resources/style.notes/copy", map[string]any{"override": true}, 200)["data"].(map[string]any)
	id := copied["id"].(string)
	if copied["overrides"] != "style.notes" || copied["owner"] != "library_user" {
		t.Fatal(copied)
	}
	source := filepath.Join(root, "new.qml")
	write(source, "<qgis><note>new text</note></qgis>")
	replaced := call("PUT", "/api/resources/"+id+"/content", map[string]any{"source_path": source}, 200)["data"].(map[string]any)
	if replaced["version"] != "1.0.1" {
		t.Fatal(replaced)
	}
	for _, checkID := range []string{"style.notes", id} {
		detail := call("GET", "/api/resources/by-id/"+checkID, nil, 200)["data"].(map[string]any)
		if detail["id"] != checkID || detail["format"] != "qml" {
			t.Fatal(detail)
		}
	}
	md := "---\n名称: 引用流程\n编号: usage-flow\n---\n## 使用的资源\n- 样式「说明」：公共资源库\n## 步骤\n### 阅读\n- 做法：查看说明\n"
	write(filepath.Join(flows.UserDir, "usage-flow/FLOW.md"), md)
	refs := call("GET", "/api/resources/"+id+"/references", nil, 200)["data"].([]any)
	if len(refs) != 1 || refs[0].(map[string]any)["via"] != "name" {
		t.Fatal(refs)
	}
	blocked := call("DELETE", "/api/resources/"+id, nil, 409)
	if len(blocked["details"].(map[string]any)["references"].([]any)) != 1 {
		t.Fatal(blocked)
	}
	// Legacy deletion cannot bypass reference protection.
	call("DELETE", "/api/resources/style/说明", nil, 409)
	write(filepath.Join(flows.UserDir, "usage-flow/flow.lock"), "apiVersion: flow-lock/v1\nresources:\n  - {type: style, name: 说明, id: style.notes, version: 1.0.0, digest: sha256:0000000000000000000000000000000000000000000000000000000000000000, source: library_builtin}\n")
	refs = call("GET", "/api/resources/"+id+"/references", nil, 200)["data"].([]any)
	if refs[0].(map[string]any)["via"] != "lock" {
		t.Fatal(refs)
	}
	call("DELETE", "/api/resources/"+id+"?force=true", nil, 200)
	call("DELETE", "/api/resources/style.notes?force=true", nil, 403)
	imported := call("POST", "/api/resources/import", map[string]any{"source_path": source, "type": "style", "name": "用户文档"}, 200)["data"].(map[string]any)
	conflict := call("POST", "/api/resources/import", map[string]any{"source_path": source, "type": "style", "name": "用户文档"}, 409)
	if conflict["details"].(map[string]any)["existing_id"] != imported["id"] {
		t.Fatal(conflict)
	}
	write(filepath.Join(user, "styles/旧文档.qml"), "<qgis><note>legacy</note></qgis>")
	items, err := library.List("user")
	if err != nil {
		t.Fatal(err)
	}
	oldID := ""
	for _, item := range items {
		if item.Name == "旧文档" {
			oldID = item.ID
		}
	}
	upgrade := call("POST", "/api/resources/"+oldID+"/upgrade", map[string]any{}, 200)["data"].(map[string]any)
	if upgrade["old_id"] != oldID || upgrade["legacy"] != false || !strings.HasPrefix(upgrade["id"].(string), "style.u-") {
		t.Fatal(upgrade)
	}
	call("POST", "/api/resources/style.notes/upgrade", map[string]any{}, 403)
}
