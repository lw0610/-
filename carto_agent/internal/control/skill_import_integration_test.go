package control

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/settings"
)

type importActiveRunFixture struct{}

func (importActiveRunFixture) ListActiveRuns() []ActiveRunInfo {
	return []ActiveRunInfo{{SessionID: "existing-task", Status: "running"}}
}

func TestSkillImportSkippedExtensionPreservesMetadataAndEnablement(t *testing.T) {
	globalDir, configDir, sourceRoot := t.TempDir(), t.TempDir(), t.TempDir()
	source := filepath.Join(sourceRoot, "demo")
	writePackageTestFile(t, filepath.Join(source, "SKILL.md"), "---\nname: demo\nkind: category\ntools: [fixture_tool]\ndescription: demo\n---\nfirst version\n")
	manifest := filepath.Join(sourceRoot, "extension.yaml")
	manifestText := "apiVersion: extension/v1\nkind: skill-package\nmetadata:\n  name: demo-extension\nspec:\n  source_dir: " + source + "\n  activation:\n    scope: global\n    enabled_by_default: false\n"
	writePackageTestFile(t, manifest, manifestText)
	catalog := settings.NewFileSkillService(globalDir, nil)
	refresher := &stubSnapshotRefresher{}
	runtime := &stubRuntimeController{activeConfig: "default"}
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		SkillCatalog: catalog, SnapshotRefresher: refresher, RuntimeController: runtime,
		AgentConfigManager: config.NewAgentConfigManager(filepath.Join(configDir, "agent_configs")),
	})
	if _, err := service.ImportSkill(manifest, false); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(configDir, "extensions", "demo-extension.yaml")
	metadataBefore, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	writePackageTestFile(t, manifest, strings.ReplaceAll(manifestText, "enabled_by_default: false", "enabled_by_default: true"))
	writePackageTestFile(t, filepath.Join(source, "SKILL.md"), "---\nname: demo\nkind: category\ntools: [fixture_tool]\ndescription: demo\n---\nsecond version\n")
	result, err := service.ImportSkill(manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	mutation := result["mutation"].(settings.SettingsMutationResult)
	if mutation.Persisted || !mutation.Idempotent || mutation.RuntimeApplied || mutation.RequiredAction != settings.MutationRequiredActionNone {
		t.Fatalf("skipped import must be a no-op: %#v", mutation)
	}
	if len(result["imported"].([]map[string]any)) != 0 || len(result["skipped"].([]map[string]any)) != 1 {
		t.Fatalf("unexpected import result: %#v", result)
	}
	metadataAfter, err := os.ReadFile(metadataPath)
	if err != nil || !bytes.Equal(metadataBefore, metadataAfter) {
		t.Fatalf("skipped import changed extension metadata: %v", err)
	}
	detail, err := catalog.GetSkillPackage("demo")
	if err != nil || detail.Enabled {
		t.Fatalf("skipped import changed activation: %#v, %v", detail, err)
	}
	if len(refresher.reasons) != 1 || runtime.activeRuntimeCalls != 0 {
		t.Fatalf("skipped import refreshed runtime: %#v, %d", refresher.reasons, runtime.activeRuntimeCalls)
	}
	result, err = service.ImportSkill(manifest, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result["mutation"].(settings.SettingsMutationResult).Persisted || len(refresher.reasons) != 2 {
		t.Fatalf("explicit overwrite not applied: %#v", result)
	}
	updated, err := os.ReadFile(filepath.Join(globalDir, "demo", "SKILL.md"))
	if err != nil || !strings.Contains(string(updated), "second version") {
		t.Fatalf("explicit overwrite did not replace package: %s, %v", updated, err)
	}
}

func TestSkillImportDefersToolRuntimeRebuildDuringActiveRun(t *testing.T) {
	globalDir, configDir, sourceRoot := t.TempDir(), t.TempDir(), t.TempDir()
	source := filepath.Join(sourceRoot, "tools")
	writePackageTestFile(t, filepath.Join(source, "demo", "tool.yaml"), `apiVersion: tool-package/v1
kind: ToolPackage
metadata:
  name: import_demo
  domain: analysis
  runtime: qgis
spec:
  description: import demo
  parameters: []
execution:
  provider: qgis
  binding: handler
  handler: handlers.analysis.import_demo
`)
	manifest := filepath.Join(sourceRoot, "extension.yaml")
	writePackageTestFile(t, manifest, "apiVersion: extension/v2\nkind: PackageImport\nmetadata:\n  name: tool-extension\nspec:\n  resources:\n    - type: tool_package\n      source_dir: "+source+"\n  activation:\n    scope: global\n    enabled_by_default: true\n")
	runtime := &stubRuntimeController{activeConfig: "default"}
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		SkillCatalog: settings.NewFileSkillService(globalDir, nil), RuntimeController: runtime,
		AgentConfigManager: config.NewAgentConfigManager(filepath.Join(configDir, "agent_configs")),
		ActiveRuns:         importActiveRunFixture{},
	})
	result, err := service.ImportSkill(manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	mutation := result["mutation"].(settings.SettingsMutationResult)
	if !mutation.Persisted || mutation.RuntimeApplied || runtime.activeRuntimeCalls != 0 || mutation.RequiredAction != settings.MutationRequiredActionRuntimeRebuild {
		t.Fatalf("active task must not be interrupted by rebuilding: %#v calls=%d", mutation, runtime.activeRuntimeCalls)
	}
	if !strings.Contains(strings.Join(mutation.Warnings, " "), "运行时重建已延后") {
		t.Fatalf("missing deferred activation explanation: %#v", mutation)
	}
}

func TestSkillImportOrdinaryPackageRefreshesOnlyAfterWrite(t *testing.T) {
	source := filepath.Join(t.TempDir(), "demo.md")
	writePackageTestFile(t, source, "---\nname: demo\nkind: category\ntools: [fixture_tool]\ndescription: demo\n---\nbody\n")
	refresher := &stubSnapshotRefresher{}
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		SkillCatalog: settings.NewFileSkillService(t.TempDir(), nil), SnapshotRefresher: refresher,
	})
	for _, overwrite := range []bool{false, false, true} {
		if _, err := service.ImportSkill(source, overwrite); err != nil {
			t.Fatal(err)
		}
	}
	if len(refresher.reasons) != 2 || refresher.reasons[0] != "skill_import" || refresher.reasons[1] != "skill_import" {
		t.Fatalf("unexpected refreshes: %#v", refresher.reasons)
	}
}
