package sessionfiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProvidedDirectoryGrantAndLegacyFileMigration(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "selected")
	child := filepath.Join(dir, "nested", "source.txt")
	neighbor := filepath.Join(parent, "other.txt")
	if err := os.MkdirAll(filepath.Dir(child), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{child, neighbor} {
		if err := os.WriteFile(path, []byte("source"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewWithRoots("directory-input", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	canonicalNeighbor, _ := filepath.EvalSymlinks(neighbor)
	legacy, _ := json.Marshal(map[string]string{neighbor: canonicalNeighbor, canonicalNeighbor: canonicalNeighbor})
	if err = os.WriteFile(filepath.Join(store.Dir, ".provided-files.json"), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if err = store.RegisterProvidedDirectory(dir); err != nil {
		t.Fatal(err)
	}
	again, err := NewWithRoots(store.SessionID, store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, filepath.Dir(child), child, neighbor} {
		if _, err = again.ResolveProvidedPath(path); err != nil {
			t.Fatalf("provided %s denied: %v", path, err)
		}
	}
	if _, err = again.ResolveProvidedPath(parent); err == nil {
		t.Fatal("parent authorized")
	}
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err = os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "escape.txt")
	if err = os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err = again.ResolveProvidedPath(link); err == nil {
		t.Fatal("directory symlink escaped grant")
	}
	if _, _, err = again.ValidatePath(link); err == nil {
		t.Fatal("legacy validator escaped directory grant")
	}
	if err = os.Remove(neighbor); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(neighbor, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(neighbor, "new.txt"), []byte("unselected"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = again.ResolveProvidedPath(filepath.Join(neighbor, "new.txt")); err == nil {
		t.Fatal("legacy exact file expanded to directory")
	}
}

func TestProvidedExactFileAndSymlinkChange(t *testing.T) {
	outside := t.TempDir()
	path := filepath.Join(outside, "file.geojson")
	neighbor := filepath.Join(outside, "neighbor.geojson")
	os.WriteFile(path, []byte("original"), 0600)
	os.WriteFile(neighbor, []byte("private"), 0600)
	store, _ := NewWithRoots("example-session", t.TempDir())
	item, err := store.RegisterProvided(path, Origin{Type: "user"})
	if err != nil {
		t.Fatal(err)
	}
	again, _ := NewWithRoots(store.SessionID, store.Dir)
	if _, err = again.Get(item.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = again.ValidatePath(neighbor); err == nil {
		t.Fatal("neighbor authorized")
	}
	if _, err = again.RegisterProvided(outside, Origin{Type: "user"}); err == nil {
		t.Fatal("directory authorized")
	}
	os.Remove(path)
	os.Symlink(neighbor, path)
	if _, err = again.Get(item.ID); err == nil {
		t.Fatal("changed symlink authorized")
	}
}
