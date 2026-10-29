package dataservice

import (
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/osmdata"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Service, *atomic.Int32) {
	t.Helper()
	root := t.TempDir()
	input := filepath.Join(root, "source.geojson")
	os.WriteFile(input, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"LineString","coordinates":[[110,25],[111,26]]},"properties":{"name":"river"}}]}`), 0600)
	catalog := &datasource.Catalog{Categories: []datasource.Category{{ID: "water.river", Name: "河流", Aliases: []datasource.Alias{{Name: "河流线"}}, Geometry: "line", Validate: datasource.Validation{MinFeatures: 1}}}, Sources: []datasource.Source{{ID: "user.local", Name: "本机河流", Kind: "local_folder", Root: root, Provides: []datasource.Provide{{Category: "water.river", Path: "source.geojson"}}, Handbook: datasource.Handbook{GoodFor: "河流线"}}}}
	calls := &atomic.Int32{}
	engine := &acquire.Engine{Catalog: catalog, Store: acquire.NewStore(filepath.Join(root, "cache"), 0), Adapters: map[string]acquire.Adapter{"local_folder": acquire.LocalAdapter{}}, Config: config.AcquisitionConfig{}.WithDefaults()}
	engine.Invoke = func(_ context.Context, name string, args map[string]any) (any, error) {
		calls.Add(1)
		if name != "data_normalize" {
			return nil, fmt.Errorf("unexpected %s", name)
		}
		input := args["input"].(map[string]any)["path"].(string)
		output := args["output"].(string)
		if err := copyFile(input, output); err != nil {
			return nil, err
		}
		geometry := "LineString"
		if strings.Contains(input, "wrong") {
			geometry = "Point"
		}
		return map[string]any{"success": true, "data": map[string]any{"path": output, "paths": []string{output}, "count": 1, "geometry_type": geometry, "crs": "EPSG:4326", "extent_wgs84": []float64{110, 25, 111, 26}, "fields": map[string]any{"name": 1.0}}}, nil
	}
	service := New(Options{SessionID: "test-session", SessionDir: filepath.Join(root, "session"), Environment: datasource.PlanningEnvironment{Catalog: catalog, AutoAcquire: "allow", Integrations: map[string]bool{"web": true, "osm": true}, WebSearch: true}, Engine: engine, Memory: flow.NewDataMemory(filepath.Join(root, "memory.yaml")), Config: config.AcquisitionConfig{NetworkPolicy: "direct", FallbackPolicy: "allow", WebSearchEnabled: true}})
	return service, calls
}
func TestGetOSMThroughLocalServer(t *testing.T) {
	s, _ := fixture(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		r.ParseForm()
		if strings.Contains(r.Form.Get("data"), "out count") {
			w.Write([]byte(`{"elements":[{"type":"count","tags":{"nodes":"0","ways":"1","relations":"0","total":"1"}}]}`))
			return
		}
		w.Write([]byte(`{"elements":[{"type":"way","id":12,"tags":{"waterway":"river","name":"test"},"geometry":[{"lat":25,"lon":110},{"lat":26,"lon":111}]}]}`))
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	s.options.Environment.Catalog.Sources = []datasource.Source{{ID: "source.osm.fixture", Name: "OSM", Kind: "overpass", Hosts: []string{u.Host}, Provides: []datasource.Provide{{Category: "water.river", Tags: []string{"waterway=river"}, Types: "way"}}, License: "ODbL", Attribution: "© OpenStreetMap contributors"}}
	s.options.Engine.Adapters["overpass"] = acquire.OverpassAdapter{Options: osmdata.Options{Enabled: true, Authorized: true, Endpoints: []string{server.URL}, TimeoutSeconds: 5, TotalTimeoutSeconds: 15, MaxAttempts: 1, MaxResponseMB: 2}, Catalog: s.options.Environment.Catalog}
	result, err := s.Find(context.Background(), riverRequest())
	if err != nil {
		t.Fatal(err)
	}
	var transferred bool
	s.options.Progress = func(row string, p acquire.Progress) {
		if row != "" && p.Bytes > 0 {
			transferred = true
		}
	}
	got, err := s.Get(context.Background(), GetRequest{FindID: result.FindID})
	if err != nil || len(got.Rows) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if !transferred || requests.Load() < 2 || got.Rows[0].License != "ODbL" || len(got.Rows[0].SHA256) != 1 {
		t.Fatalf("requests=%d result=%+v", requests.Load(), got)
	}
}
func riverRequest() FindRequest {
	return FindRequest{Need: Need{Name: "河流", What: What{Category: "河流"}, Extent: "bbox:24,109,27,112", Scale: "auto"}}
}

type failTransport struct{ calls *atomic.Int32 }

func (t failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return nil, fmt.Errorf("network forbidden")
}
func TestFindReadOnlyKindsRankingAndChoiceExpiry(t *testing.T) {
	s, calls := fixture(t)
	var network atomic.Int32
	old := http.DefaultTransport
	http.DefaultTransport = failTransport{&network}
	defer func() { http.DefaultTransport = old }()
	path := s.options.Environment.Catalog.Sources[0].Root + "/source.geojson"
	s.options.UserData = []Binding{{Name: "河流", Path: path}}
	s.options.Memory.Remember(flow.DataMemoryEntry{Guide: "route-map", Name: "河流", Path: path})
	for _, what := range []What{{Category: "河流线"}, {Description: "湖南省主要河流线"}} {
		request := riverRequest()
		request.Guide = "route-map"
		request.Need.What = what
		result, err := s.Find(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rows) != 1 || result.Rows[0].Candidates[0].Kind != "user" || result.Rows[0].Recommended == "" {
			t.Fatalf("%+v", result)
		}
		if result.Resolved.Extent.BBox[0] != 109 || result.Resolved.Extent.BBox[1] != 24 {
			t.Fatal("bbox axes changed")
		}
		if _, err = s.ValidateChoice(context.Background(), result.FindID, map[string]string{"*": "invented"}); err == nil {
			t.Fatal("invented source accepted")
		}
		s.options.Now = func() time.Time { return result.ExpiresAt.Add(time.Second) }
		if _, err = s.ValidateChoice(context.Background(), result.FindID, nil); err == nil || !strings.Contains(err.Error(), "DATA_FIND_EXPIRED") {
			t.Fatal(err)
		}
		s.options.Now = time.Now
	}
	if network.Load() != 0 || calls.Load() != 0 {
		t.Fatalf("find performed work: network=%d tools=%d", network.Load(), calls.Load())
	}
}
func TestGetVerifiedUserBindingAndAutomaticNeed(t *testing.T) {
	s, calls := fixture(t)
	original, _ := os.ReadFile(filepath.Join(s.options.Environment.Catalog.Sources[0].Root, "source.geojson"))
	outside := filepath.Join(t.TempDir(), "river.geojson")
	os.WriteFile(outside, original, 0600)
	s.options.UserData = []Binding{{Name: "用户河流", Path: outside, Explicit: true}}
	request := riverRequest()
	request.Need.Name = "用户河流"
	found, err := s.Find(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), GetRequest{FindID: found.FindID})
	if err != nil || len(got.Rows) != 1 || got.Rows[0].Paths[0] != outside || got.Rows[0].Kind != "user" {
		t.Fatal(got, err)
	}
	after, _ := os.ReadFile(outside)
	if string(after) != string(original) || calls.Load() != 0 {
		t.Fatal("user file changed or normalized unnecessarily")
	}
	s.options.UserData = nil
	request.Need.Name = "缺少的河流"
	found, err = s.Find(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	id := ""
	for _, c := range found.Rows[0].Candidates {
		if c.Kind == "local_source" {
			id = c.ID
		}
	}
	got, err = s.Get(context.Background(), GetRequest{FindID: found.FindID, Choice: map[string]string{"*": id}})
	if err != nil || len(got.Rows) != 1 || got.Rows[0].StoreKey == "" {
		t.Fatal(got, err)
	}
	if _, err = os.Stat(got.ProvenancePath); err != nil {
		t.Fatal(err)
	}
	index, _ := sessionfiles.NewWithRoots(s.options.SessionID, s.options.SessionDir)
	files, err := index.List(true)
	if err != nil || len(files) < 3 {
		t.Fatal(files, err)
	}
}

func TestGetHTTPAndOfflineUseRealEngineAndCache(t *testing.T) {
	for _, kind := range []string{"http_file", "offline_asset"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := fixture(t)
			var downloads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				downloads.Add(1)
				w.Write([]byte("actual local test data"))
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			source := datasource.Source{ID: "source.fixture", Name: "fixture", Kind: kind, Hosts: []string{u.Host}, Provides: []datasource.Provide{{Category: "water.river", URL: server.URL + "/data.geojson", Tool: "admin_boundary_extract"}}}
			s.options.Environment.Catalog.Sources = []datasource.Source{source}
			if kind == "http_file" {
				s.options.Engine.Adapters[kind] = acquire.HTTPAdapter{Client: server.Client()}
			} else {
				s.options.Engine.Adapters[kind] = acquire.OfflineAdapter{Invoke: func(_ context.Context, name string, args map[string]any) (any, error) {
					downloads.Add(1)
					output := args["output"].(string)
					os.MkdirAll(filepath.Dir(output), 0700)
					os.WriteFile(output, []byte("offline actual fixture"), 0600)
					return map[string]any{"success": true, "output": output}, nil
				}}
			}
			result, err := s.Find(context.Background(), riverRequest())
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.Get(context.Background(), GetRequest{FindID: result.FindID})
			if err != nil || len(got.Rows) != 1 {
				t.Fatalf("%+v %v", got, err)
			}
			again, err := s.Find(context.Background(), riverRequest())
			if err != nil {
				t.Fatal(err)
			}
			if cacheCandidate(again.Rows[0]) == "" {
				t.Fatalf("%+v", again.Rows[0])
			}
			s.options.Config.NetworkPolicy = "deny"
			if _, err = s.Get(context.Background(), GetRequest{FindID: again.FindID, Choice: map[string]string{"*": cacheCandidate(again.Rows[0])}}); err != nil {
				t.Fatal(err)
			}
			if downloads.Load() != 1 {
				t.Fatal("cache redownloaded")
			}
		})
	}
}
func cacheCandidate(row FindRow) string {
	for _, candidate := range row.Candidates {
		if candidate.Kind == "data_store" {
			return candidate.ID
		}
	}
	return ""
}
func TestFallbackRegistrationMismatchAndProvenance(t *testing.T) {
	s, _ := fixture(t)
	s.options.Environment.Catalog.Sources = nil
	request := riverRequest()
	request.IncludeFallback = true
	result, err := s.Find(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"wrong.geojson", "valid.geojson"} {
		path := filepath.Join(s.options.SessionDir, name)
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, []byte("fixture"), 0600)
		got, err := s.Register(context.Background(), RegisterRequest{FindID: result.FindID, Row: "河流", Path: path, Kind: "web_search", URL: "https://example.test/data?token=private", License: "CC0"})
		if name == "wrong.geojson" {
			if err == nil || !strings.Contains(err.Error(), "DATA_REGISTER_MISMATCH") {
				t.Fatal(err)
			}
			continue
		}
		if err != nil || got.Status != "completed" {
			t.Fatalf("%+v %v", got, err)
		}
		raw, _ := os.ReadFile(got.ProvenancePath)
		if strings.Contains(string(raw), "private") {
			t.Fatal("secret leaked")
		}
		if got.Rows[0].StoreKey == "" {
			t.Fatal("fallback bypassed store")
		}
	}
}
