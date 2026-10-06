package config

import (
	"sort"
	"strings"
	"testing"
)

func sortedAgentNames[V any](items map[string]V) []string {
	if len(items) == 0 {
		return nil
	}
	names := make([]string, 0, len(items))
	for name := range items {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func TestBuiltInDefaultAgentProfileMatchesNewTopology(t *testing.T) {
	cfg := builtInDefaultAgentConfig()
	if cfg == nil {
		t.Fatal("expected built-in default config")
	}
	if cfg.Runtime == nil {
		t.Fatal("expected runtime config")
	}
	if cfg.Plan == nil {
		t.Fatal("expected plan config")
	}

	runtimeAgents := sortedAgentNames(cfg.Runtime.Agents)
	expectedRuntimeAgents := []string{"cartographer", "data_engineer", "knowledge_engineer", "orchestrator", "quality_feedback", "scripting_specialist", "spatial_analyst"}
	if len(runtimeAgents) != len(expectedRuntimeAgents) {
		t.Fatalf("unexpected runtime agent count: got=%v want=%v", runtimeAgents, expectedRuntimeAgents)
	}
	for i, expected := range expectedRuntimeAgents {
		if runtimeAgents[i] != expected {
			t.Fatalf("unexpected runtime agents: got=%v want=%v", runtimeAgents, expectedRuntimeAgents)
		}
	}

	planner, ok := cfg.Plan.Agents["planner"]
	if !ok {
		t.Fatal("expected planner agent in plan config")
	}
	if planner.Mode != PlanToolStrategyReadonlyPlus {
		t.Fatalf("unexpected planner mode: %q", planner.Mode)
	}
	if !containsString(cfg.Plan.Global.SkillPackages, "runtime-control") {
		t.Fatalf("expected runtime-control in plan global skill packages: %v", cfg.Plan.Global.SkillPackages)
	}
	if !containsString(cfg.Plan.Global.SkillPackages, "build-capability-awareness") {
		t.Fatalf("expected build-capability-awareness in plan global skill packages: %v", cfg.Plan.Global.SkillPackages)
	}
	if !strings.Contains(planner.Prompts["runtime_mode_instruction"], "query_build_capabilities") {
		t.Fatalf("expected planner runtime instruction to mention query_build_capabilities: %q", planner.Prompts["runtime_mode_instruction"])
	}

	scripting, ok := cfg.Runtime.Agents["scripting_specialist"]
	if !ok {
		t.Fatal("expected scripting_specialist agent")
	}
	if !strings.Contains(scripting.Prompts["static_instructions"], "标准工具链不足") {
		t.Fatalf("expected scripting specialist prompt boundary, got: %q", scripting.Prompts["static_instructions"])
	}
	orchestrator, ok := cfg.Runtime.Agents["orchestrator"]
	if !ok {
		t.Fatal("expected orchestrator agent")
	}
	if containsString(orchestrator.SkillPackages, "pyqgis-scripting") {
		t.Fatalf("orchestrator should not own pyqgis-scripting: %v", orchestrator.SkillPackages)
	}
	qualityFeedback, ok := cfg.Runtime.Agents["quality_feedback"]
	if !ok {
		t.Fatal("expected quality_feedback agent")
	}
	if len(qualityFeedback.SkillPackages) != 1 || qualityFeedback.SkillPackages[0] != "quality-feedback" {
		t.Fatalf("quality_feedback should only own quality-feedback: %v", qualityFeedback.SkillPackages)
	}
	if !strings.Contains(qualityFeedback.Prompts["static_instructions"], "prepare_quality_review") {
		t.Fatalf("quality_feedback prompt should require multimodal initial review: %q", qualityFeedback.Prompts["static_instructions"])
	}
	if strings.Contains(qualityFeedback.Prompts["static_instructions"], "generate_image2_candidate") {
		t.Fatalf("quality_feedback prompt must not require Image2 candidate generation: %q", qualityFeedback.Prompts["static_instructions"])
	}
}

func TestBuiltInStatisticalWorkflowKeepsPlanReadOnlyAndDataRoleScoped(t *testing.T) {
	cfg := builtInDefaultAgentConfig()
	if cfg == nil || cfg.Runtime == nil || cfg.Plan == nil {
		t.Fatal("default profile must contain Build and Plan")
	}
	for role, packages := range map[string][]string{
		"orchestrator":  {"statistical-map-orchestration"},
		"data_engineer": {"statistical-data-preparation", "statistical-data-tools"},
		"cartographer":  {"thematic-map-styling", "print-layout-statistical", "cartography-styling", "print-layout"},
	} {
		for _, name := range packages {
			if !containsString(cfg.Runtime.Agents[role].SkillPackages, name) {
				t.Errorf("embedded default %s is missing %s", role, name)
			}
		}
	}
	planner, ok := cfg.Plan.Agents["planner"]
	if !ok || planner.Mode != PlanToolStrategyReadonlyPlus || !containsString(planner.SkillPackages, "statistical-map-orchestration") {
		t.Fatal("Plan must discover statistical orchestration under its read-only strategy")
	}
	for _, name := range []string{"statistical-data-tools", "statistical-data-preparation", "print-layout-statistical"} {
		if containsString(planner.SkillPackages, name) || containsString(cfg.Plan.Global.SkillPackages, name) {
			t.Errorf("execution package %s leaked into Plan", name)
		}
	}
	if containsString(cfg.Runtime.Global.SkillPackages, "statistical-data-tools") {
		t.Fatal("statistical preparation must not be a global tool permission")
	}
}
