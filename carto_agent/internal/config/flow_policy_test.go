package config

import (
	"gopkg.in/yaml.v3"
	"testing"
)

func TestLegacyFlowPolicyIsIgnored(t *testing.T) {
	raw := `version: "2.0"
flow_policy:
  role_fill: strict
  auto_acquire: deny
  role_mapping:
    sample: {worker: missing-is-allowed}
runtime:
  agents:
    main: {kind: agent, display_name: main, is_main: true, enabled: true}
`
	var data AgentConfigData
	if err := yaml.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatal(err)
	}
	manager := NewAgentConfigManager(t.TempDir())
	if err := manager.SaveConfig("", "test", &data); err != nil {
		t.Fatal(err)
	}
	got, err := manager.GetConfigData("test")
	if err != nil {
		t.Fatal(err)
	}
	if got.Flows != nil || got.FlowPolicy != nil {
		t.Fatalf("legacy policy survived: %+v", got)
	}
	data.FlowPolicy["role_fill"] = "invalid"
	if err := manager.SaveConfig("", "test", &data); err != nil {
		t.Fatal("obsolete policy must not block saving runtime configuration", err)
	}
}
