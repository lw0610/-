"""Production Go handles drive real QGIS operations in an isolated process."""
import csv
import os
from pathlib import Path
import shutil

import pytest

from autocarto.tests.support.resource_handles import load_handle, resolve_handle
from autocarto.tests.test_style_set_qgis import style_qgis_runtime, style_project, _layer


ROOT = Path(__file__).resolve().parents[1]


def test_contract_bound_list_file_order_and_qgis_sources(style_project, tmp_path, monkeypatch):
    from autocarto.tools.system._shared.data_loading_impl import load_list_handler
    from autocarto.tests.test_data_loading_qgis import _raster
    project, _ = style_project
    assets = tmp_path / "chosen-data"
    assets.mkdir()
    polygon = Path(load_handle("data-list")["entries"][0]["path"])
    (assets / "area.geojson").write_bytes(polygon.read_bytes())
    _raster(assets / "height.tif")
    source = tmp_path / "list.csv"
    source.write_text("名称,路径,类型\n高程,height.tif,栅格\n区域,area.geojson,矢量\n", encoding="utf-8")
    handle = resolve_handle(source, "data_list", base_dir=assets)
    assert handle["base_dir"] == str(assets)
    # The tool must not reread a table or depend on cwd after Go has resolved it.
    source.write_text("invalid table after link", encoding="utf-8")
    unrelated = tmp_path / "unrelated"
    unrelated.mkdir()
    monkeypatch.chdir(unrelated)
    result = load_list_handler({"data_list": handle, "group": "契约数据"})
    assert result["success"], result
    assert [(row["name"], row["path"]) for row in result["data"]["entries"]] == [(row["name"], row["path"]) for row in handle["entries"]]
    assert all(row["status"] == "loaded" for row in result["data"]["entries"])
    for expected, receipt in zip(handle["entries"], result["data"]["entries"]):
        layer = project.mapLayer(receipt["layer_id"])
        assert layer.isValid() and layer.source().split("|", 1)[0] == expected["path"]
    assert [node.layer().name() for node in project.layerTreeRoot().findGroup("契约数据").children()] == [row["name"] for row in handle["entries"]]


def test_contract_data_folder_ignores_metadata_and_loads_only_delivered_files(style_project, tmp_path):
    from autocarto.tools.system._shared.data_loading_impl import load_vector_handler
    project, _ = style_project
    source = Path(load_handle("data-folder")["entries"][0]["path"])
    for name in ("b.geojson", "a.geojson", ".hidden.geojson", "meta.json", "upstream-manifest.json"):
        (tmp_path / name).write_bytes(source.read_bytes())
    (tmp_path / "__MACOSX").mkdir()
    (tmp_path / "__MACOSX/ignored.geojson").write_bytes(source.read_bytes())
    handle = resolve_handle(tmp_path, "data_folder")
    assert [row["name"] for row in handle["entries"]] == ["a", "b"]
    receipts = [load_vector_handler({"path": row["path"], "layer_name": row["name"]}) for row in handle["entries"]]
    assert all(result["success"] for result in receipts), receipts
    assert [project.mapLayer(result["data"]["layer_id"]).source() for result in receipts] == [row["path"] for row in handle["entries"]]
    assert len(project.mapLayers()) == 2


def test_contract_single_style_and_xlsx_style_set_match_qgis_label_fields(style_project):
    from autocarto.tools.system._shared.style_basic_impl import load_style_handler
    from autocarto.tools.system._shared.style_set_impl import apply_style_set_handler
    layer = _layer("Polygon", "区域")
    single = load_handle("style")
    result = load_style_handler({"layer": layer.name(), "style": single})
    assert result["success"], result
    assert single["geometry"] == "polygon"
    styles = load_handle("contract-style-xlsx")
    result = apply_style_set_handler({"style_set": styles, "layers": [layer.id()], "strict": True})
    assert result["success"], result
    expected = styles["entries"][0]
    assert result["resource"]["id"] == styles["id"]
    assert layer.labelsEnabled() and layer.labeling().settings().fieldName == expected["label_field"]
    assert result["data"]["applied"][0]["style_file"] == expected["path"]


def test_contract_style_label_modes_and_application_order(style_project):
    from autocarto.tools.system._shared.style_set_impl import apply_style_set_handler
    handle = load_handle("style-set-modes")
    layers = [_layer("Polygon", row["name"]) for row in handle["entries"]]
    result = apply_style_set_handler({"style_set": handle, "layers": [layer.id() for layer in layers], "strict": True})
    assert result["success"], result
    assert [row["layer_id"] for row in result["data"]["applied"]] == [layer.id() for layer in layers]
    assert [row["style_file"] for row in result["data"]["applied"]] == [row["path"] for row in handle["entries"]]
    for row, layer in zip(handle["entries"], layers):
        if row["label_mode"] == "off":
            assert not layer.labelsEnabled()
        else:
            assert layer.labelsEnabled()
            assert layer.labeling().settings().fieldName == (row["label_field"] or "NAME")


def test_contract_symbols_xlsx_real_renderer_uses_go_files_and_sizes(style_project, tmp_path):
    from autocarto.tools.system._shared.route_points_impl import route_points_build_handler
    project, _ = style_project
    symbols = load_handle("contract-symbols-xlsx")
    dataset = tmp_path / "dataset"
    shutil.copytree(ROOT / "tests/golden/fixtures/route_dataset", dataset)
    table = dataset / "points.csv"
    with table.open(encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        fields, rows = reader.fieldnames, list(reader)
    for index, row in enumerate(rows):
        row["symbol_type"] = "start_flag" if index == 0 else "waypoint"
    with table.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, fields)
        writer.writeheader()
        writer.writerows(rows)
    result = route_points_build_handler({"dataset_source": "explicit", "dataset_dir": str(dataset),
                                         "symbols": symbols, "default_symbol_types": []})
    assert result["success"], result
    layer = project.mapLayersByName("专题点")[0]
    rules = {rule.label(): rule for rule in layer.renderer().rootRule().children()}
    for row in symbols["entries"]:
        rule = rules[row["description"]]
        marker = rule.symbol().symbolLayer(0)
        assert marker.path() == row["path"]
        assert marker.size() == pytest.approx(row["size"])
    assert result["resource"]["id"] == symbols["id"]


def test_contract_layout_override_format_cannot_fall_back_and_template_executes(style_project, tmp_path):
    from qgis.core import QgsCoordinateReferenceSystem
    from autocarto.tools.system._shared.layout_compose_impl import compose_layout_handler
    from autocarto.tools.system._shared.print_layout_impl import import_layout_template_handler
    project, _ = style_project
    project.setCrs(QgsCoordinateReferenceSystem("EPSG:4326"))
    _layer("Polygon", "区域")
    builtin, user = tmp_path / "builtin/layouts", tmp_path / "user/layouts"
    builtin.mkdir(parents=True)
    user.mkdir(parents=True)
    (builtin / "map.layout.yaml").write_bytes(Path(load_handle("four-corner")["entry"]).read_bytes())
    (user / "map.qpt").write_bytes(Path(load_handle("layout-template")["entry"]).read_bytes())
    (builtin / "map.resource.yaml").write_text("apiVersion: resource/v1\nid: layout.contract\ntype: layout\nname: 契约版式\nentry: map.layout.yaml\nversion: 1.0.0\n", encoding="utf-8")
    (user / "map.resource.yaml").write_text("apiVersion: resource/v1\nid: layout.custom\ntype: layout\nname: 用户版式\nentry: map.qpt\nversion: 2.0.0\noverrides: layout.contract\n", encoding="utf-8")
    handle = resolve_handle("公共资源库/layout.contract", "layout", builtin_dir=builtin.parent, user_dir=user.parent)
    assert handle["id"] == "layout.custom" and handle["format"] == "qpt"
    assert handle["entry"] == str(user / "map.qpt")
    refused = compose_layout_handler({"layout": "不应创建", "spec": handle, "title": "契约"})
    assert refused["success"] is False and refused["error_type"] == "resource_format_mismatch", refused
    assert project.layoutManager().layoutByName("不应创建") is None
    imported = import_layout_template_handler({"template": handle, "name": "用户模板", "map_layers": ["区域"], "extent_layers": ["区域"]})
    assert imported["success"], imported
    assert project.layoutManager().layoutByName("用户模板").itemById("main_map") is not None
    composed = compose_layout_handler({"layout": "YAML版式", "spec": load_handle("four-corner"), "title": "契约标题", "map_layers": ["区域"], "extent_layers": ["区域"]})
    assert composed["success"], composed
    assert project.layoutManager().layoutByName("YAML版式").itemById("main_map") is not None


def test_contract_route_styles_references_widths_and_actual_drawing(style_project, tmp_path):
    from autocarto.tools.system._shared.route_drawing_impl import _build_route_payloads_from_human_tables, route_draw_handler
    from qgis.core import QgsCoordinateReferenceSystem, QgsRectangle
    from qgis.utils import iface
    project, _ = style_project
    renderer = os.environ.get("AUTOCARTO_ROUTE_RENDERER_DIR")
    if not renderer or not Path(renderer).is_dir():
        pytest.skip("真实路线契约测试需要现有路线渲染插件目录")
    project.setCrs(QgsCoordinateReferenceSystem("EPSG:4004"))
    iface.mapCanvas().setDestinationCrs(project.crs())
    iface.mapCanvas().setExtent(QgsRectangle(100, 26, 120, 40))
    dataset = tmp_path / "dataset"
    shutil.copytree(ROOT / "tests/golden/fixtures/route_dataset", dataset)
    table = dataset / "routes.csv"
    with table.open(encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        fields, rows = reader.fieldnames, list(reader)
    for row in rows:
        row["style_id"] = row["draw_type"] = row["width_scale"] = ""
    with table.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, fields)
        writer.writeheader()
        writer.writerows(rows)
    original = table.read_bytes()
    handle = load_handle("contract-route-xlsx")
    payloads = _build_route_payloads_from_human_tables(dataset, renderer, route_style=handle)
    expected_ids = {row["reference_style_id"] for row in handle["entries"]}
    assert {row["style_id"] for row in payloads} == expected_ids
    assert all(row["options"]["width_scale"] == 1.25 for row in payloads)
    result = route_draw_handler({"dataset_source": "explicit", "dataset_dir": str(dataset), "route_style": handle, "renderer_plugin_dir": renderer})
    assert result["success"], result
    actual = result["data"]["routes"]["items"]
    assert [(row["route_id"], row["payload"]["style_id"]) for row in actual] == [(row["route_id"], row["style_id"]) for row in payloads]
    assert all(row["success"] for row in actual)
    assert project.mapLayers() and result["data"]["route_labels"]["count"] > 0
    assert table.read_bytes() == original
