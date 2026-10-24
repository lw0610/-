package skill

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadPackageDirectoryParsesFrontMatterConfigAndTools(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillPackageFixture(t, root, "vector-tools", `---
name: vector-tools
description: Vector tools.
kind: category
backend: compute
enabled: true
tags: [vector, tools]
tools:
  - buffer_analysis
---

# Vector tools

Package body.
`, map[string]string{
		"config.yaml": `enabled: false
owner: test
`,
		"tools/buffer_analysis.yaml": toolDefinitionFixture("buffer_analysis", "compute", "Create a buffer.", "buffer"),
	})

	pkg, ok, err := loadPackageDirectory(filepath.Join(root, "vector-tools"), root)
	if err != nil {
		t.Fatalf("loadPackageDirectory() error = %v", err)
	}
	if !ok {
		t.Fatal("expected package to load")
	}
	if pkg.Name != "vector-tools" {
		t.Fatalf("unexpected package name: %s", pkg.Name)
	}
	if pkg.Kind != SkillPackageKindCategory {
		t.Fatalf("unexpected package kind: %s", pkg.Kind)
	}
	if pkg.MarkdownBody != "# Vector tools\n\nPackage body." {
		t.Fatalf("unexpected markdown body: %q", pkg.MarkdownBody)
	}
	if pkg.Config == nil || pkg.Config.Enabled == nil || *pkg.Config.Enabled {
		t.Fatalf("expected config enabled override to false, got %#v", pkg.Config)
	}
	if pkg.Enabled {
		t.Fatal("expected effective package enabled to be false")
	}
	if len(pkg.FrontMatter.Tags) != 2 || pkg.FrontMatter.Tags[0] != "vector" {
		t.Fatalf("unexpected frontmatter tags: %#v", pkg.FrontMatter.Tags)
	}
	if len(pkg.ToolRefs) != 1 || pkg.ToolRefs[0] != "buffer_analysis" {
		t.Fatalf("unexpected tool refs: %#v", pkg.ToolRefs)
	}
	if len(pkg.Tools) != 0 {
		t.Fatalf("expected embedded tools to be ignored, got %d", len(pkg.Tools))
	}
}

func TestLoadPackagesFromDirectoryInfersWorkflowWithoutTools(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillPackageFixture(t, root, "buffer-workflow", `---
name: buffer-workflow
description: Buffer workflow.
requires_tools:
  - buffer
---

# Buffer workflow
`, nil)

	packages, err := loadPackagesFromDirectory(root)
	if err != nil {
		t.Fatalf("loadPackagesFromDirectory() error = %v", err)
	}
	if len(packages) != 1 {
		t.Fatalf("unexpected package count: %d", len(packages))
	}
	if packages[0].Kind != SkillPackageKindWorkflow {
		t.Fatalf("expected inferred workflow kind, got %s", packages[0].Kind)
	}
	if len(packages[0].RequiresTools) != 1 || packages[0].RequiresTools[0] != "buffer" {
		t.Fatalf("unexpected requires tools: %#v", packages[0].RequiresTools)
	}
}

func TestLoadPackageDirectoryParsesToolRefsFromFrontMatter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillPackageFixture(t, root, "buffer-workflow", `---
name: buffer-workflow
description: Buffer workflow.
kind: workflow
tool_refs:
  - buffer
  - clip
---

# Buffer workflow
`, nil)

	pkg, ok, err := loadPackageDirectory(filepath.Join(root, "buffer-workflow"), root)
	if err != nil {
		t.Fatalf("loadPackageDirectory() error = %v", err)
	}
	if !ok {
		t.Fatal("expected workflow package to load")
	}
	if len(pkg.ToolRefs) != 2 || pkg.ToolRefs[0] != "buffer" || pkg.ToolRefs[1] != "clip" {
		t.Fatalf("unexpected tool refs: %#v", pkg.ToolRefs)
	}
}

func TestLoadPackageDirectoryIgnoresLegacyToolsDirectoryForCategoryPackage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillPackageFixture(t, root, "vector-tools", `---
name: vector-tools
description: Vector tools.
kind: category
tools:
  - buffer_analysis
---

# Vector tools
`, map[string]string{
		"tools/buffer_analysis.yaml": toolDefinitionFixture("buffer_analysis", "compute", "Create a buffer.", "buffer"),
	})

	pkg, ok, err := loadPackageDirectory(filepath.Join(root, "vector-tools"), root)
	if err != nil {
		t.Fatalf("loadPackageDirectory() error = %v", err)
	}
	if !ok {
		t.Fatal("expected package to load")
	}
	if len(pkg.ToolRefs) != 1 || pkg.ToolRefs[0] != "buffer_analysis" {
		t.Fatalf("unexpected tool refs: %#v", pkg.ToolRefs)
	}
	if len(pkg.Tools) != 0 {
		t.Fatalf("expected legacy tools directory to be ignored, got %d tools", len(pkg.Tools))
	}
}

func TestLoadPackageDirectoryIgnoresLegacyToolsDirectoryForWorkflowPackage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillPackageFixture(t, root, "broken-workflow", `---
name: broken-workflow
description: Broken workflow.
kind: workflow
tools:
  - buffer_analysis
---

# Broken workflow
`, map[string]string{
		"tools/buffer_analysis.yaml": toolDefinitionFixture("buffer_analysis", "compute", "Create a buffer.", "buffer"),
	})

	pkg, ok, err := loadPackageDirectory(filepath.Join(root, "broken-workflow"), root)
	if err != nil {
		t.Fatalf("loadPackageDirectory() error = %v", err)
	}
	if !ok {
		t.Fatal("expected workflow package to load")
	}
	if len(pkg.ToolRefs) != 1 || pkg.ToolRefs[0] != "buffer_analysis" {
		t.Fatalf("unexpected tool refs: %#v", pkg.ToolRefs)
	}
	if len(pkg.Tools) != 0 {
		t.Fatalf("expected legacy tools directory to be ignored, got %d tools", len(pkg.Tools))
	}
}

func TestPackageDirectoryIgnoresRetiredSerialWorkflowFile(t *testing.T) {
	root := t.TempDir()
	writeSkillPackageFixture(t, root, "vector-tools", "---\nname: vector-tools\nkind: category\ntools: [open_project]\n---\n使用说明", map[string]string{"workflow.yaml": "invalid retired workflow content"})
	pkg, ok, err := loadPackageDirectory(filepath.Join(root, "vector-tools"), root)
	if err != nil || !ok {
		t.Fatal(pkg, ok, err)
	}
	if len(pkg.ToolRefs) != 1 || pkg.ToolRefs[0] != "open_project" {
		t.Fatal("category references lost")
	}
	if _, exists := reflect.TypeFor[SkillPackage]().FieldByName("Workflow"); exists {
		t.Fatal("retired executable package field remains")
	}
	raw, err := json.Marshal(pkg)
	if err != nil || strings.Contains(string(raw), "\"workflow\"") {
		t.Fatal("retired workflow published", err)
	}
}
