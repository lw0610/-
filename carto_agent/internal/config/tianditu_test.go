package config

import "testing"

func TestTiandituKeyPrefersServerEnvironment(t *testing.T) {
	t.Setenv("TIANDITU_API_KEY", "  test-server-key  ")
	key, err := LoadTiandituAPIKey("")
	if err != nil || key != "test-server-key" {
		t.Fatalf("environment key not loaded: %v", err)
	}
}
