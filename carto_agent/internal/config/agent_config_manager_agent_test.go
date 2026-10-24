package config

import "testing"

func buildTestAgent(displayName string) AgentDefData {
	return AgentDefData{
		DisplayName:   displayName,
		Description:   "用于测试的智能体",
		Enabled:       true,
		SkillPackages: []string{},
		Prompts: map[string]string{
			"static_instructions": "test",
		},
	}
}

func TestAddGetUpdateRemoveAgent(t *testing.T) {
	acm := NewAgentConfigManager(t.TempDir())

	if err := acm.AddAgent("default", "vector_helper", buildTestAgent("矢量助手")); err != nil {
		t.Fatalf("AddAgent failed: %v", err)
	}

	created, err := acm.GetAgent("default", "vector_helper")
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if created.DisplayName != "矢量助手" {
		t.Fatalf("unexpected display_name: %s", created.DisplayName)
	}

	updated := *created
	updated.Description = "更新后的描述"
	updated.ToolPackages = []string{"vector-geometry"}
	if err := acm.UpdateAgent("default", "vector_helper", updated); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	refreshed, err := acm.GetAgent("default", "vector_helper")
	if err != nil {
		t.Fatalf("GetAgent after update failed: %v", err)
	}
	if refreshed.Description != "更新后的描述" {
		t.Fatalf("unexpected description: %s", refreshed.Description)
	}
	if len(refreshed.ToolPackages) != 1 || refreshed.ToolPackages[0] != "vector-geometry" {
		t.Fatalf("unexpected skill packages: %+v", refreshed.ToolPackages)
	}

	if err := acm.RemoveAgent("default", "vector_helper"); err != nil {
		t.Fatalf("RemoveAgent failed: %v", err)
	}
	if _, err := acm.GetAgent("default", "vector_helper"); err == nil {
		t.Fatal("expected GetAgent to fail after remove")
	}
}

func TestAddAgentRejectsDuplicate(t *testing.T) {
	acm := NewAgentConfigManager(t.TempDir())

	if err := acm.AddAgent("default", "worker_a", buildTestAgent("worker-a")); err != nil {
		t.Fatalf("first AddAgent failed: %v", err)
	}
	if err := acm.AddAgent("default", "worker_a", buildTestAgent("worker-a")); err == nil {
		t.Fatal("expected duplicate AddAgent to fail")
	}
}

func TestRemoveAgentRejectsMainAgent(t *testing.T) {
	acm := NewAgentConfigManager(t.TempDir())

	if err := acm.RemoveAgent("default", "orchestrator"); err == nil {
		t.Fatal("expected removing main agent to fail")
	}
}

func TestUpdateAgentNotFound(t *testing.T) {
	acm := NewAgentConfigManager(t.TempDir())

	err := acm.UpdateAgent("default", "missing_agent", buildTestAgent("missing"))
	if err == nil {
		t.Fatal("expected UpdateAgent to fail for missing agent")
	}
}

func TestSwitchConfigPersistsActiveSelection(t *testing.T) {
	dir := t.TempDir()
	acm := NewAgentConfigManager(dir)

	if err := acm.CreateConfig("custom"); err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}
	if err := acm.SwitchConfig("", "custom"); err != nil {
		t.Fatalf("SwitchConfig failed: %v", err)
	}

	reloaded := NewAgentConfigManager(dir)
	if got := reloaded.GetActiveConfigName(); got != "custom" {
		t.Fatalf("expected persisted active config 'custom', got %q", got)
	}
}

func TestDeleteActiveConfigFallsBackByExistingConfigOrder(t *testing.T) {
	dir := t.TempDir()
	acm := NewAgentConfigManager(dir)

	if err := acm.CreateConfig("zeta"); err != nil {
		t.Fatalf("CreateConfig(zeta) failed: %v", err)
	}
	if err := acm.CreateConfig("alpha"); err != nil {
		t.Fatalf("CreateConfig(alpha) failed: %v", err)
	}
	if err := acm.SwitchConfig("", "zeta"); err != nil {
		t.Fatalf("SwitchConfig(zeta) failed: %v", err)
	}
	if err := acm.DeleteConfig("", "zeta"); err != nil {
		t.Fatalf("DeleteConfig(zeta) failed: %v", err)
	}
	if got := acm.GetActiveConfigName(); got != "alpha" {
		t.Fatalf("expected lexicographically first remaining config to become active, got %q", got)
	}
}
