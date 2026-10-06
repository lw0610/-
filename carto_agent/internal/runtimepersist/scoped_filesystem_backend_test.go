package runtimepersist

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/adk/filesystem"
)

func TestResolveReductionOffloadRoot(t *testing.T) {
	root := ResolveReductionOffloadRoot("/tmp/autocarto-config", "build/default", "orchestrator:v2")
	want := filepath.Join("/tmp/autocarto-config", "runtime", "reduction", "build_default", "orchestrator_v2")
	if root != want {
		t.Fatalf("unexpected offload root: got %s want %s", root, want)
	}
}

func TestScopedFilesystemBackendReadWrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runtime", "reduction")
	backend, err := NewScopedFilesystemBackend(root)
	if err != nil {
		t.Fatalf("create backend failed: %v", err)
	}

	ctx := context.Background()
	filePath := filepath.Join(root, "clear", "call-1.txt")
	if err := backend.Write(ctx, &filesystem.WriteRequest{
		FilePath: filePath,
		Content:  "line1\nline2\nline3",
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	content, err := backend.Read(ctx, &filesystem.ReadRequest{
		FilePath: filePath,
		Offset:   2,
		Limit:    1,
	})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if content.Content != "line2" {
		t.Fatalf("unexpected content: %q", content.Content)
	}
}

func TestScopedFilesystemBackendRejectsEscapes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runtime", "reduction")
	backend, err := NewScopedFilesystemBackend(root)
	if err != nil {
		t.Fatalf("create backend failed: %v", err)
	}

	err = backend.Write(context.Background(), &filesystem.WriteRequest{
		FilePath: filepath.Join(root, "..", "outside.txt"),
		Content:  "bad",
	})
	if err == nil {
		t.Fatal("expected write outside root to fail")
	}
}
