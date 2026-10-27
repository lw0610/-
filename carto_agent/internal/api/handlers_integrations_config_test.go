package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/gorilla/mux"
)

// Exercise the same HTTP batch payload as the settings card, through the real
// control-plane mutation service and file-backed ConfigService. Saving these
// preferences must not start a browser or require a working Node installation.
func TestIntegrationsBatchConfigSaveReadAndReload(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{}
	cfg.Integrations.SetDefaults()
	service := config.NewConfigService(nil, cfg, "", userPath)
	mutations := control.NewSettingsMutationService(control.SettingsMutationServiceDeps{ConfigService: service})
	api := NewAPIWithServices(service, nil, nil, nil, nil, mutations, nil, nil)
	router := mux.NewRouter()
	api.SetupRoutes(router)

	// Include every field produced by IntegrationsConfigWorkspace.toConfig,
	// including disabled GEE, empty strings and a browser argv array.
	integrations := map[string]any{
		"osm": map[string]any{
			"enabled": false, "endpoints": []string{}, "nominatim_url": "", "proxy_url": "",
			"timeout_seconds": 90, "total_timeout_seconds": 1800, "max_attempts": 5, "max_response_mb": 100,
		},
		"gee": map[string]any{
			"enabled": false, "python": "python3", "project": "",
			"proxy_url": "", "timeout_seconds": 300,
		},
		"browser": map[string]any{
			"enabled": true, "command": "npx", "args": []string{"-y", "@playwright/mcp@0.0.83"},
			"browser": "chrome", "headless": true, "timeout_seconds": 60,
		},
		"web": map[string]any{
			"enabled": false, "timeout_seconds": 60, "max_download_mb": 100,
		},
	}
	body, err := json.Marshal(map[string]any{"items": []any{map[string]any{"path": "integrations", "value": integrations}}})
	if err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(http.MethodPut, "/api/config/batch", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("save status=%d: %s", recorder.Code, recorder.Body.String())
		}
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Persisted      bool     `json:"persisted"`
				Idempotent     bool     `json:"idempotent"`
				ConfigRevision string   `json:"config_revision"`
				AffectedPaths  []string `json:"affected_paths"`
			} `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if !response.Success || response.Data.ConfigRevision == "" || len(response.Data.AffectedPaths) != 1 || response.Data.AffectedPaths[0] != "integrations" {
			t.Fatalf("invalid mutation response: %s", recorder.Body.String())
		}
		if attempt == 0 && !response.Data.Persisted {
			t.Fatal("first save did not persist")
		}
		if attempt == 1 && (!response.Data.Idempotent || response.Data.Persisted) {
			t.Fatal("identical save was not idempotent")
		}
	}

	for _, path := range []string{"/api/config/integrations", "/api/config"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("read %s status=%d", path, recorder.Code)
		}
		var response struct {
			Success bool            `json:"success"`
			Data    json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if !response.Success || len(response.Data) == 0 {
			t.Fatalf("read %s missing configuration", path)
		}
		data := response.Data
		if path == "/api/config" {
			var all map[string]json.RawMessage
			if err := json.Unmarshal(data, &all); err != nil {
				t.Fatal(err)
			}
			data = all["integrations"]
		}
		var got config.IntegrationsConfig
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		assertSavedBrowserIntegration(t, got)
	}

	// A fresh service must see the persisted value after defaults are merged.
	reloaded := config.NewConfigService(nil, &config.Config{}, "", userPath)
	if err := reloaded.ReloadConfig(); err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.GetIntegrationsConfig()
	if err != nil {
		t.Fatal(err)
	}
	assertSavedBrowserIntegration(t, got)
}

func assertSavedBrowserIntegration(t *testing.T, got config.IntegrationsConfig) {
	t.Helper()
	if !got.Browser.Enabled || !got.Browser.Headless || got.Browser.Command != "npx" || got.Browser.Browser != "chrome" || got.Browser.TimeoutSeconds != 60 {
		t.Fatalf("browser preferences were not retained: %+v", got.Browser)
	}
	if len(got.Browser.Args) != 2 || got.Browser.Args[1] != "@playwright/mcp@0.0.83" {
		t.Fatalf("browser argv was not retained: %v", got.Browser.Args)
	}
	if got.OSM.Enabled || got.GEE.Enabled || got.Web.Enabled {
		t.Fatal("saving browser preferences enabled unrelated integrations")
	}
	if got.OSM.TimeoutSeconds != 90 || got.OSM.TotalTimeoutSeconds != 1800 || got.OSM.MaxAttempts != 5 || got.OSM.MaxResponseMB != 100 {
		t.Fatalf("OSM download limits were not retained: %+v", got.OSM)
	}
}
