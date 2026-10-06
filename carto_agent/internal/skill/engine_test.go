package skill

import (
	"context"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/compute"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/autocarto/carto_agent/internal/toolsession"
)

type fakeComputeInvoker struct {
	callCount int
	lastReq   compute.InvokeRequest
	resp      *compute.InvokeResponse
	err       error
}

func (f *fakeComputeInvoker) Invoke(ctx context.Context, req compute.InvokeRequest) (*compute.InvokeResponse, error) {
	_ = ctx
	f.callCount++
	f.lastReq = compute.InvokeRequest{
		ProjectID: strings.TrimSpace(req.ProjectID),
		Tool:      strings.TrimSpace(req.Tool),
		Params:    cloneMapAny(req.Params),
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.resp == nil {
		return &compute.InvokeResponse{Success: true, Output: map[string]any{"ok": true}}, nil
	}
	cloned := *f.resp
	cloned.Output = cloneAny(f.resp.Output)
	if len(f.resp.OutputLayers) > 0 {
		cloned.OutputLayers = append([]compute.OutputLayer(nil), f.resp.OutputLayers...)
	}
	return &cloned, nil
}

func TestEngineExecuteComputeSuccess(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	registry := loadRegistryForTest(t, map[string]string{
		"vector/buffer_analysis.yaml": `
apiVersion: tool/v1
kind: Tool
metadata:
  name: buffer_analysis
  domain: vector
  runtime: qgis
spec:
  description: Create a buffer.
  parameters:
    - name: layer
      type: string
      required: true
    - name: distance
      type: number
      required: true
    - name: dissolve
      type: boolean
      default: false
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: "{{ resolve_layer(layer) }}"
    DISTANCE: "{{ distance }}"
    DISSOLVE: "{{ dissolve }}"
    SEGMENTS: 5
    OUTPUT: "{{ auto_output_path('buffer') }}"
  validation:
    - rule: "distance > 0"
      message: "distance must be positive"
`,
	})

	computeInvoker := &fakeComputeInvoker{
		resp: &compute.InvokeResponse{
			Success:      true,
			Output:       map[string]any{"result": "ok"},
			OutputLayers: []compute.OutputLayer{{Path: filepath.Join(projectDir, "buffer.gpkg"), AutoLoad: true}},
		},
	}
	engine := NewEngine(registry, NewComputeBackend(computeInvoker))
	ctx := toolsession.WithContext(context.Background(), "session-42", filepath.Join(projectDir, "demo.qgz"), "")
	ctx = WithSessionContext(ctx, SessionContext{
		ProjectID: "project-123",
		Layers:    []SessionLayer{{Name: "roads", Path: filepath.Join(projectDir, "roads.gpkg")}},
	})

	result, err := engine.Execute(ctx, "buffer_analysis", map[string]any{
		"layer":    "roads",
		"distance": 500,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if computeInvoker.callCount != 1 {
		t.Fatalf("expected compute backend to be called once, got %d", computeInvoker.callCount)
	}
	if computeInvoker.lastReq.ProjectID != "project-123" {
		t.Fatalf("unexpected project id: %s", computeInvoker.lastReq.ProjectID)
	}
	if computeInvoker.lastReq.Tool != "buffer" {
		t.Fatalf("unexpected backend tool: %s", computeInvoker.lastReq.Tool)
	}
	wantParams := map[string]any{
		"INPUT":    filepath.Join(projectDir, "roads.gpkg"),
		"DISTANCE": 500,
		"DISSOLVE": false,
		"SEGMENTS": 5,
		"OUTPUT":   filepath.Join(projectDir, "autoc", "demo", "sessions", "session-42", "output", "session-42_buffer.gpkg"),
	}
	if !reflect.DeepEqual(computeInvoker.lastReq.Params, wantParams) {
		t.Fatalf("unexpected backend params:\nwant: %#v\n got: %#v", wantParams, computeInvoker.lastReq.Params)
	}
	if !reflect.DeepEqual(result.Params, wantParams) {
		t.Fatalf("unexpected execution result params:\nwant: %#v\n got: %#v", wantParams, result.Params)
	}
	if got, ok := result.Output.(map[string]any); !ok || got["result"] != "ok" {
		t.Fatalf("unexpected output: %#v", result.Output)
	}
	if len(result.OutputLayers) != 1 || result.OutputLayers[0].Path != filepath.Join(projectDir, "buffer.gpkg") {
		t.Fatalf("unexpected output layers: %#v", result.OutputLayers)
	}
}

func TestEngineExecuteValidationFailure(t *testing.T) {
	t.Parallel()

	registry := loadRegistryForTest(t, map[string]string{
		"vector/buffer_analysis.yaml": `
apiVersion: tool/v1
kind: Tool
metadata:
  name: buffer_analysis
  domain: vector
  runtime: qgis
spec:
  description: Create a buffer.
  parameters:
    - name: layer
      type: string
      required: true
    - name: distance
      type: number
      required: true
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: "{{ resolve_layer(layer) }}"
    DISTANCE: "{{ distance }}"
  validation:
    - rule: "layer_exists(layer)"
      message: "layer '{{ layer }}' not found"
`,
	})

	computeInvoker := &fakeComputeInvoker{}
	engine := NewEngine(registry, NewComputeBackend(computeInvoker))
	ctx := WithSessionContext(context.Background(), SessionContext{
		Layers: []SessionLayer{{Name: "roads", Path: "/layers/roads.gpkg"}},
	})

	_, err := engine.Execute(ctx, "buffer_analysis", map[string]any{
		"layer":    "buildings",
		"distance": 25,
	})
	if err == nil {
		t.Fatal("expected validation failure")
	}
	if !strings.Contains(err.Error(), "layer 'buildings' not found") {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if computeInvoker.callCount != 0 {
		t.Fatalf("expected compute backend not to be called, got %d", computeInvoker.callCount)
	}
}

func TestEngineExecuteTriggersAutoLoadHandler(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	registry := loadRegistryForTest(t, map[string]string{
		"vector/buffer_analysis.yaml": `
apiVersion: tool/v1
kind: Tool
metadata:
  name: buffer_analysis
  domain: vector
  runtime: qgis
spec:
  description: Create a buffer.
  parameters:
    - name: layer
      type: string
      required: true
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: "{{ resolve_layer(layer) }}"
    OUTPUT: "{{ auto_output_path('buffer') }}"
  output:
    auto_load: true
`,
	})

	computeInvoker := &fakeComputeInvoker{
		resp: &compute.InvokeResponse{
			Success:      true,
			Output:       map[string]any{"status": "ok"},
			OutputLayers: []compute.OutputLayer{{Path: filepath.Join(projectDir, "buffer.gpkg"), AutoLoad: true}},
		},
	}
	engine := NewEngine(registry, NewComputeBackend(computeInvoker))
	called := 0
	var gotParams map[string]any
	var gotLayers []OutputLayer
	engine.SetAutoLoadHandler(func(_ context.Context, params map[string]any, layers []OutputLayer) {
		called++
		gotParams = cloneMapAny(params)
		gotLayers = cloneOutputLayers(layers)
	})

	ctx := toolsession.WithContext(context.Background(), "session-9", filepath.Join(projectDir, "demo.qgz"), "")
	ctx = WithSessionContext(ctx, SessionContext{
		Layers: []SessionLayer{{Name: "roads", Path: filepath.Join(projectDir, "roads.gpkg")}},
	})

	if _, err := engine.Execute(ctx, "buffer_analysis", map[string]any{"layer": "roads"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if called != 1 {
		t.Fatalf("expected auto load handler to be called once, got %d", called)
	}
	if gotParams["INPUT"] != filepath.Join(projectDir, "roads.gpkg") {
		t.Fatalf("unexpected auto load params: %#v", gotParams)
	}
	if len(gotLayers) != 1 || gotLayers[0].Path != filepath.Join(projectDir, "buffer.gpkg") {
		t.Fatalf("unexpected auto load layers: %#v", gotLayers)
	}
}

func TestEngineExecuteAutoMaterializesComputeOutputLayerForAutoLoad(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	registry := loadRegistryForTest(t, map[string]string{
		"vector/extract_by_expression.yaml": `
apiVersion: tool/v1
kind: Tool
metadata:
  name: extract_by_expression
  domain: vector
  runtime: qgis
spec:
  description: Extract features by expression.
  parameters:
    - name: INPUT
      type: string
      required: true
    - name: EXPRESSION
      type: string
      required: true
    - name: OUTPUT
      type: output_layer
      default: 'memory:'
execution:
  backend_tool: extract_by_expression
  param_mapping:
    INPUT: "{{ INPUT }}"
    EXPRESSION: "{{ EXPRESSION }}"
    OUTPUT: "{{ OUTPUT }}"
  output:
    auto_load: true
`,
	})

	computeInvoker := &fakeComputeInvoker{
		resp: &compute.InvokeResponse{Success: true, Output: map[string]any{"status": "ok"}},
	}
	engine := NewEngine(registry, NewComputeBackend(computeInvoker))
	ctx := toolsession.WithContext(context.Background(), "session-77", filepath.Join(projectDir, "demo.qgz"), "")
	if _, err := engine.Execute(ctx, "extract_by_expression", map[string]any{
		"INPUT":      "hubei",
		"EXPRESSION": `"name" <> '武汉市'`,
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	gotOutput, _ := computeInvoker.lastReq.Params["OUTPUT"].(string)
	wantOutput := "memory:"
	if gotOutput != wantOutput {
		t.Fatalf("unexpected auto materialized output path:\nwant: %s\n got: %s", wantOutput, gotOutput)
	}
}

func loadRegistryForTest(t *testing.T, files map[string]string) *Registry {
	t.Helper()
	toolRoot := t.TempDir()
	toolNames := make([]string, 0, len(files))
	for relativePath, content := range files {
		writeToolFixture(t, toolRoot, filepath.Join("system", relativePath), content)
		base := filepath.Base(relativePath)
		toolNames = append(toolNames, strings.TrimSuffix(base, filepath.Ext(base)))
	}
	sort.Strings(toolNames)
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("toolReg.LoadDirectories() error = %v", err)
	}
	dir := t.TempDir()
	packageDir := filepath.Join(dir, "test-tools")
	writeSkillFixture(t, packageDir, "SKILL.md", `---
name: test-tools
description: Test tools.
kind: category
tools:
  - `+strings.Join(toolNames, "\n  - ")+`
---

# Test tools
`)
	registry := NewRegistry(toolReg)
	if err := registry.LoadDirectories(dir); err != nil {
		t.Fatalf("LoadDirectories() error = %v", err)
	}
	return registry
}
