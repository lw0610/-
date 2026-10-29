package flow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyMemoryReadsSourceChoicesWithoutRewriting(t *testing.T) {
	root := t.TempDir()
	memory := NewInputMemory(filepath.Join(root, "memory.yaml"))
	old := filepath.Join(root, "old")
	if err := memory.Set("test-flow", "旧路径", old); err != nil {
		t.Fatal(err)
	}
	rawSource := "apiVersion: flow-input-memory/v1\nflows:\n  test-flow: {旧路径: '" + old + "'}\nchoices:\n  test-flow:\n    数据: {$fill: true, path: '" + root + "', sources: {铁路: source.fixture}}\n"
	if err := os.WriteFile(memory.path, []byte(rawSource), 0600); err != nil {
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
	if string(raw) != rawSource {
		t.Fatal("read rewrote legacy memory")
	}
	if err = memory.Set("test-flow", "数据", old); err != nil {
		t.Fatal(err)
	}
	again, _ = memory.GetValues("test-flow")
	if again["数据"] != old {
		t.Fatal(again)
	}
}
