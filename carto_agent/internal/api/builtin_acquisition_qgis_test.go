package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
)

// This opt-in test executes the real Go planner/cache/HTTP adapter, actual QGIS
// extraction/normalization, production resource handles, list loader and styles.
// Six non-boundary layers are clearly labelled loopback-only synthetic fixtures.
func TestBuiltinAcquisitionRealQGIS(t *testing.T) {
	python := os.Getenv("AUTOCARTO_QGIS_PYTHON")
	if python == "" {
		t.Skip("set AUTOCARTO_QGIS_PYTHON to run isolated real-QGIS offline acceptance")
	}
	a, _, project, home := realFlowAPI(t)
	t.Setenv("AUTOCARTO_DISABLE_KEYRING", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	invoke := func(ctx context.Context, name string, args map[string]any) (any, error) {
		input, _ := json.Marshal(map[string]any{"name": name, "args": args})
		command := exec.CommandContext(ctx, python, filepath.Join(project, "autocarto/tests/support/acquisition_qgis_bridge.py"))
		command.Dir = project
		command.Env = append(os.Environ(), "PYTHONPATH="+project+string(os.PathListSeparator)+os.Getenv("PYTHONPATH"), "AUTOCARTO_HOME="+home, "AUTOCARTO_DISABLE_KEYRING=1", "QT_QPA_PLATFORM=offscreen")
		command.Stdin = bytes.NewReader(input)
		output, err := command.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("QGIS %s: %w\n%s", name, err, string(output))
		}
		marker := []byte("V4_QGIS_RESULT=")
		offset := bytes.LastIndex(output, marker)
		if offset < 0 {
			return nil, fmt.Errorf("QGIS receipt missing: %s", output)
		}
		line := bytes.SplitN(output[offset+len(marker):], []byte("\n"), 2)[0]
		var result map[string]any
		if err = json.Unmarshal(line, &result); err != nil {
			return nil, err
		}
		return result, nil
	}
	compiler, err := a.FlowCompiler()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compiler.BasePlan(ctx, "marching-route", "compile-scheme")
	if err != nil || plan.HasErrors() {
		t.Fatalf("plan=%+v error=%v", plan, err)
	}
	env, err := a.loadFlowEnvironment(ctx, "compile-scheme")
	if err != nil {
		t.Fatal(err)
	}
	catalog := env.DataSources
	if catalog == nil {
		t.Fatal("catalog unavailable")
	}
	// Keep only the actual builtin administrative source. All other network data
	// must come from the local fixture server; no credential or internet needed.
	builtin := *catalog.Source("source.builtin.cn-admin")
	catalog.Sources = []datasource.Source{builtin}
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		t.Logf("fixture download %s", r.URL.Path)
		category := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".geojson")
		coordinates := any([][]float64{{109, 29}, {114, 34}})
		geometry := "LineString"
		if category == "settlement.county_seat" {
			geometry = "Point"
			coordinates = []float64{111.5, 31.5}
		}
		if category == "transport.railway" {
			coordinates = [][]float64{{109, 31.5}, {114, 31.5}}
		}
		if category == "hydro.water_polygon" {
			geometry = "Polygon"
			coordinates = [][][]float64{{{109, 29}, {114, 29}, {114, 34}, {109, 34}, {109, 29}}}
		}
		feature := func(coords any) any {
			return map[string]any{"type": "Feature", "properties": map[string]any{"name": "合成验收数据", "name:zh": "合成验收数据", "ref": "G107", "class": "motorway", "synthetic": true}, "geometry": map[string]any{"type": geometry, "coordinates": coords}}
		}
		features := []any{feature(coordinates)}
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "FeatureCollection", "features": features})
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	for _, category := range []string{"settlement.county_seat", "transport.railway", "transport.road.major", "transport.road.national", "hydro.river_line", "hydro.water_polygon"} {
		provide := datasource.Provide{Category: category, URL: server.URL + "/" + category + ".geojson", Fields: map[string]datasource.Strings{"name": {"name:zh", "name"}, "class": {"class"}, "ref": {"ref"}}}
		catalog.Sources = append(catalog.Sources, datasource.Source{ID: "user.fixture." + category, Name: "合成验收来源", Kind: "http_file", Version: "1", Compliance: "open", Status: "active", Hosts: []string{parsed.Host}, Provides: []datasource.Provide{provide}, License: "CC0 synthetic fixture", Attribution: "AutoCarto synthetic test fixture"})
	}
	route := filepath.Join(home, "route-fixture")
	if err = os.MkdirAll(route, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(route, "places.csv"), []byte("x,y\n110,30\n113,33\n"), 0600); err != nil {
		t.Fatal(err)
	}
	planning := env.AcquisitionEnvironment()
	planning.Catalog = catalog
	engine := acquire.Engine{Catalog: catalog, Store: acquire.NewStore(filepath.Join(home, "data/acquired"), 1<<30), Config: config.AcquisitionConfig{}.WithDefaults(), Invoke: invoke, Adapters: map[string]acquire.Adapter{"offline_asset": acquire.OfflineAdapter{Invoke: invoke}, "http_file": acquire.HTTPAdapter{Client: server.Client(), MaxBytes: 1 << 20}}}
	var firstPaths map[string]string
	var firstResults []acquire.ItemResult
	for _, mode := range []string{"auto", "fill"} {
		marker := map[string]any{"$acquire": true}
		inputFolder := ""
		if mode == "fill" {
			inputFolder = filepath.Join(home, "provided-folder")
			if err = os.MkdirAll(inputFolder, 0700); err != nil {
				t.Fatal(err)
			}
			original := firstPaths["铁路"]
			files, err := filepath.Glob(strings.TrimSuffix(original, filepath.Ext(original)) + ".*")
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				data, e := os.ReadFile(file)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(filepath.Join(inputFolder, filepath.Base(file)), data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			marker = map[string]any{"$fill": true, "path": inputFolder}
		}
		selections := map[string]any{}
		for _, item := range plan.Acquisition.Items {
			if item.Category != "admin.division" && item.Category != "admin.boundary_line" {
				selections[item.Entry] = "user.fixture." + item.Category
			}
		}
		marker["sources"] = selections
		inputs := map[string]any{"底图数据": marker, "路线数据集": route, "纸张": "A1 横向"}
		acquired, issues := acquire.Plan(ctx, plan, inputs, planning, nil)
		if len(issues) > 0 {
			t.Fatal(issues)
		}
		if len(acquired.Items) != 8 {
			t.Fatal("expected eight rows", acquired)
		}
		overrides := map[string]map[string]string{}
		rowPaths := map[string]string{}
		statuses := map[string]string{}
		for _, item := range acquired.Items {
			result, e := engine.RunItem(ctx, item, nil)
			if e != nil {
				t.Fatalf("%s/%s: %v %+v", mode, item.Entry, e, result)
			}
			if item.Mode != "user" && (len(result.Attempts) != 1 || result.SourceID != item.Chosen.SourceID) {
				t.Fatalf("fixture unexpectedly fell back: %+v", result)
			}
			statuses[item.Entry] = result.Status
			t.Logf("%s %s: %s %s", mode, item.Entry, result.Status, result.StoreKey)
			if len(result.Output.Paths) == 0 || !strings.HasSuffix(result.Output.Paths[0], ".shp") {
				t.Fatalf("primary output is not Shapefile: %+v", result.Output.Paths)
			}
			path := ""
			for _, candidate := range result.Output.Paths {
				if strings.HasSuffix(candidate, ".shp") {
					path = candidate
					break
				}
			}
			if path == "" {
				t.Fatalf("%s has no shapefile: %+v", item.Entry, result)
			}
			if overrides[item.Resource] == nil {
				overrides[item.Resource] = map[string]string{}
			}
			overrides[item.Resource][item.Entry] = path
			rowPaths[item.Entry] = path
			if mode == "auto" {
				firstResults = append(firstResults, result)
			}
		}
		if mode == "auto" {
			firstPaths = rowPaths
		} else if statuses["铁路"] != "user_provided" || rowPaths["铁路"] != filepath.Join(inputFolder, "铁路.shp") {
			t.Fatalf("fill lost user data: %+v %+v", statuses, rowPaths)
		}
		resolved := flow.ResolveInputResourcesWithOverrides(plan, inputs, overrides)
		if resolved.HasErrors() {
			t.Fatalf("resource binding: %+v", resolved.Diagnostics)
		}
		value, e := invoke(ctx, "verify_basemap", map[string]any{"data_list": resolved.Resources[0].ResolvedResource().Handle(), "style_set": resolved.Resources[1].ResolvedResource().Handle()})
		if e != nil {
			t.Fatal(e)
		}
		receipt := value.(map[string]any)
		if receipt["success"] != true {
			t.Fatalf("QGIS %s load/style failed: %+v", mode, receipt)
		}
		layers := receipt["layers"].(map[string]any)
		if len(layers) != 8 {
			t.Fatal("QGIS did not load eight layers", receipt)
		}
		for name, path := range rowPaths {
			layer := layers[name].(map[string]any)
			actualInfo, actualErr := os.Stat(layer["source"].(string))
			expectedInfo, expectedErr := os.Stat(path)
			if actualErr != nil || expectedErr != nil || !os.SameFile(actualInfo, expectedInfo) || layer["crs"] != "EPSG:4326" || layer["count"].(float64) < 1 {
				t.Fatalf("source/CRS/count mismatch: %s %+v", name, layer)
			}
		}
		if layers["县级城市驻地"].(map[string]any)["count"] != float64(1) {
			t.Fatal("single-point acquisition did not remain a single-point layer", layers["县级城市驻地"])
		}
		labels := map[string]string{"省域": "NAME", "县级城市驻地": "name", "河流线": "NAME"}
		applied := receipt["style"].(map[string]any)["data"].(map[string]any)["applied"].([]any)
		for _, raw := range applied {
			row := raw.(map[string]any)
			if expected := labels[row["layer"].(string)]; expected != "" && row["label_field"] != expected {
				t.Fatal("label field mismatch", row)
			}
		}
		reused := receipt["reuse"].(map[string]any)["data"].(map[string]any)["reused_count"]
		if reused != float64(8) {
			t.Fatal("list reload did not reuse actual sources", reused)
		}
	}
	if downloads.Load() != 6 {
		t.Fatalf("fill should reuse seven cached items and user railway, downloads=%d", downloads.Load())
	}
	for _, result := range firstResults {
		if result.StoreKey == "" || result.Status != "completed" {
			t.Fatal("missing validated cache record", result)
		}
		record, found, err := engine.Store.Lookup(ctx, result.StoreKey)
		if err != nil || !found || !record.Valid || record.License == "" || record.Attribution == "" || record.CreatedAt.IsZero() || len(record.RawFiles) == 0 || len(record.OutputFiles) < 5 {
			t.Fatalf("cache/provenance incomplete: %+v %v", record, err)
		}
		for _, file := range record.Files {
			if file.SHA256 == "" || file.Bytes <= 0 {
				t.Fatalf("file digest incomplete: %+v", file)
			}
		}
		if record.Metadata["fetch"] == nil || record.Metadata["validate"] == nil {
			t.Fatalf("fetch/validation evidence missing: %+v", record)
		}
	}
	t.Logf("F51: 8 auto layers + 8 fill layers loaded and styled; 6 loopback downloads; real builtin province extraction/shared boundary; sources and label fields preserved")
}
