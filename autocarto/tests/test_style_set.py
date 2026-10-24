"""样式集资源解析的普通 Python 回归。"""

import csv
from pathlib import Path

import pytest

from autocarto.tests.support.resource_handles import load_handle, resolve_handle
from autocarto.tools.system._shared.style_set_support import (
    StyleSetError, legacy_preset, matching_style, preset_catalog,
    qml_geometry_type,
)


def resolved_entries(path):
    return resolve_handle(path.parent if path.is_file() else path, "style_set")["entries"]


def _qml(root, *names):
    for name in names:
        (root / name).write_text("<qgis><layerGeometryType>2</layerGeometryType></qgis>")




def _table(path, header, rows):
    with path.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.writer(stream)
        writer.writerow(header)
        writer.writerows(rows)


def write_xlsx(path, sheets):
    """小型 OOXML 工作簿，支持用 dict 构造错误值和未缓存公式。"""
    from zipfile import ZipFile
    import xml.etree.ElementTree as ET
    namespace = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
    relation = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
    package = "http://schemas.openxmlformats.org/package/2006/relationships"
    workbook = ET.Element("workbook", xmlns=namespace, **{"xmlns:r": relation})
    names = ET.SubElement(workbook, "sheets")
    links = ET.Element("Relationships", xmlns=package)
    with ZipFile(path, "w") as archive:
        for index, values in enumerate(sheets, 1):
            ET.SubElement(names, "sheet", name="Sheet" + str(index), sheetId=str(index), **{"r:id": "r" + str(index)})
            ET.SubElement(links, "Relationship", Id="r" + str(index), Type=relation + "/worksheet", Target=f"worksheets/sheet{index}.xml")
            root = ET.Element("worksheet", xmlns=namespace)
            data = ET.SubElement(root, "sheetData")
            for number, items in enumerate(values, 1):
                row = ET.SubElement(data, "row", r=str(number))
                for column, value in enumerate(items):
                    cell = ET.SubElement(row, "c", r=chr(65 + column) + str(number))
                    if isinstance(value, dict):
                        cell.set("t", value.get("type", "n"))
                        for key in ("f", "v"):
                            if key in value:
                                ET.SubElement(cell, key).text = str(value[key])
                    else:
                        cell.set("t", "inlineStr")
                        ET.SubElement(ET.SubElement(cell, "is"), "t").text = str(value)
            archive.writestr(f"xl/worksheets/sheet{index}.xml", ET.tostring(root, encoding="utf-8"))
        archive.writestr("xl/workbook.xml", ET.tostring(workbook, encoding="utf-8"))
        archive.writestr("xl/_rels/workbook.xml.rels", ET.tostring(links, encoding="utf-8"))


def test_style_set_chinese_english_columns_and_primary_name_priority(tmp_path):
    path = tmp_path / "对照表.csv"
    _table(path, ["图层", "别名", "style_file", "标注字段"],
           [["A", "shared、alt;备用；最后", "a.qml", "name"], ["shared", "other", "b.qml", "无"]])
    _qml(tmp_path, "a.qml", "b.qml")
    entries = resolved_entries(tmp_path)
    assert matching_style(entries, "shared")["layer"] == "shared"
    assert matching_style(entries, "最后")["layer"] == "A"
    assert matching_style(entries, "a") is None
    assert entries[0]["style_path"] == str(tmp_path / "a.qml")
    assert entries[1]["label_mode"] == "off"
    assert entries[1]["label_field"] == ""


@pytest.mark.parametrize("header,rows", [
    (["图层", "说明"], [["A", "missing"]]),
    (["layer", "图层", "style_file"], [["A", "A", "a.qml"]]),
    (["layer", "style_file"], [["A", "a.qml"], ["A", "b.qml"]]),
    (["layer", "style_file"], [["", "a.qml"]]),
    (["layer", "style_file"], [["A", "../outside.qml"]]),
    (["layer", "style_file"], [["A", "C:\\outside.qml"]]),
    (["layer", "style_file"], [["A", "a.png"]]),
])
def test_style_set_invalid_or_incomplete_resource_is_rejected(tmp_path, header, rows):
    path = tmp_path / "mapping.csv"
    _table(path, header, rows)
    _qml(tmp_path, "a.qml", "b.qml")
    with pytest.raises(ValueError, match="FLOW_RESOURCE_INVALID"):
        resolved_entries(path)


def test_style_set_duplicate_alias_does_not_choose_first(tmp_path):
    path = tmp_path / "mapping.csv"
    _table(path, ["layer", "aliases", "style_file"], [["A", "shared", "a.qml"], ["B", "shared", "b.qml"]])
    _qml(tmp_path, "a.qml", "b.qml")
    entries = resolved_entries(path)
    with pytest.raises(StyleSetError) as raised:
        matching_style(entries, "shared")
    assert raised.value.code == "style_set_ambiguous"


def test_style_set_missing_or_broken_qml_reports_resource_error(tmp_path):
    path = tmp_path / "a.qml"
    with pytest.raises(StyleSetError) as raised:
        qml_geometry_type(path)
    assert raised.value.code == "style_file_missing"
    path.write_text("<broken>")
    with pytest.raises(StyleSetError) as raised:
        qml_geometry_type(path)
    assert raised.value.code == "style_file_invalid"


def test_style_set_builtin_eleven_qml_files_and_resource_bindings_are_portable():
    catalog = preset_catalog()
    entries = load_handle("gb-basemap")["entries"]
    assert len(catalog["entries"]) == len(entries) == 11
    for entry in entries:
        assert Path(entry["style_path"]).is_file()
        assert qml_geometry_type(entry["style_path"]) in (0, 1, 2)
        assert "/Users/" not in Path(entry["style_path"]).read_text()
    assert legacy_preset("fill", "省")["legacy_type"] == "province"
    assert legacy_preset("line", "invalid") is None


@pytest.mark.parametrize("filename", ["对照表.csv", "对照表.xlsx", "mapping.csv", "mapping.xlsx", "symbol_mapping.csv", "symbol_mapping.xlsx"])
def test_style_set_accepts_all_mapping_candidates_and_skips_instruction_sheet(tmp_path, filename):
    path = tmp_path / filename
    if path.suffix == ".xlsx":
        write_xlsx(path, [[["使用说明"], [{"type": "e", "v": "#N/A"}]],
                          [[" 图层 ", "样式文件"], ["示例图层", "example.qml"]]])
    else:
        _table(path, ["图层", "样式文件"], [["示例图层", "example.qml"]])
    _qml(tmp_path, "example.qml")
    entries = resolved_entries(tmp_path)
    assert entries[0]["layer"] == "示例图层"
    assert entries[0]["style_path"] == str(tmp_path / "example.qml")


@pytest.mark.parametrize("bad_cell", [{"type": "e", "v": "#N/A"}, {"f": "1+1"}])
def test_style_set_rejects_error_in_first_matching_sheet_instead_of_trying_later_sheet(tmp_path, bad_cell):
    path = tmp_path / "对照表.xlsx"
    write_xlsx(path, [[["layer", "style_file"], ["first", bad_cell]],
                      [["layer", "style_file"], ["later", "valid.qml"]]])
    _qml(tmp_path, "valid.qml", "missing-name.qml")
    with pytest.raises(ValueError, match="FLOW_RESOURCE_INVALID"):
        resolved_entries(tmp_path)


def test_style_set_xlsx_selects_first_matching_header_even_when_row_is_invalid(tmp_path):
    path = tmp_path / "对照表.xlsx"
    write_xlsx(path, [[["layer", "style_file"], ["", "missing-name.qml"]],
                      [["layer", "style_file"], ["later", "valid.qml"]]])
    _qml(tmp_path, "valid.qml", "missing-name.qml")
    with pytest.raises(ValueError, match="FLOW_RESOURCE_INVALID"):
        resolved_entries(tmp_path)


def test_table_reader_keeps_first_sheet_default_and_explicit_sheet_index(tmp_path):
    from autocarto.tools.system._shared.table_io import read_table_rows, read_xlsx_rows
    path = tmp_path / "tables.xlsx"
    write_xlsx(path, [[["说明"]], [["layer", "style_file"], ["A", "a.qml"]]])
    assert read_table_rows(path) == [["说明"]]
    assert read_xlsx_rows(path, 1)[1] == ["A", "a.qml"]
    with pytest.raises(ValueError, match="没有包含必填列"):
        read_table_rows(path, required=("symbol_type", "svg_path"))


def test_legacy_presets_are_hidden_and_have_private_frozen_assets():
    import yaml
    from autocarto.tools.system._shared.tool_assets import tool_asset_path
    root = Path(__file__).resolve().parents[1]
    content = (root / "skills/system/cartography-styling/SKILL.md").read_text()
    package = yaml.safe_load(content.split("---", 2)[1])
    assert not {"layer_set_marker_symbol", "layer_set_line_symbol", "layer_set_fill_symbol"}.intersection(package["tools"])
    assert not (Path(load_handle("gb-basemap")["root"]) / "presets.json").exists()
    for entry in preset_catalog()["entries"]:
        path = tool_asset_path("cartography", "basemap-legacy-styles", entry["style_file"])
        assert path.is_file()
        assert qml_geometry_type(path) in (0, 1, 2)
