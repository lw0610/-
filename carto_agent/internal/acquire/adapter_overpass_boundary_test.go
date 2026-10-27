package acquire

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/osmdata"
)

func TestOverpassAdapterPrefersOriginalBoundary(t *testing.T) {
	root := t.TempDir()
	boundary := filepath.Join(root, "region.geojson")
	if err := os.WriteFile(boundary, []byte(`{"type":"Polygon","coordinates":[[[110,30],[111,30],[110.5,31],[110,30]]]}`), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "lines.geojson")
	if err := os.WriteFile(output, []byte(`{"type":"FeatureCollection","features":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		path   string
		expand float64
		want   string
	}{
		{"verified polygon", boundary, 0, "file:" + boundary},
		{"rectangle only", "", 0, "bbox:30.000000,110.000000,31.000000,111.000000"},
		{"expanded region", boundary, 10, "bbox:30.000000,110.000000,31.000000,111.000000"},
		{"other boundary format", filepath.Join(root, "region.gpkg"), 0, "bbox:30.000000,110.000000,31.000000,111.000000"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			adapter := OverpassAdapter{Invoke: func(_ context.Context, name string, args map[string]any, _ osmdata.Options) (*osmdata.Result, error) {
				calls = append(calls, name)
				extents := args["extents"].([]string)
				if len(extents) != 1 || extents[0] != test.want {
					t.Fatalf("unexpected spatial selection: %+v", args)
				}
				if name == "osm_probe" {
					return &osmdata.Result{Success: true, Data: []map[string]any{{"counts": map[string]int{"total": 1}}}}, nil
				}
				return &osmdata.Result{Success: true, ArtifactPaths: []string{output}}, nil
			}}
			result, err := adapter.Fetch(context.Background(), FetchRequest{Source: &datasource.Source{}, Provide: &datasource.Provide{Tags: []string{"highway=motorway"}, Types: "way"}, Item: flow.AcquisitionItem{Extent: &flow.ExtentPlan{ExpandPercent: test.expand}}, Extent: Extent{BBox: []float64{110, 30, 111, 31}, Path: test.path}, WorkDir: root})
			if err != nil || strings.Join(calls, ",") != "osm_probe,osm_get_features" || !strings.Contains(strings.Join(result.Notes, " "), "尚未裁剪") {
				t.Fatalf("unexpected fetch: %+v, %v, %v", result, calls, err)
			}
		})
	}
}

func TestOverpassAdapterRejectsInvalidBoundaryBeforeNetwork(t *testing.T) {
	root := t.TempDir()
	for _, raw := range []string{`{"type":"Point","coordinates":[110,30]}`, `{"type":"Polygon","crs":{"properties":{"name":"EPSG:3857"}},"coordinates":[[[110,30],[111,30],[111,31],[110,30]]]}`, "not json"} {
		path := filepath.Join(root, "invalid.geojson")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		adapter := OverpassAdapter{Invoke: func(context.Context, string, map[string]any, osmdata.Options) (*osmdata.Result, error) {
			t.Fatal("invalid boundary silently widened to bbox")
			return nil, nil
		}}
		_, err := adapter.Fetch(context.Background(), FetchRequest{Source: &datasource.Source{}, Provide: &datasource.Provide{}, Extent: Extent{BBox: []float64{110, 30, 111, 31}, Path: path}})
		if err == nil || !strings.Contains(err.Error(), "未退回") {
			t.Fatalf("accepted invalid boundary: %v", err)
		}
	}
}

func TestOverpassAdapterDoesNotReplaceOversizedPolygonWithCityBBoxes(t *testing.T) {
	boundary := filepath.Join(t.TempDir(), "region.geojson")
	if err := os.WriteFile(boundary, []byte(`{"type":"Polygon","coordinates":[[[110,30],[111,30],[110.5,31],[110,30]]]}`), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	adapter := OverpassAdapter{Invoke: func(_ context.Context, name string, args map[string]any, _ osmdata.Options) (*osmdata.Result, error) {
		calls++
		if name != "osm_probe" || args["extents"].([]string)[0] != "file:"+boundary {
			t.Fatalf("polygon constraint lost: %s %+v", name, args)
		}
		return &osmdata.Result{Success: true, Data: []map[string]any{{"counts": map[string]int{"total": 30001}}}}, nil
	}}
	_, err := adapter.Fetch(context.Background(), FetchRequest{Source: &datasource.Source{}, Provide: &datasource.Provide{Tags: []string{"highway"}, Types: "way"}, Extent: Extent{BBox: []float64{110, 30, 111, 31}, Path: boundary}})
	if err == nil || !strings.Contains(err.Error(), "未退回行政区外接矩形") || calls != 1 {
		t.Fatalf("oversized polygon widened or downloaded: %v, %d calls", err, calls)
	}
}
