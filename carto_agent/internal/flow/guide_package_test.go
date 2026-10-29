package flow

import (
	"archive/zip"
	"context"
	"github.com/autocarto/carto_agent/internal/resource"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreeFlowExportsOnlyReferencedVisualDependencies(t *testing.T) {
	s := guideTestStore(t)
	library := resource.NewLibrary("", filepath.Join(t.TempDir(), "library"), s.Packages.Bindings)
	input := filepath.Join(t.TempDir(), "river.qml")
	if err := os.WriteFile(input, []byte(`<qgis version="3.34"><renderer-v2 type="singleSymbol"/></qgis>`), 0600); err != nil {
		t.Fatal(err)
	}
	style, err := library.Import(input, "style")
	if err != nil {
		t.Fatal(err)
	}
	s.Packages.SetLibrary(library)
	g, err := s.Create("---\n名称: 测试\n说明: 导出\n---\n任意正文引用「"+style.Name+"」。", "visual-export")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(g.Dir, "resources", "data"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(g.Dir, "resources", "data", "private.csv"), []byte("private data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(g.Dir, "FLOW.md.bak"), []byte("private history"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "flow.zip")
	if _, err = s.Packages.ExportWithOptions(g.Meta.ID, target, ExportOptions{IncludeDataFiles: true, IncludeLibraryDeps: true, Library: library}); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(target)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	dependency := false
	for _, f := range archive.File {
		if strings.Contains(f.Name, "private.csv") || strings.HasSuffix(f.Name, ".bak") || strings.Contains(f.Name, "/resources/") {
			t.Fatalf("nonvisual payload exported: %s", f.Name)
		}
		if strings.Contains(f.Name, "/deps/") && strings.HasSuffix(f.Name, ".qml") {
			dependency = true
		}
	}
	if !dependency {
		t.Fatal("referenced QML missing from export")
	}
	other := guideTestStore(t)
	otherLibrary := resource.NewLibrary("", filepath.Join(t.TempDir(), "library"), other.Packages.Bindings)
	other.Packages.SetLibrary(otherLibrary)
	imported, err := other.Import(target, ImportOptions{}, otherLibrary)
	if err != nil {
		t.Fatal(err)
	}
	got, err := other.Resolve(context.Background(), imported.Meta.ID, otherLibrary)
	if err != nil || len(got.Scope.Linked) != 1 || got.Scope.Linked[0].ID != style.ID || resource.HasErrors(got.Scope.Linked[0].Problems) {
		t.Fatalf("bundle lost identity: %+v %v", got, err)
	}
}
func TestSaveGUIDECompatibilityKeepsBackup(t *testing.T) {
	s := guideTestStore(t)
	dir := filepath.Join(s.Packages.UserDir, "old-guide")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	original := "---\n名称: 旧指南\n说明: 兼容读取\n---\n任意正文。\n"
	if err := os.WriteFile(filepath.Join(dir, "GUIDE.md"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	g, err := s.Get("old-guide")
	if err != nil || g.GuideMD != original {
		t.Fatalf("compatibility read failed: %+v %v", g, err)
	}
	if _, err = s.Save("old-guide", original+"补充\n"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "GUIDE.md")); !os.IsNotExist(err) {
		t.Fatal("old main filename retained")
	}
	backup, err := os.ReadFile(filepath.Join(dir, "GUIDE.md.bak"))
	if err != nil || string(backup) != original {
		t.Fatal("original backup lost")
	}
	if _, err = os.Stat(filepath.Join(dir, "FLOW.md")); err != nil {
		t.Fatal(err)
	}
}
