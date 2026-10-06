package api

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
)

func TestIntegrationCheckRejectsUnknownService(t *testing.T) {
	a := &API{configService: config.NewConfigService(nil, &config.Config{}, "", filepath.Join(t.TempDir(), "config.yaml"))}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/integrations/check", strings.NewReader(`{"service":"shell"}`))
	a.handleCheckIntegration(w, r)
	if w.Code != 400 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
}

func TestIntegrationCheckReportsMissingCommandWithoutEnabling(t *testing.T) {
	cfg := &config.Config{}
	cfg.Integrations.SetDefaults()
	cfg.Integrations.Browser.Command = filepath.Join(t.TempDir(), "missing-node")
	a := &API{configService: config.NewConfigService(nil, cfg, "", filepath.Join(t.TempDir(), "config.yaml"))}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/integrations/check", strings.NewReader(`{"service":"browser"}`))
	a.handleCheckIntegration(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Success bool           `json:"success"`
			Result  map[string]any `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Data.Success {
		t.Fatalf("wrong diagnostic status: %s", w.Body)
	}
	if body.Data.Result["server_started"] != false || body.Data.Result["enabled"] != false {
		t.Fatalf("check started or enabled browser: %s", w.Body)
	}
}
