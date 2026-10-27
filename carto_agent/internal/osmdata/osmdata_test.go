package osmdata

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueryContracts(t *testing.T) {
	for _, tc := range []struct{ in, out string }{{"amenity=hospital", `["amenity"="hospital"]`}, {"healthcare", `["healthcare"]`}, {"highway~primary|trunk", `["highway"~"primary|trunk"]`}, {`["amenity"="clinic"]`, `["amenity"="clinic"]`}, {`name=a"b`, `["name"="a\"b"]`}} {
		got, e := TagClause(tc.in)
		if e != nil || got != tc.out {
			t.Fatalf("%q: %q %v", tc.in, got, e)
		}
	}
	if _, e := TagClause(`["x"];out;`); e == nil {
		t.Fatal("raw QL injection")
	}
	for _, id := range []string{"R123", "w2", "N3"} {
		if _, e := ParseRef(id); e != nil {
			t.Fatal(e)
		}
	}
	for _, id := range []string{"X3", "R-1", "R0", "1", "R1.5"} {
		if _, e := ParseRef(id); e == nil {
			t.Fatal(id)
		}
	}
	ext, _ := ParseExtent("bbox:30,114,31,115")
	q, e := BuildQuery(ext, []string{"railway=rail", "highway"}, []string{"node", "way"}, 120, "out geom;")
	if e != nil || strings.Count(q, "(30,114,31,115)") != 4 {
		t.Fatalf("%s %v", q, e)
	}
	q, _ = NameQuery("成都(高新)店", "", 30)
	if strings.Contains(q, ",i") || !strings.Contains(q, `成都\\(高新\\)店`) || strings.Index(q, "rel[") > strings.Index(q, "way[") {
		t.Fatal(q)
	}
	q, _ = NameQuery("C++", "", 30)
	if !strings.Contains(q, ",i") {
		t.Fatal(q)
	}
	if TagKey(`["amenity"="clinic"]`) != "amenity" {
		t.Fatal("tag key")
	}
}
func TestExtentContracts(t *testing.T) {
	e, err := ParseExtent("area:R10717504")
	if err != nil || e.Prelude != "area(3610717504)->.a;" {
		t.Fatal(e, err)
	}
	for _, raw := range []string{"area:W2", "bbox:31,114,30,115", "bbox:30,114,95,115", "around:0,30,114", "around:200000,30,114", "circle:1", "file:/tmp/x.shp"} {
		if _, err := ParseExtent(raw); err == nil {
			t.Fatal(raw)
		}
	}
	dir := t.TempDir()
	var ring []Point
	for i := 0; i < 1000; i++ {
		a := float64(i) * 2 * math.Pi / 1000
		r := 1 + .2*math.Sin(a*23)
		ring = append(ring, Point{114 + r*math.Cos(a), 30 + r*math.Sin(a)})
	}
	ring = append(ring, ring[0])
	hull := ConvexHull(ring)
	if hull[0] != hull[len(hull)-1] || len(hull) >= len(ring) {
		t.Fatal("hull")
	}
	for _, p := range ring {
		for i := 1; i < len(hull); i++ {
			if cross(hull[i-1], hull[i], p) < -1e-9 {
				t.Fatal("hull loses vertex")
			}
		}
	}
	write := func(name string, poly any) string {
		raw, _ := json.Marshal(map[string]any{"type": "MultiPolygon", "coordinates": poly})
		path := filepath.Join(dir, name)
		os.WriteFile(path, raw, 0600)
		return "file:" + path
	}
	e, err = ParseExtent(write("multi.geojson", [][][]Point{{ring}, {ring}}))
	if err != nil || len(e.Filters) != 2 || e.Clipped {
		t.Fatal(e, err)
	}
	many := make([][][]Point, 9)
	for i := range many {
		many[i] = [][]Point{ring}
	}
	e, err = ParseExtent(write("many.geojson", many))
	if err != nil || len(e.Filters) != len(many) || !strings.Contains(e.Filters[0], "poly") {
		t.Fatal(e, err)
	}
}
func fixture() []Element {
	raw := `{"elements":[{"type":"node","id":101,"lat":30.5,"lon":114.3,"tags":{"amenity":"hospital","name":"测试医院","name:en":"Test Hospital","phone":"123"}},{"type":"way","id":202,"tags":{"building":"hospital","name":"院区","addr:city":"武汉"},"geometry":[{"lat":30,"lon":114},{"lat":30,"lon":114.001},{"lat":30.001,"lon":114.001},{"lat":30,"lon":114}]}]}`
	var r Response
	json.Unmarshal([]byte(raw), &r)
	return r.Elements
}
func TestConversionFieldIdentityAndRelations(t *testing.T) {
	data := fixture()
	c := Convert(data, []string{"amenity"}, []string{"name:en"})
	if len(c.Features) != 2 || len(c.Dropped) != 0 || c.DroppedFields["phone"] != 1 || c.Features[1].Geometry.Type != "Polygon" {
		t.Fatal(c)
	}
	p := c.Features[0].Properties
	if p["name"] != "测试医院" || p["category"] != "amenity=hospital" || p["amenity"] != "hospital" || p["name:en"] != "Test Hospital" {
		t.Fatal(p)
	}
	all := Convert(data, nil, []string{"*"})
	if len(all.DroppedFields) != 0 || all.Features[0].Properties["osm_tags"] == nil {
		t.Fatal(all)
	}
	data = append(data, data...)
	if len(Convert(data, nil, nil).Features) != 2 {
		t.Fatal("dedup")
	}
	way := Element{Type: "way", ID: 200, Tags: map[string]string{"highway": "primary"}, Geometry: []Coordinate{{30, 114}, {30.1, 114.1}}}
	relation := Element{Type: "relation", ID: 901, Tags: map[string]string{"type": "route", "route": "bus"}, Members: []Member{{Type: "way", Ref: 200, Geometry: way.Geometry}}}
	broken := Element{Type: "relation", ID: 902, Tags: map[string]string{"type": "multipolygon"}, Members: []Member{{Type: "way", Role: "outer", Geometry: way.Geometry}}}
	c = Convert([]Element{way, relation, broken}, nil, nil)
	if len(c.Features) != 2 || !reflect.DeepEqual(c.Dropped, []string{"R902"}) {
		t.Fatal(c)
	}
	if _, ok := c.Features[0].Properties["name"]; !ok {
		t.Fatal("missing null name")
	}
}
func TestRingsHolesCentroidFullAndClassification(t *testing.T) {
	outer := []Coordinate{{0, 0}, {0, 4}, {4, 4}, {4, 0}, {0, 0}}
	inner := []Coordinate{{1, 1}, {1, 2}, {2, 2}, {2, 1}, {1, 1}}
	r := Element{Type: "relation", ID: 1, Tags: map[string]string{"type": "multipolygon"}, Members: []Member{{Type: "way", Role: "outer", Geometry: outer[:3]}, {Type: "way", Role: "outer", Geometry: outer[2:]}, {Type: "way", Role: "inner", Geometry: inner}}}
	g, e := GeometryOf(r)
	if e != nil || g.Type != "Polygon" || len(g.Coordinates.([][]Point)) != 2 {
		t.Fatal(g, e)
	}
	f := ToCentroid(Feature{"Feature", map[string]any{}, g})
	if f.Geometry.Coordinates.(Point) != (Point{2, 2}) || f.Properties["original_geometry"] != "Polygon" {
		t.Fatal(f)
	}
	if lineMidpoint([]Point{{0, 0}, {1, 0}, {10, 0}}) != (Point{5, 0}) {
		t.Fatal("midpoint")
	}
	p := 1.
	_, err := HydrateFull([]Element{{Type: "node", ID: 1, Lat: &p, Lon: &p}, {Type: "way", ID: 2, Nodes: []int64{1, 3}}}, Ref{"way", 2})
	if err == nil {
		t.Fatal("truncated full accepted")
	}
	if isPolygon(map[string]string{"highway": "primary"}) || !isPolygon(map[string]string{"highway": "services"}) || isPolygon(map[string]string{"building": "yes", "area": "no"}) {
		t.Fatal("polygon rules")
	}
}
func fastNetwork(t *testing.T) {
	oldInterval, oldBackoffs := requestInterval, backoffs
	requestInterval = time.Millisecond
	backoffs = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	overpassThrottle.next = time.Time{}
	nominatimThrottle.next = time.Time{}
	states = sync.Map{}
	t.Cleanup(func() { requestInterval, backoffs = oldInterval, oldBackoffs; states = sync.Map{} })
}
func TestClientRankFailoverSoftFailureAndBounds(t *testing.T) {
	fastNetwork(t)
	var fail atomic.Bool
	fail.Store(true)
	var badCalls, goodCalls atomic.Int32
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badCalls.Add(1)
		if fail.Load() {
			fmt.Fprint(w, "<html>busy</html>")
			return
		}
		fmt.Fprint(w, `{"elements":[]}`)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodCalls.Add(1)
		if r.Method != "POST" || r.Header.Get("User-Agent") == "" {
			t.Error("request protocol")
		}
		fmt.Fprint(w, `{"elements":[]}`)
	}))
	defer good.Close()
	c, _ := newClient(Options{Endpoints: []string{bad.URL, good.URL}})
	_, node, e := c.query(context.Background(), "query")
	if e != nil || node != good.URL {
		t.Fatal(node, e)
	}
	before := badCalls.Load()
	_, _, e = c.query(context.Background(), "again")
	if e != nil || badCalls.Load() != before {
		t.Fatal("not sticky")
	}
	remark := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"elements":[],"remark":"timed out"}`) }))
	defer remark.Close()
	c, _ = newClient(Options{Endpoints: []string{remark.URL}})
	if _, _, e = c.query(context.Background(), "x"); e == nil {
		t.Fatal("remark accepted")
	}
	if len(c.state().ranked) != 0 {
		t.Fatal("failed ranks not cleared")
	}
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", (1<<20)+1)) }))
	defer big.Close()
	c, _ = newClient(Options{MaxResponseMB: 1})
	if _, e = c.request(context.Background(), "GET", big.URL, "", false); e == nil || !strings.Contains(e.Error(), "缩小范围") {
		t.Fatal(e)
	}
}
func TestGlobalThrottleAndCancellation(t *testing.T) {
	fastNetwork(t)
	requestInterval = 30 * time.Millisecond
	var mu sync.Mutex
	var times []time.Time
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		times = append(times, time.Now())
		mu.Unlock()
		fmt.Fprint(w, `{}`)
	}))
	defer s.Close()
	c, _ := newClient(Options{})
	for i := 0; i < 3; i++ {
		if _, e := c.request(context.Background(), "GET", s.URL, "", false); e != nil {
			t.Fatal(e)
		}
	}
	for i := 1; i < len(times); i++ {
		if times[i].Sub(times[i-1]) < 25*time.Millisecond {
			t.Fatal("throttle")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.request(ctx, "GET", s.URL, "", false); e == nil {
		t.Fatal("cancel")
	}
}
func TestOutputsDefaultExclusiveSplitCleanupAndTraversal(t *testing.T) {
	dir := t.TempDir()
	opt := Options{OutputDir: filepath.Join(dir, "output/osm"), ProjectDir: filepath.Join(dir, "project")}
	features := Convert(fixture(), nil, nil).Features
	result := &Result{}
	if e := writeOutputs(opt, map[string]any{}, features, result); e != nil {
		t.Fatal(e)
	}
	if len(result.ArtifactPaths) != 4 {
		t.Fatal(result)
	}
	var source map[string]any
	raw, _ := os.ReadFile(result.ArtifactPaths[1])
	if e := json.Unmarshal(raw, &source); e != nil || source["license"] != "ODbL 1.0" {
		t.Fatal(source, e)
	}
	again := &Result{}
	if e := writeOutputs(opt, map[string]any{}, features, again); e != nil || reflect.DeepEqual(result.ArtifactPaths, again.ArtifactPaths) {
		t.Fatal(again, e)
	}
	if e := writeOutputs(opt, map[string]any{"overwrite": true}, features[:1], &Result{}); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(opt.OutputDir, "osm-data-polygons.geojson")); !os.IsNotExist(e) {
		t.Fatal("stale polygon")
	}
	for _, raw := range []string{"../../../../escape.geojson", filepath.Join(dir, "bad.geojson")} {
		if _, e := outputBase(opt, raw); e == nil {
			t.Fatal(raw)
		}
	}
	os.MkdirAll(opt.ProjectDir, 0700)
	os.Symlink(dir, filepath.Join(opt.ProjectDir, "link"))
	if _, e := outputBase(opt, filepath.Join(opt.ProjectDir, "link/x.geojson")); e == nil {
		t.Fatal("symlink escaped")
	}
}
func TestToolDownloadAndObjectFallback(t *testing.T) {
	fastNetwork(t)
	payload, _ := json.Marshal(Response{Elements: fixture()})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			r.ParseForm()
			if strings.Contains(r.Form.Get("data"), "out skel") {
				fmt.Fprint(w, `{"elements":[]}`)
			} else if strings.Contains(r.Form.Get("data"), "node(101)") {
				fmt.Fprint(w, `{"elements":[]}`)
			} else {
				w.Write(payload)
			}
			return
		}
		fmt.Fprint(w, `{"elements":[{"type":"node","id":101,"lat":30,"lon":114,"tags":{"name":"fallback"}}]}`)
	}))
	defer server.Close()
	opt := Options{Enabled: true, Authorized: true, Endpoints: []string{server.URL}, OSMAPIURL: server.URL, OutputDir: t.TempDir()}
	r, e := Invoke(context.Background(), "osm_get_features", map[string]any{"extents": []string{"bbox:29,113,31,115"}, "tags": []string{"amenity"}, "fields": []string{"name:en"}}, opt)
	if e != nil || r.FeatureCount != 2 || len(r.ArtifactPaths) != 4 {
		t.Fatal(r, e)
	}
	r, e = Invoke(context.Background(), "osm_get_objects", map[string]any{"ids": []string{"N101"}}, opt)
	if e != nil || r.FeatureCount != 1 {
		t.Fatal(r, e)
	}
	opt.Authorized = false
	if _, e = Invoke(context.Background(), "osm_get_objects", map[string]any{"ids": []string{"N101"}}, opt); e == nil {
		t.Fatal("confirmation bypass")
	}
}

func TestSearchProbeAndPartialResults(t *testing.T) {
	fastNetwork(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `[{"osm_type":"relation","osm_id":1,"display_name":"示例","lat":"30","lon":"114","class":"boundary","type":"administrative"}]`)
			return
		}
		r.ParseForm()
		q := r.Form.Get("data")
		switch {
		case strings.Contains(q, "out skel"):
			fmt.Fprint(w, `{"elements":[]}`)
		case strings.Contains(q, "out count"):
			fmt.Fprint(w, `{"elements":[{"type":"count","tags":{"nodes":"2","ways":"3","relations":"1","total":"6"}}]}`)
		case strings.Contains(q, "[\"type\"=\"site\"]"):
			fmt.Fprint(w, `{"elements":[{"type":"relation","id":1,"tags":{"type":"site"},"members":[{"type":"way","ref":20}]}]}`)
		case strings.Contains(q, "out tags center"):
			fmt.Fprint(w, `{"elements":[{"type":"relation","id":1,"center":{"lat":30,"lon":114},"tags":{"name":"示例","boundary":"administrative","admin_level":"4"}}]}`)
		case strings.Contains(q, "(0,0,1,1)"):
			fmt.Fprint(w, `{"remark":"timeout"}`)
		default:
			raw, _ := json.Marshal(Response{Elements: fixture()})
			w.Write(raw)
		}
	}))
	defer server.Close()
	opt := Options{Enabled: true, Authorized: true, Endpoints: []string{server.URL}, NominatimURL: server.URL, OutputDir: t.TempDir()}
	r, e := Invoke(context.Background(), "osm_search", map[string]any{"query": "示例"}, opt)
	if e != nil {
		t.Fatal(e)
	}
	rows := r.Data.([]candidate)
	if len(rows) != 1 || len(rows[0].Sources) != 2 || len(rows[0].Members) != 1 {
		t.Fatal(rows)
	}
	r, e = Invoke(context.Background(), "osm_probe", map[string]any{"extents": []string{"bbox:29,113,31,115"}, "tags": []string{"highway"}}, opt)
	if e != nil || r.Data.([]map[string]any)[0]["counts"].(map[string]int)["total"] != 6 {
		t.Fatal(r, e)
	}
	r, e = Invoke(context.Background(), "osm_get_features", map[string]any{"extents": []string{"bbox:29,113,31,115", "bbox:0,0,1,1"}, "tags": []string{"highway"}}, opt)
	if e != nil || !r.Partial || len(r.FailedExtents) != 1 || !strings.HasPrefix(r.Info, "部分结果") || r.FeatureCount != 2 {
		t.Fatal(r, e)
	}
}
