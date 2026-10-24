package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

func TestEngineExecuteCustomCLIUsesSkillDirPackageRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	packageDir := filepath.Join(root, "cli-tools")
	writeSkillFixture(t, packageDir, "SKILL.md", `---
name: cli-tools
description: CLI tools.
kind: category
tools:
  - custom_cli
---

# CLI tools
`)
	toolRoot := t.TempDir()
	writeToolFixture(t, toolRoot, "system/cli-tools/custom_cli.yaml", `apiVersion: tool/v1
kind: Tool
metadata:
  name: custom_cli
  domain: cli-tools
  runtime: cli
spec:
  description: Execute custom cli.
execution:
  cli:
    command: /bin/sh
    args:
      - '{{ skill_dir }}/scripts/run.sh'
    working_dir: '{{ skill_dir }}'
    stdin: json
    stdout: json
    env:
      AUTOCARTO_TEST_MODE: '{{ project_id }}'
  param_mapping:
    input_file: '{{ input_file }}'
`)
	writeToolFixture(t, toolRoot, filepath.Join("system", "cli-tools", "scripts", "run.sh"), `#!/bin/sh
payload="$(cat)"
printf '{"success":true,"output":{"cwd":"%s","payload":%s,"mode":"%s"},"error":"","output_layers":[]}' "$PWD" "$payload" "$AUTOCARTO_TEST_MODE"
`)
	toolPackageDir := filepath.Join(toolRoot, "system", "cli-tools")
	if err := os.Chmod(filepath.Join(toolPackageDir, "scripts", "run.sh"), 0o755); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("toolReg.LoadDirectories() error = %v", err)
	}
	registry := NewRegistry(toolReg)
	if err := registry.LoadDirectories(root); err != nil {
		t.Fatalf("LoadDirectories() error = %v", err)
	}

	engine := NewEngine(registry, NewCLIBackend(nil))
	ctx := WithSessionContext(context.Background(), SessionContext{ProjectID: "project-123"})
	result, err := engine.Execute(ctx, "custom_cli", map[string]any{"input_file": "/tmp/input.geojson"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output type: %#v", result.Output)
	}
	physicalDir, err := filepath.EvalSymlinks(toolPackageDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := output["cwd"].(string); got != physicalDir {
		t.Fatalf("unexpected cwd: got=%q want=%q", got, toolPackageDir)
	}
	if got, _ := output["mode"].(string); got != "project-123" {
		t.Fatalf("unexpected env propagation: got=%q", got)
	}
	payload, ok := output["payload"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload: %#v", output["payload"])
	}
	if got, _ := payload["input_file"].(string); got != "/tmp/input.geojson" {
		t.Fatalf("unexpected payload input_file: %q", got)
	}
}

func TestValidateSkillDefinitionAcceptsCLIExecutionWithoutBackendTool(t *testing.T) {
	t.Parallel()

	err := ValidateSkillDefinition(SkillDefinition{
		APIVersion: SkillAPIVersionV2,
		Kind:       SkillKindAtomic,
		Metadata: SkillMetadata{
			Name:    "custom_cli",
			Runtime: RuntimeIDCLI,
		},
		Spec: SkillSpec{Description: "custom cli"},
		Execution: SkillExecution{
			CLI:          &CLIExecutionConfig{Command: "/bin/sh"},
			ParamMapping: map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("ValidateSkillDefinition() error = %v", err)
	}
}
