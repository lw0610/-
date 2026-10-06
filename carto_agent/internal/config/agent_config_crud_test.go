package config

import "testing"

func TestCreateManagedConfigAndAssignManagedSkillPackages(t *testing.T) {
	manager := NewAgentConfigManager(t.TempDir())
	if err := CreateManagedConfig(manager, "creator", "default", nil); err != nil {
		t.Fatalf("CreateManagedConfig failed: %v", err)
	}
	created, err := manager.GetConfigData("creator")
	if err != nil {
		t.Fatalf("GetConfigData failed: %v", err)
	}
	if created.Runtime == nil {
		t.Fatalf("expected runtime config in created profile")
	}

	updated, err := AssignManagedSkillPackages(manager, "creator", "runtime", "global", "", []string{"runtime-control", "skill-management"})
	if err != nil {
		t.Fatalf("AssignManagedSkillPackages failed: %v", err)
	}
	if updated.Runtime == nil {
		t.Fatalf("expected runtime config after assign")
	}
	got := updated.Runtime.Global.SkillPackages
	if len(got) != 2 || got[0] != "runtime-control" || got[1] != "skill-management" {
		t.Fatalf("unexpected runtime global skill packages: %#v", got)
	}
}
