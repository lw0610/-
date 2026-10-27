package config

import (
	"gopkg.in/yaml.v3"
	"testing"
)

func TestV4FlowPolicyRoundTrip(t *testing.T) {
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
	if got.Flows != nil || got.FlowPolicy == nil || got.FlowPolicy.RoleMapping["sample"]["worker"] != "missing-is-allowed" {
		t.Fatalf("%+v", got)
	}
	data.FlowPolicy.RoleFill = "invalid"
	if err := manager.SaveConfig("", "test", &data); err == nil {
		t.Fatal("invalid enum accepted")
	}
}
