package api

import (
	"bytes"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/gorilla/mux"
)

func TestResourceAPIUsesLibraryAndSharedRelativeBindings(t *testing.T) {
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
	builtin := filepath.Join(root, "autocarto/resources/system")
	user := filepath.Join(root, "autocarto_data/resources")
	bindingFile := filepath.Join(root, "autocarto_data/config/resource_bindings.yaml")
	write(filepath.Join(builtin, "styles/说明.qml"), "<qgis><note>内置内容</note></qgis>")
	write(filepath.Join(builtin, "styles/说明.resource.yaml"), "apiVersion: resource/v1\nid: style.notes\ntype: style\nname: 说明\nentry: 说明.qml\n")
	write(filepath.Join(root, "source/说明.qml"), "<qgis><note>用户内容</note></qgis>")
	write(filepath.Join(user, "data/底图.csv"), "name,path\n道路,road.geojson\n")
	write(filepath.Join(user, "data/底图.resource.yaml"), "apiVersion: resource/v1\nid: data_list.base\ntype: data_list\nname: 底图\nentry: 底图.csv\n")
	write(filepath.Join(root, "data/road.geojson"), "{}")
	a := &API{}
	a.SetResourceLibrary(resource.NewLibrary(builtin, user, bindings.New(bindingFile)))
	a.SetFlowStore(flow.NewStore("", filepath.Join(root, "autocarto_data/flows"), bindings.New(filepath.Join(root, "autocarto_data/config/resource_bindings.yaml"))))
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
			t.Fatalf("%s %s 返回 %d，需要 %d：%s", method, path, response.Code, want, response.Body.String())
		}
		var envelope map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope
	}
	call("GET", "/api/resources?scope=builtin", nil, 200)
	call("DELETE", "/api/resources/style/说明", nil, 403)
	t.Chdir(t.TempDir())
	call("POST", "/api/resources/import", map[string]any{"source_path": "source/说明.qml", "type": "style"}, 409)
	copied := call("POST", "/api/resources/style.notes/copy", map[string]any{"override": true, "name": "我的说明"}, 200)["data"].(map[string]any)
	loaded := call("GET", "/api/resources/style/说明", nil, 200)["data"].(map[string]any)
	if loaded["owner"] != "library_user" || loaded["entry"] != filepath.Join(user, "styles/我的说明/说明.qml") {
		t.Fatal(loaded)
	}

	if _, exists := loaded["resolved_path"]; exists {
		t.Fatal("资源管理接口仍输出已退休的路径字段", loaded)
	}
	exact := call("GET", "/api/resources/by-id/style.notes", nil, 200)["data"].(map[string]any)
	if exact["id"] != "style.notes" || exact["owner"] != "library_builtin" || exact["overridden_by"] != copied["id"] || exact["version"] != "1.0.0" || exact["digest"] == "" {
		t.Fatal(exact)
	}
	call("POST", "/api/resources/import", map[string]any{"source_path": "source/说明.qml", "type": "style"}, 409)
	call("POST", "/api/resources/import", map[string]any{"source_path": "source/说明.qml", "type": "style", "unexpected": true}, 400)
	call("PUT", "/api/resources/bindings", map[string]any{"binding_key": "library:data_list.base", "base_dir": "data"}, 410)
	if err := bindings.New(bindingFile).Set("library:data_list.base", "data"); err != nil {
		t.Fatal(err)
	}
	call("GET", "/api/resources/data_list/底图", nil, 404)

	raw, err := os.ReadFile(bindingFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "flow:test-flow/") || !strings.Contains(string(raw), "library:data_list.base: data") || strings.Contains(string(raw), root) {
		t.Fatalf("两类 API 必须共享相对路径绑定配置：%s", raw)
	}
	call("DELETE", "/api/resources/style/我的说明", nil, 200)
	if item := call("GET", "/api/resources/style/说明", nil, 200)["data"].(map[string]any); item["origin"] != "library_builtin" {
		t.Fatal(item)
	}
	call("GET", "/api/resources/style/不存在", nil, 404)
	call("GET", "/api/resources?scope=unknown", nil, 400)
	call("PUT", "/api/resources/bindings", map[string]any{"binding_key": "library:data_list.base", "base_dir": "source/说明.qml"}, 410)
}

func TestResourceAPIRejectsUnsupportedContractVersion(t *testing.T) {
	a := &API{}
	request := httptest.NewRequest(http.MethodGet, "/api/resources", nil)
	request.Header.Set("X-AutoCarto-Contract-Version", "future")
	response := httptest.NewRecorder()
	a.handleResources(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("返回 %d", response.Code)
	}
}

func TestResourceTypesAPI(t *testing.T) {
	a := &API{}
	router := mux.NewRouter()
	a.SetupRoutes(router)
	request := httptest.NewRequest("GET", "/api/resources/types", nil)
	request.Header.Set("X-AutoCarto-Contract-Version", "v1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var envelope struct {
		Data []resource.TypeFormat `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 2 {
		t.Fatal(envelope.Data)
	}
	for _, typ := range envelope.Data {
		if typ.Label == "" || typ.Group == "" || len(typ.Words) == 0 {
			t.Fatal(typ)
		}
	}
}

func TestResourceLibraryFallbackIsShared(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_HOME", "runtime")
	t.Setenv("AUTOCARTO_ASSETS_ROOT", "assets")
	if err := os.MkdirAll(filepath.Join(root, "assets/skills/system"), 0700); err != nil {
		t.Fatal(err)
	}
	a := &API{}
	first, err := a.resourceStore()
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.resourceStore()
	if err != nil || first != second {
		t.Fatalf("fallback library must retain index cache: %p %p %v", first, second, err)
	}
}
