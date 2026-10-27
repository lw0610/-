package config

import "testing"

func TestOSMConfigDefaultsAndValidation(t *testing.T) {
	var cfg IntegrationsConfig
	cfg.SetDefaults()
	if cfg.OSM.Enabled || cfg.OSM.TimeoutSeconds != 120 || cfg.OSM.MaxResponseMB != 200 {
		t.Fatal(cfg.OSM)
	}
	if e := cfg.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, address := range []string{"file:///tmp/osm", "https://u:p@example.com/osm", "https://example.com/?key=secret"} {
		cfg.OSM.Endpoints = []string{address}
		if cfg.Validate() == nil {
			t.Fatal(address)
		}
	}
	cfg.OSM.Endpoints = nil
	cfg.OSM.MaxResponseMB = 201
	if cfg.Validate() == nil {
		t.Fatal("unbounded response")
	}
}
