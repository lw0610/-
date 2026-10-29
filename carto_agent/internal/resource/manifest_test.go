package resource

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"gopkg.in/yaml.v3"
)

func writeManifest(t *testing.T, path string, m Manifest) {
	t.Helper()
	if m.APIVersion == "" {
		m.APIVersion = "resource/v1"
	}
	if m.Name == "" {
		m.Name = resourceBaseName(path, m.Type)
	}
	if m.Entry == "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			m.Entry = filepath.Base(path)
		}
	}
	raw, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	resourceWrite(t, ManifestPath(path, m.Type), string(raw))
}
func TestManifestValidation(t *testing.T) {
	valid := Manifest{APIVersion: "resource/v1", ID: "doc.test", Type: "doc", Name: "测试", Entry: "test.md"}
	for _, test := range []struct {
		name   string
		change func(*Manifest)
	}{
		{"api", func(m *Manifest) { m.APIVersion = "v2" }}, {"id", func(m *Manifest) { m.ID = "doc.测试" }}, {"prefix", func(m *Manifest) { m.ID = "style.test" }}, {"required", func(m *Manifest) { m.ID = "" }}, {"name", func(m *Manifest) { m.Name = "坏/名" }}, {"version", func(m *Manifest) { m.Version = "1.0" }}, {"entry", func(m *Manifest) { m.Entry = "../test.md" }}, {"extension", func(m *Manifest) { m.Entry = "test.qml" }}, {"description", func(m *Manifest) { m.Description = strings.Repeat("长", 501) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := valid
			test.change(&m)
			if ValidateManifest(&m, true) == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	if err := ValidateManifest(&valid, true); err != nil || valid.Version != "1.0.0" {
		t.Fatal(valid, err)
	}
	valid.ID = ""
	if err := ValidateManifest(&valid, false); err != nil {
		t.Fatal(err)
	}
}
func TestResourceIdentityResolutionAndOverrides(t *testing.T) {
	_, library := resourceProject(t)
	ctx := context.Background()
	builtin := resourceWrite(t, filepath.Join(library.BuiltinDir, "styles/builtin.qml"), "<qgis><note>builtin</note></qgis>")
	writeManifest(t, builtin, Manifest{ID: "style.builtin", Type: "style", Name: "说明"})
	user := resourceWrite(t, filepath.Join(library.UserDir, "styles/user.qml"), "<qgis><note>user</note></qgis>")
	writeManifest(t, user, Manifest{ID: "style.user", Type: "style", Name: "说明"})
	r := library.Resolve(ctx, Ref{Type: "style", Name: "说明"}, Scope{})
	if !resourceHasProblem(r, "FLOW_RESOURCE_AMBIGUOUS", "error") || r.Handle() != nil {
		t.Fatal(r)
	}
	choices := r.Problems[0].Fix["candidates"].([]map[string]any)
	if len(choices) != 2 || choices[0]["owner"] == nil || choices[0]["version"] == nil {
		t.Fatal(choices)
	}
	locked := library.Resolve(ctx, Ref{Type: "style", Name: "说明"}, Scope{Lock: &Lock{Resources: []LockEntry{{Type: "style", Name: "说明", ID: "style.builtin"}}}})
	if locked.ID != "style.builtin" {
		t.Fatal(locked)
	}
	writeManifest(t, user, Manifest{ID: "style.user", Type: "style", Name: "自选说明", Overrides: "style.builtin"})
	for _, ref := range []Ref{{Type: "style", Name: "说明"}, {Type: "style", ID: "style.builtin"}, {Type: "style", Location: "公共资源库/style.builtin"}} {
		r = library.Resolve(ctx, ref, Scope{})
		resourceNoErrors(t, r)
		if r.ID != "style.user" || !resourceHasProblem(r, "FLOW_RESOURCE_OVERRIDDEN", "info") {
			t.Fatal(r)
		}
	}
	exact, err := library.GetByID("style.builtin")
	if err != nil || exact.ID != "style.builtin" || exact.OverriddenBy != "style.user" {
		t.Fatal(exact, err)
	}
	other := resourceWrite(t, filepath.Join(library.UserDir, "styles/other.qml"), "<qgis><note>other</note></qgis>")
	writeManifest(t, other, Manifest{ID: "style.other", Type: "style", Name: "另一个", Overrides: "style.builtin"})
	r = library.Resolve(ctx, Ref{Type: "style", ID: "style.builtin"}, Scope{})
	if r.ID != "style.builtin" {
		t.Fatal(r)
	}
	conflict, err := library.GetByID("style.user")
	if err != nil || !resourceHasProblem(conflict, "RESOURCE_OVERRIDE_CONFLICT", "error") {
		t.Fatal(conflict, err)
	}
	if err = os.Remove(other); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(user); err != nil {
		t.Fatal(err)
	}
	r = library.Resolve(ctx, Ref{Type: "style", Name: "说明"}, Scope{})
	if r.ID != "style.builtin" {
		t.Fatal(r)
	}
	missing := library.Resolve(ctx, Ref{Type: "style", Name: "说明"}, Scope{Lock: &Lock{Resources: []LockEntry{{Type: "style", Name: "说明", ID: "style.deleted"}}}})
	if !resourceHasProblem(missing, "FLOW_RESOURCE_LOCK_MISSING", "error") {
		t.Fatal(missing)
	}
}
func TestManifestConflictsLegacyTypeAndDigest(t *testing.T) {
	root, library := resourceProject(t)
	ctx := context.Background()
	a := resourceWrite(t, filepath.Join(root, "flow/resources/layouts/a.qpt"), "<Layout><LayoutItem id=\"main_map\"/></Layout>")
	resourceWrite(t, filepath.Join(root, "flow/resources/layouts/a.layout.yaml"), "name: A\npage: {}\nfonts_mm: {}\nelements: {main_map: {}}\n")
	r := library.Resolve(ctx, Ref{Type: "layout", Location: "resources/layouts/a.qpt"}, Scope{FlowDir: filepath.Join(root, "flow")})
	if !resourceHasProblem(r, "RESOURCE_NAME_CONFLICT", "error") {
		t.Fatal(r, a)
	}
	path := resourceWrite(t, filepath.Join(root, "flow/resources/docs/test.md"), "hello")
	r = library.Resolve(ctx, Ref{Type: "doc", Location: path}, Scope{})
	if !r.Legacy || !strings.HasPrefix(r.ID, "legacy.doc.") || r.Version != "0.0.0" || !resourceHasProblem(r, "FLOW_RESOURCE_MANIFEST_MISSING", "info") || !resourceHasProblem(r, "FLOW_RESOURCE_PATH_EXTERNAL", "warning") || resourceHasProblem(r, "FLOW_RESOURCE_ABSOLUTE_PATH", "info") {
		t.Fatal(r)
	}
	writeManifest(t, path, Manifest{ID: "doc.test", Type: "doc"})
	r = library.Resolve(ctx, Ref{Type: "data_list", Location: path}, Scope{})
	if r.Type != "doc" || !resourceHasProblem(r, "FLOW_RESOURCE_TYPE_MISMATCH", "error") || r.Handle() != nil {
		t.Fatal(r)
	}
	r = library.Resolve(ctx, Ref{Type: "doc", Location: path}, Scope{})
	digest := r.Digest
	copyPath := resourceWrite(t, filepath.Join(root, "copy/test.md"), "hello")
	writeManifest(t, copyPath, Manifest{ID: "doc.test", Type: "doc"})
	copied := library.Resolve(ctx, Ref{Type: "doc", Location: copyPath}, Scope{})
	if copied.Digest != digest {
		t.Fatalf("digest depends on machine path: %s %s", digest, copied.Digest)
	}
	resourceWrite(t, path, "changed content")
	changed := library.Resolve(ctx, Ref{Type: "doc", Location: path}, Scope{})
	if changed.Digest == digest {
		t.Fatal("content change was not detected")
	}
	list := resourceWrite(t, filepath.Join(root, "list.csv"), "name,path\n道路,data.geojson\n")
	data := resourceWrite(t, filepath.Join(root, "data.geojson"), "{}")
	before := library.Resolve(ctx, Ref{Type: "data_list", Location: list}, Scope{})
	resourceWrite(t, data, "{\"changed\":true}")
	after := library.Resolve(ctx, Ref{Type: "data_list", Location: list}, Scope{})
	if before.Digest != after.Digest {
		t.Fatal("external data entered resource digest")
	}
}
func TestResourceHandleFieldsAndLinkedBaseOverride(t *testing.T) {
	root, library := resourceProject(t)
	ctx := context.Background()
	flowDir := filepath.Join(root, "flow")
	list := resourceWrite(t, filepath.Join(flowDir, "resources/data/list.csv"), "name,path,required,description\n行政区,regions.gpkg|layername=regions,是,测试\n")
	resourceWrite(t, filepath.Join(root, "first/regions.gpkg"), "gpkg1")
	resourceWrite(t, filepath.Join(root, "second/regions.gpkg"), "gpkg2")
	ref := Ref{Type: "data_list", Name: "地区", Location: list, BaseDir: "first"}
	r := library.Resolve(ctx, ref, Scope{FlowDir: flowDir, FlowID: "test"})
	resourceNoErrors(t, r)
	if r.Handle() == nil || r.Entries[0].Fields["required"] != true || !strings.HasSuffix(r.Entries[0].Path, "regions.gpkg|layername=regions") {
		t.Fatal(r)
	}
	linked := library.Resolve(ctx, Ref{Type: "data_list", Name: "地区", BaseDir: "second"}, Scope{FlowID: "test", FlowDir: flowDir, Linked: []*ResolvedResource{r}})
	resourceNoErrors(t, linked)
	if linked.BaseDir != filepath.Join(root, "second") || linked.ID != r.ID || linked.Label != r.Label || linked.Key != r.Key {
		t.Fatal(linked)
	}
	if _, err := os.Stat(filepath.Join(root, "autocarto_data/config/resource_bindings.yaml")); !os.IsNotExist(err) {
		t.Fatal("single-call override persisted binding", err)
	}
	missing := library.Resolve(ctx, Ref{Type: "data_list", Location: list}, Scope{})
	if missing.Handle() != nil {
		t.Fatal("missing required file made a handle")
	}
	set := filepath.Join(flowDir, "resources/styles/set")
	resourceWrite(t, filepath.Join(set, "custom.csv"), "图层,别名,样式文件,标注字段\n保留,A、B,line.qml,\n关闭,,line.qml,无\n字段,,line.qml,NAME\n")
	resourceWrite(t, filepath.Join(set, "line.qml"), "<qgis><layerGeometryType>1</layerGeometryType></qgis>")
	writeManifest(t, set, Manifest{ID: "style_set.custom", Type: "style_set", Entry: "custom.csv"})
	styles := library.Resolve(ctx, Ref{Type: "style_set", Location: set}, Scope{FlowDir: flowDir})
	resourceNoErrors(t, styles)
	for i, mode := range []string{"keep", "off", "field"} {
		if styles.Entries[i].Fields["label_mode"] != mode || styles.Entries[i].Fields["geometry"] != "line" {
			t.Fatal(styles.Entries)
		}
	}
	if !reflect.DeepEqual(styles.Entries[0].Fields["aliases"], []string{"A", "B"}) {
		t.Fatal(styles.Entries)
	}
	clone := library.Resolve(ctx, Ref{Type: "style_set", Name: styles.Label}, Scope{Linked: []*ResolvedResource{styles}})
	if !reflect.DeepEqual(clone, styles) {
		t.Fatal("linked snapshot lost normalized types")
	}
	clone.Entries[0].Fields["label_mode"] = "changed"
	if styles.Entries[0].Fields["label_mode"] != "keep" {
		t.Fatal("snapshot mutated original")
	}
}
func TestBuiltinResourcesHaveManifests(t *testing.T) {
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	library := NewLibrary(filepath.Join(repo, "autocarto/resources/system"), t.TempDir(), bindings.New(filepath.Join(t.TempDir(), "bindings.yaml")))
	items, err := library.List("builtin")
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{"四角对齐版式": false, "统计专题版式": false, "省域面": false, "县级城市驻地": false, "铁路": false, "高速公路": false, "国道": false, "省界": false, "河流线": false, "河流面": false, "县域面": false, "市域面": false, "地级城市驻地": false}
	if len(items) != len(expected) {
		t.Fatalf("expected 11 styles and 2 layouts, got %d", len(items))
	}
	for _, item := range items {
		if _, ok := expected[item.Name]; !ok || item.Legacy || HasErrors(item.Problems) || !strings.HasPrefix(item.Digest, "sha256:") || item.PreviewStatus != "ready" {
			t.Fatalf("invalid builtin resource: %+v", item)
		}
		if item.Type == "style" && (item.Geometry == "" || item.LabelField == "") {
			t.Fatalf("style metadata missing: %+v", item)
		}
		expected[item.Name] = true
	}
	for name, seen := range expected {
		if !seen {
			t.Errorf("missing builtin: %s", name)
		}
	}
}
