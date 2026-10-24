package resource

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type contractResource struct {
	typ, relative string
	files         map[string]string
}

func contractResources() []contractResource {
	return []contractResource{
		{"data_list", "data/list.csv", map[string]string{"data/list.csv": "name,path\nsecond,b.geojson\nfirst,a.geojson\n", "data/a.geojson": "{}", "data/b.geojson": "{}"}},
		{"data_folder", "data/folder", map[string]string{"data/folder/b.geojson": "{}", "data/folder/a.geojson": "{}", "data/folder/meta.json": "{}", "data/folder/upstream-manifest.json": "{}", "data/folder/.hidden.geojson": "{}", "data/folder/__MACOSX/ignored.geojson": "{}"}},
		{"style", "styles/single.qml", map[string]string{"styles/single.qml": "<qgis><layerGeometryType>2</layerGeometryType></qgis>"}},
		{"style_set", "styles/set", map[string]string{"styles/set/mapping.csv": "layer,style_file,label_field\narea,area.qml,NAME\n", "styles/set/area.qml": "<qgis><layerGeometryType>2</layerGeometryType></qgis>"}},
		{"route_style", "styles/route.csv", map[string]string{"styles/route.csv": "topology,side,route_type,draw_type,style_id,reference_style_id,auto_width_group\nstandard,friendly,custom,bezier_arrow,custom_style,custom_reference,true\n"}},
		{"layout", "layouts/map.layout.yaml", map[string]string{"layouts/map.layout.yaml": "name: map\npage: {}\nfonts_mm: {}\nelements: {main_map: {}}\n"}},
		{"symbols", "symbols/points", map[string]string{"symbols/points/mapping.csv": "symbol_type,SVG路径,size,event_types\nwaypoint,point.svg,3.5,抵达\n", "symbols/points/point.svg": "<svg xmlns=\"http://www.w3.org/2000/svg\"/>"}},
		{"doc", "docs/readme.md", map[string]string{"docs/readme.md": "# resource contract\n"}},
	}
}

func writeContractResource(t *testing.T, root string, sample contractResource) string {
	t.Helper()
	for path, content := range sample.files {
		resourceWrite(t, filepath.Join(root, path), content)
	}
	return filepath.Join(root, sample.relative)
}

func TestResourceContractAllTypesAndExplicitOverrides(t *testing.T) {
	for _, sample := range contractResources() {
		t.Run(sample.typ, func(t *testing.T) {
			root, library := resourceProject(t)
			if !IsPublicType(sample.typ) {
				primary := writeContractResource(t, filepath.Join(root, "flow/resources"), sample)
				writeManifest(t, primary, Manifest{ID: sample.typ + ".local", Type: sample.typ, Name: "流程资源", Version: "1.0.0"})
				result := library.Resolve(context.Background(), Ref{Type: sample.typ, Location: primary}, Scope{FlowDir: filepath.Join(root, "flow")})
				resourceNoErrors(t, result)
				if result.Owner != "flow" || result.Handle() == nil {
					t.Fatal(result)
				}
				return
			}
			builtin := writeContractResource(t, library.BuiltinDir, sample)
			user := writeContractResource(t, library.UserDir, sample)
			writeManifest(t, builtin, Manifest{ID: sample.typ + ".contract", Type: sample.typ, Name: "内置资源", Version: "1.0.0"})
			writeManifest(t, user, Manifest{ID: sample.typ + ".custom", Type: sample.typ, Name: "用户资源", Version: "2.0.0", Overrides: sample.typ + ".contract"})
			resolved := library.Resolve(context.Background(), Ref{ID: sample.typ + ".contract", Type: sample.typ}, Scope{})
			resourceNoErrors(t, resolved)
			handle := resolved.Handle()
			if handle == nil || handle["id"] != sample.typ+".custom" || handle["owner"] != "library_user" || handle["root"] != user || handle["version"] != "2.0.0" {
				t.Fatalf("override was not delivered: %+v", handle)
			}
			if !resourceHasProblem(resolved, "FLOW_RESOURCE_OVERRIDDEN", "info") {
				t.Fatal("explicit override must be visible to callers")
			}
			for _, entry := range handle["entries"].([]map[string]any) {
				if path, _ := entry["path"].(string); path != "" && !inside(path, library.UserDir) {
					t.Fatalf("override mixed builtin paths into user entries: %+v", entry)
				}
			}
			if sample.typ == "data_list" || sample.typ == "data_folder" {
				names := []string{}
				for _, entry := range resolved.Entries {
					names = append(names, entry.Name)
				}
				want := []string{"second", "first"}
				if sample.typ == "data_folder" {
					want = []string{"a", "b"}
				}
				if !reflect.DeepEqual(names, want) {
					t.Fatalf("order/scan contract: %v, want %v", names, want)
				}
			}
			exact, err := library.GetByID(sample.typ + ".contract")
			if err != nil || exact.Root != builtin || exact.OverriddenBy != sample.typ+".custom" {
				t.Fatalf("management identity must remain exact: %+v %v", exact, err)
			}
		})
	}
}

func contractSheet(rows [][]string) string {
	var out strings.Builder
	out.WriteString("<worksheet><sheetData>")
	for i, row := range rows {
		fmt.Fprintf(&out, `<row r="%d">`, i+1)
		for j, value := range row {
			fmt.Fprintf(&out, `<c r="%c%d" t="inlineStr"><is><t>`, 'A'+j, i+1)
			_ = xml.EscapeText(&out, []byte(value))
			out.WriteString("</t></is></c>")
		}
		out.WriteString("</row>")
	}
	out.WriteString("</sheetData></worksheet>")
	return out.String()
}

func TestResourceContractTableRepresentations(t *testing.T) {
	tables := []struct {
		typ                   string
		english, chinese, row []string
	}{
		{"data_list", []string{"name", "path", "type", "required"}, []string{"名称", "路径", "类型", "必需"}, []string{"area", "area.geojson", "矢量", "是"}},
		{"style_set", []string{"layer", "style_file", "aliases", "label_field"}, []string{"图层", "样式文件", "别名", "标注字段"}, []string{"area", "area.qml", "区域；省域", "NAME"}},
		{"route_style", []string{"topology", "side", "route_type", "draw_type", "style_id", "default_sides", "width_scale", "label_paths", "auto_width_group", "reference_style_id"}, []string{"拓扑", "阵营", "路线类型", "绘制方式", "样式编号", "默认阵营", "线宽系数", "标注路径", "自动线宽组", "参照样式编号"}, []string{"标准", "中立", "survey", "bezier_arrow", "chosen", "中立；我方", "1.3", "是", "否", "chosen_reference"}},
		{"symbols", []string{"symbol_type", "svg_path", "size", "event_types"}, []string{"符号类型", "SVG路径", "大小", "事件类型"}, []string{"waypoint", "point.svg", "3.5", "抵达；会合"}},
	}
	for _, table := range tables {
		t.Run(table.typ, func(t *testing.T) {
			root, library := resourceProject(t)
			resourceWrite(t, filepath.Join(root, "area.geojson"), "{}")
			resourceWrite(t, filepath.Join(root, "area.qml"), "<qgis><layerGeometryType>2</layerGeometryType></qgis>")
			resourceWrite(t, filepath.Join(root, "point.svg"), "<svg/>")
			var canonical []map[string]any
			for _, representation := range []string{"english_csv", "chinese_csv", "intro_xlsx"} {
				header := table.chinese
				if representation == "english_csv" {
					header = table.english
				}
				path := filepath.Join(root, "mapping.csv")
				if representation == "intro_xlsx" {
					_ = os.Remove(path)
					path = filepath.Join(root, "mapping.xlsx")
					multiSheetWorkbook(t, path, []string{contractSheet([][]string{{"说明页"}}), contractSheet([][]string{header, table.row})})
				} else {
					var data bytes.Buffer
					writer := csv.NewWriter(&data)
					_ = writer.WriteAll([][]string{header, table.row})
					resourceWrite(t, path, "\ufeff"+data.String())
				}
				location := path
				if table.typ == "style_set" || table.typ == "symbols" {
					location = root
				}
				resolved := library.Resolve(context.Background(), Ref{Type: table.typ, Location: location}, Scope{})
				resourceNoErrors(t, resolved)
				handle := resolved.Handle()
				if handle == nil {
					t.Fatalf("no handle for %s", representation)
				}
				entries := handle["entries"].([]map[string]any)
				if len(entries) != 1 {
					t.Fatalf("unexpected rows: %+v", entries)
				}
				if canonical == nil {
					canonical = entries
				} else if !reflect.DeepEqual(canonical, entries) {
					t.Fatalf("representation changed delivered fields: %s\n%+v\n%+v", representation, canonical, entries)
				}
				switch table.typ {
				case "data_list":
					if entries[0]["required"] != true || entries[0]["kind"] != "vector" {
						t.Fatal(entries)
					}
				case "style_set":
					if entries[0]["label_mode"] != "field" || entries[0]["label_field"] != "NAME" || entries[0]["geometry"] != "polygon" {
						t.Fatal(entries)
					}
				case "route_style":
					if entries[0]["style_id"] != "chosen" || entries[0]["reference_style_id"] != "chosen_reference" || entries[0]["width_scale"] != 1.3 || entries[0]["label_paths"] != true || entries[0]["auto_width_group"] != false {
						t.Fatal(entries)
					}
				case "symbols":
					if entries[0]["size"] != 3.5 || entries[0]["format"] != "svg" {
						t.Fatal(entries)
					}
				}
			}
		})
	}
}

func TestResourceContractErrorsNeverProduceHandle(t *testing.T) {
	cases := []struct{ typ, path, content string }{
		{"data_list", "list.csv", "name\na\n"},
		{"data_folder", "folder.geojson", "{}"},
		{"style", "style.qml", "<svg/>"},
		{"style_set", "set/mapping.csv", "layer\narea\n"},
		{"route_style", "route.csv", "topology,side\nstandard,friendly\n"},
		{"layout", "map.qpt", "<Layout/>"},
		{"symbols", "points/mapping.csv", "symbol_type\nwaypoint\n"},
		{"doc", "doc.pdf", "not a PDF"},
	}
	for _, sample := range cases {
		t.Run(sample.typ, func(t *testing.T) {
			root, library := resourceProject(t)
			path := resourceWrite(t, filepath.Join(root, sample.path), sample.content)
			if sample.typ == "style_set" || sample.typ == "symbols" {
				path = filepath.Dir(path)
			}
			resolved := library.Resolve(context.Background(), Ref{Type: sample.typ, Location: path}, Scope{})
			if !HasErrors(resolved.Problems) || resolved.Handle() != nil {
				t.Fatalf("bad resource escaped preflight: %+v", resolved)
			}
		})
	}
}

func TestResourceContractMappedPathsAndSidecars(t *testing.T) {
	for _, typ := range []string{"style_set", "symbols"} {
		for _, pathKind := range []string{"escape", "absolute", "symlink"} {
			t.Run(typ+"/"+pathKind, func(t *testing.T) {
				root, library := resourceProject(t)
				ext, content, header := ".qml", "<qgis/>", "layer,style_file"
				if typ == "symbols" {
					ext, content, header = ".svg", "<svg/>", "symbol_type,svg_path"
				}
				outside := resourceWrite(t, filepath.Join(root, "outside"+ext), content)
				folder := filepath.Join(root, "set")
				_ = os.MkdirAll(folder, 0700)
				path := "../outside" + ext
				if pathKind == "absolute" {
					path = outside
				}
				if pathKind == "symlink" {
					path = "link" + ext
					if err := os.Symlink(outside, filepath.Join(folder, path)); err != nil {
						t.Fatal(err)
					}
				}
				resourceWrite(t, filepath.Join(folder, "mapping.csv"), header+"\na,"+path+"\n")
				resolved := library.Resolve(context.Background(), Ref{Type: typ, Location: folder}, Scope{})
				if !HasErrors(resolved.Problems) || resolved.Handle() != nil {
					t.Fatalf("unsafe mapped file delivered: %+v", resolved)
				}
			})
		}
	}
	for _, optional := range []bool{false, true} {
		t.Run(fmt.Sprint("shapefile_optional_", optional), func(t *testing.T) {
			root, library := resourceProject(t)
			resourceWrite(t, filepath.Join(root, "area.shp"), "shape")
			resourceWrite(t, filepath.Join(root, "area.shx"), "index")
			path := resourceWrite(t, filepath.Join(root, "list.csv"), fmt.Sprintf("name,path,required\narea,area.shp,%t\n", !optional))
			resolved := library.Resolve(context.Background(), Ref{Type: "data_list", Location: path}, Scope{})
			if optional {
				if resolved.Handle() == nil || !resourceHasProblem(resolved, "FLOW_DATA_SIDECAR_MISSING", "warning") {
					t.Fatal(resolved)
				}
				if !reflect.DeepEqual(resolved.Entries[0].Fields["sidecars_missing"], []string{".dbf", ".prj"}) {
					t.Fatal(resolved.Entries)
				}
			} else if resolved.Handle() != nil || !resourceHasProblem(resolved, "FLOW_DATA_SIDECAR_MISSING", "error") {
				t.Fatal(resolved)
			}
		})
	}
}
