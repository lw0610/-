"""真实 QGIS：可见图层、合法唯一文件名、标注字段以及 QML 往返。"""
import csv
from pathlib import Path
import re

import pytest

from autocarto.tests.golden.qgis_bootstrap import isolated_qgis, qgis_available


@pytest.fixture(scope="module")
def qgis_runtime(tmp_path_factory):
    if not qgis_available():
        pytest.skip("需要 QGIS Python")
    with isolated_qgis(tmp_path_factory.mktemp("style-export-runtime")):
        yield


@pytest.fixture
def project(qgis_runtime):
    from qgis.core import QgsProject
    project = QgsProject.instance()
    project.clear()
    yield project
    project.clear()


def _layer(project, name, label=None, expression=False):
    from qgis.core import QgsVectorLayer, QgsVectorLayerSimpleLabeling, QgsPalLayerSettings
    layer = QgsVectorLayer("Point?crs=EPSG:4326&field=name:string", name, "memory")
    project.addMapLayer(layer)
    if label:
        settings = QgsPalLayerSettings()
        settings.fieldName = label
        settings.isExpression = expression
        layer.setLabeling(QgsVectorLayerSimpleLabeling(settings))
        layer.setLabelsEnabled(True)
    return layer


def _export(params):
    # Frozen exporter is a v5 oracle for migration fixtures, no production tool.
    from autocarto.tests.support.legacy_style_export import export_style_set_handler
    return export_style_set_handler(params)


def test_default_exports_visible_vectors_and_writes_portable_mapping(project, tmp_path):
    from qgis.core import QgsRasterLayer
    field = _layer(project, "地区/边界", "name")
    expression = _layer(project, "地区:边界", "upper(name)", expression=True)
    hidden = _layer(project, "隐藏图层")
    project.layerTreeRoot().findLayer(hidden.id()).setItemVisibilityChecked(False)
    # A visible raster must not become an entry in a vector style set.
    raster = QgsRasterLayer("", "影像", "gdal")
    project.addMapLayer(raster)
    project.setDirty(False)
    directory = tmp_path / "样式集"
    result = _export({"directory": str(directory)})
    assert result["success"], result
    entries = result["data"]["entries"]
    assert {entry["layer_id"] for entry in entries} == {field.id(), expression.id()}
    names = [entry["style_file"] for entry in entries]
    assert len({name.casefold() for name in names}) == len(names)
    assert all(not re.search(r'[<>:"/\\|?*]', name) for name in names)
    assert len(list(directory.glob("*.qml"))) == 2
    with (directory / "对照表.csv").open(encoding="utf-8-sig", newline="") as stream:
        rows = {row["图层"]: row for row in csv.DictReader(stream)}
    assert rows[field.name()]["标注字段"] == "name"
    assert rows[expression.name()]["标注字段"] == ""
    clone = _layer(project, "还原样式")
    message, ok = clone.loadNamedStyle(str(directory / rows[expression.name()]["样式文件"]))
    assert ok, message
    assert clone.labeling().settings().isExpression
    assert clone.labeling().settings().fieldName == "upper(name)"
    assert field.name() == "地区/边界"


def test_explicit_ids_include_hidden_layers_and_reserved_filename(project, tmp_path):
    layer = _layer(project, "CON")
    project.layerTreeRoot().findLayer(layer.id()).setItemVisibilityChecked(False)
    result = _export({"layers": [layer.id(), layer.id()], "directory": str(tmp_path / "styles")})
    assert result["success"], result
    assert len(result["data"]["entries"]) == 1
    assert result["data"]["entries"][0]["style_file"] == "_CON.qml"


def test_duplicate_names_require_disambiguation_and_single_style_supports_ids(project, tmp_path):
    from autocarto.tools.system._shared.style_basic_impl import save_style_handler
    first = _layer(project, "同名")
    _layer(project, "同名")
    assert not _export({"layers": ["同名"], "directory": str(tmp_path / "no")})["success"]
    assert not _export({"directory": str(tmp_path / "no")})["success"]
    assert not (tmp_path / "no").exists()
    assert not save_style_handler({"layer": "同名", "path": str(tmp_path / "bad.qml")})["success"]
    result = save_style_handler({"layer": first.id(), "path": str(tmp_path / "one.qml")})
    assert result["success"], result
    assert (tmp_path / "one.qml").exists()


def test_output_directory_is_not_overwritten_and_invalid_selections_write_nothing(project, tmp_path):
    _layer(project, "图层")
    directory = tmp_path / "protected"
    directory.mkdir()
    (directory / "existing.qml").write_text("keep")
    assert not _export({"directory": str(directory)})["success"]
    assert (directory / "existing.qml").read_text() == "keep"
    for layers in ([], ["missing"], "图层"):
        assert not _export({"layers": layers, "directory": str(tmp_path / "absent")})["success"]
        assert not (tmp_path / "absent").exists()


def test_disabled_simple_and_rule_labels_leave_mapping_field_empty(project, tmp_path):
    from qgis.core import QgsRuleBasedLabeling, QgsPalLayerSettings
    disabled = _layer(project, "禁用", "name")
    disabled.setLabelsEnabled(False)
    rules = _layer(project, "规则")
    settings = QgsPalLayerSettings()
    settings.fieldName = "name"
    rules.setLabeling(QgsRuleBasedLabeling(QgsRuleBasedLabeling.Rule(settings)))
    rules.setLabelsEnabled(True)
    result = _export({"directory": str(tmp_path / "styles")})
    assert result["success"], result
    assert all(entry["label_field"] == "" for entry in result["data"]["entries"])


def test_layer_listing_uses_tree_visibility_including_hidden_parent_groups(project):
    from qgis.core import QgsVectorLayer
    from autocarto.tools.system._shared.project_state_impl import get_layers_info, get_project_info
    shown = _layer(project, "显示")
    hidden = _layer(project, "父组隐藏")
    background = QgsVectorLayer("Point?crs=EPSG:4326", "分析中间结果", "memory")
    project.addMapLayer(background, False)
    root = project.layerTreeRoot()
    node = root.findLayer(hidden.id())
    group = root.addGroup("隐藏组")
    group.addChildNode(node.clone())
    root.removeChildNode(node)
    group.setItemVisibilityChecked(False)
    result = get_layers_info({})
    assert result["success"], result
    assert [row["id"] for row in result["info"]["layers"]] == [shown.id(), hidden.id()]
    assert get_project_info({})["info"]["layer_count"]["total"] == 2
    rows = {row["id"]: row for row in result["info"]["layers"]}
    assert rows[shown.id()]["visible"] is True
    assert rows[hidden.id()]["visible"] is False
    filtered = get_layers_info({"include_invisible": False})
    assert filtered["success"], filtered
    assert [row["id"] for row in filtered["info"]["layers"]] == [shown.id()]
