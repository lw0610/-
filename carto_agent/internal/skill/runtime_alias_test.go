package skill

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/compute"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/autocarto/carto_agent/internal/toolsession"
)

func TestNormalizeRuntimeLegacyAliases(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		" bridge ":  RuntimeIDQGIS,
		"qgis":      RuntimeIDQGIS,
		" compute ": RuntimeIDCLI,
		"cli":       RuntimeIDCLI,
		"internal":  RuntimeIDInternal,
		"CuStOm":    "custom",
		"":          "",
	}

	for raw, want := range tests {
		if got := NormalizeRuntime(raw); got != want {
			t.Fatalf("NormalizeRuntime(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestRegistryLoadDirectoriesSupportsRuntimeAndV2(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	toolRoot := t.TempDir()
	writeToolFixture(t, toolRoot, "system/vector-tools/buffer_analysis.yaml", `apiVersion: tool/v1
kind: Tool
metadata:
  name: buffer_analysis
  domain: vector-tools
  runtime: cli
spec:
  description: Create a buffer around a vector layer.
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: '{{ layer }}'`)
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("toolReg.LoadDirectories() error = %v", err)
	}
	writeSkillPackageFixture(t, root, "vector-tools", `---
name: vector-tools
description: Vector tools.
kind: category
runtime: cli
tools:
  - buffer_analysis
---

# Vector tools
`, nil)

	registry := NewRegistry(toolReg)
	if err := registry.LoadDirectories(root); err != nil {
		t.Fatalf("LoadDirectories() error = %v", err)
	}

	definition, ok := registry.Get("buffer_analysis")
	if !ok {
		t.Fatalf("expected buffer_analysis to be loaded")
	}
	if definition.APIVersion != SkillAPIVersionV2 {
		t.Fatalf("unexpected apiVersion: %s", definition.APIVersion)
	}
	if definition.Metadata.Runtime != RuntimeIDCLI {
		t.Fatalf("unexpected runtime: %s", definition.Metadata.Runtime)
	}
	if definition.Metadata.Backend != RuntimeIDCLI {
		t.Fatalf("unexpected normalized backend: %s", definition.Metadata.Backend)
	}

	pkg, ok := registry.GetPackage("vector-tools")
	if !ok {
		t.Fatalf("expected vector-tools package")
	}
	if pkg.FrontMatter.Runtime != RuntimeIDCLI {
		t.Fatalf("unexpected package runtime: %s", pkg.FrontMatter.Runtime)
	}
	if pkg.FrontMatter.Backend != RuntimeIDCLI {
		t.Fatalf("unexpected package backend: %s", pkg.FrontMatter.Backend)
	}
}

func TestEngineExecuteUsesCLIBackendForQGISRuntimeTools(t *testing.T) {
	t.Parallel()

	registry := loadRegistryForTest(t, map[string]string{
		"cli/data_load.yaml": `
apiVersion: tool/v1
kind: Tool
metadata:
  name: data_load
  domain: cli
  runtime: qgis
spec:
  description: Load a layer in QGIS.
  parameters:
    - name: layer
      type: string
      required: true
execution:
  backend_tool: data_load_vector
  param_mapping:
    path: "{{ resolve_layer(layer) }}"
    mode: preview
`,
	})

	computeInvoker := &fakeComputeInvoker{resp: &compute.InvokeResponse{Success: true, Output: map[string]any{"loaded": true}}}
	engine := NewEngine(registry, NewComputeBackend(computeInvoker))
	ctx := toolsession.WithContext(context.Background(), "session-1", filepath.Join(t.TempDir(), "demo.qgz"), "instance-9")
	ctx = WithSessionContext(ctx, SessionContext{
		SessionID:   "session-1",
		ProjectPath: filepath.Join(t.TempDir(), "demo.qgz"),
		Layers:      []SessionLayer{{Name: "roads", Path: "/layers/roads.gpkg"}},
	})

	result, err := engine.Execute(ctx, "data_load", map[string]any{"layer": "roads"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.BackendID != RuntimeIDCLI {
		t.Fatalf("unexpected backend id: %s", result.BackendID)
	}
	if computeInvoker.callCount != 1 {
		t.Fatalf("expected cli runtime backend to be called once, got %d", computeInvoker.callCount)
	}
}
