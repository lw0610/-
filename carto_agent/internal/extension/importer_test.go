package extension

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
)

func TestImportSkillPackageCopiesSourceToGlobalSkillsDir(t *testing.T) {
	globalDir := t.TempDir()
	configBaseDir := t.TempDir()
	sourceDir := filepath.Join(t.TempDir(), "demo-package")
	writeManifestPackageFixture(t, sourceDir, "demo-package")
	manifest := &Manifest{
		APIVersion: APIVersionV1,
		Kind:       KindSkillPackage,
		Metadata:   Metadata{Name: "demo-extension"},
		Spec: Spec{
			SourceDir: sourceDir,
			Activation: Activation{
				Scope:            "global",
				EnabledByDefault: false,
				RebuildPolicy:    "auto",
			},
		},
	}

	result, err := ImportSkillPackage(manifest, globalDir, configBaseDir)
	if err != nil {
		t.Fatalf("ImportSkillPackage returned error: %v", err)
	}
	if result.TargetDir != filepath.Join(globalDir, "demo-package") {
		t.Fatalf("unexpected target dir: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(result.TargetDir, "SKILL.md")); err != nil {
		t.Fatalf("expected imported SKILL.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(result.TargetDir, "references.md")); err != nil {
		t.Fatalf("expected imported package reference: %v", err)
	}
	if result.Scope != "global" || result.OwnerConfig != "" {
		t.Fatalf("unexpected import result scope metadata: %#v", result)
	}
}

func TestImportSkillPackageCopiesSourceToExclusiveSkillsDir(t *testing.T) {
	globalDir := t.TempDir()
	configBaseDir := t.TempDir()
	sourceDir := filepath.Join(t.TempDir(), "demo-package")
	writeManifestPackageFixture(t, sourceDir, "demo-package")
	manifest := &Manifest{
		APIVersion: APIVersionV1,
		Kind:       KindSkillPackage,
		Metadata:   Metadata{Name: "demo-extension"},
		Spec: Spec{
			SourceDir: sourceDir,
			Activation: Activation{
				Scope:            "exclusive",
				OwnerConfig:      "default",
				EnabledByDefault: false,
				RebuildPolicy:    "auto",
			},
		},
	}

	result, err := ImportSkillPackage(manifest, globalDir, configBaseDir)
	if err != nil {
		t.Fatalf("ImportSkillPackage returned error: %v", err)
	}
	expectedTarget := filepath.Join(config.ResolveExclusiveSkillsDir(configBaseDir, "default"), "demo-package")
	if result.TargetDir != expectedTarget {
		t.Fatalf("unexpected exclusive target dir: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(expectedTarget, "SKILL.md")); err != nil {
		t.Fatalf("expected imported exclusive SKILL.md: %v", err)
	}
	if result.Scope != "exclusive" || result.OwnerConfig != "default" {
		t.Fatalf("unexpected exclusive import metadata: %#v", result)
	}
}

func TestImportSkillPackageRejectsRelativeSourceDir(t *testing.T) {
	_, err := ImportSkillPackage(&Manifest{
		APIVersion: APIVersionV1,
		Kind:       KindSkillPackage,
		Metadata:   Metadata{Name: "demo-extension"},
		Spec: Spec{
			SourceDir: "relative/path",
			Activation: Activation{
				Scope: "global",
			},
		},
	}, t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("expected relative source dir error")
	}
	if err.Error() != `extension resource source_dir must be absolute` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportManifestCopiesToolAndSkillResources(t *testing.T) {
	globalSkillsDir := t.TempDir()
	globalToolsDir := t.TempDir()
	configBaseDir := t.TempDir()
	skillSourceDir := filepath.Join(t.TempDir(), "hydrology-skill")
	toolSourceDir := filepath.Join(t.TempDir(), "hydrology-tools")
	writeManifestPackageFixture(t, skillSourceDir, "hydrology-skill")
	writeToolPackageFixture(t, toolSourceDir, "stream_order")
	manifest := &Manifest{
		APIVersion: APIVersionV2,
		Kind:       KindPackageImport,
		Metadata:   Metadata{Name: "hydrology-pack"},
		Spec: Spec{
			Resources: []Resource{
				{Type: ResourceToolPackage, SourceDir: toolSourceDir},
				{Type: ResourceSkillPackage, SourceDir: skillSourceDir},
			},
			Activation: Activation{
				Scope:            "global",
				EnabledByDefault: true,
				RebuildPolicy:    "immediate",
			},
		},
	}

	result, err := ImportManifest(manifest, globalSkillsDir, globalToolsDir, configBaseDir)
	if err != nil {
		t.Fatalf("ImportManifest returned error: %v", err)
	}
	if len(result.Resources) != 2 {
		t.Fatalf("expected two imported resources, got %#v", result)
	}
	if _, err := os.Stat(filepath.Join(config.ResolveGlobalToolsDir(configBaseDir), "hydrology-tools", "analysis", "stream_order", "tool.yaml")); err != nil {
		t.Fatalf("expected imported tool manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalSkillsDir, "hydrology-skill", "SKILL.md")); err != nil {
		t.Fatalf("expected imported skill package: %v", err)
	}
}

func TestImportManifestCopiesToolResourceToExclusiveToolsDir(t *testing.T) {
	configBaseDir := t.TempDir()
	toolSourceDir := filepath.Join(t.TempDir(), "exclusive-tools")
	writeToolPackageFixture(t, toolSourceDir, "trace_flow")
	manifest := &Manifest{
		APIVersion: APIVersionV2,
		Kind:       KindPackageImport,
		Metadata:   Metadata{Name: "exclusive-tools-pack"},
		Spec: Spec{
			Resources: []Resource{
				{Type: ResourceToolPackage, SourceDir: toolSourceDir},
			},
			Activation: Activation{
				Scope:            "exclusive",
				OwnerConfig:      "default",
				EnabledByDefault: true,
				RebuildPolicy:    "immediate",
			},
		},
	}

	result, err := ImportManifest(manifest, t.TempDir(), t.TempDir(), configBaseDir)
	if err != nil {
		t.Fatalf("ImportManifest returned error: %v", err)
	}
	expectedTarget := filepath.Join(config.ResolveExclusiveToolsDir(configBaseDir, "default"), "exclusive-tools")
	if len(result.Resources) != 1 || result.Resources[0].TargetDir != expectedTarget {
		t.Fatalf("unexpected exclusive import result: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(expectedTarget, "analysis", "trace_flow", "tool.yaml")); err != nil {
		t.Fatalf("expected imported exclusive tool manifest: %v", err)
	}
}

func writeManifestPackageFixture(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir package dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: demo\nkind: category\ntools: [fixture_tool]\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references.md"), []byte("tool usage guidance"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeToolPackageFixture(t *testing.T, dir, toolName string) {
	t.Helper()
	targetDir := filepath.Join(dir, "analysis", toolName)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir tool package dir failed: %v", err)
	}
	raw := "apiVersion: tool-package/v1\nkind: ToolPackage\nmetadata:\n  name: " + toolName + "\n  domain: analysis\n  runtime: qgis\nspec:\n  description: demo tool\n  parameters:\n    - name: input\n      type: string\nexecution:\n  provider: qgis\n  binding: handler\n  handler: handlers.analysis." + toolName + "\n"
	if err := os.WriteFile(filepath.Join(targetDir, "tool.yaml"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write tool manifest failed: %v", err)
	}
}
