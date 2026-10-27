package config

import (
	"path/filepath"
	"testing"
)

func TestV4AcquisitionSettingsUseConfigService(t *testing.T) {
	cs := NewConfigService(nil, &Config{}, "", filepath.Join(t.TempDir(), "config.yaml"))
	cfg := cs.GetAcquisitionConfig()
	if cfg.WebSearchEnabled || cfg.StoreMaxGB != 20 {
		t.Fatalf("defaults %+v", cfg)
	}
	cfg.MaxDownloadMB = 256
	cfg.StoreMaxGB = 2
	if _, err := cs.SetConfig("acquisition", cfg, ""); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []any{map[string]any{"max_download_mb": 0, "auto_fallback": false}, map[string]any{"per_host": 20}, map[string]any{"unknown": true}, map[string]any{"overpass_endpoint": "https://user:password@example.test/query"}, map[string]any{"overpass_endpoint": "file:///tmp/query"}, nil} {
		if _, err := cs.SetConfig("acquisition", bad, ""); err == nil {
			t.Fatalf("invalid settings accepted %+v", bad)
		}
	}
	got := cs.GetAcquisitionConfig()
	if got.MaxDownloadMB != 256 || !got.FallbackEnabled() {
		t.Fatalf("invalid mutation changed saved values %+v", got)
	}
	if _, err := cs.BatchSetConfig([]ConfigItem{{Path: "acquisition", Value: map[string]any{"store_max_gb": -1}}}); err == nil {
		t.Fatal("batch bypassed validation")
	}
	if got := cs.GetAcquisitionConfig(); got.StoreMaxGB != 2 {
		t.Fatal("bad batch applied")
	}
}
