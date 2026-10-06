package runtimepersist

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMemoryCheckpointStoreRevisionMismatch(t *testing.T) {
	revision := "rev-a"
	store := NewMemoryCheckpointStore(func() StoreMetadata {
		return StoreMetadata{RuntimeRevision: revision, ActiveConfig: "default"}
	})

	if err := store.Set(context.Background(), "cp-1", []byte("payload")); err != nil {
		t.Fatalf("set checkpoint failed: %v", err)
	}

	revision = "rev-b"
	if _, _, err := store.Get(context.Background(), "cp-1"); err == nil {
		t.Fatal("expected revision mismatch error")
	}
}

func TestSQLiteCheckpointStoreRoundTrip(t *testing.T) {
	revision := "rev-a"
	store, err := NewSQLiteCheckpointStore(filepath.Join(t.TempDir(), "checkpoint.db"), func() StoreMetadata {
		return StoreMetadata{RuntimeRevision: revision, ActiveConfig: "default"}
	})
	if err != nil {
		t.Fatalf("create sqlite checkpoint store failed: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	if err := store.Set(context.Background(), "cp-1", []byte("payload")); err != nil {
		t.Fatalf("set checkpoint failed: %v", err)
	}

	got, ok, err := store.Get(context.Background(), "cp-1")
	if err != nil {
		t.Fatalf("get checkpoint failed: %v", err)
	}
	if !ok {
		t.Fatal("expected checkpoint to exist")
	}
	if string(got) != "payload" {
		t.Fatalf("unexpected payload: %s", string(got))
	}

	if err := store.Delete(context.Background(), "cp-1"); err != nil {
		t.Fatalf("delete checkpoint failed: %v", err)
	}
	if _, ok, err := store.Get(context.Background(), "cp-1"); err != nil || ok {
		t.Fatalf("expected checkpoint deleted, ok=%v err=%v", ok, err)
	}
}

func TestResolveCheckpointStorePathUsesPluginDataRuntimeDir(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "autocarto")
	configDir := filepath.Join(pluginRoot, "config")
	want := filepath.Join(pluginRoot, "data", "runtime", "checkpoint_store.db")

	if got := ResolveCheckpointStorePath(configDir); got != want {
		t.Fatalf("ResolveCheckpointStorePath() = %q, want %q", got, want)
	}
}
