package websocket

import (
	"bufio"
	"context"
	"encoding/json"
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

func TestCanonicalSessionOutputRelPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		changed  bool
	}{
		{
			name:     "exports_autoc_exports",
			input:    filepath.Join("exports", "autoc", "exports", "a.gpkg"),
			expected: filepath.Join("exports", "a.gpkg"),
			changed:  true,
		},
		{
			name:     "exports_autoc_temp",
			input:    filepath.Join("exports", "autoc", "temp", "a.gpkg"),
			expected: filepath.Join("temp", "a.gpkg"),
			changed:  true,
		},
		{
			name:     "exports_temp",
			input:    filepath.Join("exports", "temp", "a.gpkg"),
			expected: filepath.Join("temp", "a.gpkg"),
			changed:  true,
		},
		{
			name:     "autoc_exports",
			input:    filepath.Join("autoc", "exports", "a.gpkg"),
			expected: filepath.Join("exports", "a.gpkg"),
			changed:  true,
		},
		{
			name:    "already_normal",
			input:   filepath.Join("exports", "a.gpkg"),
			changed: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := canonicalSessionOutputRelPath(tc.input)
			if changed != tc.changed {
				t.Fatalf("changed mismatch: got=%v want=%v", changed, tc.changed)
			}
			if got != tc.expected {
				t.Fatalf("target mismatch: got=%q want=%q", got, tc.expected)
			}
		})
	}
}

func TestRepairSessionOutputLayoutMovesAndRewritesSessionJSON(t *testing.T) {
	sessionDir := t.TempDir()
	server := &DeepServer{}

	oldNestedExport := filepath.Join(sessionDir, "exports", "autoc", "exports", "china.gpkg")
	oldTempInExport := filepath.Join(sessionDir, "exports", "temp", "report.csv")
	conflictPath := filepath.Join(sessionDir, "exports", "china.gpkg")

	mustWriteTestFile(t, oldNestedExport, "nested-gpkg")
	mustWriteTestFile(t, oldTempInExport, "csv-content")
	mustWriteTestFile(t, conflictPath, "conflict")

	payload := map[string]any{
		"messages": []any{
			map[string]any{
				"content": oldNestedExport + "|layername=test",
			},
		},
		"tool_calls": []any{
			map[string]any{
				"result": map[string]any{
					"output": oldNestedExport,
					"csv":    oldTempInExport,
				},
			},
		},
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}
	mustWriteTestFile(t, filepath.Join(sessionDir, "session.json"), string(raw))

	if err := server.repairSessionOutputLayout(sessionDir); err != nil {
		t.Fatalf("repairSessionOutputLayout failed: %v", err)
	}

	if _, err := os.Stat(oldNestedExport); !os.IsNotExist(err) {
		t.Fatalf("old nested export should be moved, stat err=%v", err)
	}
	if _, err := os.Stat(oldTempInExport); !os.IsNotExist(err) {
		t.Fatalf("old exports/temp file should be moved, stat err=%v", err)
	}

	renamedExports, err := filepath.Glob(filepath.Join(sessionDir, "exports", "china_migrated_*.gpkg"))
	if err != nil {
		t.Fatalf("glob renamed exports failed: %v", err)
	}
	if len(renamedExports) != 1 {
		t.Fatalf("expected one renamed migrated gpkg, got=%d, files=%v", len(renamedExports), renamedExports)
	}
	movedTemp := filepath.Join(sessionDir, "temp", "report.csv")
	if _, err := os.Stat(movedTemp); err != nil {
		t.Fatalf("moved temp file missing: %v", err)
	}

	if _, err := os.Stat(filepath.Join(sessionDir, "exports", "autoc")); !os.IsNotExist(err) {
		t.Fatalf("nested exports/autoc dir should be cleaned, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "exports", "temp")); !os.IsNotExist(err) {
		t.Fatalf("nested exports/temp dir should be cleaned, stat err=%v", err)
	}

	updatedSessionRaw, err := os.ReadFile(filepath.Join(sessionDir, "session.json"))
	if err != nil {
		t.Fatalf("read updated session.json failed: %v", err)
	}
	updated := string(updatedSessionRaw)
	if strings.Contains(updated, oldNestedExport) {
		t.Fatalf("session.json still contains old nested export path")
	}
	if strings.Contains(updated, oldTempInExport) {
		t.Fatalf("session.json still contains old exports/temp path")
	}
	if !strings.Contains(updated, renamedExports[0]) {
		t.Fatalf("session.json should contain migrated export path: %s", renamedExports[0])
	}
	if !strings.Contains(updated, movedTemp) {
		t.Fatalf("session.json should contain migrated temp path: %s", movedTemp)
	}

	// 第二次执行应保持幂等，不再生成额外 *_migrated_N 文件
	if err := server.repairSessionOutputLayout(sessionDir); err != nil {
		t.Fatalf("second repairSessionOutputLayout failed: %v", err)
	}
	renamedExports2, err := filepath.Glob(filepath.Join(sessionDir, "exports", "china_migrated_*.gpkg"))
	if err != nil {
		t.Fatalf("glob after second repair failed: %v", err)
	}
	if len(renamedExports2) != 1 {
		t.Fatalf("second repair should remain idempotent, got=%d files=%v", len(renamedExports2), renamedExports2)
	}
}

func TestRepairSessionOutputLayoutPreservesIntegrationWorkspaces(t *testing.T) {
	sessionDir := t.TempDir()
	server := &DeepServer{}
	root := filepath.Join(sessionDir, "output", "integrations")
	workspaces := []string{
		root,
		filepath.Join(root, "browser"),
		filepath.Join(root, "browser", "pending-download"),
		filepath.Join(root, "gee", "run-pending"),
		filepath.Join(root, "downloads", "download-pending"),
	}
	for _, dir := range workspaces {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Initialization and UI session reads invoke this repair while a browser or
	// a download may have created its directory but not yet written any files.
	for attempt := 0; attempt < 3; attempt++ {
		if err := server.repairSessionOutputLayout(sessionDir); err != nil {
			t.Fatal(err)
		}
		for _, dir := range workspaces {
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				t.Fatalf("session repair removed an integration working directory: %s (%v)", dir, err)
			}
		}
	}

	// Paths inside managed output belong to the runtime, even if their names
	// happen to resemble one of the old session-layout patterns.
	managedFile := filepath.Join(root, "browser", "sessions", "capture", "output", "page.txt")
	mustWriteTestFile(t, managedFile, "browser artifact")
	legacyFile := filepath.Join(sessionDir, "exports", "autoc", "exports", "legacy.txt")
	mustWriteTestFile(t, legacyFile, "legacy artifact")
	if err := server.repairSessionOutputLayout(sessionDir); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(managedFile); err != nil || string(got) != "browser artifact" {
		t.Fatalf("session repair moved a runtime-owned artifact: %q (%v)", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(sessionDir, "exports", "legacy.txt")); err != nil || string(got) != "legacy artifact" {
		t.Fatalf("legacy migration outside integration workspaces stopped working: %q (%v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "exports", "autoc")); !os.IsNotExist(err) {
		t.Fatalf("empty legacy directory should still be removed: %v", err)
	}
}

func TestRepairSessionOutputLayoutKeepsChildWorkingDirectory(t *testing.T) {
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
	sessionDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workingDir := filepath.Join(sessionDir, "output", "integrations", "browser")
	if err := os.MkdirAll(workingDir, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRepairSessionOutputLayoutKeepsChildWorkingDirectory$")
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
	server := &DeepServer{}
	if err := server.repairSessionOutputLayout(sessionDir); err != nil {
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
