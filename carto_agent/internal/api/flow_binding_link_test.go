package api

import (
	"bytes"
	"context"
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

func TestFlowBindingLinksWithoutInvokingModelEvenWithoutCache(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_HOME", root)
	store := flow.NewStore("", filepath.Join(root, "flows"), bindings.New(filepath.Join(root, "config", "resource_bindings.yaml")))
	item, err := store.Create("---\n名称: 绑定测试\n编号: bind-test\n---\n## 需要准备\n- 数据目录（必填，文件夹）\n## 使用的资源\n- 数据清单「底图」：resources/data/base.csv（文件所在文件夹：「数据目录」）\n## 步骤\n### 第1步 检查\n- 负责：用户\n- 做法：检查「底图」\n- 完成标准：确认\n", "bind-test")
	if err != nil {
		t.Fatal(err)
	}
	list := filepath.Join(item.Dir, "resources", "data", "base.csv")
	if err = os.MkdirAll(filepath.Dir(list), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(list, []byte("名称,路径\n区域,base.geojson\n"), 0600); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	if err = os.WriteFile(filepath.Join(base, "base.geojson"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	library := resource.NewLibrary("", filepath.Join(root, "resources"), store.Bindings)
	compiler := flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: filepath.Join(root, "cache"), LoadEnvironment: func(context.Context, string) (flow.Environment, error) {
		return flow.Environment{Scheme: "test", Model: apiFlowModelFunc(func(context.Context, flow.ModelRequest) (string, error) {
			t.Fatal("binding invoked model")
			return "", nil
		})}, nil
	}})
	a := &API{}
	a.SetFlowStore(store)
	a.SetResourceLibrary(library)
	a.SetFlowCompiler(compiler)
	router := mux.NewRouter()
	a.SetupRoutes(router)
	raw, _ := json.Marshal(map[string]string{"resource_key": "path:data/base.csv", "base_dir": base})
	req := httptest.NewRequest("PUT", "/api/flows/bind-test/bindings", bytes.NewReader(raw))
	req.Header.Set("X-AutoCarto-Contract-Version", "v1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 || strings.Contains(w.Body.String(), "FLOW_RESOURCE_UNBOUND") || !strings.Contains(w.Body.String(), "link_fingerprint") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	memory, _ := flow.DefaultInputMemory()
	values, err := memory.Get("bind-test")
	if err != nil || values["数据目录"] != base {
		t.Fatal(values, err)
	}
	files, _ := filepath.Glob(filepath.Join(root, "cache", "bind-test", "test", "*.plan.json"))
	if len(files) != 0 {
		t.Fatal("binding compiled plan")
	}
}
