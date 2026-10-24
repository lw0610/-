package api

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/gorilla/mux"
)

func TestRuntimeSnapshotExposesDirectoryDiagnostics(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "runtime")
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_HOME", home)
	t.Setenv("AUTOCARTO_DISABLE_KEYRING", "1")
	cfg := &config.Config{DataDir: filepath.Join(root, "old-project/data"), Log: config.LogConfig{Dir: filepath.Join(root, "actual-logs")}}
	service := config.NewConfigService(nil, cfg, "", filepath.Join(home, "config/config.yaml"))
	api := &API{configService: service, runtimeCoordinator: divergentRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{Services: map[string]middlewareclient.ServiceStatus{}}}}
	router := mux.NewRouter()
	api.SetupRoutes(router)
	request := httptest.NewRequest("GET", "/api/runtime/snapshot", nil)
	request.Header.Set("X-AutoCarto-Contract-Version", "v1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("runtime snapshot %d %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Paths config.RuntimeDirectoryDiagnostics `json:"runtime_paths"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	paths := envelope.Data.Paths
	if paths.RuntimeHome != home || paths.DataDir != cfg.DataDir || paths.LogFile != filepath.Join(root, "actual-logs/agent.log") || len(paths.Warnings) != 1 || paths.Warnings[0].Code != "DATA_DIR_OUTSIDE_RUNTIME_HOME" {
		t.Fatalf("diagnostics lost: %+v", paths)
	}
}
