"""独立 QGIS 下真实 authority manifest、路线资源和标注分组回归。"""
from autocarto.tests.support.resource_handles import load_handle, resolve_handle
import os
from pathlib import Path
import shutil

import pytest

from autocarto.tests.test_style_set_qgis import style_qgis_runtime, style_project


ROOT = Path(__file__).resolve().parents[1]
DATASET = ROOT / "tests/golden/fixtures/route_dataset"


def invoke(name, params):
    import yaml
    from autocarto.core.tool_engine.engine import QGISToolEngine, execute_tool
    paths = {"route_draw": "route-cartography", "route_points_build": "route-cartography",
             "layout_add_callouts": "print-layout", "cartography_symbolize_points_by_mapping": "cartography",
             "marching_route_compile_dataset": "marching-route-knowledge"}
    folder = ROOT / "tools/system" / paths[name]
    definition = yaml.safe_load((folder / (name + ".yaml")).read_text())
    module, function = definition["execution"]["handler"].rsplit(".", 1)
    engine = QGISToolEngine(load_interactive_tools=False)
    engine.update_contract(execution_manifest=[{"name": name, "runtime": "qgis", "domain": paths[name],
        "handler_file": str(folder / (module.replace(".", "/") + ".py")), "handler_function": function}])
    response = execute_tool(name, params, engine=engine)
    assert response["success"], response
    return response["output"]


def parameters(**extra):
    return dict(dataset_source="explicit", dataset_dir=str(DATASET), route_style=load_handle("red-blue"), **extra)


def layout_fixture():
    from qgis.core import QgsLayoutItemMap, QgsLayoutPoint, QgsLayoutSize, QgsPrintLayout, QgsProject, QgsRectangle
    project = QgsProject.instance()
    layout = QgsPrintLayout(project)
    layout.initializeDefaults()
    layout.setName("annotations")
    project.layoutManager().addLayout(layout)
    item = QgsLayoutItemMap(layout)
    item.setId("main_map")
    item.attemptMove(QgsLayoutPoint(10, 10))
    item.attemptResize(QgsLayoutSize(260, 180))
    item.setCrs(project.crs())
    item.zoomToExtent(QgsRectangle(100, 26, 120, 40))
    layout.addLayoutItem(item)
    return layout


def test_route_points_manifest_defaults_visible_rule_names_and_reuse(style_project, tmp_path):
    from qgis.core import QgsProject, QgsRuleBasedRenderer
    import csv
    dataset = tmp_path / "dataset"
    shutil.copytree(DATASET, dataset)
    with (dataset / "points.csv").open(encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        fields, rows = list(reader.fieldnames), list(reader)
    if "meeting_label" not in fields:
        fields.append("meeting_label")
    rows[0]["meeting_label"] = "参与者甲"
    with (dataset / "points.csv").open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, fields)
        writer.writeheader(); writer.writerows(rows)
    point_params = dict(dataset_source="explicit", dataset_dir=str(dataset))
    first = invoke("route_points_build", point_params)
    assert first["success"], first
    assert first["data"]["points_layer"]["created_count"] == 81
    layer = QgsProject.instance().mapLayersByName("专题点")[0]
    assert layer.featureCount() == 81
    assert isinstance(layer.renderer(), QgsRuleBasedRenderer)
    ordinary = [rule for rule in layer.renderer().rootRule().children() if rule.label() == "普通点"]
    assert len(ordinary) == 1 and 'waypoint' in ordinary[0].filterExpression()
    assert any(rule.description() == "汇合点标注" for rule in layer.labeling().rootRule().children())
    reused = invoke("route_points_build", dict(point_params, overwrite=False))
    assert reused["data"]["layers"][0]["id"] == layer.id()
    assert reused["data"]["points_layer"]["created_count"] == 0


def test_mapping_manifest_exports_actual_existing_handler(style_project):
    from qgis.core import QgsProject
    from autocarto.tools.system._shared.table_io import _read_csv_rows
    built = invoke("route_points_build", parameters(default_symbol_types=[]))
    assert built["success"]
    rows = _read_csv_rows(DATASET / "symbol_mapping.csv")
    for row in rows:
        row["svg_path"] = str(DATASET / row["svg_path"])
    mapped = invoke("cartography_symbolize_points_by_mapping", {"layer": "专题点", "point_rows": _read_csv_rows(DATASET / "points.csv"), "symbol_rows": rows})
    assert mapped["success"] and mapped["data"]["applied_rules"] == 5
    assert QgsProject.instance().mapLayersByName("专题点")[0].featureCount() == 81


def test_callout_manifest_groups_roles_and_direct_csv_images(style_project):
    from qgis.core import QgsProject
    layout = layout_fixture()
    response = invoke("layout_add_callouts", {"layout": layout.name(), "callouts_csv": str(DATASET / "callouts.csv"), "avoid_item_ids": []})
    assert response["success"] and response["data"]["created_count"] == 2
    for entry in response["data"]["items"]:
        for role, values in entry["created_item_ids"].items():
            for item_id in values if isinstance(values, list) else [values]:
                item = layout.itemById(item_id)
                assert item.customProperty("autocarto/callout_group") == entry["id"]
                assert item.customProperty("autocarto/callout_role") == role
                assert item.customProperty("autocarto/callout_part") == role
        assert "image" in entry["created_item_ids"]
    snapshot_handler = __import__("autocarto.tools.system._shared.print_layout_advanced_impl", fromlist=["get_items_properties_handler"])
    snapshot = snapshot_handler.get_items_properties_handler({"layout": layout.name()})
    assert snapshot["success"], snapshot
    assert len(snapshot["data"]["layout_item_ids"]["callouts"]) == 2


def test_callout_avoidance_moves_away_from_explicit_obstacle(style_project, tmp_path):
    import csv
    from qgis.core import QgsLayoutItemShape, QgsLayoutPoint, QgsLayoutSize
    from autocarto.tools.system._shared.callouts_impl import layout_add_callouts_handler
    layout = layout_fixture()
    table = tmp_path / "callouts.csv"
    with table.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.writer(stream)
        writer.writerow(["id", "x", "y", "body", "template", "width_mm"])
        writer.writerow(["A", 110, 33, "避让测试", "text_only", 42])
    first = layout_add_callouts_handler({"layout": layout.name(), "callouts_csv": str(table), "avoid_item_ids": []})
    original = first["data"]["items"][0]["box_rect_mm"]
    for item in first["data"]["items"]:
        for values in item["created_item_ids"].values():
            for item_id in values if isinstance(values, list) else [values]:
                layout.removeLayoutItem(layout.itemById(item_id))
    obstacle = QgsLayoutItemShape(layout)
    obstacle.setId("obstacle")
    obstacle.attemptMove(QgsLayoutPoint(original[0], original[1]))
    obstacle.attemptResize(QgsLayoutSize(original[2], original[3]))
    layout.addLayoutItem(obstacle)
    avoided = layout_add_callouts_handler({"layout": layout.name(), "callouts_csv": str(table), "avoid_item_ids": ["obstacle"]})
    assert avoided["success"], avoided
    assert avoided["data"]["items"][0]["box_rect_mm"] != original


def test_route_draw_manifest_real_renderer_and_dataset_style_priority(style_project, monkeypatch):
    from autocarto.tools.system._shared.route_drawing_impl import _build_route_payloads_from_human_tables
    from qgis.core import QgsProject, QgsCoordinateReferenceSystem, QgsRectangle
    from qgis.utils import iface
    QgsProject.instance().setCrs(QgsCoordinateReferenceSystem("EPSG:4004"))
    iface.mapCanvas().setDestinationCrs(QgsProject.instance().crs())
    iface.mapCanvas().setExtent(QgsRectangle(100, 26, 120, 40))
    renderer = os.environ.get("AUTOCARTO_ROUTE_RENDERER_DIR")
    if not renderer or not Path(renderer).is_dir():
        pytest.skip("需要现有路线渲染插件环境变量")
    result = invoke("route_draw", parameters(renderer_plugin_dir=renderer))
    assert result["success"], result
    assert result["data"]["routes"]["count"] == 16
    assert result["data"]["route_labels"]["count"] > 0
    assert QgsProject.instance().mapLayersByName("路线名称")
    assert not QgsProject.instance().mapLayersByName("专题点")
    table = ROOT / "resources/system/styles/路线样式-单色.csv"
    payloads = _build_route_payloads_from_human_tables(DATASET, renderer, route_style=load_handle("mono"))
    # An unrelated palette has no reference mapping for the dataset's explicit IDs.
    # Comparing with the default palette would inadvertently require its mappings.
    import csv
    with (DATASET / "routes.csv").open(encoding="utf-8-sig", newline="") as stream:
        dataset_styles = {row["style_id"] for row in csv.DictReader(stream) if row["enabled"] == "1"}
    assert {item["style_id"] for item in payloads} == dataset_styles


def test_route_renderer_discovery_uses_current_profile_and_legacy_alias(style_project, tmp_path):
    from unittest import mock
    from autocarto.tools.system._shared.route_runtime_support import renderer_directory
    from autocarto.knowledge.visual_presets import renderer_defaults
    name = renderer_defaults()["renderer_plugin_name"]
    directory = tmp_path / "python/plugins" / name
    (directory / "services").mkdir(parents=True)
    (directory / "services/combined_plot_service.py").write_text("# probe")
    with mock.patch("qgis.core.QgsApplication.qgisSettingsDirPath", return_value=str(tmp_path)):
        assert renderer_directory({}) == str(directory)
    assert renderer_directory({"plugin_dir": str(directory)}) == str(directory)


def test_route_tools_reject_dataset_contract_and_missing_renderer_before_mutation(style_project, tmp_path):
    from qgis.core import QgsProject
    from autocarto.tools.system._shared.route_drawing_impl import route_draw_handler
    from autocarto.tools.system._shared.route_points_impl import route_points_build_handler
    for handler in (route_draw_handler, route_points_build_handler):
        assert handler({"dataset_source": "invalid"})["error_type"] == "route_dataset_source_invalid"
        wrong = handler(parameters(expected_table_counts={"points.csv": 1}))
        assert wrong["success"] is False
        assert wrong["failed_stages"] == ["dataset_handoff"]
    missing = route_draw_handler(parameters(renderer_plugin_dir=str(tmp_path / "absent")))
    assert missing["success"] is False
    assert not QgsProject.instance().mapLayers()


def test_route_style_resource_supplies_missing_ids_and_width_without_changing_input(style_project, tmp_path):
    import csv
    from autocarto.tools.system._shared.route_drawing_impl import _build_route_payloads_from_human_tables
    dataset = tmp_path / "dataset"
    shutil.copytree(DATASET, dataset)
    table = dataset / "routes.csv"
    with table.open(encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        columns, rows = list(reader.fieldnames), list(reader)
    for row in rows:
        row["style_id"] = ""
        row["draw_type"] = ""
        row["width_scale"] = ""
    with table.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, columns)
        writer.writeheader(); writer.writerows(rows)
    before = table.read_bytes()
    payloads = _build_route_payloads_from_human_tables(dataset, "unused", route_style=load_handle("mono"))
    assert len(payloads) == 16
    assert {item["style_id"] for item in payloads} == {"route_blue_reference"}
    assert table.read_bytes() == before


def test_independent_point_and_callout_receipts_are_real_and_combined_tool_is_retired(style_project):
    from qgis.core import QgsProject
    from autocarto.tools.system._shared import route_drawing_impl
    layout = layout_fixture()
    points = invoke("route_points_build", parameters(points_layer="调用者专题点", default_symbol_types=[]))
    annotations = invoke("layout_add_callouts", parameters(layout=layout.name(), avoid_item_ids=[]))
    assert points["success"] and annotations["success"]
    assert points["data"]["points_layer"]["layer_name"] == "调用者专题点"
    layer = QgsProject.instance().mapLayersByName("调用者专题点")[0]
    assert layer.featureCount() == points["data"]["points_layer"]["created_count"] == 81
    assert annotations["data"]["created_count"] == 2 and annotations["data"]["failed_count"] == 0
    assert len(annotations["data"]["items"]) == 2
    assert not hasattr(route_drawing_impl, "cartography_build_and_draw_from_tables_handler")
    assert not (ROOT/"tools/system/cartography/cartography_build_and_draw_from_tables.yaml").exists()


def test_compile_manifest_and_validate_only_consume_the_same_custom_route_resource(style_project, tmp_path):
    import json
    from autocarto.knowledge.extraction import build_extraction_contract
    graph = json.loads((ROOT / "tests/fixtures/route_compiler/graph.json").read_text())
    graph["routes"][0]["style_ref"] = "custom_ref"
    source = "部队从甲地到乙地。"
    table = tmp_path / "styles.csv"
    table.write_text("topology,side,route_type,draw_type,style_id,preset_ref\nstandard,friendly,custom,bezier_arrow,custom_style,custom_ref\n")
    params = {"knowledge_json": graph, "source_text": source,
              "contract_id": build_extraction_contract(source)["contract_id"],
              "route_style": resolve_handle(table, "route_style"), "symbols": load_handle("military"), "output_dir": str(tmp_path / "compiled")}
    preview = invoke("marching_route_compile_dataset", dict(params, validate_only=True))
    assert preview["data"]["validation"]["valid"], preview
    assert not (tmp_path / "compiled").exists()
    compiled = invoke("marching_route_compile_dataset", params)
    assert compiled["success"], compiled
    assert "custom_style" in (tmp_path / "compiled/routes.csv").read_text(encoding="utf-8-sig")


def test_route_reference_and_width_groups_use_the_selected_resource(style_project, monkeypatch):
    from autocarto.tools.system._shared import route_drawing_impl as drawing
    from autocarto.knowledge.visual_presets import load_route_styles
    table = ROOT.parent / "docs/refactor/acceptance-flows/survey-route/resources/styles/考察单色路线.csv"
    selected, _ = load_route_styles(load_handle("survey-route"))
    assert drawing._route_reference_style_id("route_blue_reference", "bezier_arrow", selected) == "route_blue_reference"
    # The caller's entry can map the same ID differently from an installed palette.
    selected["standard_neutral"]["reference_style_id"] = "caller_reference"
    assert drawing._route_reference_style_id("route_blue_reference", "bezier_arrow", selected) == "caller_reference"
    calls = []
    monkeypatch.setattr(drawing, "draw_bezier_arrow_from_points_handler", lambda payload:
        calls.append(payload) or {"success": True, "data": {"width_group_supported": True}})
    monkeypatch.setattr(drawing, "load_route_styles", lambda *args: pytest.fail("不应重新读取默认路线样式"))
    payloads = [{"route_id": "same__render_1", "style_id": "caller_reference", "options": {}},
                {"route_id": "same__render_2", "style_id": "caller_reference", "options": {}}]
    drawing._draw_bezier_route_batch(payloads, selected)
    assert len(calls) == 2 and not any(call.get("_prepare_only") for call in calls)
    calls.clear()
    selected["standard_neutral"]["auto_width_group"] = "true"
    drawing._draw_bezier_route_batch(payloads, selected)
    assert len([call for call in calls if call.get("_prepare_only")]) == 2
    assert all(call["options"]["width_group"]["id"] == "same" for call in calls)


def test_route_points_uses_selected_symbol_descriptions(style_project):
    from qgis.core import QgsProject
    symbols = load_handle("military")
    result = invoke("route_points_build", parameters(symbols=symbols, default_symbol_types=[]))
    assert result["success"], result
    labels = {rule.label() for rule in QgsProject.instance().mapLayersByName("专题点")[0].renderer().rootRule().children()}
    assert {row["description"] for row in symbols["entries"]} <= labels
    assert result["resource"]["id"] == symbols["id"]
