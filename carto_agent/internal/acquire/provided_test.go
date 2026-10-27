package acquire

import (
	"context"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"os"
	"path/filepath"
	"testing"
)

func TestProvidedFileValidationRejectsWrongGeometryAndPreservesOriginal(t *testing.T) {
	file := filepath.Join(t.TempDir(), "user.geojson")
	original := []byte("original bytes")
	_ = os.WriteFile(file, original, 0600)
	before, _ := os.Stat(file)
	item := flow.AcquisitionItem{Key: "河流", Category: "water.river_line", Fields: map[string]string{"NAME": "name"}, Extent: &flow.ExtentPlan{BBox: []float64{100, 20, 101, 21}}}
	geometry := "Polygon"
	names := map[string]float64{"NAME": 1}
	engine := Engine{Store: NewStore(t.TempDir(), 1024*1024), Catalog: &datasource.Catalog{Categories: []datasource.Category{{ID: item.Category, Geometry: "line", Validate: datasource.Validation{MinFeatures: 1, NameFillRatio: .95}}}}, Invoke: func(_ context.Context, name string, args map[string]any) (any, error) {
		if name != "data_normalize" || len(args["fields"].(map[string][]string)) != 0 {
			t.Fatal("validation must not invent/rename original fields")
		}
		if err := os.WriteFile(args["output"].(string), []byte("temporary"), 0600); err != nil {
			return nil, err
		}
		return map[string]any{"data": map[string]any{"count": 1, "geometry_type": geometry, "crs": "EPSG:4326", "fields": names, "extent_wgs84": []float64{100, 20, 101, 21}}}, nil
	}}
	if _, err := engine.ValidateProvided(context.Background(), item, file); err == nil {
		t.Fatal("polygon accepted as river line")
	}
	geometry = "LineString"
	names = map[string]float64{"name": 1}
	if _, err := engine.ValidateProvided(context.Background(), item, file); err == nil {
		t.Fatal("missing NAME accepted")
	}
	names = map[string]float64{"NAME": 1}
	result, err := engine.ValidateProvided(context.Background(), item, file)
	if err != nil || result.Status != "user_provided" || result.Output.Paths[0] != file {
		t.Fatal(result, err)
	}
	after, _ := os.Stat(file)
	raw, _ := os.ReadFile(file)
	if string(raw) != string(original) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("original file modified")
	}
	entries, _ := os.ReadDir(engine.Store.Root)
	if len(entries) != 0 {
		t.Fatal("validation temporary files leaked")
	}
}
func TestProvidedTableRejectsMissingColumnsEmptyAndInvalidEncoding(t *testing.T) {
	file := filepath.Join(t.TempDir(), "stats.csv")
	for _, data := range [][]byte{[]byte("region,value\nA,12\n"), []byte("region,year,value\n"), {0xff, 0xfe, 0x00}} {
		_ = os.WriteFile(file, data, 0600)
		if _, err := inspectProvidedTable(file, map[string]string{"region": "region", "year": "year", "value": "value"}); err == nil {
			t.Fatalf("invalid table accepted: %q", data)
		}
	}
	_ = os.WriteFile(file, []byte("region,year,value\nA,2020,12\n"), 0600)
	stats, err := inspectProvidedTable(file, map[string]string{"region": "region", "year": "year", "value": "value"})
	if err != nil || number(stats["count"]) != 1 {
		t.Fatal(stats, err)
	}
}
