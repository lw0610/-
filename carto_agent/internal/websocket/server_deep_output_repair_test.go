package websocket

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustWriteTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir failed for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write failed for %s: %v", path, err)
	}
}

func TestSessionReadsAndInitializationPreserveRuntimeFiles(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	server := NewDeepServer(0, nil)
	project := filepath.Join(t.TempDir(), "map.qgz")
	const sessionID = "pending-download"
	sessionDir, err := server.getSessionDir(project, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(sessionDir, "output")
	workspaces := []string{
		root,
		filepath.Join(root, "osm"),
		filepath.Join(root, "integrations", "browser", "pending-download"),
		filepath.Join(root, "another-worker", "pending"),
	}
	for _, dir := range workspaces {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(sessionDir, "session.json"):                           `{"session_id":"pending-download"}`,
		filepath.Join(root, "sessions", "capture", "output", "page.txt"):    "runtime artifact",
		filepath.Join(sessionDir, "exports", "autoc", "exports", "map.txt"): "existing output",
	}
	for path, content := range files {
		mustWriteTestFile(t, path, content)
	}
	// Empty directories belong to pending work; names that resemble an old
	// output layout do not authorize a session read to relocate the files.
	for _, action := range []struct {
		name string
		run  func() error
	}{
		{"load", func() error { _, err := server.loadSessionFromFile(sessionID, project); return err }},
		{"list", func() error { _, err := server.listSessions(project); return err }},
		{"initialize", func() error { _, err := server.initSessionDir(project, sessionID); return err }},
	} {
		t.Run(action.name, func(t *testing.T) {
			if err := action.run(); err != nil {
				t.Fatal(err)
			}
			for _, dir := range workspaces {
				if info, err := os.Stat(dir); err != nil || !info.IsDir() {
					t.Fatalf("session operation removed a working directory: %s (%v)", dir, err)
				}
			}
			for path, content := range files {
				if got, err := os.ReadFile(path); err != nil || string(got) != content {
					t.Fatalf("session operation changed %s: %q (%v)", path, got, err)
				}
			}
		})
	}
}

func TestSessionRefreshKeepsChildWorkingDirectory(t *testing.T) {
	const childFlag = "AUTOCARTO_TEST_BROWSER_CWD_CHILD"
	if os.Getenv(childFlag) == "1" {
		// Pause between chdir at process creation and the runtime's first cwd
		// lookup, matching the Node/npm startup interval from the failure.
		fmt.Fprintln(os.Stdout, "ready")
		if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(os.Stdout, cwd)
		return
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOCARTO_HOME", home)
	server := NewDeepServer(0, nil)
	project := filepath.Join(t.TempDir(), "map.qgz")
	const sessionID = "working-directory"
	sessionDir, err := server.getSessionDir(project, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteTestFile(t, filepath.Join(sessionDir, "session.json"), `{"session_id":"working-directory"}`)
	workingDir := filepath.Join(sessionDir, "output", "osm")
	if err := os.MkdirAll(workingDir, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSessionRefreshKeepsChildWorkingDirectory$")
	cmd.Dir = workingDir
	cmd.Env = append(os.Environ(), childFlag+"=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	reader := bufio.NewReader(stdout)
	if ready, err := reader.ReadString('\n'); err != nil || strings.TrimSpace(ready) != "ready" {
		t.Fatalf("child did not reach startup barrier: %q (%v)", ready, err)
	}
	if _, err := server.listSessions(project); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(stdin, "continue"); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if cwd, err := reader.ReadString('\n'); err != nil || strings.TrimSpace(cwd) != workingDir {
		t.Fatalf("session refresh invalidated the child's cwd: %q (%v), expected %s", cwd, err, workingDir)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("browser startup child failed: %v", err)
	}
}
