package websocket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionPersistence_UsesAppLocalRuntimeDir(t *testing.T) {
	runtimeHome := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", runtimeHome)

	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "runtime-owned-session"

	if err := server.saveSessionToFile(map[string]any{
		"session_id":   sessionID,
		"project_path": projectPath,
		"messages":     []any{},
		"tool_calls":   []any{},
		"snapshots":    []any{},
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	baseDir, err := server.getSessionsBaseDir(projectPath)
	if err != nil {
		t.Fatalf("getSessionsBaseDir failed: %v", err)
	}
	if !strings.HasPrefix(baseDir, filepath.Join(runtimeHome, "data", "sessions")) {
		t.Fatalf("expected runtime-owned sessions dir, got=%s", baseDir)
	}
	if _, err := os.Stat(filepath.Join(baseDir, sessionID, "session.json")); err != nil {
		t.Fatalf("expected session.json in runtime-owned dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "autoc")); !os.IsNotExist(err) {
		t.Fatalf("project-local autoc dir should not be the canonical owner, err=%v", err)
	}
}

func TestSessionSnapshot_RestoresAfterBackendRestart(t *testing.T) {
	runtimeHome := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", runtimeHome)

	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "restart-restore-session"

	writer := NewDeepServer(0, nil)
	if err := writer.saveSessionToFile(map[string]any{
		"session_id":   sessionID,
		"project_path": projectPath,
		"messages": []any{
			map[string]any{"role": "user", "content": "persist me"},
		},
		"created_at": "2026-03-20T10:00:00Z",
		"updated_at": "2026-03-20T10:01:00Z",
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	baseDir, err := writer.getSessionsBaseDir(projectPath)
	if err != nil {
		t.Fatalf("getSessionsBaseDir failed: %v", err)
	}
	sessionDir := filepath.Join(baseDir, sessionID)
	if err := os.Remove(filepath.Join(sessionDir, "session.json")); err != nil {
		t.Fatalf("remove session.json failed: %v", err)
	}

	reader := NewDeepServer(0, nil)
	restored, err := reader.loadSessionFromFile(sessionID, projectPath)
	if err != nil {
		t.Fatalf("loadSessionFromFile failed: %v", err)
	}
	if !strings.HasPrefix(sessionDir, filepath.Join(runtimeHome, "data", "sessions")) {
		t.Fatalf("expected runtime-owned session dir, got=%s", sessionDir)
	}
	messages, ok := restored["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("unexpected restored messages payload: %#v", restored["messages"])
	}
}
