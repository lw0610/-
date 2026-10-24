package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/gorilla/mux"
)

func TestAgentConfigV2MigrationHTTPAndCompatibleSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old", "agent.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte("version: '1.0'\nruntime:\n  agents:\n    worker:\n      enabled: true\n      skill_packages: [data-loading, unknown-old]\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	m := config.NewAgentConfigManager(dir)
	handler := NewAPI(nil, m, nil, nil, "", nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)
	request := func(method, url, body string) *httptest.ResponseRecorder {
		t.Helper()
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest(method, url, bytes.NewBufferString(body)))
		if rr.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", method, url, rr.Code, rr.Body.String())
		}
		return rr
	}
	rr := request("POST", "/api/agent-configs/old/migrate", `{"dry_run":true}`)
	resp := decodeAPIResponse(t, rr)
	data := resp.Data.(map[string]any)
	if data["dry_run"] != true || data["changed"] != true || len(data["warnings"].([]any)) != 1 {
		t.Fatalf("%s", rr.Body.String())
	}
	current, _ := os.ReadFile(path)
	if !bytes.Equal(raw, current) {
		t.Fatal("dry run wrote config")
	}
	if _, err := os.Stat(path + ".v1.bak"); !os.IsNotExist(err) {
		t.Fatal("dry run made backup")
	}
	request("POST", "/api/agent-configs/old/migrate", `{"dry_run":false}`)
	backup, _ := os.ReadFile(path + ".v1.bak")
	if !bytes.Equal(backup, raw) {
		t.Fatal("missing original backup")
	}
	// Both the transitional v1 profile and direct v2 data reach the same saver.
	request("PUT", "/api/agent-configs/old", `{"runtime_agents":[{"name":"worker","agent":{"enabled":true,"skill_packages":["data-loading"],"prompts":{}}}],"runtime_shared":{"skill_packages":["vector-selection"]}}`)
	request("PUT", "/api/agent-configs/old", `{"version":"2.0","runtime":{"shared_tool_packages":["charts"],"agents":{"renamed":{"kind":"executor","executor":"route_knowledge","flow_only":true,"enabled":true}}},"flows":{"enabled":[]}}`)
	got, err := m.GetConfigData("old")
	if err != nil {
		t.Fatal(err)
	}
	if a := got.Runtime.Agents["renamed"]; a.Kind != "executor" || !a.FlowOnly {
		t.Fatalf("%+v", got)
	}
	if got.Flows == nil || len(got.Flows.Enabled) != 0 {
		t.Fatal("explicit no flows lost")
	}
	rr = request("GET", "/api/agent-configs/old/data", "")
	view := decodeAPIResponse(t, rr).Data.(map[string]any)
	if view["version"] != "2.0" || view["flows"] == nil {
		t.Fatalf("%s", rr.Body.String())
	}
}
