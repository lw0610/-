package runtimecontrol

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/skill"
)

func TestHandles(t *testing.T) {
	if !Handles(ToolHITLRequest) || !Handles(ToolOutputGetInfo) || !Handles(ToolReferenceRead) {
		t.Fatal("expected runtime-control tools to be handled")
	}
	if Handles("missing") {
		t.Fatal("did not expect unknown tool to be handled")
	}
}

func TestGetOutputInfoUsesSessionDirFallback(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "session-1")
	outputDir := filepath.Join(sessionDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "a.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Invoke(context.Background(), ToolOutputGetInfo, map[string]any{"session_dir": sessionDir})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	payload := result.(map[string]any)
	if success, _ := payload["success"].(bool); !success {
		t.Fatalf("expected success payload: %#v", payload)
	}
	if payload["output"] != outputDir {
		t.Fatalf("unexpected output dir: %#v", payload["output"])
	}
	info := payload["info"].(map[string]any)
	if info["files_count"] != 1 {
		t.Fatalf("unexpected files count: %#v", info)
	}
}

func TestGetOutputInfoUsesTrustedSessionWithoutModelArguments(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "project.qgz")
	session := skill.SessionContext{SessionID: "output-context-test", ProjectPath: project}
	sessionDir, err := config.ResolveSessionDir(project, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(sessionDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "population.csv"), []byte("population\n100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := skill.WithSessionContext(context.Background(), session)
	for _, args := range []map[string]any{
		nil,
		{},
		{"output_dir": t.TempDir(), "session_dir": t.TempDir()},
	} {
		result, err := Invoke(ctx, ToolOutputGetInfo, args)
		if err != nil {
			t.Fatalf("Invoke() error = %v", err)
		}
		payload := result.(map[string]any)
		if payload["success"] != true || payload["output"] != outputDir {
			t.Fatalf("expected trusted canonical session output, got %#v", payload)
		}
		info := payload["info"].(map[string]any)
		files := info["files"].([]map[string]any)
		if info["files_count"] != 1 || len(files) != 1 || files[0]["name"] != "population.csv" {
			t.Fatalf("expected actual session artifact, got %#v", info)
		}
	}
	if _, err := os.Stat(session.SessionDir()); !os.IsNotExist(err) {
		t.Fatalf("legacy project-local session directory must not be created: %v", err)
	}
}

func TestGetOutputInfoRetainsExplicitOutputDirWithoutSession(t *testing.T) {
	outputDir := t.TempDir()
	result, err := Invoke(context.Background(), ToolOutputGetInfo, map[string]any{"output_dir": outputDir})
	if err != nil {
		t.Fatal(err)
	}
	payload := result.(map[string]any)
	if payload["success"] != true || payload["output"] != outputDir {
		t.Fatalf("explicit legacy directory no longer works: %#v", payload)
	}
}

func TestGetOutputInfoDoesNotFallbackFromInvalidTrustedSession(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	ctx := skill.WithSessionContext(context.Background(), skill.SessionContext{
		SessionID: "../invalid", ProjectPath: filepath.Join(t.TempDir(), "project.qgz"),
	})
	_, err := Invoke(ctx, ToolOutputGetInfo, map[string]any{"output_dir": t.TempDir()})
	if err == nil {
		t.Fatal("invalid trusted session should fail instead of reading the argument directory")
	}
}

func TestGetOutputInfoDiscoversDirectArtifactSubdirectories(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	session := skill.SessionContext{
		SessionID: "nested-artifacts", ProjectPath: filepath.Join(t.TempDir(), "project.qgz"),
	}
	sessionDir, err := config.ResolveSessionDir(session.ProjectPath, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(sessionDir, "output")
	for _, relative := range []string{"henan_population/population.csv", "integrations/gee/run-1/image.tif"} {
		path := filepath.Join(outputDir, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test artifact"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Invoke(skill.WithSessionContext(context.Background(), session), ToolOutputGetInfo, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := result.(map[string]any)
	info := payload["info"].(map[string]any)
	if payload["success"] != true || info["exists"] != true || info["files_count"] != 0 || len(info["files"].([]map[string]any)) != 0 {
		t.Fatalf("direct-file semantics changed: %#v", payload)
	}
	subdirs := info["subdirs"].([]map[string]any)
	if info["subdirs_count"] != 2 || len(subdirs) != 2 {
		t.Fatalf("expected directly discoverable artifact directories: %#v", info)
	}
	for index, name := range []string{"henan_population", "integrations"} {
		if subdirs[index]["name"] != name || subdirs[index]["path"] != filepath.Join(outputDir, name) {
			t.Fatalf("unexpected direct subdirectory: %#v", subdirs[index])
		}
	}
}

func TestGetOutputInfoMissingDirectoryKeepsEmptyListingFields(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "does-not-exist")
	result, err := Invoke(context.Background(), ToolOutputGetInfo, map[string]any{"output_dir": outputDir})
	if err != nil {
		t.Fatal(err)
	}
	payload := result.(map[string]any)
	info := payload["info"].(map[string]any)
	if payload["success"] != true || info["exists"] != false || info["files_count"] != 0 || info["subdirs_count"] != 0 {
		t.Fatalf("unexpected missing directory result: %#v", payload)
	}
	if len(info["files"].([]map[string]any)) != 0 || len(info["subdirs"].([]map[string]any)) != 0 {
		t.Fatalf("missing directory must expose empty lists: %#v", info)
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("read-only lookup must not create the directory: %v", err)
	}
}
