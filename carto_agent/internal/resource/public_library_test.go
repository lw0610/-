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
	result, err := library.ImportMany(folder)
	if err != nil || len(result.Items) != 2 {
		t.Fatal(result, err)
	}
	if _, err = os.Stat(filepath.Join(folder, "对照表.csv")); !os.IsNotExist(err) {
		t.Fatal("source was modified", err)
	}
	if result.Items[0].Name != "河流" || result.Items[0].Type != "style" {
		t.Fatal(result.Items)
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
func TestRetiredLibraryTypesAreMigrationOnly(t *testing.T) {
	_, library := resourceProject(t)
	retired := resourceWrite(t, filepath.Join(library.UserDir, "docs/old.md"), "old")
	writeManifest(t, retired, Manifest{ID: "doc.old", Type: "doc", Name: "旧说明"})
	rows, err := library.List("all")
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	r := library.Resolve(context.Background(), Ref{Type: "doc", Name: "旧说明"}, Scope{})
	if r.Handle() != nil {
		t.Fatal("retired document exposed as public resource")
	}
	migrations, err := library.MigrateV6()
	if err != nil || len(migrations) != 1 {
		t.Fatal(migrations, err)
	}
	if _, err = os.Stat(filepath.Join(filepath.Dir(library.UserDir), ".backups/resources-v5/docs/old.md")); err != nil {
		t.Fatal(err)
	}
	again, err := library.MigrateV6()
	if err != nil || len(again) != 0 {
		t.Fatal(again, err)
	}
}

func TestMetadataEditKeepsIdentityAndContent(t *testing.T) {
	root, library := resourceProject(t)
	source := resourceWrite(t, filepath.Join(root, "source/roads.qml"), "<qgis><layerGeometryType>1</layerGeometryType></qgis>")
	original, err := library.ImportNamed(source, "style", "")
	if err != nil {
		t.Fatal(err)
	}
	name, description, field := "新道路", "道路线符号", "NAME"
	tags := []string{"道路", "主干路"}
	changed, err := library.UpdateMetadata(original.ID, MetadataUpdate{Name: &name, Description: &description, Tags: &tags, LabelField: &field})
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID != original.ID || changed.Name != name || changed.Digest == original.Digest || changed.Root != original.Root || changed.LabelField != field || len(changed.Tags) != 2 {
		t.Fatal(changed)
	}
	raw, _ := os.ReadFile(changed.Entry)
	if string(raw) != "<qgis><layerGeometryType>1</layerGeometryType></qgis>" {
		t.Fatal("metadata changed payload")
	}
	bad := "../invalid"
	if _, err = library.UpdateMetadata(original.ID, MetadataUpdate{Name: &bad}); err == nil {
		t.Fatal("invalid name accepted")
	}
	current, _ := library.GetByID(original.ID)
	if current.Digest != changed.Digest {
		t.Fatal("failed edit modified resource")
	}
}
