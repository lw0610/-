package runtimepersist

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/session"
	"github.com/cloudwego/eino/schema"
)

func TestSessionSnapshotRoundTrip(t *testing.T) {
	sess := session.NewSession("sess-1", 20)
	sess.CreatedAt = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	sess.AddMessage(schema.UserMessage("hello"))
	sess.AddMessage(schema.AssistantMessage("world", nil))
	sess.SetPlanContext(&session.PlanContext{RawOutput: "plan"})

	snapshot := CaptureSessionSnapshot(sess, "default", "rev-1")
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}

	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := snapshot.SaveJSON(path); err != nil {
		t.Fatalf("save snapshot failed: %v", err)
	}

	loaded, err := LoadSessionSnapshotJSON(path)
	if err != nil {
		t.Fatalf("load snapshot failed: %v", err)
	}
	restored := loaded.RestoreSession(20)
	if restored == nil {
		t.Fatal("expected restored session")
	}
	if restored.ID != "sess-1" {
		t.Fatalf("unexpected session id: %s", restored.ID)
	}
	if restored.MessageCount() != 2 {
		t.Fatalf("unexpected message count: %d", restored.MessageCount())
	}
	if plan := restored.GetPlanContext(); plan == nil || plan.RawOutput != "plan" {
		t.Fatalf("unexpected restored plan context: %#v", plan)
	}
}

func TestSessionSnapshot_SaveAndRestore(t *testing.T) {
	sess := session.NewSession("sess-save-restore", 20)
	sess.CreatedAt = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	sess.AddMessage(schema.UserMessage("hello"))
	sess.AddMessage(schema.AssistantMessage("world", nil))
	sess.SetPlanContext(&session.PlanContext{RawOutput: "plan"})

	snapshot := CaptureSessionSnapshot(sess, "default", "rev-save")
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	snapshot.RuntimeProjection = &SessionRuntimeProjection{
		RuntimeStatus:    "running",
		BoundInstanceID:  "inst-save",
		BoundProjectPath: "/tmp/save-restore.qgz",
		RuntimeRevision:  "runtime-rev-save",
	}

	path := filepath.Join(t.TempDir(), "snapshot-save-restore.json")
	if err := snapshot.SaveJSON(path); err != nil {
		t.Fatalf("save snapshot failed: %v", err)
	}

	loaded, err := LoadSessionSnapshotJSON(path)
	if err != nil {
		t.Fatalf("load snapshot failed: %v", err)
	}
	if loaded.RuntimeProjection == nil || loaded.RuntimeProjection.BoundInstanceID != "inst-save" {
		t.Fatalf("unexpected restored runtime projection: %#v", loaded.RuntimeProjection)
	}
	if restored := loaded.RestoreSession(20); restored == nil || restored.ID != "sess-save-restore" {
		t.Fatalf("unexpected restored session: %#v", restored)
	}
}

func TestSessionSnapshot_RestoreAfterBackendRestart(t *testing.T) {
	sess := session.NewSession("sess-restart", 20)
	sess.CreatedAt = time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)
	sess.AddMessage(schema.UserMessage("resume after restart"))
	sess.AddMessage(schema.AssistantMessage("ack", nil))

	snapshot := CaptureSessionSnapshot(sess, "default", "rev-before-restart")
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	snapshot.RuntimeProjection = &SessionRuntimeProjection{
		RuntimeStatus:    "interrupted",
		BoundInstanceID:  "inst-restart",
		BoundProjectPath: "/tmp/restart.qgz",
		CheckpointID:     "checkpoint-1",
		RuntimeRevision:  "runtime-rev-restart",
	}

	root := t.TempDir()
	path := filepath.Join(root, "runtime", "session_snapshot.json")
	if err := snapshot.SaveJSON(path); err != nil {
		t.Fatalf("save snapshot failed: %v", err)
	}

	loaded, err := LoadSessionSnapshotJSON(path)
	if err != nil {
		t.Fatalf("load snapshot after restart failed: %v", err)
	}
	if loaded.RuntimeProjection == nil || loaded.RuntimeProjection.CheckpointID != "checkpoint-1" {
		t.Fatalf("unexpected runtime projection after restart: %#v", loaded.RuntimeProjection)
	}
	restored := loaded.RestoreSession(20)
	if restored == nil {
		t.Fatal("expected restored session after backend restart")
	}
	if restored.MessageCount() != 2 {
		t.Fatalf("unexpected message count after restart: %d", restored.MessageCount())
	}
}
