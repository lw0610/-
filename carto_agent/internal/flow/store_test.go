package flow

import (
	"archive/zip"
	"errors"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const storedFlowFixture = "---\n名称: 示例流程\n编号: example-flow\n---\n## 步骤\n### 第1步 检查\n- 做法：检查工程\n"

func newTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	return NewStore(filepath.Join(root, "builtin"), filepath.Join(root, "home", "flows"), bindings.New(filepath.Join(root, "home", "config", "resource_bindings.yaml")))
}
func putFlowFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestStoreCreateSaveHistoryCopyAndDelete(t *testing.T) {
	s := newTestStore(t)
	item, err := s.Create(storedFlowFixture, "")
	if err != nil {
		t.Fatal(err)
	}
	if item.Meta.ID != "example-flow" || item.Source != "user" {
		t.Fatalf("unexpected flow: %+v", item)
	}
	if _, err = s.Create(storedFlowFixture, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate accepted: %v", err)
	}
	if _, err = s.Save(item.Meta.ID, strings.ReplaceAll(item.FlowMD, "检查工程", "检查图层")); err != nil {
		t.Fatal(err)
	}
	histories, err := os.ReadDir(filepath.Join(item.Dir, ".history"))
	if err != nil || len(histories) != 1 {
		t.Fatalf("history missing: %v %v", histories, err)
	}
	old, err := os.ReadFile(filepath.Join(item.Dir, ".history", histories[0].Name(), "FLOW.md"))
	if err != nil || !strings.Contains(string(old), "检查工程") {
		t.Fatalf("old version lost: %v", err)
	}
	copy, err := s.Copy(item.Meta.ID, "copied-flow", "另一个流程")
	if err != nil {
		t.Fatal(err)
	}
	if copy.Meta.Name != "另一个流程" {
		t.Fatalf("copy metadata: %+v", copy.Meta)
	}
	if err = s.Delete(copy.Meta.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(copy.Meta.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestStoreImportRoundTripAndResources(t *testing.T) {
	s := newTestStore(t)
	source := filepath.Join(t.TempDir(), "示例")
	putFlowFile(t, filepath.Join(source, "FLOW.md"), storedFlowFixture)
	putFlowFile(t, filepath.Join(source, "resources", "docs", "guide.md"), "资料正文")
	item, err := s.Import(source, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Resources) != 1 {
		t.Fatalf("resources: %v", item.Resources)
	}
	archive := filepath.Join(t.TempDir(), "flow.zip")
	if _, err = s.Export(item.Meta.ID, archive, true); err != nil {
		t.Fatal(err)
	}
	other := newTestStore(t)
	imported, err := other.Import(archive, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if imported.FlowMD != item.FlowMD || len(imported.Resources) != 0 {
		t.Fatal("round trip changed the package")
	}
	putFlowFile(t, filepath.Join(source, "resources", "docs", "guide.md"), "更新正文")
	if _, err = s.Import(source, ImportOptions{Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(t.TempDir(), "FLOW.md")
	putFlowFile(t, md, strings.ReplaceAll(storedFlowFixture, "example-flow", "markdown-flow"))
	if _, err = s.Import(md, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestStoreProtectsBuiltinAndRejectsSymlinks(t *testing.T) {
	s := newTestStore(t)
	path := filepath.Join(s.BuiltinDir, "example-flow", "FLOW.md")
	putFlowFile(t, path, storedFlowFixture)
	if err := s.Delete("example-flow"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("deleted builtin: %v", err)
	}
	if _, err := s.Save("example-flow", storedFlowFixture); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("edited builtin: %v", err)
	}
	if _, err := s.Import(path, ImportOptions{Overwrite: true}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("overwrote builtin: %v", err)
	}
	if _, err := s.Copy("example-flow", "my-flow", ""); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "package")
	putFlowFile(t, filepath.Join(source, "FLOW.md"), strings.ReplaceAll(storedFlowFixture, "example-flow", "linked-flow"))
	if err := os.Symlink(path, filepath.Join(source, "link.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Import(source, ImportOptions{}); err == nil {
		t.Fatal("accepted linked package")
	}
	if _, err := s.Get("linked-flow"); !errors.Is(err, ErrNotFound) {
		t.Fatal("failed import was published")
	}
}

func TestStoreRejectsUnsafeZIP(t *testing.T) {
	for _, name := range []string{"../outside", "/outside", "C:/outside", "resources/../../outside", `resources\outside`} {
		t.Run(name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "bad.zip")
			out, _ := os.Create(archive)
			w := zip.NewWriter(out)
			entry, _ := w.Create(name)
			entry.Write([]byte("bad"))
			w.Close()
			out.Close()
			if _, err := newTestStore(t).Import(archive, ImportOptions{}); err == nil {
				t.Fatal("accepted unsafe ZIP")
			}
		})
	}
}

func TestStoreRejectsOversizeAndInvalidIDs(t *testing.T) {
	s := newTestStore(t)
	for _, id := range []string{"../other", "A", "a/b", "_invalid"} {
		if _, err := s.Create(storedFlowFixture, id); err == nil {
			t.Fatalf("accepted %s", id)
		}
	}
	source := filepath.Join(t.TempDir(), "package")
	putFlowFile(t, filepath.Join(source, "FLOW.md"), storedFlowFixture)
	large, err := os.Create(filepath.Join(source, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err = large.Truncate(MaxFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	large.Close()
	if _, err = s.Import(source, ImportOptions{}); err == nil {
		t.Fatal("accepted oversize file")
	}
}
