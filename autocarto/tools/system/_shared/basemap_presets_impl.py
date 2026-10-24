"""隐藏旧预设工具的兼容包装，仅读取冻结的工具私有资产。"""
from typing import Any, Dict, List, Optional, Tuple

from qgis.PyQt.QtCore import Qt
from qgis.PyQt.QtGui import QColor
from qgis.core import (
    QgsLineSymbol, QgsPalLayerSettings, QgsProject, QgsProperty,
    QgsSimpleLineSymbolLayer, QgsTextFormat, QgsUnitTypes, QgsVectorLayer,
    QgsVectorLayerSimpleLabeling, QgsWkbTypes,
)
from autocarto.tools.system._shared.style_common import _get_layer, _wrap_main_thread
from autocarto.tools.system._shared.style_set_impl import restore_qml_font_families
from autocarto.tools.system._shared.style_set_support import (
    legacy_preset, preset_catalog, style_file_path,
)
from autocarto.tools.system._shared.tool_assets import tool_asset_path

try:
    _RIVER_LINE_COLOR = preset_catalog().get("compatibility", {}).get("river_line_color")
except (OSError, ValueError):
    _RIVER_LINE_COLOR = None


def _preset_layer(entry):
    layer = QgsVectorLayer(entry["geometry"] + "?crs=EPSG:4326&field=name:string&field=NAME:string", "preset", "memory")
    path = style_file_path(tool_asset_path("cartography", "basemap-legacy-styles"), entry["style_file"])
    message, ok = layer.loadNamedStyle(str(path))
    if not ok:
        raise ValueError("无法读取内置 QML: " + message)
    restore_qml_font_families(layer)
    return layer


def _apply_legacy_labels(layer, prototype, entry, label_field):
    policy = entry["label_policy"]
    if policy["mode"] == "preserve" or not label_field:
        return False
    source = prototype.labeling().settings()
    if policy["mode"] == "replace":
        settings = QgsPalLayerSettings(source)
    else:
        existing = layer.labeling()
        settings = existing.settings() if existing is not None and hasattr(existing, "settings") else QgsPalLayerSettings()
        for name in policy["settings"]:
            if hasattr(settings, name) and hasattr(source, name):
                setattr(settings, name, getattr(source, name))
        target_format = QgsTextFormat(settings.format())
        source_format = QgsTextFormat(source.format())
        operations = {"font": ("font", "setFont"), "size": ("size", "setSize"),
                      "size_unit": ("sizeUnit", "setSizeUnit"), "color": ("color", "setColor"),
                      "buffer": ("buffer", "setBuffer")}
        for name in policy["text"]:
            getter, setter = operations[name]
            getattr(target_format, setter)(getattr(source_format, getter)())
        settings.setFormat(target_format)
    settings.fieldName = label_field
    if policy.get("clear_size_property"):
        properties = settings.dataDefinedProperties()
        if hasattr(properties, "removeProperty"):
            properties.removeProperty(QgsPalLayerSettings.Property.Size)
        else:
            properties.setProperty(QgsPalLayerSettings.Property.Size, QgsProperty())
        settings.setDataDefinedProperties(properties)
    layer.setLabeling(QgsVectorLayerSimpleLabeling(settings))
    layer.setLabelsEnabled(True)
    if policy.get("mark_dirty"):
        QgsProject.instance().setDirty(True)
    return True


def _set_line_layer_units(line_layer):
    if hasattr(line_layer, "setWidthUnit"):
        line_layer.setWidthUnit(QgsUnitTypes.RenderMillimeters)
    if hasattr(line_layer, "setOffsetUnit"):
        line_layer.setOffsetUnit(QgsUnitTypes.RenderMillimeters)



def _gb_line_layer(
    color: str,
    width_mm: float,
    pen_style=Qt.SolidLine,
    offset_mm: float = 0.0,
    dash_pattern: Optional[List[float]] = None,
    cap_style=Qt.FlatCap,
    join_style=Qt.MiterJoin,
):
    line_layer = QgsSimpleLineSymbolLayer()
    line_layer.setColor(QColor(color))
    line_layer.setWidth(float(width_mm))
    line_layer.setPenStyle(pen_style)
    if offset_mm and hasattr(line_layer, "setOffset"):
        line_layer.setOffset(float(offset_mm))
    if dash_pattern and hasattr(line_layer, "setUseCustomDashPattern"):
        line_layer.setUseCustomDashPattern(True)
        line_layer.setCustomDashVector([float(item) for item in dash_pattern])
    if hasattr(line_layer, "setPenCapStyle"):
        line_layer.setPenCapStyle(cap_style)
    if hasattr(line_layer, "setPenJoinStyle"):
        line_layer.setPenJoinStyle(join_style)
    _set_line_layer_units(line_layer)
    return line_layer



def _gb_line_symbol(symbol_layers: List[QgsSimpleLineSymbolLayer]):
    symbol = QgsLineSymbol()
    symbol.changeSymbolLayer(0, symbol_layers[0])
    for symbol_layer in symbol_layers[1:]:
        symbol.appendSymbolLayer(symbol_layer)
    return symbol



def _line_symbol_by_type(line_type: str) -> Tuple[Optional[QgsLineSymbol], Optional[str]]:
    entry = legacy_preset("line", line_type)
    if entry is None:
        return None, None
    prototype = _preset_layer(entry)
    return prototype.renderer().symbol().clone(), entry["legacy_type"]



def set_marker_symbol_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer, error = _get_layer(params.get("layer"))
        if error:
            return error
        if layer.geometryType() != QgsWkbTypes.PointGeometry:
            return {
                "success": False,
                "output": "",
                "info": "错误：仅点图层支持 marker symbol",
            }

        layer_type = str(params.get("layer_type") or "").strip().lower()
        if layer_type not in {"city", "county"}:
            return {
                "success": False,
                "output": "",
                "info": "错误：layer_type 必须是 city 或 county",
            }

        label_field = params.get("label_field")
        if not label_field:
            return {"success": False, "output": "", "info": "错误：缺少标注字段参数 label_field"}
        label_field = str(label_field)
        if layer.fields().indexFromName(label_field) < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：图层字段 '{label_field}' 不存在",
            }

        entry = legacy_preset("marker", layer_type)
        prototype = _preset_layer(entry)
        layer.setRenderer(prototype.renderer().clone())
        _apply_legacy_labels(layer, prototype, entry, label_field)

        layer.triggerRepaint()
        return {
            "success": True,
            "output": layer.name(),
            "info": "点图层符号已更新",
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"设置点图层符号失败: {str(e)}"}



def set_line_symbol_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer_name = params.get("layer")
        layer, error = _get_layer(layer_name)
        if error:
            return error
        if layer.geometryType() != QgsWkbTypes.LineGeometry:
            return {
                "success": False,
                "output": "",
                "info": "错误：仅线图层支持 line symbol",
            }
        line_type = str(params.get("line_type") or "").strip()
        symbol, style_name = _line_symbol_by_type(line_type)
        if symbol is None:
            return {
                "success": False,
                "output": "",
                "info": "错误：line_type 必须是“铁路”“高速公路”“国道”“省界”或“河流”",
            }

        label_field = str(params.get("label_field") or "").strip()
        entry = legacy_preset("line", line_type)
        default_field = entry["label_policy"].get("default_field", "")
        if not label_field and default_field and layer.fields().indexFromName(default_field) >= 0:
            label_field = default_field
        if label_field and layer.fields().indexFromName(label_field) < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：图层字段 '{label_field}' 不存在",
            }

        prototype = _preset_layer(entry)
        layer.setRenderer(prototype.renderer().clone())
        label_updated = _apply_legacy_labels(layer, prototype, entry, label_field)
        if label_updated:
            applied_settings = layer.labeling().settings()
            if not layer.labelsEnabled() or applied_settings.fieldName != label_field:
                return {"success": False, "output": "", "info": f"河流名称标注未成功写入图层：{layer.name()}（字段 {label_field}）"}

        layer.triggerRepaint()
        return {
            "success": True,
            "output": layer.name(),
            "info": (
                f"线图层符号和河流名称标注已更新：{style_name}（{label_field}）"
                if label_updated
                else f"线图层符号已更新：{style_name}"
            ),
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"设置线图层符号失败: {str(e)}"}



def set_fill_symbol_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer, error = _get_layer(params.get("layer"))
        if error:
            return error
        if layer.geometryType() != QgsWkbTypes.PolygonGeometry:
            return {
                "success": False,
                "output": "",
                "info": "错误：仅面图层支持 fill symbol",
            }

        layer_type_raw = str(params.get("layer_type") or "").strip()
        entry = legacy_preset("fill", layer_type_raw)
        if entry is None:
            return {
                "success": False,
                "output": "",
                "info": "错误：layer_type 必须是 县、市、省 或 河流",
            }

        label_field = str(params.get("label_field") or "").strip()
        if label_field and layer.fields().indexFromName(label_field) < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：图层字段 '{label_field}' 不存在",
            }

        prototype = _preset_layer(entry)
        layer.setRenderer(prototype.renderer().clone())
        label_updated = _apply_legacy_labels(layer, prototype, entry, label_field)
        if label_updated:
            applied_settings = layer.labeling().settings()
            if not layer.labelsEnabled() or applied_settings.fieldName != label_field:
                return {"success": False, "output": "", "info": f"面图层标注未成功写入图层：{layer.name()}（字段 {label_field}）"}
        layer.triggerRepaint()
        return {
            "success": True,
            "output": layer.name(),
            "info": (
                f"面图层填充符号和标注已更新：{layer_type_raw}"
                if label_updated
                else f"面图层填充符号已更新：{layer_type_raw}"
            ),
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"设置面图层填充失败: {str(e)}"}


set_marker_symbol_handler = _wrap_main_thread(set_marker_symbol_handler)
set_line_symbol_handler = _wrap_main_thread(set_line_symbol_handler)
set_fill_symbol_handler = _wrap_main_thread(set_fill_symbol_handler)
