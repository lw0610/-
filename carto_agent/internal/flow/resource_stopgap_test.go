package flow

import (
	"context"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"os"
	"path/filepath"
	"testing"
)

func TestResourceTypeWordsAreIndependentOfLocation(t *testing.T) {
	doc := Parse("## 使用的资源\n- 样式「道路」：公共资源库\n- 样式集「底图」：公共资源库\n")
	if len(doc.Resources) != 2 || doc.Resources[0].Type != "style" || doc.Resources[1].Type != "style_set" {
		t.Fatalf("%+v", doc.Resources)
	}
}
func TestShapefileSidecarChangesLinkFingerprint(t *testing.T) {
	env := fixtureEnvironment()
	raw := testFlow
	compiler, store, _ := compilerFixture(t, raw, &env)
	stored, err := store.Get("test-flow")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "data.shp")
	for _, file := range append([]string{path}, resource.ShapefileSidecarPaths(path)...) {
		if err = os.WriteFile(file, []byte("first"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := &compilationSource{stored: stored, env: env, resources: []*resource.ResolvedResource{{Type: "data_list", Entries: []resource.ResourceEntry{{Path: path}}}}}
	first, err := compiler.linkFingerprint(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(path), "data.dbf"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := compiler.linkFingerprint(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("dbf修改未改变指纹")
	}
}
func TestFlowAndLibraryShareInjectedBindingPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	file := filepath.Join(root, "custom-config", "bindings.yaml")
	store := NewStore("", filepath.Join(root, "flows"), bindings.New(file))
	library := resource.NewLibrary("", filepath.Join(root, "resources"), store.Bindings)
	if store.Bindings != library.Bindings {
		t.Fatal("binding stores must share the injected pointer")
	}
	dir := filepath.Join(root, "data")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := store.SetBinding("test-flow", "path:data/base.csv", dir); err != nil {
		t.Fatal(err)
	}
	got, found, err := library.Bindings.Get("flow:test-flow/path:data/base.csv")
	if err != nil || !found || got != dir {
		t.Fatalf("%s %v %v", got, found, err)
	}
}
