package websocket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/settings"
)

func writeSkillFile(t *testing.T, skillsDir, skillName, description, content string) string {
	t.Helper()
	skillDir := filepath.Join(skillsDir, skillName)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}

	skillPath := filepath.Join(skillDir, "SKILL.md")
	raw := strings.Join([]string{
		"---",
		"name: " + skillName,
		"description: " + description,
		"---",
		"",
		content,
		"",
	}, "\n")
	if err := os.WriteFile(skillPath, []byte(raw), 0644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
	return skillPath
}

func writeRawFile(t *testing.T, path, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir for raw file: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatalf("write raw file: %v", err)
	}
}

func enterIsolatedWorkingDir(t *testing.T) {
	t.Helper()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	isolatedDir := t.TempDir()
	if err := os.Chdir(isolatedDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
}

func mustImportedList(t *testing.T, result map[string]any) []map[string]any {
	t.Helper()
	raw, ok := result["imported"]
	if !ok {
		t.Fatalf("result missing imported field")
	}
	items, ok := raw.([]map[string]any)
	if !ok {
		t.Fatalf("imported should be []map[string]any, got %T", raw)
	}
	return items
}

func mustSkippedList(t *testing.T, result map[string]any) []map[string]any {
	t.Helper()
	raw, ok := result["skipped"]
	if !ok {
		t.Fatalf("result missing skipped field")
	}
	items, ok := raw.([]map[string]any)
	if !ok {
		t.Fatalf("skipped should be []map[string]any, got %T", raw)
	}
	return items
}

func TestListSkillsIncludesMetadata(t *testing.T) {
	enterIsolatedWorkingDir(t)
	skillsDir := t.TempDir()
	skillPath := writeSkillFile(t, skillsDir, "demo-skill", "desc", "# demo")

	s := &DeepServer{
		userSkillsDir: skillsDir,
		skillsDir:     skillsDir,
	}

	skills, err := s.listSkills()
	if err != nil {
		t.Fatalf("listSkills error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	item := skills[0]
	if item["skill_id"] != "demo-skill" {
		t.Fatalf("unexpected skill_id: %v", item["skill_id"])
	}
	if item["name"] != "demo-skill" {
		t.Fatalf("unexpected name: %v", item["name"])
	}
	if item["description"] != "desc" {
		t.Fatalf("unexpected description: %v", item["description"])
	}
	if item["path"] != skillPath {
		t.Fatalf("unexpected path: %v", item["path"])
	}
	if _, ok := item["size_bytes"].(int64); !ok {
		t.Fatalf("size_bytes should be int64, got %T", item["size_bytes"])
	}
	updatedAt, _ := item["updated_at"].(string)
	if _, err := time.Parse(time.RFC3339, updatedAt); err != nil {
		t.Fatalf("updated_at should be RFC3339, got %q, err=%v", updatedAt, err)
	}
}

func TestSkillIdentifierUsesDirectoryName(t *testing.T) {
	enterIsolatedWorkingDir(t)
	skillsDir := t.TempDir()
	skillFilePath := filepath.Join(skillsDir, "folder-id", "SKILL.md")
	writeRawFile(t, skillFilePath, strings.Join([]string{
		"---",
		"name: Display Name",
		"description: desc",
		"---",
		"",
		"# content",
	}, "\n"))

	s := &DeepServer{userSkillsDir: skillsDir, skillsDir: skillsDir}
	items, err := s.listSkills()
	if err != nil {
		t.Fatalf("listSkills error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(items))
	}

	item := items[0]
	if item["skill_id"] != "folder-id" {
		t.Fatalf("unexpected skill_id: %v", item["skill_id"])
	}
	if item["name"] != "Display Name" {
		t.Fatalf("unexpected display name: %v", item["name"])
	}

	content, err := s.getSkillContent("folder-id")
	if err != nil {
		t.Fatalf("getSkillContent error: %v", err)
	}
	if content["skill_id"] != "folder-id" {
		t.Fatalf("unexpected detail skill_id: %v", content["skill_id"])
	}
	if content["skill_name"] != "Display Name" {
		t.Fatalf("unexpected detail skill_name: %v", content["skill_name"])
	}
}

func TestImportSkillFromFile(t *testing.T) {
	enterIsolatedWorkingDir(t)
	skillsDir := t.TempDir()
	sourceDir := t.TempDir()
	sourceFile := filepath.Join(sourceDir, "import-me.md")
	writeRawFile(t, sourceFile, strings.Join([]string{
		"---",
		"name: imported-file-skill",
		"description: imported from file",
		"---",
		"",
		"# content",
	}, "\n"))

	s := &DeepServer{userSkillsDir: skillsDir, skillsDir: skillsDir}

	result, err := s.importSkill(sourceFile, false)
	if err != nil {
		t.Fatalf("importSkill(file) error: %v", err)
	}
	if result["mode"] != "file" {
		t.Fatalf("expected mode=file, got %v", result["mode"])
	}

	imported := mustImportedList(t, result)
	if len(imported) != 1 {
		t.Fatalf("expected 1 imported skill, got %d", len(imported))
	}
	if imported[0]["name"] != "imported-file-skill" {
		t.Fatalf("unexpected imported skill name: %v", imported[0]["name"])
	}
	if len(mustSkippedList(t, result)) != 0 {
		t.Fatalf("expected skipped list empty")
	}

	targetPath := filepath.Join(skillsDir, "imported-file-skill", "SKILL.md")
	data, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatalf("read imported target failed: %v", readErr)
	}
	raw := string(data)
	if !strings.Contains(raw, "name: imported-file-skill") {
		t.Fatalf("imported skill file not normalized with name: %s", raw)
	}
}

func TestImportSkillFromSingleDirectory(t *testing.T) {
	enterIsolatedWorkingDir(t)
	skillsDir := t.TempDir()
	sourceDir := filepath.Join(t.TempDir(), "single-dir")
	sourceSkillPath := filepath.Join(sourceDir, "SKILL.md")
	writeRawFile(t, sourceSkillPath, strings.Join([]string{
		"---",
		"name: imported-dir-skill",
		"description: imported from dir",
		"---",
		"",
		"# content",
	}, "\n"))

	s := &DeepServer{userSkillsDir: skillsDir, skillsDir: skillsDir}
	result, err := s.importSkill(sourceDir, false)
	if err != nil {
		t.Fatalf("importSkill(single dir) error: %v", err)
	}
	if result["mode"] != "directory_single" {
		t.Fatalf("expected mode=directory_single, got %v", result["mode"])
	}
	imported := mustImportedList(t, result)
	if len(imported) != 1 {
		t.Fatalf("expected 1 imported skill, got %d", len(imported))
	}
	if imported[0]["name"] != "imported-dir-skill" {
		t.Fatalf("unexpected imported skill name: %v", imported[0]["name"])
	}
}

func TestImportSkillFromBatchDirectory(t *testing.T) {
	enterIsolatedWorkingDir(t)
	skillsDir := t.TempDir()
	batchDir := t.TempDir()
	writeRawFile(t, filepath.Join(batchDir, "alpha", "SKILL.md"), strings.Join([]string{
		"---",
		"name: alpha-skill",
		"description: alpha",
		"---",
		"",
		"# alpha",
	}, "\n"))
	writeRawFile(t, filepath.Join(batchDir, "beta", "SKILL.md"), strings.Join([]string{
		"---",
		"description: beta without explicit name",
		"---",
		"",
		"# beta",
	}, "\n"))

	s := &DeepServer{userSkillsDir: skillsDir, skillsDir: skillsDir}
	result, err := s.importSkill(batchDir, false)
	if err != nil {
		t.Fatalf("importSkill(batch dir) error: %v", err)
	}
	if result["mode"] != "directory_batch" {
		t.Fatalf("expected mode=directory_batch, got %v", result["mode"])
	}

	imported := mustImportedList(t, result)
	if len(imported) != 2 {
		t.Fatalf("expected 2 imported skills, got %d", len(imported))
	}

	if _, statErr := os.Stat(filepath.Join(skillsDir, "alpha-skill", "SKILL.md")); statErr != nil {
		t.Fatalf("alpha-skill should be imported, stat err: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(skillsDir, "beta", "SKILL.md")); statErr != nil {
		t.Fatalf("beta should be imported by fallback dir name, stat err: %v", statErr)
	}
}

func TestImportSkillConflictSkipsByDefault(t *testing.T) {
	enterIsolatedWorkingDir(t)
	skillsDir := t.TempDir()
	writeSkillFile(t, skillsDir, "conflict-skill", "old", "# old")

	sourceDir := t.TempDir()
	sourceFile := filepath.Join(sourceDir, "conflict.md")
	writeRawFile(t, sourceFile, strings.Join([]string{
		"---",
		"name: conflict-skill",
		"description: new",
		"---",
		"",
		"# new",
	}, "\n"))

	s := &DeepServer{userSkillsDir: skillsDir, skillsDir: skillsDir}
	result, err := s.importSkill(sourceFile, false)
	if err != nil {
		t.Fatalf("importSkill(conflict) error: %v", err)
	}

	imported := mustImportedList(t, result)
	if len(imported) != 0 {
		t.Fatalf("expected 0 imported on conflict, got %d", len(imported))
	}
	skipped := mustSkippedList(t, result)
	if len(skipped) != 1 {
		t.Fatalf("expected 1 skipped on conflict, got %d", len(skipped))
	}
	reason, _ := skipped[0]["reason"].(string)
	if !strings.Contains(reason, "已存在") {
		t.Fatalf("expected skip reason to mention existing skill, got %q", reason)
	}
}

func TestDeleteSkillBlocksReferencedSkill(t *testing.T) {
	enterIsolatedWorkingDir(t)
	skillsDir := t.TempDir()
	configDir := t.TempDir()

	blockedPath := writeSkillFile(t, skillsDir, "blocked-skill", "blocked", "# blocked")
	writeSkillFile(t, skillsDir, "free-skill", "free", "# free")

	cfgRaw := `version: "1.0"
runtime:
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: []
    tool_groups: {}
  agents:
    analyst:
      is_main: true
      display_name: Analyst
      description: ""
      enabled: true
      tool_policy:
        private:
          exclusive: []
          tool_groups: []
        share:
          exclusive: []
          tool_groups: []
      skill_policy:
        private: [blocked-skill]
        share: []
      prompts:
        static_instructions: "test"
        user_prompt_template: "{{ user_input }}"
`
	if err := os.WriteFile(filepath.Join(configDir, "skillrefs.yaml"), []byte(cfgRaw), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	s := &DeepServer{
		userSkillsDir:      skillsDir,
		skillsDir:          skillsDir,
		agentConfigManager: config.NewAgentConfigManager(configDir),
	}

	err := s.deleteSkill("blocked-skill")
	if err == nil {
		t.Fatalf("expected delete blocked-skill to fail")
	}
	if !strings.Contains(err.Error(), "正在被引用") {
		t.Fatalf("expected reference-block error, got: %v", err)
	}
	if _, statErr := os.Stat(blockedPath); statErr != nil {
		t.Fatalf("blocked skill should remain, stat err: %v", statErr)
	}

	if err := s.deleteSkill("free-skill"); err != nil {
		t.Fatalf("delete free skill failed: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(skillsDir, "free-skill")); !os.IsNotExist(statErr) {
		t.Fatalf("free skill directory should be deleted")
	}
}

func TestScopedSkillMutationHelpersTargetExclusiveLayer(t *testing.T) {
	enterIsolatedWorkingDir(t)
	globalDir := t.TempDir()
	rootDir := t.TempDir()
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

	acm := config.NewAgentConfigManager(filepath.Join(rootDir, ".autocarto", "agent_configs"))
	exclusiveDir := acm.ExclusiveSkillsDir("default")
	s := &DeepServer{userSkillsDir: globalDir, skillsDir: globalDir, agentConfigManager: acm}

	if err := s.saveSkillWithScope("scoped-skill", "exclusive", "default", "scoped package", "# scoped"); err != nil {
		t.Fatalf("saveSkillWithScope error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "scoped-skill", "SKILL.md")); err != nil {
		t.Fatalf("expected scoped save target in exclusive dir: %v", err)
	}

	importSource := filepath.Join(t.TempDir(), "scoped-import.md")
	writeRawFile(t, importSource, strings.Join([]string{
		"---",
		"name: imported-scoped-skill",
		"description: imported from scoped helper",
		"kind: workflow",
		"---",
		"",
		"# imported",
	}, "\n"))
	if _, err := s.importSkillWithScope(importSource, false, "exclusive", "default"); err != nil {
		t.Fatalf("importSkillWithScope error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "imported-scoped-skill", "SKILL.md")); err != nil {
		t.Fatalf("expected scoped import target in exclusive dir: %v", err)
	}

	if err := s.deleteSkillWithScope("scoped-skill", "exclusive", "default"); err != nil {
		t.Fatalf("deleteSkillWithScope error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "scoped-skill")); !os.IsNotExist(err) {
		t.Fatalf("expected scoped delete to remove exclusive package, err=%v", err)
	}
}

func TestDefaultSkillMutationHelpersKeepGlobalTargetWithExclusiveOverlayPresent(t *testing.T) {
	enterIsolatedWorkingDir(t)
	globalDir := t.TempDir()
	exclusiveDir := t.TempDir()
	store := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{globalDir, exclusiveDir},
		ActiveConfigName: "default",
		ResolveExclusiveSkillDir: func(configName string) string {
			if configName == "default" {
				return exclusiveDir
			}
			return ""
		},
	}, nil)
	s := &DeepServer{userSkillsDir: globalDir, skillsDir: globalDir, skillCatalogStore: store}

	if err := s.saveSkill("default-global-skill", "default global", "# default global"); err != nil {
		t.Fatalf("saveSkill error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "default-global-skill", "SKILL.md")); err != nil {
		t.Fatalf("expected default save target in global dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "default-global-skill", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected exclusive dir untouched by default save, err=%v", err)
	}

	importSource := filepath.Join(t.TempDir(), "default-import.md")
	writeRawFile(t, importSource, strings.Join([]string{
		"---",
		"name: imported-default-global-skill",
		"description: imported default global",
		"kind: workflow",
		"---",
		"",
		"# imported default global",
	}, "\n"))
	if _, err := s.importSkill(importSource, false); err != nil {
		t.Fatalf("importSkill error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "imported-default-global-skill", "SKILL.md")); err != nil {
		t.Fatalf("expected default import target in global dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "imported-default-global-skill", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected exclusive dir untouched by default import, err=%v", err)
	}
}
