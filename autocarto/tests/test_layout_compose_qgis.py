"""每个集成场景使用独立 QGIS 子进程，不接触运行中的桌面工程。"""
from autocarto.tests.support.resource_handles import load_handle, resolve_handle
import json
import os
from pathlib import Path
import subprocess
import sys

import pytest

from autocarto.tests.golden.qgis_bootstrap import isolated_qgis, qgis_available


def _scene(output):
    from qgis.core import QgsProject, QgsVectorLayer, QgsFeature, QgsGeometry, QgsCoordinateReferenceSystem
    project = QgsProject.instance()
    project.setCrs(QgsCoordinateReferenceSystem("EPSG:3857"))
    for name, crs, wkt in (("主图层", "EPSG:3857", "POLYGON((0 0,1000 0,1000 500,0 500,0 0))"),
                           ("地理图层", "EPSG:4326", "POLYGON((110 30,111 30,111 31,110 31,110 30))")):
        layer = QgsVectorLayer("Polygon?crs="+crs, name, "memory")
        feature = QgsFeature(layer.fields())
        feature.setGeometry(QgsGeometry.fromWkt(wkt))
        layer.dataProvider().addFeatures([feature])
        layer.updateExtents()
        project.addMapLayer(layer)
    project.layerTreeRoot().findLayer(project.mapLayersByName("地理图层")[0].id()).setItemVisibilityChecked(False)
    return project


def _run_case(case, output):
    from qgis.core import Qgis, QgsLayoutItemLabel, QgsLayoutItemMapGrid, QgsCoordinateTransform
    from qgis.PyQt.QtGui import QImage, QColor
    from autocarto.tools.system._shared.layout_compose_impl import compose_layout_handler
    project = _scene(output)
    common = {"layout": "组合测试", "spec": load_handle("four-corner"), "paper": "A4", "title": "真实排版测试"}

    def compose(**overrides):
        result = compose_layout_handler({**common, **overrides})
        assert result["success"], result
        return result["data"], project.layoutManager().layoutByName(common["layout"])

    if case == "replace_and_optional":
        result, layout = compose(overview_text="")
        assert "overview" not in result["item_ids"]
        marker = QgsLayoutItemLabel(layout)
        marker.setId("legacy_marker")
        marker.setText("旧内容")
        layout.addLayoutItem(marker)
        refused = compose_layout_handler({**common, "replace_existing": False})
        assert refused["success"] is False
        assert layout.itemById("legacy_marker").text() == "旧内容"
        invalid_title = compose_layout_handler({**common, "title": None})
        assert not invalid_title["success"]
        assert layout.itemById("legacy_marker").text() == "旧内容"
        result, replaced = compose(elements={"border": False, "grid": False, "legend": False, "scalebar": False, "north_arrow": False})
        assert replaced.itemById("legacy_marker") is None
        assert set(result["item_ids"]) == {"main_map", "title"}
        assert len(project.layoutManager().layouts()) == 1
        assert result["warnings"]  # 装饰框关闭后的标题回退可见，不静默改变锚点。
        retained = replaced.itemById("title").text()
        bad = compose_layout_handler({**common, "extent": {"xmin": 0}, "replace_existing": True})
        assert not bad["success"]
        assert replaced.itemById("title").text() == retained
    elif case == "point_layer_projected_extent":
        from qgis.core import QgsVectorLayer, QgsFeature, QgsGeometry, QgsPointXY
        points = QgsVectorLayer("Point?crs=EPSG:4326", "采样站点", "memory")
        for x, y in [(110, 30), (111, 31), (112, 30.5)]:
            feature = QgsFeature(points.fields())
            feature.setGeometry(QgsGeometry.fromPointXY(QgsPointXY(x, y)))
            assert points.dataProvider().addFeatures([feature])[0]
        points.updateExtents()
        project.addMapLayer(points)
        result, layout = compose(map_layers=[points.name()], extent_layers=[points.name()])
        main = layout.itemById("main_map")
        transform = QgsCoordinateTransform(points.crs(), main.crs(), project)
        for feature in points.getFeatures():
            assert main.extent().contains(transform.transform(feature.geometry().asPoint()))
        assert main.extent().width() > 0 and main.extent().height() > 0
    elif case == "grid_extent_and_metadata":
        result, layout = compose(map_layers=["主图层", "地理图层"], extent_layers=["地理图层"],
                                 overview_text="综述内容由调用者提供。", overview_title="来源与方法")
        main = layout.itemById("main_map")
        assert [layer.name() for layer in main.layers()] == ["主图层", "地理图层"]
        assert main.sizeWithUnits().width() == pytest.approx(252.45)
        assert main.sizeWithUnits().height() == pytest.approx(168.4896775757576)
        source = project.mapLayersByName("地理图层")[0]
        transformed = QgsCoordinateTransform(source.crs(), main.crs(), project).transformBoundingBox(source.extent())
        assert main.extent().contains(transformed)
        grids = main.grids().asList()
        assert len(grids) == 1 and isinstance(grids[0], QgsLayoutItemMapGrid)
        assert main.extent().width()/grids[0].intervalX() <= 4
        assert main.extent().height()/grids[0].intervalY() <= 4
        assert "font-size:2.5mm" in layout.itemById("overview").text()
        assert layout.itemById("overview").textFormat().size() == 2
        assert layout.itemById("overview").textFormat().sizeUnit() == Qgis.RenderUnit.Millimeters
        for role, item_id in result["item_ids"].items():
            item = grids[0] if role == "grid" else layout.itemById(item_id)
            assert item.customProperty("autocarto/compose_role") == role
            assert item.customProperty("autocarto/compose_spec") == "四角对齐版式"
            assert item.customProperty("autocarto/font_locked") is True
            assert isinstance(json.loads(item.customProperty("autocarto/anchor")), dict)
        assert layout.itemById("title").textFormat().font().bold()
        default, visible = compose()
        assert [layer.name() for layer in visible.itemById("main_map").layers()] == ["主图层"]
    elif case == "natural_loaded_order":
        from qgis.core import QgsApplication
        from qgis.gui import QgsLayerTreeMapCanvasBridge
        import qgis.utils
        from autocarto.tools.system._shared.print_layout_advanced_impl import set_map_layers_handler
        root = project.layerTreeRoot()
        bottom = project.mapLayersByName("主图层")[0]
        hidden = project.mapLayersByName("地理图层")[0]
        top = bottom.clone()
        top.setName("最后加载的上层")
        project.addMapLayer(top)
        background = bottom.clone()
        background.setName("后台计算结果")
        project.addMapLayer(background, False)
        group = root.addGroup("隐藏分组")
        node = root.findLayer(hidden.id())
        group.addChildNode(node.clone())
        node.parent().removeChildNode(node)
        group.setItemVisibilityChecked(False)
        canvas = qgis.utils.iface.mapCanvas()
        bridge = QgsLayerTreeMapCanvasBridge(root, canvas)
        bridge.setCanvasLayers()
        before = [(node.layerId(), node.isVisible()) for node in root.findLayers()]
        expected = [top.id(), bottom.id()]
        assert [layer.id() for layer in canvas.layers()] == expected
        result, layout = compose()
        QgsApplication.processEvents()
        assert result["map_layer_ids"] == expected
        assert [layer.id() for layer in layout.itemById("main_map").layers()] == expected
        assert [layer.id() for layer in canvas.layers()] == expected
        assert [(node.layerId(), node.isVisible()) for node in root.findLayers()] == before
        assert not root.hasCustomLayerOrder()
        assert root.findLayer(background.id()) is None

        # An independent map frame can use different layers without publishing them.
        result, layout = compose(map_layers=[bottom.id(), background.id()], extent_layers=[bottom.id()])
        assert result["map_layer_ids"] == [bottom.id(), background.id()]
        changed = set_map_layers_handler({"layout": common["layout"], "item_id": "main_map",
                                          "layers": [hidden.id()]})
        assert changed["success"], changed
        QgsApplication.processEvents()
        assert changed["data"]["layer_ids"] == [hidden.id()]
        assert [layer.id() for layer in canvas.layers()] == expected
        assert [(node.layerId(), node.isVisible()) for node in root.findLayers()] == before
        assert not root.hasCustomLayerOrder()
        assert root.findLayer(background.id()) is None
        assert len(project.mapLayers()) == 4
        del bridge
    elif case == "extra_legend_and_rollback":
        image_path = output/"symbol.png"
        image = QImage(10, 10, QImage.Format_ARGB32)
        image.fill(QColor("red"))
        assert image.save(str(image_path))
        extra = {"label": "自定义示意", "image_path": str(image_path), "size_mm": 5}
        result, layout = compose(map_layers=["主图层"], legend_extra_items=[extra], elements={"grid": False})
        legend = layout.itemById("legend")
        assert [node.layer().name() for node in legend.model().rootGroup().findLayers()] == ["主图层", "自定义示意"]
        assert [layer.name() for layer in layout.itemById("main_map").layers()] == ["主图层"]
        count = len(project.mapLayers())
        failed = compose_layout_handler({**common, "legend_extra_items": [extra, {"label": "不存在", "image_path": str(output/"missing.png")}]})
        assert not failed["success"]
        assert len(project.mapLayers()) == count
        assert project.layoutManager().layoutByName(common["layout"]).itemById("legend") is legend
        result, replaced = compose(map_layers=["主图层"], elements={"grid": False})
        assert len(project.mapLayers()) == 2  # 替换完成后只移除该版面的旧图例占位层。
    elif case == "legend_labels_and_anchor":
        from qgis.PyQt.QtCore import Qt
        from qgis.core import QgsLayoutPoint
        from autocarto.tools.system._shared.print_layout_advanced_impl import set_legend_style_handler, _item_rect
        primary = project.mapLayersByName("主图层")[0]
        secondary = project.mapLayersByName("地理图层")[0]
        branch = secondary.clone()
        branch.setName("internal_branch")
        project.addMapLayer(branch)
        selected = [primary, secondary, branch]
        original_names = [layer.name() for layer in selected]
        _, layout = compose(map_layers=[layer.id() for layer in selected],
                            legend_labels={primary.id(): "运输路线", secondary.name(): "巡检路线", branch.id(): "巡检支线"})
        legend = layout.itemById("legend")
        main = layout.itemById("main_map")
        main_rect, initial = _item_rect(main), _item_rect(legend)
        bottom = main_rect["y"] + main_rect["height"]
        assert initial["x"] == pytest.approx(main_rect["x"])
        assert initial["y"] + initial["height"] == pytest.approx(bottom)
        assert [node.name() for node in legend.model().rootGroup().findLayers()] == ["运输路线", "巡检路线", "巡检支线"]

        result = set_legend_style_handler({"layout": common["layout"], "item_id": "legend",
                    "legend_layers": [primary.id(), secondary.id()],
                    "legend_labels": {primary.id(): "运输路线", secondary.id(): "巡检路线"}})
        assert result["success"], result
        smaller = _item_rect(legend)
        assert smaller["height"] < initial["height"]
        assert smaller["x"] == pytest.approx(main_rect["x"])
        assert smaller["y"] + smaller["height"] == pytest.approx(bottom)
        nodes = legend.model().rootGroup().findLayers()
        assert [node.name() for node in nodes] == ["运输路线", "巡检路线"]
        for node in nodes:
            assert [entry.data(Qt.DisplayRole) for entry in legend.model().layerLegendNodes(node)] == [node.name()]
        assert [layer.name() for layer in selected] == original_names
        assert [layer.id() for layer in main.layers()] == [layer.id() for layer in selected]

        result = set_legend_style_handler({"layout": common["layout"], "item_id": "legend", "unlock_font": True, "body_font_size": 3})
        assert result["success"], result
        resized = _item_rect(legend)
        assert resized["y"] + resized["height"] == pytest.approx(bottom)
        assert [node.name() for node in legend.model().rootGroup().findLayers()] == ["运输路线", "巡检路线"]
        legend.attemptMove(QgsLayoutPoint(resized["x"]+7, resized["y"]-11))
        result = set_legend_style_handler({"layout": common["layout"], "item_id": "legend", "legend_layers": [primary.id()]})
        assert result["success"], result
        moved = _item_rect(legend)
        assert moved["x"] == pytest.approx(main_rect["x"]+7)
        assert moved["y"] + moved["height"] == pytest.approx(bottom-11)
        result = set_legend_style_handler({"layout": common["layout"], "item_id": "legend", "legend_labels": {}})
        assert result["success"], result
        assert legend.model().rootGroup().findLayers()[0].name() == primary.name()
    elif case == "warnings":
        import yaml
        from autocarto.tools.system._shared.layout_compose_impl import _read_spec, _rect, _move
        from autocarto.tools.system._shared.print_layout_impl import get_layout_info_handler
        # Main map/background, decorative border, and foreground placement are
        # intentional in the normal template and must not trigger intersections.
        result, _ = compose()
        normal = get_layout_info_handler({"name": common["layout"]})
        assert normal["success"] and normal["warnings"] == [], normal
        result, layout = compose(spec=load_handle("statistical"), overview_text="来源")
        original = {role: _rect(layout.itemById(role)) for role in ("title", "north_arrow")}
        assert get_layout_info_handler({"name": common["layout"]})["warnings"] == []
        spec = _read_spec(load_handle("statistical"))
        spec["elements"]["title"]["anchor"]["offset_mm"] = {"x": -20, "y": -5}
        spec["elements"]["north_arrow"]["anchor"]["offset_mm"] = {"x": 223, "y": 55}
        path = output/"重叠.layout.yaml"
        path.write_text(yaml.safe_dump(spec, allow_unicode=True))
        result, layout = compose(spec=resolve_handle(path, "layout"), overview_text="来源")
        assert any("title 超出页面" in warning for warning in result["warnings"])
        assert any("legend 与 north_arrow 相交" in warning for warning in result["warnings"])
        queried = get_layout_info_handler({"name": common["layout"]})
        assert queried["success"] and set(queried["warnings"]) == set(result["warnings"]), queried
        for role, rect in original.items():
            _move(layout.itemById(role), rect["x"], rect["y"])
        repaired = get_layout_info_handler({"name": common["layout"]})
        assert repaired["success"] and repaired["warnings"] == [], repaired
    return {"success": True, "case": case}


@pytest.mark.parametrize("case", ["replace_and_optional", "grid_extent_and_metadata", "natural_loaded_order", "extra_legend_and_rollback", "legend_labels_and_anchor", "warnings", "point_layer_projected_extent"])
def test_layout_compose_qgis(case, tmp_path):
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
