package websocket

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func prepareDeletionSession(t *testing.T, server *DeepServer, sessionID string) (string, string) {
	t.Helper()
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	projectPath := filepath.Join(t.TempDir(), "test.qgz")
	if err := server.SaveSession(map[string]any{"session_id": sessionID, "messages": []any{}}, projectPath); err != nil {
		t.Fatal(err)
	}
	sessionDir, err := server.getSessionDir(projectPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	return projectPath, sessionDir
}

func TestDeleteSessionWaitsForDisconnectedWorkersAndPreservesExternalFiles(t *testing.T) {
	server := NewDeepServer(0, nil)
	const sid = "delete-running-session"
	projectPath, sessionDir := prepareDeletionSession(t, server, sid)
	externalPath := filepath.Join(filepath.Dir(projectPath), "external-image.tif")
	if err := os.WriteFile(externalPath, []byte("shared project data"), 0600); err != nil {
		t.Fatal(err)
	}

	// A replacement runner may start before the preceding runner has returned.
	// Deletion must join both, even when there are no connected clients.
	workerStarted := make(chan struct{}, 2)
	workerCancelled := make(chan struct{}, 2)
	releaseWorkers := make(chan struct{})
	workerErrors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		if !server.startSessionTask(ctx, nil, sid, cancel, func() {
			workerStarted <- struct{}{}
			<-ctx.Done()
			workerCancelled <- struct{}{}
			<-releaseWorkers
			// Model a downloader finishing its cancellation cleanup. This write
			// must happen before removal, never resurrect a deleted directory.
			workerErrors <- os.WriteFile(filepath.Join(sessionDir, "output", "last-write"), []byte("done"), 0600)
		}) {
			t.Fatal("failed to start test worker")
		}
	}
	<-workerStarted
	<-workerStarted

	deleted := make(chan error, 1)
	go func() { deleted <- server.DeleteSession(sid, projectPath) }()
	for i := 0; i < 2; i++ {
		select {
		case <-workerCancelled:
		case <-time.After(2 * time.Second):
			close(releaseWorkers)
			t.Fatal("disconnected session worker was not cancelled")
		}
	}
	select {
	case err := <-deleted:
		close(releaseWorkers)
		t.Fatalf("deletion returned before workers stopped: %v", err)
	default:
	}
	if _, err := os.Stat(sessionDir); err != nil {
		t.Fatalf("session files removed while worker was still finishing: %v", err)
	}
	if err := server.SaveSession(map[string]any{"session_id": sid}, projectPath); err == nil {
		t.Fatal("delayed save accepted during deletion")
	}
	if _, err := server.InitSessionDir(projectPath, sid); err == nil {
		t.Fatal("directory initialization accepted during deletion")
	}
	ctx, cancel := context.WithCancel(context.Background())
	if server.startSessionTask(ctx, nil, sid, cancel, func() { t.Error("deleted session runner executed") }) {
		t.Fatal("new runner accepted during deletion")
	}
	if ctx.Err() == nil {
		t.Fatal("rejected runner context was not cancelled")
	}
	close(releaseWorkers)
	select {
	case err := <-deleted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("deletion did not finish after workers stopped")
	}
	for i := 0; i < 2; i++ {
		if err := <-workerErrors; err != nil {
			t.Fatalf("worker file was removed prematurely: %v", err)
		}
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("session directory remains: %v", err)
	}
	if err := server.SaveSession(map[string]any{"session_id": sid}, projectPath); err == nil {
		t.Fatal("late client save resurrected deleted conversation")
	}
	if content, err := os.ReadFile(externalPath); err != nil || string(content) != "shared project data" {
		t.Fatalf("external project output was changed: %q, %v", content, err)
	}
}

func TestDeleteInterruptedSessionDoesNotReenterFileLock(t *testing.T) {
	server := NewDeepServer(0, nil)
	const sid = "delete-paused-session"
	projectPath, sessionDir := prepareDeletionSession(t, server, sid)
	server.markSessionRuntimeRunning(sid, "run-paused", executionModeBuild, "building", projectPath, "")
	server.markSessionRuntimeInterrupted(sid, "checkpoint-paused")
	done := make(chan error, 1)
	go func() { done <- server.DeleteSession(sid, projectPath) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("paused cancellation deadlocked with session persistence")
	}
	if server.hasActiveSessionRuntime(sid) {
		t.Fatal("paused runtime remains active")
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("paused session directory remains: %v", err)
	}
}

func TestCancelSessionTasksForSessionWithoutClients(t *testing.T) {
	server := NewDeepServer(0, nil)
	ctx, cancel := context.WithCancel(context.Background())
	server.setSessionTaskCancel(nil, "disconnected-session", cancel)
	server.cancelSessionTasksForSession("disconnected-session")
	if ctx.Err() != context.Canceled {
		t.Fatal("global session cancellation depended on a connected client")
	}
}

func TestWaitSessionWorkersTimesOutWithoutPretendingCompletion(t *testing.T) {
	worker := &sessionTaskWorker{done: make(chan struct{})}
	if err := waitSessionWorkers([]*sessionTaskWorker{worker}, time.Millisecond); err == nil {
		t.Fatal("unfinished worker was treated as complete")
	}
	close(worker.done)
	if err := waitSessionWorkers([]*sessionTaskWorker{worker}, time.Second); err != nil {
		t.Fatal(err)
	}
}
