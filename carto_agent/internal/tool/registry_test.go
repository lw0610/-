package tool

import (
	"path/filepath"
	"testing"
)

func TestRegistryLoadDirectoriesRejectsDuplicateToolNames(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeToolFixture(t, root, "system/vector-geometry/buffer.yaml", toolDefinitionFixture("buffer", "vector-geometry", "qgis", "native:buffer", ""))
	writeToolFixture(t, root, "user/custom/buffer.yaml", toolDefinitionFixture("buffer", "custom", "qgis", "native:custombuffer", ""))

	registry := NewRegistry()
	err := registry.LoadDirectories(filepath.Join(root, "system"), filepath.Join(root, "user"))
	if err == nil {
		t.Fatal("expected duplicate tool name error")
	}
	if got := err.Error(); got == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestRegistryRegisterDynamicOverridesStaticLookup(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeToolFixture(t, root, "system/vector-geometry/buffer.yaml", toolDefinitionFixture("buffer", "vector-geometry", "qgis", "native:buffer", ""))

	registry := NewRegistry()
	if err := registry.LoadDirectories(root); err != nil {
		t.Fatalf("LoadDirectories() error = %v", err)
	}

	registry.RegisterDynamic([]ToolDefinition{{
		APIVersion: ToolAPIVersionV1,
		Kind:       ToolKindDefinition,
		Metadata: ToolMetadata{
			Name:    "buffer",
			Domain:  "bridge",
			Runtime: RuntimeIDQGIS,
			Source:  "bridge",
			Dynamic: true,
		},
		Spec: ToolSpec{Description: "Bridge buffer."},
		Execution: ToolExecution{
			BackendTool:  "buffer",
			ParamMapping: map[string]any{"INPUT": "{{ INPUT }}"},
		},
	}})

	definition, ok := registry.Get("buffer")
	if !ok {
		t.Fatal("expected dynamic definition")
	}
	if definition.Metadata.Source != "bridge" {
		t.Fatalf("expected dynamic definition to override static one, got source %s", definition.Metadata.Source)
	}

	registry.UnregisterDynamic([]string{"buffer"})
	definition, ok = registry.Get("buffer")
	if !ok {
		t.Fatal("expected static definition after unregister")
	}
	if definition.Metadata.Source != "" {
		t.Fatalf("expected static definition after unregister, got source %s", definition.Metadata.Source)
	}
}
