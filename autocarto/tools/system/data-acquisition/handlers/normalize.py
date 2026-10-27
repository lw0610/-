"""Deterministic spatial normalization from resolved specifications, never resource tables."""
import hashlib
import json
import math
import os
import tempfile
import shutil
from pathlib import Path
import re

from autocarto.core.paths import resolve_project_path


def _source(params):
    from qgis.core import QgsVectorLayer
    raw = params.get("input")
    if isinstance(raw, dict):
        name, raw = raw.get("layer"), raw.get("path")
    else:
        name = params.get("layer_name")
    path = resolve_project_path(raw or "")
    if not path.is_file():
        raise ValueError("原始数据文件不存在")
    layer = QgsVectorLayer(str(path) + ("|layername=" + name if name else ""), path.stem, "ogr")
    return path, layer


def _output(params):
    output = resolve_project_path(params.get("output") or "")
    if output.exists() or output.is_symlink():
        raise ValueError("输出已存在，不能覆盖原件")
    output.parent.mkdir(parents=True, exist_ok=True)
    return output


def _checked_geometry(geometry, label, *, polygon=False, allow_empty=False):
    from qgis.core import QgsWkbTypes
    error = geometry.lastError()
    if error:
        raise ValueError(f"{label}失败：{error}")
    if geometry.isNull():
        raise ValueError(f"{label}没有有效几何")
    if geometry.isEmpty():
        if allow_empty:
            return
        raise ValueError(f"{label}为空")
    if polygon and geometry.type() != QgsWkbTypes.PolygonGeometry:
        raise ValueError(f"{label}必须为面几何")
    if not geometry.isGeosValid():
        raise ValueError(f"{label}包含无效几何，未自动修复")
    if any(not math.isfinite(value) for vertex in geometry.vertices() for value in (vertex.x(), vertex.y())):
        raise ValueError(f"{label}包含非有限坐标")


def _clip(raw, crs):
    from qgis.core import QgsGeometry, QgsRectangle, QgsCoordinateReferenceSystem, QgsCoordinateTransform, QgsProject, QgsVectorLayer, Qgis
    if raw is None or raw == "":
        return None, {"clip_applied": False}
    if not crs.isValid():
        raise ValueError("原始数据坐标系未定义")
    metadata = {"clip_applied": False}
    if isinstance(raw, dict):
        candidates = [raw[key] for key in ("bbox", "path") if raw.get(key) is not None and raw.get(key) != ""]
        if len(candidates) != 1:
            raise ValueError("裁剪参数须且仅能包含 bbox 或 path 中的一项")
        raw = candidates[0]
    if isinstance(raw, list):
        if len(raw) != 4:
            raise ValueError("裁剪范围须为西、南、东、北四个坐标")
        values = [float(v) for v in raw]
        if not all(math.isfinite(v) for v in values) or values[0] >= values[2] or values[1] >= values[3]:
            raise ValueError("裁剪范围无效")
        if values[0] < -180 or values[2] > 180 or values[1] < -90 or values[3] > 90:
            raise ValueError("裁剪外包范围必须使用 WGS84 经纬度")
        geometry = QgsGeometry.fromRect(QgsRectangle(*values))
        source_crs = QgsCoordinateReferenceSystem("EPSG:4326")
        metadata.update(clip_kind="bbox", clip_bbox=values)
    else:
        if not isinstance(raw, (str, Path)):
            raise ValueError("裁剪边界须为文件路径")
        path = resolve_project_path(str(raw))
        if not path.is_file():
            raise ValueError("裁剪边界文件不存在")
        layer = QgsVectorLayer(str(path), "clip", "ogr")
        if not layer.isValid() or not layer.crs().isValid():
            raise ValueError("裁剪边界或其坐标系无效")
        geometries = []
        for feature in layer.getFeatures():
            item = QgsGeometry(feature.geometry())
            _checked_geometry(item, f"裁剪边界要素 {feature.id()}", polygon=True)
            geometries.append(item)
        if not geometries:
            raise ValueError("裁剪边界没有面要素")
        geometry = QgsGeometry.unaryUnion(geometries)
        source_crs = layer.crs()
        metadata.update(clip_kind="polygon", clip_path=str(path), clip_boundary_features=len(geometries))
    _checked_geometry(geometry, "裁剪边界合并", polygon=True)
    if source_crs != crs:
        transformed = geometry.transform(QgsCoordinateTransform(source_crs, crs, QgsProject.instance().transformContext()))
        if transformed != Qgis.GeometryOperationResult.Success:
            raise ValueError(f"裁剪边界坐标转换失败：{transformed}")
        _checked_geometry(geometry, "裁剪边界坐标转换", polygon=True)
    metadata.update(clip_crs=crs.authid() or crs.toWkt(), clip_source_crs=source_crs.authid() or source_crs.toWkt())
    return geometry, metadata


def _intersection(geometry, boundary, family, label):
    from qgis.core import QgsWkbTypes
    clipped = geometry.intersection(boundary)
    _checked_geometry(clipped, label, allow_empty=True)
    if clipped.isEmpty():
        return clipped
    if QgsWkbTypes.flatType(clipped.wkbType()) == QgsWkbTypes.GeometryCollection:
        # GEOS can return a line plus an isolated touching point. QGIS's
        # existing extractor retains only members of the original family.
        if not clipped.convertGeometryCollectionToSubclass(family):
            raise ValueError(f"{label}无法保留原始几何类型")
        _checked_geometry(clipped, label, allow_empty=True)
        if clipped.isEmpty():
            return None
    # A line touching a corner produces a point; two polygons sharing only an
    # edge produce a line. These contain no geometry of the source dimension.
    return clipped if clipped.type() == family else None


def _tags(feature, fields):
    values = {field.name(): feature[field.name()] for field in fields}
    raw = values.get("other_tags")
    if isinstance(raw, str):
        for key, value in re.findall(r'"((?:[^"\\]|\\.)*)"\s*=>\s*"((?:[^"\\]|\\.)*)"', raw):
            values.setdefault(json.loads('"' + key + '"'), json.loads('"' + value + '"'))
    return values


def _present(value):
    return value is not None and type(value).__name__ != "QPyNullVariant" and str(value).strip() != ""


def _shapefile_fields(fields):
    """Retain source values with explicit DBF-safe names, never drop columns."""
    from qgis.core import QgsFields, QgsField
    result, source_names, used = QgsFields(), {}, set()
    def short(value, size):
        return value.encode("utf-8")[:size].decode("utf-8", errors="ignore")
    for field in fields:
        original = field.name()
        base = re.sub(r"[^\w]", "_", original) or "field"
        name = short(base, 10)
        index = 2
        while name.casefold() in used:
            suffix = "_" + str(index)
            name = short(base, 10-len(suffix)) + suffix
            index += 1
        used.add(name.casefold())
        copy = QgsField(field)
        copy.setName(name)
        result.append(copy)
        source_names[name] = original
    return result, source_names


def _publish(stage, output):
    """Exclusive hard links keep original files intact even if another writer races."""
    files = sorted(stage.parent.glob(stage.stem + ".*")) if output.suffix.lower() == ".shp" else [stage]
    published = []
    try:
        for path in files:
            destination = output.with_suffix(path.suffix) if output.suffix.lower() == ".shp" else output
            os.link(path, destination)
            published.append(destination)
        return [str(path) for path in published]
    except Exception:
        for path in published:
            path.unlink(missing_ok=True)
        raise


def _write_vector(layer, output, fields, features, geometry_type):
    temporary = Path(tempfile.mkdtemp(prefix=".normalize-", dir=output.parent))
    try:
        stage = temporary / output.name
        data = _write_vector_inner(layer, stage, fields, features, geometry_type)
        paths = _publish(stage, output)
        data.update(path=str(output), paths=paths, sha256={path: hashlib.sha256(Path(path).read_bytes()).hexdigest() for path in paths})
        return data
    finally:
        shutil.rmtree(temporary, ignore_errors=True)


def _write_vector_inner(layer, output, fields, features, geometry_type):
    from qgis.core import QgsVectorFileWriter, QgsProject, QgsFeature, QgsGeometry, QgsWkbTypes
    drivers = {".shp": "ESRI Shapefile", ".gpkg": "GPKG", ".geojson": "GeoJSON"}
    if output.suffix.lower() not in drivers:
        raise ValueError("规范化矢量仅支持 Shapefile、GeoPackage、GeoJSON")
    if output.suffix.lower() == ".shp" and any(len(f.name().encode("utf-8")) > 10 for f in fields):
        raise ValueError("Shapefile 字段名超过 10 字节，请使用 GeoPackage")
    options = QgsVectorFileWriter.SaveVectorOptions()
    options.driverName = drivers[output.suffix.lower()]
    options.fileEncoding = "UTF-8"
    options.layerName = output.stem
    if output.suffix.lower() == ".shp":
        options.layerOptions = ["ENCODING=UTF-8"]
    writer = QgsVectorFileWriter.create(str(output), fields, geometry_type, layer.crs(), QgsProject.instance().transformContext(), options)
    if writer.hasError() != QgsVectorFileWriter.NoError:
        raise ValueError(writer.errorMessage())
    count, filled, extent = 0, {f.name(): 0 for f in fields}, None
    try:
        for geometry, values in features:
            if geometry.isEmpty():
                continue
            if QgsWkbTypes.isMultiType(geometry_type):
                geometry.convertToMultiType()
            item = QgsFeature(fields)
            item.setGeometry(geometry)
            item.setAttributes([values.get(f.name()) for f in fields])
            if not writer.addFeature(item):
                raise ValueError(writer.lastError())
            count += 1
            for key in filled:
                filled[key] += int(_present(values.get(key)))
            if extent is None:
                extent = geometry.boundingBox()
            else:
                extent.combineExtentWith(geometry.boundingBox())
    finally:
        del writer
    paths = [str(p) for p in sorted(output.parent.glob(output.stem + ".*"))] if output.suffix.lower() == ".shp" else [str(output)]
    return {"path": str(output), "paths": paths, "count": count, "geometry_type": QgsWkbTypes.displayString(geometry_type),
            "crs": layer.crs().authid() or layer.crs().toWkt(), "extent": [] if extent is None else [extent.xMinimum(), extent.yMinimum(), extent.xMaximum(), extent.yMaximum()],
            "extent_wgs84": _wgs84_extent(extent,layer.crs()),
            "fields": {key: (value / count if count else 0) for key, value in filled.items()},
            "sha256": {p: hashlib.sha256(Path(p).read_bytes()).hexdigest() for p in paths}, "warnings": []}


def normalize_handler(params):
    try:
        from qgis.core import QgsExpression, QgsExpressionContext, QgsFields, QgsField, QgsFeature, QgsWkbTypes, QgsGeometry, QgsUnitTypes
        from qgis.PyQt.QtCore import QVariant
        path, layer = _source(params)
        output = _output(params)
        if not layer.isValid():
            return _normalize_raster(path, output, params)
        if not layer.crs().isValid():
            raise ValueError("原始数据坐标系未定义")
        expected = str(params.get("expected_geometry") or "").lower()
        actual = {QgsWkbTypes.PointGeometry: "point", QgsWkbTypes.LineGeometry: "line", QgsWkbTypes.PolygonGeometry: "polygon"}.get(layer.geometryType(), "unknown")
        if expected and expected not in (actual, "vector"):
            raise ValueError("原始数据几何类型不符合要求：" + actual)
        clip, clip_info = _clip(params.get("clip"), layer.crs())
        clip_counts = {"clip_input_features": 0, "clip_empty_features": 0, "clip_nonmatching_geometry_features": 0}
        mapping = params.get("fields") or {}
        if not isinstance(mapping, dict) or any(not isinstance(v, list) or not all(isinstance(k, str) for k in v) for v in mapping.values()):
            raise ValueError("fields 必须为输出字段到候选字段列表的映射")
        fields = QgsFields(layer.fields()) if not mapping else QgsFields()
        for name in mapping:
            fields.append(QgsField(name, QVariant.String))
        for name in params.get("standard_fields", ["kind", "ref"]):
            if layer.fields().indexFromName(name) >= 0 and fields.indexFromName(name) < 0:
                fields.append(layer.fields().field(name))
            elif fields.indexFromName(name) < 0 and any(_present(_tags(feature, layer.fields()).get(name)) for feature in layer.getFeatures()):
                fields.append(QgsField(name, QVariant.String))
        source_names = {}
        if output.suffix.lower() == ".shp" and not mapping:
            fields, source_names = _shapefile_fields(fields)
        expression = QgsExpression(params.get("filter") or "TRUE")
        if expression.hasParserError():
            raise ValueError("筛选表达式无效：" + expression.parserErrorString())
        eval_fields = QgsFields(layer.fields())
        for name in set(expression.referencedColumns()).union(*(set(v) for v in mapping.values())):
            if name != "*" and eval_fields.indexFromName(name) < 0:
                eval_fields.append(QgsField(name, QVariant.String))
        context = QgsExpressionContext()
        context.setFields(eval_fields)
        tolerance = float(params.get("simplify_tolerance_m") or 0)
        if not math.isfinite(tolerance) or tolerance < 0:
            raise ValueError("简化容差须为非负有限数")
        if layer.crs().isGeographic():
            tolerance /= 111320 * max(.01, math.cos(math.radians(layer.extent().center().y())))
        else:
            tolerance /= QgsUnitTypes.fromUnitToUnitFactor(layer.crs().mapUnits(), QgsUnitTypes.DistanceMeters)

        def rows():
            for feature in layer.getFeatures():
                values = _tags(feature, layer.fields())
                probe = QgsFeature(eval_fields)
                probe.setAttributes([values.get(f.name()) for f in eval_fields])
                probe.setGeometry(feature.geometry())
                context.setFeature(probe)
                keep = expression.evaluate(context)
                if expression.hasEvalError():
                    raise ValueError("筛选表达式执行失败：" + expression.evalErrorString())
                if not _present(keep) or not bool(keep):
                    continue
                geometry = QgsGeometry(feature.geometry())
                if clip is not None:
                    clip_counts["clip_input_features"] += 1
                    _checked_geometry(geometry, f"原始数据要素 {feature.id()}")
                    # GEOS failures can produce a null/empty geometry. Inspect
                    # the operation error before treating an empty intersection
                    # as an ordinary feature outside the requested boundary.
                    geometry = _intersection(geometry, clip, layer.geometryType(), f"要素 {feature.id()} 精确裁剪")
                    if geometry is None:
                        clip_counts["clip_nonmatching_geometry_features"] += 1
                        continue
                    if geometry.isEmpty():
                        clip_counts["clip_empty_features"] += 1
                        continue
                if geometry.isNull() or geometry.isEmpty():
                    continue
                if not geometry.isGeosValid():
                    raise ValueError("原始数据包含无效几何，未自动修复")
                if tolerance:
                    geometry = geometry.simplify(tolerance)
                    _checked_geometry(geometry, f"要素 {feature.id()} 简化")
                    if clip is not None:
                        # Simplification can bridge a concavity or a hole. Keep
                        # the delivered geometry inside the original boundary.
                        geometry = _intersection(geometry, clip, layer.geometryType(), f"要素 {feature.id()} 简化后精确裁剪")
                        if geometry is None:
                            clip_counts["clip_nonmatching_geometry_features"] += 1
                            continue
                        if geometry.isEmpty():
                            clip_counts["clip_empty_features"] += 1
                            continue
                mapped = {name: next((values.get(key) for key in candidates if _present(values.get(key))), None) for name, candidates in mapping.items()}
                for field in fields:
                    if field.name() not in mapped:
                        mapped[field.name()] = values.get(source_names.get(field.name(), field.name()))
                yield geometry, mapped

        data = _write_vector(layer, output, fields, rows(), QgsWkbTypes.multiType(layer.wkbType()))
        clip_info["clip_applied"] = clip is not None
        if clip is not None:
            clip_info.update(clip_counts, clip_output_features=data["count"])
        data.update(clip_info)
        renamed = {original: target for target, original in source_names.items() if target != original}
        if renamed:
            data["field_renames"] = renamed
            data["warnings"].append("Shapefile字段名已按10字节限制兼容重命名，原字段值保留；对应关系见field_renames")
        return {"success": True, "output": str(output), "info": "数据规范化完成", "data": data}
    except Exception as exc:
        return {"success": False, "error_type": "data_normalize_failed", "info": str(exc), "message": str(exc)}


def _wgs84_extent(extent, crs):
    if extent is None:
        return []
    from qgis.core import QgsCoordinateTransform, QgsCoordinateReferenceSystem, QgsProject
    transformed = QgsCoordinateTransform(crs, QgsCoordinateReferenceSystem("EPSG:4326"), QgsProject.instance().transformContext()).transformBoundingBox(extent)
    return [transformed.xMinimum(), transformed.yMinimum(), transformed.xMaximum(), transformed.yMaximum()]


def _normalize_raster(path, output, params):
    temporary = Path(tempfile.mkdtemp(prefix=".raster-", dir=output.parent))
    try:
        stage = temporary / output.name
        result = _normalize_raster_inner(path, stage, params)
        paths = _publish(stage, output)
        result["output"] = str(output)
        result["data"].update(path=str(output), paths=paths, sha256={p: hashlib.sha256(Path(p).read_bytes()).hexdigest() for p in paths})
        return result
    finally:
        shutil.rmtree(temporary, ignore_errors=True)


def _normalize_raster_inner(path, output, params):
    from osgeo import gdal
    from qgis.core import QgsRasterLayer
    if params.get("expected_geometry") not in (None, "", "raster"):
        raise ValueError("原始栅格不符合所需矢量几何类型")
    if output.suffix.lower() not in (".tif", ".tiff"):
        raise ValueError("规范化栅格输出须为 GeoTIFF")
    source = gdal.Open(str(path), gdal.GA_ReadOnly)
    if source is None or not source.GetProjection():
        raise ValueError("无法读取栅格或坐标系未定义")
    args = {"format": "GTiff", "dstSRS": source.GetProjection(), "creationOptions": ["COMPRESS=DEFLATE"]}
    clip = params.get("clip")
    if isinstance(clip, dict):
        clip = clip.get("bbox") or clip.get("path")
        if not clip:
            raise ValueError("裁剪参数须含 bbox 或 path")
    if isinstance(clip, list):
        if len(clip) != 4 or not all(math.isfinite(float(v)) for v in clip) or clip[0] >= clip[2] or clip[1] >= clip[3]:
            raise ValueError("裁剪范围无效")
        args.update(outputBounds=clip, outputBoundsSRS="EPSG:4326")
    elif clip:
        args.update(cutlineDSName=str(resolve_project_path(str(clip))), cropToCutline=True)
    result = gdal.Warp(str(output), source, **args)
    if result is None:
        raise ValueError("栅格裁剪失败")
    result.FlushCache()
    data = {"path": str(output), "paths": [str(output)], "width": result.RasterXSize, "height": result.RasterYSize,
            "band_count": result.RasterCount, "crs": result.GetProjection(), "warnings": []}
    band = result.GetRasterBand(1)
    mask = band.GetMaskBand().ReadAsArray(buf_xsize=min(512, result.RasterXSize), buf_ysize=min(512, result.RasterYSize))
    data["nodata_ratio"] = float((mask == 0).mean())
    result = None
    layer = QgsRasterLayer(str(output), output.stem)
    extent = layer.extent()
    data["extent"] = [extent.xMinimum(), extent.yMinimum(), extent.xMaximum(), extent.yMaximum()]
    data["resolution"] = [layer.rasterUnitsPerPixelX(), layer.rasterUnitsPerPixelY()]
    data["extent_wgs84"] = _wgs84_extent(extent, layer.crs())
    data["sha256"] = {str(output): hashlib.sha256(output.read_bytes()).hexdigest()}
    return {"success": True, "output": str(output), "info": "栅格规范化完成", "data": data}
