package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileParsesSerialWorkflowV1(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "workflow.yaml")
	writeWorkflowFile(t, path, `apiVersion: workflow/v1
kind: serial_workflow
metadata:
  name: data-loading-inspection-vector
spec:
  inputs:
    project_path:
      type: string
      required: true
  steps:
    - id: open_project
      type: tool
      tool: open_project
      with:
        project_path: "{{ inputs.project_path }}"
    - id: validate_layer
      type: tool
      tool: get_layers_info
      with: {}
`)

	definition, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if definition.APIVersion != APIVersionV1 || definition.Kind != KindSerialWorkflow {
		t.Fatalf("unexpected workflow header: %#v", definition)
	}
	if definition.Metadata.Name != "data-loading-inspection-vector" {
		t.Fatalf("unexpected metadata name: %#v", definition.Metadata)
	}
	if len(definition.Spec.Inputs) != 1 || !definition.Spec.Inputs["project_path"].Required {
		t.Fatalf("unexpected inputs: %#v", definition.Spec.Inputs)
	}
	if len(definition.Spec.Steps) != 2 || definition.Spec.Steps[0].Tool != "open_project" || definition.Spec.Steps[1].ID != "validate_layer" {
		t.Fatalf("unexpected steps: %#v", definition.Spec.Steps)
	}
}

func TestLoadFileRejectsUnsupportedWorkflowKind(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "workflow.yaml")
	writeWorkflowFile(t, path, `apiVersion: workflow/v1
kind: branching_workflow
metadata:
  name: bad-workflow
spec:
  steps: []
`)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected unsupported workflow kind error")
	}
	if err.Error() != `unsupported workflow kind "branching_workflow"` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFileRejectsNonToolStepInV1(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "workflow.yaml")
	writeWorkflowFile(t, path, `apiVersion: workflow/v1
kind: serial_workflow
metadata:
  name: bad-step-workflow
spec:
  steps:
    - id: decide
      type: branch
      tool: open_project
      with: {}
`)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected unsupported step type error")
	}
	if err.Error() != `workflow step "decide" has unsupported type "branch"` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFileRejectsMissingToolName(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "workflow.yaml")
	writeWorkflowFile(t, path, `apiVersion: workflow/v1
kind: serial_workflow
metadata:
  name: missing-tool
spec:
  steps:
    - id: open_project
      type: tool
      with: {}
`)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected missing tool error")
	}
	if err.Error() != `workflow step "open_project" missing tool` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFileRejectsUnsupportedTemplateVariable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "workflow.yaml")
	writeWorkflowFile(t, path, `apiVersion: workflow/v1
kind: serial_workflow
metadata:
  name: bad-template
spec:
  steps:
    - id: open_project
      type: tool
      tool: open_project
      with:
        project_path: "{{ context.project_path }}"
`)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected invalid template variable error")
	}
	if err.Error() != `workflow step "open_project" has unsupported template "{{ context.project_path }}"` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeWorkflowFile(t *testing.T, path, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir workflow dir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write workflow file failed: %v", err)
	}
}
