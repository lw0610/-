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
    elif case == "warnings":
        import yaml
        from autocarto.tools.system._shared.layout_compose_impl import _read_spec
        spec = _read_spec(load_handle("statistical"))
        spec["elements"]["title"]["anchor"]["offset_mm"] = {"x": -20, "y": -5}
        spec["elements"]["north_arrow"]["anchor"]["offset_mm"] = {"x": 223, "y": 55}
        path = output/"重叠.layout.yaml"
        path.write_text(yaml.safe_dump(spec, allow_unicode=True))
        result, _ = compose(spec=resolve_handle(path, "layout"), overview_text="来源")
        assert any("title 超出页面" in warning for warning in result["warnings"])
        assert any("legend 与 north_arrow 相交" in warning for warning in result["warnings"])
    return {"success": True, "case": case}


@pytest.mark.parametrize("case", ["replace_and_optional", "grid_extent_and_metadata", "extra_legend_and_rollback", "warnings", "point_layer_projected_extent"])
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
