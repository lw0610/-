package config

import (
	"gopkg.in/yaml.v3"
	"testing"
)

func TestFlowCompileDefaultsAndYAMLRoundTrip(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte("flow_compile:\n  provider: fast-model\n  concurrency: 2\n  call_timeout_seconds: 12\n  warm_enabled_flows: true\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.setDefaults()
	if cfg.FlowCompile.Provider != "fast-model" || cfg.FlowCompile.Concurrency != 2 || cfg.FlowCompile.CallTimeoutSeconds != 12 || cfg.FlowCompile.JobTimeoutSeconds != 900 || cfg.FlowCompile.RetryAfterSeconds != 600 || !cfg.FlowCompile.WarmEnabledFlows {
		t.Fatalf("defaults %+v", cfg.FlowCompile)
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var round Config
	if err = yaml.Unmarshal(raw, &round); err != nil || round.FlowCompile != cfg.FlowCompile {
		t.Fatalf("round trip: %+v %v", round.FlowCompile, err)
	}
	defaults := FlowCompileConfig{}.WithDefaults()
	if defaults.Concurrency != 3 || defaults.CallTimeoutSeconds != 90 || defaults.WarmEnabledFlows {
		t.Fatalf("zero defaults %+v", defaults)
	}
}
