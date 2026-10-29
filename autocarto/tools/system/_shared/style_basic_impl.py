"""基础样式操作，保留旧工具的参数和返回结构。"""
from typing import (
    Any,
    Dict,
)
from qgis.PyQt.QtGui import QColor
from qgis.core import (
    QgsCategorizedSymbolRenderer,
    QgsMarkerSymbol,
    QgsProject,
    QgsProperty,
    QgsRendererCategory,
    QgsSingleSymbolRenderer,
    QgsStyle,
    QgsSymbol,
)
from autocarto.tools.system._shared.resource_handle import (
    ResourceHandleError, require_handle, handle_entry_path, handle_summary,
)
from autocarto.tools.system._shared.style_common import (
    _blend_mode,
    _get_layer,
    _opacity,
    _style_info,
    _wrap_main_thread,
)


def set_layer_style_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer_name = params.get("layer")
        style_type = params.get("style_type", "simple")
        color = params.get("color", "#FF0000")
        size = params.get("size", 2)
        field = params.get("field")

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层参数"}

        layer, error = _get_layer(layer_name)
        if error:
            return error

        if style_type == "simple":
            symbol = QgsSymbol.defaultSymbol(layer.geometryType())
            symbol.setColor(QColor(color))
            symbol.setOpacity(0.8)

            if symbol.symbolLayerCount() > 0:
                sl = symbol.symbolLayer(0)
                if hasattr(sl, "setSize"):
                    sl.setSize(size)
                elif hasattr(sl, "setWidth"):
                    sl.setWidth(size)

            renderer = QgsSingleSymbolRenderer(symbol)
            layer.setRenderer(renderer)

        elif style_type == "categorized":
            if not field:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：分类符号需要指定字段参数",
                }

            field_index = layer.fields().indexFromName(field)
            if field_index < 0:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：字段 '{field}' 不存在",
                }

            unique_values = layer.uniqueValues(field_index)
            categories = []
            colors = [
                "#FF0000",
                "#00FF00",
                "#0000FF",
                "#FFFF00",
                "#FF00FF",
                "#00FFFF",
                "#FFA500",
                "#800080",
            ]

            for idx, value in enumerate(unique_values):
                symbol = QgsSymbol.defaultSymbol(layer.geometryType())
                color_idx = idx % len(colors)
                symbol.setColor(QColor(colors[color_idx]))
                symbol.setOpacity(0.8)

                category = QgsRendererCategory(value, symbol, str(value))
                categories.append(category)

            renderer = QgsCategorizedSymbolRenderer(field, categories)
            layer.setRenderer(renderer)

        layer.triggerRepaint()

        return {
            "success": True,
            "output": layer_name,
            "info": f"图层样式设置完成。样式类型: {style_type}",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"设置图层样式失败: {str(e)}"}



def set_layer_opacity_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer, error = _get_layer(params.get("layer"))
        if error:
            return error
        value = _opacity(params.get("opacity", 1.0))
        layer.setOpacity(value)
        layer.triggerRepaint()
        return {
            "success": True,
            "output": layer.name(),
            "info": f"图层透明度已更新为 {value:.2f}",
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"设置图层透明度失败: {str(e)}"}



def set_layer_blend_mode_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer, error = _get_layer(params.get("layer"))
        if error:
            return error
        mode = _blend_mode(params.get("blend_mode"))
        if mode is None:
            return {"success": False, "output": "", "info": "错误：无效的 blend_mode"}
        layer.setBlendMode(mode)
        layer.triggerRepaint()
        return {"success": True, "output": layer.name(), "info": "图层混合模式已更新"}
    except Exception as e:
        return {
            "success": False,
            "output": "",
            "info": f"设置图层混合模式失败: {str(e)}",
        }



def set_color_ramp_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer, error = _get_layer(params.get("layer"))
        if error:
            return error
        ramp_name = str(params.get("color_ramp") or "").strip()
        style = QgsStyle.defaultStyle()
        ramp = style.colorRamp(ramp_name) if ramp_name else None
        renderer = layer.renderer()
        if (
            ramp is None
            or renderer is None
            or not hasattr(renderer, "setSourceColorRamp")
        ):
            return {
                "success": False,
                "output": "",
                "info": "错误：当前渲染器不支持色带设置",
            }
        renderer.setSourceColorRamp(ramp)
        if hasattr(renderer, "updateColorRamp"):
            renderer.updateColorRamp(ramp)
        layer.triggerRepaint()
        return {"success": True, "output": layer.name(), "info": "色带已更新"}
    except Exception as e:
        return {"success": False, "output": "", "info": f"设置色带失败: {str(e)}"}



def set_symbol_size_scale_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer, error = _get_layer(params.get("layer"))
        if error:
            return error
        renderer = layer.renderer()
        symbol = (
            renderer.symbol()
            if renderer is not None and hasattr(renderer, "symbol")
            else None
        )
        if symbol is None:
            return {
                "success": False,
                "output": "",
                "info": "错误：当前图层没有可用符号",
            }
        field = params.get("field")
        expression = params.get("expression")
        if not field and not expression:
            return {
                "success": False,
                "output": "",
                "info": "错误：缺少 field 或 expression 参数",
            }
        prop = (
            QgsProperty.fromField(field)
            if field
            else QgsProperty.fromExpression(str(expression))
        )
        if isinstance(symbol, QgsMarkerSymbol):
            symbol.setDataDefinedSize(prop)
        else:
            symbol_layer = (
                symbol.symbolLayer(0) if symbol.symbolLayerCount() > 0 else None
            )
            if symbol_layer is None or not hasattr(
                symbol_layer, "setDataDefinedProperty"
            ):
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：当前符号不支持大小比例属性",
                }
            property_key = getattr(symbol_layer, "PropertySize", None) or getattr(
                symbol_layer, "PropertyWidth", None
            )
            if property_key is None:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：当前符号图层不支持大小比例属性",
                }
            symbol_layer.setDataDefinedProperty(property_key, prop)
        layer.triggerRepaint()
        return {
            "success": True,
            "output": layer.name(),
            "info": "符号大小数据驱动比例已更新",
        }
    except Exception as e:
        return {
            "success": False,
            "output": "",
            "info": f"设置符号大小比例失败: {str(e)}",
        }



def get_style_info_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer, error = _get_layer(params.get("layer"))
        if error:
            return error
        return {
            "success": True,
            "output": layer.name(),
            "info": "图层样式信息获取成功",
            "data": _style_info(layer),
        }
    except Exception as e:
        return {
            "success": False,
            "output": "",
            "info": f"获取图层样式信息失败: {str(e)}",
        }



def save_style_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        from autocarto.tools.system._shared.style_common import find_style_layer
        layer = find_style_layer(QgsProject.instance(), params.get("layer"))
        path = params.get("path") or params.get("style_path")
        if not path:
            return {"success": False, "output": "", "info": "错误：缺少 path 参数"}
        message, ok = layer.saveNamedStyle(str(path))
        if not ok:
            return {
                "success": False,
                "output": "",
                "info": f"保存图层样式失败: {message}",
            }
        return {"success": True, "output": str(path), "info": "图层样式已保存", "data": {"layer_id": layer.id(), "layer_name": layer.name()}}
    except Exception as e:
        return {"success": False, "output": "", "info": f"保存图层样式失败: {str(e)}"}



def load_style_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer, error = _get_layer(params.get("layer"))
        if error:
            return error
        handle = require_handle(params.get("style"), "style", formats=("qml",))
        path = handle_entry_path(handle)
        explicit = params.get("label_field")
        if explicit is not None and (not isinstance(explicit, str) or not explicit.strip()):
            raise ValueError("label_field 必须是实际字段名，关闭标注请填“无”")
        fields = layer.fields().names() if hasattr(layer, "fields") else []
        if explicit and explicit != "无" and explicit not in fields:
            raise ValueError("标注字段不存在：" + explicit)
        geometry = handle.get("geometry")
        if hasattr(layer, "geometryType") and geometry in ("point", "line", "polygon") and layer.geometryType() != {"point": 0, "line": 1, "polygon": 2}[geometry]:
            raise ValueError("样式几何类型与图层不一致")
        import math
        scale = float(params.get("scale", 1))
        if not math.isfinite(scale) or scale <= 0:
            raise ValueError("scale 必须大于 0")
        message, ok = layer.loadNamedStyle(str(path))
        if not ok:
            return {
                "success": False,
                "output": "",
                "info": f"加载图层样式失败: {message}",
            }
        from autocarto.tools.system._shared.style_labeling import restore_qml_font_families, _override_label_field, resolve_name_field
        from autocarto.tools.system._shared.symbology import read_symbology, symbology_differences, scale_loaded_style
        from autocarto.tools.system._shared.style_labeling import select_label_font
        expected, scale_warnings = scale_loaded_style(layer, scale)
        if layer.labeling() is not None and hasattr(layer.labeling(), "settings"):
            expected["labels"]["font"] = select_label_font(layer.labeling().settings().format().font().family())[0]
        warnings = list(handle.get("warnings", [])) + scale_warnings + restore_qml_font_families(layer)
        actual = ""
        requested = explicit if explicit is not None else handle.get("label_field")
        labeling = layer.labeling() if hasattr(layer, "labeling") else None
        if explicit is None and labeling is not None and hasattr(labeling, "settings") and labeling.settings().isExpression:
            actual = labeling.settings().fieldName
            requested = None
        if requested == "无":
            if hasattr(layer, "setLabelsEnabled"): layer.setLabelsEnabled(False)
            if hasattr(layer, "setLabeling"): layer.setLabeling(None)
        elif requested and not fields:
            if hasattr(layer, "setLabelsEnabled"): layer.setLabelsEnabled(False)
            warnings.append("图层没有可用名称字段，已关闭标注")
        elif fields and (requested or labeling is not None):
            if not requested and labeling is not None and hasattr(labeling, "settings"):
                settings = labeling.settings()
                if settings.isExpression:
                    actual = settings.fieldName
                else:
                    requested = settings.fieldName
            if requested:
                actual = resolve_name_field(fields, requested)
                if actual:
                    _override_label_field(layer, actual)
                    if actual != requested: warnings.append("标注字段不存在，已使用 " + actual)
                else:
                    layer.setLabelsEnabled(False)
                    warnings.append("图层没有可用名称字段，已关闭标注")
        if requested == "无" or (not actual and requested):
            expected["labels"] = {"enabled": False}
        elif actual:
            expected["labels"].update(enabled=True, field=actual)
        readback = read_symbology(layer)
        differences = symbology_differences(expected, readback)
        layer.triggerRepaint()
        return {"success": True, "output": str(path), "info": "图层样式已加载",
                "resource": handle_summary(handle), "label_field": actual, "warnings": warnings,
                "data": {"layer_id": layer.id(), "layer_name": layer.name(), "label_field": actual,
                         "requested": expected, "actual": readback, "warnings": warnings, "differences": differences}}
    except ResourceHandleError as exc:
        return {"success": False, "output": "", "info": str(exc), "error_type": exc.code, "data": exc.details}
    except Exception as e:
        return {"success": False, "output": "", "info": f"加载图层样式失败: {str(e)}"}




def load_styles_handler(params):
    items = params.get("items")
    if not isinstance(items, list) or not items or len(items) > 1000:
        return {"success": False, "info": "items 必须是 1–1000 项图层与样式列表", "error_type": "invalid_arguments"}
    results = []
    for item in items:
        if not isinstance(item, dict):
            results.append({"success": False, "info": "样式项必须是对象"})
            continue
        result = load_style_handler(dict(item, scale=params.get("scale", 1)))
        results.append(dict(result, layer=item.get("layer")))
    return {"success": all(r.get("success") for r in results), "output": "", "info": "批量样式处理完成", "data": {"items": results}}

set_layer_style_handler = _wrap_main_thread(set_layer_style_handler)
set_layer_opacity_handler = _wrap_main_thread(set_layer_opacity_handler)
set_layer_blend_mode_handler = _wrap_main_thread(set_layer_blend_mode_handler)
set_color_ramp_handler = _wrap_main_thread(set_color_ramp_handler)
set_symbol_size_scale_handler = _wrap_main_thread(set_symbol_size_scale_handler)
get_style_info_handler = _wrap_main_thread(get_style_info_handler)
save_style_handler = _wrap_main_thread(save_style_handler)
load_style_handler = _wrap_main_thread(load_style_handler)
