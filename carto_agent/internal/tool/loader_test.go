package tool

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEmptyAuthorityDoesNotLoadEmbeddedFallback(t *testing.T) {
	definitions, err := LoadAuthorityDefinitions(t.TempDir())
	if err != nil || len(definitions) != 0 {
		t.Fatalf("empty authority unexpectedly loaded stale tools: %v %+v", err, definitions)
	}
}

func TestLoadDefinitionRetainsChineseDiscoveryMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	raw := strings.Replace(toolDefinitionFixture("sample", "vector", "qgis", "native:buffer", ""),
		"  name: sample", "  name: sample\n  display_name: 创建要素缓冲区\n  keywords: [' 邻近分析 ', 缓冲]", 1)
	writeToolFixture(t, root, "sample.yaml", raw)
	definition, err := loadDefinitionFile(filepath.Join(root, "sample.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if definition.Metadata.DisplayName != "创建要素缓冲区" || !reflect.DeepEqual(definition.Metadata.Keywords, []string{"邻近分析", "缓冲"}) {
		t.Fatalf("discovery metadata lost or not normalized: %#v", definition.Metadata)
	}
	cloned := definition.Clone()
	cloned.Metadata.Keywords[0] = "changed"
	if definition.Metadata.Keywords[0] != "邻近分析" {
		t.Fatal("cloning a tool must isolate mutable keyword metadata")
	}
}

func TestLoadDefinitionsFromDirectoryRecursive(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeToolFixture(t, root, "system/vector-geometry/buffer.yaml", toolDefinitionFixture("buffer", "vector-geometry", "qgis", "native:buffer", ""))
	writeToolFixture(t, root, "system/cartography/cartography_set_style.yaml", toolDefinitionFixture("cartography_set_style", "cartography", "qgis", "", "handlers.style.set_layer_style_handler"))

	definitions, err := loadDefinitionsFromDirectory(root)
	if err != nil {
		t.Fatalf("loadDefinitionsFromDirectory() error = %v", err)
	}
	if len(definitions) != 2 {
		t.Fatalf("unexpected definition count: %d", len(definitions))
	}
	definitionsByName := make(map[string]ToolDefinition, len(definitions))
	for _, definition := range definitions {
		definitionsByName[definition.Metadata.Name] = definition
	}
	buffer, ok := definitionsByName["buffer"]
	if !ok {
		t.Fatal("expected buffer definition")
	}
	if !filepath.IsAbs(buffer.SourcePath) {
		t.Fatalf("expected absolute source path, got %s", buffer.SourcePath)
	}
	styleTool, ok := definitionsByName["cartography_set_style"]
	if !ok {
		t.Fatal("expected cartography_set_style definition")
	}
	if styleTool.Execution.Handler != "handlers.style.set_layer_style_handler" {
		t.Fatalf("unexpected handler path: %s", styleTool.Execution.Handler)
	}
}

func TestLoadDefinitionsFromDirectorySupportsToolPackageAndIgnoresNonManifestYAML(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeToolFixture(t, root, "system/project-state/get_project_info/tool.yaml", strings.TrimSpace(`
apiVersion: tool-package/v1
kind: ToolPackage
metadata:
  name: get_project_info
  display_name: 获取工程信息
  domain: project-state
  version: 1.0.0
spec:
  capability_summary: 获取当前工程信息
  parameters:
    - name: include_layers
      type: boolean
policy:
  visibility: public
  authorization_scopes:
    - project.read
  runtime: qgis
execution:
  provider: qgis
  binding: handler
  handler: handlers.project.get_project_info
status_model:
  readiness_from: qgis_provider
`))
	writeToolFixture(t, root, "system/project-state/get_project_info/examples.yaml", strings.TrimSpace(`
examples:
  - args:
      include_layers: true
`))

	definitions, err := loadDefinitionsFromDirectory(root)
	if err != nil {
		t.Fatalf("loadDefinitionsFromDirectory() error = %v", err)
	}
	if len(definitions) != 1 {
		t.Fatalf("expected 1 tool definition, got %d", len(definitions))
	}
	definition := definitions[0]
	if got := definition.APIVersion; got != ToolPackageAPIVersionV1 {
		t.Fatalf("expected tool-package apiVersion, got %q", got)
	}
	if got := definition.Metadata.Name; got != "get_project_info" {
		t.Fatalf("unexpected tool name: %q", got)
	}
	if got := definition.EffectiveDescription(); got != "获取当前工程信息" {
		t.Fatalf("unexpected effective description: %q", got)
	}
	if got := definition.Policy.Visibility; got != VisibilityPublic {
		t.Fatalf("unexpected policy visibility: %q", got)
	}
}

func TestLoadDefinitionFileRejectsInvalidToolDefinition(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "broken.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(`
apiVersion: tool/v1
kind: Tool
metadata:
  name: broken
spec:
  parameters:
    - name: layer
execution:
  param_mapping: {}
`)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := loadDefinitionFile(path)
	if err == nil {
		t.Fatal("expected invalid tool definition error")
	}
	message := err.Error()
	for _, want := range []string{
		"metadata.runtime is required",
		"spec.description is required",
		"execution.backend_tool, execution.handler, or execution.handler_file is required",
		"spec.parameters[0].type is required",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected error %q to contain %q", message, want)
		}
	}
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

func toolDefinitionFixture(name string, domain string, runtime string, backendTool string, handler string) string {
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
