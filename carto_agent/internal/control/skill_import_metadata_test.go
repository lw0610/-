package control

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/settings"
)

func writeMetadataImportFixture(t *testing.T, root, packageName string, enabled bool) string {
	t.Helper()
	source := filepath.Join(root, packageName)
	writePackageTestFile(t, filepath.Join(source, "SKILL.md"), "---\nname: "+packageName+"\nkind: category\ntools: [fixture_tool]\nenabled: true\n---\ncontent\n")
	writePackageTestFile(t, filepath.Join(source, "references", "guide.md"), "reference "+packageName)
	activation := "false"
	if enabled {
		activation = "true"
	}
	manifest := filepath.Join(root, "extension.yaml")
	writePackageTestFile(t, manifest, "apiVersion: extension/v1\nkind: skill-package\nmetadata:\n  name: shared-extension\nspec:\n  source_dir: "+source+"\n  activation:\n    scope: global\n    enabled_by_default: "+activation+"\n")
	return manifest
}

func TestSkillImportMetadataConflictSkipsDifferentResourceDirectories(t *testing.T) {
	globalDir, configDir, sourceRoot := t.TempDir(), t.TempDir(), t.TempDir()
	manifest := writeMetadataImportFixture(t, sourceRoot, "first", true)
	refresher := &stubSnapshotRefresher{}
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		SkillCatalog: settings.NewFileSkillService(globalDir, nil), SnapshotRefresher: refresher,
		AgentConfigManager: config.NewAgentConfigManager(filepath.Join(configDir, "agent_configs")),
	})
	if _, err := service.ImportSkill(manifest, false); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(configDir, "extensions", "shared-extension.yaml")
	before, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest = writeMetadataImportFixture(t, sourceRoot, "second", true)
	result, err := service.ImportSkill(manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	mutation := result["mutation"].(settings.SettingsMutationResult)
	if mutation.Persisted || !mutation.Idempotent || len(result["imported"].([]map[string]any)) != 0 {
		t.Fatalf("metadata conflict must skip whole extension: %#v", result)
	}
	skipped := result["skipped"].([]map[string]any)
	if len(skipped) != 1 || !strings.Contains(skipped[0]["reason"].(string), "同名扩展导入记录") {
		t.Fatalf("missing metadata conflict explanation: %#v", skipped)
	}
	after, err := os.ReadFile(metadataPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("metadata overwritten without consent: %s, %v", after, err)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "second")); !os.IsNotExist(err) {
		t.Fatalf("second package was installed: %v", err)
	}
	if len(refresher.reasons) != 1 {
		t.Fatalf("skipped import refreshed capabilities: %#v", refresher.reasons)
	}
	if result, err = service.ImportSkill(manifest, true); err != nil || len(result["imported"].([]map[string]any)) != 1 {
		t.Fatalf("explicit metadata overwrite failed: %#v, %v", result, err)
	}
	after, err = os.ReadFile(metadataPath)
	if err != nil || !strings.Contains(string(after), "package_id: second") {
		t.Fatalf("metadata not updated on overwrite: %s, %v", after, err)
	}
}

type importPostFailureCatalog struct {
	*settings.FileSkillService
	onManagedDir    func()
	activationError error
}

func (s *importPostFailureCatalog) ManagedSkillsDir() string {
	if s.onManagedDir != nil {
		s.onManagedDir()
	}
	return s.FileSkillService.ManagedSkillsDir()
}

func (s *importPostFailureCatalog) SetSkillPackageEnabledByScope(name, scope, owner string, enabled bool) error {
	if s.activationError != nil {
		return s.activationError
	}
	return s.FileSkillService.SetSkillPackageEnabledByScope(name, scope, owner, enabled)
}

func TestSkillImportRegistrationFailureReportsPersistedResources(t *testing.T) {
	globalDir, configDir, sourceRoot := t.TempDir(), t.TempDir(), t.TempDir()
	manifest := writeMetadataImportFixture(t, sourceRoot, "package", true)
	catalog := &importPostFailureCatalog{FileSkillService: settings.NewFileSkillService(globalDir, nil)}
	// Simulate the metadata destination changing after the read-only preflight.
	catalog.onManagedDir = func() {
		if err := os.MkdirAll(filepath.Join(configDir, "extensions", "shared-extension.yaml"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	refresher := &stubSnapshotRefresher{}
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		SkillCatalog: catalog, SnapshotRefresher: refresher,
		AgentConfigManager: config.NewAgentConfigManager(filepath.Join(configDir, "agent_configs")),
	})
	result, err := service.ImportSkill(manifest, false)
	if err != nil {
		t.Fatalf("post-import metadata failure must not hide persisted resources: %v", err)
	}
	imported := result["imported"].([]map[string]any)
	mutation := result["mutation"].(settings.SettingsMutationResult)
	if len(imported) != 1 || !mutation.Persisted || !strings.Contains(strings.Join(mutation.Warnings, "\n"), "导入记录保存失败") {
		t.Fatalf("missing partial-success feedback: %#v", result)
	}
	if imported[0]["metadata_path"] != "" {
		t.Fatalf("must not advertise unsaved metadata: %#v", imported[0])
	}
	reference, err := os.ReadFile(filepath.Join(globalDir, "package", "references", "guide.md"))
	if err != nil || string(reference) != "reference package" {
		t.Fatalf("imported reference missing: %s, %v", reference, err)
	}
	if len(refresher.reasons) != 1 {
		t.Fatalf("persisted resources must refresh capabilities: %#v", refresher.reasons)
	}
}

func TestSkillImportActivationFailureReportsPersistedResources(t *testing.T) {
	globalDir, configDir, sourceRoot := t.TempDir(), t.TempDir(), t.TempDir()
	manifest := writeMetadataImportFixture(t, sourceRoot, "package", false)
	catalog := &importPostFailureCatalog{FileSkillService: settings.NewFileSkillService(globalDir, nil), activationError: errors.New("simulated activation failure")}
	refresher := &stubSnapshotRefresher{}
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		SkillCatalog: catalog, SnapshotRefresher: refresher,
		AgentConfigManager: config.NewAgentConfigManager(filepath.Join(configDir, "agent_configs")),
	})
	result, err := service.ImportSkill(manifest, false)
	if err != nil {
		t.Fatalf("activation failure must not hide persisted resources: %v", err)
	}
	mutation := result["mutation"].(settings.SettingsMutationResult)
	if len(result["imported"].([]map[string]any)) != 1 || !mutation.Persisted || !strings.Contains(strings.Join(mutation.Warnings, "\n"), "默认停用设置失败") {
		t.Fatalf("missing activation feedback: %#v", result)
	}
	if result["activation_incomplete"] != true || len(refresher.reasons) != 1 {
		t.Fatalf("incomplete activation state not retained: %#v", result)
	}
}

func TestSkillImportInvalidMetadataPathFailsBeforeWritingResources(t *testing.T) {
	globalDir, configDir, sourceRoot := t.TempDir(), t.TempDir(), t.TempDir()
	manifest := writeMetadataImportFixture(t, sourceRoot, "package", true)
	writePackageTestFile(t, filepath.Join(configDir, "extensions"), "not a directory")
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		SkillCatalog:       settings.NewFileSkillService(globalDir, nil),
		AgentConfigManager: config.NewAgentConfigManager(filepath.Join(configDir, "agent_configs")),
	})
	if _, err := service.ImportSkill(manifest, true); err == nil {
		t.Fatal("expected invalid metadata path error")
	}
	if _, err := os.Stat(filepath.Join(globalDir, "package")); !os.IsNotExist(err) {
		t.Fatalf("metadata preflight failure wrote package: %v", err)
	}
}
