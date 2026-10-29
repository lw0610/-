package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestV2MigrationLegacyDefaultFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/v1_default_agent.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var old AgentConfigFile
	if err = yaml.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	got := normalizeAgentConfigFile(&old)
	if got.Version != AgentConfigVersionV20 {
		t.Fatal(got.Version)
	}
	if !reflect.DeepEqual(got.Runtime.SharedToolPackages, []string{"vector-selection"}) {
		t.Fatal(got.Runtime.SharedToolPackages)
	}
	want := map[string][]string{
		"orchestrator":         {"cartography-styling", "print-layout"},
		"knowledge_engineer":   {},
		"data_engineer":        {"browser-data-acquisition", "gee-remote-sensing", "admin-boundaries", "data-loading", "vector-fields", "vector-selection", "crs-projection", "data-conversion", "geocoding", "file-operations", "statistical-data-tools"},
		"spatial_analyst":      {"gee-remote-sensing", "admin-boundaries", "vector-geometry", "vector-creation", "raster-analysis", "spatial-analysis", "network-analysis", "gdal-processing", "grass-analysis", "visibility-analysis"},
		"cartographer":         {"cartography-styling", "route-cartography", "print-layout", "canvas-navigation", "charts"},
		"quality_feedback":     {"quality-feedback"},
		"scripting_specialist": {"gee-remote-sensing", "admin-boundaries", "browser-data-acquisition", "pyqgis-scripting", "point-cloud", "gps-data", "mesh-analysis"},
	}
	for name, packages := range want {
		a := got.Runtime.Agents[name]
		if strings.Join(a.ToolPackages, ",") != strings.Join(packages, ",") {
			t.Errorf("%s: got %v want %v", name, a.ToolPackages, packages)
		}
		if !reflect.DeepEqual(a.Prompts, old.Runtime.Agents[name].Prompts) {
			t.Errorf("%s lost editable prompts", name)
		}
		if len(a.SkillPackages) > 0 || a.legacyWorkflowPackage != "" {
			t.Errorf("legacy fields leaked: %+v", a)
		}
	}
	k := got.Runtime.Agents["knowledge_engineer"]
	if k.Kind != "executor" || k.Executor != "route_knowledge" || !k.FlowOnly {
		t.Fatalf("executor migration: %+v", k)
	}
	if got.Flows != nil || got.Plan != nil {
		t.Fatal("legacy per-scheme flows or independent planner survived migration")
	}
	if len(got.MigrationWarnings) != 0 {
		t.Fatalf("unexpected warnings %+v", got.MigrationWarnings)
	}
	second := normalizeAgentConfigFile(got)
	a, _ := yaml.Marshal(got)
	b, _ := yaml.Marshal(second)
	if !bytes.Equal(a, b) {
		t.Fatal("migration not idempotent")
	}
}

func TestV2MigrationUnknownWorkflowAndExecutorInference(t *testing.T) {
	for _, name := range []string{"knowledge_engineer", "renamed-specialist"} {
		t.Run(name, func(t *testing.T) {
			old := &AgentConfigFile{Version: "1.0", Runtime: &RuntimeConfig{Agents: map[string]AgentDefData{name: {Enabled: true, SkillPackages: []string{"marching-route-knowledge-tools", "missing-custom"}, legacyWorkflowPackage: "old-pilot", Prompts: map[string]string{"static_instructions": "fallback"}}}}}
			got := normalizeAgentConfigFile(old)
			a := got.Runtime.Agents[name]
			if a.Kind != "executor" || a.Executor != "route_knowledge" || !a.FlowOnly || len(a.ToolPackages) != 0 {
				t.Fatalf("%+v", a)
			}
			if !reflect.DeepEqual(a.LegacyUnresolvedPackages, []string{"missing-custom"}) || len(got.MigrationWarnings) != 2 {
				t.Fatalf("%+v %+v", a, got.MigrationWarnings)
			}
			if old.Runtime.Agents[name].legacyWorkflowPackage != "old-pilot" {
				t.Fatal("source mutated")
			}
		})
	}
	// A v2 role ID is never an instruction to select an executor.
	v2 := normalizeAgentConfigFile(&AgentConfigFile{Version: "2.0", Runtime: &RuntimeConfig{Agents: map[string]AgentDefData{"knowledge_engineer": {Kind: "agent", Enabled: true, ToolPackages: []string{"data-loading"}}}}})
	if a := v2.Runtime.Agents["knowledge_engineer"]; a.Kind != "agent" || a.Executor != "" {
		t.Fatalf("role-name inference leaked into v2: %+v", a)
	}
}

func TestV2MigrationReadOnlyBackupAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "default", "agent.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte("version: '1.0'\nmeta: {description: my saved scheme}\nruntime:\n  agents:\n    custom:\n      enabled: true\n      skill_packages: [data-loading, external-package, missing]\n      prompts: {static_instructions: custom instructions}\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	// Installed custom category packages remain usable; genuinely unknown IDs warn.
	pkg := filepath.Join(dir, "default", "skills", "external-package")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "SKILL.md"), []byte("---\nname: external-package\nkind: category\ntools: [custom_tool]\n---\n使用方法\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewAgentConfigManager(dir)
	after, _ := os.ReadFile(path)
	if !bytes.Equal(raw, after) {
		t.Fatal("read overwrote existing default")
	}
	if _, err := os.Stat(path + ".v1.bak"); !os.IsNotExist(err) {
		t.Fatalf("read created backup: %v", err)
	}
	preview, err := m.PreviewMigration("default")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Changed || !preview.DryRun || preview.Config.Meta.Description != "my saved scheme" {
		t.Fatalf("%+v", preview)
	}
	if !containsNormalized(preview.Config.Runtime.Agents["custom"].ToolPackages, "external-package") {
		t.Fatal("installed custom package lost")
	}
	if err = m.SaveConfig("", "default", preview.Config); err != nil {
		t.Fatal(err)
	}
	backup, _ := os.ReadFile(path + ".v1.bak")
	if !bytes.Equal(raw, backup) {
		t.Fatal("backup differs from original bytes")
	}
	saved, _ := os.ReadFile(path)
	if bytes.Contains(saved, []byte("skill_packages:")) || !bytes.Contains(saved, []byte("tool_packages:")) {
		t.Fatalf("legacy YAML written: %s", saved)
	}
	preview.Config.Meta.Description = "edited"
	if err = m.SaveConfig("", "default", preview.Config); err != nil {
		t.Fatal(err)
	}
	backup2, _ := os.ReadFile(path + ".v1.bak")
	if !bytes.Equal(backup, backup2) {
		t.Fatal("backup overwritten")
	}
	reloaded := NewAgentConfigManager(dir)
	data, err := reloaded.GetConfigData("default")
	if err != nil {
		t.Fatal(err)
	}
	if data.Meta.Description != "edited" || data.Runtime.Agents["custom"].Prompts["static_instructions"] != "custom instructions" {
		t.Fatal("startup reset saved configuration")
	}
	if len(data.Runtime.Agents) != 1 {
		t.Fatal("startup restored built-in roles over user's configuration")
	}
}

func TestMigrationIgnoresPerSchemeFlowsAndKeepsRuntimeJSONRoundTrip(t *testing.T) {
	for _, flows := range []*FlowConfig{nil, {Enabled: []string{}}} {
		input := &AgentConfigFile{Version: "2.0", Flows: flows, Runtime: &RuntimeConfig{SharedToolPackages: []string{"charts"}, Agents: map[string]AgentDefData{"special": {Kind: "executor", Executor: "custom", FlowOnly: true, ToolPackages: []string{}, LegacyUnresolvedPackages: []string{"legacy"}, Enabled: true}}}}
		got := normalizeAgentConfigFile(input)
		raw, _ := json.Marshal(got)
		var decoded AgentConfigFile
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Flows != nil {
			t.Fatal("guide enablement must not be revived from per-scheme flow settings")
		}
		if !decoded.Runtime.Agents["special"].FlowOnly || decoded.Runtime.Agents["special"].Executor != "custom" {
			t.Fatal("executor fields lost")
		}
	}
}

func TestV2SaveFailureDoesNotPublishCandidate(t *testing.T) {
	m := NewAgentConfigManager(t.TempDir())
	old, _ := m.GetConfigData("default")
	candidate, _ := m.GetConfigData("default")
	candidate.Meta.Description = "should not publish"
	path := m.configFilePath("default")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveConfig("", "default", candidate); err == nil {
		t.Fatal("expected write failure")
	}
	current, _ := m.GetConfigData("default")
	if current.Meta.Description != old.Meta.Description {
		t.Fatal("failed save published candidate")
	}
}

func TestV2MigrationRetainsOriginalRoleToolSets(t *testing.T) {
	// Fixture extracted from this worktree's preserved Git baseline, including
	// category ownership and disabled-package policy, rather than current output.
	raw, err := os.ReadFile("testdata/v1_tool_baseline.json")
	if err != nil {
		t.Fatal(err)
	}
	var baseline map[string][]string
	if err = json.Unmarshal(raw, &baseline); err != nil {
		t.Fatal(err)
	}
	projectRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	packages := filepath.Join(projectRoot, "autocarto", "skills", "system")
	legacyRaw, err := os.ReadFile("testdata/v1_default_agent.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var legacy AgentConfigFile
	if err := yaml.Unmarshal(legacyRaw, &legacy); err != nil {
		t.Fatal(err)
	}
	runtime := ConvertToRuntimeSkillConfigWithPlugin(&legacy, packages, "", "", t.TempDir(), "default")
	if len(baseline) != 7 {
		t.Fatal("baseline missing roles")
	}
	for role, before := range baseline {
		after := runtime.GetAgentAllowedTools(role)
		if len(before) < 10 {
			t.Fatalf("%s has an empty or incomplete baseline", role)
		}
		for _, name := range before {
			// Retired combined route and frozen preset entries are intentionally
			// hidden. Their supported replacements remain authorized.
			if name == "cartography_build_and_draw_from_tables" || name == "layer_set_fill_symbol" || name == "layer_set_line_symbol" || name == "layer_set_marker_symbol" {
				if containsNormalized(after, name) {
					t.Errorf("retired entry point %s still authorized", name)
				}
				continue
			}
			if !containsNormalized(after, name) {
				t.Errorf("%s lost %s", role, name)
			}
		}
		if role == "cartographer" && !containsNormalized(after, "layer_load_styles") {
			t.Error("batch style replacement is not authorized")
		}
		t.Logf("%s: %d -> %d", role, len(before), len(after))
	}
}
