"""真实 QGIS 加载、清单和边界工具回归；只使用独立工程与临时文件。"""

import csv
import json
import os
from pathlib import Path
from unittest import mock

import pytest

from autocarto.tests.golden.qgis_bootstrap import isolated_qgis, qgis_available
from autocarto.tests.support.resource_handles import resolve_handle


@pytest.fixture(scope="module")
def data_load_qgis_runtime(tmp_path_factory):
    if not qgis_available():
        pytest.skip("需要可导入 QGIS 的 Python 环境")
    from qgis.core import QgsApplication
    if QgsApplication.instance() is not None:
        pytest.skip("数据加载回归必须在独立进程运行")
    root = tmp_path_factory.mktemp("data-load-runtime")
    with mock.patch.dict(os.environ, {}, clear=False):
        with isolated_qgis(root):
            yield


@pytest.fixture
def data_load_project(data_load_qgis_runtime, tmp_path, monkeypatch):
    from qgis.core import QgsCoordinateReferenceSystem, QgsProject
    from autocarto.tools.system._shared import data_loading_impl, output_manager
    monkeypatch.setenv("AUTOCARTO_PROJECT_ROOT", str(tmp_path))
    monkeypatch.setenv("AUTOCARTO_HOME", str(tmp_path / "runtime"))
    monkeypatch.setenv("AUTOCARTO_OUTPUT_ROOT", str(tmp_path / "exports"))
    project = QgsProject.instance()
    project.clear()
    project.setCrs(QgsCoordinateReferenceSystem("EPSG:3857"))
    source_dir = tmp_path / "sources"
    source_dir.mkdir()
    path = source_dir / "points.geojson"
    path.write_text(json.dumps({"type": "FeatureCollection", "features": [
        {"type": "Feature", "properties": {"name": "示例点"}, "geometry": {"type": "Point", "coordinates": [110, 30]}}
    ]}), encoding="utf-8")
    yield project, tmp_path, path, data_loading_impl
    project.clear()
    output_manager.clear_session_context()


def _write_list(path, rows):
    with path.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.writer(stream)
        writer.writerow(["名称", "路径", "类型", "必需"])
        writer.writerows(rows)


def _raster(path):
    from osgeo import gdal, osr
    dataset = gdal.GetDriverByName("GTiff").Create(str(path), 4, 3, 1, gdal.GDT_Byte)
    dataset.SetGeoTransform((250000, 10, 0, 3400000, 0, -10))
    crs = osr.SpatialReference()
    crs.ImportFromEPSG(32650)
    dataset.SetProjection(crs.ExportToWkt())
    dataset.GetRasterBand(1).Fill(1)
    dataset = None
    return path


def test_data_load_vector_reuses_real_source_and_applies_requested_name(data_load_project, monkeypatch):
    project, root, path, loader = data_load_project
    other = root / "unrelated-cwd"
    other.mkdir()
    monkeypatch.chdir(other)
    first = loader.load_vector_handler({"path": "sources/points.geojson", "layer_name": "输入点"})
    assert first["success"], first
    assert first["data"]["reused"] is False
    assert first["data"]["geometry_type"] == "Point"
    assert first["data"]["crs"] == "EPSG:4326"
    second = loader.load_vector_handler({"path": str(path) + "|encoding=UTF-8", "layer_name": "请求的另一名称"})
    assert second["success"], second
    assert second["data"]["reused"] is True
    assert second["data"]["layer_id"] == first["data"]["layer_id"]
    assert second["output"] == "请求的另一名称"
    independent = loader.load_vector_handler({"path": str(path), "layer_name": "独立点", "reuse_existing": False})
    assert independent["success"], independent
    assert independent["data"]["reused"] is True
    assert independent["data"]["layer_id"] == first["data"]["layer_id"]
    assert independent["data"]["layer_name"] == "独立点"
    assert len(project.mapLayers()) == 1
    assert first["data"]["shown"] is True
    assert [node.layerId() for node in project.layerTreeRoot().findLayers()] == [first["data"]["layer_id"]]


def test_prepared_results_only_enter_tree_when_shown_in_sequence(data_load_project):
    from qgis.core import QgsApplication, QgsCoordinateTransform, QgsRectangle, QgsVectorLayer
    from qgis.gui import QgsLayerTreeMapCanvasBridge
    import qgis.utils
    project, root, path, loader = data_load_project
    canvas = qgis.utils.iface.mapCanvas()
    canvas.setLayers([])
    canvas.setDestinationCrs(project.crs())
    canvas.setExtent(QgsRectangle())
    bridge = QgsLayerTreeMapCanvasBridge(project.layerTreeRoot(), canvas)
    base = loader.load_raster_handler({"path": str(_raster(root / "sources/base.tif")), "show": False})
    boundary = loader.load_vector_handler({"path": str(path), "show": False})
    memory = QgsVectorLayer("Point?crs=EPSG:4326", "分析成果", "memory")
    project.addMapLayer(memory, False)
    assert base["success"] and boundary["success"]
    QgsApplication.processEvents()
    assert not project.layerTreeRoot().findLayers()
    assert canvas.layers() == []
    ids = [base["data"]["layer_id"], boundary["data"]["layer_id"], memory.id()]
    items = [{"name": name, "layer_id": layer_id} for name, layer_id in zip(("背景", "边界", "站点"), ids)]
    project.layerTreeRoot().setHasCustomLayerOrder(True)
    for index, item in enumerate(items):
        result = loader.load_list_handler({"items": [item]})
        assert result["success"], result
        QgsApplication.processEvents()
        assert [layer.id() for layer in canvas.layers()] == list(reversed(ids[:index + 1]))
        if index == 0:
            layer = project.mapLayer(ids[0])
            extent = QgsCoordinateTransform(layer.crs(), canvas.mapSettings().destinationCrs(), project).transformBoundingBox(layer.extent())
            assert not canvas.extent().isEmpty()
            assert canvas.extent().contains(extent.center())
    assert [node.layerId() for node in project.layerTreeRoot().findLayers()] == list(reversed(ids))
    assert not project.layerTreeRoot().hasCustomLayerOrder()
    assert len(project.mapLayers()) == 3
    # Retrying a displayed layer neither reorders it nor hides it for analysis.
    assert loader.load_raster_handler({"layer_id": ids[0], "show": True})["success"]
    assert loader.load_vector_handler({"layer_id": ids[1], "show": False})["success"]
    assert [node.layerId() for node in project.layerTreeRoot().findLayers()] == list(reversed(ids))


def test_reloading_same_source_reads_external_file_changes(data_load_project):
    project, _, path, loader = data_load_project
    first = loader.load_vector_handler({"path": str(path), "show": True})
    layer = project.mapLayer(first["data"]["layer_id"])
    assert layer.featureCount() == 1
    content = json.loads(path.read_text(encoding="utf-8"))
    content["features"].append({"type": "Feature", "properties": {"name": "新增点", "count": 2},
                                "geometry": {"type": "Point", "coordinates": [115, 35]}})
    path.write_text(json.dumps(content), encoding="utf-8")
    refreshed = loader.load_vector_handler({"path": str(path)})
    assert refreshed["success"], refreshed
    assert refreshed["data"]["layer_id"] == layer.id()
    assert layer.featureCount() == 2
    assert "count" in layer.fields().names()
    assert layer.extent().xMaximum() == 115
    assert len(project.mapLayers()) == 1
    assert project.layerTreeRoot().findLayer(layer.id()) is not None


def test_owned_selection_export_reuses_layer_and_loading_preserves_ownership(data_load_project):
    from autocarto.tools.system._shared.vector_selection_impl import extract_by_attribute_handler
    project, root, path, loader = data_load_project
    before = path.read_bytes()
    original = loader.load_vector_handler({"path": str(path), "layer_name": "原始材料"})
    original_id = original["data"]["layer_id"]
    context = {"session_id": "test", "session_dir": str(root), "_autocarto_output_owner": {"session_id": "test", "step_id": "prepare"}}
    reused = loader.load_vector_handler({"path": str(path), **context})
    assert reused["data"]["layer_id"] == original_id
    assert not project.mapLayer(original_id).customProperty("autocarto/output_owner", "")
    selected = extract_by_attribute_handler({"INPUT": original_id, "FIELD": "name", "OPERATOR": 0, "VALUE": "示例点", "OUTPUT": "memory:选取结果", **context})
    assert selected["success"], selected
    selected_id = selected["output"]["layer_id"]
    destination = root / "exports" / "选取结果.geojson"
    saved = loader.export_vector_handler({"layer": selected_id, "output": str(destination), **context})
    assert saved["success"], saved
    assert saved["data"]["layer_id"] == selected_id
    assert project.mapLayer(selected_id).providerType() == "ogr"
    loaded = loader.load_vector_handler({"path": str(destination), **context})
    assert loaded["success"] and loaded["data"]["layer_id"] == selected_id
    assert len(project.mapLayers()) == 2
    assert path.read_bytes() == before
    assert json.loads(project.mapLayer(selected_id).customProperty("autocarto/output_owner"))[0] == "test"
    project.removeMapLayer(selected_id)
    fresh = loader.load_vector_handler({"path": str(destination), **context})
    assert fresh["success"] and not fresh["data"]["reused"]
    assert json.loads(project.mapLayer(fresh["data"]["layer_id"]).customProperty("autocarto/output_owner"))[0] == "test"


def test_data_load_geopackage_sub_layers_have_distinct_identity(data_load_project):
    from qgis.core import QgsVectorFileWriter, QgsVectorLayer
    project, root, path, loader = data_load_project
    layer = QgsVectorLayer(str(path), "source", "ogr")
    package = root / "multi.gpkg"
    for index, name in enumerate(("first", "second")):
        options = QgsVectorFileWriter.SaveVectorOptions()
        options.driverName = "GPKG"
        options.layerName = name
        if index:
            options.actionOnExistingFile = QgsVectorFileWriter.CreateOrOverwriteLayer
        result = QgsVectorFileWriter.writeAsVectorFormatV3(layer, str(package), project.transformContext(), options)
        assert result[0] == QgsVectorFileWriter.NoError, result
    first = loader.load_vector_handler({"path": str(package) + "|layername=first"})
    second = loader.load_vector_handler({"path": str(package) + "|layername=second"})
    repeat = loader.load_vector_handler({"path": str(package) + "|layername=first|encoding=UTF-8"})
    assert first["success"] and second["success"] and repeat["success"], (first, second, repeat)
    assert first["data"]["layer_id"] != second["data"]["layer_id"]
    assert repeat["data"]["layer_id"] == first["data"]["layer_id"]
    assert repeat["data"]["reused"] is True
    project.clear()
    default_first = loader.load_vector_handler({"path": str(package)})
    default_repeat = loader.load_vector_handler({"path": str(package)})
    assert default_first["success"] and default_repeat["success"], (default_first, default_repeat)
    assert default_repeat["data"]["reused"] is True
    assert default_repeat["data"]["layer_id"] == default_first["data"]["layer_id"]


def test_data_load_raster_returns_real_metadata_and_reuses_layer(data_load_project):
    project, root, _, loader = data_load_project
    source = _raster(root / "image.tif")
    first = loader.load_raster_handler({"path": str(source), "layer_name": "影像"})
    second = loader.load_raster_handler({"path": str(source)})
    assert first["success"] and second["success"], (first, second)
    assert first["data"]["crs"] == "EPSG:32650"
    assert first["data"]["geometry_type"] == "Raster"
    assert second["data"]["reused"] is True
    assert second["data"]["layer_id"] == first["data"]["layer_id"]
    assert len(project.mapLayers()) == 1
    assert first["data"]["shown"] is True


def test_data_load_list_missing_required_prevents_all_loading_and_group_creation(data_load_project):
    project, root, path, loader = data_load_project
    original = loader.load_vector_handler({"path": str(path)})
    before = set(project.mapLayers())
    manifest = root / "list.csv"
    _write_list(manifest, [["已有数据", "sources/points.geojson", "矢量", "是"], ["缺失数据", "missing.geojson", "", "是"]])
    vanished = root / "missing.geojson"
    vanished.write_bytes(path.read_bytes())
    handle = resolve_handle(manifest, "data_list")
    vanished.unlink()  # 解析后文件消失：工具仍在任何修改前拒绝。
    result = loader.load_list_handler({"items": handle["entries"], "group": "不应创建"})
    assert result["success"] is False
    assert result["error_type"] == "data_list_required_missing"
    assert result["data"]["loaded_count"] == 0
    assert result["data"]["reused_count"] == 0
    assert set(project.mapLayers()) == before
    assert project.layerTreeRoot().findGroup("不应创建") is None


def test_data_load_list_mixed_crs_optional_missing_and_repeated_group_are_truthful(data_load_project):
    project, root, _, loader = data_load_project
    raster = _raster(root / "sources/image.tif")
    manifest = root / "sources/list.csv"
    _write_list(manifest, [["点", "points.geojson", "矢量", "是"], ["影像", raster.name, "栅格", "是"], ["可选", "absent.geojson", "", "否"]])
    first = loader.load_list_handler({"items": resolve_handle(manifest, "data_list")["entries"], "group": "输入数据", "show": True})
    second = loader.load_list_handler({"items": resolve_handle(manifest, "data_list")["entries"], "group": "输入数据", "show": True})
    assert first["success"] and second["success"], (first, second)
    assert [entry["status"] for entry in first["data"]["entries"]] == ["loaded", "loaded", "missing"]
    assert [entry["status"] for entry in second["data"]["entries"]] == ["reused", "reused", "missing"]
    assert set(first["data"]["crs_summary"]) == {"EPSG:4326", "EPSG:32650"}
    assert project.crs().authid() == "EPSG:3857"
    assert len(project.mapLayers()) == 2
    assert len(project.layerTreeRoot().findGroup("输入数据").children()) == 2


def test_data_load_list_selection_non_strict_missing_and_missing_columns(data_load_project):
    project, root, _, loader = data_load_project
    manifest = root / "list.csv"
    _write_list(manifest, [["点", "points.geojson", "vector", "true"], ["缺失", "absent.geojson", "vector", "true"]])
    vanished = root / "sources/absent.geojson"
    vanished.write_text('{"type":"FeatureCollection","features":[]}', encoding="utf-8")
    handle = resolve_handle(manifest, "data_list", base_dir=root / "sources")
    vanished.unlink()
    filtered = loader.load_list_handler({"items": handle["entries"], "base_dir": str(root / "sources"), "names": ["点"]})
    assert filtered["success"], filtered
    assert len(project.mapLayers()) == 1
    partial = loader.load_list_handler({"items": handle["entries"], "strict": False})
    assert partial["success"] is False
    assert [entry["status"] for entry in partial["data"]["entries"]] == ["reused", "missing"]
    assert "resource" not in partial
    invalid_base = loader.load_list_handler({"items": [{"name":"invalid","path":42}]})
    assert invalid_base["error_type"] == "invalid_arguments"
    manifest.write_text("名称,说明\n点,缺少路径\n", encoding="utf-8")
    with pytest.raises(ValueError, match="FLOW_RESOURCE_INVALID"):
        resolve_handle(manifest, "data_list")
    assert len(project.mapLayers()) == 1


def test_data_load_invalid_arguments_do_not_add_layers(data_load_project):
    project, _, path, loader = data_load_project
    for handler, params in ((loader.load_vector_handler, {}), (loader.load_raster_handler, {}),
                            (loader.load_list_handler, {})):
        result = handler(params)
        assert result["success"] is False, result
    assert not project.mapLayers()


def test_admin_boundary_real_polygon_output_and_repeat_are_unchanged(data_load_project):
    from autocarto.tools.system._shared import admin_boundary_impl as boundary
    project, root, _, _ = data_load_project
    params = {"level": "省", "code": "420000", "name": "湖北省", "output": "outputs/region.geojson"}
    first = boundary.extract_boundary_handler(params)
    assert first["success"], first
    assert first["data"]["layer_name"] == "湖北省边界"
    output = root / "outputs/region.geojson"
    before = output.read_bytes()
    stamp = output.stat().st_mtime_ns
    second = boundary.extract_boundary_handler(params)
    assert second["success"], second
    assert second["data"]["reused"] is True
    assert second["data"]["reused_output"] is True
    assert second["data"]["layer_id"] == first["data"]["layer_id"]
    assert output.read_bytes() == before
    assert output.stat().st_mtime_ns == stamp
    selected, _, _ = boundary._select_boundary(params)
    assert json.loads(before)["features"] == [selected]
    assert first["data"]["geometry"]["valid"] is True
    assert first["data"]["geometry"]["vertices"] > 100
    assert len(project.mapLayers()) == 1
    assert first["data"]["shown"] is True
    assert [node.layerId() for node in project.layerTreeRoot().findLayers()] == [first["data"]["layer_id"]]


def test_admin_boundary_city_and_counties_have_distinct_names_and_explicit_loading(data_load_project):
    from autocarto.tools.system._shared import admin_boundary_impl as boundary
    project, _, _, loader = data_load_project
    city = boundary.extract_boundary_handler({"level": "市", "name": "武汉市", "show": None})
    counties = boundary.extract_boundary_handler({"level": "县", "within": "武汉市", "load": False})
    assert city["success"] and counties["success"], (city, counties)
    assert city["data"]["layer_name"] == "武汉市边界"
    assert counties["data"]["layer_name"] == "武汉市区县边界"
    assert counties["data"]["feature_count"] == 13
    assert "layer_id" not in counties["data"]
    assert len(project.mapLayers()) == 1
    assert city["data"]["shown"] is True
    assert [node.layerId() for node in project.layerTreeRoot().findLayers()] == [city["data"]["layer_id"]]

    prepared = boundary.extract_boundary_handler({"level": "县", "within": "武汉市", "show": False})
    assert prepared["success"] and prepared["data"]["shown"] is False, prepared
    assert project.layerTreeRoot().findLayer(prepared["data"]["layer_id"]) is None

    loaded = loader.load_vector_handler({"path": counties["data"]["source"], "layer_name": counties["data"]["layer_name"]})
    assert loaded["success"], loaded
    assert loaded["data"]["layer_id"] == prepared["data"]["layer_id"]
    repeated = boundary.extract_boundary_handler({"level": "县", "within": "武汉市"})
    assert repeated["success"] and repeated["data"]["reused"], repeated
    assert repeated["data"]["layer_id"] == loaded["data"]["layer_id"]
    assert {layer.name() for layer in project.mapLayers().values()} == {"武汉市边界", "武汉市区县边界"}
    assert project.mapLayer(city["data"]["layer_id"]).featureCount() == 1
    assert project.mapLayer(loaded["data"]["layer_id"]).featureCount() == 13
    assert [node.layerId() for node in project.layerTreeRoot().findLayers()] == [loaded["data"]["layer_id"], city["data"]["layer_id"]]


def test_admin_boundary_default_output_stays_in_runtime_and_existing_data_is_preserved(data_load_project):
    from autocarto.tools.system._shared import admin_boundary_impl as boundary
    project, root, _, _ = data_load_project
    existing = root / "user.geojson"
    existing.write_bytes(b"existing user content")
    rejected = boundary.extract_boundary_handler({"level": "province", "code": "420000", "output": "user.geojson"})
    assert rejected["success"] is False
    assert rejected["error_type"] == "output_exists"
    assert existing.read_bytes() == b"existing user content"
    assert not project.mapLayers()
    normal = boundary.extract_boundary_handler({"level": "province", "code": "420000"})
    assert normal["success"], normal
    assert Path(normal["output"]).is_relative_to(root / "runtime/data/boundaries")


def test_data_load_new_handlers_execute_through_authority_manifest(data_load_project):
    from autocarto.core.tool_engine.engine import QGISToolEngine, execute_tool
    from autocarto.core.paths import get_plugin_root
    project, root, _, _ = data_load_project
    manifest = root / "list.csv"
    _write_list(manifest, [["点", "sources/points.geojson", "vector", "true"]])
    tools = get_plugin_root() / "tools/system"
    engine = QGISToolEngine(load_interactive_tools=False)
    engine.update_contract(execution_manifest=[
        {"name": "data_load_list", "runtime": "qgis", "domain": "data-loading",
         "handler_file": str(tools / "data-loading/handlers/loader.py"), "handler_function": "load_list_handler"},
        {"name": "admin_boundary_extract", "runtime": "qgis", "domain": "admin-boundaries",
         "handler_file": str(tools / "admin-boundaries/handlers/boundary.py"), "handler_function": "extract_boundary_handler"},
    ])
    loaded = execute_tool("data_load_list", {"items": resolve_handle(manifest, "data_list")["entries"]}, engine=engine)
    assert loaded["success"], loaded
    assert loaded["output"]["data"]["entries"][0]["status"] == "loaded"
    extracted = execute_tool("admin_boundary_extract", {"level": "省", "code": "420000"}, engine=engine)
    assert extracted["success"], extracted
    assert extracted["output"]["data"]["selected"]["adcode"] == "420000"
    assert len(project.mapLayers()) == 2



def test_data_load_list_rejects_raw_resource_paths_and_wrong_resource_types(data_load_project):
    from autocarto.tests.support.resource_handles import load_handle
    project, _, _, loader = data_load_project
    for value, code in [("数据清单", "invalid_arguments"),
                        (load_handle("style"), "invalid_arguments")]:
        result = loader.load_list_handler({"data_list": value})
        assert result["success"] is False and result["error_type"] == code, result
    assert not project.mapLayers()
