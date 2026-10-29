package resource

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyPathTypeAdjustmentForMigration(t *testing.T) {
	_, library := resourceProject(t)
	resourceWrite(t, filepath.Join(library.BuiltinDir, "styles/单样式.qml"), "<qgis/>")
	resourceWrite(t, filepath.Join(library.BuiltinDir, "styles/组合/mapping.csv"), "layer,style_file\n道路,a.qml\n")
	resourceWrite(t, filepath.Join(library.BuiltinDir, "styles/组合/a.qml"), "<qgis/>")
	for _, item := range []struct {
		label, requested, actual string
		adjusted                 bool
	}{{"单样式", "style", "style", false}, {"单样式", "style_set", "style", true}, {"组合", "style", "style_set", true}, {"组合", "style_set", "style_set", false}} {
		location := filepath.Join(library.BuiltinDir, "styles", item.label)
		if item.label == "单样式" {
			location += ".qml"
		}
		got := resolveRequest(library, Request{Type: item.requested, Label: item.label, Location: location})
		resourceNoErrors(t, got)
		if got.Type != item.actual || resourceHasProblem(got, "FLOW_RESOURCE_TYPE_ADJUSTED", "warning") != item.adjusted {
			t.Fatalf("%+v", got)
		}
	}
}
func TestFolderSkipsMetadataAndHiddenPaths(t *testing.T) {
	root, library := resourceProject(t)
	dir := filepath.Join(root, "folder")
	for _, file := range []string{"meta.json", "upstream-manifest.json", "table.csv", ".hidden.geojson", ".cache/a.geojson", "__MACOSX/a.geojson", "real.geojson", "nested/height.tif"} {
		resourceWrite(t, filepath.Join(dir, file), "{}")
	}
	got := resolveRequest(library, Request{Type: "data_folder", Label: "数据", Location: dir})
	resourceNoErrors(t, got)
	if len(got.Entries) != 2 {
		t.Fatalf("%+v", got.Entries)
	}
	for _, file := range []string{"meta.json", "table.csv"} {
		list := resourceWrite(t, filepath.Join(root, "list.csv"), "name,path\n数据,"+filepath.Join(dir, file)+"\n")
		if result := resolveRequest(library, Request{Type: "data_list", Label: "清单", Location: list}); !resourceHasProblem(result, "FLOW_RESOURCE_INVALID", "error") {
			t.Fatalf("隐式类型应失败: %+v", result)
		}
		resourceWrite(t, list, "name,path,type\n数据,"+filepath.Join(dir, file)+",vector\n")
		resourceNoErrors(t, resolveRequest(library, Request{Type: "data_list", Label: "清单", Location: list}))
	}
}
func TestShapefileSidecars(t *testing.T) {
	root, library := resourceProject(t)
	shp := resourceWrite(t, filepath.Join(root, "roads.shp"), "shape")
	for _, required := range []string{"yes", "no"} {
		list := resourceWrite(t, filepath.Join(root, "list.csv"), "name,path,required\n道路,roads.shp,"+required+"\n")
		result := resolveRequest(library, Request{Type: "data_list", Label: "清单", Location: list})
		level := "warning"
		if required == "yes" {
			level = "error"
		}
		if !resourceHasProblem(result, "FLOW_DATA_SIDECAR_MISSING", level) {
			t.Fatalf("%+v", result)
		}
	}
	resourceWrite(t, strings.TrimSuffix(shp, ".shp")+".dbf", "dbf")
	resourceWrite(t, strings.TrimSuffix(shp, ".shp")+".shx", "shx")
	result := resolveRequest(library, Request{Type: "data_folder", Location: root})
	resourceNoErrors(t, result)
	if !resourceHasProblem(result, "FLOW_DATA_SIDECAR_MISSING", "warning") {
		t.Fatalf("缺prj应警告: %+v", result)
	}
}
func TestSymbolFileColumnAliases(t *testing.T) {
	root, library := resourceProject(t)
	resourceWrite(t, filepath.Join(root, "point.svg"), "<svg/>")
	for _, alias := range []string{"SVG路径", "svg路径", "符号文件"} {
		resourceWrite(t, filepath.Join(root, "mapping.csv"), "符号类型,"+alias+"\npoint,point.svg\n")
		result := resolveRequest(library, Request{Type: "symbols", Location: root})
		resourceNoErrors(t, result)
		if len(result.Entries) != 1 || result.Entries[0].Path != filepath.Join(root, "point.svg") {
			t.Fatal(result)
		}
	}
}
func multiSheetWorkbook(t *testing.T, filename string, sheets []string) {
	t.Helper()
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(file)
	add := func(name, content string) {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(content)); e != nil {
			t.Fatal(e)
		}
	}
	var book, rels strings.Builder
	book.WriteString(`<workbook xmlns:r="urn:r"><sheets>`)
	rels.WriteString(`<Relationships>`)
	for i, sheet := range sheets {
		fmt.Fprintf(&book, `<sheet name="Sheet%d" r:id="s%d"/>`, i, i)
		fmt.Fprintf(&rels, `<Relationship Id="s%d" Type="urn:r/worksheet" Target="worksheets/s%d.xml"/>`, i, i)
		add(fmt.Sprintf("xl/worksheets/s%d.xml", i), sheet)
	}
	book.WriteString(`</sheets></workbook>`)
	rels.WriteString(`</Relationships>`)
	add("xl/workbook.xml", book.String())
	add("xl/_rels/workbook.xml.rels", rels.String())
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestXLSXSelectsByHeadersNotBySuccessfulRows(t *testing.T) {
	root, _ := resourceProject(t)
	file := filepath.Join(root, "list.xlsx")
	intro := `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>说明页</t></is></c></row></sheetData></worksheet>`
	sheet := `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>名称</t></is></c><c r="B1" t="inlineStr"><is><t>路径</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>道路</t></is></c><c r="B2" t="inlineStr"><is><t>road.geojson</t></is></c></row></sheetData></worksheet>`
	multiSheetWorkbook(t, file, []string{intro, sheet})
	rows, err := readTable(file, "data_list")
	if err != nil || len(rows) != 1 || rows[0].values["name"] != "道路" {
		t.Fatalf("%+v %v", rows, err)
	}
	bad := strings.Replace(sheet, ">road.geojson<", "><", 1)
	multiSheetWorkbook(t, file, []string{intro, bad, sheet})
	if _, err = readTable(file, "data_list"); err == nil {
		t.Fatal("不得跳过首个匹配表的数据行错误")
	}
}
