"""普通 Python 下视觉资源替换、编译前后六表与资产回归。"""
from autocarto.tests.support.resource_handles import load_handle, resolve_handle
import csv
import json
from pathlib import Path
import shutil

import pytest

from autocarto.knowledge.compiler import compile_task_knowledge
from autocarto.knowledge.visual_presets import load_route_styles, load_symbol_styles


FIXTURE = Path(__file__).parent / "fixtures/route_compiler"


def test_route_compile_six_tables_match_real_pre_refactor_baseline(tmp_path):
    graph = json.loads((FIXTURE / "graph.json").read_text())
    result = compile_task_knowledge(graph, tmp_path / "compiled", route_style=load_handle("red-blue"), symbols=load_handle("military"))
    assert result.success
    for source in FIXTURE.glob("*.csv"):
        assert_baseline(source, Path(result.dataset_dir) / source.name)


def test_route_compile_accepts_custom_visual_tables_without_changing_global_defaults(tmp_path):
    table = tmp_path / "routes.csv"
    with table.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.writer(stream)
        writer.writerow(["拓扑", "阵营", "路线类型", "绘制方式", "样式编号", "线宽系数", "preset_ref"])
        writer.writerow(["standard", "friendly", "custom", "bezier_arrow", "custom_style", "1.2", "custom_ref"])
    symbol_dir = tmp_path / "symbols"
    symbol_dir.mkdir()
    _, _, sources, rows = load_symbol_styles(load_handle("military"))
    for path in sources.values():
        shutil.copyfile(path, symbol_dir / path.name)
    with Path(load_handle("military")["entry"]).open(encoding="utf-8-sig", newline="") as stream:
        rows = list(csv.DictReader(stream))
    rows[0]["大小"] = "9"
    with (symbol_dir / "对照表.csv").open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, list(rows[0]))
        writer.writeheader(); writer.writerows(rows)
    graph = json.loads((FIXTURE / "graph.json").read_text())
    graph["routes"][0]["style_ref"] = "custom_ref"
    result = compile_task_knowledge(graph, tmp_path / "custom", route_style=resolve_handle(table, "route_style"), symbols=resolve_handle(symbol_dir, "symbols"))
    with (Path(result.dataset_dir) / "routes.csv").open(encoding="utf-8-sig", newline="") as stream:
        route = next(csv.DictReader(stream))
    assert (route["style_id"], route["route_type"], route["width_scale"]) == ("custom_style", "custom", "1.2")
    with (Path(result.dataset_dir) / "symbol_mapping.csv").open(encoding="utf-8-sig", newline="") as stream:
        symbol = next(csv.DictReader(stream))
    assert float(symbol["size"]) == 9
    assert (Path(result.dataset_dir) / symbol["svg_path"]).read_bytes() == sources[symbol["symbol_type"]].read_bytes()
    original = compile_task_knowledge(json.loads((FIXTURE / "graph.json").read_text()), tmp_path / "default", route_style=load_handle("red-blue"), symbols=load_handle("military"))
    for source in FIXTURE.glob("*.csv"):
        assert_baseline(source, Path(original.dataset_dir) / source.name)


def test_route_resource_tables_cover_defaults_and_conflicts_are_explicit(tmp_path):
    presets, defaults = load_route_styles(load_handle("red-blue"))
    assert len(presets) == 6 and len(defaults) == 9
    symbols, events, sources, _ = load_symbol_styles(load_handle("military"))
    assert len(symbols) == 5 and len(events) == 4
    assert all(path.is_file() for path in sources.values())
    assert len(list(Path(load_handle("military")["entry"]).parent.glob("*.svg"))) == 8
    bad = tmp_path / "styles.csv"
    bad.write_text("topology,side,route_type,draw_type,style_id\nstandard,neutral,main,bezier_arrow,a\nstandard,neutral,main,bezier_arrow,b\n")
    with pytest.raises(ValueError):
        load_route_styles(resolve_handle(bad, "route_style"))


@pytest.mark.parametrize("path", ["../outside.svg", "..\\outside.svg", "C:\\outside.svg"])
def test_route_symbol_resources_reject_paths_outside_the_table_directory(tmp_path, path):
    bad = tmp_path / "对照表.csv"
    bad.write_text("符号类型,文件\nwaypoint," + path + "\n", encoding="utf-8")
    with pytest.raises(ValueError):
        load_symbol_styles(resolve_handle(bad.parent, "symbols"))


def test_route_compile_xlsx_symbols_match_csv_six_tables(tmp_path):
    from zipfile import ZipFile
    import xml.etree.ElementTree as ET
    directory = tmp_path / "symbols"
    directory.mkdir()
    _, _, sources, _ = load_symbol_styles(load_handle("military"))
    for source in sources.values():
        shutil.copyfile(source, directory / source.name)
    with Path(load_handle("military")["entry"]).open(encoding="utf-8-sig", newline="") as stream:
        rows = list(csv.reader(stream))
    ns = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
    root = ET.Element("worksheet", xmlns=ns)
    data = ET.SubElement(root, "sheetData")
    for number, values in enumerate(rows, 1):
        row = ET.SubElement(data, "row", r=str(number))
        for column, value in enumerate(values):
            cell = ET.SubElement(row, "c", r=chr(65 + column) + str(number), t="inlineStr")
            ET.SubElement(ET.SubElement(cell, "is"), "t").text = value
    table = directory / "对照表.xlsx"
    with ZipFile(table, "w") as archive:
        archive.writestr("[Content_Types].xml", '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>')
        archive.writestr("_rels/.rels", '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>')
        archive.writestr("xl/workbook.xml", '<workbook xmlns="' + ns + '" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="符号" sheetId="1" r:id="rId1"/></sheets></workbook>')
        archive.writestr("xl/_rels/workbook.xml.rels", '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>')
        archive.writestr("xl/worksheets/sheet1.xml", ET.tostring(root, encoding="utf-8"))
    graph = json.loads((FIXTURE / "graph.json").read_text())
    for index, supplied in enumerate((directory,)):
        result = compile_task_knowledge(graph, tmp_path / ("compiled_" + str(index)), symbols=resolve_handle(supplied, "symbols"), route_style=load_handle("red-blue"))
        for reference in FIXTURE.glob("*.csv"):
            assert_baseline(reference, Path(result.dataset_dir) / reference.name)


@pytest.mark.parametrize("column", ["SVG路径", "svg路径", "符号文件"])
def test_symbol_file_alias_and_instruction_sheet_are_accepted(tmp_path, column):
    from autocarto.tests.test_style_set import write_xlsx
    path = tmp_path / "对照表.xlsx"
    write_xlsx(path, [[["说明"]], [["符号类型", column, "事件类型"], ["waypoint", "point.svg", " 出发 "]]])
    (tmp_path / "point.svg").write_text('<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"><circle cx="4" cy="4" r="3"/></svg>')
    presets, events, sources, _ = load_symbol_styles(resolve_handle(path.parent, "symbols"))
    assert presets["waypoint"]["size"] == 4
    assert sources["waypoint"] == tmp_path / "point.svg"
    assert events == {"出发": "waypoint"}


def test_duplicate_symbol_event_type_is_rejected(tmp_path):
    path = tmp_path / "对照表.csv"
    path.write_text("符号类型,文件,event_types\na,a.svg,到达\nb,b.svg,出发； 到达\n", encoding="utf-8")
    for filename in ("a.svg", "b.svg"):
        (tmp_path / filename).write_text('<svg xmlns="http://www.w3.org/2000/svg"/>')
    with pytest.raises(ValueError):
        load_symbol_styles(resolve_handle(path.parent, "symbols"))


def test_renderer_configuration_is_a_private_asset_not_a_public_resource():
    from autocarto.knowledge.visual_presets import renderer_defaults
    renderer = renderer_defaults()
    assert {"labels", "callouts", "default_point_symbol", "curve_params", "route_kind_by_type"}.issubset(renderer)
    assert not {"legacy", "route_label_layer", "rendezvous_label_rule", "symbol_labels"}.intersection(renderer)


def assert_baseline(reference, actual):
    if reference.name != "symbol_mapping.csv":
        assert reference.read_bytes() == actual.read_bytes(), reference.name
        return
    with reference.open(encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        fields, rows = reader.fieldnames, list(reader)
    with actual.open(encoding="utf-8-sig", newline="") as stream:
        actual_rows = list(csv.DictReader(stream))
    assert [{key: row[key] for key in fields} for row in actual_rows] == rows
    assert all(row["description"] for row in actual_rows)


def test_symbol_assets_with_same_basename_preserve_both_contents(tmp_path):
    from autocarto.knowledge.compiler import _copy_assets
    source = tmp_path / "source"
    for name, color in (("a", "red"), ("b", "blue")):
        (source / name).mkdir(parents=True)
        (source / name / "point.svg").write_text('<svg xmlns="http://www.w3.org/2000/svg"><circle r="3" fill="' + color + '"/></svg>')
    (source / "对照表.csv").write_text("symbol_type,svg_path\na,a/point.svg\nb,b/point.svg\n")
    presets, _, sources, _ = load_symbol_styles(resolve_handle(source, "symbols"))
    assert presets["a"]["svg_path"] != presets["b"]["svg_path"]
    stage = tmp_path / "compiled"
    stage.mkdir()
    _copy_assets({}, stage, template_dataset_dir=None, warnings=[], symbol_presets=presets, symbol_sources=sources)
    for name in ("a", "b"):
        assert (stage / presets[name]["svg_path"]).read_bytes() == sources[name].read_bytes()
