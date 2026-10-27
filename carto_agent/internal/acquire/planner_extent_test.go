package acquire

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
)

func writeExtentFixture(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "extent.geojson")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

const polygonExtentFixture = `{"type":"Polygon","coordinates":[[[110,30],[111,30],[111,31],[110,31],[110,30]]]}`

func assertExtentBBox(t *testing.T, got, want []float64) {
	t.Helper()
	if len(got) != 4 || len(want) != 4 {
		t.Fatal(got, want)
	}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-10 {
			t.Fatal(got, want)
		}
	}
}

func TestAcquisitionExtentUsesOnlyUnexpandedPolygonMask(t *testing.T) {
	for _, tt := range []struct {
		name, raw      string
		expanded, mask bool
	}{
		{"polygon", polygonExtentFixture, false, true},
		{"expanded polygon", polygonExtentFixture, true, false},
		{"multipolygon", `{"type":"MultiPolygon","coordinates":[[[[110,30],[111,30],[111,31],[110,31],[110,30]]]]}`, false, true},
		{"polygon features", `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":` + polygonExtentFixture + `}]}`, false, true},
		{"line", `{"type":"LineString","coordinates":[[110,30],[111,31]]}`, false, false},
		{"expanded line", `{"type":"LineString","coordinates":[[110,30],[111,31]]}`, true, false},
		{"points", `{"type":"MultiPoint","coordinates":[[110,30],[111,31]]}`, false, false},
		{"mixed features", `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":` + polygonExtentFixture + `},{"type":"Feature","geometry":{"type":"Point","coordinates":[110.5,30.5]}}]}`, false, false},
		{"geometry collection", `{"type":"GeometryCollection","geometries":[` + polygonExtentFixture + `]}`, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AUTOCARTO_HOME", t.TempDir())
			path := writeExtentFixture(t, tt.raw)
			p, e := plannerFixture(t)
			if tt.expanded {
				p.Acquisition.Items[0].ExtentExpression = "「范围」外扩 20%"
			}
			plan, issues := Plan(context.Background(), p, map[string]any{"数据": map[string]any{"$acquire": true}, "范围": path}, e, nil)
			if len(issues) > 0 {
				t.Fatal(issues)
			}
			extent := plan.Items[0].Extent
			if extent == nil || (extent.Path != "") != tt.mask {
				t.Fatalf("%+v", extent)
			}
			if tt.mask && extent.Path != path {
				t.Fatal(extent.Path)
			}
			want := []float64{110, 30, 111, 31}
			if tt.expanded {
				want = []float64{109.8, 29.8, 111.2, 31.2}
			}
			assertExtentBBox(t, extent.BBox, want)
		})
	}
}

func TestRuntimeExtentExpansionDropsOriginalPolygonMask(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	path := writeExtentFixture(t, polygonExtentFixture)
	p, e := plannerFixture(t)
	item := p.Acquisition.Items[0]
	item.Extent = &flow.ExtentPlan{Source: "路线数据集", Pending: true, DependencyStep: "s2", ExpandPercent: 20}
	result, err := ResolveRuntimeExtent(context.Background(), item, path, nil, e, nil)
	if err != nil || result.Extent.Path != "" || result.Extent.Pending || result.Extent.Source != "路线数据集" {
		t.Fatalf("%+v %v", result.Extent, err)
	}
	assertExtentBBox(t, result.Extent.BBox, []float64{109.8, 29.8, 111.2, 31.2})
}

func TestAcquisitionExtentRejectsZeroAreaWithoutInventingBuffer(t *testing.T) {
	for _, raw := range []string{
		`{"type":"Point","coordinates":[110,30]}`,
		`{"type":"LineString","coordinates":[[110,30],[111,30]]}`,
		`{"type":"LineString","coordinates":[[110,30],[110,31]]}`,
	} {
		for _, suffix := range []string{"", "外扩 20%"} {
			t.Run(raw+suffix, func(t *testing.T) {
				t.Setenv("AUTOCARTO_HOME", t.TempDir())
				path := writeExtentFixture(t, raw)
				p, e := plannerFixture(t)
				p.Acquisition.Items[0].ExtentExpression = "「范围」" + suffix
				plan, issues := Plan(context.Background(), p, map[string]any{"数据": map[string]any{"$acquire": true}, "范围": path}, e, nil)
				found := false
				for _, issue := range issues {
					found = found || issue.Code == "FLOW_REQ_EXTENT_INVALID" && strings.Contains(issue.Message, "没有面积")
				}
				if !found || plan.Items[0].Extent != nil {
					t.Fatalf("%+v %+v", plan, issues)
				}
				item := p.Acquisition.Items[0]
				item.Extent = &flow.ExtentPlan{Pending: true, ExpandPercent: 20}
				if _, err := ResolveRuntimeExtent(context.Background(), item, path, nil, e, nil); err == nil || !strings.Contains(err.Error(), "没有面积") {
					t.Fatal(err)
				}
			})
		}
	}
	if got := expandBBox([]float64{110, 30, 110, 30}, 20); got[0] != 110 || got[1] != 30 || got[2] != 110 || got[3] != 30 {
		t.Fatal("invented degree buffer", got)
	}
}

func TestRouteExtentRejectsNonFiniteCoordinates(t *testing.T) {
	for _, text := range []string{"NaN", "Inf", "-Inf", "Infinity"} {
		for _, axis := range []string{"longitude", "latitude"} {
			t.Run(text+axis, func(t *testing.T) {
				dir := t.TempDir()
				row := text + ",30"
				if axis == "latitude" {
					row = "110," + text
				}
				path := filepath.Join(dir, "places.csv")
				if err := os.WriteFile(path, []byte("lon,lat\n110,30\n111,31\n"+row+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := routeExtent(path); err == nil || !strings.Contains(err.Error(), "NaN/Inf") {
					t.Fatal(err)
				}
				if _, err := resolveExtent(context.Background(), dir, nil, nil); err == nil {
					t.Fatal("non-finite route extent accepted")
				}
			})
		}
	}
}

func TestRouteExtentUsesBBoxAndPercentExpansion(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "places.csv"), []byte("lon,lat\n110,30\n,\n111,31\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, e := plannerFixture(t)
	p.Acquisition.Items[0].Extent = &flow.ExtentPlan{Pending: true, Source: "路线数据集", ExpandPercent: 20}
	item, err := ResolveRuntimeExtent(context.Background(), p.Acquisition.Items[0], dir, nil, e, nil)
	if err != nil || item.Extent.Path != "" {
		t.Fatalf("%+v %v", item, err)
	}
	assertExtentBBox(t, item.Extent.BBox, []float64{109.8, 29.8, 111.2, 31.2})
}

func TestInvalidFileCoordinatesDoNotFallBackToResolver(t *testing.T) {
	for _, coordinate := range []string{"NaN", "Infinity", "1e999", "181"} {
		t.Run(coordinate, func(t *testing.T) {
			called := false
			resolver := func(context.Context, string) (*flow.ExtentPlan, error) {
				called = true
				return &flow.ExtentPlan{BBox: []float64{110, 30, 111, 31}}, nil
			}
			path := writeExtentFixture(t, `{"type":"LineString","coordinates":[[110,30],[`+coordinate+`,31]]}`)
			if _, err := resolveExtent(context.Background(), path, nil, resolver); err == nil || called {
				t.Fatalf("invalid coordinate accepted/fell back: %v %v", err, called)
			}
		})
	}
}

func TestExternalExtentValidationAndUnverifiedMask(t *testing.T) {
	for _, tt := range []struct {
		name    string
		box     []float64
		invalid bool
	}{
		{"finite", []float64{110, 30, 111, 31}, false},
		{"nan", []float64{math.NaN(), 30, 111, 31}, true},
		{"infinite", []float64{110, 30, math.Inf(1), 31}, true},
		{"outside wgs84", []float64{180, 30, 181, 31}, true},
		{"reversed", []float64{111, 30, 110, 31}, true},
		{"missing", nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AUTOCARTO_HOME", t.TempDir())
			p, e := plannerFixture(t)
			original := &flow.ExtentPlan{BBox: tt.box, Path: "unverified-layer"}
			resolver := func(context.Context, string) (*flow.ExtentPlan, error) { return original, nil }
			plan, issues := Plan(context.Background(), p, map[string]any{"数据": map[string]any{"$acquire": true}, "范围": "test-layer"}, e, resolver)
			found := false
			for _, issue := range issues {
				found = found || issue.Code == "FLOW_REQ_EXTENT_INVALID"
			}
			if found != tt.invalid {
				t.Fatalf("%+v %+v", plan, issues)
			}
			if !tt.invalid && plan.Items[0].Extent.Path != "" {
				t.Fatal("unverified mask retained")
			}
			if original.Path != "unverified-layer" {
				t.Fatal("resolver-owned extent mutated")
			}
			if _, err := json.Marshal(plan); err != nil {
				t.Fatal("invalid float leaked into response", err)
			}
		})
	}
}

func TestRouteExtentWithNoAreaRequiresExplicitRegion(t *testing.T) {
	for _, rows := range []string{"110,30\n", "110,30\n111,30\n", "110,30\n110,31\n"} {
		t.Run(strings.TrimSpace(rows), func(t *testing.T) {
			t.Setenv("AUTOCARTO_HOME", t.TempDir())
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "places.csv"), []byte("lon,lat\n"+rows), 0600); err != nil {
				t.Fatal(err)
			}
			p, e := plannerFixture(t)
			item := p.Acquisition.Items[0]
			item.Extent = &flow.ExtentPlan{Pending: true, ExpandPercent: 20}
			if _, err := ResolveRuntimeExtent(context.Background(), item, dir, nil, e, nil); err == nil || !strings.Contains(err.Error(), "有面积的区域或边界") {
				t.Fatal(err)
			}
		})
	}
}

func TestProjectedGeoJSONMayResolveWGS84BBoxWithoutUsingOriginalMask(t *testing.T) {
	path := writeExtentFixture(t, `{"type":"Polygon","crs":{"type":"name","properties":{"name":"EPSG:3857"}},"coordinates":[[[100000,100000],[200000,100000],[200000,200000],[100000,100000]]]}`)
	called := false
	resolver := func(context.Context, string) (*flow.ExtentPlan, error) {
		called = true
		return &flow.ExtentPlan{BBox: []float64{1, 1, 2, 2}, Path: path}, nil
	}
	extent, err := resolveExtent(context.Background(), path, nil, resolver)
	if err != nil || !called || extent.Path != "" {
		t.Fatal(extent, err, called)
	}
	assertExtentBBox(t, extent.BBox, []float64{1, 1, 2, 2})
}
