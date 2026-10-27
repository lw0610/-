package acquire

import (
	"context"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/osmdata"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPAdapterWhitelistLimitAndRedaction(t *testing.T) {
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect escaped whitelist") }))
	defer outside.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, outside.URL, 302)
		case "/large":
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte(strings.Repeat("x", 33)))
		default:
			_, _ = w.Write([]byte("synthetic"))
		}
	}))
	defer server.Close()
	host, _ := url.Parse(server.URL)
	source := &datasource.Source{ID: "source.fixture", Hosts: []string{host.Host}}
	adapter := HTTPAdapter{Client: server.Client(), MaxBytes: 32}
	request := FetchRequest{Source: source, Provide: &datasource.Provide{URL: server.URL + "/sample.csv?token=test-only"}, WorkDir: t.TempDir()}
	result, err := adapter.Fetch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 9 || len(result.SHA256) != 1 || strings.Contains(strings.Join(result.URLs, ""), "test-only") {
		t.Fatalf("%+v", result)
	}
	for _, path := range []string{"/large", "/redirect"} {
		request.Provide = &datasource.Provide{URL: server.URL + path}
		request.WorkDir = t.TempDir()
		if _, err := adapter.Fetch(context.Background(), request); err == nil {
			t.Fatal("accepted", path)
		}
		files, _ := os.ReadDir(request.WorkDir)
		if len(files) > 0 {
			t.Fatal("partial file leaked", files)
		}
	}
	request.Provide = &datasource.Provide{URL: outside.URL}
	if _, err := adapter.Fetch(context.Background(), request); err == nil {
		t.Fatal("unauthorized source accepted")
	}
}
func TestLocalAndOfflineAdaptersReadOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rail.geojson")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	result, err := (LocalAdapter{}).Fetch(context.Background(), FetchRequest{Source: &datasource.Source{Root: root}, Provide: &datasource.Provide{Path: "rail.geojson"}})
	if err != nil || len(result.Files) != 1 || result.Files[0] != canonicalPath(path) {
		t.Fatalf("%+v %v", result, err)
	}
	after, _ := os.Stat(path)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("source changed")
	}
	source := &datasource.Source{Root: root}
	if _, err = (LocalAdapter{}).Fetch(context.Background(), FetchRequest{Source: source, Provide: &datasource.Provide{Path: "../escape"}}); err == nil {
		t.Fatal("traversal accepted")
	}
	output := t.TempDir()
	called := false
	offline := OfflineAdapter{Invoke: func(_ context.Context, name string, args map[string]any) (any, error) {
		called = true
		if name != "admin_boundary_extract" || args["load"] != false || args["within"] != "河南省" || args["level"] != "市" {
			t.Fatalf("%s %+v", name, args)
		}
		path := args["output"].(string)
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		return map[string]any{"success": true, "output": path}, nil
	}}
	_, err = offline.Fetch(context.Background(), FetchRequest{Provide: &datasource.Provide{Tool: "admin_boundary_extract"}, Item: flow.AcquisitionItem{Params: map[string]string{"level": "市"}, Extent: &flow.ExtentPlan{Region: "河南省"}}, WorkDir: output})
	if err != nil || !called {
		t.Fatal(err)
	}
}
func TestOverpassAdapterUsesSharedRuntimeAndRejectsRequiredPartial(t *testing.T) {
	calls := []string{}
	root := t.TempDir()
	path := filepath.Join(root, "lines.geojson")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	partial := false
	adapter := OverpassAdapter{Options: osmdata.Options{Enabled: true}, Invoke: func(_ context.Context, name string, args map[string]any, options osmdata.Options) (*osmdata.Result, error) {
		calls = append(calls, name)
		if args["extents"].([]string)[0] != "bbox:30.000000,110.000000,31.000000,111.000000" {
			t.Fatal(args)
		}
		if !options.Authorized {
			t.Fatal("plan authorization not passed")
		}
		if name == "osm_probe" {
			return &osmdata.Result{Success: true, Data: []map[string]any{{"counts": map[string]int{"total": 1}}}}, nil
		}
		return &osmdata.Result{Success: true, ArtifactPaths: []string{path}, Partial: partial, Queries: []string{"fixture query"}}, nil
	}}
	request := FetchRequest{Source: &datasource.Source{Limits: datasource.Limits{MaxFeatures: 30000}}, Provide: &datasource.Provide{Tags: []string{"railway=rail"}, Types: "way"}, Item: flow.AcquisitionItem{Required: true}, Extent: Extent{BBox: []float64{110, 30, 111, 31}}, WorkDir: root}
	result, err := adapter.Fetch(context.Background(), request)
	if err != nil || result.Query != "fixture query" || strings.Join(calls, ",") != "osm_probe,osm_get_features" {
		t.Fatalf("%+v %v %v", result, err, calls)
	}
	partial = true
	if _, err = adapter.Fetch(context.Background(), request); err == nil {
		t.Fatal("required partial accepted")
	}
}

func canonicalPath(path string) string { result, _ := filepath.EvalSymlinks(path); return result }
