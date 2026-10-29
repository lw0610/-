package api

import (
	"bytes"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/gorilla/mux"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRetiredFlowResourceMutationRoutesAreUnavailable(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", root)
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	library := resource.NewLibrary("", filepath.Join(root, "resources"), nil)
	store := flow.NewStore("", filepath.Join(root, "flows"), nil)
	raw := "---\n名称: 测试\n说明: 自由文档\n---\n用户正文\n"
	item, err := store.Create(raw, "safe-flow")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "notes.md")
	if err = os.WriteFile(source, []byte("private source"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &API{}
	a.SetResourceLibrary(library)
	a.SetGuideStore(flow.NewGuideStore(store, filepath.Join(root, "guides.yaml")))
	router := mux.NewRouter()
	a.SetupRoutes(router)
	for _, c := range []struct{ method, path string }{{"POST", "resources/import"}, {"POST", "resources/adopt"}, {"DELETE", "resources"}} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(c.method, "/api/guides/safe-flow/"+c.path, bytes.NewBufferString(`{"source_path":"`+source+`"}`)))
		if w.Code != 404 {
			t.Fatalf("retired route %s returned %d", c.path, w.Code)
		}
	}
	after, err := store.Get("safe-flow")
	if err != nil || after.FlowMD != item.FlowMD {
		t.Fatal("retired route mutated flow", err)
	}
	data, err := os.ReadFile(source)
	if err != nil || string(data) != "private source" {
		t.Fatal("retired route changed user source", err)
	}
}
