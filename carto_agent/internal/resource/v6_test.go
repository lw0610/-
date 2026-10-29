package resource

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV6MigrationBackupIdempotencyAndPresets(t *testing.T) {
	_, lib := resourceProject(t)
	set := filepath.Join(lib.UserDir, "styles/旧组合")
	resourceWrite(t, filepath.Join(set, "对照表.csv"), "layer,style_file,label_field,aliases\n道路,line.qml,NAME,道路、交通\n区域,area.qml,无,行政区\n")
	resourceWrite(t, filepath.Join(set, "line.qml"), "<qgis><layerGeometryType>1</layerGeometryType></qgis>")
	resourceWrite(t, filepath.Join(set, "area.qml"), "<qgis><layerGeometryType>2</layerGeometryType></qgis>")
	writeManifest(t, set, Manifest{ID: "style_set.old", Type: "style_set", Name: "旧组合", Entry: "对照表.csv"})
	preset := resourceWrite(t, filepath.Join(lib.UserDir, "styles/route.csv"), "topology,side,route_type,draw_type,style_id\nstandard,neutral,route,bezier_arrow,red\n")
	writeManifest(t, preset, Manifest{ID: "route_style.custom", Type: "route_style", Name: "我的预设"})
	resourceWrite(t, filepath.Join(lib.UserDir, "styles/平面.qml"), "<qgis/>")
	rows, err := lib.MigrateV6()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatal(rows)
	}
	items, err := lib.List("all")
	if err != nil || len(items) != 3 {
		t.Fatal(items, err)
	}
	line, err := lib.Get("style", "旧组合-道路")
	if err != nil || line.LabelField != "NAME" || len(line.Tags) != 2 {
		t.Fatal(line, err)
	}
	if _, err = os.Stat(filepath.Join(filepath.Dir(lib.UserDir), "presets/route-cartography/styles/我的预设.csv")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(filepath.Dir(lib.UserDir), ".backups/resources-v5/styles/旧组合/对照表.csv")); err != nil {
		t.Fatal(err)
	}
	again, err := lib.MigrateV6()
	if err != nil || len(again) != 0 {
		t.Fatal(again, err)
	}
	mappings, err := lib.LegacyMigrationItems()
	if err != nil || len(mappings) != 4 {
		t.Fatal(mappings, err)
	}
	after, _ := lib.List("all")
	if len(after) != 3 {
		t.Fatal(after)
	}
}

func TestV6FlowMigrationRollsBackFailedBatch(t *testing.T) {
	root, lib := resourceProject(t)
	flow := filepath.Join(root, "flow")
	source := resourceWrite(t, filepath.Join(flow, "resources/styles/good.qml"), "<qgis/>")
	bad := resourceWrite(t, filepath.Join(flow, "resources/layouts/bad.qpt"), "not XML")
	if _, err := lib.MigrateFlowResources(flow); err == nil {
		t.Fatal("invalid package migrated")
	}
	items, _ := lib.List("all")
	if len(items) != 0 {
		t.Fatal("partial migration left live resources", items)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("original removed", err)
	}
	resourceWrite(t, bad, "<Layout><LayoutItem id=\"main_map\"/></Layout>")
	rows, err := lib.MigrateFlowResources(flow)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	again, err := lib.MigrateFlowResources(flow)
	if err != nil || len(again) != 2 || again[0].To != rows[0].To {
		t.Fatal(again, err)
	}
	items, _ = lib.List("all")
	if len(items) != 2 {
		t.Fatal(items)
	}
	if _, err = os.Stat(filepath.Join(flow, ".resources-v5-backup/styles/good.qml")); err != nil {
		t.Fatal(err)
	}
}

func testPreviewPNG(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, image.NewRGBA(image.Rect(0, 0, 8, 8)))
}

func TestV6PreviewCacheDigestAndInvalidation(t *testing.T) {
	root, lib := resourceProject(t)
	source := resourceWrite(t, filepath.Join(root, "source/line.qml"), "<qgis><layerGeometryType>1</layerGeometryType></qgis>")
	item, err := lib.Import(source, "style")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	render := func(_ context.Context, r *ResolvedResource, path string) error { calls++; return testPreviewPNG(path) }
	ready, err := lib.GeneratePreview(context.Background(), item.ID, render, false)
	if err != nil || ready.PreviewStatus != "ready" {
		t.Fatal(ready, err)
	}
	current, _ := lib.GetByID(item.ID)
	if current.Digest != item.Digest {
		t.Fatal("preview altered content digest")
	}
	if _, err = lib.GeneratePreview(context.Background(), item.ID, render, false); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	field := "NAME"
	changed, err := lib.UpdateMetadata(item.ID, MetadataUpdate{LabelField: &field})
	if err != nil {
		t.Fatal(err)
	}
	_, valid, err := lib.PreviewPath(item.ID)
	if err != nil || valid || changed.Digest == item.Digest {
		t.Fatal(valid, changed, err)
	}
	if _, err = lib.GeneratePreview(context.Background(), item.ID, func(_ context.Context, _ *ResolvedResource, path string) error {
		return os.WriteFile(path, []byte("not PNG"), 0600)
	}, true); err == nil {
		t.Fatal("invalid image accepted")
	}
	_, valid, _ = lib.PreviewPath(item.ID)
	if valid {
		t.Fatal("failed render published cache")
	}
	if _, err = lib.GeneratePreview(context.Background(), item.ID, render, false); err != nil || calls != 2 {
		t.Fatal(err, calls)
	}
	// A concurrent resource edit invalidates the snapshot while rendering.
	_, err = lib.GeneratePreview(context.Background(), item.ID, func(_ context.Context, _ *ResolvedResource, path string) error {
		name := "New name"
		if _, e := lib.UpdateMetadata(item.ID, MetadataUpdate{Name: &name}); e != nil {
			return e
		}
		return testPreviewPNG(path)
	}, true)
	if err == nil {
		t.Fatal("stale render was published")
	}
}

func TestV6BuiltinPreviewCacheDoesNotWriteBuiltin(t *testing.T) {
	_, lib := resourceProject(t)
	path := resourceWrite(t, filepath.Join(lib.BuiltinDir, "styles/line.qml"), "<qgis/>")
	writeManifest(t, path, Manifest{ID: "style.builtin", Type: "style", Name: "线"})
	result, err := lib.GeneratePreview(context.Background(), "style.builtin", func(_ context.Context, _ *ResolvedResource, path string) error { return testPreviewPNG(path) }, false)
	if err != nil || result.PreviewStatus != "ready" {
		t.Fatal(result, err)
	}
	pngPath, valid, err := lib.PreviewPath(result.ID)
	if err != nil || !valid || !inside(pngPath, lib.UserDir) {
		t.Fatal(pngPath, valid, err)
	}
	items, _ := lib.List("builtin")
	if len(items) != 1 || items[0].PreviewStatus != "ready" {
		t.Fatal(items)
	}
	if _, err = os.Stat(filepath.Join(lib.BuiltinDir, "styles/preview.png")); !os.IsNotExist(err) {
		t.Fatal("modified built-in directory")
	}
}

func TestPreviewRendererUpgradeInvalidatesOldImageWithoutChangingContent(t *testing.T) {
	root, lib := resourceProject(t)
	source := resourceWrite(t, filepath.Join(root, "source/line.qml"), "<qgis/>")
	item, err := lib.Import(source, "style")
	if err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(item.Root, "preview.png")
	if err = testPreviewPNG(imagePath); err != nil {
		t.Fatal(err)
	}
	resourceWrite(t, filepath.Join(item.Root, "preview.digest"), item.Digest+"\n")
	if _, ready, err := lib.PreviewPath(item.ID); err != nil || ready {
		t.Fatal("old map-like preview accepted", ready, err)
	}
	renders := 0
	updated, err := lib.GeneratePreview(context.Background(), item.ID, func(_ context.Context, _ *ResolvedResource, path string) error {
		renders++
		return testPreviewPNG(path)
	}, false)
	if err != nil || renders != 1 || updated.Digest != item.Digest {
		t.Fatal(updated, renders, err)
	}
	if _, ready, err := lib.PreviewPath(item.ID); err != nil || !ready {
		t.Fatal("new symbol preview unavailable", ready, err)
	}
}

func TestV6ForcePreviewUsesUserCacheOverShippedPreview(t *testing.T) {
	_, lib := resourceProject(t)
	path := resourceWrite(t, filepath.Join(lib.BuiltinDir, "styles/line.qml"), "<qgis/>")
	writeManifest(t, path, Manifest{ID: "style.force", Type: "style", Name: "强制预览"})
	item, err := lib.GetByID("style.force")
	if err != nil {
		t.Fatal(err)
	}
	shipped := filepath.Join(filepath.Dir(path), "preview.png")
	if err = testPreviewPNG(shipped); err != nil {
		t.Fatal(err)
	}
	resourceWrite(t, filepath.Join(filepath.Dir(path), "preview.digest"), previewStamp(item.Digest))
	before, _ := os.ReadFile(shipped)
	_, err = lib.GeneratePreview(context.Background(), item.ID, func(_ context.Context, _ *ResolvedResource, p string) error {
		if inside(p, lib.BuiltinDir) {
			t.Fatal("renderer target is read-only builtin")
		}
		return testPreviewPNG(p)
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	preview, ready, err := lib.PreviewPath(item.ID)
	if err != nil || !ready || !inside(preview, lib.UserDir) {
		t.Fatal(preview, ready, err)
	}
	after, _ := os.ReadFile(shipped)
	if string(before) != string(after) {
		t.Fatal("shipped image changed")
	}
}

func TestV6UniqueNamesAcrossTypesAndResourceExport(t *testing.T) {
	root, lib := resourceProject(t)
	style := resourceWrite(t, filepath.Join(root, "in/line.qml"), "<qgis/>")
	imported, err := lib.ImportNamed(style, "style", "道路")
	if err != nil {
		t.Fatal(err)
	}
	layout := resourceWrite(t, filepath.Join(root, "in/map.qpt"), "<Layout><LayoutItem id=\"main_map\"/></Layout>")
	if _, err = lib.ImportNamed(layout, "layout", "道路"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	output := filepath.Join(root, "out/resource.zip")
	if _, err = lib.Export(context.Background(), imported.ID, output); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	manifest, payload := false, false
	for _, f := range z.File {
		if strings.HasSuffix(f.Name, "resource.yaml") {
			manifest = true
		}
		if strings.HasSuffix(f.Name, ".qml") {
			payload = true
		}
		if strings.Contains(f.Name, "..") || strings.HasPrefix(f.Name, "/") {
			t.Fatal(f.Name)
		}
	}
	if !manifest || !payload {
		t.Fatal(z.File)
	}
	if _, err = lib.Export(context.Background(), imported.ID, output); err == nil {
		t.Fatal("export silently overwrote existing file")
	}
}

func TestV6QueuedPreviewSurvivesReconnectAndProcessRestart(t *testing.T) {
	root, lib := resourceProject(t)
	source := resourceWrite(t, filepath.Join(root, "sample.qml"), "<qgis/>")
	item, err := lib.Import(source, "style")
	if err != nil {
		t.Fatal(err)
	}
	render := func(_ context.Context, _ *ResolvedResource, p string) error { return testPreviewPNG(p) }
	if _, err = lib.GeneratePreview(context.Background(), item.ID, render, false); err != nil {
		t.Fatal(err)
	}
	queued, err := lib.QueuePreview(item.ID)
	if err != nil || queued.PreviewStatus != "pending" {
		t.Fatal(queued, err)
	}
	restarted := NewLibrary(lib.BuiltinDir, lib.UserDir, lib.Bindings)
	_, ready, err := restarted.PreviewPath(item.ID)
	if err != nil || ready {
		t.Fatal(ready, err)
	}
	if _, err = restarted.GeneratePreview(context.Background(), item.ID, render, false); err != nil {
		t.Fatal(err)
	}
	_, ready, err = restarted.PreviewPath(item.ID)
	if err != nil || !ready {
		t.Fatal(ready, err)
	}
}

func TestV6BatchImportRollsBackInvalidMember(t *testing.T) {
	root, lib := resourceProject(t)
	folder := filepath.Join(root, "input")
	resourceWrite(t, filepath.Join(folder, "a.qml"), "<qgis/>")
	resourceWrite(t, filepath.Join(folder, "z.qml"), "invalid XML")
	if _, err := lib.ImportMany(folder); err == nil {
		t.Fatal("invalid batch imported")
	}
	items, err := lib.List("user")
	if err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
}

func TestV6QPTMetadataIsReadWithoutQGIS(t *testing.T) {
	root, lib := resourceProject(t)
	source := resourceWrite(t, filepath.Join(root, "map.qpt"), `<Layout><PageCollection><LayoutItem size="297,210,mm"/></PageCollection><LayoutItem id="main_map"/><LayoutItem id="title"/></Layout>`)
	item, err := lib.Import(source, "layout")
	if err != nil {
		t.Fatal(err)
	}
	if item.Paper != "A4" || item.Orientation != "landscape" || len(item.Elements) != 2 {
		t.Fatal(item)
	}
}

func TestV6LegacyContentReaderIsConfinedToMigratedOriginals(t *testing.T) {
	root, lib := resourceProject(t)
	csv := resourceWrite(t, filepath.Join(lib.UserDir, "data/旧清单.csv"), "名称,路径,自定义说明\n道路,roads.geojson,用户补充\n")
	writeManifest(t, csv, Manifest{ID: "data_list.old", Type: "data_list", Name: "旧清单"})
	doc := resourceWrite(t, filepath.Join(lib.UserDir, "docs/旧说明.md"), "# 原始说明\n用户内容\n")
	writeManifest(t, doc, Manifest{ID: "doc.old", Type: "doc", Name: "旧说明"})
	if _, err := lib.MigrateV6(); err != nil {
		t.Fatal(err)
	}
	content, err := lib.ReadLegacyContent("data_list", "旧清单", "data_list.old")
	if err != nil || !strings.Contains(content, "道路,roads.geojson,用户补充") || !strings.Contains(content, "自定义说明") {
		t.Fatal(content, err)
	}
	content, err = lib.ReadLegacyContent("doc", "旧说明", "doc.old")
	if err != nil || content != "# 原始说明\n用户内容\n" {
		t.Fatal(content, err)
	}
	if _, err = lib.ReadLegacyContent("style", "旧说明", ""); err == nil {
		t.Fatal("public style accepted by legacy reader")
	}
	outside := resourceWrite(t, filepath.Join(root, "external.md"), "must not read")
	raw, _ := json.Marshal([]MigrationItem{{From: outside, Type: "doc", Name: "外部", PreviousName: "外部"}})
	resourceWrite(t, filepath.Join(lib.UserDir, ".migration-v6-report.json"), string(raw))
	if content, err = lib.ReadLegacyContent("doc", "外部", ""); err == nil || content != "" {
		t.Fatal(content, err)
	}
}

func TestV6ImportManyPreservesNestedManagedPackagesAndSVG(t *testing.T) {
	root, lib := resourceProject(t)
	parent := filepath.Join(root, "packages")
	for i, name := range []string{"道路符号", "站点符号"} {
		folder := filepath.Join(parent, "nested", name)
		resourceWrite(t, filepath.Join(folder, "map.qml"), `<qgis><layerGeometryType>0</layerGeometryType><Option name="name" value="icons/marker.svg"/></qgis>`)
		resourceWrite(t, filepath.Join(folder, "icons/marker.svg"), `<svg xmlns="http://www.w3.org/2000/svg"><circle r="3"/></svg>`)
		// Supporting QML must remain inside this package, not become a third item.
		resourceWrite(t, filepath.Join(folder, "alternate.qml"), "<qgis/>")
		writeManifest(t, folder, Manifest{ID: fmt.Sprintf("style.package-%d", i), Type: "style", Name: name, Entry: "map.qml", Description: "保留包说明", Tags: []string{"交通"}, LabelField: "name", Source: ResourceSource{Author: "用户作者"}})
	}
	batch, err := lib.ImportMany(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Items) != 2 {
		t.Fatal(batch)
	}
	for _, item := range batch.Items {
		if item.Description != "保留包说明" || item.LabelField != "name" || len(item.Tags) != 1 || item.Source.Author != "用户作者" {
			t.Fatal(item)
		}
		for _, rel := range []string{"icons/marker.svg", "alternate.qml", "resource.yaml"} {
			if _, err = os.Stat(filepath.Join(item.Root, rel)); err != nil {
				t.Fatal(rel, err)
			}
		}
		qml, _ := os.ReadFile(item.Entry)
		if !strings.Contains(string(qml), "icons/marker.svg") {
			t.Fatal(string(qml))
		}
	}
}

func TestV6LegacyContentReaderSupportsSafePackageEntries(t *testing.T) {
	root, lib := resourceProject(t)
	data := filepath.Join(lib.UserDir, "data/旧数据包")
	text := "名称,路径,用户列\n道路,roads.geojson,必须保留\n"
	resourceWrite(t, filepath.Join(data, "tables/list.csv"), text)
	writeManifest(t, data, Manifest{ID: "data_list.package", Type: "data_list", Name: "旧数据包", Entry: "tables/list.csv"})
	doc := filepath.Join(lib.UserDir, "docs/旧说明包")
	resourceWrite(t, filepath.Join(doc, "text/notes.md"), "# 包内说明\n必须保留\n")
	writeManifest(t, doc, Manifest{ID: "doc.package", Type: "doc", Name: "旧说明包", Entry: "text/notes.md"})
	if _, err := lib.MigrateV6(); err != nil {
		t.Fatal(err)
	}
	content, err := lib.ReadLegacyContent("data_list", "旧数据包", "data_list.package")
	if err != nil || content != text {
		t.Fatal(content, err)
	}
	content, err = lib.ReadLegacyContent("doc", "旧说明包", "doc.package")
	if err != nil || !strings.Contains(content, "包内说明") {
		t.Fatal(content, err)
	}
	archived := filepath.Join(lib.UserDir, ".migration-v6/docs/旧说明包")
	outside := resourceWrite(t, filepath.Join(root, "outside.md"), "不能读取")
	entry := filepath.Join(archived, "text/notes.md")
	if err = os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, entry); err != nil {
		t.Fatal(err)
	}
	if content, err = lib.ReadLegacyContent("doc", "旧说明包", "doc.package"); err == nil || content != "" {
		t.Fatal("followed archive entry symlink", content, err)
	}
	resourceWrite(t, filepath.Join(archived, "resource.yaml"), "apiVersion: resource/v1\nid: doc.package\ntype: doc\nname: 旧说明包\nentry: ../../outside.md\n")
	if content, err = lib.ReadLegacyContent("doc", "旧说明包", "doc.package"); err == nil || content != "" {
		t.Fatal("read escaping archive entry", content, err)
	}
}
