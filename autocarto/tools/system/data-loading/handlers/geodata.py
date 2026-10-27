"""Read and render temporary data layers without registering them in a project."""
import json
import math
import os
from pathlib import Path
import tempfile
from urllib.parse import urlencode

from autocarto.core.paths import resolve_project_path


def _path(params):
    raw = params.get("path")
    if not isinstance(raw, str) or not raw.strip():
        raise ValueError("请提供地理文件路径")
    path = resolve_project_path(raw)
    if not path.is_file():
        raise ValueError("地理文件不存在或不是普通文件")
    return path


def _limit(value, default, minimum, maximum):
    if value is None:
        return default
    if isinstance(value, bool) or not isinstance(value, (int, float)) or int(value) != value or not minimum <= value <= maximum:
        raise ValueError(f"参数必须是 {minimum}–{maximum} 之间的整数")
    return int(value)


def _bounds(extent):
    return [extent.xMinimum(), extent.yMinimum(), extent.xMaximum(), extent.yMaximum()]


def _json_value(value):
    if value is None or type(value).__name__ == "QPyNullVariant":
        return None
    if isinstance(value, float) and not math.isfinite(value):
        return None
    if isinstance(value, (str, bool, int, float)):
        return value
    if hasattr(value, "toPyDateTime"):
        return value.toPyDateTime().isoformat()
    if hasattr(value, "toPyDate"):
        return value.toPyDate().isoformat()
    return str(value)


def _vector(path, name, params):
    from qgis.core import QgsVectorLayer
    if path.suffix.lower() == ".csv" and (params.get("x_field") or params.get("y_field")):
        if not params.get("x_field") or not params.get("y_field") or not params.get("crs"):
            raise ValueError("CSV 坐标预览需要同时提供 x_field、y_field 和已知 crs")
        query = urlencode({"type": "csv", "xField": params["x_field"], "yField": params["y_field"], "crs": params["crs"], "spatialIndex": "no", "watchFile": "no"})
        return QgsVectorLayer(path.as_uri() + "?" + query, name or path.stem, "delimitedtext")
    uri = str(path)
    if name:
        if "|" in name or "\x00" in name:
            raise ValueError("子图层名称无效")
        uri += "|layername=" + name
    return QgsVectorLayer(uri, name or path.stem, "ogr")


def inspect_handler(params):
    try:
        from osgeo import gdal, ogr
        from qgis.core import QgsRasterLayer, QgsWkbTypes
        path = _path(params)
        limit = _limit(params.get("sample_limit"), 5, 0, 200)
        try:
            ds = ogr.Open(str(path), 0)
        except RuntimeError:
            ds = None
        requested = params.get("layer_name")
        if ds is not None:
            names = [ds.GetLayerByIndex(i).GetName() for i in range(ds.GetLayerCount())]
            if requested and requested not in names:
                raise ValueError("数据文件中没有指定的子图层")
            rows = []
            for name in ([requested] if requested else names):
                layer = _vector(path, name, params)
                if not layer.isValid():
                    raise ValueError("无法读取矢量子图层：" + name)
                fields = [{"name": f.name(), "type": f.typeName()} for f in layer.fields()]
                samples = []
                if limit:
                    for feature in layer.getFeatures():
                        samples.append({f["name"]: _json_value(feature[f["name"]]) for f in fields})
                        if len(samples) >= limit:
                            break
                rows.append({"name": name, "crs": layer.crs().authid() or layer.crs().toWkt(),
                             "extent": _bounds(layer.extent()), "feature_count": layer.featureCount(),
                             "geometry_type": QgsWkbTypes.displayString(layer.wkbType()), "fields": fields, "samples": samples})
            data = {"path": str(path), "kind": "vector", "format": ds.GetDriver().GetName(), "layers": rows}
        else:
            raster = gdal.Open(str(path), gdal.GA_ReadOnly)
            layer = QgsRasterLayer(str(path), path.stem)
            if raster is None or not layer.isValid():
                raise ValueError("文件损坏或不属于支持的矢量、栅格数据格式")
            data = {"path": str(path), "kind": "raster", "format": raster.GetDriver().ShortName,
                    "crs": layer.crs().authid() or layer.crs().toWkt(), "extent": _bounds(layer.extent()),
                    "width": raster.RasterXSize, "height": raster.RasterYSize, "band_count": raster.RasterCount,
                    "resolution": [abs(raster.GetGeoTransform()[1]), abs(raster.GetGeoTransform()[5])],
                    "bands": [{"band": i, "type": gdal.GetDataTypeName(raster.GetRasterBand(i).DataType),
                               "nodata": _json_value(raster.GetRasterBand(i).GetNoDataValue())} for i in range(1, raster.RasterCount + 1)]}
        json.dumps(data, allow_nan=False)
        return {"success": True, "data": data, "info": "地理文件信息已读取；未加载到工程"}
    except Exception as exc:
        return {"success": False, "error_type": "geodata_inspect_failed", "message": str(exc), "info": str(exc)}


def thumbnail_handler(params):
    temporary = None
    try:
        from qgis.core import QgsRasterLayer, QgsMapSettings, QgsMapRendererParallelJob, QgsContrastEnhancement, QgsSingleBandGrayRenderer
        from qgis.PyQt.QtCore import QSize
        from qgis.PyQt.QtGui import QColor
        path = _path(params)
        size = _limit(params.get("size"), 512, 64, 2048)
        output = resolve_project_path(params.get("output_path") or "")
        if output.suffix.lower() != ".png" or output.exists() or output.is_symlink():
            raise ValueError("缩略图输出须为尚不存在的 PNG 文件")
        layer = _vector(path, params.get("layer_name"), params)
        if not layer.isValid() or not layer.isSpatial():
            layer = QgsRasterLayer(str(path), path.stem)
            if not layer.isValid():
                raise ValueError("文件缺少可渲染的空间信息；CSV 坐标表请提供坐标列和坐标系")
            provider = layer.dataProvider()
            if layer.bandCount() == 1:
                low, high = provider.cumulativeCut(1, .02, .98)
                enhancement = QgsContrastEnhancement(provider.dataType(1))
                enhancement.setContrastEnhancementAlgorithm(QgsContrastEnhancement.StretchToMinimumMaximum)
                enhancement.setMinimumValue(low)
                enhancement.setMaximumValue(high if high > low else low + 1)
                renderer = QgsSingleBandGrayRenderer(provider, 1)
                renderer.setContrastEnhancement(enhancement)
                layer.setRenderer(renderer)
        extent = layer.extent()
        if extent.isEmpty():
            extent.grow(0.01 if layer.crs().isGeographic() else 10)
        extent.scale(1.08)
        settings = QgsMapSettings()
        settings.setLayers([layer])
        settings.setDestinationCrs(layer.crs())
        settings.setExtent(extent)
        settings.setOutputSize(QSize(size, size))
        settings.setBackgroundColor(QColor("white"))
        job = QgsMapRendererParallelJob(settings)
        job.start()
        job.waitForFinished()
        output.parent.mkdir(parents=True, exist_ok=True)
        fd, temporary = tempfile.mkstemp(prefix=".preview-", suffix=".png", dir=output.parent)
        os.close(fd)
        if not job.renderedImage().save(temporary, "PNG"):
            raise ValueError("PNG 缩略图写入失败")
        os.link(temporary, output)
        return {"success": True, "output": str(output), "info": "缩略图已生成；未修改工程", "data": {"path": str(output), "source_path": str(path), "width": size, "height": size, "artifact_paths": [str(output)]}}
    except Exception as exc:
        return {"success": False, "error_type": "geodata_thumbnail_failed", "message": str(exc), "info": str(exc)}
    finally:
        if temporary:
            Path(temporary).unlink(missing_ok=True)
