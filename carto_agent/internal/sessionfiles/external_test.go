package sessionfiles

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalPathsDoNotRequireProvidedRegistry(t *testing.T) {
	folder := t.TempDir()
	path := put(t, filepath.Join(folder, "nested", "source.txt"), "source")
	store, err := NewAt("local-input", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Old sessions may retain the former access list. It is no longer read.
	put(t, filepath.Join(store.Dir, ".provided-files.json"), `{"version":2,"files":{},"directories":{}}`)
	for _, input := range []string{folder, filepath.Dir(path), path} {
		if _, _, err := store.ValidatePath(input); err != nil {
			t.Fatalf("local input %s rejected: %v", input, err)
		}
	}
	link := filepath.Join(t.TempDir(), "source-link.txt")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := store.ValidatePath(link); err != nil {
		t.Fatalf("local input symlink rejected: %v", err)
	}
	if _, _, err := store.ValidatePath(filepath.Join(folder, "missing.txt")); !os.IsNotExist(err) {
		t.Fatalf("missing input should report the filesystem error: %v", err)
	}
}

func TestProvidedFileRegistrationOnlyMaintainsMaterialIndex(t *testing.T) {
	folder := t.TempDir()
	path := put(t, filepath.Join(folder, "file.geojson"), "original")
	neighbor := put(t, filepath.Join(folder, "neighbor.geojson"), "local source")
	store, err := NewAt("example-session", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.RegisterProvided(path, Origin{Type: "provided"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := NewAt(store.SessionID, store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := again.Get(item.ID); err != nil || got.Origin.Type != "provided" {
		t.Fatalf("material index not retained: %+v %v", got, err)
	}
	if _, _, err := again.ValidatePath(neighbor); err != nil {
		t.Fatalf("unregistered local file rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.Dir, ".provided-files.json")); !os.IsNotExist(err) {
		t.Fatalf("registration still created an access list: %v", err)
	}
}
