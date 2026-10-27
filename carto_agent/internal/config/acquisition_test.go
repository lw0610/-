package config

import (
	"gopkg.in/yaml.v3"
	"testing"
)

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
