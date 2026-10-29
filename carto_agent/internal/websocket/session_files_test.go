package websocket

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	preview, _ := json.Marshal(map[string]any{"find_id": "df-fixture", "rows": []any{map[string]any{"row": "roads", "candidates": []any{map[string]any{"path": input}, map[string]any{"path": "/unavailable/builtin/省界.qml"}}}}})
	if failures := registerToolArtifacts(store, tooldefinition.ToolDefinition{}, "data_find", "orchestrator", &schema.Message{Content: string(preview)}, consumeEventsOptions{}); len(failures) != 0 {
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

func TestFileReadPreservesRegisteredSourceAndDoesNotPublishInputs(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "map.qgz")
	store, err := sessionfiles.New(project, "read-source")
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(store.Dir, "output", "source.csv")
	if err := os.MkdirAll(filepath.Dir(original), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, []byte("region,value\nA,10\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := store.Register(original, sessionfiles.Origin{
		Type: "web", Tool: "web_download_file", Label: "官方统计原件", CallID: "download-source",
		Agent: "system_data_acquisition", SourceID: "official-table", License: "source-attribution",
	}, sessionfiles.WithFlow("flow-source", "acquire-statistics"))
	if err != nil {
		t.Fatal(err)
	}
	before, err = store.Get(before.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A read can return an already indexed source, a newly inspected project
	// input, or an authorized bound file outside the artifact roots. None is a
	// newly produced output, including during Plan-mode inspection.
	for _, path := range []string{original, filepath.Join(filepath.Dir(project), "existing.csv"), filepath.Join(t.TempDir(), "bound.csv")} {
		if path != original {
			if err := os.WriteFile(path, []byte("input"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		raw, _ := json.Marshal(map[string]any{"success": true, "path": path, "kind": "text", "content": "input"})
		message := &schema.Message{ToolCallID: "inspect-source", UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeText, Text: string(raw)}}}
		if failures := registerToolArtifacts(store, tooldefinition.ToolDefinition{}, "file_read", "orchestrator", message, consumeEventsOptions{}); len(failures) != 0 {
			t.Fatalf("reading an input attempted artifact registration: %v", failures)
		}
	}
	after, err := store.Get(before.ID)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("reading a source changed its provenance or registration: before=%+v after=%+v err=%v", before, after, err)
	}
	items, err := store.List(true)
	if err != nil || len(items) != 1 {
		t.Fatalf("read inputs appeared as new artifacts: %+v %v", items, err)
	}
	// New files written by the companion file tool remain real outputs.
	written := filepath.Join(store.Dir, "output", "result.csv")
	if err := os.WriteFile(written, []byte("region,value\nA,10\n"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"success": true, "path": written})
	if failures := registerToolArtifacts(store, tooldefinition.ToolDefinition{}, "file_write", "system_data_acquisition", &schema.Message{Content: string(raw)}, consumeEventsOptions{}); len(failures) != 0 {
		t.Fatal(failures)
	}
	items, err = store.List(true)
	if err != nil || len(items) != 2 {
		t.Fatalf("written output was lost: %+v %v", items, err)
	}
}
