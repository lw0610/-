package api

import (
	"bytes"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/gorilla/mux"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuideAPIManagementWithoutCompiler(t *testing.T) {
	root := t.TempDir()
	binding := bindings.New(filepath.Join(root, "bindings.yaml"))
	s := flow.NewGuideStore(flow.NewStore(filepath.Join(root, "builtin"), filepath.Join(root, "flows"), binding), filepath.Join(root, "guides.yaml"))
	a := &API{resourceLibrary: resource.NewLibrary("", "", binding)}
	a.SetGuideStore(s)
	router := mux.NewRouter()
	a.SetupRoutes(router)
	call := func(method, path string, body any, status int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("X-AutoCarto-Contract-Version", "v1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	raw := "---\n名称: 测试指南\n编号: guide-test\n---\n## 适用任务\n检查资料\n## 数据\n无\n## 制作要点\n阅读后总结\n## 产出与检查\n- 「结果」（文本）\n"
	created := call("POST", "/api/guides", map[string]any{"guide_md": raw}, 200)["data"].(map[string]any)
	if created["guide"] == nil || created["plan"] != nil || created["enabled"] != true {
		t.Fatal(created)
	}
	context := call("GET", "/api/guides/guide-test/context", nil, 200)["data"].(map[string]any)["content"].(string)
	if !strings.Contains(context, "阅读后总结") || !strings.Contains(context, "引用的样式与版面") {
		t.Fatal(context)
	}
	call("POST", "/api/guides", map[string]any{"guide_md": raw}, 409)
	call("PUT", "/api/guides/guide-test", map[string]any{"guide_md": raw + "\n说明\n"}, 200)
	disabled := call("PUT", "/api/guides/guide-test/enabled", map[string]any{"enabled": false}, 200)["data"].(map[string]any)
	if disabled["enabled"] != false {
		t.Fatal(disabled)
	}
	call("GET", "/api/guides", nil, 200)
	call("POST", "/api/guides/guide-test/copy", map[string]any{"new_id": "guide-copy"}, 200)
	zip := filepath.Join(t.TempDir(), "package.zip")
	call("POST", "/api/guides/guide-copy/export", map[string]any{"target_path": zip, "include_data_files": true}, 200)
	call("DELETE", "/api/guides/guide-copy", nil, 200)
	call("POST", "/api/guides/import", map[string]any{"source_path": zip}, 200)
	call("GET", "/api/guides/guide-copy", nil, 200)
	call("POST", "/api/guides", map[string]any{"guide_md": raw, "unknown": true}, 400)
	call("GET", "/api/guides/missing", nil, 404)
}
