package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentConfigManagerMigratesFlatConfigToFolderizedLayout(t *testing.T) {
	tmpDir := t.TempDir()
	legacyPath := filepath.Join(tmpDir, "custom.yaml")
	legacyConfig := `version: "1.0"
runtime:
  global:
    skill_packages: []
  agents:
    orchestrator:
      display_name: "统一运行智能体"
      description: "测试运行内核"
      is_main: true
      enabled: true
      llm:
        provider: ""
      skill_packages: []
      prompts:
        static_instructions: "test"
`
	if err := os.WriteFile(legacyPath, []byte(legacyConfig), 0o644); err != nil {
		t.Fatalf("write legacy config failed: %v", err)
	}

	manager := NewAgentConfigManager(tmpDir)
	if _, err := manager.GetConfigData("custom"); err != nil {
		t.Fatalf("GetConfigData(custom) failed after migrate: %v", err)
	}

	migratedPath := filepath.Join(tmpDir, "custom", "agent.yaml")
	if _, err := os.Stat(migratedPath); err != nil {
		t.Fatalf("expected migrated config at %s: %v", migratedPath, err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("expected legacy flat config removed, err=%v", err)
	}

	reloaded := NewAgentConfigManager(tmpDir)
	if _, err := reloaded.GetConfigData("custom"); err != nil {
		t.Fatalf("GetConfigData(custom) failed after idempotent reload: %v", err)
	}
	if _, err := os.Stat(migratedPath); err != nil {
		t.Fatalf("expected migrated config to remain after reload: %v", err)
	}
}

func TestCloneConfigCopiesExclusiveSkillsDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewAgentConfigManager(tmpDir)

	sourceSkill := filepath.Join(tmpDir, "default", "skills", "demo-package", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(sourceSkill), 0o755); err != nil {
		t.Fatalf("mkdir source skill dir failed: %v", err)
	}
	if err := os.WriteFile(sourceSkill, []byte("---\nname: demo-package\ndescription: demo\nkind: workflow\n---\n\n# demo\n"), 0o644); err != nil {
		t.Fatalf("write source skill failed: %v", err)
	}

	if err := manager.CloneConfig("default", "copied"); err != nil {
		t.Fatalf("CloneConfig failed: %v", err)
	}

	clonedConfig := filepath.Join(tmpDir, "copied", "agent.yaml")
	if _, err := os.Stat(clonedConfig); err != nil {
		t.Fatalf("expected cloned config file at %s: %v", clonedConfig, err)
	}
	clonedSkill := filepath.Join(tmpDir, "copied", "skills", "demo-package", "SKILL.md")
	if _, err := os.Stat(clonedSkill); err != nil {
		t.Fatalf("expected cloned skill file at %s: %v", clonedSkill, err)
	}
}

func TestSaveConfigPreservesFolderizedExclusiveSkills(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewAgentConfigManager(tmpDir)
	if err := manager.CreateConfig("custom"); err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}

	skillPath := filepath.Join(tmpDir, "custom", "skills", "demo-package", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("mkdir custom skill dir failed: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte("---\nname: demo-package\ndescription: demo\nkind: workflow\n---\n\n# demo\n"), 0o644); err != nil {
		t.Fatalf("write custom skill failed: %v", err)
	}

	data, err := manager.GetConfigData("custom")
	if err != nil {
		t.Fatalf("GetConfigData(custom) failed: %v", err)
	}
	data.Meta.Description = "updated"
	if err := manager.SaveConfig("", "custom", data); err != nil {
		t.Fatalf("SaveConfig(custom) failed: %v", err)
	}

	configPath := filepath.Join(tmpDir, "custom", "agent.yaml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected folderized config at %s: %v", configPath, err)
	}
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("expected exclusive skill to remain after save: %v", err)
	}
}

func TestDeleteConfigRemovesFolderizedDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewAgentConfigManager(tmpDir)
	if err := manager.CreateConfig("custom"); err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}

	configDir := filepath.Join(tmpDir, "custom")
	if _, err := os.Stat(configDir); err != nil {
		t.Fatalf("expected config dir to exist before delete: %v", err)
	}
	if err := manager.DeleteConfig("", "custom"); err != nil {
		t.Fatalf("DeleteConfig failed: %v", err)
	}
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("expected config dir removed after delete, err=%v", err)
	}
}
