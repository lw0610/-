"""T46：每个真实场景独立初始化 QGIS，所有文件和图层均为临时合成夹具。"""
from autocarto.tests.support.resource_handles import load_handle, resolve_handle
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

import pytest

from autocarto.tests.golden.qgis_bootstrap import isolated_qgis, qgis_available


def _scene():
    from qgis.core import (QgsProject, QgsVectorLayer, QgsFeature, QgsGeometry, QgsCoordinateReferenceSystem,
                           QgsPrintLayout, QgsLayoutItemMap, QgsLayoutPoint, QgsLayoutSize, QgsRectangle)
    project = QgsProject.instance()
    project.setCrs(QgsCoordinateReferenceSystem("EPSG:3857"))
    layer = QgsVectorLayer("Polygon?crs=EPSG:3857", "输入数据", "memory")
    feature = QgsFeature(layer.fields())
    feature.setGeometry(QgsGeometry.fromWkt("POLYGON((0 0,1000 0,1000 500,0 500,0 0))"))
    layer.dataProvider().addFeatures([feature])
    layer.updateExtents()
    project.addMapLayer(layer)
    layout = QgsPrintLayout(project)
    layout.initializeDefaults()
    layout.setName("测试版面")
    project.layoutManager().addLayout(layout)
    main = QgsLayoutItemMap(layout)
    main.setId("main_map")
    layout.addLayoutItem(main)
    main.attemptMove(QgsLayoutPoint(20, 20))
    main.attemptResize(QgsLayoutSize(200, 150))
    main.setCrs(project.crs())
    main.setLayers([layer])
    main.zoomToExtent(QgsRectangle(0, 0, 1000, 500))
    return project, layer, layout, main


def _label(layout, item_id="label", x=40, y=40, width=20, height=10):
    from qgis.core import QgsLayoutItemLabel, QgsLayoutPoint, QgsLayoutSize
    label = QgsLayoutItemLabel(layout)
    label.setId(item_id)
    label.setText("原始文字")
    layout.addLayoutItem(label)
    label.attemptMove(QgsLayoutPoint(x, y))
    label.attemptResize(QgsLayoutSize(width, height))
    return label


def _run_case(case, output, manifest=None):
    from qgis.core import (Qgis, QgsLayoutItemLegend, QgsLayoutItemScaleBar, QgsLegendStyle,
                           QgsLayoutSize, QgsLayoutPoint, QgsLayoutItemShape)
    from qgis.PyQt.QtGui import QImage, QColor
    from autocarto.tools.system._shared import print_layout_advanced_impl as advanced, print_layout_items_impl as items
    project, layer, layout, main = _scene()
    base = {"layout": layout.name()}
    if case == "plan_observation":
        from qgis.core import QgsLayoutItemMapGrid
        from autocarto.tools.system._shared.print_layout_impl import get_layout_info_handler
        main.setCustomProperty("autocarto/compose_role", "main_map")
        grid = QgsLayoutItemMapGrid("verification-grid", main)
        main.grids().addGrid(grid)
        grid.setEnabled(True)
        note = _label(layout, "source_notes")
        note.setText("来源：测试夹具；仅验证结构")
        observed = get_layout_info_handler({"name": layout.name()})
        assert observed["success"], observed
        by_id = {item["item_id"]: item for item in observed["layout"]["items"]}
        assert by_id["main_map"]["type"] == "QgsLayoutItemMap"
        assert by_id["main_map"]["role"] == "main_map"
        assert by_id["main_map"]["grids"] == [{"name": "verification-grid", "enabled": True}]
        assert by_id["source_notes"]["text"] == note.text()
        assert layout.itemById("source_notes").text() == "来源：测试夹具；仅验证结构"
    elif case == "legend_extras":
        legend = QgsLayoutItemLegend(layout)
        legend.setId("legend")
        legend.setLinkedMap(main)
        layout.addLayoutItem(legend)
        png = output/"symbol.png"
        image = QImage(10, 10, QImage.Format_ARGB32)
        image.fill(QColor("red"))
        assert image.save(str(png))
        svg = output/"symbol.svg"
        svg.write_text('<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><circle cx="5" cy="5" r="4" fill="blue"/></svg>')
        extras = [{"label": "像素示意", "image_path": str(png), "size_mm": 5}, {"label": "矢量示意", "image_path": str(svg), "size_mm": 6}]
        params = {**base, "item_id": "legend", "legend_layers": [layer.id()], "legend_extra_items": extras}
        result = advanced.set_legend_style_handler(params)
        assert result["success"], result
        nodes = legend.model().rootGroup().findLayers()
        assert [node.layer().name() for node in nodes] == ["输入数据", "像素示意", "矢量示意"]
        for node in nodes[1:]:
            assert node.layer().customProperty("autocarto/legend_extra_item")
            assert project.layerTreeRoot().findLayer(node.layer().id()) is None
        count = len(project.mapLayers())
        assert advanced.set_legend_style_handler(params)["success"]
        assert len(project.mapLayers()) == count
        bad = advanced.set_legend_style_handler({**params, "legend_layers": [], "legend_extra_items": [extras[0], {"label": "丢失", "image_path": str(output/"missing.png")}]})
        assert not bad["success"]
        assert len(legend.model().rootGroup().findLayers()) == 3
        assert len(project.mapLayers()) == count
        kept = advanced.set_legend_style_handler({**base, "item_id": "legend", "title": "说明"})
        assert kept["success"] and len(legend.model().rootGroup().findLayers()) == 3
        legacy = advanced.set_legend_style_handler({**base, "item_id": "legend", "legend_extra_items": [{"label": "路线箭头", "symbol_type": "route_arrow"}], "legend_symbols": load_handle("route-arrow")})
        assert legacy["success"], legacy
        assert [entry["name"] for entry in legacy["data"]["legend_layers"]] == ["输入数据", "路线箭头"]
        old = legend.model().rootGroup().findLayers()[-1].layer()
        old.removeCustomProperty("autocarto/legend_extra_item")
        old.setCustomProperty("autocarto/military_arrow_legend", True)
        assert advanced.set_legend_style_handler({**base, "item_id": "legend"})["success"]
        assert len(legend.model().rootGroup().findLayers()) == 1
    elif case == "font_lock":
        label = _label(layout)
        label.setTextFormat(advanced._build_mm_text_format("Noto Sans CJK SC", 6.25, True))
        legend = QgsLayoutItemLegend(layout)
        legend.setId("legend")
        layout.addLayoutItem(legend)
        scale = QgsLayoutItemScaleBar(layout)
        scale.setId("scale")
        scale.setLinkedMap(main)
        layout.addLayoutItem(scale)
        scale.setTextFormat(advanced._build_mm_text_format("Noto Sans CJK SC", 2.5))
        for item, handler, size_param in ((label, advanced.set_label_style_handler, "font_size"),
                                         (legend, advanced.set_legend_style_handler, "body_font_size"),
                                         (scale, advanced.set_scalebar_style_handler, "font_size")):
            item.setCustomProperty("autocarto/font_locked", True)
            result = handler({**base, "item_id": item.id(), size_param: 7, "text": "不得部分改变"})
            assert not result["success"] and result["info"] == "该元素字号由版式锁定，如需修改请传 unlock_font=true"
            allowed = handler({**base, "item_id": item.id(), size_param: 7, "unlock_font": True})
            assert allowed["success"], allowed
            assert item.customProperty("autocarto/font_locked") is True
        assert label.text() == "原始文字"
        result = advanced.set_label_style_handler({**base, "item_id": label.id(), "font_color": "#123456", "font_family": "Arial"})
        assert result["success"] and label.textFormat().size() == 7
        assert label.textFormat().sizeUnit() == Qgis.RenderUnit.Millimeters
        assert label.textFormat().font().bold()
        assert advanced.set_scalebar_style_handler({**base, "item_id": "scale", "font_family": "Arial"})["success"]
        assert scale.textFormat().size() == 7
        advanced._apply_legend_text_format(legend, [QgsLegendStyle.Group], advanced._build_mm_text_format("Arial", 4.25))
        assert advanced.set_legend_style_handler({**base, "item_id": "legend", "body_font_family": "Arial"})["success"]
        style = legend.style(QgsLegendStyle.SymbolLabel)
        assert style.textFormat().size() == 7
        group_style = legend.style(QgsLegendStyle.Group)
        assert group_style.textFormat().size() == 4.25
    elif case == "snapshot":
        from autocarto.tools.system._shared.layout_compose_impl import compose_layout_handler
        result = compose_layout_handler({**base, "spec": load_handle("four-corner"), "paper": "A4", "title": "锚点测试", "overview_text": "调用者正文"})
        assert result["success"], result
        layout = project.layoutManager().layoutByName(base["layout"])
        for role in ("box", "title", "body", "leader", "leader"):
            label = _label(layout, item_id="callout_"+role if role != "leader" else "line_"+str(len(layout.items())))
            label.setCustomProperty("autocarto/callout_group", "stable-row-id")
            label.setCustomProperty("autocarto/callout_role", role)
        state = advanced.get_items_properties_handler(base)
        assert state["success"], state
        data = state["data"]
        assert data["layout_item_ids"]["standard"]["main_map"] == "main_map"
        assert "grid" in data["layout_item_ids"]["standard"]
        group = data["layout_item_ids"]["callouts"]["stable-row-id"]
        assert set(group) == {"box", "title", "body", "leader"} and len(group["leader"]) == 2
        assert data["items"]["title"]["anchor"]["to"] == "border"
        assert data["items"]["title"]["font_locked"] is True
        snapshot = json.loads(Path(data["layout_snapshot_path"]).read_text())
        assert snapshot["layout_item_ids"] == data["layout_item_ids"]
        assert snapshot["layout_item_state"]["items"]["title"]["anchor"] == data["items"]["title"]["anchor"]
        image = advanced.export_image_handler({**base, "purpose": "review", "output": str(output/"initial.png")})
        assert image["success"], image
        import importlib.util
        script = Path(__file__).resolve().parents[1]/"tools/system/quality-feedback/scripts/quality_review.py"
        spec = importlib.util.spec_from_file_location("review_test", script)
        review = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(review)
        prepared = review.prepare_review({"original_image_path": image["output"], "layout_snapshot_path": data["layout_snapshot_path"],
                                          "task_requirement": "检查已有地图", "locked_content": ["原始文字"]})
        assert prepared["review_context"]["legal_anchors"]["title"] == data["items"]["title"]["anchor"]
    elif case == "review_dpi":
        for paper, expected in (("A1", 140), ("A0", 100), ("A4", 200)):
            layout.pageCollection().page(0).setPageSize(paper)
            result = advanced.export_image_handler({**base, "purpose": "review", "output": str(output/(paper+".png"))})
            assert result["success"], result
            assert result["data"]["dpi"] == expected
            image = QImage(result["output"])
            assert not image.isNull() and image.width()*image.height() <= 16_000_000
        layout.pageCollection().page(0).setPageSize(QgsLayoutSize(1000, 1000))
        result = advanced.export_image_handler({**base, "purpose": "review", "dpi": 300, "output": str(output/"budget.png")})
        assert result["success"] and 0 < result["data"]["dpi"] < 200
        image = QImage(result["output"])
        assert image.width()*image.height() <= 16_000_000
        layout.pageCollection().page(0).setPageSize("A4")
        explicit = advanced.export_image_handler({**base, "purpose": "review", "dpi": 72, "output": str(output/"explicit.png")})
        assert explicit["success"] and explicit["data"]["dpi"] == 72
        layout.renderContext().setDpi(96)
        final = advanced.export_image_handler({**base, "output": str(output/"final.png")})
        assert final["success"] and final["data"] == {"dpi": 96, "purpose": "final"}
        for invalid in (0, -1, float("inf"), True):
            assert not advanced.export_image_handler({**base, "purpose": "review", "dpi": invalid})["success"]
    elif case == "bounds":
        label = _label(layout)
        before = advanced._item_rect(label)
        bad = items.move_item_handler({**base, "item_id": "label", "x": 215, "y": 40, "container_item_id": "main_map"})
        assert not bad["success"] and "right" in bad["data"]["overflow_edges"]
        assert advanced._item_rect(label) == before
        bad = items.resize_item_handler({**base, "item_id": "label", "width": 250, "height": 10, "container_item_id": "main_map"})
        assert not bad["success"] and "right" in bad["data"]["overflow_edges"]
        assert advanced._item_rect(label) == before
        good = items.move_item_handler({**base, "item_id": "label", "x": 50, "y": 50, "container_item_id": "main_map"})
        assert good["success"], good
        bad = items.move_item_handler({**base, "item_id": "label", "x": -2, "y": -3, "container_item_id": "page"})
        assert not bad["success"] and set(bad["data"]["overflow_edges"]) == {"left", "top"}
        label.setItemRotation(45)
        before = advanced._item_rect(label)
        bad = items.move_item_handler({**base, "item_id": "label", "x": 220, "y": 165, "container_item_id": "main_map"})
        assert not bad["success"] and advanced._item_rect(label) == before
    elif case == "callout_bounds":
        from autocarto.tools.system._shared.print_layout_callout_impl import move_callout_handler
        box = QgsLayoutItemShape(layout)
        box.setId("box")
        layout.addLayoutItem(box)
        box.attemptMove(QgsLayoutPoint(40, 40))
        box.attemptResize(QgsLayoutSize(40, 25))
        body = _label(layout, "body", 50, 45, 80, 15)
        before = advanced._item_rect(body)
        bad = move_callout_handler({**base, "callout_item_ids": {"box": "box", "body": "body"}, "x": 170, "y": 50, "container_item_id": "main_map"})
        assert not bad["success"] and "right" in bad["data"]["overflow_edges"]
        assert advanced._item_rect(body) == before
        good = move_callout_handler({**base, "callout_item_ids": {"box": "box", "body": "body"}, "x": 50, "y": 55, "container_item_id": "main_map"})
        assert good["success"], good
    elif case == "scalebar_actual_geometry":
        from qgis.core import QgsLayoutItemMap, QgsCoordinateReferenceSystem, QgsRectangle
        project.setCrs(QgsCoordinateReferenceSystem("EPSG:4326"))
        main.setCrs(project.crs())
        main.zoomToExtent(QgsRectangle(110, 30, 112, 31))
        original_crs = main.crs()
        original_extent = main.extent()
        result = items.add_scalebar_handler({**base, "map_id": "main_map", "item_id": "scale", "width": 80})
        assert result["success"], result
        scale = layout.itemById("scale")
        assert scale.linkedMap() is main
        assert main.crs() == original_crs and main.extent() == original_extent
        assert [item for item in layout.items() if isinstance(item, QgsLayoutItemMap)] == [main]
        assert not main.excludeFromExports()
        assert scale.units() == Qgis.DistanceUnit.Kilometers
        assert scale.segmentSizeMode() == Qgis.ScaleBarSegmentSizeMode.FitWidth
        assert scale.minimumBarWidth() == 60 and scale.maximumBarWidth() == 80
        assert result["data"]["bar_width_range_mm"] == {"minimum": 60, "maximum": 80}
        resized = items.resize_item_handler({**base, "item_id": "scale", "width": 1, "height": 1})
        assert resized["success"], resized
        data = resized["data"]
        assert data["requested_size_mm"] == {"width": 1, "height": 1}
        assert data["rect_mm"] == advanced._item_rect(scale)
        actual = data["rect_mm"]
        assert data["auto_adjusted"] == (abs(actual["width"]-1)>0.1 or abs(actual["height"]-1)>0.1)
        assert data["auto_adjusted"] is True
        main.setCrs(QgsCoordinateReferenceSystem("EPSG:3857"))
        grid_result = items.add_grid_handler({**base, "map_id": "main_map", "interval_x": 0.1, "interval_y": 0.1})
        assert grid_result["success"], grid_result
        assert main.grids().asList()[0].crs() == project.crs()
    elif case == "map_frame_extent_ratio":
        from qgis.core import QgsLayoutItemMap
        result = items.add_map_item_handler({**base, "item_id": "measured_map"})
        assert result["success"], result
        measured = layout.itemById("measured_map")
        assert isinstance(measured, QgsLayoutItemMap)
        assert measured.crs() == project.crs() == layer.crs()
        size = measured.sizeWithUnits()
        assert size.width()/size.height() == pytest.approx(layer.extent().width()/layer.extent().height())
        assert measured.extent().width()/measured.extent().height() == pytest.approx(size.width()/size.height())
    elif case == "overview":
        from unittest.mock import patch
        from autocarto.tools.system._shared.layout_compose_impl import compose_layout_handler
        text = "调用者正文\n保持完整 <原样> & 不读取源文件"
        with patch("builtins.open", side_effect=AssertionError("不得读取源文件")):
            result = compose_layout_handler({"layout": "综述测试", "spec": load_handle("four-corner"), "title": "调用者标题", "overview_text": text, "overview_title": "来源", "overview_max_width_ratio": 1})
        assert result["success"], result
        actual = project.layoutManager().layoutByName("综述测试").itemById("overview")
        assert "调用者正文<br/>保持完整 &lt;原样&gt; &amp; 不读取源文件" in actual.text()
        empty = compose_layout_handler({"layout": "无综述", "spec": load_handle("four-corner"), "title": "标题"})
        assert empty["success"] and "overview" not in empty["data"]["item_ids"]
    elif case == "handlers_manifest":
        from autocarto.core.tool_engine.definitions import ToolDefinitionLoader
        from autocarto.core.tool_engine.executor import ProcessingToolExecutor
        definitions = ToolDefinitionLoader(json.loads(manifest.read_text())).load_all()
        executor = ProcessingToolExecutor(definitions)
        for name in ("layout_get_item_corners", "layout_set_map_scale"):
            assert "print-layout/handlers/layout_items.py" in definitions[name]["handler_file"]
        corners = executor.execute("layout_get_item_corners", base)
        assert corners["success"], corners
        assert corners["data"]["corners"]["bottom_right"] == {"x": 220, "y": 170}
        scale = executor.execute("layout_set_map_scale", {**base, "item_id": "main_map", "scale": 25000})
        assert scale["success"] and main.scale() == pytest.approx(25000), scale
    return {"success": True, "case": case}


def _authority_manifest(output):
    if shutil.which("go") is None:
        pytest.skip("真实 authority manifest 验证需要 Go")
    repository = Path(__file__).resolve().parents[2]
    module = repository/"carto_agent"
    with tempfile.TemporaryDirectory(prefix=".t46-manifest-", dir=module) as temporary:
        Path(temporary, "main.go").write_text('''package main
import("encoding/json";"os";"github.com/autocarto/carto_agent/internal/tool")
func main(){defs,err:=tool.LoadAuthorityDefinitions(os.Args[1]);if err!=nil{panic(err)};json.NewEncoder(os.Stdout).Encode(tool.BuildExecutionManifestEntries(defs))}
''')
        result = subprocess.run(["go", "run", ".", str(repository/"autocarto/tools/system/print-layout")], cwd=temporary,
                                env=dict(os.environ, GOPROXY="off"), capture_output=True, text=True, timeout=180)
    assert result.returncode == 0, result.stdout+result.stderr
    path = output/"authority-manifest.json"
    path.write_text(result.stdout)
    return path


@pytest.mark.parametrize("case", ["plan_observation", "legend_extras", "font_lock", "snapshot", "review_dpi", "bounds", "callout_bounds", "overview", "handlers_manifest", "scalebar_actual_geometry", "map_frame_extent_ratio"])
def test_layout_refactor_qgis(case, tmp_path):
    if not qgis_available():
        pytest.skip("需要独立 QGIS Python 环境")
    args = [sys.executable, str(Path(__file__).resolve()), "--case", case, "--out", str(tmp_path)]
    if case == "handlers_manifest":
        args += ["--manifest", str(_authority_manifest(tmp_path))]
    process = subprocess.run(args, env=dict(os.environ, QT_QPA_PLATFORM="offscreen", PYTHONDONTWRITEBYTECODE="1"),
                             capture_output=True, text=True, timeout=180)
    assert process.returncode == 0, process.stdout+"\n"+process.stderr
    assert json.loads(process.stdout.strip().splitlines()[-1])["success"]


if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--manifest", type=Path)
    args = parser.parse_args()
    with isolated_qgis(args.out):
        result = _run_case(args.case, args.out, args.manifest)
    print(json.dumps(result, ensure_ascii=False))
