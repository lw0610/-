package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/autocarto/carto_agent/internal/compute"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

func processingExportRegistry(t *testing.T) (*Registry, *toolpkg.Registry, string) {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tools := toolpkg.NewRegistry()
	if err := tools.LoadDirectories(filepath.Join(root, "autocarto/tools")); err != nil {
		t.Fatal(err)
	}
	skillRoot := t.TempDir()
	writeSkillPackageFixture(t, skillRoot, "export-test", `---
name: export-test
description: Real processing export bindings.
kind: workflow
tools:
  - package_layers
  - save_vector_features_to_file
  - join_attributes_by_nearest
---
`, nil)
	registry := NewRegistry(tools)
	if err := registry.LoadDirectories(skillRoot); err != nil {
		t.Fatal(err)
	}
	return registry, tools, root
}

func TestProcessingPublicDispatchAndNativeManifestRemainSeparate(t *testing.T) {
	registry, tools, _ := processingExportRegistry(t)
	for name, algorithm := range map[string]string{
		"package_layers":               "native:package",
		"save_vector_features_to_file": "native:savefeatures",
		"join_attributes_by_nearest":   "native:joinbynearest",
	} {
		definition, _ := registry.Get(name)
		native, _ := tools.Get(name)
		if definition.Execution.BackendTool != name {
			t.Fatalf("dispatch %s: %s", name, definition.Execution.BackendTool)
		}
		if native.ExecutionManifestEntry().BackendTool != algorithm {
			t.Fatalf("native %s: %#v", name, native.ExecutionManifestEntry())
		}
	}
	definition, _ := registry.Get("package_layers")
	layers := BuildSkillSchemaMap(definition)["properties"].(map[string]any)["LAYERS"].(map[string]any)
	if layers["type"] != "array" || !reflect.DeepEqual(layers["items"], map[string]any{"type": "string"}) {
		t.Fatalf("model must receive an array of layer references, got %#v", layers)
	}
	var called string
	var actual map[string]any
	backend := NewQGISBackend(func(_ context.Context, name string, args map[string]any) (*compute.InvokeResponse, error) {
		called, actual = name, args
		return &compute.InvokeResponse{Success: true}, nil
	}, nil, nil)
	_, err := NewEngine(registry, backend).Execute(context.Background(), "package_layers", map[string]any{
		"LAYERS": []any{"cities", "stations"}, "OUTPUT": "/tmp/test-package.gpkg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if called != "package_layers" || !reflect.DeepEqual(actual["LAYERS"], []any{"cities", "stations"}) {
		t.Fatalf("public name and array changed before remote dispatch: %s %#v", called, actual)
	}
	// CLI tools keep their own execution target. Legacy AtomicSkill definitions
	// do not go through this authority tool/v1 conversion.
	cli := toolpkg.ToolDefinition{Metadata: toolpkg.ToolMetadata{Name: "public-cli", Runtime: RuntimeIDCLI}, Execution: toolpkg.ToolExecution{BackendTool: "private-cli"}}
	if got := convertToolDefinition(cli).Execution.BackendTool; got != "private-cli" {
		t.Fatalf("CLI target changed: %s", got)
	}
}

func TestProcessingExportsThroughRealQGISDispatch(t *testing.T) {
	python := os.Getenv("AUTOCARTO_QGIS_PYTHON")
	if python == "" {
		t.Skip("set AUTOCARTO_QGIS_PYTHON for the real QGIS boundary test")
	}
	registry, tools, root := processingExportRegistry(t)
	var definitions []toolpkg.ToolDefinition
	for _, name := range []string{"package_layers", "save_vector_features_to_file", "join_attributes_by_nearest"} {
		definition, ok := tools.Get(name)
		if !ok {
			t.Fatal(name)
		}
		definitions = append(definitions, definition)
	}
	manifest := toolpkg.BuildExecutionManifestEntries(definitions)
	directory := t.TempDir()
	backend := NewQGISBackend(func(ctx context.Context, name string, args map[string]any) (*compute.InvokeResponse, error) {
		body, err := json.Marshal(map[string]any{"name": name, "params": args, "manifest": manifest, "directory": directory})
		if err != nil {
			return nil, err
		}
		command := exec.CommandContext(ctx, python, filepath.Join(root, "autocarto/tests/fixtures/processing_dispatch_qgis.py"))
		command.Dir = root
		command.Stdin = bytes.NewReader(body)
		var diagnostics bytes.Buffer
		command.Stderr = &diagnostics
		output, err := command.Output()
		if err != nil {
			return nil, fmt.Errorf("real QGIS %s: %w\n%s\n%s", name, err, diagnostics.String(), output)
		}
		var result compute.InvokeResponse
		if err := json.Unmarshal(output, &result); err != nil {
			return nil, fmt.Errorf("real response: %w\n%s", err, output)
		}
		return &result, nil
	}, nil, nil)
	engine := NewEngine(registry, backend)
	cases := []struct {
		name string
		args map[string]any
	}{
		{"save_vector_features_to_file", map[string]any{"INPUT": "cities", "OUTPUT": filepath.Join(directory, "single.gpkg")}},
		{"package_layers", map[string]any{"LAYERS": []any{"cities", "stations"}, "OUTPUT": filepath.Join(directory, "package.gpkg")}},
		{"package_layers", map[string]any{"LAYERS": []any{"cities", "stations"}}},
		{"join_attributes_by_nearest", map[string]any{"INPUT": "cities", "INPUT_2": "stations", "FIELDS_TO_COPY": "name", "MAX_DISTANCE": 10, "OUTPUT": "memory:joined"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result, err := engine.Execute(context.Background(), test.name, test.args)
			if err != nil {
				t.Fatal(err)
			}
			if result.ToolName != test.name || len(result.OutputLayers) == 0 {
				t.Fatalf("not published through Go: %#v", result)
			}
			output, ok := result.Output.(map[string]any)
			if !ok || output["OUTPUT"] == nil {
				t.Fatalf("structured output missing: %#v", result.Output)
			}
			if test.name == "join_attributes_by_nearest" && output["JOINED_COUNT"] != float64(2) {
				t.Fatalf("native counters lost: %#v", output)
			}
			t.Logf("%s: actual QGIS result returned and independently reopened/inspected", test.name)
		})
	}
}
