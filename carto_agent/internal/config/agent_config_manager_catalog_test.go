package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGetActiveToolCatalogUsesCategorySkillsAndDedups(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv(assetsRootEnvVar, filepath.Join(rootDir, "autocarto"))
	configDir := filepath.Join(rootDir, ".autocarto", "config", "agent_configs")
	skillsDir := filepath.Join(rootDir, "autocarto", "skills", "system")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	writePackage := func(packageName string, toolNames ...string) {
		t.Helper()
		packageDir := filepath.Join(skillsDir, packageName)
		if err := os.MkdirAll(packageDir, 0o755); err != nil {
			t.Fatalf("mkdir package dir: %v", err)
		}
		toolRefs := strings.Join(toolNames, "\n  - ")
		skillMarkdown := strings.TrimSpace(`---
name: `+packageName+`
description: `+packageName+` package
kind: category
backend: compute
enabled: true
tools:
  - `+toolRefs+`
---

`+packageName+` package`) + "\n"
		if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(skillMarkdown), 0o644); err != nil {
			t.Fatalf("write SKILL.md: %v", err)
		}
	}

	writePackage("common-skill", "common_a", "shared_lookup")
	writePackage("analysis-skill", "analysis_a")
	writePackage("shared-skill", "shared_lookup", "shared_validate")

	cfg := `version: "1.0"
runtime:
  global:
    common_category_skills: [common-skill]
  agents:
    agent_a:
      enabled: true
      skill_policy:
        private:
          category_skills: [analysis-skill]
        share:
          category_skills: [shared-skill]
      prompts: {}
plan:
  max_iterations: 100
  global:
    common_category_skills: [common-skill]
  agents:
    planner:
      is_main: true
      display_name: Planner
      tool_policy:
        mode: readonly_plus
      skill_policy:
        private:
          category_skills: [shared-skill]
        share: {}
      prompts:
        static_instructions: "plan"
        runtime_mode_instruction: "runtime"
        user_prompt_template: "{{ user_input }}"
`
	if err := os.WriteFile(filepath.Join(configDir, "catalog.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write catalog config: %v", err)
	}

	manager := NewAgentConfigManager(configDir)
	if err := manager.SwitchConfig("", "catalog"); err != nil {
		t.Fatalf("SwitchConfig(catalog) failed: %v", err)
	}

	tools, err := manager.GetActiveToolCatalog()
	if err != nil {
		t.Fatalf("GetActiveToolCatalog returned error: %v", err)
	}

	expected := []string{"analysis_a", "common_a", "shared_lookup", "shared_validate"}
	if !reflect.DeepEqual(tools, expected) {
		t.Fatalf("unexpected tools: got=%v want=%v", tools, expected)
	}
}

func TestGetActiveToolCatalogWhenNoActiveConfig(t *testing.T) {
	manager := &AgentConfigManager{
		configs:      map[string]*AgentConfigFile{},
		activeConfig: "missing",
	}

	if _, err := manager.GetActiveToolCatalog(); err == nil {
		t.Fatal("expected error when active config is missing")
	}
}

func TestGetToolCatalogIncludesUserSkillPackages(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv(assetsRootEnvVar, filepath.Join(rootDir, "autocarto"))
	configDir := filepath.Join(rootDir, ".autocarto", "config", "agent_configs")
	systemSkillsDir := filepath.Join(rootDir, "autocarto", "skills", "system")
	userSkillsDir := filepath.Join(rootDir, "autocarto", "skills", "user")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.MkdirAll(systemSkillsDir, 0o755); err != nil {
		t.Fatalf("mkdir system skills dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(userSkillsDir, "user-category"), 0o755); err != nil {
		t.Fatalf("mkdir user skills dir: %v", err)
	}
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.WriteFile(filepath.Join(userSkillsDir, "user-category", "SKILL.md"), []byte("---\nname: user-category\ndescription: user category\nkind: category\ntools:\n  - user_scope_tool\n---\n"), 0o644); err != nil {
		t.Fatalf("write user SKILL.md: %v", err)
	}
	manager := &AgentConfigManager{
		configDir:    configDir,
		configs:      map[string]*AgentConfigFile{"catalog": normalizeAgentConfigFile(&AgentConfigFile{Version: "2.0", Runtime: &RuntimeConfig{SharedToolPackages: []string{"user-category"}, Agents: map[string]AgentDefData{"orchestrator": {Enabled: true, Prompts: map[string]string{}}}}})},
		activeConfig: "catalog",
	}
	activeTools, err := manager.GetActiveToolCatalog()
	if err != nil {
		t.Fatalf("GetActiveToolCatalog returned error: %v", err)
	}
	if !reflect.DeepEqual(activeTools, []string{"user_scope_tool"}) {
		t.Fatalf("unexpected active tools: %#v", activeTools)
	}
	globalTools, err := manager.GetGlobalToolCatalog()
	if err != nil {
		t.Fatalf("GetGlobalToolCatalog returned error: %v", err)
	}
	if !reflect.DeepEqual(globalTools, []string{"user_scope_tool"}) {
		t.Fatalf("unexpected global tools: %#v", globalTools)
	}
}

func TestMigrateExclusiveSkillsUsesPluginRootSystemPackages(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv(assetsRootEnvVar, filepath.Join(rootDir, "autocarto"))
	configDir := filepath.Join(rootDir, ".autocarto", "config", "agent_configs")
	systemSkillsDir := filepath.Join(rootDir, "autocarto", "skills", "system")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.MkdirAll(systemSkillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	packageDir := filepath.Join(systemSkillsDir, "exclusive-only")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatalf("mkdir package dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte("---\nname: exclusive-only\ndescription: exclusive-only\nkind: category\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	cfg := `version: "1.0"
runtime:
  global:
    skill_packages: []
  agents:
    orchestrator:
      enabled: true
      skill_packages: [exclusive-only]
      prompts: {}
`
	if err := os.WriteFile(filepath.Join(configDir, "creator.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write creator config: %v", err)
	}

	manager := NewAgentConfigManager(configDir)
	result, err := manager.MigrateExclusiveSkills()
	if err != nil {
		t.Fatalf("MigrateExclusiveSkills returned error: %v", err)
	}
	if len(result.Migrated) != 1 {
		t.Fatalf("expected 1 migrated package, got %#v", result)
	}
	targetPath := filepath.Join(manager.ExclusiveSkillsDir("creator"), "exclusive-only", "SKILL.md")
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("expected migrated package at %s: %v", targetPath, err)
	}
	if _, err := os.Stat(filepath.Join(systemSkillsDir, "exclusive-only")); !os.IsNotExist(err) {
		t.Fatalf("expected source package removed, stat err=%v", err)
	}
}
