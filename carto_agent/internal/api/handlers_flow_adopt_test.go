package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/gorilla/mux"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetiredResourceAdoptionIsAtomicAndKeepsLibrary(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", root)
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	library := resource.NewLibrary("", filepath.Join(root, "resources"), nil)
	store := flow.NewStore(filepath.Join(root, "builtin"), filepath.Join(root, "flows"), nil)
	dir := filepath.Join(library.UserDir, "docs")
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "说明.md"), []byte("notes"), 0600)
	_ = os.WriteFile(filepath.Join(dir, "说明.resource.yaml"), []byte("apiVersion: resource/v1\nid: doc.notes\ntype: doc\nname: 说明\nentry: 说明.md\n"), 0600)
	raw := "---\n编号: adopt-flow\n名称: 移入资源\n---\n## 使用的资源\n- 参考「说明」：公共资源库/doc.notes\n## 步骤\n### 阅读\n- 负责：用户\n- 做法：查看「说明」\n"
	item, err := store.Create(raw, "adopt-flow")
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = store.ResolveResources(context.Background(), "adopt-flow", library)
	if err != nil {
		t.Fatal(err)
	}
	a := &API{}
	a.SetFlowStore(store)
	a.SetResourceLibrary(library)
	a.SetFlowCompiler(flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: filepath.Join(root, "cache"), LoadEnvironment: func(context.Context, string) (flow.Environment, error) {
		return flow.Environment{Scheme: "default"}, nil
	}}))
	router := mux.NewRouter()
	a.SetupRoutes(router)
	call := func(path string, body string, want int) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", path, bytes.NewBufferString(body)))
		if w.Code != want {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		return w
	}
	call("/api/flows/adopt-flow/resources/adopt", `{"resource_id":"doc.notes","label":"missing"}`, 400)
	old, _ := store.Get("adopt-flow")
	if old.FlowMD != item.FlowMD {
		t.Fatal("failed adopt changed flow")
	}
	w := call("/api/flows/adopt-flow/resources/adopt", `{"resource_id":"doc.notes","label":"说明"}`, 200)
	var body struct {
		Data flow.StoredFlow `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Data.Plan == nil || strings.Contains(body.Data.FlowMD, "公共资源库/doc.notes") {
		t.Fatal(body)
	}
	after, _ := store.Get("adopt-flow")
	lock, err := flow.ReadLock(after.Dir)
	if err != nil || len(lock.Resources) != 0 {
		t.Fatal(lock, err)
	}
	value := library.Resolve(context.Background(), resource.Ref{Type: "doc", Name: "说明", Location: flow.Parse(after.FlowMD).Resources[0].Location}, resource.Scope{FlowDir: after.Dir})
	if value.Owner != "flow" || value.Handle() == nil {
		t.Fatal(value)
	}
	if _, err = library.GetByID("doc.notes"); err != nil {
		t.Fatal("library original removed", err)
	}
	_ = os.MkdirAll(filepath.Join(store.BuiltinDir, "builtin-flow"), 0700)
	_ = os.WriteFile(filepath.Join(store.BuiltinDir, "builtin-flow/FLOW.md"), []byte(raw), 0600)
	call("/api/flows/builtin-flow/resources/adopt", `{"resource_id":"doc.notes","label":"说明"}`, 403)
}
