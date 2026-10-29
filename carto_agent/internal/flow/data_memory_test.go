package flow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataMemoryMigratesWithBackupAndGuideCategoryKeys(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "flow_input_memory.yaml")
	original := []byte("apiVersion: flow-input-memory/v1\nflows:\n  route-map:\n    河流: /tmp/river.geojson\n")
	os.WriteFile(old, original, 0600)
	memory := NewDataMemory(filepath.Join(root, "data_memory.yaml"))
	if err := memory.MigrateLegacy(old); err != nil {
		t.Fatal(err)
	}
	if err := memory.MigrateLegacy(old); err != nil {
		t.Fatal(err)
	}
	backup, _ := os.ReadFile(old + ".v5.bak")
	if string(backup) != string(original) {
		t.Fatal("backup changed")
	}
	entries, err := memory.List()
	if err != nil || len(entries) != 1 || entries[0].Guide != "route-map" {
		t.Fatalf("%+v %v", entries, err)
	}
	entry := DataMemoryEntry{Name: "河流", Category: "water.river", Extent: "湖南", SourceID: "source.osm.overpass"}
	if err = memory.Remember(entry); err != nil {
		t.Fatal(err)
	}
	entries, _ = memory.List()
	if len(entries) != 2 {
		t.Fatal(entries)
	}
	if err = memory.Forget(entry); err != nil {
		t.Fatal(err)
	}
	entries, _ = memory.List()
	if len(entries) != 1 {
		t.Fatal(entries)
	}
}
