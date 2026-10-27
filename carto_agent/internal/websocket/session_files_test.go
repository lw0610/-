package websocket

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/sessionfiles"
	tooldefinition "github.com/autocarto/carto_agent/internal/tool"
	"github.com/cloudwego/eino/schema"
)

func TestRegisterToolArtifactsBoundaryAndLayoutOutputs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", home)
	project := filepath.Join(t.TempDir(), "map.qgz")
	store, err := sessionfiles.New(project, "tool-files")
	if err != nil {
		t.Fatal(err)
	}
	write := func(path string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, tc := range []struct{ tool, path string }{
		{"admin_boundary_extract", filepath.Join(home, "data", "boundaries", "city.geojson")},
		{"export_layout_image", filepath.Join(store.Dir, "output", "layout.png")},
		{"export_layout_pdf", filepath.Join(filepath.Dir(project), "layout.pdf")},
		{"export_layout_svg", filepath.Join(store.Dir, "exports", "layout.svg")},
	} {
		path := write(tc.path)
		raw, _ := json.Marshal(map[string]any{"success": true, "output": path, "data": map[string]any{"artifact_paths": []string{path}}})
		parts, _ := json.Marshal([]map[string]any{{"type": "text", "text": string(raw)}, {"type": "file_url", "file": map[string]any{"url": "file://" + path, "extra": map[string]any{"path": path}}}})
		message := &schema.Message{ToolCallID: "call-" + tc.tool, Content: string(raw) + "\n[文件] " + filepath.Base(path)}
		if err := json.Unmarshal(parts, &message.UserInputMultiContent); err != nil {
			t.Fatal(err)
		}
		if failures := registerToolArtifacts(store, tooldefinition.ToolDefinition{}, tc.tool, "cartographer", message, consumeEventsOptions{}); len(failures) != 0 {
			t.Fatal(failures)
		}
	}
	items, err := store.List(false)
	if err != nil || len(items) != 4 {
		t.Fatalf("boundary/layout not registered: %+v %v", items, err)
	}
	for _, item := range items {
		if item.Name != "city.geojson" && item.Group != "deliverable" {
			t.Fatalf("layout hidden: %+v", item)
		}
	}
	input := write(filepath.Join(filepath.Dir(project), "existing.geojson"))
	raw, _ := json.Marshal(map[string]any{"path": input})
	registerToolArtifacts(store, tooldefinition.ToolDefinition{}, "data_load_vector", "data_engineer", &schema.Message{Content: string(raw)}, consumeEventsOptions{})
	preview, _ := json.Marshal(map[string]any{"plan": map[string]any{"resources": []any{map[string]any{"entries": []any{map[string]any{"path": input}, map[string]any{"path": "/unavailable/builtin/省界.qml"}}}}}})
	if failures := registerToolArtifacts(store, tooldefinition.ToolDefinition{}, "flow_preview", "orchestrator", &schema.Message{Content: string(preview)}, consumeEventsOptions{}); len(failures) != 0 {
		t.Fatalf("preview input generated a registration warning: %v", failures)
	}
	items, _ = store.List(false)
	if len(items) != 4 {
		t.Fatal("input registered as output", items)
	}
	outside := write(filepath.Join(t.TempDir(), "outside.pdf"))
	raw, _ = json.Marshal(map[string]any{"output": outside})
	failures := registerToolArtifacts(store, tooldefinition.ToolDefinition{}, "export_layout_pdf", "cartographer", &schema.Message{Content: string(raw)}, consumeEventsOptions{})
	if len(failures) != 1 || !errors.Is(failures[0], sessionfiles.ErrOutsideRoot) {
		t.Fatalf("registration failure was swallowed: %v", failures)
	}
}
