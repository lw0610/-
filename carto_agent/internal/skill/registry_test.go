package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

func TestRegistryLoadDirectoriesValid(t *testing.T) {
	t.Parallel()

	toolRoot := t.TempDir()
	writeToolFixture(t, toolRoot, "system/vector-tools/buffer_analysis.yaml", toolDefinitionFixtureV1("buffer_analysis", "vector-tools", "cli", "buffer", ""))
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("toolReg.LoadDirectories() error = %v", err)
	}

	root := t.TempDir()
	writeSkillPackageFixture(t, root, "vector-tools", `---
name: vector-tools
description: Vector tools.
kind: category
tools:
  - buffer_analysis
---

# Vector tools
`, nil)
	writeSkillPackageFixture(t, root, "buffer-workflow", `---
name: buffer-workflow
description: Buffer workflow guidance.
kind: workflow
requires_tools:
  - buffer_analysis
---

# Buffer workflow
`, nil)
	writeSkillPackageFixture(t, root, "prompt-refinement", `---
name: prompt-refinement
description: Prompt knowledge.
kind: knowledge
---

# Prompt knowledge
`, nil)

	registry := NewRegistry(toolReg)
	if err := registry.LoadDirectories(root); err != nil {
		t.Fatalf("LoadDirectories() error = %v", err)
	}

	definition, ok := registry.Get("buffer_analysis")
	if !ok {
		t.Fatalf("expected buffer_analysis to be loaded")
	}
	if definition.Metadata.Backend != RuntimeIDCLI {
		t.Fatalf("unexpected backend: %s", definition.Metadata.Backend)
	}
	if !strings.HasSuffix(definition.SourcePath, filepath.Join("vector-tools", "buffer_analysis.yaml")) {
		t.Fatalf("unexpected source path: %s", definition.SourcePath)
	}

	pkg, ok := registry.GetPackage("vector-tools")
	if !ok {
		t.Fatalf("expected vector-tools package")
	}
	if pkg.Kind != SkillPackageKindCategory {
		t.Fatalf("unexpected package kind: %s", pkg.Kind)
	}
	if len(pkg.ToolRefs) != 1 || pkg.ToolRefs[0] != "buffer_analysis" {
		t.Fatalf("unexpected package tool refs: %#v", pkg.ToolRefs)
	}
	if len(pkg.Tools) != 0 {
		t.Fatalf("expected public package view to hide hydrated tools, got %#v", pkg.Tools)
	}
	if !pkg.Enabled {
		t.Fatal("expected category package enabled by default")
	}

	workflow, ok := registry.GetPackage("buffer-workflow")
	if !ok {
		t.Fatalf("expected workflow package")
	}
	if workflow.Kind != SkillPackageKindWorkflow {
		t.Fatalf("unexpected workflow kind: %s", workflow.Kind)
	}
	if len(workflow.RequiresTools) != 1 || workflow.RequiresTools[0] != "buffer_analysis" {
		t.Fatalf("unexpected requires tools: %#v", workflow.RequiresTools)
	}

	if len(registry.ToolDefinitions()) != 1 {
		t.Fatalf("unexpected tool definition count: %d", len(registry.ToolDefinitions()))
	}
	if len(registry.Packages()) != 3 {
		t.Fatalf("unexpected package count: %d", len(registry.Packages()))
	}
	if len(registry.CategoryPackages()) != 1 {
		t.Fatalf("unexpected category package count: %d", len(registry.CategoryPackages()))
	}
	if len(registry.WorkflowPackages()) != 1 {
		t.Fatalf("unexpected workflow package count: %d", len(registry.WorkflowPackages()))
	}

	definitions := registry.ToolsForPackages([]string{"vector-tools", "buffer-workflow", "missing"})
	if len(definitions) != 1 || definitions[0].Metadata.Name != "buffer_analysis" {
		t.Fatalf("unexpected tools for packages: %#v", definitions)
	}

	packages := registry.Packages()
	if len(packages) != 3 {
		t.Fatalf("unexpected package count: %d", len(packages))
	}
	for _, candidate := range packages {
		if candidate.Name == "vector-tools" && len(candidate.Tools) != 0 {
			t.Fatalf("expected Packages() public view to omit hydrated tools, got %#v", candidate.Tools)
		}
	}
}

func TestSkillPackageJSONOmitsEmbeddedToolsField(t *testing.T) {
	t.Parallel()

	pkg := SkillPackage{
		Name:        "vector-tools",
		Kind:        SkillPackageKindCategory,
		ToolRefs:    []string{"buffer_analysis"},
		Tools:       []SkillDefinition{{Metadata: SkillMetadata{Name: "buffer_analysis"}}},
		Enabled:     true,
		FrontMatter: SkillFrontMatter{Name: "vector-tools"},
	}

	raw, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(raw), `"tools"`) {
		t.Fatalf("expected public skill package json to omit embedded tools field, got %s", raw)
	}
}

func TestDefaultVectorFieldToolsUseQGISRuntime(t *testing.T) {
	t.Parallel()

	toolRoot := repoSamplePath(t, "autocarto/tools/system")
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("toolReg.LoadDirectories() error = %v", err)
	}
	pluginRoot := repoSamplePath(t, "autocarto/skills/system")
	registry := NewRegistry(toolReg)
	if err := registry.LoadDirectories(pluginRoot); err != nil {
		t.Fatalf("LoadDirectories() error = %v", err)
	}

	for _, name := range []string{
		"add_field_to_attributes_table",
		"add_autoincremental_field",
		"order_by_expression",
		"join_attributes_by_location",
	} {
		definition, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected %s to be loaded", name)
		}
		if definition.Metadata.Backend != RuntimeIDQGIS {
			t.Fatalf("expected %s backend %s, got %s", name, RuntimeIDQGIS, definition.Metadata.Backend)
		}
	}
}

func TestRegistryLoadDirectoriesInvalidKeepsPreviousState(t *testing.T) {
	t.Parallel()

	validRoot := t.TempDir()
	invalidRoot := t.TempDir()
	toolRoot := t.TempDir()
	writeToolFixture(t, toolRoot, "system/vector-tools/buffer_analysis.yaml", toolDefinitionFixtureV1("buffer_analysis", "vector-tools", RuntimeIDQGIS, "buffer", ""))
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("toolReg.LoadDirectories() error = %v", err)
	}
	writeSkillPackageFixture(t, validRoot, "vector-tools", `---
name: vector-tools
description: Vector tools.
kind: category
tools:
  - buffer_analysis
---

# Vector tools
`, nil)
	writeSkillPackageFixture(t, invalidRoot, "broken-tools", `---
name: broken-tools
description: Broken tools.
kind: unsupported
---

# Broken tools
`, nil)

	registry := NewRegistry(toolReg)
	if err := registry.LoadDirectories(validRoot); err != nil {
		t.Fatalf("initial LoadDirectories() error = %v", err)
	}

	err := registry.LoadDirectories(validRoot, invalidRoot)
	if err == nil {
		t.Fatalf("expected LoadDirectories() to fail for invalid definition")
	}
	message := err.Error()
	for _, want := range []string{"invalid skill package broken-tools kind \"unsupported\""} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected error %q to contain %q", message, want)
		}
	}

	definitions := registry.Definitions()
	if len(definitions) != 1 {
		t.Fatalf("expected previous registry state to stay intact, got %d definitions", len(definitions))
	}
	if definitions[0].Metadata.Name != "buffer_analysis" {
		t.Fatalf("unexpected retained definition: %s", definitions[0].Metadata.Name)
	}
}

func TestRegistryLoadDirectoriesMergeSamePackageAcrossRoots(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	overrideDir := t.TempDir()
	toolRoot := t.TempDir()
	writeToolFixture(t, toolRoot, "system/vector-tools/buffer_analysis.yaml", toolDefinitionFixtureV1("buffer_analysis", "vector-tools", RuntimeIDQGIS, "qgis_buffer", ""))
	writeToolFixture(t, toolRoot, "system/vector-tools/clip_analysis.yaml", toolDefinitionFixtureV1("clip_analysis", "vector-tools", RuntimeIDQGIS, "clip", ""))
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("toolReg.LoadDirectories() error = %v", err)
	}
	writeSkillPackageFixture(t, baseDir, "vector-tools", `---
name: vector-tools
description: Base vector tools.
kind: category
tools:
  - buffer_analysis
  - clip_analysis
---

# Base vector tools
`, nil)
	writeSkillPackageFixture(t, overrideDir, "vector-tools", `---
name: vector-tools
description: Override vector tools.
kind: category
tools:
  - buffer_analysis
---

# Override vector tools
`, nil)

	registry := NewRegistry(toolReg)
	if err := registry.LoadDirectories(baseDir, overrideDir); err != nil {
		t.Fatalf("LoadDirectories() error = %v", err)
	}

	definitions := registry.Definitions()
	if len(definitions) != 1 {
		t.Fatalf("unexpected definition count: %d", len(definitions))
	}

	buffer, ok := registry.Get("buffer_analysis")
	if !ok {
		t.Fatalf("expected buffer_analysis to exist")
	}
	if buffer.Metadata.Backend != RuntimeIDQGIS {
		t.Fatalf("expected override backend, got %s", buffer.Metadata.Backend)
	}
	if buffer.Execution.BackendTool != "buffer_analysis" {
		t.Fatalf("expected override backend tool, got %s", buffer.Execution.BackendTool)
	}
	manifestTool, _ := toolReg.Get("buffer_analysis")
	if manifestTool.ExecutionManifestEntry().BackendTool != "qgis_buffer" {
		t.Fatal("authority manifest lost the native binding")
	}
	if !strings.HasPrefix(buffer.SourcePath, toolRoot) {
		t.Fatalf("expected override source path, got %s", buffer.SourcePath)
	}

	pkg, ok := registry.GetPackage("vector-tools")
	if !ok {
		t.Fatalf("expected merged package")
	}
	if pkg.FrontMatter.Description != "Override vector tools." {
		t.Fatalf("expected override frontmatter description, got %s", pkg.FrontMatter.Description)
	}
	if len(pkg.ToolRefs) != 1 || pkg.ToolRefs[0] != "buffer_analysis" {
		t.Fatalf("expected override tool refs to win, got %#v", pkg.ToolRefs)
	}
	if len(pkg.Tools) != 0 {
		t.Fatalf("expected public merged package view to hide hydrated tools, got %#v", pkg.Tools)
	}
}

func TestRegistryLoadDirectoriesAllowsSharedToolRefsAcrossPackages(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	toolRoot := t.TempDir()
	writeToolFixture(t, toolRoot, "system/vector-tools/buffer_analysis.yaml", toolDefinitionFixtureV1("buffer_analysis", "vector-tools", RuntimeIDQGIS, "buffer", ""))
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("toolReg.LoadDirectories() error = %v", err)
	}
	writeSkillPackageFixture(t, root, "vector-tools", `---
name: vector-tools
description: Vector tools.
kind: category
tools:
  - buffer_analysis
---

# Vector tools
`, nil)
	writeSkillPackageFixture(t, root, "other-tools", `---
name: other-tools
description: Other tools.
kind: category
tools:
  - buffer_analysis
---

# Other tools
`, nil)

	registry := NewRegistry(toolReg)
	if err := registry.LoadDirectories(root); err != nil {
		t.Fatalf("LoadDirectories() error = %v", err)
	}
	definitions := registry.AllDefinitions()
	if len(definitions) != 1 || definitions[0].Metadata.Name != "buffer_analysis" {
		t.Fatalf("expected shared tool refs to deduplicate definitions, got %#v", definitions)
	}
}

func TestRegistryLoadBuiltinSkillPackages(t *testing.T) {
	pluginDir := repoPluginSkillPackagesPath(t)
	toolRoot := repoSamplePath(t, "autocarto/tools/system")
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("toolReg.LoadDirectories() error = %v", err)
	}
	registry := NewRegistry(toolReg)
	if err := registry.LoadDirectories(pluginDir); err != nil {
		t.Fatalf("LoadDirectories() error = %v", err)
	}

	definitions := registry.ToolDefinitions()
	if len(definitions) < 200 {
		t.Fatalf("expected builtin tool definitions to be hydrated, got %d", len(definitions))
	}
	if len(registry.CategoryPackages()) < 28 {
		t.Fatalf("expected at least 28 builtin category packages, got %d", len(registry.CategoryPackages()))
	}
	if len(registry.WorkflowPackages()) != 0 {
		t.Fatal("retired method packages remain in the builtin catalog")
	}
	for _, pkg := range registry.Packages() {
		if pkg.Kind != SkillPackageKindCategory {
			t.Errorf("builtin package %s has retired kind %s", pkg.Name, pkg.Kind)
		}
	}
	buffer, ok := registry.Get("buffer")
	if !ok {
		t.Fatal("expected buffer definition to exist")
	}
	qualityPackage, ok := registry.GetPackage("quality-feedback")
	if !ok {
		t.Fatal("expected quality-feedback package to exist")
	}
	if qualityPackage.Kind != SkillPackageKindCategory {
		t.Fatalf("expected quality-feedback to be a category, got %s", qualityPackage.Kind)
	}
	for _, toolName := range []string{"prepare_quality_review", "load_review_images"} {
		definition, exists := registry.Get(toolName)
		if !exists {
			t.Fatalf("expected %s definition to exist", toolName)
		}
		if definition.Metadata.Backend != RuntimeIDCLI {
			t.Fatalf("expected %s backend %s, got %s", toolName, RuntimeIDCLI, definition.Metadata.Backend)
		}
	}
	for _, toolName := range []string{
		"layout_get_items_properties",
		"layout_resize_item",
		"layout_set_item_background",
		"layout_set_item_frame",
		"layout_set_north_arrow_style",
	} {
		definition, exists := registry.Get(toolName)
		if !exists {
			t.Fatalf("expected %s definition to exist", toolName)
		}
		if definition.Metadata.Backend != RuntimeIDQGIS {
			t.Fatalf("expected %s backend %s, got %s", toolName, RuntimeIDQGIS, definition.Metadata.Backend)
		}
	}
	if buffer.Metadata.Backend != RuntimeIDQGIS {
		t.Fatalf("unexpected buffer backend: %s", buffer.Metadata.Backend)
	}

	openProject, ok := registry.Get("open_project")
	if !ok {
		t.Fatal("expected open_project definition to exist")
	}
	if openProject.Metadata.Backend != RuntimeIDQGIS {
		t.Fatalf("unexpected open_project backend: %s", openProject.Metadata.Backend)
	}

	exportPrintLayout, ok := registry.Get("export_print_layout")
	if !ok {
		t.Fatal("expected export_print_layout definition to exist")
	}
	if exportPrintLayout.Metadata.Backend != RuntimeIDQGIS {
		t.Fatalf("unexpected export_print_layout backend: %s", exportPrintLayout.Metadata.Backend)
	}
	if !strings.Contains(exportPrintLayout.SourcePath, filepath.Join("autocarto", "tools", "system", "print-layout")) {
		t.Fatalf("unexpected export_print_layout source path: %s", exportPrintLayout.SourcePath)
	}

	listSkillPackages, ok := registry.Get("list_skill_packages")
	if !ok {
		t.Fatal("expected list_skill_packages definition to exist")
	}
	if listSkillPackages.Metadata.Backend != RuntimeIDInternal {
		t.Fatalf("unexpected list_skill_packages backend: %s", listSkillPackages.Metadata.Backend)
	}
	if !strings.Contains(listSkillPackages.SourcePath, filepath.Join("autocarto", "tools", "system", "creator-tools")) {
		t.Fatalf("unexpected list_skill_packages source path: %s", listSkillPackages.SourcePath)
	}

	createDirectory, ok := registry.Get("create_directory")
	if !ok {
		t.Fatal("expected create_directory definition to exist")
	}
	if createDirectory.Metadata.Backend != RuntimeIDInternal {
		t.Fatalf("unexpected create_directory backend: %s", createDirectory.Metadata.Backend)
	}

	for _, name := range []string{"runtime-control", "charts", "route-cartography", "admin-boundaries", "flow-control"} {
		pkg, ok := registry.GetPackage(name)
		if !ok || pkg.Kind != SkillPackageKindCategory || len(pkg.ToolRefs) == 0 {
			t.Errorf("missing category with actual tool references: %s", name)
		}
	}
	for _, name := range []string{"prompt-refinement", "chart-visualization", "misc-tools"} {
		if _, ok := registry.GetPackage(name); ok {
			t.Errorf("retired builtin package remains: %s", name)
		}
	}

	projectStateTools := registry.ToolsForPackages([]string{"project-state"})
	if len(projectStateTools) != 7 {
		t.Fatalf("unexpected project-state tool count: %d", len(projectStateTools))
	}
}

func TestRegistryLoadsMarchingRouteKnowledgeTools(t *testing.T) {
	pluginDir := repoPluginSkillPackagesPath(t)
	toolRoot := repoSamplePath(t, "autocarto/tools/system")
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("toolReg.LoadDirectories() error = %v", err)
	}
	registry := NewRegistry(toolReg)
	if err := registry.LoadDirectories(pluginDir); err != nil {
		t.Fatalf("LoadDirectories() error = %v", err)
	}

	knowledgeToolsPackage, ok := registry.GetPackage("marching-route-knowledge-tools")
	if !ok {
		t.Fatal("expected marching-route-knowledge-tools package to exist")
	}
	if knowledgeToolsPackage.Kind != SkillPackageKindCategory {
		t.Fatalf("unexpected marching-route-knowledge-tools kind: %s", knowledgeToolsPackage.Kind)
	}
	if _, ok := registry.GetPackage("marching-route-knowledge-extraction"); ok {
		t.Fatal("retired knowledge method remains a package")
	}

	knowledgeTools := registry.ToolsForPackages([]string{"marching-route-knowledge-tools"})
	if len(knowledgeTools) != 2 {
		t.Fatalf("unexpected marching-route knowledge tool count: %d", len(knowledgeTools))
	}
	knowledgeToolNames := make(map[string]bool, len(knowledgeTools))
	var compileTool SkillDefinition
	for _, definition := range knowledgeTools {
		knowledgeToolNames[definition.Metadata.Name] = true
		if definition.Metadata.Name == "marching_route_compile_dataset" {
			compileTool = definition
		}
	}
	for _, name := range []string{"marching_route_get_extraction_contract", "marching_route_compile_dataset"} {
		if !knowledgeToolNames[name] {
			t.Fatalf("expected %s in marching-route-knowledge-tools: %#v", name, knowledgeToolNames)
		}
	}
	if compileTool.Metadata.Name == "" {
		t.Fatal("expected marching_route_compile_dataset definition")
	}
	var knowledgeParam *SkillParameter
	for idx := range compileTool.Spec.Parameters {
		if compileTool.Spec.Parameters[idx].Name == "knowledge_json" {
			knowledgeParam = &compileTool.Spec.Parameters[idx]
			break
		}
	}
	if knowledgeParam == nil {
		t.Fatal("expected knowledge_json parameter")
	}
	if knowledgeParam.Type != "object" {
		t.Fatalf("knowledge_json type = %q, want object", knowledgeParam.Type)
	}
	schemaMap := BuildSkillSchemaMap(compileTool)
	properties, _ := schemaMap["properties"].(map[string]any)
	property, _ := properties["knowledge_json"].(map[string]any)
	if property["type"] != "object" {
		t.Fatalf("knowledge_json schema = %#v, want object", property)
	}
}

func TestRegistryToolsForPackagesUsesToolRefsWhenToolRegistryIsPresent(t *testing.T) {
	t.Parallel()

	toolRoot := t.TempDir()
	writeToolFixture(t, toolRoot, "system/vector-geometry/buffer.yaml", toolDefinitionFixtureV1("buffer", "vector-geometry", "qgis", "native:buffer", ""))
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("toolReg.LoadDirectories() error = %v", err)
	}

	skillRoot := t.TempDir()
	writeSkillPackageFixture(t, skillRoot, "buffer-workflow", `---
name: buffer-workflow
description: Buffer workflow guidance.
kind: workflow
tools:
  - buffer
---

# Buffer workflow
`, nil)

	registry := NewRegistry(toolReg)
	if err := registry.LoadDirectories(skillRoot); err != nil {
		t.Fatalf("LoadDirectories() error = %v", err)
	}

	definitions := registry.ToolsForPackages([]string{"buffer-workflow"})
	if len(definitions) != 1 {
		t.Fatalf("unexpected definitions: %#v", definitions)
	}
	resolved, ok := registry.Get("buffer")
	if !ok {
		t.Fatal("expected registry.Get to resolve tool ref")
	}
	if resolved.Metadata.Name != "buffer" {
		t.Fatalf("unexpected resolved tool name: %s", resolved.Metadata.Name)
	}
	if definitions[0].Metadata.Name != "buffer" {
		t.Fatalf("unexpected tool name: %s", definitions[0].Metadata.Name)
	}
	if definitions[0].Metadata.Category != "vector-geometry" {
		t.Fatalf("unexpected tool category: %s", definitions[0].Metadata.Category)
	}
	if definitions[0].Execution.BackendTool != "buffer" {
		t.Fatalf("unexpected backend tool: %s", definitions[0].Execution.BackendTool)
	}
	toolDefinition, _ := toolReg.Get("buffer")
	if toolDefinition.ExecutionManifestEntry().BackendTool != "native:buffer" {
		t.Fatal("QGIS manifest must retain the actual native algorithm binding")
	}
}

func TestConvertToolDefinitionFallsBackToToolNameForHandlerBoundTools(t *testing.T) {
	t.Parallel()

	definition := convertToolDefinition(toolpkg.ToolDefinition{
		APIVersion: toolpkg.ToolAPIVersionV1,
		Kind:       toolpkg.ToolKindDefinition,
		Metadata: toolpkg.ToolMetadata{
			Name:    "get_project_info",
			Domain:  "project-state",
			Runtime: RuntimeIDQGIS,
		},
		Spec: toolpkg.ToolSpec{
			Description: "获取工程信息",
		},
		Execution: toolpkg.ToolExecution{
			Handler: "handlers.project.get_project_info",
		},
	})

	if definition.Execution.BackendTool != "get_project_info" {
		t.Fatalf("expected handler-bound tool to fallback to tool name, got %q", definition.Execution.BackendTool)
	}
}

func TestRegistryDynamicDefinitionsMergedWithStaticDefinitions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	toolRoot := t.TempDir()
	writeToolFixture(t, toolRoot, "system/vector-tools/buffer_analysis.yaml", toolDefinitionFixtureV1("buffer_analysis", "vector-tools", RuntimeIDQGIS, "buffer", ""))
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("toolReg.LoadDirectories() error = %v", err)
	}
	writeSkillPackageFixture(t, root, "vector-tools", `---
name: vector-tools
description: Vector tools.
kind: category
tools:
  - buffer_analysis
---

# Vector tools
`, nil)

	registry := NewRegistry(toolReg)
	if err := registry.LoadDirectories(root); err != nil {
		t.Fatalf("LoadDirectories() error = %v", err)
	}

	changes := 0
	registry.OnChange(func() {
		changes++
	})

	registry.RegisterDynamic([]SkillDefinition{buildDynamicSkillDefinition("bridge_buffer", []string{"instance-a"})})

	definitions := registry.AllDefinitions()
	if len(definitions) != 2 {
		t.Fatalf("expected static + dynamic definitions, got %d", len(definitions))
	}
	if _, ok := registry.Get("bridge_buffer"); !ok {
		t.Fatal("expected dynamic definition to be readable via Get")
	}
	if changes != 1 {
		t.Fatalf("expected change callback after register, got %d", changes)
	}

	registry.UnregisterDynamic([]string{"bridge_buffer"})
	if _, ok := registry.Get("bridge_buffer"); ok {
		t.Fatal("expected dynamic definition to be removed")
	}
	if len(registry.AllDefinitions()) != 1 {
		t.Fatalf("expected only static definitions to remain, got %d", len(registry.AllDefinitions()))
	}
	if changes != 2 {
		t.Fatalf("expected change callback after unregister, got %d", changes)
	}
}

func TestRegistryDynamicToolRegistryDoesNotExposeVirtualPackage(t *testing.T) {
	t.Parallel()

	dynamicRegistry := NewBridgeDynamicRegistry()
	dynamicRegistry.RegisterInstanceTools("instance-a", []bridgepkg.ToolDefinition{{
		Name:        "flash_feature",
		Description: "Flash feature.",
		Category:    "vector",
		InputSchema: map[string]any{"type": "object"},
	}})

	toolRegistry := toolpkg.NewRegistry()
	dynamicRegistry.ApplyToRegistry(toolRegistry)

	registry := NewRegistry(toolRegistry)
	if err := registry.LoadDirectories(t.TempDir()); err != nil {
		t.Fatalf("LoadDirectories() error = %v", err)
	}

	definition, ok := registry.Get("flash_feature")
	if !ok {
		t.Fatal("expected dynamic tool to remain readable from registry")
	}
	if definition.Metadata.Name != "flash_feature" || !definition.Metadata.Dynamic {
		t.Fatalf("unexpected dynamic tool definition: %#v", definition)
	}

	if _, ok := registry.GetPackage("_bridge-dynamic"); ok {
		t.Fatal("expected _bridge-dynamic package to be removed")
	}
	if packages := registry.Packages(); len(packages) != 0 {
		t.Fatalf("expected no virtual packages, got %#v", packages)
	}
	if categoryPackages := registry.CategoryPackages(); len(categoryPackages) != 0 {
		t.Fatalf("expected no virtual category packages, got %#v", categoryPackages)
	}
	if definitions := registry.ToolsForPackages([]string{"_bridge-dynamic"}); len(definitions) != 0 {
		t.Fatalf("expected no tools resolved for removed virtual package, got %#v", definitions)
	}
}

func buildDynamicSkillDefinition(name string, instanceIDs []string) SkillDefinition {
	return SkillDefinition{
		APIVersion: SkillAPIVersionV2,
		Kind:       SkillKindAtomic,
		Metadata: SkillMetadata{
			Name:        name,
			Runtime:     RuntimeIDQGIS,
			Backend:     RuntimeIDQGIS,
			Source:      "bridge",
			Dynamic:     true,
			InstanceIDs: append([]string(nil), instanceIDs...),
		},
		Spec: SkillSpec{Description: "Dynamic bridge tool."},
		Execution: SkillExecution{
			BackendTool:  name,
			ParamMapping: map[string]any{},
		},
	}
}

func writeSkillPackageFixture(t *testing.T, root string, packageName string, markdown string, files map[string]string) {
	t.Helper()
	packageDir := filepath.Join(root, packageName)
	writeSkillFixture(t, packageDir, "SKILL.md", markdown)
	for relativePath, content := range files {
		writeSkillFixture(t, packageDir, relativePath, content)
	}
}

func writeSkillFixture(t *testing.T, root string, relativePath string, content string) {
	t.Helper()
	fullPath := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", fullPath, err)
	}
}

func toolDefinitionFixture(name string, backend string, description string, backendTool string) string {
	return strings.TrimSpace(`apiVersion: skill/v1
kind: AtomicSkill
metadata:
  name: `+name+`
  backend: `+backend+`
spec:
  description: `+description+`
execution:
  backend_tool: `+backendTool+`
  param_mapping:
    INPUT: '{{ layer }}'`) + "\n"
}

func repoPluginSkillPackagesPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve caller path")
	}
	base := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	return filepath.Join(base, "autocarto", "skills", "system")
}

func writeToolFixture(t *testing.T, root string, relativePath string, content string) {
	t.Helper()
	fullPath := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", fullPath, err)
	}
}

func toolDefinitionFixtureV1(name string, domain string, runtime string, backendTool string, handler string) string {
	lines := []string{
		"apiVersion: tool/v1",
		"kind: Tool",
		"metadata:",
		"  name: " + name,
		"  domain: " + domain,
		"  runtime: " + runtime,
		"spec:",
		"  description: Test tool.",
		"  parameters:",
		"    - name: INPUT",
		"      type: layer",
		"execution:",
	}
	if backendTool != "" {
		lines = append(lines, "  backend_tool: "+backendTool)
	}
	if handler != "" {
		lines = append(lines, "  handler: "+handler)
	}
	lines = append(lines,
		"  param_mapping:",
		"    INPUT: '{{ INPUT }}'",
	)
	return strings.Join(lines, "\n")
}
