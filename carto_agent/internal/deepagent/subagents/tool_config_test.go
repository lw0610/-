package subagents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPackageToolsMultiDirLaterDirectoryOverridesSamePackage(t *testing.T) {
	baseDir := t.TempDir()
	overrideDir := t.TempDir()

	writeCategoryPackage(t, baseDir, "vector-tools", []string{"buffer", "clip"})
	writeCategoryPackage(t, overrideDir, "vector-tools", []string{"dissolve"})

	packageTools, err := LoadPackageToolsMultiDir(baseDir, overrideDir)
	if err != nil {
		t.Fatalf("LoadPackageToolsMultiDir failed: %v", err)
	}

	tools := packageTools["vector-tools"]
	if len(tools) != 1 || tools[0] != "dissolve" {
		t.Fatalf("expected later directory to override same package tools, got %#v", tools)
	}
}

func TestLoadPackageToolsSupportsToolRefsField(t *testing.T) {
	baseDir := t.TempDir()

	packageDir := filepath.Join(baseDir, "project-tools")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatalf("mkdir package dir failed: %v", err)
	}
	content := "---\nname: project-tools\nkind: category\nbackend: qgis\ntool_refs:\n  - get_project_info\n  - get_layers_info\n---\n"
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md failed: %v", err)
	}

	packageTools, err := LoadPackageToolsMultiDir(baseDir)
	if err != nil {
		t.Fatalf("LoadPackageToolsMultiDir failed: %v", err)
	}
	tools := packageTools["project-tools"]
	if len(tools) != 2 || tools[0] != "get_project_info" || tools[1] != "get_layers_info" {
		t.Fatalf("unexpected tool refs: %#v", tools)
	}
}

func writeCategoryPackage(t *testing.T, rootDir, packageName string, toolNames []string) {
	t.Helper()
	packageDir := filepath.Join(rootDir, packageName)
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatalf("mkdir tools dir failed: %v", err)
	}
	toolRefs := strings.Join(toolNames, "\n  - ")
	frontMatter := "---\nname: " + packageName + "\nkind: category\nbackend: compute\ntools:\n  - " + toolRefs + "\n---\n"
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(frontMatter), 0o644); err != nil {
		t.Fatalf("write SKILL.md failed: %v", err)
	}
}
