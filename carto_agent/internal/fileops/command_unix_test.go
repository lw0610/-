//go:build !windows

package fileops

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestShellCancellationTerminatesChildProcessGroup(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	w := testWorkspace(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan map[string]any, 1)
	errors := make(chan error, 1)
	go func() {
		value, err := w.run(ctx, map[string]any{"command": "sleep 30 & child=$!; printf '%s' \"$child\" > child.pid; wait"})
		if err != nil {
			errors <- err
			return
		}
		finished <- value.(map[string]any)
	}()
	var pid int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(filepath.Join(w.output, "child.pid")); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
			if pid > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid <= 0 {
		t.Fatal("child process never started")
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	cancel()
	select {
	case result := <-finished:
		if result["cancelled"] != true || result["success"] != false {
			t.Fatalf("cancellation not reported: %#v", result)
		}
	case err := <-errors:
		t.Fatal(err)
	case <-time.After(4 * time.Second):
		t.Fatal("cancellation did not return promptly")
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background child survived process-group cancellation")
}
