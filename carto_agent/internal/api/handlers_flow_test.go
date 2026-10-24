package api

import (
	"bytes"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/gorilla/mux"
)

func TestFlowAPIUsesRealStoreAndProtectsReadonlyPackages(t *testing.T) {
	root := t.TempDir()
	store := flow.NewStore(filepath.Join(root, "builtin"), filepath.Join(root, "home", "flows"), bindings.New(filepath.Join(root, "home", "config", "resource_bindings.yaml")))
	a := &API{}
	a.SetFlowStore(store)
	router := mux.NewRouter()
	a.SetupRoutes(router)
	call := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("X-AutoCarto-Contract-Version", "v1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: status %d: %s", method, path, w.Code, w.Body.String())
		}
		var envelope map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope
	}
	md := "---\n名称: 测试流程\n编号: test-flow\n---\n## 步骤\n### 检查\n- 做法：读取工程\n"
	created := call(http.MethodPost, "/api/flows", map[string]any{"flow_md": md}, 200)
	if created["success"] != true {
		t.Fatal(created)
	}
	call(http.MethodPost, "/api/flows", map[string]any{"flow_md": md}, 409)
	loaded := call(http.MethodGet, "/api/flows/test-flow", nil, 200)
	data := loaded["data"].(map[string]any)
	if data["plan"] != nil {
		t.Fatal("uncompiled package unexpectedly has a plan")
	}
	call(http.MethodPut, "/api/flows/test-flow", map[string]any{"flow_md": md + "\n补充说明"}, 200)
	call(http.MethodPost, "/api/flows/test-flow/copy", map[string]any{"new_id": "copied-flow", "new_name": "副本"}, 200)
	call(http.MethodGet, "/api/flows", nil, 200)
	call(http.MethodPost, "/api/flows/copied-flow/export", map[string]any{"target_path": filepath.Join(t.TempDir(), "package.zip"), "include_data_files": true}, 200)
	call(http.MethodDelete, "/api/flows/copied-flow", nil, 200)
	call(http.MethodGet, "/api/flows/copied-flow", nil, 404)
	call(http.MethodPost, "/api/flows", map[string]any{"flow_md": md, "unknown": true}, 400)
}

func TestFlowAPIRejectsUnsupportedContractVersion(t *testing.T) {
	a := &API{}
	r := httptest.NewRequest("GET", "/api/flows", nil)
	r.Header.Set("X-AutoCarto-Contract-Version", "future")
	w := httptest.NewRecorder()
	a.handleFlows(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
}

func TestBindingStatusReportsOrphansWithEmptyFlowList(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_HOME", "autocarto_data")
	t.Setenv("AUTOCARTO_ASSETS_ROOT", "autocarto")
	if err := os.MkdirAll(filepath.Join(root, "autocarto/skills/system"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "autocarto_data/config/resource_bindings.yaml")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	raw := "bindings:\n  flow:deleted-flow/resource_0123456789ab: ../keep\n"
	if err := os.WriteFile(file, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	a := &API{}
	router := mux.NewRouter()
	a.SetupRoutes(router)
	request := httptest.NewRequest("GET", "/api/flows/bindings/status", nil)
	request.Header.Set("X-AutoCarto-Contract-Version", "v1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	var result struct {
		Data struct {
			OrphanedCount int `json:"orphaned_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Data.OrphanedCount != 1 {
		t.Fatalf("%s %v", response.Body.String(), err)
	}
	store, err := a.flowStore()
	if err != nil {
		t.Fatal(err)
	}
	library, err := a.resourceStore()
	if err != nil {
		t.Fatal(err)
	}
	if store.Bindings != library.Bindings {
		t.Fatal("HTTP fallback constructed a separate binding store")
	}
	current, _ := os.ReadFile(file)
	if string(current) != raw {
		t.Fatal("status mutated the old file")
	}
}
