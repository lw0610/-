package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/osmdata"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/autocarto/carto_agent/internal/skill"
)

func osmClipFixture(t *testing.T) (osmdata.Options, map[string]any, string, *atomic.Int32) {
	t.Helper()
	root := t.TempDir()
	boundary := filepath.Join(root, "boundary.geojson")
	if err := os.WriteFile(boundary, []byte(`{"type":"Polygon","coordinates":[[[114,30],[115,30],[115,31],[114,31],[114,30]]]}`), 0600); err != nil {
		t.Fatal(err)
	}
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		query := r.FormValue("data")
		if strings.Contains(query, "out geom;") {
			downloads.Add(1)
		}
		fmt.Fprint(w, `{"elements":[{"type":"way","id":9007199254740993,"tags":{"highway":"motorway","name":"原始名称","ref":"G1"},"geometry":[{"lon":113,"lat":30.5},{"lon":114.5,"lat":30.5},{"lon":116,"lat":30.5}]}]}`)
	}))
	t.Cleanup(server.Close)
	return osmdata.Options{Enabled: true, Authorized: true, Endpoints: []string{server.URL}, OutputDir: filepath.Join(root, "output"), TotalTimeoutSeconds: 20}, map[string]any{"extents": []string{"file:" + boundary}, "tags": []string{"highway=motorway"}, "types": []string{"way"}, "fields": []string{"ref"}, "output": "roads.geojson"}, boundary, &downloads
}

// This mock does not implement geometry clipping. The deterministic fixture's
// expected clipped coordinates let us test orchestration and publication while
// the Python suite tests actual QGIS/GEOS intersections independently.
func fixtureGIS(t *testing.T, empty bool, mutate func(map[string]any)) GISInvoker {
	t.Helper()
	return func(_ context.Context, name string, args map[string]any) (any, error) {
		if name != "data_normalize" || args["simplify_tolerance_m"] != 0 || len(args["fields"].(map[string]any)) != 0 || len(args["standard_fields"].([]string)) != 0 {
			t.Fatalf("lossy normalization contract: %s %+v", name, args)
		}
		input := args["input"].(map[string]any)["path"].(string)
		features, err := osmdata.ReadFeatures(input)
		if err != nil {
			t.Fatal(err)
		}
		inputCount := len(features)
		fields := map[string]any{}
		for _, feature := range features {
			for field := range feature.Properties {
				fields[field] = 1
			}
		}
		for i := range features {
			features[i].Geometry = osmdata.Geometry{Type: "MultiLineString", Coordinates: [][][]float64{{{114, 30.5}, {115, 30.5}}}}
		}
		if empty {
			features = []osmdata.Feature{}
		}
		raw, err := json.Marshal(map[string]any{"type": "FeatureCollection", "features": features})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(args["output"].(string), raw, 0600); err != nil {
			t.Fatal(err)
		}
		boundary := args["clip"].(map[string]any)["path"].(string)
		data := map[string]any{"clip_applied": true, "clip_path": boundary, "clip_kind": "polygon", "crs": "EPSG:4326", "count": len(features), "clip_input_features": inputCount, "clip_output_features": len(features), "fields": fields}
		if mutate != nil {
			mutate(data)
		}
		return map[string]any{"success": true, "output": map[string]any{"success": true, "data": data}}, nil
	}
}

func TestOSMClipPublishesOnlyFinalFilesAndPreservesFields(t *testing.T) {
	options, args, boundary, downloads := osmClipFixture(t)
	var progress []osmdata.Progress
	ctx := osmdata.WithProgressCallback(context.Background(), func(p osmdata.Progress) { progress = append(progress, p) })
	result, err := invokeOSM(ctx, "osm_get_features", args, options, fixtureGIS(t, false, nil))
	if err != nil || !result.Success || !result.Clipped || result.ClipStatus != "complete" || result.FeatureCount != 1 || len(result.ArtifactPaths) != 2 || downloads.Load() != 1 {
		t.Fatalf("unexpected final result: %+v %v, downloads=%d", result, err, downloads.Load())
	}
	canonicalBoundary, _ := canonicalOSMPath(boundary)
	if result.ClipBoundary != canonicalBoundary || fmt.Sprint(result.Bounds) != "[114 30.5 115 30.5]" || result.GeometryCounts["MultiLineString"] != 1 {
		t.Fatalf("incorrect clipped metadata: %+v", result)
	}
	features, err := osmdata.ReadFeatures(filepath.Join(options.OutputDir, "roads.geojson"))
	if err != nil || fmt.Sprint(features[0].Properties["osm_id"]) != "9007199254740993" || features[0].Properties["ref"] != "G1" {
		t.Fatalf("lost original properties: %+v %v", features, err)
	}
	raw, _ := os.ReadFile(filepath.Join(options.OutputDir, "roads.geojson.source.json"))
	var source map[string]any
	_ = json.Unmarshal(raw, &source)
	if source["clipped"] != true || source["clip_status"] != "complete" || source["feature_count"] != float64(1) {
		t.Fatalf("incorrect source metadata: %s", raw)
	}
	entries, _ := os.ReadDir(options.OutputDir)
	if len(entries) != 2 {
		t.Fatalf("temporary or duplicate output leaked: %+v", entries)
	}
	for _, event := range progress {
		for _, path := range event.ArtifactPaths {
			if strings.Contains(path, ".osm-clip-") {
				t.Fatalf("temporary artifact leaked through progress: %+v", event)
			}
		}
	}
	if progress[len(progress)-1].Phase != "completed" {
		t.Fatal("final success was not emitted")
	}
}

func TestOSMClipEmptyIntersectionPublishesValidEmptyCollection(t *testing.T) {
	options, args, _, _ := osmClipFixture(t)
	result, err := invokeOSM(context.Background(), "osm_get_features", args, options, fixtureGIS(t, true, nil))
	if err != nil || !result.Clipped || result.FeatureCount != 0 {
		t.Fatalf("empty intersection failed: %+v %v", result, err)
	}
	features, err := osmdata.ReadFeatures(filepath.Join(options.OutputDir, "roads.geojson"))
	if err != nil || features == nil || len(features) != 0 {
		t.Fatalf("invalid empty collection: %+v %v", features, err)
	}
}

func TestOSMClipFailurePreservesRawWithoutRedownload(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		t.Run(fmt.Sprint(overwrite), func(t *testing.T) {
			options, args, _, downloads := osmClipFixture(t)
			args["overwrite"] = overwrite
			target := filepath.Join(options.OutputDir, "roads.geojson")
			if overwrite {
				_ = os.MkdirAll(options.OutputDir, 0700)
				_ = os.WriteFile(target, []byte("existing final data"), 0600)
				_ = os.WriteFile(target+".source.json", []byte("existing metadata"), 0600)
			}
			result, err := invokeOSM(context.Background(), "osm_get_features", args, options, fixtureGIS(t, false, func(data map[string]any) { data["clip_applied"] = false }))
			if err == nil || result.Success || result.Clipped || result.ClipStatus != "failed" || downloads.Load() != 1 || !strings.Contains(err.Error(), "无需重新下载") {
				t.Fatalf("incorrect recovery: %+v %v", result, err)
			}
			if overwrite {
				raw, _ := os.ReadFile(target)
				metadata, _ := os.ReadFile(target + ".source.json")
				if string(raw) != "existing final data" || string(metadata) != "existing metadata" || !isOSMStagingPath(options.OutputDir, result.ArtifactPaths[0]) {
					t.Fatalf("existing output overwritten or recovery missing: %s %s %+v", raw, metadata, result)
				}
			} else {
				if _, err := osmdata.ReadFeatures(target); err != nil {
					t.Fatal("initial failed clip lost raw download", err)
				}
				metadata, _ := os.ReadFile(target + ".source.json")
				if !strings.Contains(string(metadata), `"clipped": false`) || !strings.Contains(string(metadata), `"clip_status": "failed"`) {
					t.Fatal(string(metadata))
				}
			}
			for _, path := range result.ArtifactPaths {
				if _, err := os.Stat(path); err != nil {
					t.Fatal("recovery artifact missing", path, err)
				}
			}
		})
	}
}

func TestOSMClipRejectsBoundaryChangeDuringGIS(t *testing.T) {
	options, args, boundary, downloads := osmClipFixture(t)
	result, err := invokeOSM(context.Background(), "osm_get_features", args, options, fixtureGIS(t, false, func(data map[string]any) {
		file, _ := os.OpenFile(boundary, os.O_APPEND|os.O_WRONLY, 0600)
		_, _ = file.WriteString(" ")
		_ = file.Close()
	}))
	if err == nil || result.Clipped || downloads.Load() != 1 || !strings.Contains(err.Error(), "边界发生变化") {
		t.Fatalf("boundary mutation accepted: %+v %v", result, err)
	}
}

func TestOSMClipDoesNotInferBoundaryFromBBox(t *testing.T) {
	options, args, _, _ := osmClipFixture(t)
	args["extents"] = []string{"bbox:30,114,31,115"}
	result, err := invokeOSM(context.Background(), "osm_get_features", args, options, func(context.Context, string, map[string]any) (any, error) {
		t.Fatal("invented exact boundary from rectangle")
		return nil, nil
	})
	if err != nil || result.Clipped {
		t.Fatalf("bbox claimed clipping: %+v %v", result, err)
	}
}

func TestOSMClipDoesNotGuessMultipleBoundaryUnion(t *testing.T) {
	options, args, boundary, _ := osmClipFixture(t)
	args["extents"] = []string{"file:" + boundary, "file:" + boundary}
	result, err := invokeOSM(context.Background(), "osm_get_features", args, options, func(context.Context, string, map[string]any) (any, error) {
		t.Fatal("multiple boundary clipping was inferred")
		return nil, nil
	})
	if err != nil || result.Clipped {
		t.Fatalf("multiple boundaries claimed clipping: %+v %v", result, err)
	}
}

func TestOSMNormalizeDataRejectsNestedFailure(t *testing.T) {
	failure := map[string]any{"success": false, "info": "fixture failure", "clip_applied": true}
	for _, payload := range []any{failure, map[string]any{"output": failure}, map[string]any{"result": failure}, map[string]any{"data": failure}} {
		if _, err := osmNormalizeData(payload); err == nil {
			t.Fatalf("nested failure accepted: %+v", payload)
		}
	}
}

func TestOSMClipFailedOverwriteDoesNotRegisterRecoveryStage(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	options, args, _, _ := osmClipFixture(t)
	args["overwrite"] = true
	project := filepath.Join(t.TempDir(), "project.qgz")
	sessionID := "clip-recovery"
	root, err := config.ResolveSessionDir(project, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "output", "osm")
	_ = os.MkdirAll(output, 0700)
	_ = os.WriteFile(filepath.Join(output, "roads.geojson"), []byte(`{"type":"FeatureCollection","features":[]}`), 0600)
	ctx := skill.WithSessionContext(context.Background(), skill.SessionContext{ProjectPath: project, SessionID: sessionID})
	value, err := Invoke(ctx, "osm_get_features", args, fixedConfig{config.IntegrationsConfig{OSM: config.OSMConfig{Enabled: true, Endpoints: options.Endpoints, TotalTimeoutSeconds: 20, MaxAttempts: 1}}}, fixtureGIS(t, false, func(data map[string]any) { data["clip_applied"] = false }))
	result, ok := value.(*osmdata.Result)
	if err == nil || !ok || len(result.ArtifactPaths) == 0 || !isOSMStagingPath(output, result.ArtifactPaths[0]) {
		t.Fatalf("expected diagnostic recovery files: %+v %v", value, err)
	}
	store, err := sessionfiles.New(project, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		raw, _ := json.Marshal(item)
		if strings.Contains(string(raw), ".osm-clip-") {
			t.Fatalf("recovery stage registered as delivered file: %s", raw)
		}
	}
}
