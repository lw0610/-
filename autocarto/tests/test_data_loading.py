"""数据清单与文件身份的纯逻辑回归，不导入 QGIS。"""

import csv
from pathlib import Path
from zipfile import ZipFile

import pytest

from autocarto.tests.support.resource_handles import resolve_handle
from autocarto.tools.system._shared.data_loading_support import (
    DataLoadingError,
    boolean_parameter,
    normalized_data_source,
)


def resolved_entries(path):
    return resolve_handle(path, "data_list")["entries"]




def test_data_load_source_identity_respects_sub_layers_and_project_root(tmp_path, monkeypatch):
    monkeypatch.setenv("AUTOCARTO_PROJECT_ROOT", str(tmp_path))
    folder = tmp_path / "data"
    folder.mkdir()
    source = folder / "data.gpkg"
    source.touch()
    link = tmp_path / "alias.gpkg"
    link.symlink_to(source)
    first = normalized_data_source("data/../data/data.gpkg|layername=places|encoding=UTF-8")
    assert first == normalized_data_source(str(link) + "|layername=places|geometrytype=Point")
    assert first != normalized_data_source(str(source) + "|layername=routes")
    assert normalized_data_source(r"C:\Data\MAP.GPKG|layername=POINTS", windows=True) == normalized_data_source("c:/data/map.gpkg|layername=points|encoding=UTF-8", windows=True)
    with pytest.raises(DataLoadingError):
        normalized_data_source(str(source) + "|layername=one|layername=two")


def test_data_load_csv_accepts_bom_aliases_optional_and_provider_paths(tmp_path):
    path = tmp_path / "list.csv"
    with path.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.writer(stream)
        writer.writerow(["名称", "路径", "类型", "必需", "说明"])
        writer.writerow(["区域", "maps.gpkg|layername=regions", "矢量", "", "面数据"])
        writer.writerow(["影像", "image.TIFF", "", "否", "可选"])
    (tmp_path / "maps.gpkg").touch()
    rows = resolved_entries(path)
    assert rows[0]["required"] is True
    assert rows[0]["kind"] == "vector"
    assert rows[0]["path"] == str(tmp_path / "maps.gpkg") + "|layername=regions"
    assert rows[1]["required"] is False
    assert rows[1]["kind"] == "raster"


@pytest.mark.parametrize("content", [
    "name,type\na,vector\n",
    "name,名称,path\na,a,a.geojson\n",
    "name,path\na,a.geojson\na,b.geojson\n",
    "name,path,type\na,a.geojson,unknown\n",
    "name,path,required\na,a.geojson,maybe\n",
    "name,path\na,\n",
    "name,path\n",
])
def test_data_load_csv_rejects_incomplete_or_ambiguous_resources(tmp_path, content):
    path = tmp_path / "invalid.csv"
    path.write_text(content, encoding="utf-8")
    for name in ("a.geojson", "b.geojson"):
        (tmp_path / name).write_text('{"type":"FeatureCollection","features":[]}')
    with pytest.raises(ValueError, match="FLOW_RESOURCE_INVALID"):
        resolved_entries(path)


def test_data_load_xlsx_reads_actual_first_sheet_shared_inline_and_sparse_cells(tmp_path):
    path = tmp_path / "list.xlsx"
    with ZipFile(path, "w") as archive:
        archive.writestr("[Content_Types].xml", '''<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/custom.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/></Types>''')
        archive.writestr("_rels/.rels", '''<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>''')
        archive.writestr("xl/workbook.xml", '''<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="数据" sheetId="1" r:id="rId3"/></sheets></workbook>''')
        archive.writestr("xl/_rels/workbook.xml.rels", '''<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/custom.xml"/><Relationship Id="strings" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/></Relationships>''')
        archive.writestr("xl/sharedStrings.xml", '''<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>name</t></si><si><t>path</t></si><si><r><t>区</t></r><r><t>域</t></r></si></sst>''')
        archive.writestr("xl/worksheets/custom.xml", '''<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="D1" t="inlineStr"><is><t>required</t></is></c></row><row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2" t="inlineStr"><is><t>regions.geojson</t></is></c><c r="D2" t="b"><v>0</v></c></row></sheetData></worksheet>''')
    rows = resolved_entries(path)
    assert len(rows) == 1
    assert rows[0]["name"] == "区域"
    assert rows[0]["path"] == str(tmp_path / "regions.geojson")
    assert rows[0]["required"] is False


@pytest.mark.parametrize("value,expected", [(False, False), (True, True), (None, True), ("false", False), ("否", False), (0, False)])
def test_data_load_boolean_arguments_are_explicit(value, expected):
    assert boolean_parameter(value) is expected


def test_data_load_malformed_xlsx_is_reported_as_resource_error(tmp_path):
    path = tmp_path / "broken.xlsx"
    path.write_bytes(b"not a workbook")
    with pytest.raises(ValueError, match="FLOW_RESOURCE_INVALID"):
        resolved_entries(path)


def test_data_load_optional_blank_columns_and_raster_image_suffix(tmp_path):
    path = tmp_path / "list.csv"
    path.write_text("name,path,type,,required\n底图,image.png,raster,,false\n", encoding="utf-8")
    rows = resolved_entries(path)
    assert rows[0]["kind"] == "raster"
    assert rows[0]["required"] is False
