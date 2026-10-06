package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRuntimeSkillDirsPrefersPluginBuiltinUserAndExclusive(t *testing.T) {
	rootDir := t.TempDir()
	pluginDir := filepath.Join(rootDir, "autocarto", "skills", "system")
	configBaseDir := filepath.Join(rootDir, "autocarto", "config")
	builtinRuntimeDir := filepath.Join(rootDir, "autocarto", "skills", "system")
	userRuntimeDir := filepath.Join(rootDir, "autocarto", "skills", "user")
	globalRuntimeDir := filepath.Join(configBaseDir, "skills", "global")
	for _, dir := range []string{pluginDir, builtinRuntimeDir, userRuntimeDir, globalRuntimeDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
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

	got := ResolveRuntimeSkillDirs(builtinRuntimeDir, userRuntimeDir, configBaseDir, "active")
	want := []string{pluginDir, userRuntimeDir, globalRuntimeDir, filepath.Join(configBaseDir, "agent_configs", "active", "skills")}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("ResolveRuntimeSkillDirs mismatch:\nwant=%#v\n got=%#v", want, got)
	}
}

func TestResolveRuntimeSkillDirsWithPluginOverrideKeepsExplicitPluginDirOnlyOnce(t *testing.T) {
	rootDir := t.TempDir()
	explicitPluginDir := filepath.Join(t.TempDir(), "plugin-skills")
	configBaseDir := filepath.Join(rootDir, "autocarto", "config")
	builtinRuntimeDir := filepath.Join(rootDir, "autocarto", "skills", "system")
	userRuntimeDir := filepath.Join(rootDir, "autocarto", "skills", "user")
	globalRuntimeDir := filepath.Join(configBaseDir, "skills", "global")
	for _, dir := range []string{explicitPluginDir, builtinRuntimeDir, userRuntimeDir, globalRuntimeDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
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

	got := ResolveRuntimeSkillDirsWithPlugin(explicitPluginDir, builtinRuntimeDir, userRuntimeDir, configBaseDir, "active")
	want := []string{explicitPluginDir, builtinRuntimeDir, userRuntimeDir, globalRuntimeDir, filepath.Join(configBaseDir, "agent_configs", "active", "skills")}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("ResolveRuntimeSkillDirsWithPlugin mismatch:\nwant=%#v\n got=%#v", want, got)
	}
}

func TestResolveRuntimeToolDirsPrefersPluginBuiltinUserGlobalAndExclusive(t *testing.T) {
	rootDir := t.TempDir()
	pluginDir := filepath.Join(rootDir, "autocarto", "tools", "system")
	configBaseDir := filepath.Join(rootDir, "autocarto", "config")
	builtinRuntimeDir := filepath.Join(rootDir, "autocarto", "tools", "system")
	userRuntimeDir := filepath.Join(rootDir, "autocarto", "tools", "user")
	for _, dir := range []string{pluginDir, builtinRuntimeDir, userRuntimeDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
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

	got := ResolveRuntimeToolDirs(builtinRuntimeDir, userRuntimeDir, configBaseDir, "active")
	want := []string{
		pluginDir,
		userRuntimeDir,
		filepath.Join(configBaseDir, "tools", "global"),
		filepath.Join(configBaseDir, "agent_configs", "active", "tools"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("ResolveRuntimeToolDirs mismatch:\nwant=%#v\n got=%#v", want, got)
	}
}

func TestResolveRuntimeToolDirsWithPluginOverrideKeepsExplicitPluginDirOnlyOnce(t *testing.T) {
	rootDir := t.TempDir()
	explicitPluginDir := filepath.Join(t.TempDir(), "plugin-tools")
	configBaseDir := filepath.Join(rootDir, "autocarto", "config")
	builtinRuntimeDir := filepath.Join(rootDir, "autocarto", "tools", "system")
	userRuntimeDir := filepath.Join(rootDir, "autocarto", "tools", "user")
	for _, dir := range []string{explicitPluginDir, builtinRuntimeDir, userRuntimeDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
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

	got := ResolveRuntimeToolDirsWithPlugin(explicitPluginDir, builtinRuntimeDir, userRuntimeDir, configBaseDir, "active")
	want := []string{
		explicitPluginDir,
		builtinRuntimeDir,
		userRuntimeDir,
		filepath.Join(configBaseDir, "tools", "global"),
		filepath.Join(configBaseDir, "agent_configs", "active", "tools"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("ResolveRuntimeToolDirsWithPlugin mismatch:\nwant=%#v\n got=%#v", want, got)
	}
}

func TestFindSkillReferences(t *testing.T) {
	tmpDir := t.TempDir()

	defaultCfg := `version: "1.0"
runtime:
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: [global-skill]
    tool_groups: {}
  agents:
    analyst:
      enabled: true
      tool_policy:
        private:
          exclusive: []
          tool_groups: []
        share:
          exclusive: []
          tool_groups: []
      skill_policy:
        private: [direct-skill]
        share: []
      prompts: {}
plan:
  max_iterations: 100
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: [plan-global]
    tool_groups: {}
  agents:
    planner:
      is_main: true
      display_name: Planner
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
        private: [plan-skill]
        share: []
      prompts:
        static_instructions: "plan"
        runtime_mode_instruction: "runtime"
        user_prompt_template: "{{ user_input }}"
`

	altCfg := `version: "1.0"
runtime:
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: []
    tool_groups: {}
  agents:
    worker:
      enabled: true
      tool_policy:
        private:
          exclusive: []
          tool_groups: []
        share:
          exclusive: []
          tool_groups: []
      skill_policy:
        private: []
        share: [direct-skill]
      prompts: {}
`

	if err := os.WriteFile(filepath.Join(tmpDir, "primary.yaml"), []byte(defaultCfg), 0o644); err != nil {
		t.Fatalf("write primary config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "alt.yaml"), []byte(altCfg), 0o644); err != nil {
		t.Fatalf("write alt config: %v", err)
	}

	acm := NewAgentConfigManager(tmpDir)

	directRefs := acm.FindSkillReferences("direct-skill")
	if len(directRefs) != 2 {
		t.Fatalf("expected 2 references for direct-skill, got %d", len(directRefs))
	}

	planRefs := acm.FindSkillReferences("plan-skill")
	if len(planRefs) != 1 {
		t.Fatalf("expected 1 reference for plan-skill, got %d", len(planRefs))
	}
	if planRefs[0].Layer != "plan/agent" {
		t.Fatalf("unexpected plan reference layer: %s", planRefs[0].Layer)
	}

	noneRefs := acm.FindSkillReferences("missing-skill")
	if len(noneRefs) != 0 {
		t.Fatalf("expected 0 references for missing-skill, got %d", len(noneRefs))
	}
}

func TestFindSkillReferencesIncludesCategoryAndWorkflowLayers(t *testing.T) {
	rootDir := t.TempDir()
	configDir := filepath.Join(rootDir, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	cfg := `version: "1.0"
runtime:
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: [legacy-global]
    common_category_skills: [global-category]
    common_workflow_skills: [global-workflow]
    tool_groups: {}
  agents:
    analyst:
      enabled: true
      tool_policy:
        private:
          exclusive: []
          tool_groups: []
        share:
          exclusive: []
          tool_groups: []
      skill_policy:
        private:
          skills: [legacy-private]
          category_skills: [private-category]
          workflow_skills: [private-workflow]
        share:
          skills: []
          category_skills: [share-category]
          workflow_skills: [share-workflow]
      prompts: {}
plan:
  max_iterations: 100
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: []
    common_category_skills: [plan-global-category]
    common_workflow_skills: [plan-global-workflow]
    tool_groups: {}
  agents:
    planner:
      is_main: true
      display_name: Planner
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
        private:
          skills: []
          category_skills: [plan-private-category]
          workflow_skills: [plan-private-workflow]
        share:
          skills: []
          category_skills: []
          workflow_skills: [plan-share-workflow]
      prompts:
        static_instructions: "plan"
        runtime_mode_instruction: "runtime"
        user_prompt_template: "{{ user_input }}"
`

	if err := os.WriteFile(filepath.Join(configDir, "skills-profile.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write skills profile config: %v", err)
	}

	acm := NewAgentConfigManager(configDir)

	assertLayer := func(skillName string, expectedLayer string, expectedSource string) {
		t.Helper()
		refs := acm.FindSkillReferences(skillName)
		if len(refs) != 1 {
			t.Fatalf("expected 1 reference for %s, got %d", skillName, len(refs))
		}
		if refs[0].Layer != expectedLayer {
			t.Fatalf("unexpected layer for %s: %s", skillName, refs[0].Layer)
		}
		if refs[0].Source != expectedSource {
			t.Fatalf("unexpected source for %s: %s", skillName, refs[0].Source)
		}
	}

	assertLayer("global-category", "runtime/global", "skill_packages")
	assertLayer("global-workflow", "runtime/global", "skill_packages")
	assertLayer("private-category", "runtime/agent", "skill_packages")
	assertLayer("share-workflow", "runtime/agent", "skill_packages")
	assertLayer("plan-global-category", "plan/global", "skill_packages")
	assertLayer("plan-private-workflow", "plan/agent", "skill_packages")
	assertLayer("plan-share-workflow", "plan/agent", "skill_packages")
}

func TestGetActiveToolCatalogIncludesCategorySkillTools(t *testing.T) {
	rootDir := t.TempDir()
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

	writePackage("canvas-navigation", "canvas_zoom_to_extent", "canvas_zoom_to_layer")
	writePackage("buffer-category", "buffer")

	cfg := `version: "1.0"
runtime:
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: []
    common_category_skills: [canvas-navigation]
    common_workflow_skills: []
    tool_groups: {}
  agents:
    analyst:
      enabled: true
      tool_policy:
        private:
          exclusive: []
          tool_groups: []
        share:
          exclusive: []
          tool_groups: []
      skill_policy:
        private:
          skills: []
          category_skills: [buffer-category]
          workflow_skills: []
        share:
          skills: []
          category_skills: []
          workflow_skills: []
      prompts: {}
plan:
  max_iterations: 100
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: []
    common_category_skills: []
    common_workflow_skills: []
    tool_groups: {}
  agents:
    planner:
      is_main: true
      display_name: Planner
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
        private:
          skills: []
          category_skills: []
          workflow_skills: []
        share:
          skills: []
          category_skills: []
          workflow_skills: []
      prompts:
        static_instructions: "plan"
        runtime_mode_instruction: "runtime"
        user_prompt_template: "{{ user_input }}"
`

	if err := os.WriteFile(filepath.Join(configDir, "catalog.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write catalog config: %v", err)
	}

	acm := NewAgentConfigManager(configDir)
	if err := acm.SwitchConfig("", "catalog"); err != nil {
		t.Fatalf("SwitchConfig(catalog) failed: %v", err)
	}
	tools, err := acm.GetActiveToolCatalog()
	if err != nil {
		t.Fatalf("GetActiveToolCatalog failed: %v", err)
	}
	joined := strings.Join(tools, ",")
	for _, expected := range []string{"canvas_zoom_to_extent", "canvas_zoom_to_layer", "buffer"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected tool %s in active catalog, got %v", expected, tools)
		}
	}
}
