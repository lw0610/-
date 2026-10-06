package skill

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveParamMappingPreservesNestedObjectForFullTemplate(t *testing.T) {
	t.Parallel()

	graph := map[string]any{
		"schema_version": "1.0",
		"task": map[string]any{
			"task_id":  "TASK_MAIN",
			"map_type": "marching_route",
		},
		"routes": []any{
			map[string]any{"route_id": "R1"},
		},
	}
	resolved, err := resolveParamMapping(
		map[string]any{"knowledge_json": "{{ knowledge_json }}"},
		map[string]any{"knowledge_json": graph},
		[]SkillParameter{{Name: "knowledge_json", Type: "object", Required: true}},
		SessionContext{},
		"",
	)
	if err != nil {
		t.Fatalf("resolveParamMapping() error = %v", err)
	}
	if _, isString := resolved["knowledge_json"].(string); isString {
		t.Fatalf("knowledge_json was stringified: %#v", resolved["knowledge_json"])
	}
	if !reflect.DeepEqual(resolved["knowledge_json"], graph) {
		t.Fatalf("knowledge_json changed: got=%#v want=%#v", resolved["knowledge_json"], graph)
	}
}

func TestBuildResolveVarsIncludesExecutionContextPaths(t *testing.T) {
	t.Parallel()

	session := SessionContext{
		ProjectID:     "project-1",
		PythonPath:    " /opt/qgis/bin/python ",
		AutocartoHome: " /tmp/.autocarto ",
	}

	vars := buildResolveVars(nil, session, "/tmp/skills/vector/tools/buffer.yaml")

	if got := vars["python_path"]; got != "/opt/qgis/bin/python" {
		t.Fatalf("unexpected python_path: %#v", got)
	}
	if got := vars["autocarto_home"]; got != "/tmp/.autocarto" {
		t.Fatalf("unexpected autocarto_home: %#v", got)
	}
	if got := vars["skill_dir"]; got != "/tmp/skills/vector" {
		t.Fatalf("unexpected skill_dir: %#v", got)
	}
}

func TestBuildResolveVarsDefaultsAutocartoHomeFromResolvedHome(t *testing.T) {
	want := filepath.Join(t.TempDir(), "autocarto")
	t.Setenv("AUTOCARTO_HOME", want)

	vars := buildResolveVars(nil, SessionContext{}, "")
	if got := vars["autocarto_home"]; got != want {
		t.Fatalf("unexpected autocarto_home: got=%#v want=%#v", got, want)
	}
}

func TestSessionContextRoundTripPreservesExecutionPaths(t *testing.T) {
	t.Parallel()

	ctx := WithSessionContext(context.Background(), SessionContext{
		PythonPath:    " /opt/qgis/python ",
		AutocartoHome: " /tmp/.autocarto ",
	})

	session := SessionContextFromContext(ctx)
	if session.PythonPath != "/opt/qgis/python" {
		t.Fatalf("unexpected PythonPath: %q", session.PythonPath)
	}
	if session.AutocartoHome != "/tmp/.autocarto" {
		t.Fatalf("unexpected AutocartoHome: %q", session.AutocartoHome)
	}
}
