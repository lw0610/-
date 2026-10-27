package osmdata

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestComplexPolygonQueryKeepsAllOriginalOuterVertices(t *testing.T) {
	var ring []Point
	for i := 0; i < 1479; i++ {
		a := float64(i) * 2 * math.Pi / 1479
		r := 1 + .2*math.Sin(a*23)
		ring = append(ring, Point{114 + r*math.Cos(a), 30 + r*math.Sin(a)})
	}
	ring = append(ring, ring[0])
	write := func(polygons [][][]Point) string {
		raw, _ := json.Marshal(map[string]any{"type": "MultiPolygon", "coordinates": polygons})
		path := filepath.Join(t.TempDir(), "region.geojson")
		_ = os.WriteFile(path, raw, 0600)
		return "file:" + path
	}
	extent, err := ParseExtent(write([][][]Point{{ring}}))
	if err != nil || len(extent.Filters) != 1 || strings.Count(extent.Filters[0], " ") != len(ring)*2-1 {
		t.Fatalf("complex concave boundary was simplified: %+v %v", extent, err)
	}
	polygons := make([][][]Point, 19)
	for i := range polygons {
		polygons[i] = [][]Point{{{110, 30}, {111, 30}, {111, 31}, {110, 30}}}
	}
	extent, err = ParseExtent(write(polygons))
	if err != nil || len(extent.Filters) != 19 {
		t.Fatalf("multi-polygon fell back to bbox: %+v %v", extent, err)
	}
	tooMany := make([]Point, maxPolygonQueryVertices+1)
	for i := range tooMany {
		tooMany[i] = Point{114, 30}
	}
	if _, err = ParseExtent(write([][][]Point{{tooMany}})); err == nil || !strings.Contains(err.Error(), "未改用外接矩形") {
		t.Fatal("oversized region silently widened", err)
	}
	if _, err = BuildQuery(Extent{Filters: []string{strings.Repeat("x", 1<<20)}}, []string{"highway"}, []string{"way"}, 120, "out geom;"); err == nil {
		t.Fatal("oversized query was accepted")
	}
}

func TestReplacementRollbackRestoresUppercaseMixedFamily(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "roads.GEOJSON")
	options := Options{OutputDir: root}
	if err := PublishFeatures(options, map[string]any{"output": base}, Convert(fixture(), nil, nil).Features, &Result{}); err != nil {
		t.Fatal(err)
	}
	var paths []string
	previous := map[string]string{}
	for _, path := range familyPaths(base) {
		for _, name := range []string{path, path + ".source.json"} {
			if raw, err := os.ReadFile(name); err == nil {
				paths = append(paths, name)
				previous[name] = string(raw)
			}
		}
	}
	if len(paths) != 4 || strings.Contains(strings.Join(paths, " "), ".GEOJSON-") {
		t.Fatalf("incorrect uppercase family: %+v", paths)
	}
	sort.Strings(paths)
	stage := t.TempDir()
	for i := range paths {
		_ = os.WriteFile(filepath.Join(stage, fmt.Sprint(i)), []byte("replacement"), 0600)
	}
	calls := 0
	err := publishReplacement(base, stage, paths, func(from, to string) error {
		calls++
		if calls == 2 {
			return fmt.Errorf("synthetic second-file failure")
		}
		return os.Rename(from, to)
	})
	if err == nil {
		t.Fatal("publication fault not exercised")
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != previous[path] {
			t.Fatalf("old family damaged: %s %v", path, err)
		}
	}
}
