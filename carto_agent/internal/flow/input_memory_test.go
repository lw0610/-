package flow

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestInputMemoryReadIsPureAndMutationsAreIsolated(t *testing.T) {
	root := t.TempDir()
	memory := NewInputMemory(filepath.Join(root, "config", "memory.yaml"))
	values, err := memory.Get("first-flow")
	if err != nil || len(values) != 0 {
		t.Fatal(values, err)
	}
	if _, err := os.Stat(filepath.Join(root, "config")); !os.IsNotExist(err) {
		t.Fatal("read created local files")
	}
	path := filepath.Join(root, "data")
	if err := memory.Set("first-flow", "数据", path); err != nil {
		t.Fatal(err)
	}
	values, _ = memory.Get("first-flow")
	values["数据"] = "mutated"
	again, _ := memory.Get("first-flow")
	if again["数据"] != path {
		t.Fatal("Get exposed mutable stored state")
	}
	if err := memory.Set("second-flow", "数据", path); err != nil {
		t.Fatal(err)
	}
	if err := memory.DeleteFlow("first-flow"); err != nil {
		t.Fatal(err)
	}
	values, _ = memory.Get("first-flow")
	if len(values) != 0 {
		t.Fatal(values)
	}
	values, _ = memory.Get("second-flow")
	if values["数据"] != path {
		t.Fatal(values)
	}
	if err := memory.Set("second-flow", "数据", ""); err != nil {
		t.Fatal(err)
	}
	values, _ = memory.Get("second-flow")
	if len(values) != 0 {
		t.Fatal(values)
	}
	info, _ := os.Stat(memory.path)
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("local choices are readable by other users")
	}
}

func TestInputMemoryConcurrentInstancesDoNotLoseWrites(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "memory.yaml")
	var group sync.WaitGroup
	for index := 0; index < 25; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			if err := NewInputMemory(file).Set("parallel-flow", fmt.Sprint(index), filepath.Join(root, fmt.Sprint(index))); err != nil {
				t.Error(err)
			}
		}(index)
	}
	group.Wait()
	values, err := NewInputMemory(file).Get("parallel-flow")
	if err != nil || len(values) != 25 {
		t.Fatal(values, err)
	}
}

func TestInputMemoryRejectsInvalidPathsAndPreservesCorruptFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "memory.yaml")
	memory := NewInputMemory(file)
	for _, value := range []string{"relative/path", "../escape", "/tmp/bad\x00path"} {
		if err := memory.Set("valid-flow", "数据", value); err == nil {
			t.Fatal("accepted", value)
		}
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("invalid values created file")
	}
	corrupt := "apiVersion: unknown/v999\nflows: {}\n"
	lockWrite(t, file, corrupt)
	if err := memory.Set("valid-flow", "数据", root); err == nil {
		t.Fatal("overwrote unsupported memory document")
	}
	raw, _ := os.ReadFile(file)
	if string(raw) != corrupt {
		t.Fatal("modified unsupported file")
	}
}
