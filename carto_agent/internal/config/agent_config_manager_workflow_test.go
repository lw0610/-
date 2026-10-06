package config

import (
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAgentWorkflowPackageParsesAndConvertsToRuntimeSkillConfig(t *testing.T) {
	cfg := `version: "1.0"
runtime:
  global:
    skill_packages: [project-state]
  agents:
    orchestrator:
      display_name: 编排智能体
      description: 主智能体
      is_main: true
      enabled: true
      workflow_package: data-loading-inspection-vector
      skill_packages: [prompt-refinement]
      prompts:
        static_instructions: run
        user_prompt_template: "{{ user_input }}"
`
	var parsed AgentConfigFile
	if err := yaml.Unmarshal([]byte(cfg), &parsed); err != nil {
		t.Fatalf("yaml unmarshal failed: %v", err)
	}
	if parsed.Runtime == nil {
		t.Fatal("expected active runtime config")
	}
	if got := parsed.Runtime.Agents["orchestrator"].WorkflowPackage; got != "data-loading-inspection-vector" {
		t.Fatalf("unexpected workflow package in parsed config: %q", got)
	}
	rootDir := t.TempDir()
	runtimeConfig := ConvertToRuntimeSkillConfig(&parsed, filepath.Join(rootDir, "skills", "builtin"), filepath.Join(rootDir, "skills", "user"), filepath.Join(rootDir, "config"), "default")
	if runtimeConfig == nil {
		t.Fatal("expected runtime skill config")
	}
	if got := runtimeConfig.Agents["orchestrator"].WorkflowPackage; got != "data-loading-inspection-vector" {
		t.Fatalf("unexpected workflow package in runtime skill config: %q", got)
	}
}

func TestNormalizeRuntimeConfigPreservesWorkflowPackage(t *testing.T) {
	normalized := normalizeRuntimeConfig(&RuntimeConfig{
		Agents: map[string]AgentDefData{
			"orchestrator": {
				DisplayName:     "编排智能体",
				Description:     "主智能体",
				Enabled:         true,
				WorkflowPackage: "data-loading-inspection-vector",
			},
		},
	})
	if normalized == nil {
		t.Fatal("expected normalized runtime config")
	}
	if got := normalized.Agents["orchestrator"].WorkflowPackage; got != "data-loading-inspection-vector" {
		t.Fatalf("unexpected normalized workflow package: %q", got)
	}
}
