package config

import (
	"path/filepath"
	"testing"
)

func TestWebSearchProviderDefaultsAndPersistence(t *testing.T) {
	legacy, err := decodeIntegrationsConfig(map[string]any{"web": map[string]any{"enabled": false, "timeout_seconds": 90, "max_download_mb": 80}})
	if err != nil || legacy.Web.Enabled || legacy.Web.SearchProvider != "exa" || legacy.Web.TimeoutSeconds != 90 || legacy.Web.MaxDownloadMB != 80 {
		t.Fatalf("legacy preferences changed: %+v, %v", legacy.Web, err)
	}
	cs := NewConfigService(nil, &Config{}, "", filepath.Join(t.TempDir(), "config.yaml"))
	legacy.Web.SearchProvider = "parallel"
	if _, err := cs.SetConfig("integrations", legacy, ""); err != nil {
		t.Fatal(err)
	}
	if err := cs.ReloadConfig(); err != nil {
		t.Fatal(err)
	}
	got, err := cs.GetIntegrationsConfig()
	if err != nil || got.Web.SearchProvider != "parallel" || got.Web.Enabled {
		t.Fatalf("provider lost or service enabled: %+v, %v", got.Web, err)
	}
	legacy.Web.SearchProvider = "unsupported"
	if _, err := cs.SetConfig("integrations", legacy, ""); err == nil {
		t.Fatal("invalid provider accepted")
	}
	got, err = cs.GetIntegrationsConfig()
	if err != nil || got.Web.SearchProvider != "parallel" {
		t.Fatalf("invalid setting changed saved provider: %+v, %v", got.Web, err)
	}
}
