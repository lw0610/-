package resource

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestResourceFormatDefinitions(t *testing.T) {
	if err := validateFormats(Formats()); err != nil {
		t.Fatal(err)
	}
	if len(TypeInfos()) != 8 {
		t.Fatal("必须包含八类资源")
	}
	for _, typ := range TypeInfos() {
		for _, word := range typ.Words {
			actual, ok := TypeForWord(word)
			if !ok || actual != typ.Type {
				t.Fatal(word, actual)
			}
		}
		if typ.Folder {
			kind, _ := classify(typ.Group, "example", true)
			if kind != typ.Type {
				t.Fatal(typ)
			}
		} else {
			for _, ext := range typ.EntryExtensions {
				kind, name := classify(typ.Group, "example"+ext, false)
				if kind != typ.Type || name != "example" {
					t.Fatal(typ, kind, name)
				}
			}
		}
	}
	if DataKindForFile("metadata.json", true) != "" || DataKindForFile("data.csv", false) != "" {
		t.Fatal("清单专用扩展名不允许自动推断")
	}
}
func TestTableFormatNormalizationAndRules(t *testing.T) {
	root, _ := resourceProject(t)
	tests := []struct {
		typ, body string
		invalid   bool
	}{
		{"data_list", "名称,路径,类型,必需,备注\n道路,roads.json,矢量,y,保留\n", false},
		{"data_list", "name,path,required\n道路,a.geojson,maybe\n", true},
		{"data_list", "name,path\n道路,a.geojson\n道路,b.geojson\n", true},
		{"data_list", "name,path,description\n道路,a.geojson\n", false},
		{"data_list", "name,path\n道路,a.geojson,,\n", false},
		{"data_list", "name,path\n道路,a.geojson,extra\n", true},
		{"style_set", "图层,样式文件,别名\n道路,a.qml,公路；铁路、道路\n", false},
		{"symbols", "符号类型,文件,事件类型\na,a.svg,meeting\nb,b.svg,meeting\n", true},
		{"symbols", "symbol_type,svg_path,size\na,a.svg,NaN\n", true},
		{"route_style", "拓扑,阵营,路线类型,绘制方式,样式编号,默认阵营,标注路径\n标准,中立,route,bezier_arrow,a,我方；对方,是\n", false},
		{"route_style", "topology,side,route_type,draw_type,style_id,default_sides\nstandard,neutral,a,bezier_arrow,a,friendly\nstandard,friendly,b,bezier_arrow,b,friendly\n", true},
		{"route_style", "topology,side,route_type,draw_type,style_id,preset_ref\nstandard,neutral,a,bezier_arrow,a,shared\nbranch,neutral,b,branch_bezier,b,shared\n", true},
	}
	for _, item := range tests {
		file := resourceWrite(t, filepath.Join(root, "table.csv"), item.body)
		rows, err := readTable(file, item.typ)
		if (err != nil) != item.invalid {
			t.Fatalf("%s %s: %v", item.typ, item.body, err)
		}
		if err == nil && len(rows) == 0 {
			t.Fatal("empty")
		}
	}
	file := resourceWrite(t, filepath.Join(root, "table.csv"), tests[0].body)
	rows, err := readTable(file, "data_list")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].fields["required"] != true || rows[0].fields["type"] != "vector" || rows[0].values["备注"] != "保留" {
		t.Fatal(rows)
	}
}
func TestXLSXFormulaCacheAndErrorValues(t *testing.T) {
	root, _ := resourceProject(t)
	file := filepath.Join(root, "table.xlsx")
	header := `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>name</t></is></c><c r="B1" t="inlineStr"><is><t>path</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>道路</t></is></c>`
	for _, item := range []struct {
		cell    string
		invalid bool
	}{{`<c r="B2"><f>"a.geojson"</f><v>a.geojson</v></c>`, false}, {`<c r="B2"><f>"a.geojson"</f></c>`, true}, {`<c r="B2" t="e"><v>#N/A</v></c>`, true}} {
		multiSheetWorkbook(t, file, []string{header + item.cell + `</row></sheetData></worksheet>`})
		_, err := readTable(file, "data_list")
		if (err != nil) != item.invalid {
			t.Fatalf("%s %v", item.cell, err)
		}
	}
}
func TestGuideResourceExamples(t *testing.T) {
	docs := filepath.Join("..", "..", "..", "docs")
	paths, err := filepath.Glob(filepath.Join(docs, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile("(?s)<!-- resource-example: ([a-z_]+) -->\\s*```csv\\s*\\n(.*?)\\n```")
	count := 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range pattern.FindAllStringSubmatch(string(raw), -1) {
			count++
			file := resourceWrite(t, filepath.Join(t.TempDir(), "example.csv"), match[2])
			if _, err := readTable(file, match[1]); err != nil {
				t.Errorf("%s (%s): %v", path, match[1], err)
			}
		}
	}
	if count < 4 {
		t.Fatalf("资源表文档示例缺失：%d", count)
	}
}
func TestLayoutStructuralFormat(t *testing.T) {
	root, _ := resourceProject(t)
	good := "name: 通用\npage: {}\nfonts_mm: {}\nelements: {main_map: {}}\n"
	for _, item := range []struct {
		body    string
		invalid bool
	}{{good, false}, {strings.Replace(good, "fonts_mm: {}\n", "", 1), true}, {good + "  invalid: {}\n", true}} {
		file := resourceWrite(t, filepath.Join(root, "test.layout.yaml"), item.body)
		err := validateLayout(file)
		if (err != nil) != item.invalid {
			t.Fatal(item.body, err)
		}
	}
}
