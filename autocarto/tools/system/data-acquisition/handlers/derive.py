"""Deterministic spatial derivatives with explicit approximations."""
import importlib
import tempfile
from pathlib import Path
import shutil


def derive_handler(params):
    try:
        from qgis.core import QgsFields, QgsField, QgsGeometry, QgsWkbTypes
        from qgis.PyQt.QtCore import QVariant
        normalize = importlib.import_module("autocarto.tools.system.data-acquisition.handlers.normalize")
        path, layer = normalize._source(params)
        output = normalize._output(params)
        method = params.get("method")
        if method == "hillshade":
            from osgeo import gdal
            temporary = Path(tempfile.mkdtemp(prefix=".hillshade-", dir=output.parent))
            try:
                stage = temporary / "hillshade.tif"
                source = gdal.Open(str(path), gdal.GA_ReadOnly)
                if source is None or not source.GetProjection():
                    raise ValueError("DEM 缺少有效坐标系")
                result = gdal.DEMProcessing(str(stage), source, "hillshade", format="GTiff")
                if result is None:
                    raise ValueError("DEM 晕渲生成失败")
                result = None
                return normalize._normalize_raster(stage, output, {})
            finally:
                shutil.rmtree(temporary, ignore_errors=True)
        if not layer.isValid() or layer.geometryType() != QgsWkbTypes.PolygonGeometry:
            raise ValueError("此派生方法需要有效行政区面")
        if not layer.crs().isValid():
            raise ValueError("行政区面缺少坐标系")
        features = list(layer.getFeatures())
        if method == "point_on_surface":
            fields = QgsFields(layer.fields())
            rows = [(f.geometry().pointOnSurface(), {field.name(): normalize._tags(f, layer.fields()).get(field.name()) for field in fields}) for f in features]
            data = normalize._write_vector(layer, output, fields, rows, QgsWkbTypes.Point)
            data["warnings"] = ["近似：县域代表点不代表驻地实际位置"]
            data["approximate"] = True
        elif method == "polygon_to_boundary_lines":
            fields = QgsFields()
            fields.append(QgsField("kind", QVariant.String))
            boundaries = [QgsGeometry(f.geometry().constGet().boundary()) for f in features]
            shared = []
            for index, a in enumerate(boundaries):
                for b in boundaries[index + 1:]:
                    if a.boundingBox().intersects(b.boundingBox()):
                        intersection = a.intersection(b)
                        if not intersection.isEmpty() and intersection.type() == QgsWkbTypes.LineGeometry:
                            shared.append(intersection)
            shared_union = QgsGeometry.unaryUnion(shared) if shared else QgsGeometry()
            rows = [(g, {"kind": params.get("boundary_kind") or "省界"}) for g in shared]
            if params.get("include_outer", False):
                outer = QgsGeometry.unaryUnion(boundaries)
                if not shared_union.isEmpty():
                    outer = outer.difference(shared_union)
                rows.append((outer, {"kind": "外围界线"}))
            data = normalize._write_vector(layer, output, fields, rows, QgsWkbTypes.MultiLineString)
            data["warnings"] = ["外围界线未区分国界与海岸线"] if params.get("include_outer", False) else []
        else:
            raise ValueError("不支持的数据派生方法")
        return {"success": True, "output": str(output), "data": data, "info": "数据派生完成"}
    except Exception as exc:
        return {"success": False, "error_type": "data_derive_failed", "message": str(exc), "info": str(exc)}
