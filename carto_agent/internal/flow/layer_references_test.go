package flow

import (
	"archive/zip"
	"github.com/autocarto/carto_agent/internal/layers"
	"github.com/autocarto/carto_agent/internal/resource"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuideLayerReferencesAndLock(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	item := layers.Item{Manifest: layers.Manifest{ID: "layer.user.test", Name: "省域", Digest: digest}, Status: "ready"}
	g := ParseGuide("---\nname: 测试\ndescription: 测试引用\nversion: 1.0.0\n---\n用图层「省域」和内置图层「未安装」，样式「省域面」。", GuideEnvironment{Layers: []layers.Item{item}})
	if len(g.Layers) != 2 || len(g.Resources) != 1 || g.Layers[0].ID != item.ID {
		t.Fatalf("%+v", g)
	}
	missing := false
	for _, d := range g.Diagnostics {
		missing = missing || d.Code == "FLOW_LAYER_NOT_FOUND"
	}
	if !missing {
		t.Fatal(g.Diagnostics)
	}
	lock := retainedLock(&resource.Lock{APIVersion: resource.LockAPIVersion}, &FlowDoc{Layers: g.Layers})
	if len(lock.Layers) != 1 || lock.Layers[0].Digest != digest {
		t.Fatal(lock)
	}
	if e := lock.Validate(); e != nil {
		t.Fatal(e)
	}
	g.Layers[0].Digest = "sha256:" + strings.Repeat("b", 64)
	locked := retainedLock(lock, &FlowDoc{Layers: g.Layers})
	if locked.Layers[0].Digest != digest {
		t.Fatal("silently relocked changed data")
	}
}

func TestFlowLayerReferenceExportImportDoesNotBundleOriginal(t *testing.T) {
	assets, e := filepath.Abs("../../../autocarto")
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("AUTOCARTO_ASSETS_ROOT", assets)
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	catalog, e := layers.NewDefaultLibrary()
	if e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(t.TempDir(), "private.geojson")
	os.WriteFile(file, []byte(`{"type":"FeatureCollection","features":[]}`), 0600)
	item, e := catalog.Save(layers.Manifest{APIVersion: "layer/v1", Name: "测试私有河流", Pack: "测试", File: file, Category: "hydro.river_line", Geometry: "line", CRS: "EPSG:4326"})
	if e != nil {
		t.Fatal(e)
	}
	store := guideTestStore(t)
	guide, e := store.Create("---\n名称: 引用导出\n说明: 测试\n---\n图层「测试私有河流」", "layer-export")
	if e != nil {
		t.Fatal(e)
	}
	lock, e := ReadLock(guide.Dir)
	if e != nil || len(lock.Layers) != 1 || lock.Layers[0].ID != item.ID {
		t.Fatal(lock, e)
	}
	target := filepath.Join(t.TempDir(), "flow.zip")
	if _, e = store.Packages.ExportWithOptions(guide.Meta.ID, target, ExportOptions{}); e != nil {
		t.Fatal(e)
	}
	archive, e := zip.OpenReader(target)
	if e != nil {
		t.Fatal(e)
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if strings.HasSuffix(entry.Name, ".geojson") {
			t.Fatal("original data bundled")
		}
	}
	if e = catalog.Delete(item.ID); e != nil {
		t.Fatal(e)
	}
	dest := guideTestStore(t)
	imported, e := dest.Import(target, ImportOptions{})
	if e != nil {
		t.Fatal(e)
	}
	missing := false
	for _, d := range imported.Diagnostics {
		missing = missing || d.Code == "FLOW_LAYER_NOT_FOUND"
	}
	if !missing {
		t.Fatal(imported.Diagnostics)
	}
}
