package resource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestManagedImportCopyReplaceDeleteUpgrade(t *testing.T) {
	root, library := resourceProject(t)
	source := resourceWrite(t, filepath.Join(root, "source/说明.qml"), "<qgis><note>initial</note></qgis>")
	imported, err := library.ImportNamed(source, "style", "新说明")
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID(imported.ID) || !strings.HasPrefix(imported.ID, "style.u-") || imported.Legacy || imported.Version != "1.0.0" || imported.Name != "新说明" {
		t.Fatal(imported)
	}
	if _, err = os.Stat(filepath.Join(library.UserDir, "styles/新说明.resource.yaml")); err != nil {
		t.Fatal(err)
	}
	_, err = library.ImportNamed(source, "style", "新说明")
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.ExistingID != imported.ID {
		t.Fatal(err)
	}
	second, err := library.ImportNamed(source, "style", "另存名称")
	if err != nil || second.ID == imported.ID {
		t.Fatal(second, err)
	}
	resourceWrite(t, source, "<qgis><note>replacement</note></qgis>")
	replaced, err := library.Replace(imported.ID, source)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.ID != imported.ID || replaced.Version != "1.0.1" || replaced.Digest == imported.Digest {
		t.Fatal(replaced)
	}
	backups, err := filepath.Glob(filepath.Join(library.UserDir, ".backup", imported.ID, "*", "新说明.qml"))
	if err != nil || len(backups) != 1 {
		t.Fatal(backups, err)
	}
	raw, _ := os.ReadFile(backups[0])
	if string(raw) != "<qgis><note>initial</note></qgis>" {
		t.Fatal(string(raw))
	}
	bad := resourceWrite(t, filepath.Join(root, "source/bad.qml"), string([]byte{0xff}))
	if _, err = library.Replace(imported.ID, bad); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	after, err := library.GetByID(imported.ID)
	if err != nil || after.Version != "1.0.1" || after.Digest != replaced.Digest {
		t.Fatal(after, err)
	}
	builtin := resourceWrite(t, filepath.Join(library.BuiltinDir, "styles/内置.qml"), "<qgis><note>builtin</note></qgis>")
	writeManifest(t, builtin, Manifest{ID: "style.builtin", Type: "style", Name: "内置"})
	copied, err := library.Copy("style.builtin", true, "")
	if err != nil || copied.Overrides != "style.builtin" {
		t.Fatal(copied, err)
	}
	resolved := library.Resolve(context.Background(), Ref{ID: "style.builtin"}, Scope{})
	if resolved.ID != copied.ID {
		t.Fatal(resolved)
	}
	if _, err = library.Copy("style.builtin", true, "再次覆盖"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	for _, operation := range []func() error{func() error { return library.DeleteID("style.builtin") }, func() error { _, e := library.Replace("style.builtin", source); return e }, func() error { _, e := library.Upgrade("style.builtin"); return e }} {
		if err = operation(); !errors.Is(err, ErrReadOnly) {
			t.Fatal(err)
		}
	}
	if err = library.DeleteID(copied.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(ManifestPath(copied.Root, "style")); !os.IsNotExist(err) {
		t.Fatal("manifest left after delete", err)
	}
	resolved = library.Resolve(context.Background(), Ref{ID: "style.builtin"}, Scope{})
	if resolved.ID != "style.builtin" {
		t.Fatal(resolved)
	}
	legacy := resourceWrite(t, filepath.Join(library.UserDir, "styles/旧资源.qml"), "<qgis><note>old</note></qgis>")
	old, err := library.Get("style", "旧资源")
	if err != nil || !old.Legacy {
		t.Fatal(old, err)
	}
	upgraded, err := library.Upgrade(old.ID)
	if err != nil || upgraded.Legacy || upgraded.ID == old.ID || upgraded.Version != "1.0.0" {
		t.Fatal(upgraded, err)
	}
	raw, _ = os.ReadFile(legacy)
	if string(raw) != "<qgis><note>old</note></qgis>" {
		t.Fatal("upgrade changed content")
	}
	if _, err = library.GetByID(old.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("legacy alias must not hide identity change", err)
	}
}
func TestManagedConcurrentImportAndAtomicReads(t *testing.T) {
	root, library := resourceProject(t)
	source := resourceWrite(t, filepath.Join(root, "source/test.qml"), "<qgis><note>v1</note></qgis>")
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := library.Import(source, "style"); results <- err }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal(success)
	}
	item, err := library.Get("style", "test")
	if err != nil {
		t.Fatal(err)
	}
	next := resourceWrite(t, filepath.Join(root, "source/next.qml"), "<qgis><note>v2</note></qgis>")
	readErrors := make(chan error, 100)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				r, e := library.GetByID(item.ID)
				if e != nil {
					readErrors <- e
				} else if r.Handle() == nil {
					readErrors <- errors.New("partial resource visible")
				}
			}
		}()
	}
	for i := 0; i < 5; i++ {
		if _, err = library.Replace(item.ID, next); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	close(readErrors)
	for err := range readErrors {
		t.Error(err)
	}
}
func TestManagedFolderEntryAndFormatReplacement(t *testing.T) {
	root, library := resourceProject(t)
	folder := filepath.Join(root, "source/set")
	resourceWrite(t, filepath.Join(folder, "maps/custom.csv"), "layer,style_file\n道路,maps/line.qml\n")
	resourceWrite(t, filepath.Join(folder, "maps/line.qml"), "<qgis/>")
	writeManifest(t, folder, Manifest{ID: "style_set.source", Type: "style_set", Name: "样式", Entry: "maps/custom.csv"})
	imported, err := library.Import(folder, "style_set")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(imported.Entry, "maps/custom.csv") || imported.Handle() == nil {
		t.Fatal(imported)
	}
	qpt := resourceWrite(t, filepath.Join(root, "source/layout.qpt"), "<Layout><LayoutItem id=\"main_map\"/></Layout>")
	layout, err := library.Import(qpt, "layout")
	if err != nil {
		t.Fatal(err)
	}
	yaml := resourceWrite(t, filepath.Join(root, "source/layout.layout.yaml"), "name: 布局\npage: {}\nfonts_mm: {}\nelements: {main_map: {}}\n")
	// Same-name sources in one directory are intentionally invalid; move the old source away.
	if err = os.Remove(qpt); err != nil {
		t.Fatal(err)
	}
	changed, err := library.Replace(layout.ID, yaml)
	if err != nil || changed.Format != "layout_yaml" || changed.Version != "1.0.1" {
		t.Fatal(changed, err)
	}
	if _, err = os.Stat(layout.Root); !os.IsNotExist(err) {
		t.Fatal("old format primary left behind", err)
	}
}
