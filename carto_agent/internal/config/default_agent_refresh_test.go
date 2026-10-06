package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultAgentConfigRefreshPreservesUnknownPackages(t *testing.T) {
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
	if data.Runtime == nil || data.Plan == nil {
		t.Fatalf("expected runtime and plan after refresh: %+v", data)
	}

	embedded := builtInDefaultAgentConfig()
	if embedded == nil || embedded.Runtime == nil || embedded.Plan == nil {
		t.Fatal("expected embedded default config")
	}

	gotRuntimeAgents := sortedAgentNames(data.Runtime.Agents)
	wantRuntimeAgents := sortedAgentNames(embedded.Runtime.Agents)
	if len(gotRuntimeAgents) != len(wantRuntimeAgents) {
		t.Fatalf("unexpected runtime agents after refresh: got=%v want=%v", gotRuntimeAgents, wantRuntimeAgents)
	}
	for i, want := range wantRuntimeAgents {
		if gotRuntimeAgents[i] != want {
			t.Fatalf("unexpected runtime agents after refresh: got=%v want=%v", gotRuntimeAgents, wantRuntimeAgents)
		}
	}

	if data.Runtime.Agents["orchestrator"].Prompts["static_instructions"] != embedded.Runtime.Agents["orchestrator"].Prompts["static_instructions"] {
		t.Fatalf("expected orchestrator prompt refreshed to embedded default")
	}
	if data.Plan.Agents["planner"].Prompts["runtime_mode_instruction"] != embedded.Plan.Agents["planner"].Prompts["runtime_mode_instruction"] {
		t.Fatalf("expected planner runtime prompt refreshed to embedded default")
	}
	if !containsString(data.Runtime.Global.SkillPackages, "third-party-global") {
		t.Fatalf("expected runtime global third-party package preserved: %v", data.Runtime.Global.SkillPackages)
	}
	if !containsString(data.Runtime.Agents["scripting_specialist"].SkillPackages, "third-party-script") {
		t.Fatalf("expected scripting third-party package preserved: %v", data.Runtime.Agents["scripting_specialist"].SkillPackages)
	}
	if !containsString(data.Runtime.Agents["orchestrator"].SkillPackages, "third-party-orchestrator") {
		t.Fatalf("expected orchestrator third-party package preserved: %v", data.Runtime.Agents["orchestrator"].SkillPackages)
	}
	if !containsString(data.Plan.Global.SkillPackages, "third-party-plan") {
		t.Fatalf("expected plan third-party package preserved: %v", data.Plan.Global.SkillPackages)
	}
	if containsString(data.Runtime.Agents["orchestrator"].SkillPackages, "data-to-map-full-pipeline") {
		t.Fatalf("expected retired built-in package removed from orchestrator: %v", data.Runtime.Agents["orchestrator"].SkillPackages)
	}
	if containsString(data.Runtime.Agents["orchestrator"].SkillPackages, "third-party-script") {
		t.Fatalf("unexpected third-party script package leaked into orchestrator: %v", data.Runtime.Agents["orchestrator"].SkillPackages)
	}
	if _, err := os.Stat(exclusiveSkillPath); err != nil {
		t.Fatalf("expected exclusive skill file preserved: %v", err)
	}
}
