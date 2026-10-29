package acquire

import (
	"context"
	"errors"
	"fmt"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestPipelineFallbackCacheAndRawContainment(t *testing.T) {
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			http.Error(w, "failed", 500)
			return
		}
		downloads.Add(1)
		_, _ = w.Write([]byte("synthetic"))
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	catalog := &datasource.Catalog{Categories: []datasource.Category{{ID: "transport.railway", Geometry: "line", Validate: datasource.Validation{MinFeatures: 1}}}, Sources: []datasource.Source{{ID: "source.fail", Name: "失败", Kind: "http_file", Hosts: []string{u.Host}, Version: "1"}, {ID: "source.good", Name: "成功", Kind: "http_file", Hosts: []string{u.Host}, Version: "1", Attribution: "synthetic fixture"}}}
	normalizes := 0
	engine := Engine{Catalog: catalog, Store: NewStore(t.TempDir(), 1024*1024), Config: config.AcquisitionConfig{}.WithDefaults(), Adapters: map[string]Adapter{"http_file": HTTPAdapter{Client: server.Client(), MaxBytes: 1024}}, Invoke: func(_ context.Context, name string, args map[string]any) (any, error) {
		if name != "data_normalize" {
			return nil, fmt.Errorf("unexpected %s", name)
		}
		normalizes++
		input := args["input"].(map[string]any)["path"].(string)
		output := args["output"].(string)
		if err := copyReadonly(input, output); err != nil {
			return nil, err
		}
		bbox := args["clip"].(map[string]any)["bbox"]
		return map[string]any{"success": true, "output": output, "data": map[string]any{"count": 1, "geometry_type": "MultiLineString", "crs": "EPSG:4326", "extent_wgs84": bbox, "paths": []string{output}, "fields": map[string]any{}}}, nil
	}}
	good := flow.SourceChoice{SourceID: "source.good", Tier: 5, Provide: datasource.Provide{Category: "transport.railway", URL: server.URL + "/rail.geojson"}}
	item := flow.AcquisitionItem{Key: "铁路", Requirement: "铁路", Category: "transport.railway", Mode: "auto", Geometry: "line", Required: true, Fields: map[string]string{}, Extent: &flow.ExtentPlan{BBox: []float64{100, 20, 120, 40}}, Chosen: &flow.SourceChoice{SourceID: "source.fail", Tier: 5, Provide: datasource.Provide{URL: server.URL + "/fail"}}, Alternatives: []flow.SourceChoice{good}}
	result, err := engine.RunItem(context.Background(), item, nil)
	if err != nil || result.Status != "completed" || len(result.Attempts) != 2 || len(result.Output.Paths) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err := os.Stat(result.Output.Paths[0]); err != nil {
		t.Fatal("published output missing", err)
	}
	item.Chosen = &good
	item.Alternatives = nil
	repeated, err := engine.RunItem(context.Background(), item, nil)
	if err != nil || repeated.Status != "reused" || downloads.Load() != 1 {
		t.Fatalf("%+v %v %d", repeated, err, downloads.Load())
	}
	item.Extent = &flow.ExtentPlan{BBox: []float64{110, 30, 111, 31}}
	item.Scale = 250000
	smaller, err := engine.RunItem(context.Background(), item, nil)
	if err != nil || smaller.Status != "completed" || downloads.Load() != 1 || normalizes != 5 {
		t.Fatalf("%+v %v downloads=%d normalize=%d", smaller, err, downloads.Load(), normalizes)
	}
	if filepath.Dir(smaller.Output.Paths[0]) == filepath.Dir(result.Output.Paths[0]) {
		t.Fatal("new normalization overwrote previous")
	}
	conversionFailure := fmt.Errorf("fixture conversion failure")
	engine.Invoke = func(context.Context, string, map[string]any) (any, error) { return nil, conversionFailure }
	item.Params = map[string]string{"variant": "new-processing"}
	item.Alternatives = []flow.SourceChoice{{SourceID: "source.fail", Provide: datasource.Provide{URL: server.URL + "/fail"}}}
	failed, err := engine.RunItem(context.Background(), item, nil)
	var processing *ProcessingError
	if !errors.As(err, &processing) || !errors.Is(err, conversionFailure) || len(failed.Attempts) != 1 || downloads.Load() != 1 {
		t.Fatalf("local conversion triggered another download/source: %+v %v downloads=%d", failed, err, downloads.Load())
	}
	if len(processing.Paths) != 1 {
		t.Fatalf("original path missing: %+v", processing)
	}
	if _, err := os.Stat(processing.Paths[0]); err != nil {
		t.Fatalf("original was not retained: %v", err)
	}
}
func TestValidateStatsDoesNotAcceptEmptyOrWrongFields(t *testing.T) {
	category := &datasource.Category{Geometry: "point", Validate: datasource.Validation{MinFeatures: 1, NameFillRatio: .95}}
	item := flow.AcquisitionItem{Fields: map[string]string{"NAME": "name"}, Extent: &flow.ExtentPlan{BBox: []float64{100, 20, 101, 21}}}
	stats := map[string]any{"count": 0, "geometry_type": "Polygon", "crs": "", "fields": map[string]float64{}, "extent_wgs84": []float64{110, 30, 111, 31}}
	result := ValidateStats(category, item, stats)
	if result.OK || len(result.Errors) < 4 {
		t.Fatalf("%+v", result)
	}
}
