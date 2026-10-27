package acquire

import (
	"context"
	"github.com/autocarto/carto_agent/internal/datasource"
	"testing"
)

func TestPlanGEERequiredDefaultsAndExplicitYear(t *testing.T) {
	for _, tt := range []struct {
		name, category string
		params         map[string]string
		wantYear       int
	}{
		{"DEM without params", "terrain.dem", nil, 0},
		{"population explicit year", "population.grid", map[string]string{"year": "2019"}, 2019},
		{"population required year from default", "population.grid", nil, 2020},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, e := plannerFixture(t)
			e.Integrations["gee"] = true
			original := &p.Acquisition.Items[0]
			original.Category = tt.category
			original.Geometry = "raster"
			original.Params = tt.params
			if tt.category == "population.grid" {
				e.Catalog.Category(tt.category).Params = map[string]datasource.Parameter{"year": {Required: true, Values: []string{"2019", "2020"}}}
			}
			result, issues := Plan(context.Background(), p, map[string]any{"数据": map[string]any{"$acquire": true}, "范围": "郑州市"}, e, nil)
			if len(issues) > 0 {
				t.Fatal(issues)
			}
			item := result.Items[0]
			if !item.Available || item.Chosen == nil || item.Chosen.SourceID != "source.gee.catalog" {
				t.Fatalf("%+v", item)
			}
			params, err := geeParameters(FetchRequest{Provide: &item.Chosen.Provide, Item: item, Extent: Extent{BBox: item.Extent.BBox}})
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantYear > 0 && params["year"] != tt.wantYear {
				t.Fatal(params)
			}
			if tt.params == nil && len(item.Params) > 0 {
				t.Fatal("source defaults leaked into request", item.Params)
			}
			spec, err := cacheSpecification(e.Catalog, item, *item.Chosen, *e.Catalog.Source(item.Chosen.SourceID))
			if err != nil || spec.Params["scale"] == "" {
				t.Fatalf("defaults missing from cache spec %+v %v", spec, err)
			}
		})
	}
}
