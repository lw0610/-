package flow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV4MemoryKeepsSourceChoicesWithoutAcquiredValues(t *testing.T) {
	root := t.TempDir()
	memory := NewInputMemory(filepath.Join(root, "memory.yaml"))
	old := filepath.Join(root, "old")
	if err := memory.Set("test-flow", "旧路径", old); err != nil {
		t.Fatal(err)
	}
	choice := map[string]any{"$fill": true, "path": root, "sources": map[string]any{"铁路": "source.fixture"}}
	if err := memory.SetChoice("test-flow", "数据", choice); err != nil {
		t.Fatal(err)
	}
	values, err := memory.GetValues("test-flow")
	if err != nil {
		t.Fatal(err)
	}
	if values["旧路径"] != old || values["数据"].(map[string]any)["$fill"] != true {
		t.Fatal(values)
	}
	values["数据"].(map[string]any)["path"] = "changed"
	again, _ := memory.GetValues("test-flow")
	if again["数据"].(map[string]any)["path"] != root {
		t.Fatal("aliased memory")
	}
	raw, _ := os.ReadFile(filepath.Join(root, "memory.yaml"))
	if !strings.Contains(string(raw), "flow-input-memory/v1") {
		t.Fatal("unnecessary disk version migration")
	}
	if err = memory.Set("test-flow", "数据", old); err != nil {
		t.Fatal(err)
	}
	again, _ = memory.GetValues("test-flow")
	if again["数据"] != old {
		t.Fatal(again)
	}
}
