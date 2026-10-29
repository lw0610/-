package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentConfigManagerLoadsRuntimeAlias(t *testing.T) {
	tmpDir := t.TempDir()

	configText := `version: "1.0"
meta:
  default_execution_mode: build
  description: "runtime alias"
  plan_enabled: true
runtime:
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: []
    tool_groups: {}
  agents:
    orchestrator:
      display_name: "统一运行智能体"
      description: "测试运行内核"
      is_main: true
      deletable: false
      enabled: true
      llm:
        provider: ""
      tool_policy:
        private:
          exclusive: []
          tool_groups: []
        share:
          exclusive: []
          tool_groups: []
      skill_policy:
        private: []
        share: []
      prompts:
        static_instructions: "test"
plan:
  max_iterations: 100
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: []
    tool_groups: {}
  agents:
    planner:
      is_main: true
      display_name: "Planner"
      llm:
        provider: ""
      tool_policy:
        mode: readonly_plus
        private:
          exclusive: []
          tool_groups: []
        share:
          exclusive: []
          tool_groups: []
        exclude_tools: []
      skill_policy:
        private: []
        share: []
      prompts:
        static_instructions: "plan"
        runtime_mode_instruction: "runtime"
        user_prompt_template: "{{ user_input }}"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "default.yaml"), []byte(configText), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	acm := NewAgentConfigManager(tmpDir)
	data, err := acm.GetConfigData("default")
	if err != nil {
		t.Fatalf("GetConfigData failed: %v", err)
	}
	if data.Runtime == nil {
		t.Fatalf("expected runtime config to be populated")
	}
	if _, ok := data.Runtime.Agents["orchestrator"]; !ok {
		t.Fatalf("expected runtime.agents.orchestrator to exist")
	}
}

func TestAgentConfigManagerRebuildsLegacyBuildOnlyDefaultToEmbeddedDefault(t *testing.T) {
	tmpDir := t.TempDir()

	configText := `version: "1.0"
meta:
  default_execution_mode: build
  description: "legacy build"
  plan_enabled: false
build:
  global:
    common_skills: []
  agents:
    orchestrator:
      display_name: "旧执行智能体"
      description: "兼容旧字段"
      is_main: true
      enabled: true
      skill_policy:
        private: []
        share: []
      prompts:
        static_instructions: "legacy"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "default.yaml"), []byte(configText), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	acm := NewAgentConfigManager(tmpDir)
	data, err := acm.GetConfigData("default")
	if err != nil {
		t.Fatalf("GetConfigData failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(tmpDir, "default", "agent.yaml"))
	if err != nil {
		t.Fatalf("read default config failed: %v", err)
	}
	content := string(raw)
	if strings.Contains(content, "build:") {
		t.Fatalf("expected legacy build-only default to be replaced, got: %s", content)
	}
	if got := data.Runtime.Agents["orchestrator"].DisplayName; got != "编排智能体" {
		t.Fatalf("expected legacy build-only config to be replaced by embedded default, got %q", got)
	}
}

func TestAgentConfigManagerPersistsRuntimeAsPrimaryField(t *testing.T) {
	tmpDir := t.TempDir()

	_ = NewAgentConfigManager(tmpDir)

	raw, err := os.ReadFile(filepath.Join(tmpDir, "default", "agent.yaml"))
	if err != nil {
		t.Fatalf("read default config failed: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "runtime:") {
		t.Fatalf("expected persisted config to contain runtime field, got: %s", content)
	}
	if strings.Contains(content, "build:") {
		t.Fatalf("expected persisted config to omit legacy build field, got: %s", content)
	}
}

func TestAgentConfigManagerIgnoresLegacyPlannerBehavior(t *testing.T) {
	tmpDir := t.TempDir()

	configText := `version: "1.0"
meta:
  default_execution_mode: build
  description: "legacy plan behavior"
  plan_enabled: true
runtime:
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: []
    tool_groups: {}
  agents:
    orchestrator:
      display_name: "运行智能体"
      description: "测试"
      is_main: true
      enabled: true
      llm:
        provider: ""
      tool_policy:
        private:
          exclusive: []
          tool_groups: []
        share:
          exclusive: []
          tool_groups: []
      skill_policy:
        private: []
        share: []
      prompts:
        static_instructions: "run"
plan:
  behavior:
    max_iterations: 42
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: []
    tool_groups: {}
  agents:
    planner:
      is_main: true
      display_name: "Planner"
      llm:
        provider: ""
      tool_policy:
        mode: readonly_plus
        private:
          exclusive: []
          tool_groups: []
        share:
          exclusive: []
          tool_groups: []
        exclude_tools: []
      skill_policy:
        private: []
        share: []
      prompts:
        static_instructions: "plan"
        runtime_mode_instruction: "runtime"
        user_prompt_template: "{{ user_input }}"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "default.yaml"), []byte(configText), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	acm := NewAgentConfigManager(tmpDir)
	data, err := acm.GetConfigData("default")
	if err != nil {
		t.Fatalf("GetConfigData failed: %v", err)
	}
	if data.Plan != nil {
		t.Fatal("legacy independent planner must not be revived")
	}
	if data.Runtime == nil {
		t.Fatal("runtime configuration was lost")
	}
}
