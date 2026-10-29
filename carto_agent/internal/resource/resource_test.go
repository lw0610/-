package resource

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"gopkg.in/yaml.v3"
)

func resourceProject(t *testing.T) (string, *Library) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	library := NewLibrary(filepath.Join(root, "autocarto/resources/system"), filepath.Join(root, "autocarto_data/resources"), bindings.New(filepath.Join(root, "autocarto_data/config/resource_bindings.yaml")))
	return root, library
}

func resourceWrite(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func resourceHasProblem(resource *ResolvedResource, code, level string) bool {
	for _, problem := range resource.Problems {
		if problem.Code == code && (level == "" || problem.Level == level) {
			return true
		}
	}
	return false
}
func resourceNoErrors(t *testing.T, result *ResolvedResource) {
	t.Helper()
	for _, problem := range result.Problems {
		if problem.Level == "error" {
			t.Fatalf("资源有错误：%+v", result)
		}
	}
}

func TestResolveAllResourceTypes(t *testing.T) {
	tests := []struct {
		name, kind, location string
		files                map[string]string
		entries              int
	}{
		{"数据清单", "data_list", "resources/data/list.csv", map[string]string{"resources/data/list.csv": "\ufeff名称,路径,类型,必需,说明\n道路,road.geojson,矢量,是,原始道路\n高程,height.tif,栅格,是,DEM\n", "resources/data/road.geojson": "{}", "resources/data/height.tif": "data"}, 2},
		{"数据文件夹", "data_folder", "resources/data/folder", map[string]string{"resources/data/folder/a.geojson": "{}", "resources/data/folder/child/elevation.tif": "raster", "resources/data/folder/README.md": "说明"}, 2},
		{"样式", "style", "resources/styles/a.qml", map[string]string{"resources/styles/a.qml": "<qgis><renderer-v2/></qgis>"}, 0},
		{"样式集", "style_set", "resources/styles/set", map[string]string{"resources/styles/set/对照表.csv": "图层,别名,样式文件,标注字段,说明\n铁路,河南铁路、铁路线,rail.qml,无,线路\n", "resources/styles/set/rail.qml": "<qgis/>"}, 1},
		{"路线样式", "route_style", "resources/styles/routes.csv", map[string]string{"resources/styles/routes.csv": "拓扑,阵营,路线类型,绘制方式,样式编号,线宽系数,图例名称\n标准,我方,advance,bezier_arrow,red,1.2,推进\n"}, 1},
		{"QPT版式", "layout", "resources/layouts/template.qpt", map[string]string{"resources/layouts/template.qpt": "<Layout><LayoutItem id=\"main_map\"/></Layout>"}, 0},
		{"YAML版式", "layout", "resources/layouts/template.layout.yaml", map[string]string{"resources/layouts/template.layout.yaml": "name: 版式\npage: {default_paper: A4}\nfonts_mm: {}\nelements: {main_map: {fill: content_area}}\n"}, 0},
		{"符号", "symbols", "resources/symbols/set", map[string]string{"resources/symbols/set/对照表.csv": "符号类型,文件,大小,优先级,锚点X,锚点Y,说明\nmeeting,meeting.svg,4,10,0.5,0.5,会议\n", "resources/symbols/set/meeting.svg": "<svg xmlns=\"http://www.w3.org/2000/svg\"/>"}, 1},
		{"文档", "doc", "resources/docs/说明.md", map[string]string{"resources/docs/说明.md": "# 说明\n资源说明"}, 0},
		{"PDF文档", "doc", "resources/docs/说明.pdf", map[string]string{"resources/docs/说明.pdf": "%PDF-1.7\n"}, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, library := resourceProject(t)
			flowDir := filepath.Join(root, "autocarto_data/flows/test-flow")
			for path, content := range test.files {
				resourceWrite(t, filepath.Join(flowDir, path), content)
			}
			result := resolveRequest(library, Request{Key: "resource", Label: test.name, Type: test.kind, Location: test.location, FlowID: "test-flow", FlowDir: flowDir})
			resourceNoErrors(t, result)
			if result.Origin != "flow" || result.Root != filepath.Join(flowDir, test.location) || len(result.Entries) != test.entries {
				t.Fatalf("解析结构错误：%+v", result)
			}
			for _, entry := range result.Entries {
				if !entry.Exists {
					t.Fatalf("条目文件应存在：%+v", entry)
				}
			}
			if test.kind == "route_style" && result.Entries[0].Extra["topology"] != "standard" {
				t.Fatal("中文路线取值未归一化")
			}
			if test.kind == "style_set" && result.Entries[0].Extra["aliases"] != "河南铁路、铁路线" {
				t.Fatal("样式别名未保留")
			}
		})
	}
}

func TestResolveResourceProblems(t *testing.T) {
	tests := []struct {
		name, kind, location, content string
		code                          string
	}{
		{"清单缺列", "data_list", "list.csv", "名称\n道路\n", "FLOW_RESOURCE_INVALID"},
		{"清单缺数据", "data_list", "list.csv", "name,path\n道路,missing.geojson\n", "FLOW_DATA_FILE_MISSING"},
		{"清单无效类型", "data_list", "list.csv", "name,path,type\n道路,a.geojson,unknown\n", "FLOW_RESOURCE_INVALID"},
		{"清单列数错误", "data_list", "list.csv", "name,path\n道路,a.geojson,extra\n", "FLOW_RESOURCE_INVALID"},
		{"清单无效必需值", "data_list", "list.csv", "name,path,required\n道路,a.geojson,perhaps\n", "FLOW_RESOURCE_INVALID"},
		{"样式XML错误", "style", "style.qml", "<qgis>", "FLOW_RESOURCE_INVALID"},
		{"样式根错误", "style", "style.qml", "<svg/>", "FLOW_RESOURCE_INVALID"},
		{"样式多个根", "style", "style.qml", "<qgis/><qgis/>", "FLOW_RESOURCE_INVALID"},
		{"数据文件夹类型错误", "data_folder", "folder.geojson", "{}", "FLOW_RESOURCE_INVALID"},
		{"样式集缺对照表", "style_set", "set/a.qml", "<qgis/>", "FLOW_RESOURCE_INVALID"},
		{"路线缺列", "route_style", "route.csv", "topology,side\nstandard,friendly\n", "FLOW_RESOURCE_INVALID"},
		{"路线无效值", "route_style", "route.csv", "topology,side,route_type,draw_type,style_id\nother,friendly,a,bezier_arrow,a\n", "FLOW_RESOURCE_INVALID"},
		{"路线非有限数字", "route_style", "route.csv", "topology,side,route_type,draw_type,style_id,width_scale\nstandard,friendly,a,bezier_arrow,a,NaN\n", "FLOW_RESOURCE_INVALID"},
		{"版式缺主图", "layout", "layout.qpt", "<Layout/>", "FLOW_RESOURCE_INVALID"},
		{"版式无效YAML", "layout", "layout.layout.yaml", "name: [\n", "FLOW_RESOURCE_INVALID"},
		{"符号缺对照表", "symbols", "symbols/a.svg", "<svg/>", "FLOW_RESOURCE_INVALID"},
		{"文档编码错误", "doc", "doc.txt", string([]byte{0xff}), "FLOW_RESOURCE_INVALID"},
		{"PDF文件头错误", "doc", "doc.pdf", "not a pdf", "FLOW_RESOURCE_INVALID"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, library := resourceProject(t)
			path := resourceWrite(t, filepath.Join(root, test.location), test.content)
			if test.kind == "style_set" || test.kind == "symbols" {
				path = filepath.Dir(path)
			}
			result := resolveRequest(library, Request{Key: "key", Label: test.name, Type: test.kind, Location: path})
			if !resourceHasProblem(result, test.code, "error") {
				t.Fatalf("缺少资源错误：%+v", result)
			}
		})
	}
	root, library := resourceProject(t)
	missing := resolveRequest(library, Request{Label: "缺少", Type: "doc", Location: filepath.Join(root, "missing.md")})
	if !resourceHasProblem(missing, "FLOW_RESOURCE_NOT_FOUND", "error") {
		t.Fatal(missing)
	}
	forward := resolveRequest(library, Request{Label: "越界", Type: "doc", Location: "../outside.md", FlowDir: root})
	if !resourceHasProblem(forward, "FLOW_RESOURCE_INVALID", "error") {
		t.Fatal(forward)
	}
}

func TestDataBindingRelativePersistenceAndFallback(t *testing.T) {
	root, library := resourceProject(t)
	list := resourceWrite(t, filepath.Join(root, "flow/resources/data/base.csv"), "name,path,required\n道路,road.geojson,yes\n旁置,local.geojson,yes\n可选,optional.geojson,no\n")
	resourceWrite(t, filepath.Join(root, "flow/resources/data/local.geojson"), "{}")
	request := Request{Key: "basemap", Label: "底图", Type: "data_list", Location: list, FlowID: "test-flow", FlowDir: filepath.Join(root, "flow")}
	before := resolveRequest(library, request)
	if !resourceHasProblem(before, "FLOW_RESOURCE_UNBOUND", "error") {
		t.Fatalf("缺少绑定提示：%+v", before)
	}
	resourceWrite(t, filepath.Join(root, "数据/road.geojson"), "{}")
	if err := library.SetBinding("flow:test-flow/path:data/base.csv", "数据"); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	t.Chdir(other)
	after := resolveRequest(library, request)
	resourceNoErrors(t, after)
	if after.BaseDir != filepath.Join(root, "数据") || after.Entries[0].Path != filepath.Join(root, "数据/road.geojson") || after.Entries[1].Path != filepath.Join(root, "flow/resources/data/local.geojson") || after.Entries[2].Exists {
		t.Fatalf("绑定或清单旁置回退错误：%+v", after)
	}
	if !resourceHasProblem(after, "FLOW_DATA_FILE_MISSING", "warning") {
		t.Fatal("可选缺失文件应只有警告")
	}
	raw, err := os.ReadFile(filepath.Join(root, "autocarto_data/config/resource_bindings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), root) || !strings.Contains(string(raw), "数据") {
		t.Fatalf("项目内绑定必须持久化为相对路径：%s", raw)
	}
	if err = library.SetBinding("flow:test-flow/path:data/base.csv", ""); err != nil {
		t.Fatal(err)
	}
	if _, found, err := library.Bindings.Get("flow:test-flow/path:data/base.csv"); err != nil || found {
		t.Fatalf("清除绑定失败：%v %v", found, err)
	}
}

func TestLibraryExplicitOverrideImportAndReadonly(t *testing.T) {
	root, library := resourceProject(t)
	resourceWrite(t, filepath.Join(library.BuiltinDir, "styles/说明.qml"), "<qgis><note>内置</note></qgis>")
	resourceWrite(t, filepath.Join(library.BuiltinDir, "styles/说明.resource.yaml"), "apiVersion: resource/v1\nid: style.notes\ntype: style\nname: 说明\nentry: 说明.qml\n")
	builtin, err := library.Get("style", "说明")
	if err != nil || builtin.Origin != "library_builtin" {
		t.Fatalf("%+v %v", builtin, err)
	}
	if err = library.Delete("style", "说明"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("内置资源不能删除：%v", err)
	}
	source := resourceWrite(t, filepath.Join(root, "import/说明.qml"), "<qgis><note>用户内容</note></qgis>")
	if _, err = library.Import(source, "style"); !errors.Is(err, ErrConflict) {
		t.Fatalf("builtin names must also be unique: %v", err)
	}
	user, err := library.Copy("style.notes", true, "我的说明")
	if err != nil || user.Origin != "library_user" {
		t.Fatal(user, err)
	}
	resolved := library.Resolve(context.Background(), Ref{ID: "style.notes"}, Scope{})
	if resolved.ID != user.ID {
		t.Fatal(resolved)
	}

	all, err := library.List("all")
	if err != nil || len(all) != 2 {
		t.Fatalf("%v %v", all, err)
	}
	for _, scope := range []string{"builtin", "user"} {
		items, err := library.List(scope)
		if err != nil || len(items) != 1 {
			t.Fatalf("scope %s: %v %v", scope, items, err)
		}
	}
	if err = library.DeleteID(user.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := library.Get("style", "说明")
	if err != nil || restored.Origin != "library_builtin" {
		t.Fatalf("%+v %v", restored, err)
	}
	if _, err = library.List("unknown"); err == nil {
		t.Fatal("应拒绝未知资源范围")
	}
	if _, err = library.Get("style", "../说明"); err == nil {
		t.Fatal("应拒绝越界名称")
	}
	manifest := resourceWrite(t, filepath.Join(root, "import/底图.csv"), "name,path\n道路,missing.geojson\n")
	if _, err := library.Import(manifest, "data_list"); err == nil {
		t.Fatal("non-public import accepted")
	}
	resourceWrite(t, filepath.Join(library.BuiltinDir, "layouts/通用版式.layout.yaml"), "name: 通用版式\npage: {default_paper: A4}\nfonts_mm: {}\nelements: {main_map: {fill: content_area}}\n")
	layout := resolveRequest(library, Request{Key: "layout", Label: "通用", Type: "layout", Location: "通用版式"})
	resourceNoErrors(t, layout)
	if layout.Origin != "library_builtin" || !strings.HasSuffix(layout.ResolvedPath, "通用版式.layout.yaml") {
		t.Fatal(layout)
	}
}

func TestResourceImportRejectsInvalidAndSymlinkWithoutPartialWrites(t *testing.T) {
	root, library := resourceProject(t)
	source := resourceWrite(t, filepath.Join(root, "source/坏样式.qml"), "broken")
	if _, err := library.Import(source, "style"); err == nil {
		t.Fatal("无效样式不能入库")
	}
	if _, err := os.Stat(filepath.Join(library.UserDir, "styles/坏样式.qml")); !os.IsNotExist(err) {
		t.Fatal("失败导入留下目标文件")
	}
	entries, err := os.ReadDir(filepath.Join(library.UserDir, "styles"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("失败导入留下临时文件：%v %v", entries, err)
	}
	valid := resourceWrite(t, filepath.Join(root, "source/valid.qml"), "<qgis/>")
	link := filepath.Join(root, "source/link.qml")
	if err = os.Symlink(valid, link); err != nil {
		t.Fatal(err)
	}
	if _, err = library.Import(link, "style"); err == nil {
		t.Fatal("符号链接不能入库")
	}
	folder := filepath.Join(root, "source/set")
	resourceWrite(t, filepath.Join(folder, "对照表.csv"), "layer,style_file\n道路,a.qml\n")
	if err = os.Symlink(valid, filepath.Join(folder, "a.qml")); err != nil {
		t.Fatal(err)
	}
	if _, err = library.Import(folder, "style_set"); err == nil {
		t.Fatal("含符号链接的资源包不能入库")
	}
	resourceWrite(t, filepath.Join(root, "source/traversal/对照表.csv"), "layer,style_file\n道路,../valid.qml\n")
	if _, err = library.Import(filepath.Join(root, "source/traversal"), "style_set"); err == nil {
		t.Fatal("对照表不能越出资源包")
	}
	large := filepath.Join(root, "source/large.md")
	file, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(MaxFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err = library.Import(large, "doc"); err == nil {
		t.Fatal("超大文件不能入库")
	}
}

func TestBindingStoresShareLockAndRejectUnsafePaths(t *testing.T) {
	root, library := resourceProject(t)
	dir := filepath.Join(root, "数据")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "autocarto_data/config/resource_bindings.yaml")
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			errs <- bindings.New(path).Set(fmt.Sprintf("library:data_list.resource-%d", index), dir)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var document struct {
		Bindings map[string]string `yaml:"bindings"`
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Bindings) != 12 {
		t.Fatalf("并发保存覆盖了其他绑定：%v", document.Bindings)
	}
	for _, bad := range []string{"bad", "flow:/key", "flow:test-flow/", "flow:../key", "library:..", "library:../bad", "flow:test-flow/a/b"} {
		if err = library.SetBinding(bad, dir); err == nil {
			t.Fatalf("应拒绝非法绑定键 %s", bad)
		}
	}
	file := resourceWrite(t, filepath.Join(root, "file.txt"), "文件")
	if err = library.SetBinding("library:data_list.file", file); err == nil {
		t.Fatal("不能绑定普通文件")
	}
	link := filepath.Join(root, "链接")
	if err = os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err = library.SetBinding("library:data_list.link", link); err == nil {
		t.Fatal("不能绑定符号链接")
	}
	external, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = library.SetBinding("library:data_list.external", external); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), external) {
		t.Fatalf("项目外数据目录应保留绝对路径：%s", raw)
	}
}

func TestSharedDataListsAreNotRuntimeResources(t *testing.T) {
	_, library := resourceProject(t)
	source := resourceWrite(t, filepath.Join(library.UserDir, "data/底图.csv"), "name,path\n道路,road.geojson\n")
	writeManifest(t, source, Manifest{ID: "data_list.base", Type: "data_list", Name: "底图"})
	r := library.Resolve(context.Background(), Ref{Type: "data_list", Name: "底图"}, Scope{})
	if r.Handle() != nil || !resourceHasProblem(r, "FLOW_RESOURCE_NOT_FOUND", "error") {
		t.Fatal(r)
	}
	migrations, err := library.MigrateV6()
	if err != nil || len(migrations) != 1 || migrations[0].Type != "data_list" {
		t.Fatal(migrations, err)
	}
}

func TestMappedResourceFilesAndNumericValidation(t *testing.T) {
	root, library := resourceProject(t)
	set := filepath.Join(root, "styles")
	resourceWrite(t, filepath.Join(set, "对照表.csv"), "layer,style_file\n道路,missing.qml\n")
	style := resolveRequest(library, Request{Label: "样式", Type: "style_set", Location: set})
	if !resourceHasProblem(style, "FLOW_RESOURCE_INVALID", "error") || len(style.Entries) != 1 || style.Entries[0].Exists {
		t.Fatal(style)
	}
	symbols := filepath.Join(root, "symbols")
	resourceWrite(t, filepath.Join(symbols, "对照表.csv"), "symbol_type,svg_path,size,anchor_x\npoint,point.png,4,0.5\n")
	file, err := os.Create(filepath.Join(symbols, "point.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(file, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	file.Close()
	good := resolveRequest(library, Request{Label: "符号", Type: "symbols", Location: symbols})
	resourceNoErrors(t, good)
	resourceWrite(t, filepath.Join(symbols, "对照表.csv"), "symbol_type,svg_path,size,anchor_x\npoint,point.png,-1,NaN\n")
	bad := resolveRequest(library, Request{Label: "符号", Type: "symbols", Location: symbols})
	if !resourceHasProblem(bad, "FLOW_RESOURCE_INVALID", "error") {
		t.Fatal(bad)
	}
	resourceWrite(t, filepath.Join(symbols, "point.png"), "corrupted png")
	bad = resolveRequest(library, Request{Label: "符号", Type: "symbols", Location: symbols})
	if !resourceHasProblem(bad, "FLOW_RESOURCE_INVALID", "error") {
		t.Fatal(bad)
	}
}

func resourceWorkbook(t *testing.T, path string, records [][]string) {
	t.Helper()
	var raw bytes.Buffer
	archive := zip.NewWriter(&raw)
	var sheet strings.Builder
	sheet.WriteString("<worksheet><sheetData>")
	for i, row := range records {
		fmt.Fprintf(&sheet, "<row r=\"%d\">", i+1)
		for j, value := range row {
			fmt.Fprintf(&sheet, "<c r=\"%c%d\" t=\"inlineStr\"><is><t>", 'A'+j, i+1)
			if err := xml.EscapeText(&sheet, []byte(value)); err != nil {
				t.Fatal(err)
			}
			sheet.WriteString("</t></is></c>")
		}
		sheet.WriteString("</row>")
	}
	sheet.WriteString("</sheetData></worksheet>")
	for _, entry := range []struct{ name, content string }{{"xl/workbook.xml", "<workbook xmlns:r=\"urn:r\"><sheets><sheet name=\"资源表\" r:id=\"sheet\"/></sheets></workbook>"}, {"xl/_rels/workbook.xml.rels", "<Relationships><Relationship Id=\"sheet\" Type=\"urn:r/worksheet\" Target=\"worksheets/sheet1.xml\"/></Relationships>"}, {"xl/worksheets/sheet1.xml", sheet.String()}} {
		file, err := archive.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write([]byte(entry.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestResourceXLSXAndEnglishHeaders(t *testing.T) {
	root, library := resourceProject(t)
	source := filepath.Join(root, "列表.xlsx")
	resourceWorkbook(t, source, [][]string{{"NAME", "path", "type", "required"}, {"道路", "road.geojson", "vector", "true"}})
	resourceWrite(t, filepath.Join(root, "road.geojson"), "{}")
	result := resolveRequest(library, Request{Key: "data", Label: "列表", Type: "data_list", Location: source})
	resourceNoErrors(t, result)
	if len(result.Entries) != 1 || result.Entries[0].Name != "道路" || result.Entries[0].Kind != "vector" {
		t.Fatal(result)
	}
	styles := filepath.Join(root, "styles")
	resourceWrite(t, filepath.Join(styles, "a.qml"), "<qgis/>")
	resourceWorkbook(t, filepath.Join(styles, "对照表.xlsx"), [][]string{{"layer", "aliases", "style_file", "label_field"}, {"道路", "道路线;a", "a.qml", "name"}})
	styled := resolveRequest(library, Request{Label: "样式", Type: "style_set", Location: styles})
	resourceNoErrors(t, styled)
	if !reflect.DeepEqual(styled.Entries[0].Extra, map[string]string{"layer": "道路", "aliases": "道路线;a", "style_file": "a.qml", "label_field": "name"}) {
		t.Fatal(styled)
	}
	resourceWorkbook(t, source, [][]string{{"name", "wrong"}, {"道路", "a"}})
	if bad := resolveRequest(library, Request{Label: "表头", Type: "data_list", Location: source}); !resourceHasProblem(bad, "FLOW_RESOURCE_INVALID", "error") {
		t.Fatal(bad)
	}
}

func resolveRequest(l *Library, request Request) *ResolvedResource {
	return l.Resolve(context.Background(), Ref{Key: request.Key, Name: request.Label, Type: request.Type, Location: request.Location}, Scope{FlowID: request.FlowID, FlowDir: request.FlowDir})
}

func TestStableDataBindingIdentityIgnoresDisplayLabelAndCallerKey(t *testing.T) {
	root, library := resourceProject(t)
	flowDir := filepath.Join(root, "flow")
	path := resourceWrite(t, filepath.Join(flowDir, "resources/data/base.csv"), "name,path\n道路,road.geojson\n")
	resourceWrite(t, filepath.Join(flowDir, "resources/data/base.resource.yaml"), "apiVersion: resource/v1\nid: data_list.base\ntype: data_list\nname: 底图\nentry: base.csv\n")
	resourceWrite(t, filepath.Join(root, "bound/road.geojson"), "{}")
	if err := library.SetBinding("flow:test-flow/data_list.base", "bound"); err != nil {
		t.Fatal(err)
	}
	result := library.Resolve(context.Background(), Ref{Key: "unresolved:data_list:新标签", Name: "新标签", Type: "data_list", Location: path}, Scope{FlowID: "test-flow", FlowDir: flowDir})
	resourceNoErrors(t, result)
	if result.Key != "data_list.base" || result.BindingKey != "flow:test-flow/data_list.base" || result.BaseDir != filepath.Join(root, "bound") {
		t.Fatal(result)
	}
	// Shared lists no longer participate in runtime resolution; legacy flow
	// snapshots above remain readable only for migration/replay.
	resourceWrite(t, filepath.Join(library.UserDir, "data/旧清单.csv"), "name,path\n道路,road.geojson\n")
	if _, err := library.Get("data_list", "旧清单"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
