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

func TestMigrateLegacySessionsBaseDirFallsBackAcrossDevices(t *testing.T) {
	crossDeviceRoot := requireCrossDeviceDir(t)
	legacyBaseDir := filepath.Join(t.TempDir(), "autoc", "sessions", "demo.qgz")
	legacySessionDir := filepath.Join(legacyBaseDir, "sid-001")
	if err := os.MkdirAll(legacySessionDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy session dir failed: %v", err)
	}
	legacySessionFile := filepath.Join(legacySessionDir, "session.json")
	legacyContent := []byte(`{"session_id":"sid-001"}`)
	if err := os.WriteFile(legacySessionFile, legacyContent, 0o644); err != nil {
		t.Fatalf("write legacy session failed: %v", err)
	}

	canonicalBaseDir := filepath.Join(crossDeviceRoot, "autocarto-session-test", "demo")
	_ = os.RemoveAll(filepath.Dir(canonicalBaseDir))

	if err := migrateLegacySessionsBaseDir(legacyBaseDir, canonicalBaseDir); err != nil {
		t.Fatalf("migrateLegacySessionsBaseDir failed: %v", err)
	}
	if _, err := os.Stat(legacySessionFile); !os.IsNotExist(err) {
		t.Fatalf("legacy session file should be removed, err=%v", err)
	}
	migratedSessionFile := filepath.Join(canonicalBaseDir, "sid-001", "session.json")
	data, err := os.ReadFile(migratedSessionFile)
	if err != nil {
		t.Fatalf("read migrated session failed: %v", err)
	}
	if string(data) != string(legacyContent) {
		t.Fatalf("migrated session content mismatch: got=%q want=%q", string(data), string(legacyContent))
	}
	_ = os.RemoveAll(filepath.Dir(canonicalBaseDir))
}
