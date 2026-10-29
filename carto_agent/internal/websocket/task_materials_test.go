package websocket

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/fileops"
	"github.com/autocarto/carto_agent/internal/taskmaterials"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/schema"
)

func TestTaskAttachmentsAreReadableAndRetainedAcrossUserTurns(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "map.qgz")
	sid := "attachment-roundtrip"
	ctx := toolsession.WithContext(context.Background(), sid, project, "")
	server := &DeepServer{}
	folder := t.TempDir()
	path := filepath.Join(folder, "史料.txt")
	if err := os.WriteFile(path, []byte("从何家冲出发，经桐柏地区到枣阳。"), 0600); err != nil {
		t.Fatal(err)
	}
	bind := func(attachments []map[string]any) {
		t.Helper()
		msg := Message{Content: "处理提供的材料", Data: map[string]any{"attachments": attachments}}
		if err := server.bindTaskReferences(ctx, sid, project, msg, schema.UserMessage(msg.Content)); err != nil {
			t.Fatal(err)
		}
	}
	bind([]map[string]any{{"path": path, "kind": "file"}})
	first, err := taskmaterials.Prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	message, err := taskmaterials.ContextMessage(first)
	if err != nil || message == nil || !strings.Contains(message.Content, "何家冲") {
		t.Fatalf("upload did not reach model context: %+v %v", message, err)
	}
	bind([]map[string]any{{"path": folder, "kind": "folder"}})
	bind([]map[string]any{{"path": folder, "kind": "folder"}})
	bind(nil) // Follow-up turns keep the latest materials without re-uploading.
	dir, err := config.ResolveSessionDir(project, sid)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := dataservice.LoadBindings(dir)
	if err != nil || len(bindings) != 2 || bindings[0].Current || !bindings[1].Current {
		t.Fatalf("bad current/history references: %+v %v", bindings, err)
	}
	result, err := fileops.Invoke(ctx, fileops.ToolReadFile, map[string]any{"path": folder})
	if err != nil || result.(map[string]any)["kind"] != "directory" {
		t.Fatalf("uploaded folder not readable: %+v %v", result, err)
	}
	text, err := fileops.ReadText(ctx, path)
	if err != nil || !strings.Contains(text, "何家冲") {
		t.Fatalf("folder child unreadable: %q %v", text, err)
	}
}
