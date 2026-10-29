package dataservice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
)

func tileFixture(t *testing.T) (*Service, FindRequest) {
	t.Helper()
	s, _ := fixture(t)
	s.options.Environment.Catalog.Categories = []datasource.Category{{ID: "basemap.tiles", Name: "在线底图", Geometry: "tiles", Validate: datasource.Validation{MinFeatures: 1}}}
	s.options.Environment.Catalog.Sources = []datasource.Source{{ID: "test.official", Name: "测试官方底图", Kind: "tianditu", Provides: []datasource.Provide{{Category: "basemap.tiles"}}}}
	s.options.Engine.Adapters["tianditu"] = acquire.TiandituAdapter{}
	request := FindRequest{Need: Need{Name: "底图", What: What{Category: "在线底图"}, Extent: "bbox:24,109,27,112", Scale: "auto"}}
	return s, request
}

func TestOnlineTileDefinitionsExcludedBeforeAndAfterCache(t *testing.T) {
	s, request := tileFixture(t)
	ctx := context.Background()
	for _, cached := range []bool{false, true} {
		found, err := s.Find(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if len(found.Rows) != 1 || found.Rows[0].Recommended != "" || len(found.Rows[0].Candidates) != 0 {
			t.Fatalf("online definition advertised as usable data: %+v", found)
		}
		var reason string
		for _, excluded := range found.Rows[0].Excluded {
			if excluded.SourceID == "test.official" {
				reason = excluded.Reason
			}
		}
		if !strings.Contains(reason, "尚不支持安全加载") || !strings.Contains(reason, "密钥写入 QGIS 工程") {
			t.Fatalf("missing actionable exclusion: %q", reason)
		}
		query, err := s.load(found.FindID)
		if err != nil {
			t.Fatal(err)
		}
		item := query.Items["底图"]
		if item.Chosen == nil || item.Chosen.CacheHit != cached {
			t.Fatalf("cache fixture not exercised: %+v", item.Chosen)
		}
		// The real adapter prepares only a definition, without a key or network.
		prepared, err := s.options.Engine.RunItem(ctx, item, nil)
		if err != nil || prepared.Status != "waiting_user" || prepared.Output.LayerDefinition == nil {
			t.Fatalf("unexpected engine contract: %+v, %v", prepared, err)
		}
		// A previously pinned selection must also fail safely at execution time.
		selection := Selection{FindID: found.FindID, Request: request, Rows: []SelectedRow{{Row: "底图", Item: item, Candidate: Candidate{SourceID: "test.official", Kind: "official_api", Choice: item.Chosen}}}}
		result, err := s.GetSelection(ctx, selection, GetRequest{})
		if err == nil || !strings.Contains(err.Error(), "DATA_ONLINE_TILES_UNSUPPORTED") || result.Status != "failed" {
			t.Fatalf("definition accepted: %+v, %v", result, err)
		}
		assertNoTileDataPublished(t, s, result)
	}
}

func assertNoTileDataPublished(t *testing.T, s *Service, result GetResult) {
	t.Helper()
	if len(result.Rows) != 0 || result.ProvenancePath != "" {
		t.Fatalf("unloaded definition entered a result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(s.options.SessionDir, "output")); !os.IsNotExist(err) {
		t.Fatalf("unloaded definition published session output: %v", err)
	}
	index, err := sessionfiles.NewWithRoots(s.options.SessionID, s.options.SessionDir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := index.List(false)
	if err != nil || len(files) != 0 {
		t.Fatalf("unloaded definition registered as data: %+v, %v", files, err)
	}
	entries, err := s.options.Memory.List()
	if err != nil || len(entries) != 0 {
		t.Fatalf("unloaded definition remembered: %+v, %v", entries, err)
	}
}

type definitionAdapter struct{}

func (definitionAdapter) Kind() string { return "local_folder" }
func (definitionAdapter) Fetch(context.Context, acquire.FetchRequest) (acquire.FetchResult, error) {
	return acquire.FetchResult{Format: "online_layer", LayerDefinition: map[string]any{"type": "test_xyz", "requires_project_key_confirmation": true}}, nil
}

func TestGetRejectsUnexpectedLayerDefinitionFromAdapter(t *testing.T) {
	s, calls := fixture(t)
	s.options.Engine.Adapters["local_folder"] = definitionAdapter{}
	found, err := s.Find(context.Background(), riverRequest())
	if err != nil || len(found.Rows) != 1 || found.Rows[0].Recommended == "" {
		t.Fatalf("%+v, %v", found, err)
	}
	result, err := s.Get(context.Background(), GetRequest{FindID: found.FindID})
	if err == nil || !strings.Contains(err.Error(), "DATA_ONLINE_TILES_UNSUPPORTED") || result.Status != "failed" {
		t.Fatalf("unexpected adapter definition accepted: %+v, %v", result, err)
	}
	if calls.Load() != 0 {
		t.Fatal("unconfirmed definition was loaded or normalized")
	}
	assertNoTileDataPublished(t, s, result)
}

func TestOnlineTileExclusionKeepsDownloadedRasterAndExistingLayer(t *testing.T) {
	s, request := tileFixture(t)
	path := filepath.Join(t.TempDir(), "downloaded.tif")
	if err := os.WriteFile(path, []byte{'I', 'I', 42, 0, 8, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	s.options.UserData = []Binding{{Name: "底图", Path: path}}
	s.options.Layers = func(context.Context) ([]Binding, error) {
		return []Binding{{Name: "底图", LayerID: "existing-raster-layer", Geometry: "raster"}}, nil
	}
	s.options.Engine.Invoke = func(_ context.Context, name string, args map[string]any) (any, error) {
		if name != "data_normalize" || args["expected_geometry"] != "raster" {
			t.Fatalf("unexpected operation: %s %+v", name, args)
		}
		return map[string]any{"success": true, "data": map[string]any{"geometry_type": "raster", "count": 1, "crs": "EPSG:4326", "extent_wgs84": []float64{109, 24, 112, 27}}}, nil
	}
	found, err := s.Find(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Rows) != 1 || len(found.Rows[0].Candidates) != 2 {
		t.Fatalf("existing data incorrectly excluded: %+v", found)
	}
	for _, candidate := range found.Rows[0].Candidates {
		result, err := s.Get(context.Background(), GetRequest{FindID: found.FindID, Choice: map[string]string{"底图": candidate.ID}})
		if err != nil || result.Status != "completed" || len(result.Rows) != 1 {
			t.Fatalf("existing %s unusable: %+v, %v", candidate.Kind, result, err)
		}
		row := result.Rows[0]
		if candidate.Kind == "project_layer" {
			if row.LayerID != "existing-raster-layer" || len(row.Paths) != 0 {
				t.Fatalf("real layer reference lost: %+v", row)
			}
		} else if len(row.Paths) != 1 || row.Paths[0] != path {
			t.Fatalf("downloaded raster not retained: %+v", row)
		}
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != string([]byte{'I', 'I', 42, 0, 8, 0, 0, 0}) {
		t.Fatalf("user raster changed: %q, %v", raw, err)
	}
}
