"""Synthetic file previews never modify the live QGIS project."""
import importlib
import json
from pathlib import Path

import pytest

from autocarto.tests.golden.qgis_bootstrap import isolated_qgis, qgis_available


@pytest.fixture(scope="module", autouse=True)
def runtime(tmp_path_factory):
    if not qgis_available():
        pytest.skip("需要 QGIS Python")
    with isolated_qgis(tmp_path_factory.mktemp("inspect-runtime")):
        yield


def module():
    return importlib.import_module("autocarto.tools.system.data-loading.handlers.geodata")


def vector_file(tmp_path, extension="geojson", names=("points",)):
    from osgeo import ogr, osr
    driver = ogr.GetDriverByName({"geojson": "GeoJSON", "gpkg": "GPKG", "shp": "ESRI Shapefile"}[extension])
    path = tmp_path / ("example." + extension)
    dataset = driver.CreateDataSource(str(path))
    crs = osr.SpatialReference()
    crs.ImportFromEPSG(4326)
    for name in names:
        layer = dataset.CreateLayer(name, crs, ogr.wkbPoint, options=["ENCODING=UTF-8"] if extension == "shp" else [])
        layer.CreateField(ogr.FieldDefn("NAME", ogr.OFTString))
        feature = ogr.Feature(layer.GetLayerDefn())
        feature.SetField("NAME", "测试点")
        feature.SetGeometry(ogr.CreateGeometryFromWkt("POINT (114 30)"))
        layer.CreateFeature(feature)
    dataset = None
    return path


@pytest.mark.parametrize("extension", ["shp", "geojson", "gpkg"])
def test_vector_metadata_and_thumbnail_are_read_only(tmp_path, extension):
    from qgis.core import QgsProject
    project = QgsProject.instance()
    project.clear()
    project.setDirty(False)
    path = vector_file(tmp_path, extension, ("first", "second") if extension == "gpkg" else ("points",))
    original = path.read_bytes()
    result = module().inspect_handler({"path": str(path)})
    assert result["success"], result
    assert len(result["data"]["layers"]) == (2 if extension == "gpkg" else 1)
    row = result["data"]["layers"][0]
    assert row["feature_count"] == 1
    assert row["crs"] == "EPSG:4326"
    assert row["samples"][0]["NAME"] == "测试点"
    output = tmp_path / "preview.png"
    preview = module().thumbnail_handler({"path": str(path), "layer_name": row["name"], "output_path": str(output)})
    assert preview["success"], preview
    assert output.read_bytes().startswith(b"\x89PNG")
    assert path.read_bytes() == original
    assert not project.mapLayers() and not project.isDirty()
    assert not module().thumbnail_handler({"path": str(path), "output_path": str(output)})["success"]


def test_raster_csv_and_invalid_files(tmp_path):
    from osgeo import gdal, osr
    from qgis.core import QgsProject
    raster_path = tmp_path / "raster.tif"
    ds = gdal.GetDriverByName("GTiff").Create(str(raster_path), 20, 20, 1, gdal.GDT_Byte)
    crs = osr.SpatialReference()
    crs.ImportFromEPSG(4326)
    ds.SetProjection(crs.ExportToWkt())
    ds.SetGeoTransform([114, .01, 0, 31, 0, -.01])
    ds.GetRasterBand(1).Fill(100)
    ds.GetRasterBand(1).SetNoDataValue(0)
    ds = None
    result = module().inspect_handler({"path": str(raster_path)})
    assert result["success"], result
    assert result["data"]["band_count"] == 1
    assert result["data"]["resolution"] == [.01, .01]
    preview = module().thumbnail_handler({"path": str(raster_path), "output_path": str(tmp_path / "raster.png")})
    assert preview["success"], preview
    csv = tmp_path / "points.csv"
    csv.write_text("name,x,y\n甲,114,30\n乙,115,31\n", encoding="utf-8")
    params = {"path": str(csv), "x_field": "x", "y_field": "y", "crs": "EPSG:4326", "sample_limit": 1}
    result = module().inspect_handler(params)
    assert result["success"], result
    assert result["data"]["layers"][0]["feature_count"] == 2
    assert len(result["data"]["layers"][0]["samples"]) == 1
    assert json.dumps(result, ensure_ascii=False)
    broken = tmp_path / "broken.geojson"
    broken.write_text("bad")
    assert not module().inspect_handler({"path": str(broken)})["success"]
    assert not module().inspect_handler({"path": str(csv), "sample_limit": 201})["success"]
    assert not QgsProject.instance().mapLayers()
