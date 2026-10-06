package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

func TestBuildRuntimeSkillEngineRegistersInternalBackend(t *testing.T) {
	tempDir := t.TempDir()
	skillsDir := filepath.Join(tempDir, "skills")
	toolsDir := filepath.Join(tempDir, "tools")
	writeRuntimeFile(t, filepath.Join(skillsDir, "creator", "SKILL.md"), "---\nname: creator\ndescription: creator tools\nkind: category\ntools:\n  - list_skill_packages\n---\n\ncreator tools\n")
	writeRuntimeFile(t, filepath.Join(toolsDir, "creator", "list_skill_packages.yaml"), "apiVersion: tool/v1\nkind: Tool\nmetadata:\n  name: list_skill_packages\n  domain: creator\n  runtime: internal\nspec:\n  description: 列出技能包\nexecution:\n  backend_tool: list_skill_packages\n  param_mapping: {}\n")
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolsDir); err != nil {
		t.Fatalf("toolReg.LoadDirectories failed: %v", err)
	}
	registry := skill.NewRegistry(toolReg)
	if err := registry.LoadDirectories(skillsDir); err != nil {
		t.Fatalf("LoadDirectories failed: %v", err)
	}

	called := false
	runtimeEngine, err := buildRuntimeSkillEngine(registry, skill.BuildOptions{
		ComputeClient: compute.NewWSClient(noopComputeInvoker{}, "", time.Second),
		InternalInvoker: func(context.Context, string, map[string]any) (any, error) {
			called = true
			return map[string]any{"packages": []string{"skill-management"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("buildRuntimeSkillEngine failed: %v", err)
	}
	result, err := runtimeEngine.Execute(context.Background(), "list_skill_packages", map[string]any{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !called {
		t.Fatal("expected internal invoker to be called")
	}
	if result == nil || result.BackendID != skill.RuntimeIDInternal {
		t.Fatalf("expected internal backend result, got %#v", result)
	}
}

func TestBuildRuntimeSkillEngineRoutesOutputGetInfoThroughInternalBackend(t *testing.T) {
	tempDir := t.TempDir()
	skillsDir := filepath.Join(tempDir, "skills")
	toolsDir := filepath.Join(tempDir, "tools")
	writeRuntimeFile(t, filepath.Join(skillsDir, "runtime-control", "SKILL.md"), "---\nname: runtime-control\ndescription: runtime control\nkind: category\ntools:\n  - output_get_info\n---\n\nruntime control\n")
	writeRuntimeFile(t, filepath.Join(toolsDir, "runtime-control", "output_get_info.yaml"), "apiVersion: tool/v1\nkind: Tool\nmetadata:\n  name: output_get_info\n  domain: runtime-control\n  runtime: internal\nspec:\n  description: 输出信息\nexecution:\n  backend_tool: output_get_info\n  param_mapping: {}\n")
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolsDir); err != nil {
		t.Fatalf("toolReg.LoadDirectories failed: %v", err)
	}
	registry := skill.NewRegistry(toolReg)
	if err := registry.LoadDirectories(skillsDir); err != nil {
		t.Fatalf("LoadDirectories failed: %v", err)
	}

	called := false
	runtimeEngine, err := buildRuntimeSkillEngine(registry, skill.BuildOptions{
		ComputeClient: compute.NewWSClient(noopComputeInvoker{}, "", time.Second),
		InternalInvoker: func(context.Context, string, map[string]any) (any, error) {
			called = true
			return map[string]any{"success": true, "output": "/tmp/output", "info": map[string]any{"files_count": 0}}, nil
		},
	})
	if err != nil {
		t.Fatalf("buildRuntimeSkillEngine failed: %v", err)
	}
	result, err := runtimeEngine.Execute(context.Background(), "output_get_info", map[string]any{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !called {
		t.Fatal("expected internal invoker to be called")
	}
	if result == nil || result.BackendID != skill.RuntimeIDInternal {
		t.Fatalf("expected internal backend result, got %#v", result)
	}
}

func TestBuildRuntimeSkillEngineForwardsQGISArgsMutator(t *testing.T) {
	tempDir := t.TempDir()
	skillsDir := filepath.Join(tempDir, "skills")
	toolsDir := filepath.Join(tempDir, "tools")
	writeRuntimeFile(
		t,
		filepath.Join(skillsDir, "route-knowledge", "SKILL.md"),
		"---\nname: route-knowledge\ndescription: route knowledge\nkind: knowledge\ntools:\n  - marching_route_compile_dataset\n---\n\nroute knowledge\n",
	)
	writeRuntimeFile(
		t,
		filepath.Join(toolsDir, "route-knowledge", "marching_route_compile_dataset.yaml"),
		"apiVersion: tool/v1\nkind: Tool\nmetadata:\n  name: marching_route_compile_dataset\n  domain: route-knowledge\n  runtime: qgis\nspec:\n  description: compile route knowledge\n  parameters:\n    - name: source_text\n      type: string\n      required: true\nexecution:\n  backend_tool: marching_route_compile_dataset\n  param_mapping:\n    source_text: '{{ source_text }}'\n",
	)
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolsDir); err != nil {
		t.Fatalf("toolReg.LoadDirectories failed: %v", err)
	}
	registry := skill.NewRegistry(toolReg)
	if err := registry.LoadDirectories(skillsDir); err != nil {
		t.Fatalf("LoadDirectories failed: %v", err)
	}

	var invokedArgs map[string]any
	runtimeEngine, err := buildRuntimeSkillEngine(registry, skill.BuildOptions{
		ComputeClient: compute.NewWSClient(noopComputeInvoker{}, "", time.Second),
		QGISResponseInvoker: func(_ context.Context, _ string, args map[string]any) (*compute.InvokeResponse, error) {
			invokedArgs = args
			return &compute.InvokeResponse{Success: true, Output: map[string]any{"ok": true}}, nil
		},
		ArgsMutator: func(_ context.Context, args map[string]any) {
			args["session_dir"] = "/tmp/autocarto/session-1"
			args["session_subdirs"] = map[string]any{
				"intermediate": "/tmp/autocarto/session-1/intermediate",
			}
		},
	})
	if err != nil {
		t.Fatalf("buildRuntimeSkillEngine failed: %v", err)
	}
	result, err := runtimeEngine.Execute(
		context.Background(),
		"marching_route_compile_dataset",
		map[string]any{"source_text": "测试文本"},
	)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == nil || result.BackendID != skill.RuntimeIDQGIS {
		t.Fatalf("expected QGIS backend result, got %#v", result)
	}
	if got := invokedArgs["session_dir"]; got != "/tmp/autocarto/session-1" {
		t.Fatalf("session_dir = %v, want runtime-injected directory", got)
	}
}

func writeRuntimeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

type noopComputeInvoker struct{}

func (noopComputeInvoker) ListInstanceTools(context.Context, string) ([]map[string]any, error) {
	return nil, nil
}

func (noopComputeInvoker) InvokeTool(context.Context, string, map[string]any) (*compute.InvokeResponse, error) {
	return nil, errors.New("unexpected compute invoke")
}
