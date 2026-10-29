package config

import (
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

func TestCartographicPlanConfigAndRemovedCompilerFields(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte("flow_compile:\n  provider: old\nplan:\n  confirm: always\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Plan.Confirm != "always" || cfg.Plan.Validate() != nil {
		t.Fatal(cfg.Plan)
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil || strings.Contains(string(raw), "flow_compile") {
		t.Fatal(string(raw), err)
	}
	if (CartographicPlanConfig{}).WithDefaults().Confirm != "when_needed" {
		t.Fatal("wrong default")
	}
	for _, v := range []string{"always", "when_needed", "never"} {
		if err := (CartographicPlanConfig{Confirm: v}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if err := (CartographicPlanConfig{Confirm: "sometimes"}).Validate(); err == nil {
		t.Fatal("invalid policy accepted")
	}
}
