package datasource

import (
	"path/filepath"
	"testing"
)

func TestV7RoutingStableAndAreaBounded(t *testing.T) {
	root := filepath.Join("..", "..", "..", "autocarto", "tools", "system", "data-acquisition", "assets")
	catalog, err := Load(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	environment := PlanningEnvironment{Catalog: catalog, Integrations: map[string]bool{"osm": true, "web": true, "gee": true, "browser": true}, Secrets: map[string]bool{}, AutoAcquire: "allow", MaxDownloadMB: 2048}
	for _, test := range []struct {
		name, category string
		scale          int
		bbox           []float64
		want           string
	}{
		{"湖南河流", "hydro.river_line", 2200000, []float64{108.7, 24.6, 114.2, 30.2}, "osm_extract"},
		{"湖南铁路", "transport.railway", 2200000, []float64{108.7, 24.6, 114.2, 30.2}, "osm_extract"},
		{"河南高速", "transport.road.expressway", 2340000, []float64{110.3, 31.4, 116.7, 36.4}, "osm_extract"},
		{"郑州地铁", "transport.metro", 370000, []float64{112.7, 34.3, 114.2, 35.0}, "overpass"},
		{"全国铁路", "transport.railway", 14000000, []float64{73, 18, 135, 54}, "ne"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for i := 0; i < 3; i++ {
				item := environment.Rank(AcquisitionItem{Category: test.category, Acquirable: true, Scale: test.scale, Extent: &ExtentPlan{BBox: test.bbox}, Params: map[string]string{}})
				if item.Chosen == nil {
					t.Fatalf("no route: %+v", item.Excluded)
				}
				source := catalog.Source(item.Chosen.SourceID)
				if source.RoutingMethod() != test.want {
					t.Fatalf("got %s want %s", source.RoutingMethod(), test.want)
				}
			}
		})
	}
}
