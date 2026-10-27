package acquire

import (
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"testing"
)

func TestValidateStatsFeatureEnvelopeIsNotPointOrLineCoverage(t *testing.T) {
	item := flow.AcquisitionItem{Fields: map[string]string{"NAME": "name"}, Extent: &flow.ExtentPlan{BBox: []float64{100, 20, 101, 21}}}
	for _, tt := range []struct {
		name, category, geometry string
		bbox                     []float64
		ok                       bool
	}{
		{"one station", "point", "Point", []float64{100.5, 20.5, 100.5, 20.5}, true},
		{"sparse places", "point", "MultiPoint", []float64{100.4, 20.4, 100.6, 20.6}, true},
		{"vertical railway", "line", "LineString", []float64{100.5, 20.1, 100.5, 20.9}, true},
		{"horizontal river", "line", "MultiLineString", []float64{100.1, 20.5, 100.9, 20.5}, true},
		{"short curved road", "line", "LineString", []float64{100.4, 20.4, 100.6, 20.6}, true},
		{"station outside", "point", "Point", []float64{110, 30, 110, 30}, false},
		{"railway outside", "line", "LineString", []float64{110, 30, 110, 31}, false},
		{"polygon insufficient", "polygon", "MultiPolygon", []float64{100.4, 20.4, 100.6, 20.6}, false},
		{"raster insufficient", "raster", "", []float64{100.4, 20.4, 100.6, 20.6}, false},
		{"polygon full", "polygon", "Polygon", []float64{100, 20, 101, 21}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			category := &datasource.Category{Geometry: tt.category, Validate: datasource.Validation{MinFeatures: 1, NameFillRatio: .95}}
			result := ValidateStats(category, item, map[string]any{"count": 1, "crs": "EPSG:4326", "geometry_type": tt.geometry, "fields": map[string]float64{"NAME": 1}, "extent_wgs84": tt.bbox})
			if result.OK != tt.ok || tt.ok && len(result.Warnings) != 0 {
				t.Fatalf("%+v", result)
			}
		})
	}
}
