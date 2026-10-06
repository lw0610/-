package websocket

import (
	"path/filepath"
	"testing"
)

func TestApplyMessageLimit(t *testing.T) {
	sessionData := map[string]any{
		"messages": []any{"m1", "m2", "m3", "m4", "m5"},
	}

	trimmed := applyMessageLimit(sessionData, 3)
	if trimmed != 2 {
		t.Fatalf("trimmed messages mismatch, got=%d want=2", trimmed)
	}

	messages, ok := sessionData["messages"].([]any)
	if !ok {
		t.Fatalf("messages type mismatch")
	}
	if len(messages) != 3 {
		t.Fatalf("messages length mismatch, got=%d want=3", len(messages))
	}
	if messages[0] != "m3" || messages[1] != "m4" || messages[2] != "m5" {
		t.Fatalf("messages content mismatch after trim: %#v", messages)
	}
}

func TestEnforceSessionActiveLimit(t *testing.T) {
	server := &DeepServer{}
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")

	first := map[string]any{
		"session_id": "session-1",
		"messages":   []any{},
		"tool_calls": []any{},
		"snapshots":  []any{},
	}
	if err := server.saveSessionToFile(first, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("initial saveSessionToFile failed: %v", err)
	}

	newSessionDir, err := server.getSessionDir(projectPath, "session-2")
	if err != nil {
		t.Fatalf("getSessionDir failed: %v", err)
	}
	if err := server.enforceSessionActiveLimit(projectPath, newSessionDir, 1); err == nil {
		t.Fatalf("enforceSessionActiveLimit should fail when active sessions reach limit")
	}

	existingSessionDir, err := server.getSessionDir(projectPath, "session-1")
	if err != nil {
		t.Fatalf("getSessionDir existing failed: %v", err)
	}
	if err := server.enforceSessionActiveLimit(projectPath, existingSessionDir, 1); err != nil {
		t.Fatalf("existing session update should bypass active limit: %v", err)
	}
}
