import logging
import os
from typing import Any, Dict

import processing
from qgis.core import (
    QgsProcessingContext,
    QgsProcessingFeedback,
    QgsProject,
    QgsRasterLayer,
    QgsVectorLayer,
    QgsWkbTypes,
)

from autocarto.tools.system._shared.canvas_refresh import refresh_canvas
from autocarto.tools.system._shared.input_path_resolver import resolve_input_path
from autocarto.tools.system._shared.output_manager import (
    resolve_output_path,
    sync_session_context_from_params,
)

logger = logging.getLogger(__name__)


def _crs_label(crs: Any) -> str:
    """Describe valid custom CRSs instead of reporting an empty auth id."""
    if crs is None or not crs.isValid():
        return "未定义或无效"
    return crs.authid() or crs.description() or "有效自定义 CRS"


def _format_input_resolution_error(raw_path: str, resolution: Dict[str, Any]) -> str:
    status = resolution.get("status")
    reason = resolution.get("reason")
    candidates = resolution.get("candidates") or []

    if status == "ambiguous":
        preview = ", ".join(candidates[:3])
        if len(candidates) > 3:
            preview = f"{preview}, ..."
        return f"错误：输入路径存在歧义 '{raw_path}'，候选: {preview}"

    if status == "invalid_path":
        return f"错误：输入路径无效 '{raw_path}' ({reason})"

    return f"错误：文件不存在 '{raw_path}'"


def load_vector_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        file_path = params.get("path")
        layer_name = params.get("layer_name")

        if not file_path:
            return {"success": False, "output": "", "info": "错误：缺少文件路径参数"}

        resolution = resolve_input_path(file_path, expected_kind="vector")
        if resolution.get("status") != "resolved":
            logger.warning(
                "矢量输入路径解析失败: raw=%s status=%s reason=%s",
                file_path,
                resolution.get("status"),
                resolution.get("reason"),
            )
            return {
                "success": False,
                "output": "",
                "info": _format_input_resolution_error(file_path, resolution),
            }
        resolved_path = resolution.get("resolved_path") or file_path
        logger.debug(
            "矢量输入路径解析: raw=%s resolved=%s source=%s",
            file_path,
            resolved_path,
            resolution.get("source"),
        )

        if not layer_name:
            layer_name = os.path.splitext(os.path.basename(resolved_path))[0]

        vector_layer = QgsVectorLayer(resolved_path, layer_name, "ogr")

        if not vector_layer.isValid():
            return {
                "success": False,
                "output": "",
                "info": "错误：无法加载图层，文件格式可能不支持或文件损坏",
            }

        QgsProject.instance().addMapLayer(vector_layer)
        refresh_canvas()

        feature_count = vector_layer.featureCount()
        geometry_type = QgsWkbTypes.geometryDisplayString(vector_layer.geometryType())
        crs = _crs_label(vector_layer.crs())
        field_count = len(vector_layer.fields())

        info_msg = (
            f"矢量图层加载成功。"
            f"要素数: {feature_count}, "
            f"几何类型: {geometry_type}, "
            f"坐标系: {crs}, "
            f"字段数: {field_count}, "
            f"resolved_from: {resolution.get('source', 'unknown')}"
        )

        return {"success": True, "output": layer_name, "info": info_msg}

    except Exception as e:
        return {"success": False, "output": "", "info": f"加载矢量图层失败: {str(e)}"}


def export_raster_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        sync_session_context_from_params(params)
        layer_name = params.get("layer")
        output_path = params.get("output")

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层名称参数"}

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }

        default_filename = f"{layer_name}_export.tif"
        output_path = resolve_output_path(output_path, default_filename, "exports")

        feedback = QgsProcessingFeedback()
        context = QgsProcessingContext()

        processing.run(
            "gdal:translate",
            {"INPUT": layers[0], "OUTPUT": output_path},
            context=context,
            feedback=feedback,
        )

        return {
            "success": True,
            "output": output_path,
            "info": f"栅格图层导出成功: {output_path}",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"导出栅格图层失败: {str(e)}"}


def export_vector_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        from qgis.core import QgsVectorFileWriter

        sync_session_context_from_params(params)
        layer_name = params.get("layer")
        output_path = params.get("output")
        output_format = str(params.get("format") or "").strip()

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层名称参数"}

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }

        ext_map = {
            "ESRI Shapefile": ".shp",
            "GeoJSON": ".geojson",
            "GPKG": ".gpkg",
            "GeoPackage": ".gpkg",
            "KML": ".kml",
        }
        if not output_format and output_path:
            lower_output = str(output_path).lower()
            if lower_output.endswith(".geojson") or lower_output.endswith(".json"):
                output_format = "GeoJSON"
            elif lower_output.endswith(".gpkg"):
                output_format = "GPKG"
            elif lower_output.endswith(".kml"):
                output_format = "KML"
            elif lower_output.endswith(".shp"):
                output_format = "ESRI Shapefile"
        if not output_format:
            output_format = "GeoJSON"
        ext = ext_map.get(output_format, ".shp")
        default_filename = f"{layer_name}_export{ext}"
        output_path = resolve_output_path(output_path, default_filename, "exports")

        write_result = QgsVectorFileWriter.writeAsVectorFormat(
            layers[0],
            output_path,
            "UTF-8",
            layers[0].crs(),
            output_format,
        )
        if isinstance(write_result, tuple):
            error_code = write_result[0]
            error_message = write_result[1] if len(write_result) > 1 else ""
        else:
            error_code = write_result
            error_message = ""
        if error_code != QgsVectorFileWriter.NoError:
            return {
                "success": False,
                "output": "",
                "info": f"导出矢量图层失败: {error_message or error_code}",
            }

        return {
            "success": True,
            "output": output_path,
            "info": f"矢量图层导出成功: {output_path}",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"导出矢量图层失败: {str(e)}"}


def load_raster_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        file_path = params.get("path")
        layer_name = params.get("layer_name")

        if not file_path:
            return {"success": False, "output": "", "info": "错误：缺少文件路径参数"}

        resolution = resolve_input_path(file_path, expected_kind="raster")
        if resolution.get("status") != "resolved":
            logger.warning(
                "栅格输入路径解析失败: raw=%s status=%s reason=%s",
                file_path,
                resolution.get("status"),
                resolution.get("reason"),
            )
            return {
                "success": False,
                "output": "",
                "info": _format_input_resolution_error(file_path, resolution),
            }
        resolved_path = resolution.get("resolved_path") or file_path
        logger.debug(
            "栅格输入路径解析: raw=%s resolved=%s source=%s",
            file_path,
            resolved_path,
            resolution.get("source"),
        )

        if not layer_name:
            layer_name = os.path.splitext(os.path.basename(resolved_path))[0]

        raster_layer = QgsRasterLayer(resolved_path, layer_name)

        if not raster_layer.isValid():
            return {"success": False, "output": "", "info": "错误：无法加载栅格图层"}

        QgsProject.instance().addMapLayer(raster_layer)
        refresh_canvas()

        info_msg = (
            f"栅格图层加载成功。"
            f"宽度: {raster_layer.width()}, "
            f"高度: {raster_layer.height()}, "
            f"波段数: {raster_layer.bandCount()}, "
            f"坐标系: {_crs_label(raster_layer.crs())}, "
            f"resolved_from: {resolution.get('source', 'unknown')}"
        )

        return {"success": True, "output": layer_name, "info": info_msg}

    except Exception as e:
        return {"success": False, "output": "", "info": f"加载栅格图层失败: {str(e)}"}
