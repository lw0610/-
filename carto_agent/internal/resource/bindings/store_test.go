package bindings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBindingKeysV2(t *testing.T) {
	for _, key := range []string{"flow:test-flow/data_list.base", "flow:test-flow/path:data/中国底图.csv", "flow:test-flow/library:data_list.base", "library:data_list.base", "library:legacy.data_list.012345abcd"} {
		if err := ValidateKey(key); err != nil {
			t.Errorf("%s: %v", key, err)
		}
	}
	for _, key := range []string{"flow:test-flow/resource_6d294f11b03f", "library:底图", "flow:test-flow/path:../secret.csv", "flow:test-flow/path:/absolute.csv", "flow:test-flow/path:data/../base.csv", "flow:test-flow/path:data//base.csv", "flow:test-flow/path:data\\base.csv", "flow:bad/id/path:data.csv", "flow:test-flow/library:foo", "library:data_list.base\n"} {
		if err := ValidateKey(key); err == nil {
			t.Errorf("accepted %q", key)
		}
	}
}

func TestMigrationPreservesAmbiguityCollisionAndOrphans(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	original := Document{Bindings: map[string]string{"library:唯一": "data", "library:同名": "one", "flow:test-flow/resource_olda": "one", "flow:test-flow/resource_oldb": "two", "library:未知": "../unresolved"}, Orphaned: map[string]string{"untouched": "../keep"}}
	candidates := map[string][]string{"library:唯一": {"library:data_list.unique"}, "library:同名": {"library:data_list.first", "library:data_list.second"}, "flow:test-flow/resource_olda": {"flow:test-flow/data_list.base"}, "flow:test-flow/resource_oldb": {"flow:test-flow/data_list.base"}}
	doc, err := migrateV1(original, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc.Bindings, map[string]string{"library:data_list.unique": "data"}) || len(doc.Orphaned) != 5 || doc.Orphaned["library:未知"] != "../unresolved" || doc.Orphaned["untouched"] != "../keep" {
		t.Fatalf("%+v", doc)
	}
}

func TestV2ReadIsIdempotentAndUnknownVersionCannotWrite(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	file := filepath.Join(root, "config/bindings.yaml")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	raw := "apiVersion: resource-bindings/v2\nbindings:\n  library:data_list.base: data\norphaned:\n  old: ../keep\n"
	if err := os.WriteFile(file, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	store := New(file)
	if err := store.PrepareMigration(map[string][]string{"library:data_list.base": {"library:data_list.wrong"}}); err != nil {
		t.Fatal(err)
	}
	doc, err := store.Snapshot()
	if err != nil || doc.Bindings["library:data_list.base"] != "data" || doc.Orphaned["old"] != "../keep" {
		t.Fatalf("%+v %v", doc, err)
	}
	current, _ := os.ReadFile(file)
	if string(current) != raw {
		t.Fatal("v2 read rewrote bytes")
	}
	if err := os.WriteFile(file, []byte(strings.Replace(raw, "resource-bindings/v2", "resource-bindings/v99", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("library:data_list.base", root); err == nil {
		t.Fatal("unknown version was overwritten")
	}
}

func TestFailedBackupPreservesOriginal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	file := filepath.Join(root, "bindings.yaml")
	raw := "bindings:\n  library:旧底图: data\n"
	if err := os.WriteFile(file, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backups"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	store := New(file)
	if err := store.PrepareMigration(map[string][]string{"library:旧底图": {"library:data_list.base"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("library:data_list.base", root); err == nil {
		t.Fatal("backup failure should abort write")
	}
	current, _ := os.ReadFile(file)
	if string(current) != raw {
		t.Fatal("failed backup changed original")
	}
}
