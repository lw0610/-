package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/defaults"
	"gopkg.in/yaml.v3"
)

func TestDefaultV2ProfileMatchesDeclaredRolesAndToolPackages(t *testing.T) {
	raw, err := defaults.FS.ReadFile("agent_configs/default/agent.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg AgentConfigFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Version != AgentConfigVersionV20 || cfg.Runtime == nil || cfg.Plan == nil {
		t.Fatal("default must already be v2, rather than depend on migration")
	}
	if bytes.Contains(raw, []byte("skill_packages:")) || bytes.Contains(raw, []byte("workflow_package:")) {
		t.Fatal("legacy package bindings remain in default")
	}
	if !reflect.DeepEqual(cfg.Runtime.SharedToolPackages, []string{"vector-selection"}) {
		t.Fatal(cfg.Runtime.SharedToolPackages)
	}
	want := map[string][]string{
		"orchestrator":         {},
		"knowledge_engineer":   {},
		"data_engineer":        {"data-loading", "data-conversion", "file-operations", "crs-projection", "geocoding", "gps-data", "statistical-data-tools", "admin-boundaries", "browser-data-acquisition", "gee-remote-sensing", "vector-fields", "osm-data"},
		"spatial_analyst":      {"vector-geometry", "vector-creation", "raster-analysis", "gdal-processing", "grass-analysis", "spatial-analysis", "network-analysis", "visibility-analysis", "mesh-analysis", "gee-remote-sensing", "admin-boundaries"},
		"cartographer":         {"cartography-styling", "route-cartography", "print-layout", "canvas-navigation", "charts"},
		"quality_feedback":     {"quality-feedback"},
		"scripting_specialist": {"pyqgis-scripting", "point-cloud", "gps-data", "gee-remote-sensing", "admin-boundaries", "browser-data-acquisition", "mesh-analysis"},
	}
	if len(cfg.Runtime.Agents) != len(want) {
		t.Fatal("unexpected default role count")
	}
	for name, packages := range want {
		role := cfg.Runtime.Agents[name]
		if !reflect.DeepEqual(role.ToolPackages, packages) || !role.Enabled {
			t.Errorf("%s packages=%v enabled=%v", name, role.ToolPackages, role.Enabled)
		}
		if name == "knowledge_engineer" {
			if role.Kind != "executor" || role.Executor != "route_knowledge" || !role.FlowOnly || role.Prompts["static_instructions"] != "" || role.DisplayName != "制图知识抽取专家" {
				t.Fatal("knowledge role must be an explicitly typed flow executor with step rules")
			}
		} else if role.Kind != "agent" || role.Executor != "" || role.FlowOnly {
			t.Fatalf("ordinary role has executor semantics: %s", name)
		}
	}
	if !cfg.Runtime.Agents["orchestrator"].IsMain || cfg.Runtime.Agents["cartographer"].DisplayName != "制图布局专家" || cfg.Runtime.Agents["quality_feedback"].LLM.Provider != "qwen" {
		t.Fatal("main designation, flow display-name binding or review provider changed")
	}
	if cfg.Flows == nil || !reflect.DeepEqual(cfg.Flows.Enabled, []string{"marching-route", "statistical-choropleth", "traffic-route", "data-to-map"}) {
		t.Fatal("all four builtin flows must be explicitly enabled")
	}
	if cfg.Plan.Agents["planner"].Kind != "agent" || len(cfg.Plan.Agents["planner"].ToolPackages) != 0 || len(cfg.Plan.Global.ToolPackages) != 0 || !cfg.Meta.PlanEnabled || cfg.Plan.Enabled == nil || !*cfg.Plan.Enabled {
		t.Fatal("Plan should use its system and hidden auto-mounted packages")
	}
	if err := validateAgentConfig(normalizeAgentConfigFile(&cfg)); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultV2ProjectionUsesActualPackageOwnershipAndAutoMounts(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	assets := filepath.Join(root, "autocarto", "skills", "system")
	cfg := builtInDefaultAgentConfig()
	runtime := ConvertToRuntimeSkillConfigWithPlugin(cfg, assets, "", "", t.TempDir(), "default")
	for name, role := range cfg.Runtime.Agents {
		for _, id := range role.ToolPackages {
			if len(runtime.Global.PackageTools[id]) == 0 {
				// A saved explicit package disable remains authoritative during migration.
				var override struct {
					Enabled *bool `yaml:"enabled"`
				}
				raw, err := os.ReadFile(filepath.Join(assets, id, "config.yaml"))
				if err != nil || yaml.Unmarshal(raw, &override) != nil || override.Enabled == nil || *override.Enabled {
					t.Errorf("%s assigned missing or empty package %s", name, id)
				} else {
					t.Logf("%s remains explicitly disabled by its existing package configuration", id)
				}
			}
		}
	}
	for role, tools := range map[string][]string{
		"orchestrator":       {"flow_start", "flow_preview", "reference_read", "get_layers_info"},
		"knowledge_engineer": {"marching_route_get_extraction_contract", "marching_route_compile_dataset"},
		"data_engineer":      {"data_load_list", "admin_boundary_extract", "data_prepare_statistical", "gee_run_script"},
		"spatial_analyst":    {"buffer", "merge_vector_layers"},
		"cartographer":       {"style_set_apply", "route_draw", "route_points_build", "layout_compose", "layout_add_callouts", "import_layout_template", "bar_chart"},
	} {
		allowed := runtime.GetAgentAllowedTools(role)
		for _, name := range tools {
			if !containsNormalized(allowed, name) {
				t.Errorf("%s lacks %s in actual package projection", role, name)
			}
		}
	}
	main := runtime.GetAgentAllowedTools("orchestrator")
	for _, forbidden := range []string{"data_load_vector", "style_set_apply", "layout_compose", "marching_route_compile_dataset"} {
		if containsNormalized(main, forbidden) {
			t.Errorf("main acquired specialist execution tool %s", forbidden)
		}
	}
	for role := range cfg.Runtime.Agents {
		if role != "orchestrator" && containsNormalized(runtime.GetAgentAllowedTools(role), "flow_start") {
			t.Errorf("nested flow start available to %s", role)
		}
	}
}

func TestV2MigrationCanSavePlanOnlyRepairWithoutLosingOriginalPlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-plan", "agent.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte("version: '1.0'\nmeta: {description: saved Plan, default_execution_mode: build, plan_enabled: true}\nplan:\n  max_iterations: 17\n  agents:\n    planner:\n      is_main: true\n      mode: readonly_plus\n      llm: {provider: custom-plan-model}\n      skill_packages: [traffic-map-orchestration]\n      prompts: {static_instructions: saved Plan rules, runtime_mode_instruction: custom mode rules, user_prompt_template: '{{ user_input }}'}\n")
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	var source AgentConfigFile
	if err := yaml.Unmarshal(legacy, &source); err != nil {
		t.Fatal(err)
	}
	// T51 explicitly repairs the incomplete work copy; the reader must not silently add roles.
	source.Runtime = builtInDefaultAgentConfig().Runtime
	candidate := normalizeAgentConfigFile(&source)
	manager := NewAgentConfigManager(dir)
	if err := manager.SaveConfig("", "custom-plan", &AgentConfigData{Version: candidate.Version, Meta: candidate.Meta, Runtime: candidate.Runtime, Plan: candidate.Plan, Flows: candidate.Flows}); err != nil {
		t.Fatal(err)
	}
	data, err := NewAgentConfigManager(dir).GetConfigData("custom-plan")
	if err != nil || data.Plan.MaxIterations != 17 || data.Plan.Agents["planner"].LLM.Provider != "custom-plan-model" || !reflect.DeepEqual(data.Plan.Agents["planner"].Prompts, source.Plan.Agents["planner"].Prompts) || len(data.Runtime.Agents) != 7 {
		t.Fatal("Plan-only repair lost saved Plan or failed runtime initialization", err)
	}
	backup, err := os.ReadFile(path + ".v1.bak")
	if err != nil || !bytes.Equal(backup, legacy) {
		t.Fatal("incomplete original config was not preserved", err)
	}
}

func TestDefaultV2BootstrapAndCustomMigrationRemainDistinct(t *testing.T) {
	dir := t.TempDir()
	manager := NewAgentConfigManager(dir)
	defaultData, err := manager.GetConfigData("default")
	if err != nil || defaultData.Version != AgentConfigVersionV20 || len(defaultData.Runtime.Agents["orchestrator"].ToolPackages) != 0 {
		t.Fatal("bootstrap did not use the new v2 default", err)
	}
	path := filepath.Join(dir, "custom", "agent.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte("version: '1.0'\nmeta: {description: custom scheme}\nruntime:\n  agents:\n    coordinator:\n      is_main: true\n      enabled: true\n      llm: {provider: custom-provider}\n      skill_packages: [data-loading, unknown-custom-package]\n      prompts: {static_instructions: custom rules, user_prompt_template: '{{ user_input }}'}\n    disabled-specialist:\n      enabled: false\n      skill_packages: [print-layout-marching-route]\n      prompts: {static_instructions: saved user rules}\n")
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	manager = NewAgentConfigManager(dir)
	preview, err := manager.PreviewMigration("custom")
	if err != nil || !preview.Changed || len(preview.Warnings) != 1 {
		t.Fatal(preview, err)
	}
	if err := manager.SaveConfig("", "custom", preview.Config); err != nil {
		t.Fatal(err)
	}
	reloaded := NewAgentConfigManager(dir)
	custom, err := reloaded.GetConfigData("custom")
	if err != nil {
		t.Fatal(err)
	}
	coordinator := custom.Runtime.Agents["coordinator"]
	if len(custom.Runtime.Agents) != 2 || coordinator.LLM.Provider != "custom-provider" || coordinator.Prompts["static_instructions"] != "custom rules" || !containsNormalized(coordinator.ToolPackages, "data-loading") || !containsNormalized(coordinator.LegacyUnresolvedPackages, "unknown-custom-package") || custom.Runtime.Agents["disabled-specialist"].Enabled {
		t.Fatal("default migration overwrote custom roles, providers, rules or tool ownership")
	}
	backup, err := os.ReadFile(path + ".v1.bak")
	if err != nil || !bytes.Equal(backup, legacy) {
		t.Fatal("migration did not preserve original bytes", err)
	}
	if raw, err := os.ReadFile(path); err != nil || strings.Contains(string(raw), "skill_packages:") {
		t.Fatal("v2 save retained legacy binding", err)
	}
}
