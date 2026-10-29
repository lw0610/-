package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultAgentConfigPreservesUserRolesPromptsAndUnknownPackages(t *testing.T) {
	tmpDir := t.TempDir()
	defaultDir := filepath.Join(tmpDir, "default")
	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		t.Fatalf("mkdir default dir failed: %v", err)
	}

	legacyConfig := `version: "1.0"
meta:
  default_execution_mode: build
  description: legacy default
  plan_enabled: true
runtime:
  global:
    skill_packages: [project-state, runtime-control, third-party-global]
  agents:
    orchestrator:
      display_name: 编排智能体
      description: 旧版编排
      is_main: true
      enabled: true
      llm:
        provider: ""
      skill_packages: [prompt-refinement, data-to-map-full-pipeline, third-party-orchestrator]
      prompts:
        static_instructions: legacy orchestrator prompt
        user_prompt_template: "{{ user_input }}"
    data_engineer:
      display_name: 数据工程专家
      description: 旧版数据
      enabled: true
      llm:
        provider: ""
      skill_packages: [data-loading]
      prompts:
        static_instructions: legacy data prompt
        user_prompt_template: "{{ user_input }}"
    spatial_analyst:
      display_name: 空间分析专家
      description: 旧版分析
      enabled: true
      llm:
        provider: ""
      skill_packages: [spatial-analysis]
      prompts:
        static_instructions: legacy analyst prompt
        user_prompt_template: "{{ user_input }}"
    cartographer:
      display_name: 制图布局专家
      description: 旧版制图
      enabled: true
      llm:
        provider: ""
      skill_packages: [cartography-styling]
      prompts:
        static_instructions: legacy cartography prompt
        user_prompt_template: "{{ user_input }}"
    scripting_specialist:
      display_name: 脚本专家
      description: 旧版脚本
      enabled: true
      llm:
        provider: ""
      skill_packages: [pyqgis-scripting, third-party-script]
      prompts:
        static_instructions: legacy scripting prompt
        user_prompt_template: "{{ user_input }}"
plan:
  global:
    skill_packages: [project-state, runtime-control, third-party-plan]
  agents:
    planner:
      display_name: 规划智能体
      is_main: true
      mode: readonly_plus
      llm:
        provider: ""
      skill_packages: [prompt-refinement]
      prompts:
        static_instructions: legacy planner prompt
        runtime_mode_instruction: legacy runtime instruction
        user_prompt_template: "{{ user_input }}"
`
	configPath := filepath.Join(defaultDir, agentConfigFileName)
	if err := os.WriteFile(configPath, []byte(legacyConfig), 0o644); err != nil {
		t.Fatalf("write legacy default config failed: %v", err)
	}

	exclusiveSkillPath := filepath.Join(defaultDir, "skills", "external-package", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(exclusiveSkillPath), 0o755); err != nil {
		t.Fatalf("mkdir exclusive skill dir failed: %v", err)
	}
	if err := os.WriteFile(exclusiveSkillPath, []byte("# external skill\n"), 0o644); err != nil {
		t.Fatalf("write exclusive skill file failed: %v", err)
	}

	manager := NewAgentConfigManager(tmpDir)
	data, err := manager.GetConfigData("default")
	if err != nil {
		t.Fatalf("GetConfigData(default) failed: %v", err)
	}
	if data.Runtime == nil || data.Plan != nil {
		t.Fatalf("expected runtime with legacy planner omitted: %+v", data)
	}

	if len(data.Runtime.Agents) != 5 {
		t.Fatalf("startup added or deleted user roles: %v", sortedAgentNames(data.Runtime.Agents))
	}
	if data.Runtime.Agents["orchestrator"].Prompts["static_instructions"] != "legacy orchestrator prompt" {
		t.Fatal("startup overwrote editable user prompts")
	}
	for _, check := range []struct {
		got  []string
		want string
	}{
		{data.Runtime.LegacyUnresolvedPackages, "third-party-global"},
		{data.Runtime.Agents["orchestrator"].LegacyUnresolvedPackages, "third-party-orchestrator"},
		{data.Runtime.Agents["scripting_specialist"].LegacyUnresolvedPackages, "third-party-script"},
	} {
		if !containsNormalized(check.got, check.want) {
			t.Errorf("unknown package not preserved: %s", check.want)
		}
	}
	if data.Flows != nil {
		t.Fatal("legacy per-scheme flow selection survived migration")
	}
	if raw, err := os.ReadFile(configPath); err != nil || string(raw) != legacyConfig {
		t.Fatal("read changed the user's saved file", err)
	}
	warned := map[string]bool{}
	for _, warning := range data.MigrationWarnings {
		if warning.Code == "LEGACY_PACKAGE_UNRESOLVED" {
			warned[warning.Package] = true
		}
	}
	if len(warned) != 4 {
		t.Fatal("unresolved package warnings missing", data.MigrationWarnings)
	}
	if _, err := os.Stat(exclusiveSkillPath); err != nil {
		t.Fatalf("expected exclusive skill file preserved: %v", err)
	}
}
