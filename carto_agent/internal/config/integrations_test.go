package config

import (
	"path/filepath"
	"testing"
)

func TestIntegrationsConfigRoundTripAndValidation(t *testing.T) {
	cfg := &Config{}
	cs := NewConfigService(nil, cfg, "", filepath.Join(t.TempDir(), "config.yaml"))
	base, err := cs.GetIntegrationsConfig()
	if err != nil {
		t.Fatal(err)
	}
	if base.OSM.Enabled || base.GEE.Enabled || base.Browser.Enabled || base.Web.Enabled {
		t.Fatal("integrations must be opt in")
	}
	if base.OSM.TimeoutSeconds != 120 || base.OSM.TotalTimeoutSeconds != 600 || base.OSM.MaxAttempts != 3 {
		t.Fatalf("OSM defaults: %+v", base.OSM)
	}
	base.OSM.TotalTimeoutSeconds = 1800
	base.OSM.MaxAttempts = 5
	base.GEE.Enabled = true
	base.GEE.Project = "test-project"
	base.Web.Enabled = true
	if _, err := cs.SetConfig("integrations", base, ""); err != nil {
		t.Fatal(err)
	}
	got, err := cs.GetIntegrationsConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !got.GEE.Enabled || got.GEE.Project != "test-project" || !got.Web.Enabled {
		t.Fatalf("round trip: %+v", got)
	}
	if got.OSM.TotalTimeoutSeconds != 1800 || got.OSM.MaxAttempts != 5 {
		t.Fatalf("OSM limits lost during round trip: %+v", got.OSM)
	}
	if err := cs.ReloadConfig(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := cs.GetIntegrationsConfig()
	if err != nil || reloaded.OSM.TotalTimeoutSeconds != 1800 || reloaded.OSM.MaxAttempts != 5 {
		t.Fatalf("OSM limits lost after persisted config reload: %+v, %v", reloaded.OSM, err)
	}
	got.Browser.Args[0] = "changed"
	fresh, _ := cs.GetIntegrationsConfig()
	if fresh.Browser.Args[0] == "changed" {
		t.Fatal("mutable config leaked")
	}
	bad := base
	bad.GEE.ProxyURL = "https://user:password@example.com"
	if _, err := cs.SetConfig("integrations", bad, ""); err == nil {
		t.Fatal("accepted credentials in proxy URL")
	}
	fresh, _ = cs.GetIntegrationsConfig()
	if fresh.GEE.ProxyURL != "" {
		t.Fatal("invalid mutation was applied")
	}
	if _, err := cs.SetConfig("integrations", map[string]any{"unknown": true}, ""); err == nil {
		t.Fatal("unknown config fields silently accepted")
	}
	if _, err := cs.SetConfig("integrations", nil, ""); err == nil {
		t.Fatal("nil accepted")
	}
	for _, limit := range []int{-1, 3601} {
		bad = base
		bad.GEE.TimeoutSeconds = limit
		if _, err := cs.SetConfig("integrations", bad, ""); err == nil {
			t.Fatalf("accepted timeout %d", limit)
		}
	}
}

func TestOSMLegacySettingsReceiveBoundedDownloadDefaults(t *testing.T) {
	cfg, err := decodeIntegrationsConfig(map[string]any{
		"osm": map[string]any{"enabled": true, "timeout_seconds": 90, "max_response_mb": 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OSM.Enabled || cfg.OSM.TimeoutSeconds != 90 || cfg.OSM.MaxResponseMB != 100 || cfg.OSM.TotalTimeoutSeconds != 600 || cfg.OSM.MaxAttempts != 3 {
		t.Fatalf("legacy preferences changed or new defaults missing: %+v", cfg.OSM)
	}
	// Valid explicit limits survive decoding instead of receiving defaults.
	for _, limits := range [][2]int{{1, 1}, {7200, 8}} {
		got, err := decodeIntegrationsConfig(map[string]any{"osm": map[string]any{"total_timeout_seconds": limits[0], "max_attempts": limits[1]}})
		if err != nil || got.OSM.TotalTimeoutSeconds != limits[0] || got.OSM.MaxAttempts != limits[1] {
			t.Fatalf("valid boundary rejected: %v, %+v, %v", limits, got.OSM, err)
		}
	}
}

func TestOSMDownloadLimitValidationDoesNotMutateSavedSettings(t *testing.T) {
	cs := NewConfigService(nil, &Config{}, "", filepath.Join(t.TempDir(), "config.yaml"))
	for _, value := range []map[string]any{
		{"total_timeout_seconds": -1}, {"total_timeout_seconds": 7201},
		{"max_attempts": -1}, {"max_attempts": 9}, {"max_attempts": 1.5},
	} {
		if _, err := cs.SetConfig("integrations", map[string]any{"osm": value}, ""); err == nil {
			t.Fatalf("invalid limits accepted: %v", value)
		}
		got, err := cs.GetIntegrationsConfig()
		if err != nil || got.OSM.TotalTimeoutSeconds != 600 || got.OSM.MaxAttempts != 3 {
			t.Fatalf("invalid update changed stored limits: %+v, %v", got.OSM, err)
		}
	}
}
