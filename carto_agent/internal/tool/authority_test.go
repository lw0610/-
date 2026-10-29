package tool

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestExecutionManifestPreservesExplicitAutoLoad(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		definition := ToolDefinition{Execution: ToolExecution{Output: &ToolOutput{AutoLoad: enabled}}}
		raw, err := json.Marshal(definition.ExecutionManifestEntry())
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["auto_load"] != enabled {
			t.Fatalf("output loading policy lost in manifest: %s", raw)
		}
	}
}

func TestExecutionManifestEntryDerivesHandlerFileAndFunction(t *testing.T) {
	t.Parallel()

	definition := ToolDefinition{
		APIVersion: ToolAPIVersionV1,
		Kind:       ToolKindDefinition,
		Metadata: ToolMetadata{
			Name:    "open_project",
			Domain:  "project-state",
			Runtime: RuntimeIDQGIS,
		},
		Spec: ToolSpec{
			Description: "打开工程",
		},
		Execution: ToolExecution{
			Handler: "handlers.project.open_project",
		},
		SourcePath: filepath.ToSlash(filepath.Join("tools", "system", "project-state", "open_project.yaml")),
	}

	entry := definition.ExecutionManifestEntry()
	if got := entry.HandlerFile; got != "tools/system/project-state/handlers/project.py" {
		t.Fatalf("unexpected handler_file: %q", got)
	}
	if got := entry.HandlerFunction; got != "open_project" {
		t.Fatalf("unexpected handler_function: %q", got)
	}
	if got := string(entry.Visibility); got != VisibilityPublic {
		t.Fatalf("unexpected visibility: %q", got)
	}
}

func TestBuildAccessPolicyIndexInfersInternalRuntime(t *testing.T) {
	t.Parallel()

	index := BuildAccessPolicyIndex([]ToolDefinition{
		{
			APIVersion: ToolAPIVersionV1,
			Kind:       ToolKindDefinition,
			Metadata: ToolMetadata{
				Name:    "hitl_request",
				Runtime: RuntimeIDInternal,
			},
			Spec: ToolSpec{Description: "hitl"},
			Execution: ToolExecution{
				BackendTool: "hitl_request",
			},
		},
	})

	policy, ok := index["hitl_request"]
	if !ok {
		t.Fatal("expected hitl_request policy")
	}
	if got := policy.Visibility; got != VisibilityInternal {
		t.Fatalf("unexpected visibility: %q", got)
	}
	if len(policy.AuthorizationScopes) != 0 {
		t.Fatalf("expected no authorization scopes, got %#v", policy.AuthorizationScopes)
	}
}
