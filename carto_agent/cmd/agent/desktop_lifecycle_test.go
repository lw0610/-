package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestDesktopParentWatchStandaloneDoesNotMonitor(t *testing.T) {
	ch, err := watchDesktopParentWithProbe(context.Background(), "", time.Millisecond, func(int) (func() bool, func(), error) {
		t.Fatal("standalone mode must not inspect its parent")
		return nil, nil, nil
	})
	if err != nil || ch != nil {
		t.Fatalf("standalone watch = %v, %v", ch, err)
	}
}

func TestDesktopParentWatchRejectsInvalidManagedPID(t *testing.T) {
	for _, pid := range []string{"0", "1", "-1", "not-a-pid"} {
		if _, err := watchDesktopParentWithProbe(context.Background(), pid, time.Millisecond, nil); err == nil {
			t.Errorf("accepted invalid managed PID %q", pid)
		}
	}
}

func TestDesktopParentWatchClosesAfterParentExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var alive atomic.Bool
	alive.Store(true)
	closed := make(chan struct{})
	ch, err := watchDesktopParentWithProbe(ctx, "123", time.Millisecond, func(pid int) (func() bool, func(), error) {
		if pid != 123 {
			t.Fatalf("parent PID = %d", pid)
		}
		return alive.Load, func() { close(closed) }, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
		t.Fatal("live parent triggered shutdown")
	default:
	}
	alive.Store(false)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("parent exit did not trigger shutdown")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("parent probe was not released")
	}
}

func TestDesktopParentWatchDetectsParentAlreadyGone(t *testing.T) {
	ch, err := watchDesktopParentWithProbe(context.Background(), "123", time.Second, func(int) (func() bool, func(), error) {
		return func() bool { return false }, func() {}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	default:
		t.Fatal("already missing parent must be reported before startup")
	}
}

func TestDesktopParentWatchCancellationReleasesProbeWithoutReportingParentDeath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	closed := make(chan struct{})
	ch, err := watchDesktopParentWithProbe(ctx, "123", time.Hour, func(int) (func() bool, func(), error) {
		return func() bool { return true }, func() { close(closed) }, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("probe leaked on normal shutdown")
	}
	select {
	case <-ch:
		t.Fatal("normal shutdown was reported as parent death")
	default:
	}
}

func TestRemoveOwnedRuntimePortFile(t *testing.T) {
	for _, test := range []struct {
		name, content string
		removed       bool
	}{
		{"own", "50010\n", true},
		{"newer", "50011", false},
		{"invalid", "not-a-port", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".port")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := removeOwnedRuntimePortFile(path, 50010); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if test.removed {
				if !os.IsNotExist(err) {
					t.Fatalf("own file was not removed: %v", err)
				}
			} else if err != nil || string(got) != test.content {
				t.Fatalf("changed another backend's file: %q, %v", got, err)
			}
		})
	}
	if err := removeOwnedRuntimePortFile(filepath.Join(t.TempDir(), "missing"), 50010); err != nil {
		t.Fatal(err)
	}
}
