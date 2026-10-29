package layers

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, l *Library, name string) Item {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".geojson")
	if e := os.WriteFile(path, []byte(`{"type":"FeatureCollection","features":[]}`), 0600); e != nil {
		t.Fatal(e)
	}
	m := Manifest{APIVersion: "layer/v1", Name: name, Pack: "测试", Category: "admin.division", Params: map[string]string{"level": "省"}, Geometry: "polygon", File: path, CRS: "EPSG:4326", Coverage: Coverage{BBox: []float64{100, 20, 125, 45}}, Fields: map[string]string{"name": "NAME"}}
	i, e := l.Save(m)
	if e != nil {
		t.Fatal(e)
	}
	return i
}
func TestLibraryRegistrationDigestAndReadOnly(t *testing.T) {
	l := NewLibrary("", t.TempDir())
	i := fixture(t, l, "省域")
	m := i.Manifest
	m.ID = ""
	if _, e := l.Save(m); e == nil {
		t.Fatal("duplicate name accepted")
	}
	files, e := l.ReadableFiles()
	if e != nil || len(files) != 1 || files[0] != i.File {
		t.Fatal(files, e)
	}
	if e = os.WriteFile(i.File, []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	item, e := l.Get(i.ID)
	if e != nil || item.Status != "changed" {
		t.Fatal(item, e)
	}
	c, e := l.Match(Requirement{Category: "admin.division"})
	if e != nil || len(c) != 0 {
		t.Fatal(c, e)
	}
	if e = l.Delete(i.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(i.File); e != nil {
		t.Fatal("deleted original", e)
	}
}
func TestAliasAndMatchingPriority(t *testing.T) {
	a := Item{Manifest: Manifest{ID: "a", Name: "省域", Aliases: []string{"省级行政区"}, Category: "admin.division", Params: map[string]string{"level": "省"}, Geometry: "polygon", Coverage: Coverage{BBox: []float64{100, 20, 120, 40}}, Fields: map[string]string{"name": "NAME"}}, Status: "ready", Scope: "user"}
	b := a
	b.ID = "b"
	b.Name = "全国省域"
	b.Scope = "system"
	b.Coverage.BBox = []float64{70, 0, 140, 60}
	items := []Item{a, b}
	if _, e := ResolveName(items, "省级行政区"); !errors.Is(e, ErrAmbiguous) {
		t.Fatal(e)
	}
	r := Requirement{Category: "admin.division", Params: map[string]string{"level": "省"}, Geometry: "polygon", Fields: []string{"name"}, Extent: []float64{110, 30, 115, 35}}
	c := Match(items, r)
	if len(c) != 2 || c[0].Layer.ID != "a" {
		t.Fatal(c)
	}
	r.ExplicitName = b.Name
	c = Match(items, r)
	if c[0].Layer.ID != "b" {
		t.Fatal(c)
	}
	r.ExplicitName = a.Name
	r.Extent = []float64{125, 30, 130, 40}
	c = Match(items, r)
	if len(c) != 1 || !c[0].NeedsConfirmation {
		t.Fatal(c)
	}
	r.Fields = []string{"missing"}
	if len(Match(items, r)) != 0 {
		t.Fatal("missing field matched")
	}
	a.Name = "省级行政区"
	if x, e := ResolveName([]Item{a, b}, a.Name); e != nil || x.ID != "a" {
		t.Fatal(x, e)
	}
}
func TestManifestValidationAndMigration(t *testing.T) {
	l := NewLibrary("", t.TempDir())
	i := fixture(t, l, "河流")
	for _, change := range []func(*Manifest){func(m *Manifest) { m.Name = "../escape" }, func(m *Manifest) { m.Geometry = "table" }, func(m *Manifest) { m.CRS = "" }, func(m *Manifest) { m.Coverage.BBox = []float64{1, 2, 0, 3} }} {
		m := i.Manifest
		change(&m)
		if e := l.Validate(m); e == nil {
			t.Fatal(m)
		}
	}
	home := t.TempDir()
	folder := t.TempDir()
	os.MkdirAll(filepath.Join(home, "config"), 0700)
	os.WriteFile(filepath.Join(home, "config", "flow_input_memory.yaml"), []byte("flows:\n  marching-route:\n    底图数据: "+folder+"\n"), 0600)
	suggestions, e := l.MigrationSuggestions(home)
	if e != nil || len(suggestions) != 1 || suggestions[0].Path != folder {
		t.Fatal(suggestions, e)
	}
}
func TestShapefileDigestIncludesSidecars(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "layer.shp")
	os.WriteFile(p, []byte("shape"), 0600)
	os.WriteFile(filepath.Join(root, "layer.dbf"), []byte("attributes"), 0600)
	a, _ := Digest(p)
	os.WriteFile(filepath.Join(root, "layer.dbf"), []byte("changed"), 0600)
	b, _ := Digest(p)
	if a == b || !strings.HasPrefix(a, "sha256:") {
		t.Fatal(a, b)
	}
}

func TestScaleWithinSameScopeAndChangedSystemLayer(t *testing.T) {
	a := Item{Manifest: Manifest{ID: "a", Name: "粗", Category: "hydro.river_line", Geometry: "line", Scale: Scale{BestFrom: 1000000, BestTo: 10000000}}, Scope: "user", Status: "ready"}
	b := a
	b.ID = "b"
	b.Name = "细"
	b.Scale = Scale{BestFrom: 10000, BestTo: 100000}
	matches := Match([]Item{a, b}, Requirement{Category: "hydro.river_line", Scale: 50000})
	if len(matches) != 2 || matches[0].Layer.ID != "b" {
		t.Fatal(matches)
	}
	l := NewLibrary("", t.TempDir())
	i := fixture(t, l, "只读")
	l.SystemRoot = l.UserRoot
	l.UserRoot = ""
	if e := l.Delete(i.ID); !errors.Is(e, ErrReadOnly) {
		t.Fatal(e)
	}
}

func TestExplicitLayerWithMissingFieldsRequiresConfirmationForReplacement(t *testing.T) {
	original := Item{Manifest: Manifest{ID: "original", Name: "原始河流", Category: "hydro.river_line", Geometry: "line"}, Scope: "user", Status: "ready"}
	replacement := original
	replacement.ID, replacement.Name = "replacement", "有名称的河流"
	replacement.Fields = map[string]string{"name": "NAME"}
	matches := Match([]Item{original, replacement}, Requirement{ExplicitName: original.Name, Category: original.Category, Geometry: "line", Fields: []string{"name"}})
	if len(matches) != 1 || !matches[0].NeedsConfirmation || matches[0].Layer.ID != "replacement" {
		t.Fatal(matches)
	}
}
