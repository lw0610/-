package api

import (
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/layers"
	"github.com/gorilla/mux"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinGuidesParseAndResolve(t *testing.T) {
	a, _, _, _ := realFlowAPI(t)
	catalog, err := layers.NewDefaultLibrary()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ name, geometry, category, level string }{{"省域", "polygon", "admin.division", "省"}, {"地区界线", "line", "admin.boundary_line", "省"}, {"河流线", "line", "hydro.river_line", ""}, {"河流面", "polygon", "hydro.water_polygon", ""}, {"公路", "line", "transport.road.major", ""}, {"国道", "line", "transport.road.national", ""}, {"铁路", "line", "transport.railway", ""}, {"县级城市驻地", "point", "settlement.county_seat", ""}} {
		file := filepath.Join(t.TempDir(), entry.name+".geojson")
		os.WriteFile(file, []byte(`{"type":"FeatureCollection","features":[]}`), 0600)
		_, err = catalog.Save(layers.Manifest{APIVersion: "layer/v1", Name: entry.name, Pack: "China_map 测试夹具", Category: entry.category, Params: map[string]string{"level": entry.level}, Geometry: entry.geometry, File: file, CRS: "EPSG:4326"})
		if err != nil {
			t.Fatal(err)
		}
	}
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
	for _, id := range []string{"marching-route", "statistical-choropleth"} {
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
