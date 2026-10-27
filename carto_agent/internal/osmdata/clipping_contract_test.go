package osmdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpatialSelectionNeverClaimsGeometryClipping(t *testing.T) {
	fastNetwork(t)
	boundary := filepath.Join(t.TempDir(), "boundary.geojson")
	// An interior hole must remain available to a later geometry intersection;
	// Overpass's polygon selector only uses the exterior ring.
	if err := os.WriteFile(boundary, []byte(`{"type":"Polygon","coordinates":[[[114,30],[115,30],[115,31],[114,31],[114,30]],[[114.4,30.4],[114.6,30.4],[114.6,30.6],[114.4,30.6],[114.4,30.4]]]}`), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Overpass selects the entire way because part of it matches the region.
		fmt.Fprint(w, `{"elements":[{"type":"way","id":42,"tags":{"highway":"motorway"},"geometry":[{"lon":113,"lat":30.5},{"lon":114.5,"lat":30.5},{"lon":116,"lat":30.5}]}]}`)
	}))
	defer server.Close()
	for _, extent := range []string{"area:R1", "bbox:30,114,31,115", "around:1000,30.5,114.5", "file:" + boundary} {
		t.Run(strings.Split(extent, ":")[0], func(t *testing.T) {
			parsed, err := ParseExtent(extent)
			if err != nil || parsed.Clipped {
				t.Fatalf("spatial selection advertised clipping: %+v, %v", parsed, err)
			}
			result, err := Invoke(context.Background(), "osm_get_features", map[string]any{"extents": []string{extent}, "tags": []string{"highway=motorway"}, "types": []string{"way"}}, Options{Enabled: true, Authorized: true, Endpoints: []string{server.URL}, OutputDir: t.TempDir()})
			if err != nil || result.Clipped || result.FeatureCount != 1 || len(result.Warnings) == 0 {
				t.Fatalf("incorrect untrimmed result: %+v, %v", result, err)
			}
			var dataPath string
			for _, path := range result.ArtifactPaths {
				if strings.HasSuffix(path, ".geojson") {
					dataPath = path
				}
			}
			if dataPath == "" {
				t.Fatal("missing output")
			}
			var document struct {
				Features []struct {
					Geometry struct {
						Coordinates []Point `json:"coordinates"`
					} `json:"geometry"`
				} `json:"features"`
			}
			raw, err := os.ReadFile(dataPath)
			if err != nil || json.Unmarshal(raw, &document) != nil || len(document.Features) != 1 || document.Features[0].Geometry.Coordinates[0][0] != 113 {
				t.Fatalf("fixture did not retain crossing geometry: %s, %v", raw, err)
			}
			var provenance struct {
				Clipped  bool     `json:"clipped"`
				Extents  []string `json:"extents"`
				Warnings []string `json:"warnings"`
			}
			raw, err = os.ReadFile(dataPath + ".source.json")
			if err != nil || json.Unmarshal(raw, &provenance) != nil || provenance.Clipped || len(provenance.Extents) != 1 || provenance.Extents[0] != extent || len(provenance.Warnings) == 0 {
				t.Fatalf("incorrect clipping provenance: %s, %v", raw, err)
			}
		})
	}
}
