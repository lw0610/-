package sessionfiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, path, body string) string {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
	return path
}
func TestRegistrationScanMergeAndRebuild(t *testing.T) {
	root := t.TempDir()
	store, e := NewWithRoots("demo", root)
	if e != nil {
		t.Fatal(e)
	}
	pdf := put(t, filepath.Join(root, "exports", "地图.pdf"), "pdf")
	put(t, filepath.Join(root, "output", "roads.shp"), "shape")
	put(t, filepath.Join(root, "output", "roads.dbf"), "table")
	put(t, filepath.Join(root, "output", "roads.prj"), "crs")
	put(t, filepath.Join(root, "output", "orphan.dbf"), "table")
	put(t, filepath.Join(root, "intermediate", "data.csv"), "a,b\n1,2")
	put(t, filepath.Join(root, "flows", "run-1", "run.json"), "{}")
	put(t, filepath.Join(root, "output", "session.json"), "private")
	put(t, filepath.Join(root, "temp", "script.py"), "pass")
	events := make(chan Change, 2)
	store.OnChange(func(c Change) { events <- c })
	item, e := store.Register(pdf, Origin{Type: "tool", Label: "导出"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.Register(pdf, Origin{Type: "tool", Label: "再次导出"}); e != nil {
		t.Fatal(e)
	}
	select {
	case event := <-events:
		if event.Added != 1 || event.Updated != 1 {
			t.Fatal(event)
		}
	case <-time.After(time.Second * 2):
		t.Fatal("no event")
	}
	items, e := store.List(false)
	if e != nil {
		t.Fatal(e)
	}
	if len(items) != 4 {
		t.Fatalf("%+v", items)
	}
	for _, it := range items {
		if it.Name == "roads.shp" && len(it.Members) != 2 {
			t.Fatal(it)
		}
		if it.ID == item.ID && it.Origin.Label != "再次导出" {
			t.Fatal(it)
		}
	}
	if got, e := store.Get(item.ID); e != nil || got.Group != "deliverable" {
		t.Fatal(got, e)
	}
	all, e := store.List(true)
	if e != nil || len(all) != 6 {
		t.Fatal(all, e)
	}
	put(t, filepath.Join(root, "files.index.json"), "invalid json")
	if _, e = store.List(false); e != nil {
		t.Fatal(e)
	}
	backups, _ := filepath.Glob(filepath.Join(root, "files.index.json.corrupt-*"))
	if len(backups) != 1 {
		t.Fatal(backups)
	}
}
func TestRejectsOutsideAndChangedSymlinkAndUnindexed(t *testing.T) {
	root := t.TempDir()
	store, _ := NewWithRoots("demo", root)
	outside := put(t, filepath.Join(t.TempDir(), "secret.txt"), "private")
	if _, e := store.Register(outside, Origin{}); e == nil {
		t.Fatal("outside registered")
	}
	if _, e := store.Get("f_unknown"); e == nil {
		t.Fatal("unindexed read")
	}
	file := put(t, filepath.Join(root, "output", "result.txt"), "okay")
	item, e := store.Register(file, Origin{})
	if e != nil {
		t.Fatal(e)
	}
	os.Remove(file)
	if e = os.Symlink(outside, file); e != nil {
		t.Skip(e)
	}
	if _, e = store.Get(item.ID); e == nil {
		t.Fatal("symlink accepted")
	}
	entries, e := store.List(true)
	if e != nil || len(entries) != 0 {
		t.Fatal(entries, e)
	}
}
func TestStructuredPathsDoNotInterpretProse(t *testing.T) {
	if paths := ArtifactPaths("输出在 /tmp/file.pdf"); len(paths) != 0 {
		t.Fatal(paths)
	}
	paths := ArtifactPaths(`{"output_path":"/tmp/map.pdf","source_path":"/tmp/input.csv","data":{"path":"/tmp/roads.geojson"}}`)
	if len(paths) != 2 {
		t.Fatal(paths)
	}
	for _, path := range paths {
		if strings.Contains(path, "input") {
			t.Fatal(paths)
		}
	}
}

func TestDirectoryRolesOverrideLegacyGenericData(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	store, _ := NewWithRoots("roles", root)
	var legacy []Item
	for _, tc := range []struct{ path, want string }{
		{"exports/result.geojson", "deliverable"},
		{"exports/result.tif", "deliverable"},
		{"exports/result.csv", "deliverable"},
		{"output/osm/download.geojson", "source"},
		{"output/other_clean.geojson", "data"},
	} {
		path := put(t, filepath.Join(root, tc.path), "fixture")
		item, err := store.Register(path, Origin{Type: "tool", Tool: "osm_get_features"}, WithGroup("data"))
		if err != nil || item.Group != tc.want {
			t.Fatalf("register %s: %+v %v", tc.path, item, err)
		}
		item.Group = "data"
		legacy = append(legacy, item)
	}
	old, _ := json.Marshal(index{Version: 1, Items: legacy})
	put(t, filepath.Join(root, "files.index.json"), string(old))
	items, err := store.List(false)
	if err != nil || len(items) != 5 {
		t.Fatalf("list: %+v %v", items, err)
	}
	want := map[string]string{"exports/result.geojson": "deliverable", "exports/result.tif": "deliverable", "exports/result.csv": "deliverable", "output/osm/download.geojson": "source", "output/other_clean.geojson": "data"}
	for _, item := range items {
		rel, _ := filepath.Rel(root, item.Path)
		if item.Group != want[rel] {
			t.Fatalf("legacy group not upgraded: %+v", item)
		}
	}
	persisted, err := store.read()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range persisted.Items {
		rel, _ := filepath.Rel(root, item.Path)
		if item.Group != want[rel] {
			t.Fatalf("upgrade not persisted: %+v", item)
		}
	}
}

func TestReplacingFileKeepsOneIdentityAndSidecars(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	store, _ := NewWithRoots("replacement", root)
	path := put(t, filepath.Join(root, "output", "osm", "roads.geojson"), `{"type":"FeatureCollection","features":[]}`)
	sidecar := put(t, path+".source.json", `{"source":"test"}`)
	original, err := store.Register(path, Origin{Type: "tool", Label: "下载"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.List(false); err != nil {
		t.Fatal(err)
	}

	// The replacement is prepared separately; another unpublished staging file
	// remains on disk to ensure a refresh never exposes work in progress.
	corrected := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"road"},"geometry":{"type":"LineString","coordinates":[[110,28],[111,29]]}}]}`
	stageDir := filepath.Join(filepath.Dir(path), ".staging")
	stage := put(t, filepath.Join(stageDir, "ready.geojson"), corrected)
	put(t, filepath.Join(stageDir, "unfinished.geojson"), "unfinished")
	modified := original.ModifiedAt.Add(2 * time.Second)
	if err = os.Chtimes(stage, modified, modified); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(stage, path); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Register(path, Origin{Type: "tool", Label: "修正"}); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(true)
	if err != nil || len(items) != 1 || items[0].ID != original.ID {
		t.Fatalf("replacement duplicated files or exposed staging: %+v %v", items, err)
	}
	if len(items[0].Members) != 1 || items[0].Members[0] != sidecar {
		t.Fatalf("replacement lost sidecar: %+v", items[0])
	}
	if items[0].Size != int64(len(corrected)+len(`{"source":"test"}`)) || !items[0].ModifiedAt.Equal(modified) || items[0].Origin.Label != "修正" {
		t.Fatalf("file list still describes the old content: %+v", items[0])
	}
	current, err := store.Get(original.ID)
	if err != nil || current.ID != original.ID || current.Size != int64(len(corrected)) || !current.ModifiedAt.Equal(modified) || current.Origin.Label != "修正" {
		t.Fatalf("replacement metadata stayed stale: %+v %v", current, err)
	}
	if content, err := os.ReadFile(current.Path); err != nil || string(content) != corrected {
		t.Fatalf("stable file identity does not expose corrected content: %v", err)
	}
}
