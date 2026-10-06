package workflow

import "testing"

func TestCompileSerialPlanResolvesInputTemplates(t *testing.T) {
	definition := &Definition{
		APIVersion: APIVersionV1,
		Kind:       KindSerialWorkflow,
		Metadata:   Metadata{Name: "vector-workflow"},
		Spec: Spec{
			Inputs: map[string]Input{
				"project_path": {Type: "string", Required: true},
			},
			Steps: []Step{
				{ID: "open_project", Type: StepTypeTool, Tool: "open_project", With: map[string]any{"project_path": "{{ inputs.project_path }}"}},
				{ID: "report", Type: StepTypeTool, Tool: "get_layers_info", With: map[string]any{}},
			},
		},
	}

	plan, err := CompileSerialPlan(definition, map[string]any{"project_path": "/tmp/demo.qgz"})
	if err != nil {
		t.Fatalf("CompileSerialPlan returned error: %v", err)
	}
	if plan.Name != "vector-workflow" || len(plan.Steps) != 2 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if got := plan.Steps[0].Arguments["project_path"]; got != "/tmp/demo.qgz" {
		t.Fatalf("unexpected resolved argument: %#v", plan.Steps[0].Arguments)
	}
	if plan.Steps[1].ToolName != "get_layers_info" {
		t.Fatalf("unexpected second step: %#v", plan.Steps[1])
	}
}

func TestCompileSerialPlanRejectsMissingRequiredInput(t *testing.T) {
	definition := &Definition{
		APIVersion: APIVersionV1,
		Kind:       KindSerialWorkflow,
		Metadata:   Metadata{Name: "vector-workflow"},
		Spec: Spec{
			Inputs: map[string]Input{
				"project_path": {Type: "string", Required: true},
			},
			Steps: []Step{{ID: "open_project", Type: StepTypeTool, Tool: "open_project", With: map[string]any{"project_path": "{{ inputs.project_path }}"}}},
		},
	}

	_, err := CompileSerialPlan(definition, map[string]any{})
	if err == nil {
		t.Fatal("expected missing required input error")
	}
	if err.Error() != `workflow input "project_path" is required` {
		t.Fatalf("unexpected error: %v", err)
	}
}
