package api

import (
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/gorilla/mux"
	"net/http/httptest"
	"testing"
)

func TestBuiltinGuidesParseAndResolve(t *testing.T) {
	a, _, _, _ := realFlowAPI(t)
	library, err := a.resourceStore()
	if err != nil {
		t.Fatal(err)
	}
	store, err := flow.NewDefaultGuideStore(library)
	if err != nil {
		t.Fatal(err)
	}
	a.SetGuideStore(store)
	router := mux.NewRouter()
	a.SetupRoutes(router)
	results := []map[string]any{}
	for _, id := range []string{"marching-route", "statistical-choropleth", "traffic-route", "data-to-map"} {
		snapshot, err := store.Resolve(context.Background(), id, library)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range snapshot.Doc.Diagnostics {
			if d.Level != "info" {
				t.Errorf("%s: %+v", id, d)
			}
		}
		for _, r := range snapshot.Scope.Linked {
			for _, d := range r.Problems {
				if d.Level == "error" {
					t.Errorf("%s resource: %+v", id, d)
				}
			}
		}
		req := httptest.NewRequest("GET", "/api/guides/"+id, nil)
		req.Header.Set("X-AutoCarto-Contract-Version", "v1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", id, w.Code, w.Body.String())
		}
		var response struct {
			Data flow.StoredGuide `json:"data"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Data.Guide == nil || response.Data.Meta.ID != id {
			t.Fatal("missing guide detail")
		}
		results = append(results, map[string]any{"id": id, "references": len(snapshot.Doc.Resources), "body_bytes": len(snapshot.Doc.Body), "diagnostics": snapshot.Doc.Diagnostics})
	}
	encoded, _ := json.Marshal(results)
	t.Log("BUILTIN_GUIDE_RESULT=" + string(encoded))
}
