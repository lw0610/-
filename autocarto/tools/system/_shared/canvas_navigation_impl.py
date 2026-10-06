import importlib

from typing import Any, Dict, Optional

from autocarto.tools.system._shared.canvas_refresh import refresh_canvas


def _safe_refresh_canvas():
    refresh_canvas()


def _runtime_dependencies():
    core = importlib.import_module("qgis.core")
    utils = importlib.import_module("qgis.utils")
    return (
        getattr(utils, "iface", None),
        getattr(core, "QgsProject"),
        getattr(core, "QgsCoordinateTransform"),
        getattr(core, "QgsRectangle"),
    )


def get_canvas_info(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    try:
        iface, _, _, _ = _runtime_dependencies()
        if not iface:
            return {"success": False, "output": "无法访问QGIS界面", "info": {}}

        canvas = iface.mapCanvas()
        extent = canvas.extent()
        center = canvas.center()

        info = {
            "scale": canvas.scale(),
            "rotation": canvas.rotation(),
            "center": {"x": center.x(), "y": center.y()},
            "extent": {
                "xmin": extent.xMinimum(),
                "ymin": extent.yMinimum(),
                "xmax": extent.xMaximum(),
                "ymax": extent.yMaximum(),
            },
            "width": canvas.width(),
            "height": canvas.height(),
            "layers_count": canvas.layerCount(),
        }

        return {"success": True, "output": "画布信息获取成功", "info": info}

    except Exception as e:
        return {"success": False, "output": f"获取画布信息失败: {str(e)}", "info": {}}


def get_map_extent(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    try:
        iface, _, _, _ = _runtime_dependencies()
        if params is None:
            params = {}

        format_type = params.get("format", "bbox")

        if not iface:
            return {"success": False, "output": "无法访问QGIS界面", "info": {}}

        canvas = iface.mapCanvas()
        extent = canvas.extent()

        if format_type == "wkt":
            extent_info = {"wkt": extent.asWktPolygon()}
        else:
            extent_info = {
                "xmin": extent.xMinimum(),
                "ymin": extent.yMinimum(),
                "xmax": extent.xMaximum(),
                "ymax": extent.yMaximum(),
                "width": extent.width(),
                "height": extent.height(),
            }

        return {
            "success": True,
            "output": f"地图范围获取成功 (格式: {format_type})",
            "info": extent_info,
        }

    except Exception as e:
        return {"success": False, "output": f"获取地图范围失败: {str(e)}", "info": {}}


def zoom_to_layer_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        iface, QgsProject, QgsCoordinateTransform, _ = _runtime_dependencies()
        layer_name = params.get("layer")

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层名称参数"}

        if not iface:
            return {"success": False, "output": "", "info": "无法访问QGIS界面"}

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }

        layer = layers[0]
        canvas = iface.mapCanvas()

        extent = layer.extent()
        if layer.crs() != canvas.mapSettings().destinationCrs():
            transform = QgsCoordinateTransform(
                layer.crs(),
                canvas.mapSettings().destinationCrs(),
                QgsProject.instance(),
            )
            extent = transform.transformBoundingBox(extent)

        canvas.setExtent(extent)
        _safe_refresh_canvas()

        return {
            "success": True,
            "output": layer_name,
            "info": f"已缩放到图层 '{layer_name}' 的范围",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"缩放到图层失败: {str(e)}"}


def zoom_to_extent_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        iface, _, _, QgsRectangle = _runtime_dependencies()
        xmin = params.get("xmin")
        ymin = params.get("ymin")
        xmax = params.get("xmax")
        ymax = params.get("ymax")

        if None in [xmin, ymin, xmax, ymax]:
            return {
                "success": False,
                "output": "",
                "info": "错误：缺少范围参数(xmin, ymin, xmax, ymax)",
            }

        if not iface:
            return {"success": False, "output": "", "info": "无法访问QGIS界面"}

        canvas = iface.mapCanvas()
        xmin_value = float(xmin)
        ymin_value = float(ymin)
        xmax_value = float(xmax)
        ymax_value = float(ymax)
        extent = QgsRectangle(xmin_value, ymin_value, xmax_value, ymax_value)
        canvas.setExtent(extent)
        _safe_refresh_canvas()

        return {
            "success": True,
            "output": f"[{xmin}, {ymin}, {xmax}, {ymax}]",
            "info": "已缩放到指定范围",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"缩放到范围失败: {str(e)}"}


def set_scale_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        iface, _, _, _ = _runtime_dependencies()
        scale = params.get("scale")

        if scale is None:
            return {"success": False, "output": "", "info": "错误：缺少比例尺参数"}

        if not iface:
            return {"success": False, "output": "", "info": "无法访问QGIS界面"}

        scale = float(scale)
        if scale <= 0:
            return {"success": False, "output": "", "info": "错误：比例尺必须大于0"}

        canvas = iface.mapCanvas()
        canvas.zoomScale(scale)
        _safe_refresh_canvas()

        return {
            "success": True,
            "output": str(scale),
            "info": f"已设置比例尺为 1:{int(scale)}",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"设置比例尺失败: {str(e)}"}
