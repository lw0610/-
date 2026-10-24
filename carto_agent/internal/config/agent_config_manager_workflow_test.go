package config

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"gopkg.in/yaml.v3"
)

func TestLegacyWorkflowInputIsRetiredBeforeRuntimeProjection(t *testing.T) {
	raw := `version: "1.0"
runtime:
  agents:
    worker:
      enabled: true
      workflow_package: data-loading-inspection-vector
      skill_packages: [data-loading]
      prompts: {static_instructions: saved rules, user_prompt_template: "{{ user_input }}"}
`
	var source AgentConfigFile
	if err := yaml.Unmarshal([]byte(raw), &source); err != nil {
		t.Fatal(err)
	}
	if source.Runtime.Agents["worker"].legacyWorkflowPackage != "data-loading-inspection-vector" {
		t.Fatal("legacy input was not recognized")
	}
	migrated := normalizeAgentConfigFile(&source)
	if len(migrated.MigrationWarnings) != 1 || migrated.MigrationWarnings[0].Code != "LEGACY_WORKFLOW_RETIRED" {
		t.Fatal(migrated.MigrationWarnings)
	}
	if migrated.Runtime.Agents["worker"].legacyWorkflowPackage != "" || migrated.Runtime.Agents["worker"].Prompts["static_instructions"] != "saved rules" {
		t.Fatal("retired selector leaked or editable rules lost")
	}
	runtime := ConvertToRuntimeSkillConfigWithPlugin(&source, t.TempDir(), "", "", filepath.Join(t.TempDir(), "config"), "custom")
	if runtime == nil || runtime.Agents["worker"].Kind != "agent" {
		t.Fatal("legacy selector changed runtime kind")
	}
	if _, exists := reflect.TypeFor[subagents.RuntimeSkillAgentConfig]().FieldByName("WorkflowPackage"); exists {
		t.Fatal("retired workflow runtime field still exists")
	}
	saved, err := yaml.Marshal(migrated)
	if err != nil || strings.Contains(string(saved), "workflow_package:") {
		t.Fatal("retired selector written as v2", err)
	}
	if source.Runtime.Agents["worker"].legacyWorkflowPackage == "" {
		t.Fatal("migration mutated source")
	}
}

func TestLegacyWorkflowJSONInputWarnsWithoutPublicExecutionField(t *testing.T) {
	var source AgentConfigFile
	if err := json.Unmarshal([]byte(`{"version":"1.0","runtime":{"agents":{"worker":{"enabled":true,"workflow_package":"old-pilot","skill_packages":["data-loading"]}}}}`), &source); err != nil {
		t.Fatal(err)
	}
	migrated := normalizeAgentConfigFile(&source)
	if len(migrated.MigrationWarnings) != 1 || migrated.MigrationWarnings[0].Package != "old-pilot" {
		t.Fatal(migrated.MigrationWarnings)
	}
	raw, err := json.Marshal(migrated)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	role := payload["runtime"].(map[string]any)["agents"].(map[string]any)["worker"].(map[string]any)
	if _, exists := role["workflow_package"]; exists {
		t.Fatal("legacy input was published as a role property")
	}
	if _, exists := reflect.TypeFor[AgentDefData]().FieldByName("WorkflowPackage"); exists {
		t.Fatal("public role still exposes retired selector")
	}
}
