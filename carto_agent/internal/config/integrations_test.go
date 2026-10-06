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
	if base.GEE.Enabled || base.Browser.Enabled || base.Web.Enabled {
		t.Fatal("integrations must be opt in")
	}
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
