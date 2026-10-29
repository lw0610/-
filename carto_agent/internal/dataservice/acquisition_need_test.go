package dataservice

import (
	"context"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/stats"
	"os"
	"path/filepath"
	"testing"
)

func TestV7RegisterStatisticsWithoutFrozenQueryAndReuse(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	s, _ := fixture(t)
	path := filepath.Join(s.options.SessionDir, "output", "population.csv")
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("region_name,value,year,unit\n甲市,10,2023,万人\n乙市,20,2023,万人\n"), 0600)
	total := 30.0
	need := AcquisitionNeed{ID: "plan-1/population", Need: Need{Name: "人口表"}, Item: datasource.AcquisitionItem{Category: "stats.population"}, Statistics: &stats.Need{RegionCode: "fixture", Region: "夹具省", Level: "市", Year: 2023, Indicator: "常住人口", Caliber: "常住", Unit: "万人", Total: &total, RoundingUnit: 1}, ExpectedUnits: []stats.Unit{{Code: "1", Name: "甲市"}, {Code: "2", Name: "乙市"}}}
	ctx := WithAcquisitionNeed(context.Background(), need)
	registered, err := s.Register(ctx, RegisterRequest{Path: path, URL: "https://example.org/fixture", SourceTitle: "synthetic test fixture", TableRef: "table 1", Method: "html"})
	if err != nil {
		t.Fatal(err)
	}
	if registered.Status != "completed" || len(registered.Rows) != 1 {
		t.Fatalf("%+v", registered)
	}
	if _, err = s.RegisteredNeed(ctx, need.ID); err != nil {
		t.Fatal(err)
	}
	need.ID = "plan-2/renamed"
	need.Need.Name = "换个名字的人口"
	hit, ok, err := s.LookupStatistics(ctx, need)
	if err != nil || !ok || hit.Rows[0].Paths[0] != registered.Rows[0].Paths[0] {
		t.Fatalf("semantic reuse failed: %+v %v %v", hit, ok, err)
	}
	if _, err = s.RegisteredNeed(ctx, need.ID); err != nil {
		t.Fatal(err)
	}
}
