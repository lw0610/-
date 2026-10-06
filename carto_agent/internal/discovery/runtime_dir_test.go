package discovery

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstanceRegistry_UsesAppLocalDataDir(t *testing.T) {
	runtimeHome := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", runtimeHome)

	originalArg0 := os.Args[0]
	os.Args[0] = "agent"
	defer func() {
		os.Args[0] = originalArg0
	}()

	registry := NewInstanceRegistry()
	defer registry.Close()

	want := filepath.Join(runtimeHome, "data", "instances.db")
	if registry.dbPath != want {
		t.Fatalf("registry.dbPath = %q, want %q", registry.dbPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected persisted registry db at %s: %v", want, err)
	}
}

func TestInstanceRegistry_LoadsPersistedInstancesAfterRestart(t *testing.T) {
	runtimeHome := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", runtimeHome)

	originalArg0 := os.Args[0]
	os.Args[0] = "agent"
	defer func() {
		os.Args[0] = originalArg0
	}()

	writer := NewInstanceRegistry()
	if err := writer.Register(RegisterRequest{
		InstanceID:  "persisted-restart-case",
		PID:         os.Getpid(),
		QGISVersion: "3.34",
		ProjectPath: "/tmp/demo.qgz",
		Source:      "reverse_ws",
		StartTime:   time.Now().Format(time.RFC3339),
	}); err != nil {
		writer.Close()
		t.Fatalf("register instance failed: %v", err)
	}
	writer.Close()

	reader := NewInstanceRegistry()
	defer reader.Close()

	inst := reader.Get("persisted-restart-case")
	if inst == nil {
		t.Fatal("expected persisted instance after restart")
	}
	if inst.ProjectPath != "/tmp/demo.qgz" {
		t.Fatalf("unexpected project path after restart: %q", inst.ProjectPath)
	}
	if reader.dbPath != filepath.Join(runtimeHome, "data", "instances.db") {
		t.Fatalf("reader.dbPath = %q", reader.dbPath)
	}
}
