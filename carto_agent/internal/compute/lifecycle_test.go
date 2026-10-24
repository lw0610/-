package compute

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveComputeWorkingDirSupportsDirectorySeed(t *testing.T) {
	root := t.TempDir()
	got := resolveComputeWorkingDir(root)
	if got != root {
		t.Fatalf("working dir mismatch, got=%q want=%q", got, root)
	}
}

func TestResolveComputeWorkingDirSupportsFileSeed(t *testing.T) {
	root := t.TempDir()
	seedFile := filepath.Join(root, "autocarto", "autocarto_plugin.py")
	if err := os.MkdirAll(filepath.Dir(seedFile), 0o755); err != nil {
		t.Fatalf("mkdir seed file dir failed: %v", err)
	}
	if err := os.WriteFile(seedFile, []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write seed file failed: %v", err)
	}

	got := resolveComputeWorkingDir(seedFile)
	if got != filepath.Dir(seedFile) {
		t.Fatalf("working dir mismatch, got=%q want=%q", got, filepath.Dir(seedFile))
	}
}

func TestResolveComputeWorkingDirFallsBackToCurrentWorkingDirectory(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalWD)
	}()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	if got := resolveComputeWorkingDir(filepath.Join(root, "missing")); got != root {
		t.Fatalf("working dir mismatch, got=%q want=%q", got, root)
	}
}

func TestInferQGISRuntimeDirsFromPython(t *testing.T) {
	root := t.TempDir()
	pythonPath := filepath.Join(root, "QGIS.app", "Contents", "MacOS", "bin", "python3.9")
	resourcesDir := filepath.Join(root, "QGIS.app", "Contents", "Resources")
	if err := os.MkdirAll(filepath.Dir(pythonPath), 0o755); err != nil {
		t.Fatalf("mkdir python dir failed: %v", err)
	}
	if err := os.MkdirAll(resourcesDir, 0o755); err != nil {
		t.Fatalf("mkdir resources dir failed: %v", err)
	}
	if err := os.WriteFile(pythonPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write python path failed: %v", err)
	}

	prefixPath, detectedResources := inferQGISRuntimeDirsFromPython(pythonPath)
	if prefixPath != filepath.Join(root, "QGIS.app", "Contents", "MacOS") {
		t.Fatalf("prefix path mismatch, got=%q", prefixPath)
	}
	if detectedResources != resourcesDir {
		t.Fatalf("resources dir mismatch, got=%q want=%q", detectedResources, resourcesDir)
	}
}

func TestDeriveHeadlessRuntimeEnvDarwin(t *testing.T) {
	env := deriveHeadlessRuntimeEnv("darwin")
	if got := env["QT_MAC_DISABLE_FOREGROUND_APPLICATION_TRANSFORM"]; got != "1" {
		t.Fatalf("unexpected QT_MAC_DISABLE_FOREGROUND_APPLICATION_TRANSFORM=%q", got)
	}
}

func TestDeriveHeadlessRuntimeEnvNonDarwin(t *testing.T) {
	env := deriveHeadlessRuntimeEnv("linux")
	if len(env) != 0 {
		t.Fatalf("expected empty env for linux, got=%v", env)
	}
}

func TestDeriveQGISEnvFromPython(t *testing.T) {
	root := t.TempDir()
	pythonPath := filepath.Join(root, "QGIS.app", "Contents", "MacOS", "bin", "python3.9")
	resourcesDir := filepath.Join(root, "QGIS.app", "Contents", "Resources")
	if err := os.MkdirAll(filepath.Dir(pythonPath), 0o755); err != nil {
		t.Fatalf("mkdir python dir failed: %v", err)
	}
	for _, dir := range []string{
		filepath.Join(resourcesDir, "proj"),
		filepath.Join(resourcesDir, "gdal"),
		filepath.Join(resourcesDir, "python"),
		filepath.Join(resourcesDir, "python", "plugins"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(resourcesDir, "proj", "proj.db"), []byte("db"), 0o644); err != nil {
		t.Fatalf("write proj.db failed: %v", err)
	}
	if err := os.WriteFile(pythonPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write python path failed: %v", err)
	}

	env := deriveQGISEnvFromPython(pythonPath)
	if got := env["QGIS_PREFIX_PATH"]; got == "" {
		t.Fatal("expected QGIS_PREFIX_PATH")
	}
	if got := env["AUTOCARTO_QGIS_PREFIX_PATH"]; got == "" {
		t.Fatal("expected AUTOCARTO_QGIS_PREFIX_PATH")
	}
	if got := env["PROJ_LIB"]; got != filepath.Join(resourcesDir, "proj") {
		t.Fatalf("unexpected PROJ_LIB=%q", got)
	}
	if got := env["GDAL_DATA"]; got != filepath.Join(resourcesDir, "gdal") {
		t.Fatalf("unexpected GDAL_DATA=%q", got)
	}
	if got := env["PYTHONPATH"]; got == "" || !containsPath(got, filepath.Join(resourcesDir, "python", "plugins")) {
		t.Fatalf("unexpected PYTHONPATH=%q", got)
	}
}

func TestDeriveLinuxQGISEnv(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	if !dirExists("/usr/share/qgis/python/plugins") {
		t.Skip("system qgis plugin path unavailable")
	}
	env := deriveLinuxQGISEnv()
	if got := env["PYTHONPATH"]; got == "" || !containsPath(got, "/usr/share/qgis/python/plugins") {
		t.Fatalf("unexpected linux PYTHONPATH=%q", got)
	}
}

func containsPath(pathList string, target string) bool {
	for _, item := range filepath.SplitList(pathList) {
		if item == target {
			return true
		}
	}
	return false
}
