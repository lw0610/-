"""样式处理的共用转换、查询与主线程执行。"""
from typing import (
    Any,
    Dict,
    Optional,
)
from qgis.PyQt.QtCore import Qt
from qgis.PyQt.QtGui import (
    QColor,
    QPainter,
)
from qgis.core import QgsProject
from autocarto.tools.system._shared.layout_runtime import run_in_main_thread


def _wrap_main_thread(handler):
    def _wrapped(params: Dict[str, Any]) -> Dict[str, Any]:
        try:
            return run_in_main_thread(lambda: handler(params))
        except Exception as e:
            import traceback

            detail = traceback.format_exc()
            return {
                "success": False,
                "output": "",
                "info": f"{handler.__name__} 执行失败: {str(e)}",
                "error_type": "main_thread_execution_failed",
                "message": str(e),
                "debug": {
                    "handler": handler.__name__,
                    "params_type": type(params).__name__,
                    "traceback": detail,
                },
                "detail": detail,
            }

    _wrapped.__name__ = handler.__name__
    _wrapped.__doc__ = handler.__doc__
    return _wrapped



def _get_layer(layer_name: Optional[str]):
    if not layer_name:
        return None, {"success": False, "output": "", "info": "错误：缺少图层参数"}
    layers = QgsProject.instance().mapLayersByName(str(layer_name))
    if not layers:
        return None, {
            "success": False,
            "output": "",
            "info": f"错误：未找到图层 '{layer_name}'",
        }
    return layers[0], None



def _color(value: Optional[str], default: str) -> QColor:
    result = QColor(str(value or default).strip() or default)
    if not result.isValid():
        result = QColor(default)
    return result



def _opacity(value: Any, default: float = 1.0) -> float:
    try:
        return max(0.0, min(1.0, float(value)))
    except (TypeError, ValueError):
        return default



def _pen_style(value: Optional[str]):
    mapping = {
        "solid": Qt.SolidLine,
        "dash": Qt.DashLine,
        "dot": Qt.DotLine,
        "dashdot": Qt.DashDotLine,
        "dashdotdot": Qt.DashDotDotLine,
        "none": Qt.NoPen,
    }
    return mapping.get(str(value or "solid").strip().lower(), Qt.SolidLine)



def _brush_style(value: Optional[str]):
    mapping = {
        "solid": Qt.SolidPattern,
        "dense1": Qt.Dense1Pattern,
        "dense2": Qt.Dense2Pattern,
        "dense3": Qt.Dense3Pattern,
        "dense4": Qt.Dense4Pattern,
        "dense5": Qt.Dense5Pattern,
        "dense6": Qt.Dense6Pattern,
        "dense7": Qt.Dense7Pattern,
        "cross": Qt.CrossPattern,
        "diagcross": Qt.DiagCrossPattern,
        "hor": Qt.HorPattern,
        "ver": Qt.VerPattern,
        "none": Qt.NoBrush,
    }
    return mapping.get(str(value or "solid").strip().lower(), Qt.SolidPattern)



def _blend_mode(value: Optional[str]):
    mapping = {
        "sourceover": QPainter.CompositionMode_SourceOver,
        "multiply": QPainter.CompositionMode_Multiply,
        "screen": QPainter.CompositionMode_Screen,
        "overlay": QPainter.CompositionMode_Overlay,
        "darken": QPainter.CompositionMode_Darken,
        "lighten": QPainter.CompositionMode_Lighten,
        "difference": QPainter.CompositionMode_Difference,
        "exclusion": QPainter.CompositionMode_Exclusion,
        "plus": QPainter.CompositionMode_Plus,
    }
    key = str(value or "").replace("_", "").replace(" ", "").lower()
    return mapping.get(key)



def _style_info(layer) -> Dict[str, Any]:
    from autocarto.tools.system._shared.style_thematic_impl import _graduated_renderer_info

    renderer = layer.renderer()
    symbol = (
        renderer.symbol()
        if renderer is not None and hasattr(renderer, "symbol")
        else None
    )
    details = {
        "layer": layer.name(),
        "renderer": type(renderer).__name__ if renderer is not None else "",
        "geometry_type": int(layer.geometryType()),
        "opacity": layer.opacity(),
        "blend_mode": int(layer.blendMode()) if hasattr(layer, "blendMode") else None,
        "symbol_color": symbol.color().name() if symbol is not None else None,
        "symbol_opacity": symbol.opacity() if symbol is not None else None,
    }
    details.update(_graduated_renderer_info(renderer))
    return details
