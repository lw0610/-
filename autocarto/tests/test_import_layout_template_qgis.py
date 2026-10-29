"""用真实 QPT 和独立 QGIS 子进程验证模板排版，不复用用户工程。"""
from autocarto.tests.support.resource_handles import load_handle, resolve_handle
import json
import os
from pathlib import Path
import subprocess
import sys

import pytest

from autocarto.tests.golden.qgis_bootstrap import isolated_qgis, qgis_available


def _scene():
    from qgis.core import QgsProject, QgsVectorLayer, QgsFeature, QgsGeometry, QgsCoordinateReferenceSystem
    project = QgsProject.instance()
    project.setCrs(QgsCoordinateReferenceSystem("EPSG:3857"))
    for name, crs, wkt in (("投影图层", "EPSG:3857", "POLYGON((0 0,1000 0,1000 500,0 500,0 0))"),
                           ("地理图层", "EPSG:4326", "POLYGON((110 30,111 30,111 31,110 31,110 30))")):
        layer = QgsVectorLayer("Polygon?crs="+crs, name, "memory")
        feature = QgsFeature(layer.fields())
        feature.setGeometry(QgsGeometry.fromWkt(wkt))
        layer.dataProvider().addFeatures([feature])
        layer.updateExtents()
        project.addMapLayer(layer)
    return project


def _create_template(project, output, *, map_id="main_map", maps=True, legend=True, centimeters=False):
    from qgis.core import (QgsPrintLayout, QgsLayoutItemLabel, QgsLayoutItemMap, QgsLayoutItemLegend,
                           QgsLayoutItemPicture, QgsLayoutPoint, QgsLayoutSize, QgsUnitTypes,
                           QgsReadWriteContext, QgsPathResolver, QgsRectangle, QgsCoordinateReferenceSystem)
    path = output/"模板.qpt"
    source = QgsPrintLayout(project)
    source.initializeDefaults()
    if centimeters:
        source.setUnits(QgsUnitTypes.LayoutCentimeters)
    for item_id, text, y, html_mode in (("title", "{{标题}}", 10, False),
                                       ("overview", "{{综述}} / {{数据来源}}", 100, False),
                                       ("html", "<b>{{标题}}</b>", 120, True)):
        item = QgsLayoutItemLabel(source)
        item.setId(item_id)
        item.setText(text)
        if item_id == "title":
            from autocarto.tools.system._shared.print_layout_advanced_impl import _build_mm_text_format
            item.setTextFormat(_build_mm_text_format("Noto Sans CJK SC", 6.25, True))
        if html_mode:
            item.setMode(QgsLayoutItemLabel.ModeHtml)
        source.addLayoutItem(item)
        item.attemptResize(QgsLayoutSize(120, 12, QgsUnitTypes.LayoutMillimeters))
        item.attemptMove(QgsLayoutPoint(20, y, QgsUnitTypes.LayoutMillimeters))
    if maps:
        main = QgsLayoutItemMap(source)
        main.setId(map_id)
        main.setCrs(QgsCoordinateReferenceSystem("EPSG:3857"))
        main.setLayers([project.mapLayersByName("投影图层")[0]])
        main.setKeepLayerSet(True)
        source.addLayoutItem(main)
        main.attemptResize(QgsLayoutSize(120, 60, QgsUnitTypes.LayoutMillimeters))
        main.attemptMove(QgsLayoutPoint(20, 30, QgsUnitTypes.LayoutMillimeters))
        main.zoomToExtent(QgsRectangle(0, 0, 1000, 500))
        if map_id != "main_map":
            required_main = QgsLayoutItemMap(source)
            required_main.setId("main_map")
            source.addLayoutItem(required_main)
            required_main.attemptResize(QgsLayoutSize(1, 1, QgsUnitTypes.LayoutMillimeters))
        if legend:
            native = QgsLayoutItemLegend(source)
            native.setId("legend")
            native.setLinkedMap(main)
            native.setAutoUpdateModel(False)
            native.model().rootGroup().addLayer(project.mapLayersByName("投影图层")[0])
            source.addLayoutItem(native)
    if centimeters:
        for item_id in ("", "title"):
            label = QgsLayoutItemLabel(source)
            label.setId(item_id)
            label.setText("附加元素")
            source.addLayoutItem(label)
        symbol = output/"symbol.svg"
        symbol.write_text('<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><rect width="20" height="20" fill="red"/></svg>')
        picture = QgsLayoutItemPicture(source)
        picture.setId("picture")
        picture.setPicturePath(str(symbol))
        source.addLayoutItem(picture)
    context = QgsReadWriteContext()
    context.setPathResolver(QgsPathResolver(str(path)))
    assert source.saveAsTemplate(str(path), context)
    return path


def _execute_manifest(path, params):
    """通过 authority 的 handler_file/function 契约调用迁移后的真实 wrapper。"""
    import yaml
    from autocarto.core.tool_engine.definitions import ToolDefinitionLoader
    from autocarto.core.tool_engine.executor import ProcessingToolExecutor
    root = Path(__file__).resolve().parents[1]
    definition_path = root/"tools/system/print-layout/import_layout_template.yaml"
    definition = yaml.safe_load(definition_path.read_text())
    module, function = definition["execution"]["handler"].rsplit(".", 1)
    handler_file = definition_path.parent/Path(*module.split(".")).with_suffix(".py")
    manifest = [{"name": definition["metadata"]["name"], "domain": definition["metadata"]["domain"],
                 "runtime": "qgis", "parameters": definition["spec"]["parameters"],
                 "handler_file": str(handler_file), "handler_function": function}]
    loader = ToolDefinitionLoader(manifest)
    return ProcessingToolExecutor(loader.load_all()).execute("import_layout_template", {"template": resolve_handle(path, "layout"), "name": "模板测试", **params})


def _run_case(case, output):
    from qgis.core import (QgsLayoutItemLabel, QgsCoordinateReferenceSystem, QgsCoordinateTransform,
                           QgsRectangle, QgsLayoutExporter)
    from autocarto.tools.system._shared.print_layout_impl import import_layout_template_handler
    project = _scene()
    manager = project.layoutManager()
    path = _create_template(project, output, map_id="custom_map" if case == "explicit_extent" else "main_map",
                            maps=case != "missing_main_map", centimeters=case == "relative_resources_snapshot")

    def invoke(**params):
        result = import_layout_template_handler({"template": resolve_handle(path, "layout"), "name": "模板测试", **params})
        assert result["success"], result
        return result["data"], manager.layoutByName("模板测试")

    if case == "texts_layers_and_extent":
        root = project.layerTreeRoot()
        root.findLayer(project.mapLayersByName("地理图层")[0].id()).setItemVisibilityChecked(False)
        before = [(node.layerId(), node.isVisible()) for node in root.findLayers()]
        result = _execute_manifest(path, {"texts": {"标题": "A < B & C", "综述": "调用者编写的综述\n第二行"},
                                          "map_layers": ["地理图层", "投影图层"], "extent_layers": ["地理图层"],
                                          "legend_layers": ["投影图层"], "padding": 0.05})
        assert result["success"], result
        data = result["data"]
        layout = manager.layoutByName("模板测试")
        assert data["page_count"] == 1
        assert layout.itemById("title").text() == "A < B & C"
        from qgis.core import Qgis
        assert layout.itemById("title").textFormat().size() == 6.25
        assert layout.itemById("title").textFormat().sizeUnit() == Qgis.RenderUnit.Millimeters
        assert layout.itemById("title").textFormat().font().bold()
        assert layout.itemById("overview").text() == "调用者编写的综述\n第二行 / {{数据来源}}"
        assert layout.itemById("html").text() == "<b>A &lt; B &amp; C</b>"
        assert data["unreplaced_placeholders"] == ["数据来源"]
        assert len(data["warnings"]) == 1 and "{{数据来源}}" in data["warnings"][0]
        main = layout.itemById("main_map")
        assert [layer.name() for layer in main.layers()] == ["地理图层", "投影图层"]
        assert not root.hasCustomLayerOrder()
        assert [(node.layerId(), node.isVisible()) for node in root.findLayers()] == before
        layer = project.mapLayersByName("地理图层")[0]
        extent = QgsCoordinateTransform(layer.crs(), main.crs(), project).transformBoundingBox(layer.extent())
        extent.scale(1.1)
        assert main.extent().contains(extent)
        assert main.extent().center().x() == pytest.approx(extent.center().x())
        assert main.extent().center().y() == pytest.approx(extent.center().y())
        assert data["rects_mm"]["main_map"] == pytest.approx({"x": 20, "y": 30, "width": 120, "height": 60})
        assert data["rects_mm"]["title"] == pytest.approx({"x": 20, "y": 10, "width": 120, "height": 12})
        legend = layout.itemById("legend")
        assert [node.layer().name() for node in legend.model().rootGroup().findLayers()] == ["投影图层"]
        settings = QgsLayoutExporter.ImageExportSettings()
        settings.dpi = 80
        assert QgsLayoutExporter(layout).exportToImage(str(output/"preview.png"), settings) == QgsLayoutExporter.Success
    elif case == "explicit_extent":
        before = [layer.id() for layer in project.layerTreeRoot().layerOrder()]
        data, layout = invoke(map_item_id="custom_map", map_layers=["投影图层"],
                              extent={"xmin": 110, "ymin": 30, "xmax": 111, "ymax": 31, "crs": "EPSG:4326"}, padding=0)
        main = layout.itemById("custom_map")
        extent = QgsCoordinateTransform(QgsCoordinateReferenceSystem("EPSG:4326"), main.crs(), project).transformBoundingBox(QgsRectangle(110, 30, 111, 31))
        assert main.extent().contains(extent)
        assert main.extent().center().x() == pytest.approx(extent.center().x())
        assert main.crs().authid() == "EPSG:3857"
        assert data["rects_mm"]["custom_map"]["width"] == 120
        assert [layer.id() for layer in project.layerTreeRoot().layerOrder()] == before
        assert not project.layerTreeRoot().hasCustomLayerOrder()
    elif case == "relative_resources_snapshot":
        os.environ["AUTOCARTO_PROJECT_ROOT"] = str(output)
        elsewhere = output/"other-cwd"
        elsewhere.mkdir()
        previous = Path.cwd()
        os.chdir(elsewhere)
        try:
            result = import_layout_template_handler({"template": resolve_handle(path, "layout"), "name": "模板测试"})
        finally:
            os.chdir(previous)
        assert result["success"], result
        data = result["data"]
        layout = manager.layoutByName("模板测试")
        assert "./symbol.svg" in path.read_text()
        assert Path(layout.itemById("picture").picturePath()) == output/"symbol.svg"
        assert len(data["item_ids"]) == len(set(data["item_ids"])) == data["item_count"]
        assert set(data["item_ids"]) == set(data["rects_mm"])
        assert len(data["item_ids"]) == 9
        assert data["page_count"] == 1
        assert data["rects_mm"]["main_map"] == pytest.approx({"x": 20, "y": 30, "width": 120, "height": 60})
        assert [layer.name() for layer in layout.itemById("main_map").layers()] == ["投影图层"]
        assert not project.layerTreeRoot().hasCustomLayerOrder()
    elif case == "missing_main_map":
        # The resource contract rejects a label-only QPT before any QGIS mutation.
        with pytest.raises(ValueError, match="main_map"):
            resolve_handle(path, "layout")
        assert manager.layouts() == []
    elif case == "rollback":
        _, layout = invoke(texts={"标题": "必须保留"})
        for settings in ({"map_item_id": "不存在", "map_layers": ["投影图层"]}, {"map_layers": ["丢失图层"]},
                         {"map_layers": ["投影图层"], "extent_layers": ["投影图层"], "extent": {"crs": "EPSG:3857"}},
                         {"extent": {"xmin": 0, "ymin": 0, "xmax": 1, "ymax": 1, "crs": "invalid"}},
                         {"texts": {"标题": None}}):
            failed = import_layout_template_handler({"template": resolve_handle(path, "layout"), "name": "模板测试", **settings})
            assert not failed["success"], failed
            assert manager.layoutByName("模板测试") is layout
            assert layout.itemById("title").text() == "必须保留"
        malformed = output/"broken.qpt"
        malformed.write_text('<Layout><LayoutItem id="main_map"/></Layout>')
        handle = resolve_handle(malformed, "layout")
        malformed.write_text("<Layout>")
        failed = import_layout_template_handler({"template": handle, "name": "模板测试"})
        assert not failed["success"]
        assert manager.layoutByName("模板测试") is layout
        wrong = output/"wrong.qpt"
        wrong.write_text('<Layout><LayoutItem id="main_map"/></Layout>')
        handle = resolve_handle(wrong, "layout")
        wrong.write_text("<Project/>")
        failed = import_layout_template_handler({"template": handle, "name": "模板测试"})
        assert not failed["success"]
        assert "Layout" in failed["info"]
    return {"success": True, "case": case}


@pytest.mark.parametrize("case", ["texts_layers_and_extent", "explicit_extent", "relative_resources_snapshot", "missing_main_map", "rollback"])
def test_import_layout_template_qgis(case, tmp_path):
    if not qgis_available():
        pytest.skip("需要独立 QGIS Python 环境")
    process = subprocess.run([sys.executable, str(Path(__file__).resolve()), "--case", case, "--out", str(tmp_path)],
                             env=dict(os.environ, QT_QPA_PLATFORM="offscreen", PYTHONDONTWRITEBYTECODE="1"),
                             capture_output=True, text=True, timeout=180)
    assert process.returncode == 0, process.stdout+"\n"+process.stderr
    assert json.loads(process.stdout.strip().splitlines()[-1])["success"]


if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    with isolated_qgis(args.out):
        result = _run_case(args.case, args.out)
    print(json.dumps(result, ensure_ascii=False))
