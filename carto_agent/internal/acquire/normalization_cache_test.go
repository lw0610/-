package acquire

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
)

func TestFieldFallbackRevisionRebuildsProcessedCacheFromRaw(t *testing.T) {
	ctx := context.Background()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "autocarto", "tests", "fixtures", "metro-stations-osm.geojson"))
	if err != nil {
		t.Fatal(err)
	}
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		http.Error(w, "the original is already cached", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	source := datasource.Source{ID: "source.osm.overpass", Kind: "http_file", Version: "1.0.0"}
	provide := datasource.Provide{Category: "transport.metro_station", URL: server.URL + "/features.geojson", Fields: map[string]datasource.Strings{"name": {"name:zh", "name"}}}
	choice := flow.SourceChoice{SourceID: source.ID, Provide: provide}
	item := flow.AcquisitionItem{Key: "地铁站点", Requirement: "地铁站点", Category: provide.Category, Mode: "auto", Geometry: "point", Required: true, Fields: map[string]string{"name": "name"}, Scale: 25000, Extent: &flow.ExtentPlan{BBox: []float64{112.6, 34.2, 114.3, 35.1}}, Chosen: &choice}
	catalog := &datasource.Catalog{Categories: []datasource.Category{{ID: item.Category, Geometry: "point", Validate: datasource.Validation{MinFeatures: 1}}}, Sources: []datasource.Source{source}}
	store := NewStore(t.TempDir(), 2<<20)
	currentSpec, err := cacheSpecification(catalog, item, choice, source)
	if err != nil {
		t.Fatal(err)
	}
	oldSpec := currentSpec
	oldSpec.Normalize = map[string]any{}
	for key, value := range currentSpec.Normalize.(map[string]any) {
		if key != "revision" {
			oldSpec.Normalize.(map[string]any)[key] = value
		}
	}
	if oldSpec.Key() == currentSpec.Key() || oldSpec.baseKey() != currentSpec.baseKey() {
		t.Fatal("processing revision must change processed key while preserving the raw key")
	}
	oldOutput := []byte(`{"count":240,"named_features":93}`)
	old, _, err := store.Materialize(ctx, oldSpec, 0, func(dir string) (Manifest, error) {
		if err := os.WriteFile(filepath.Join(dir, "raw", "features.geojson"), raw, 0600); err != nil {
			return Manifest{}, err
		}
		if err := os.WriteFile(filepath.Join(dir, "out", "old-stations.gpkg"), oldOutput, 0600); err != nil {
			return Manifest{}, err
		}
		return Manifest{RawFiles: []string{"raw/features.geojson"}, OutputFiles: []string{"out/old-stations.gpkg"}, Metadata: map[string]any{"fetch": FetchResult{Format: "geojson"}, "normalize": map[string]any{"count": 240, "fields": map[string]any{"name": float64(93) / 240}}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.FindSemantic(ctx, item.Category, item.Params, currentSpec.ScaleClass, item.Extent.BBox, "")
	if err != nil || len(before) != 0 {
		t.Fatalf("old 93-name processed result remained a semantic candidate: %+v %v", before, err)
	}
	containing, ok, err := store.FindContaining(ctx, currentSpec)
	if err != nil || !ok || containing.Key != old.Key {
		t.Fatalf("old original is no longer reusable: %+v %v", containing, err)
	}
	normalizes := 0
	// This boundary fake tests the production cache/Engine path. Actual QGIS
	// candidate fallback is independently covered with this same raw fixture in
	// test_real_metro_raw_keeps_all_239_available_station_names.
	engine := Engine{Catalog: catalog, Store: store, Config: config.AcquisitionConfig{}.WithDefaults(), Adapters: map[string]Adapter{"http_file": HTTPAdapter{Client: server.Client()}}, Invoke: func(_ context.Context, tool string, args map[string]any) (any, error) {
		if tool != "data_normalize" {
			return nil, fmt.Errorf("unexpected tool %s", tool)
		}
		input := args["input"].(map[string]any)["path"].(string)
		contents, err := os.ReadFile(input)
		if err != nil || !bytes.Equal(contents, raw) {
			t.Fatalf("normalizer received stale processed data instead of reusable raw/new output: %s %v", input, err)
		}
		normalizes++
		output := args["output"].(string)
		if err := os.WriteFile(output, contents, 0600); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "data": map[string]any{"path": output, "count": 240, "geometry_type": "MultiPoint", "crs": "EPSG:4326", "extent_wgs84": args["clip"].(map[string]any)["bbox"], "fields": map[string]any{"name": float64(239) / 240}}}, nil
	}}
	first, err := engine.RunItem(ctx, item, nil)
	if err != nil || first.Status != "completed" || first.StoreKey != currentSpec.Key() || downloads.Load() != 0 || normalizes != 2 {
		t.Fatalf("raw-cache rebuild failed: %+v %v downloads=%d normalizes=%d", first, err, downloads.Load(), normalizes)
	}
	after, err := store.FindSemantic(ctx, item.Category, item.Params, currentSpec.ScaleClass, item.Extent.BBox, "")
	if err != nil || len(after) != 1 || after[0].Key != currentSpec.Key() {
		t.Fatalf("new processed result was not the sole reusable semantic candidate: %+v %v", after, err)
	}
	second, err := engine.RunItem(ctx, item, nil)
	if err != nil || second.Status != "reused" || second.StoreKey != first.StoreKey || downloads.Load() != 0 || normalizes != 3 {
		t.Fatalf("new processed result was not reused: %+v %v downloads=%d normalizes=%d", second, err, downloads.Load(), normalizes)
	}
	if contents, err := os.ReadFile(old.Outputs()[0]); err != nil || !bytes.Equal(contents, oldOutput) {
		t.Fatalf("old cached artifact was deleted or rewritten: %v", err)
	}
	if _, ok, err := store.Lookup(ctx, old.Key); err != nil || !ok {
		t.Fatalf("old record must remain readable for historical provenance: %v", err)
	}
}

func TestSingleFieldCachesKeepTheirExistingKeysAndSemanticReuse(t *testing.T) {
	ctx := context.Background()
	source := datasource.Source{ID: "source.fixture", Version: "1"}
	provide := datasource.Provide{Category: "transport.railway", Fields: map[string]datasource.Strings{"name": {"name"}}}
	choice := flow.SourceChoice{SourceID: source.ID, Provide: provide}
	item := flow.AcquisitionItem{Category: provide.Category, Geometry: "line", Fields: map[string]string{"name": "name"}, Extent: &flow.ExtentPlan{BBox: []float64{100, 20, 120, 40}}}
	catalog := &datasource.Catalog{Categories: []datasource.Category{{ID: item.Category, Geometry: "line"}}}
	spec, err := cacheSpecification(catalog, item, choice, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, changed := spec.Normalize.(map[string]any)["revision"]; changed {
		t.Fatal("single-field caches are not affected by candidate fallback")
	}
	store := NewStore(t.TempDir(), 4096)
	record, _, err := store.Materialize(ctx, spec, 0, fixtureBuild(t, "unaffected cached data"))
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := store.FindSemantic(ctx, item.Category, nil, spec.ScaleClass, item.Extent.BBox, "")
	if err != nil || len(candidates) != 1 || candidates[0].Key != record.Key {
		t.Fatalf("unaffected cache lost semantic reuse: %+v %v", candidates, err)
	}
}
