package subagents

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestAutomaticPackagePoliciesRespectRoleAndMode(t *testing.T) {
	cfg := &RuntimeSkillConfig{
		Mode: "build",
		Global: RuntimeSkillGlobalConfig{
			SkillPackages: []string{"foundation"},
			PackageTools:  map[string][]string{"foundation": {"shared"}, "launch": {"start"}, "planning": {"query"}, "executor": {"compile"}, "creator": {"create"}},
			PackageMetadata: map[string]RuntimeSkillPackageMetadata{
				"foundation": {System: true}, "launch": {Hidden: true, AutoMount: "main"}, "planning": {Hidden: true, AutoMount: "plan_main"}, "executor": {Hidden: true, AutoMount: "executor:route_knowledge"}, "creator": {Hidden: true},
			},
		},
		Agents: map[string]RuntimeSkillAgentConfig{"main": {IsMain: true}, "worker": {}, "executor": {Executor: "route_knowledge"}},
	}
	check := func(agent string, want []string) {
		t.Helper()
		got := cfg.GetAgentAllowedTools(agent)
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s/%s tools=%v want=%v", cfg.Mode, agent, got, want)
		}
	}
	check("main", []string{"shared", "start"})
	check("worker", []string{"shared"})
	check("executor", []string{"shared", "compile"})
	cfg.Mode = "plan"
	check("main", []string{"shared", "query"})
	cfg.Agents["worker"] = RuntimeSkillAgentConfig{SkillPackages: []string{"creator"}}
	check("worker", []string{"shared", "create"})
	if got := cfg.GetAgentSkills("main"); !reflect.DeepEqual(got, []string{"foundation", "planning"}) {
		t.Fatalf("unexpected mounted packages: %v", got)
	}
}

func TestPackageCatalogLoadsMetadataAndDisabledOverrideShadowsBase(t *testing.T) {
	base := t.TempDir()
	override := t.TempDir()
	writeCategoryPackage(t, base, "foundation", []string{"shared"})
	path := filepath.Join(base, "foundation", "SKILL.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), "kind: category", "kind: category\ndisplay_name: 基础能力\ndomain: 系统\nsystem: true\nhidden: false\nauto_mount: ''", 1))
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadPackageCatalogMultiDir(base)
	if err != nil {
		t.Fatal(err)
	}
	meta := catalog.Metadata["foundation"]
	if meta.DisplayName != "基础能力" || meta.Domain != "系统" || !meta.System || meta.Hidden {
		t.Fatalf("metadata lost: %#v", meta)
	}
	writeCategoryPackage(t, override, "foundation", []string{"other"})
	path = filepath.Join(override, "foundation", "SKILL.md")
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), "kind: category", "kind: category\nenabled: false", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err = LoadPackageCatalogMultiDir(base, override)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Tools) != 0 || len(catalog.Metadata) != 0 {
		t.Fatalf("disabled override did not shadow base: %#v", catalog)
	}
}

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
