package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveSessionDirUsesCanonicalRuntimeOwner(t *testing.T) {
	runtimeHome := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", runtimeHome)
	projects := t.TempDir()
	project := filepath.Join(projects, "maps", "Test1.qgz")
	got, err := ResolveSessionDir(project, "browser-session")
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(runtimeHome, "data", "sessions")
	rel, err := filepath.Rel(wantRoot, got)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		t.Fatalf("session not owned by runtime: %q, %v", got, err)
	}
	if filepath.Base(got) != "browser-session" || !strings.HasPrefix(filepath.Base(filepath.Dir(got)), "Test1-") {
		t.Fatalf("unexpected canonical session layout: %q", got)
	}
	otherProject := filepath.Join(projects, "other", "Test1.qgz")
	other, err := ResolveSessionDir(otherProject, "browser-session")
	if err != nil || got == other {
		t.Fatalf("projects with the same basename share storage: %q, %q, %v", got, other, err)
	}
	normalized, err := ResolveSessionDir("  "+filepath.Join(projects, "maps")+string(filepath.Separator)+"."+string(filepath.Separator)+"Test1.qgz  ", " browser-session ")
	if err != nil || normalized != got {
		t.Fatalf("equivalent identity did not normalize: %q != %q, %v", normalized, got, err)
	}
	// Path resolution must not create a legacy migration source or any files.
	for _, dir := range []string{filepath.Join(projects, "maps", "autoc"), filepath.Join(runtimeHome, "data")} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("resolver modified filesystem: %q, %v", dir, err)
		}
	}
}

func TestResolveSessionDirPreservesExistingStorageKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("existing POSIX path fixture")
	}
	runtimeHome := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", runtimeHome)
	got, err := ResolveSessionDir("/projects/Test1.qgz", "existing")
	want := filepath.Join(runtimeHome, "data", "sessions", "Test1-5f310a7c03e1ffa5", "existing")
	if err != nil || got != want {
		t.Fatalf("storage key changed: got %q, want %q, err %v", got, want, err)
	}
}

func TestResolveSessionDirRejectsInvalidIdentity(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "project.qgz")
	for _, sessionID := range []string{"", " ", "../escape", "a/b", `a\b`, "a..b", ".hidden", "a b", "a\x00b", strings.Repeat("a", 129)} {
		if _, err := ResolveSessionDir(project, sessionID); err == nil {
			t.Errorf("accepted invalid session ID %q", sessionID)
		}
	}
	for _, projectPath := range []string{"", " ", "relative.qgz", filepath.VolumeName(project) + string(filepath.Separator), project + "\x00"} {
		if _, err := ResolveSessionDir(projectPath, "test"); err == nil {
			t.Errorf("accepted invalid project path %q", projectPath)
		}
	}
}

func TestResolveSessionDirPreservesRelativeRuntimeHome(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project.qgz")
	relativeRuntime := filepath.Join("relative-runtime", "nested")
	t.Setenv("AUTOCARTO_HOME", relativeRuntime)
	got, err := ResolveSessionDir(project, "test")
	if err != nil {
		t.Fatal(err)
	}
	absRuntime, err := ResolveProjectPath(relativeRuntime)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOCARTO_HOME", absRuntime)
	want, err := ResolveSessionDir(project, "test")
	if err != nil || got != want || !filepath.IsAbs(got) {
		t.Fatalf("relative runtime home changed location: got %q, want %q, err %v", got, want, err)
	}
}
