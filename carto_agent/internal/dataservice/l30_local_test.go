package dataservice

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGeo(t *testing.T, path, geometry string) {
	t.Helper()
	if e := os.WriteFile(path, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"`+geometry+`","coordinates":[]},"properties":{"name":"fixture"}}]}`), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestL30ProvidedFolderMatchesHeadersWithoutWork(t *testing.T) {
	s, calls := fixture(t)
	root := t.TempDir()
	river := filepath.Join(root, "河流.geojson")
	writeGeo(t, river, "LineString")
	writeGeo(t, filepath.Join(root, "河流误存面.geojson"), "Polygon")
	req := riverRequest()
	req.Need.What = What{Description: "线状河流"}
	req.Need.Geometry = "line"
	req.Provided = []string{root}
	before, _ := os.ReadFile(river)
	result, e := s.Find(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	if len(result.ProvidedMatches) != 1 || result.ProvidedMatches[0].Path != river || result.ProvidedMatches[0].Confidence != "high" || len(result.ProvidedUnmatched) == 0 {
		t.Fatalf("%+v", result)
	}
	after, _ := os.ReadFile(river)
	if calls.Load() != 0 || string(before) != string(after) {
		t.Fatal("Find performed execution or rewrote input")
	}
	req.Provided = []string{filepath.Join(root, "河流误存面.geojson")}
	result, e = s.Find(context.Background(), req)
	if e != nil || result.Rows[0].Recommended != "" {
		t.Fatalf("invalid provided file silently replaced: %+v %v", result, e)
	}
	if _, e = s.Get(context.Background(), GetRequest{FindID: result.FindID}); e == nil {
		t.Fatal("invalid supplied geometry must not trigger fallback acquisition")
	}
}
func TestL30ShapefileAndGeoPackageReadOnlyHeaders(t *testing.T) {
	root := t.TempDir()
	shp := filepath.Join(root, "river.shp")
	h := make([]byte, 100)
	binary.BigEndian.PutUint32(h[:4], 9994)
	binary.LittleEndian.PutUint32(h[28:32], 1000)
	binary.LittleEndian.PutUint32(h[32:36], 3)
	os.WriteFile(shp, h, 0600)
	os.WriteFile(filepath.Join(root, "river.shx"), h, 0600)
	dbf := make([]byte, 65)
	binary.LittleEndian.PutUint16(dbf[8:10], 65)
	copy(dbf[32:43], "name")
	os.WriteFile(filepath.Join(root, "river.dbf"), dbf, 0600)
	if b, e := ValidateLocalData(context.Background(), shp, "line", map[string]string{"name": "string"}); e != nil || b.Geometry != "line" {
		t.Fatal(b, e)
	}
	gpkg := filepath.Join(root, "source.gpkg")
	db, e := sql.Open("sqlite3", gpkg)
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{"CREATE TABLE gpkg_geometry_columns(table_name TEXT,geometry_type_name TEXT)", "CREATE TABLE rivers(name TEXT)", "CREATE TABLE cities(name TEXT)", "INSERT INTO gpkg_geometry_columns VALUES ('rivers','LINESTRING'),('cities','POINT')"} {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	db.Close()
	before, _ := os.ReadFile(gpkg)
	layers, e := InspectProvided(context.Background(), gpkg)
	if e != nil || len(layers) != 2 {
		t.Fatal(layers, e)
	}
	b, e := ValidateLocalData(context.Background(), gpkg+"|layername=rivers", "line", map[string]string{"name": "string"})
	if e != nil || b.Sublayer != "rivers" {
		t.Fatal(b, e)
	}
	after, _ := os.ReadFile(gpkg)
	if string(before) != string(after) {
		t.Fatal("metadata inspection modified gpkg")
	}
	if _, e = ValidateLocalData(context.Background(), gpkg+"|layername=cities", "line", nil); e == nil {
		t.Fatal("wrong geometry accepted")
	}
}
func TestL30ArgumentsCorrectableAndBoundedAcrossDelegation(t *testing.T) {
	s, _ := fixture(t)
	ctx := WithService(context.Background(), s)
	for i, bad := range []any{"category", "description", 42} {
		_, e := Invoke(ctx, "data_find", map[string]any{"need": map[string]any{"what": bad}})
		var arg *ArgumentError
		if !errors.As(e, &arg) || arg.Attempts != i+1 || arg.FatalToolError() != (i == 2) || !strings.Contains(arg.Error(), `"description":"线状河流"`) {
			t.Fatal(e)
		}
	}
	corrected, _ := fixture(t)
	for i := 0; i < 2; i++ {
		corrected.Invoke(ctx, "data_find", map[string]any{"need": map[string]any{"what": "bad"}})
	}
	if _, e := corrected.Invoke(ctx, "data_find", map[string]any{"need": map[string]any{"what": map[string]any{"description": "河流线"}}}); e != nil {
		t.Fatal(e)
	}
	_, e := corrected.Invoke(ctx, "data_find", map[string]any{"need": map[string]any{"what": map[string]any{"list": "已移除"}}})
	var arg *ArgumentError
	if !errors.As(e, &arg) || arg.Attempts != 1 {
		t.Fatal(e)
	}
}
func TestL30ExplicitProjectLayerOutranksHistoricalUserFile(t *testing.T) {
	s, calls := fixture(t)
	path := filepath.Join(s.options.Environment.Catalog.Sources[0].Root, "source.geojson")
	s.options.UserData = []Binding{{Name: "河流", Path: path, Explicit: true}}
	s.options.Layers = func(context.Context) ([]Binding, error) {
		return []Binding{{Name: "用户选中的层", LayerID: "current-reference", Geometry: "LineString", Explicit: true}}, nil
	}
	result, e := s.Find(context.Background(), riverRequest())
	if e != nil {
		t.Fatal(e)
	}
	if result.Rows[0].Candidates[0].LayerID != "current-reference" || calls.Load() != 0 {
		t.Fatal(result)
	}
}
func TestL30HistoricalExplicitFileIsNotEverySameGeometryNeed(t *testing.T) {
	s, _ := fixture(t)
	file := filepath.Join(t.TempDir(), "铁路.geojson")
	writeGeo(t, file, "LineString")
	s.options.UserData = []Binding{{Name: "铁路", Path: file, Explicit: true}}
	result, e := s.Find(context.Background(), riverRequest())
	if e != nil {
		t.Fatal(e)
	}
	if len(result.ProvidedMatches) != 0 {
		t.Fatal("historical railway was treated as explicit river", result)
	}
}
