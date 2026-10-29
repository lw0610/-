package acquire

import (
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"math"
	"testing"
)

func plannerFixture(t *testing.T) datasource.PlanningEnvironment {
	t.Helper()
	catalog, err := datasource.Load("../../../autocarto/tools/system/data-acquisition/assets", "")
	if err != nil {
		t.Fatal(err)
	}
	return datasource.PlanningEnvironment{Catalog: catalog, Integrations: map[string]bool{"web": true, "osm": true}, MaxDownloadMB: 2048}
}
func TestSourceRankingDeterministicRegionalAndNationwide(t *testing.T) {
	env := plannerFixture(t)
	for _, test := range []struct{ name, source string }{{"郑州市", "source.osm.overpass"}, {"全国", "source.naturalearth.10m"}} {
		extent, err := ResolveExtent(context.Background(), test.name, env.Catalog, nil)
		if err != nil {
			t.Fatal(err)
		}
		item := datasource.AcquisitionItem{Key: "铁路", Category: "transport.railway", Geometry: "line", Acquirable: true, Required: true, Extent: extent, Scale: EstimateScale(extent, nil)}
		first := env.Rank(item)
		second := env.Rank(item)
		if first.Chosen == nil || first.Chosen.SourceID != test.source {
			t.Fatalf("%+v", first)
		}
		a, _ := json.Marshal(first)
		b, _ := json.Marshal(second)
		if string(a) != string(b) {
			t.Fatal("non deterministic")
		}
	}
	env.Integrations["osm"] = false
	extent, _ := ResolveExtent(context.Background(), "郑州市", env.Catalog, nil)
	got := env.Rank(datasource.AcquisitionItem{Key: "铁路", Category: "transport.railway", Geometry: "line", Acquirable: true, Required: true, Extent: extent})
	if got.Chosen != nil && got.Chosen.SourceID == "source.osm.overpass" {
		t.Fatalf("disabled source available: %+v", got)
	}
}
func TestEstimatedScaleUsesPaperWidth(t *testing.T) {
	scale := EstimateScale(&flow.ExtentPlan{BBox: []float64{100, 0, 100 + 2000000.0/111320, 1}}, map[string]any{"纸张": "A1 横向"})
	if math.Abs(float64(scale)-3000000) > 450000 {
		t.Fatal(scale)
	}
}

func TestV7SemanticCacheIgnoresDisplayNameAndUsesGrid(t *testing.T) {
	env := plannerFixture(t)
	source := env.Catalog.Source("source.osm.geofabrik-china")
	provide := source.Provides[0]
	choice := flow.SourceChoice{SourceID: source.ID, Provide: provide}
	item := flow.AcquisitionItem{Key: "旧名字", Filename: "old.gpkg", Category: provide.Category, Geometry: "line", Scale: 1200000, Extent: &flow.ExtentPlan{BBox: []float64{110.12, 30.12, 111.12, 31.12}}}
	first, err := cacheSpecification(env.Catalog, item, choice, *source)
	if err != nil {
		t.Fatal(err)
	}
	item.Key = "新名字"
	item.Filename = "new.gpkg"
	item.Scale = 1500000
	item.Extent.BBox = []float64{110.2, 30.2, 111.2, 31.2}
	second, err := cacheSpecification(env.Catalog, item, choice, *source)
	if err != nil {
		t.Fatal(err)
	}
	if first.Key() != second.Key() {
		t.Fatal("same semantic/grid/scale class missed cache")
	}
	if first.BBox[0] != 110 || first.BBox[2] != 111.5 {
		t.Fatalf("grid not aligned: %v", first.BBox)
	}
}
