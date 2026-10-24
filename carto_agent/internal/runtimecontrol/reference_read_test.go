package runtimecontrol

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/autocarto/carto_agent/internal/skill"
	tooldef "github.com/autocarto/carto_agent/internal/tool"
)

func referenceFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	assets, home := filepath.Join(root, "autocarto"), filepath.Join(root, "runtime")
	if err := os.MkdirAll(filepath.Join(assets, "skills", "system"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOCARTO_ASSETS_ROOT", assets)
	t.Setenv("AUTOCARTO_HOME", home)
	return assets, home
}

func writeReference(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReferenceReadAllowedRootsAndWholePackageTexts(t *testing.T) {
	assets, home := referenceFixture(t)
	packageDir := filepath.Join(assets, "skills", "system", "geometry")
	writeReference(t, filepath.Join(packageDir, "SKILL.md"), "# 真实工具包")
	writeReference(t, filepath.Join(packageDir, "config.yaml"), "enabled: true")
	writeReference(t, filepath.Join(packageDir, "references", "方法.md"), "通用方法")
	paths := []string{"geometry/references/方法.md", "geometry/SKILL.md", "geometry/config.yaml"}
	for _, item := range []struct{ root, name string }{
		{filepath.Join(assets, "skills", "user"), "user-method"},
		{filepath.Join(home, "config", "skills", "global"), "global-method"},
	} {
		writeReference(t, filepath.Join(item.root, item.name, "SKILL.md"), "包说明")
		writeReference(t, filepath.Join(item.root, item.name, "references", "方法.md"), "用户方法")
		paths = append(paths, item.name+"/references/方法.md")
	}
	for _, path := range []string{
		filepath.Join(assets, "flows", "system", "demo", "resources", "docs", "说明.txt"),
		filepath.Join(home, "flows", "demo", "resources", "styles", "对照表.csv"),
		filepath.Join(assets, "resources", "system", "layouts", "base.layout.yaml"),
		filepath.Join(home, "resources", "docs", "说明.json"),
	} {
		writeReference(t, path, "说明")
		paths = append(paths, path)
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			output, err := Invoke(context.Background(), ToolReferenceRead, map[string]any{"path": path})
			if err != nil {
				t.Fatal(err)
			}
			payload := output.(map[string]any)
			if payload["success"] != true || payload["text"] == "" || payload["truncated"] != false {
				t.Fatalf("bad text result: %#v", payload)
			}
		})
	}
}

func TestReferenceReadRejectsEscapeLinksAndUnsupportedFiles(t *testing.T) {
	assets, home := referenceFixture(t)
	pkg := filepath.Join(assets, "skills", "system", "geometry")
	writeReference(t, filepath.Join(pkg, "SKILL.md"), "工具包")
	outside := filepath.Join(t.TempDir(), "outside.md")
	writeReference(t, outside, "outside")
	fileLink := filepath.Join(pkg, "references", "linked.md")
	if err := os.MkdirAll(filepath.Dir(fileLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, fileLink); err != nil {
		t.Fatal(err)
	}
	dirLink := filepath.Join(pkg, "alias")
	if err := os.Symlink(filepath.Dir(outside), dirLink); err != nil {
		t.Fatal(err)
	}
	intermediate := filepath.Join(home, "resources")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), intermediate); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		outside, filepath.Join(assets, "skills", "system-other", "outside.md"),
		"geometry/../geometry/SKILL.md", "geometry/references/a.py", "geometry/references/a.pdf",
		fileLink, filepath.Join(dirLink, "outside.md"), filepath.Join(intermediate, "outside.md"),
		filepath.Join(home, "config", "config.yaml"),
		filepath.Join(assets, "flows", "system", "demo", "FLOW.md"),
		filepath.Join(assets, "flows", "system", "demo", "resources", "data", "notes.md"),
	} {
		if _, err := Invoke(context.Background(), ToolReferenceRead, map[string]any{"path": path}); err == nil {
			t.Errorf("accepted forbidden path %q", path)
		}
	}
}

func TestReferenceReadUsesEffectivePackageDirectoryWithoutFileFallback(t *testing.T) {
	assets, home := referenceFixture(t)
	base, override := filepath.Join(assets, "skills", "system"), filepath.Join(home, "config", "agent_configs", "active", "skills")
	for _, root := range []string{base, override} {
		writeReference(t, filepath.Join(root, "geometry", "SKILL.md"), "包说明")
	}
	writeReference(t, filepath.Join(base, "geometry", "references", "方法.md"), "old")
	writeReference(t, filepath.Join(override, "geometry", "references", "方法.md"), "new")
	ctx := WithReferencePackageDirs(context.Background(), []string{base, override})
	result, err := readReference(ctx, "geometry/references/方法.md")
	if err != nil || result["text"] != "new" {
		t.Fatalf("override mismatch: %#v %v", result, err)
	}
	if err := os.Remove(filepath.Join(override, "geometry", "references", "方法.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := readReference(ctx, "geometry/references/方法.md"); err == nil {
		t.Fatal("fell back to stale bundled reference")
	}
	// An inactive exclusive package is not searched by the default invocation.
	writeReference(t, filepath.Join(override, "only-active", "SKILL.md"), "包说明")
	writeReference(t, filepath.Join(override, "only-active", "references", "方法.md"), "active")
	if _, err := readReference(context.Background(), "only-active/references/方法.md"); err == nil {
		t.Fatal("inactive exclusive roots must not be enumerated")
	}
}

func TestReferenceReadTruncatesUTF8AndRejectsBinary(t *testing.T) {
	assets, _ := referenceFixture(t)
	path := filepath.Join(assets, "resources", "system", "docs", "large.md")
	writeReference(t, path, strings.Repeat("中", maxReferenceBytes))
	result, err := readReference(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	text := result["text"].(string)
	if len(text) > maxReferenceBytes || !utf8.ValidString(text) || result["truncated"] != true || result["warning"] == "" {
		t.Fatalf("bad truncation: bytes=%d %#v", len(text), result["warning"])
	}
	writeReference(t, path, "bad\x00binary")
	if _, err := readReference(context.Background(), path); err == nil {
		t.Fatal("accepted binary data")
	}
	writeReference(t, path, string([]byte{0xff, 0xfe}))
	if _, err := readReference(context.Background(), path); err == nil {
		t.Fatal("accepted non-UTF8 data")
	}
}

func TestReferenceReadRealYAMLExecutesThroughSkillEngine(t *testing.T) {
	assets, _ := referenceFixture(t)
	path := filepath.Join(assets, "resources", "system", "docs", "engine.md")
	writeReference(t, path, "真实链路")
	_, source, _, _ := runtime.Caller(0)
	toolsDir := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../autocarto/tools/system"))
	registry := tooldef.NewRegistry()
	if err := registry.LoadDirectories(toolsDir); err != nil {
		t.Fatal(err)
	}
	skills := skill.NewRegistry(registry)
	if err := skills.LoadDirectories(filepath.Clean(filepath.Join(toolsDir, "../../skills/system"))); err != nil {
		t.Fatal(err)
	}
	engine := skill.NewEngine(skills, skill.NewInternalBackend(Invoke))
	result, err := engine.Execute(context.Background(), ToolReferenceRead, map[string]any{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	payload := result.Output.(map[string]any)
	if payload["text"] != "真实链路" || payload["path"] != path {
		t.Fatalf("YAML parameter mapping lost: %#v", result)
	}
	ctx := WithReferencePackageDirs(context.Background(), []string{filepath.Clean(filepath.Join(toolsDir, "../../skills/system"))})
	result, err = engine.Execute(ctx, ToolReferenceRead, map[string]any{"path": "vector-geometry/references/缓冲与邻近分析.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output.(map[string]any)["text"].(string), "single_sided_buffer") {
		t.Fatal("actual merged reference was not readable through tool engine")
	}
}
