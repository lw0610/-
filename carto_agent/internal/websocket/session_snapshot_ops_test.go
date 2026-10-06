package websocket

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir failed for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write failed for %s: %v", path, err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed for %s: %v", path, err)
	}
	return string(data)
}

func assertDirEmpty(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read dir failed for %s: %v", path, err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected dir empty: %s, entries=%d", path, len(entries))
	}
}

func TestSaveSessionCreatesSnapshotBackupAndCleansOrphans(t *testing.T) {
	server := &DeepServer{}
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "session-backup"

	base := map[string]any{
		"session_id": sessionID,
		"messages":   []any{},
		"tool_calls": []any{},
		"snapshots":  []any{},
	}
	if err := server.saveSessionToFile(base, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("initial saveSessionToFile failed: %v", err)
	}

	sessionBaseDir, err := server.getSessionsBaseDir(projectPath)
	if err != nil {
		t.Fatalf("getSessionsBaseDir failed: %v", err)
	}
	sessionDir := filepath.Join(sessionBaseDir, sessionID)
	mustWriteFile(t, filepath.Join(sessionDir, "exports", "result.gpkg"), "export-data")
	mustWriteFile(t, filepath.Join(sessionDir, "output", "notes.txt"), "artifact-data")
	mustWriteFile(t, filepath.Join(sessionDir, "logs", "session.log"), "log-data")
	mustWriteFile(t, filepath.Join(sessionDir, "snapshots", "orphan", "state", "old.txt"), "stale")

	withSnapshot := map[string]any{
		"session_id": sessionID,
		"messages":   []any{},
		"tool_calls": []any{},
		"snapshots": []any{
			map[string]any{
				"snapshot_id": "snap-1",
				"created_at":  "2026-02-16T00:00:00Z",
			},
		},
	}
	if err := server.saveSessionToFile(withSnapshot, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile with snapshot failed: %v", err)
	}

	snapshotState := filepath.Join(sessionDir, "snapshots", "snap-1", "state")
	if _, err := os.Stat(filepath.Join(snapshotState, "exports", "result.gpkg")); err != nil {
		t.Fatalf("snapshot exports file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snapshotState, "output", "notes.txt")); err != nil {
		t.Fatalf("snapshot output file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snapshotState, "session.json")); !os.IsNotExist(err) {
		t.Fatalf("session.json should not be copied into snapshot state, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(snapshotState, runtimeSessionSnapshotFileName)); !os.IsNotExist(err) {
		t.Fatalf("runtime snapshot should not be copied into snapshot state, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(snapshotState, "snapshots")); !os.IsNotExist(err) {
		t.Fatalf("snapshots dir should not be copied recursively, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "snapshots", "snap-1", "manifest.json")); err != nil {
		t.Fatalf("snapshot manifest missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "snapshots", "orphan")); !os.IsNotExist(err) {
		t.Fatalf("orphan snapshot dir should be removed, err=%v", err)
	}
}

func TestRestoreSnapshotStrictRollsBackRuntimeFiles(t *testing.T) {
	server := &DeepServer{}
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "session-restore"

	base := map[string]any{
		"session_id": sessionID,
		"messages":   []any{},
		"tool_calls": []any{},
		"snapshots":  []any{},
	}
	if err := server.saveSessionToFile(base, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("initial saveSessionToFile failed: %v", err)
	}

	sessionBaseDir, err := server.getSessionsBaseDir(projectPath)
	if err != nil {
		t.Fatalf("getSessionsBaseDir failed: %v", err)
	}
	sessionDir := filepath.Join(sessionBaseDir, sessionID)
	mustWriteFile(t, filepath.Join(sessionDir, "exports", "data.txt"), "v1")
	mustWriteFile(t, filepath.Join(sessionDir, "temp", "cache.tmp"), "cache-v1")
	mustWriteFile(t, filepath.Join(sessionDir, "logs", "session.log"), "log-v1")

	withSnapshot := map[string]any{
		"session_id": sessionID,
		"messages":   []any{},
		"tool_calls": []any{},
		"snapshots": []any{
			map[string]any{"snapshot_id": "snap-restore", "created_at": "2026-02-16T01:00:00Z"},
		},
	}
	if err := server.saveSessionToFile(withSnapshot, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile with snapshot failed: %v", err)
	}

	mustWriteFile(t, filepath.Join(sessionDir, "exports", "data.txt"), "v2")
	mustWriteFile(t, filepath.Join(sessionDir, "exports", "new.txt"), "new-file")
	mustWriteFile(t, filepath.Join(sessionDir, "temp", "new.tmp"), "new-temp")
	mustWriteFile(t, filepath.Join(sessionDir, "output", "extra.txt"), "artifact")
	mustWriteFile(t, filepath.Join(sessionDir, "logs", "session.log"), "log-v2")
	mustWriteFile(t, filepath.Join(sessionDir, "logs", "old.log"), "old-log")

	op := sessionSaveOperation{Type: sessionSaveOpRestoreSnapshot, SnapshotID: "snap-restore", Mode: sessionRestoreModeStrict}
	if err := server.saveSessionToFile(withSnapshot, projectPath, op); err != nil {
		t.Fatalf("restore saveSessionToFile failed: %v", err)
	}

	if got := mustReadFile(t, filepath.Join(sessionDir, "exports", "data.txt")); got != "v1" {
		t.Fatalf("exports/data.txt mismatch after restore: got=%q want=%q", got, "v1")
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "exports", "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("exports/new.txt should be removed by strict restore, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "temp", "new.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp/new.tmp should be removed by strict restore, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "output", "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("output/extra.txt should be removed by strict restore, err=%v", err)
	}
	if got := mustReadFile(t, filepath.Join(sessionDir, "logs", "session.log")); got != "log-v1" {
		t.Fatalf("logs/session.log mismatch after restore: got=%q want=%q", got, "log-v1")
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "logs", "old.log")); !os.IsNotExist(err) {
		t.Fatalf("logs/old.log should be removed by strict restore, err=%v", err)
	}
}

func TestClearHistoryCleansRuntimeFilesAndKeepsSnapshots(t *testing.T) {
	server := &DeepServer{}
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "session-clear"

	base := map[string]any{
		"session_id": sessionID,
		"messages":   []any{},
		"tool_calls": []any{},
		"snapshots": []any{
			map[string]any{"snapshot_id": "snap-clear", "created_at": "2026-02-16T02:00:00Z"},
		},
	}
	if err := server.saveSessionToFile(base, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("initial saveSessionToFile failed: %v", err)
	}

	sessionBaseDir, err := server.getSessionsBaseDir(projectPath)
	if err != nil {
		t.Fatalf("getSessionsBaseDir failed: %v", err)
	}
	sessionDir := filepath.Join(sessionBaseDir, sessionID)
	mustWriteFile(t, filepath.Join(sessionDir, "exports", "final.txt"), "export")
	mustWriteFile(t, filepath.Join(sessionDir, "temp", "work.tmp"), "temp")
	mustWriteFile(t, filepath.Join(sessionDir, "output", "artifact.txt"), "artifact")
	mustWriteFile(t, filepath.Join(sessionDir, "logs", "session.log"), "live-log")
	mustWriteFile(t, filepath.Join(sessionDir, "logs", "other.log"), "other-log")
	mustWriteFile(t, filepath.Join(sessionDir, "misc.bin"), "misc")

	op := sessionSaveOperation{Type: sessionSaveOpClearHistory}
	if err := server.saveSessionToFile(base, projectPath, op); err != nil {
		t.Fatalf("clear_history saveSessionToFile failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(sessionDir, "session.json")); err != nil {
		t.Fatalf("session.json should exist after clear_history: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "snapshots", "snap-clear", "state")); err != nil {
		t.Fatalf("snapshot backup should be kept after clear_history: %v", err)
	}
	assertDirEmpty(t, filepath.Join(sessionDir, "exports"))
	assertDirEmpty(t, filepath.Join(sessionDir, "temp"))
	assertDirEmpty(t, filepath.Join(sessionDir, "output"))
	if _, err := os.Stat(filepath.Join(sessionDir, "logs", "other.log")); !os.IsNotExist(err) {
		t.Fatalf("logs/other.log should be removed after clear_history, err=%v", err)
	}
	logInfo, err := os.Stat(filepath.Join(sessionDir, "logs", "session.log"))
	if err != nil {
		t.Fatalf("logs/session.log should exist after clear_history: %v", err)
	}
	if logInfo.Size() != 0 {
		t.Fatalf("logs/session.log should be truncated after clear_history: size=%d", logInfo.Size())
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "misc.bin")); !os.IsNotExist(err) {
		t.Fatalf("misc.bin should be removed after clear_history, err=%v", err)
	}
}
