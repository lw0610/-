package resource

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicImportDetectionMappingAndSourcePreservation(t *testing.T) {
	root, library := resourceProject(t)
	cases := []struct{ name, content, typ string }{
		{"map.qpt", "<Layout><LayoutItem id=\"main_map\"/></Layout>", "layout"},
		{"map.layout.yaml", "name: map\npage: {}\nfonts_mm: {}\nelements: {main_map: {}}\n", "layout"},
		{"line.qml", "<qgis><layerGeometryType>1</layerGeometryType></qgis>", "style"},
	}
	for i, c := range cases {
		path := resourceWrite(t, filepath.Join(root, "source", string(rune('a'+i)), c.name), c.content)
		got, err := library.ImportNamed(path, "", string(rune('a'+i)))
		if err != nil || got.Type != c.typ || got.Handle() == nil {
			t.Fatalf("%s: %+v %v", c.name, got, err)
		}
	}
	folder := filepath.Join(root, "source/set")
	resourceWrite(t, filepath.Join(folder, "河流.qml"), "<qgis/>")
	resourceWrite(t, filepath.Join(folder, "省域.qml"), "<qgis/>")
	result, err := library.ImportNamed(folder, "", "")
	if err != nil || len(result.Entries) != 2 || !resourceHasProblem(result, "RESOURCE_STYLE_MAPPING_GENERATED", "info") {
		t.Fatal(result, err)
	}
	if _, err = os.Stat(filepath.Join(folder, "对照表.csv")); !os.IsNotExist(err) {
		t.Fatal("source was modified", err)
	}
	if result.Entries[0].Name != "河流" || result.Entries[0].Fields["label_mode"] != "keep" {
		t.Fatal(result.Entries)
	}
	wrong := resourceWrite(t, filepath.Join(root, "source/wrong.qml"), "<qgis/>")
	if _, err = library.ImportNamed(wrong, "layout", ""); err == nil {
		t.Fatal("mismatched explicit type accepted")
	}
	private := resourceWrite(t, filepath.Join(root, "source/data.csv"), "name,path\nroads,roads.gpkg\n")
	if _, err = library.ImportNamed(private, "", ""); err == nil {
		t.Fatal("private type imported")
	}
	noMain := resourceWrite(t, filepath.Join(root, "source/missing.qpt"), "<Layout><LayoutItem id=\"map\"/></Layout>")
	if _, err = library.ImportNamed(noMain, "", ""); err == nil || !strings.Contains(err.Error(), "main_map") {
		t.Fatal(err)
	}
}
func TestRetiredLibraryScopeAndToolAssets(t *testing.T) {
	_, library := resourceProject(t)
	toolRoot := filepath.Join(filepath.Dir(filepath.Dir(library.BuiltinDir)), "tools/system/example/assets/resources")
	path := resourceWrite(t, filepath.Join(toolRoot, "docs/tool.md"), "tool instructions")
	writeManifest(t, path, Manifest{ID: "doc.tool", Type: "doc", Name: "工具说明"})
	library = NewLibrary(library.BuiltinDir, library.UserDir, library.Bindings)
	retired := resourceWrite(t, filepath.Join(library.UserDir, "docs/old.md"), "old")
	writeManifest(t, retired, Manifest{ID: "doc.old", Type: "doc", Name: "旧说明"})
	listed, err := library.List("all")
	if err != nil || len(listed) != 0 {
		t.Fatal(listed, err)
	}
	old, err := library.List("retired")
	if err != nil || len(old) != 1 || old[0].ID != "doc.old" {
		t.Fatal(old, err)
	}
	resolved := library.Resolve(context.Background(), Ref{Type: "doc", Name: "旧说明", Location: "公共资源库"}, Scope{})
	if !resourceHasProblem(resolved, "FLOW_RESOURCE_LIBRARY_TYPE_RETIRED", "warning") || resolved.Handle() == nil {
		t.Fatal(resolved)
	}
	tool := library.Resolve(context.Background(), Ref{ID: "doc.tool"}, Scope{})
	if tool.Owner != "tool" || tool.Handle() == nil {
		t.Fatal(tool)
	}
	byName := library.Resolve(context.Background(), Ref{Type: "doc", Name: "工具说明"}, Scope{})
	if !HasErrors(byName.Problems) {
		t.Fatal("tool asset found by name")
	}
	override := resourceWrite(t, filepath.Join(library.UserDir, "docs/override.md"), "override")
	writeManifest(t, override, Manifest{ID: "doc.override", Type: "doc", Name: "覆盖", Overrides: "doc.tool"})
	invalid, err := library.GetByID("doc.override")
	if err != nil || !resourceHasProblem(invalid, "RESOURCE_OVERRIDE_CONFLICT", "error") {
		t.Fatal(invalid, err)
	}
	tool = library.Resolve(context.Background(), Ref{ID: "doc.tool"}, Scope{})
	if tool.Owner != "tool" || tool.Handle() == nil {
		t.Fatal(tool)
	}
}

func TestStyleSetAliasReplacementKeepsIdentityAndFiles(t *testing.T) {
	root, library := resourceProject(t)
	folder := filepath.Join(root, "source/set")
	resourceWrite(t, filepath.Join(folder, "roads.qml"), "<qgis/>")
	original, err := library.ImportNamed(folder, "", "")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := library.ReplaceAliases(original.ID, map[string]string{"roads": "道路、主干路"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID != original.ID || changed.Version != "1.0.1" || changed.Digest == original.Digest {
		t.Fatal(changed)
	}
	if len(changed.Entries) != 1 || len(changed.Entries[0].Fields["aliases"].([]string)) != 2 {
		t.Fatal(changed.Entries)
	}
	if _, err = library.ReplaceAliases(original.ID, map[string]string{"missing": "invalid"}); err == nil {
		t.Fatal("unknown layer accepted")
	}
	current, _ := library.GetByID(original.ID)
	if current.Digest != changed.Digest {
		t.Fatal("failed edit modified resource")
	}
	staged, err := os.ReadDir(filepath.Join(library.UserDir, ".staging"))
	if err != nil || len(staged) != 0 {
		t.Fatal(staged, err)
	}
}
