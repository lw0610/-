package acquire

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/geocoding"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTiandituCountySelectionFallbackAndNoKeyLeak(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	t.Setenv("AUTOCARTO_DISABLE_KEYRING", "1")
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("tk") != "test-secret" {
			t.Error("missing key")
		}
		var post map[string]any
		json.Unmarshal([]byte(r.URL.Query().Get("postStr")), &post)
		if post["specify"] == "156410101" {
			fmt.Fprint(w, `{"status":{"infocode":"1000"},"resultType":"1","count":"2","pois":[{"name":"甲县人民政府","countyCode":"156410999","lonlat":"110.5,30.5"},{"name":"甲县人民政府","countyCode":"156410101","lonlat":"110.6,30.6","hotPointID":"actual"}]}`)
		} else {
			fmt.Fprint(w, `{"status":{"infocode":"1000"},"resultType":"1","count":"1","pois":[{"name":"乙县人民政府","countyCode":"156410102","lonlat":"0,0"}]}`)
		}
	}))
	defer server.Close()
	caller := func(_ context.Context, name string, args map[string]any) (any, error) {
		features := []map[string]any{}
		for i, label := range []string{"甲县", "乙县"} {
			x := 110 + float64(i)
			geometry := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{x, 30}, {x + 1, 30}, {x + 1, 31}, {x, 31}, {x, 30}}}}
			if name == "data_derive" {
				geometry = map[string]any{"type": "Point", "coordinates": []float64{x + .5, 30.5}}
			}
			features = append(features, map[string]any{"type": "Feature", "properties": map[string]any{"name": label, "adcode": fmt.Sprintf("41010%d", i+1)}, "geometry": geometry})
		}
		raw, _ := json.Marshal(map[string]any{"type": "FeatureCollection", "features": features})
		path := args["output"].(string)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "output": path}, nil
	}
	a := TiandituAdapter{Invoke: caller, Resolver: &geocoding.Resolver{Key: "test-secret", Endpoint: server.URL, Client: server.Client()}}
	r, e := a.Fetch(context.Background(), FetchRequest{WorkDir: t.TempDir(), Item: flow.AcquisitionItem{Category: "settlement.county_seat", Params: map[string]string{"within": "示例省"}}, Extent: Extent{BBox: []float64{110, 30, 112, 31}}})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(r.Files[0])
	var doc struct {
		Features []struct {
			Properties map[string]any `json:"properties"`
		} `json:"features"`
	}
	json.Unmarshal(raw, &doc)
	if len(doc.Features) != 2 || doc.Features[0].Properties["approximate"] != false || doc.Features[1].Properties["approximate"] != true || hits.Load() != 2 {
		t.Fatal(string(raw), hits.Load())
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(raw)+string(encoded), "test-secret") {
		t.Fatal("key leaked")
	}
}
func TestTiandituTileDefinitionHasNoCredential(t *testing.T) {
	r, e := (TiandituAdapter{}).Fetch(context.Background(), FetchRequest{Item: flow.AcquisitionItem{Category: "basemap.tiles"}})
	if e != nil || r.Format != "online_layer" || r.LayerDefinition["secret_ref"] != "tianditu" || r.LayerDefinition["requires_project_key_confirmation"] != true || len(r.Files) > 0 {
		t.Fatal(r, e)
	}
}
func TestGEEAdapterUsesShippedTemplateAndSafeTypedParameters(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "acquired.tif")
	os.WriteFile(path, []byte("fixture"), 0600)
	called := false
	a := GEEAdapter{AssetsRoot: filepath.Join(root, "autocarto"), Invoke: func(_ context.Context, name string, args map[string]any) (any, error) {
		called = true
		if name != "gee_run_script" || args["template"] != "optical.py" || args["code"] != nil {
			t.Fatal(args)
		}
		p := args["params"].(map[string]any)
		if p["cloud_max"] != float64(10) || p["scale"] != float64(20) {
			t.Fatal(p)
		}
		return map[string]any{"success": true, "artifact_paths": []string{path, filepath.Join(dir, "script.py")}}, nil
	}}
	request := FetchRequest{WorkDir: dir, Item: flow.AcquisitionItem{Category: "imagery.optical", Params: map[string]string{"cloud_max": "10", "scale": "20"}}, Provide: &datasource.Provide{Template: "optical.py", Dataset: "COPERNICUS/S2_SR_HARMONIZED", Defaults: map[string]string{"bands": "B4,B3,B2", "scale": "10", "start": "2025-01-01", "end": "2026-01-01", "cloud_max": "20"}}, Extent: Extent{BBox: []float64{110, 30, 111, 31}}}
	result, e := a.Fetch(context.Background(), request)
	if e != nil || !called || len(result.Files) != 1 || result.Bytes != 7 {
		t.Fatal(result, e)
	}
	request.Provide.Template = "../../script.py"
	if _, e = a.Fetch(context.Background(), request); e == nil {
		t.Fatal("arbitrary template")
	}
	request.Provide.Template = "optical.py"
	request.Item.Params["dataset"] = "evil"
	if _, e = a.Fetch(context.Background(), request); e == nil {
		t.Fatal("dataset override")
	}
	delete(request.Item.Params, "dataset")
	request.Item.Params["scale"] = "NaN"
	if _, e = a.Fetch(context.Background(), request); e == nil {
		t.Fatal("invalid scale")
	}
}
