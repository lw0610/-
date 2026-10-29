package config

import (
	"encoding/json"
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

func TestAcquisitionLoadsHistoricalSettingsWithoutExposingRemovedControls(t *testing.T) {
	var c Config
	if err := yaml.Unmarshal([]byte(`acquisition:
  max_download_mb: 256
  exclusive_agent: true
  agent_model: retired-model
  allow_shell: true
  budget:
    acquisition_timeout_seconds: 900
    acquisition_max_tool_calls: 80
    step_timeout_seconds: 600
    step_max_tool_calls: 60
    run_timeout_seconds: 3600
`), &c); err != nil {
		t.Fatal(err)
	}
	a := c.Acquisition.WithDefaults()
	if a.MaxDownloadMB != 256 {
		t.Fatal("existing acquisition settings changed", a)
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(a)
	if err != nil || strings.Contains(string(raw), "budget") || strings.Contains(string(raw), "max_tool_calls") || strings.Contains(string(raw), "exclusive_agent") || strings.Contains(string(raw), "agent_model") || strings.Contains(string(raw), "allow_shell") {
		t.Fatalf("removed execution settings were exposed again: %s, %v", raw, err)
	}
}

func TestAcquisitionDefaultsPreserveExplicitFalse(t *testing.T) {
	var c Config
	if err := yaml.Unmarshal([]byte("acquisition:\n  auto_fallback: false\nllm:\n  providers:\n    - name: image\n      vision: true\n"), &c); err != nil {
		t.Fatal(err)
	}
	a := c.Acquisition.WithDefaults()
	if a.FallbackEnabled() || a.MaxDownloadMB != 2048 || a.StoreMaxGB != 20 || a.Concurrency != 2 || a.PerHost != 1 {
		t.Fatalf("%+v", a)
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if !c.LLM.Providers[0].Vision {
		t.Fatal("vision lost")
	}
	raw, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var restored Config
	if err = yaml.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if !restored.LLM.Providers[0].Vision || restored.Acquisition.FallbackEnabled() {
		t.Fatal("configuration round trip lost values")
	}
}
