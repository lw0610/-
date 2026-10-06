package fileops

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHandles(t *testing.T) {
	t.Parallel()

	if !Handles(ToolCreateDirectory) {
		t.Fatal("expected create_directory to be handled")
	}
	if Handles("unknown_tool") {
		t.Fatal("expected unknown_tool to be ignored")
	}
}

func TestInvokeCreateDirectoryCreatesTarget(t *testing.T) {
	t.Parallel()

	targetPath := filepath.Join(t.TempDir(), "output", "nested")
	result, err := Invoke(context.Background(), ToolCreateDirectory, map[string]any{
		"PATH": targetPath,
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %#v", result)
	}
	if payload["output"] != targetPath {
		t.Fatalf("unexpected output path: %#v", payload["output"])
	}
	info, statErr := os.Stat(targetPath)
	if statErr != nil {
		t.Fatalf("expected directory to exist: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", targetPath)
	}
}

func TestInvokeCreateDirectoryTreatsExistingDirectoryAsSuccess(t *testing.T) {
	t.Parallel()

	targetPath := filepath.Join(t.TempDir(), "existing")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatalf("mkdir existing dir failed: %v", err)
	}

	result, err := Invoke(context.Background(), ToolCreateDirectory, map[string]any{
		"PATH": targetPath,
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	payload := result.(map[string]any)
	info, ok := payload["info"].(map[string]any)
	if !ok {
		t.Fatalf("expected info map, got %#v", payload["info"])
	}
	if info["created"] != false {
		t.Fatalf("expected created=false, got %#v", info["created"])
	}
}

func TestInvokeCreateDirectoryRejectsMissingPath(t *testing.T) {
	t.Parallel()

	if _, err := Invoke(context.Background(), ToolCreateDirectory, map[string]any{}); err == nil {
		t.Fatal("expected missing PATH to fail")
	}
}

func TestInvokeCreateDirectoryRejectsExistingFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := filepath.Join(root, "already-file")
	if err := os.WriteFile(targetPath, []byte("demo"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	if _, err := Invoke(context.Background(), ToolCreateDirectory, map[string]any{
		"PATH": targetPath,
	}); err == nil {
		t.Fatal("expected existing file path to fail")
	}
}
