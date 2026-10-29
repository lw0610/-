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
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
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
	library, err := a.resourceStore()
	if err != nil {
		t.Fatal(err)
	}
	guides, err := flow.NewDefaultGuideStore(library)
	if err != nil {
		t.Fatal(err)
	}
	_, err = guides.Resolve(ctx, "marching-route", library)
	if err != nil {
		t.Fatal(err)
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
	var firstResults []dataservice.GetRow
	for _, mode := range []string{"auto", "fill"} {
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
		}
		userData := []dataservice.Binding{}
		if inputFolder != "" {
			userData = append(userData, dataservice.Binding{Name: "铁路", Category: "transport.railway", Path: filepath.Join(inputFolder, filepath.Base(firstPaths["铁路"]))})
		}
		service := dataservice.New(dataservice.Options{SessionID: "qgis-" + mode, SessionDir: filepath.Join(home, "sessions", mode), Environment: planning, Engine: &engine, UserData: userData, Config: config.AcquisitionConfig{NetworkPolicy: "direct", FallbackPolicy: "deny"}, NetworkConfirmed: true})
		// The acceptance fixture explicitly requests each real data need. The flow
		// document is context only; it never supplies a parsed acquisition list.
		needs := []dataservice.Need{
			{Name: "省域", What: dataservice.What{Category: "省域"}, Fields: map[string]string{"NAME": "name"}},
			{Name: "县级城市驻地", What: dataservice.What{Category: "settlement.county_seat"}, Fields: map[string]string{"name": "name"}},
			{Name: "铁路", What: dataservice.What{Category: "transport.railway"}},
			{Name: "公路", What: dataservice.What{Category: "transport.road.major"}},
			{Name: "国道", What: dataservice.What{Category: "transport.road.national"}},
			{Name: "地区界线", What: dataservice.What{Category: "地区界线"}},
			{Name: "河流线", What: dataservice.What{Category: "hydro.river_line"}, Fields: map[string]string{"NAME": "name"}},
			{Name: "河流面", What: dataservice.What{Category: "hydro.water_polygon"}},
		}
		acquired := dataservice.GetResult{}
		for _, need := range needs {
			need.Extent = "bbox:29.4,109.4,33.6,113.6"
			found, e := service.Find(ctx, dataservice.FindRequest{Guide: "marching-route", Need: need})
			if e != nil {
				t.Fatal(e)
			}
			if len(found.Rows) != 1 {
				t.Fatalf("expected one named need: %+v", found)
			}
			choices := map[string]string{}
			for _, row := range found.Rows {
				chosen := row.Recommended
				for _, candidate := range row.Candidates {
					if candidate.Available && (candidate.Kind == "user" && !candidate.RequiresConfirmation && (need.Name == "铁路") || candidate.Kind == "cache") {
						chosen = candidate.ID
						break
					}
					if candidate.Available && candidate.SourceID == "user.fixture."+row.Category {
						chosen = candidate.ID
					}
				}
				if chosen == "" {
					t.Fatalf("no candidate: %+v", row)
				}
				choices[row.Row] = chosen
			}
			got, e := service.Get(ctx, dataservice.GetRequest{FindID: found.FindID, Choice: choices})
			if e != nil {
				t.Fatalf("%s %s: %v %+v", mode, need.Name, e, got)
			}
			acquired.Rows = append(acquired.Rows, got.Rows...)
		}
		rowPaths := map[string]string{}
		statuses := map[string]string{}
		for _, row := range acquired.Rows {
			path := ""
			for _, candidate := range row.Paths {
				if strings.HasSuffix(resource.DataSourcePath(candidate), ".shp") || strings.HasSuffix(resource.DataSourcePath(candidate), ".gpkg") {
					path = candidate
					break
				}
			}
			if path == "" {
				t.Fatalf("%s no vector file: %+v", row.Row, row)
			}
			rowPaths[row.Row] = path
			statuses[row.Row] = row.Kind
			if mode == "auto" {
				firstResults = append(firstResults, row)
			}
		}
		if mode == "auto" {
			firstPaths = rowPaths
		} else if statuses["铁路"] != "user" || resource.DataSourcePath(rowPaths["铁路"]) != filepath.Join(inputFolder, filepath.Base(firstPaths["铁路"])) {
			t.Fatalf("fill lost user data: %+v %+v", statuses, rowPaths)
		}
		items := []map[string]any{}
		styles := []map[string]any{}
		styleNames := map[string]string{"省域": "省域面", "县级城市驻地": "县级城市驻地", "铁路": "铁路", "公路": "高速公路", "国道": "国道", "地区界线": "省界", "河流线": "河流线", "河流面": "河流面"}
		for _, need := range needs {
			items = append(items, map[string]any{"name": need.Name, "path": rowPaths[need.Name]})
			resolved := library.Resolve(ctx, resource.Ref{Type: "style", Name: styleNames[need.Name]}, resource.Scope{})
			if resource.HasErrors(resolved.Problems) {
				t.Fatalf("style %s: %+v", need.Name, resolved.Problems)
			}
			item := map[string]any{"layer": need.Name, "style": resolved.Handle()}
			if len(need.Fields) > 0 {
				for field := range need.Fields {
					item["label_field"] = field
				}
			} else {
				item["label_field"] = "无"
			}
			styles = append(styles, item)
		}
		value, e := invoke(ctx, "verify_basemap", map[string]any{"items": items, "styles": styles})
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
			actualInfo, actualErr := os.Stat(resource.DataSourcePath(layer["source"].(string)))
			expectedInfo, expectedErr := os.Stat(resource.DataSourcePath(path))
			if actualErr != nil || expectedErr != nil || !os.SameFile(actualInfo, expectedInfo) || layer["crs"] != "EPSG:4326" || layer["count"].(float64) < 1 {
				t.Fatalf("source/CRS/count mismatch: %s %+v", name, layer)
			}
		}
		if layers["县级城市驻地"].(map[string]any)["count"] != float64(1) {
			t.Fatal("single-point acquisition did not remain a single-point layer", layers["县级城市驻地"])
		}
		labels := map[string]string{"省域": "NAME", "县级城市驻地": "name", "河流线": "NAME"}
		applied := receipt["style"].(map[string]any)["data"].(map[string]any)["items"].([]any)
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
		for path, sha := range result.SHA256 {
			t.Logf("F51 output name=%s file=%s sha256=%s", result.Row, filepath.Base(path), sha)
		}
		if result.StoreKey == "" {
			t.Fatal("missing validated cache record", result)
		}
		record, found, err := engine.Store.Lookup(ctx, result.StoreKey)
		if err != nil || !found || !record.Valid || record.License == "" || record.Attribution == "" || record.CreatedAt.IsZero() || len(record.RawFiles) == 0 || len(record.OutputFiles) < 1 {
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
