package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/gorilla/mux"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func acquisitionSettingsAPI(t *testing.T) (*API, *mux.Router, string) {
	t.Helper()
	a, _, _, home := realFlowAPI(t)
	cs := config.NewConfigService(nil, &config.Config{}, "", filepath.Join(home, "config", "config.yaml"))
	a.configService = cs
	a.settingsMutationService = control.NewSettingsMutationService(control.SettingsMutationServiceDeps{ConfigService: cs})
	router := mux.NewRouter()
	a.SetupRoutes(router)
	return a, router, home
}
func acquisitionRequest(t *testing.T, router *mux.Router, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("X-AutoCarto-Contract-Version", "v1")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}
func TestV4DataSourceSettingsAPICRUDAndReadOnly(t *testing.T) {
	_, router, home := acquisitionSettingsAPI(t)
	rr := acquisitionRequest(t, router, "GET", "/api/data-sources", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "categories") || !strings.Contains(rr.Body.String(), "需要在设置") {
		t.Fatal(rr.Code, rr.Body.String())
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "rail.geojson"), []byte(`{"type":"FeatureCollection","features":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	rr = acquisitionRequest(t, router, "POST", "/api/data-sources/scan", map[string]string{"path": root})
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(home, "sources")); !os.IsNotExist(err) {
		t.Fatal("scan wrote source directory")
	}
	source := datasource.Source{APIVersion: "data_source/v1", ID: "source.osm.overpass", Name: "本机铁路", Kind: "local_folder", Root: root, Provides: []datasource.Provide{{Category: "transport.railway", Path: "rail.geojson", Fields: map[string]datasource.Strings{"name": {"NAME"}}}}}
	rr = acquisitionRequest(t, router, "POST", "/api/data-sources", source)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var response struct {
		Data struct {
			Source datasource.Source `json:"source"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	id := response.Data.Source.ID
	if !strings.HasPrefix(id, "user.") {
		t.Fatal(id)
	}
	source.Name = "修改名称"
	rr = acquisitionRequest(t, router, "PUT", "/api/data-sources/"+id, source)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	source.Provides[0].Category = "unknown"
	rr = acquisitionRequest(t, router, "PUT", "/api/data-sources/"+id, source)
	if rr.Code != 400 {
		t.Fatal("unknown category", rr.Code)
	}
	rr = acquisitionRequest(t, router, "PUT", "/api/data-sources/source.osm.overpass", source)
	if rr.Code != 403 {
		t.Fatal("builtin editable", rr.Code)
	}
	rr = acquisitionRequest(t, router, "DELETE", "/api/data-sources/"+id, nil)
	if rr.Code != 400 {
		t.Fatal("deleted without confirmation", rr.Code)
	}
	rr = acquisitionRequest(t, router, "DELETE", "/api/data-sources/"+id+"?confirm=true", nil)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "rail.geojson")); err != nil {
		t.Fatal("source data removed")
	}
}
func TestV4DataStoreSettingsReferencesAndCleanup(t *testing.T) {
	a, router, home := acquisitionSettingsAPI(t)
	store, _, err := a.dataStore()
	if err != nil {
		t.Fatal(err)
	}
	if store.Root != filepath.Join(home, "data", "acquired") {
		t.Fatal(store.Root)
	}
	store.Now = func() time.Time { return time.Now().Add(-48 * time.Hour) }
	create := func(id string) acquire.StoreRecord {
		t.Helper()
		record, _, err := store.Materialize(context.Background(), acquire.CacheSpec{SourceID: id, Category: "transport.railway"}, 1, func(dir string) (acquire.Manifest, error) {
			if err := os.WriteFile(filepath.Join(dir, "out", "rail.geojson"), []byte(`{}`), 0600); err != nil {
				return acquire.Manifest{}, err
			}
			return acquire.Manifest{OutputFiles: []string{"out/rail.geojson"}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	used, unused := create("used"), create("unused")
	runPath := filepath.Join(home, "data", "sessions", "fixture", "session", "flows", "run-1", "run.json")
	if err := os.MkdirAll(filepath.Dir(runPath), 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"flow_run_id": "run-1", "acquisition": map[string]any{"items": []any{map[string]string{"store_key": used.Key}}}})
	if err := os.WriteFile(runPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	rr := acquisitionRequest(t, router, "GET", "/api/data-store", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "run-1") {
		t.Fatal(rr.Code, rr.Body.String())
	}
	rr = acquisitionRequest(t, router, "POST", "/api/data-store/clean-expired?confirm=true", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), unused.Key) || !strings.Contains(rr.Body.String(), used.Key) {
		t.Fatal(rr.Code, rr.Body.String())
	}
	records, err := store.List(context.Background())
	if err != nil || len(records) != 1 || records[0].Key != used.Key {
		t.Fatalf("%+v %v", records, err)
	}
	rr = acquisitionRequest(t, router, "DELETE", "/api/data-store/"+used.Key, nil)
	if rr.Code != 400 {
		t.Fatal("deleted without confirmation")
	}
	rr = acquisitionRequest(t, router, "DELETE", "/api/data-store/"+used.Key+"?confirm=true", nil)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(runPath); err != nil {
		t.Fatal("run history removed")
	}
}
func TestV4AcquisitionConfigSettingsHTTP(t *testing.T) {
	a, router, _ := acquisitionSettingsAPI(t)
	rr := acquisitionRequest(t, router, "GET", "/api/acquisition/config", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"web_search_enabled":false`) {
		t.Fatal(rr.Code, rr.Body.String())
	}
	cfg := a.configService.GetAcquisitionConfig()
	cfg.MaxDownloadMB = 128
	rr = acquisitionRequest(t, router, "PUT", "/api/acquisition/config", cfg)
	if rr.Code != 200 || a.configService.GetAcquisitionConfig().MaxDownloadMB != 128 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	cfg.MaxDownloadMB = -1
	rr = acquisitionRequest(t, router, "PUT", "/api/acquisition/config", cfg)
	if rr.Code != 400 || a.configService.GetAcquisitionConfig().MaxDownloadMB != 128 {
		t.Fatal(rr.Code, rr.Body.String())
	}
}
