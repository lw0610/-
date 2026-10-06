from typing import Any, Dict, List, Optional, Tuple
import csv
import json
import hashlib
import math
import random
from pathlib import Path

from qgis.PyQt.QtCore import Qt, QVariant
from qgis.PyQt.QtGui import QColor, QFont, QPainter
from qgis.core import (
    Qgis,
    QgsCategorizedSymbolRenderer,
    QgsClassificationEqualInterval,
    QgsClassificationJenks,
    QgsClassificationQuantile,
    QgsClassificationStandardDeviation,
    QgsColorBrewerColorRamp,
    QgsCoordinateReferenceSystem,
    QgsCoordinateTransform,
    QgsFillSymbol,
    QgsFeature,
    QgsFeatureRequest,
    QgsField,
    QgsGeometry,
    QgsLayoutItem,
    QgsLayoutItemLabel,
    QgsLayoutItemMap,
    QgsLayoutItemPicture,
    QgsLayoutItemPolyline,
    QgsLayoutItemShape,
    QgsLayoutPoint,
    QgsLayoutSize,
    QgsUnitTypes,
    QgsGradientFillSymbolLayer,
    QgsGradientColorRamp,
    QgsGradientStop,
    QgsGraduatedSymbolRenderer,
    QgsLinePatternFillSymbolLayer,
    QgsLineSymbol,
    QgsMarkerSymbol,
    QgsPalLayerSettings,
    QgsProject,
    QgsProperty,
    QgsRendererCategory,
    QgsRendererRange,
    QgsSimpleFillSymbolLayer,
    QgsPointXY,
    QgsSimpleLineSymbolLayer,
    QgsSingleSymbolRenderer,
    QgsStyle,
    QgsSvgMarkerSymbolLayer,
    QgsSymbol,
    QgsTextBufferSettings,
    QgsTextFormat,
    QgsVectorLayerSimpleLabeling,
    QgsVectorLayer,
    QgsWkbTypes,
    QgsRuleBasedRenderer, 
    QgsRuleBasedLabeling,
    QgsMapLayerDependency,
    QgsSimpleMarkerSymbolLayer,
)

from autocarto.tools.system._shared.layout_runtime import run_in_main_thread
from autocarto.knowledge.dataset_handoff import (
    DatasetHandoffError,
    resolve_marching_route_dataset,
)
from autocarto.tools.system._shared.callout_text_layout import (
    wrap_chinese_text_to_mm,
)


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


def set_layer_style_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer_name = params.get("layer")
        style_type = params.get("style_type", "simple")
        color = params.get("color", "#FF0000")
        size = params.get("size", 2)
        field = params.get("field")

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层参数"}

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }

        layer = layers[0]

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


def add_labels_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer_name = params.get("layer")
        field = params.get("field")
        font_size = params.get("font_size", 10)
        color = params.get("color", "#000000")

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层参数"}
        if not field:
            return {"success": False, "output": "", "info": "错误：缺少字段参数"}

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }

        layer = layers[0]
        field_index = layer.fields().indexFromName(field)
        if field_index < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：字段 '{field}' 不存在",
            }

        text_format = QgsTextFormat()
        text_format.setSize(font_size)
        text_format.setColor(QColor(color))

        label_settings = QgsPalLayerSettings()
        label_settings.fieldName = field
        label_settings.setFormat(text_format)
        try:
            label_settings.placement = Qgis.LabelPlacement.OverPoint
        except AttributeError:
            label_settings.placement = QgsPalLayerSettings.OverPoint

        labeling = QgsVectorLayerSimpleLabeling(label_settings)
        layer.setLabeling(labeling)
        layer.setLabelsEnabled(True)
        layer.triggerRepaint()

        return {
            "success": True,
            "output": layer_name,
            "info": f"标注添加完成。字段: {field}, 字体大小: {font_size}",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"添加标注失败: {str(e)}"}


def _graduated_renderer_info(renderer) -> Dict[str, Any]:
    """Read the installed renderer, so callers can verify the thematic result."""
    if not isinstance(renderer, QgsGraduatedSymbolRenderer):
        return {}
    method = renderer.classificationMethod()
    ranges = [
        {
            "lower": float(item.lowerValue()),
            "upper": float(item.upperValue()),
            "label": item.label(),
            "color": item.symbol().color().name(QColor.HexArgb),
        }
        for item in renderer.ranges()
    ]
    return {
        "renderer_type": renderer.type(),
        "renderer_field": renderer.classAttribute(),
        "classification_method": method.id() if method is not None else "",
        "class_count": len(ranges),
        "distinct_color_count": len({item["color"] for item in ranges}),
        "ranges": ranges,
    }


def _thematic_color_ramp(name: str):
    ramp = QgsStyle.defaultStyle().colorRamp(name)
    if ramp is not None:
        return ramp, "qgis_style"
    # ColorBrewer is bundled with QGIS and also works in a fresh profile whose
    # style database has not been populated. Do not replace unknown ramp names.
    if name not in QgsColorBrewerColorRamp.listSchemeNames():
        raise ValueError(f"未知颜色带 '{name}'，请使用 QGIS 样式库或 ColorBrewer 中的名称")
    counts = QgsColorBrewerColorRamp.listSchemeVariants(name)
    count = max(counts)
    brewer = QgsColorBrewerColorRamp(name, count)
    stops = [
        QgsGradientStop(index / (count - 1), brewer.color(index / (count - 1)))
        for index in range(1, count - 1)
    ]
    return QgsGradientColorRamp(brewer.color(0), brewer.color(1), False, stops), "colorbrewer"


def graduated_style_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer, error = _get_layer(params.get("layer"))
        if error:
            return error
        if not isinstance(layer, QgsVectorLayer) or not layer.isValid() or not layer.isSpatial():
            raise ValueError("仅有效的空间矢量图层支持分级设色")
        field = str(params.get("field") or "").strip()
        field_index = layer.fields().indexFromName(field)
        if field_index < 0:
            raise ValueError(f"图层字段 '{field}' 不存在")
        if not layer.fields().at(field_index).isNumeric():
            raise ValueError(f"字段 '{field}' 必须为数值类型，请先完成字段类型转换")

        requested_classes = params.get("classes")
        if requested_classes is None or requested_classes == "":
            requested_classes = 5
        if isinstance(requested_classes, bool):
            raise ValueError("classes 必须为 2-20 的整数")
        num_classes = int(requested_classes)
        if float(requested_classes) != num_classes or not 2 <= num_classes <= 20:
            raise ValueError("classes 必须为 2-20 的整数")
        method_name = str(params.get("method") or "quantile").strip().lower()
        methods = {
            "quantile": QgsClassificationQuantile,
            "jenks": QgsClassificationJenks,
            "equal_interval": QgsClassificationEqualInterval,
            "stddev": QgsClassificationStandardDeviation,
        }
        if method_name not in methods:
            raise ValueError(f"不支持的分类方法 '{method_name}'")
        method = methods[method_name]()
        ramp_name = str(params.get("color_ramp") or "YlOrRd").strip()
        ramp, ramp_source = _thematic_color_ramp(ramp_name)

        values = []
        null_count = non_finite_count = 0
        request = QgsFeatureRequest().setSubsetOfAttributes([field_index])
        request.setFlags(QgsFeatureRequest.NoGeometry)
        for feature in layer.getFeatures(request):
            value = feature[field_index]
            if value is None or (isinstance(value, QVariant) and value.isNull()):
                null_count += 1
                continue
            value = float(value)
            if not math.isfinite(value):
                non_finite_count += 1
                continue
            values.append(value)
        unique_count = len(set(values))
        if unique_count < 2:
            raise ValueError("字段至少需要两个不同的有限数值，无法用单色结果替代分级设色")

        effective_classes = min(num_classes, unique_count)
        # Classify the validated finite values rather than asking QGIS to read
        # the layer again (which would reintroduce NaN/Inf into the breaks).
        breaks = method.classes(values, effective_classes)
        if len(breaks) < 2:
            raise ValueError("所选分类方法未生成至少两个有效分级，请调整分类方法或数据")
        symbol = QgsSymbol.defaultSymbol(layer.geometryType())
        ranges = []
        for item in breaks:
            lower, upper = float(item.lowerBound()), float(item.upperBound())
            if not math.isfinite(lower) or not math.isfinite(upper) or lower > upper:
                raise ValueError("分类方法生成了无效区间，原图层样式未修改")
            ranges.append(QgsRendererRange(lower, upper, symbol.clone(), item.label()))
        if ranges[0].lowerValue() > min(values) or ranges[-1].upperValue() < max(values):
            raise ValueError("分类区间未覆盖全部有效值，原图层样式未修改")
        renderer = QgsGraduatedSymbolRenderer(field, ranges)
        renderer.setSourceSymbol(symbol.clone())
        renderer.setClassificationMethod(method)
        renderer.updateColorRamp(ramp)
        details = _graduated_renderer_info(renderer)
        if details["distinct_color_count"] < 2:
            raise ValueError("颜色带未产生可区分的分级颜色，原图层样式未修改")

        warnings = []
        if effective_classes != num_classes or details["class_count"] != num_classes:
            warnings.append(f"请求 {num_classes} 级，按有效值和分类方法实际生成 {details['class_count']} 级")
        if null_count or non_finite_count:
            warnings.append(f"{null_count} 个空值及 {non_finite_count} 个非有限值不参与分级，不表示为零")
        # Only replace the original renderer after all validation succeeds.
        layer.setRenderer(renderer)
        layer.triggerRepaint()
        details = _graduated_renderer_info(layer.renderer())
        details.update({
            "requested_classes": num_classes,
            "color_ramp": ramp_name,
            "color_ramp_source": ramp_source,
            "valid_value_count": len(values),
            "null_value_count": null_count,
            "non_finite_value_count": non_finite_count,
            "warnings": warnings,
        })
        return {
            "success": True,
            "output": layer.name(),
            "info": f"分级设色已设置，字段: {field}，方法: {details['classification_method']}，实际分级: {details['class_count']}",
            "data": details,
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"设置分级样式失败: {str(e)}"}


def rule_based_style_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        from qgis.core import QgsRuleBasedRenderer

        layer_name = params.get("layer")
        rules = params.get("rules", [])

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层参数"}
        if not isinstance(rules, list) or len(rules) == 0:
            return {"success": False, "output": "", "info": "错误：rules 需为非空数组"}

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }

        layer = layers[0]
        symbol = QgsSymbol.defaultSymbol(layer.geometryType())
        renderer = QgsRuleBasedRenderer(symbol)
        root_rule = renderer.rootRule()
        template_rule = (
            root_rule.children()[0].clone() if root_rule.children() else None
        )

        for rule_def in rules:
            if template_rule is not None:
                rule = template_rule.clone()
            else:
                rule = QgsRuleBasedRenderer.Rule(symbol.clone())
            rule.setLabel(rule_def.get("label", ""))
            rule.setFilterExpression(rule_def.get("expression", ""))
            if "color" in rule_def:
                rule.symbol().setColor(QColor(rule_def["color"]))
            root_rule.appendChild(rule)

        if root_rule.children():
            root_rule.removeChildAt(0)
        layer.setRenderer(renderer)
        layer.triggerRepaint()

        return {
            "success": True,
            "output": layer_name,
            "info": f"规则样式设置完成，共 {len(rules)} 条规则",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"设置规则样式失败: {str(e)}"}


def heatmap_style_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        from qgis.core import QgsHeatmapRenderer

        layer_name = params.get("layer")
        radius = params.get("radius", 10)
        weight_field = params.get("weight_field")

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层参数"}

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }

        layer = layers[0]
        renderer = QgsHeatmapRenderer()
        renderer.setRadius(radius)

        if weight_field:
            renderer.setWeightExpression(weight_field)

        layer.setRenderer(renderer)
        layer.triggerRepaint()

        return {
            "success": True,
            "output": layer_name,
            "info": f"热力图样式设置完成，半径: {radius}",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"设置热力图样式失败: {str(e)}"}


def point_displacement_style_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        from qgis.core import QgsPointDisplacementRenderer

        layer_name = params.get("layer")
        distance = params.get("distance", 3)
        placement = params.get("placement", 0)

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层参数"}

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }

        layer = layers[0]
        symbol = QgsMarkerSymbol.createSimple({"name": "circle", "size": "3"})

        renderer = QgsPointDisplacementRenderer()
        renderer.setEmbeddedRenderer(QgsSingleSymbolRenderer(symbol))
        renderer.setMinimumLabelScale(0)
        renderer.setTolerance(distance)
        renderer.setPlacement(placement)

        layer.setRenderer(renderer)
        layer.triggerRepaint()

        return {
            "success": True,
            "output": layer_name,
            "info": f"点位移样式设置完成，距离: {distance}",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"设置点位移样式失败: {str(e)}"}


def style_25d_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        from qgis.core import Qgs25DRenderer

        layer_name = params.get("layer")
        height_field = params.get("height_field")

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层参数"}

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }

        layer = layers[0]
        renderer = Qgs25DRenderer()
        renderer.setRoofColor(QColor(180, 180, 180))
        renderer.setWallColor(QColor(120, 120, 120))

        if height_field:
            renderer.symbol()
            renderer.setHeight(f'"{height_field}"')

        layer.setRenderer(renderer)
        layer.triggerRepaint()

        return {"success": True, "output": layer_name, "info": "2.5D样式设置完成"}

    except Exception as e:
        return {"success": False, "output": "", "info": f"设置2.5D样式失败: {str(e)}"}

# 获取图层对象
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

# 安全转颜色
def _color(value: Optional[str], default: str) -> QColor:
    result = QColor(str(value or default).strip() or default)
    if not result.isValid():
        result = QColor(default)
    return result

# 安全转透明度
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

        transparent = QColor(0, 0, 0, 0)

        def _circle_layer(fill_color, stroke_color, size_mm: float, stroke_width_mm: float):
            symbol_layer = QgsSimpleMarkerSymbolLayer.create(
                {
                    "name": "circle",
                    "size": str(size_mm),
                    "color": fill_color.name(QColor.HexArgb),
                    "outline_color": stroke_color.name(QColor.HexArgb),
                    "outline_width": str(stroke_width_mm),
                }
            )
            if symbol_layer is None:
                symbol_layer = QgsMarkerSymbol.createSimple(
                    {
                        "name": "circle",
                        "size": str(size_mm),
                        "color": fill_color.name(QColor.HexArgb),
                        "outline_color": stroke_color.name(QColor.HexArgb),
                        "outline_width": str(stroke_width_mm),
                    }
                ).symbolLayer(0).clone()

            if hasattr(symbol_layer, "setSizeUnit"):
                symbol_layer.setSizeUnit(QgsUnitTypes.RenderMillimeters)
            if hasattr(symbol_layer, "setStrokeWidthUnit"):
                symbol_layer.setStrokeWidthUnit(QgsUnitTypes.RenderMillimeters)
            if hasattr(symbol_layer, "setFillColor"):
                symbol_layer.setFillColor(fill_color)
            elif hasattr(symbol_layer, "setColor"):
                symbol_layer.setColor(fill_color)
            if hasattr(symbol_layer, "setStrokeColor"):
                symbol_layer.setStrokeColor(stroke_color)
            if hasattr(symbol_layer, "setStrokeWidth"):
                symbol_layer.setStrokeWidth(stroke_width_mm)
            if stroke_color.alpha() == 0 and hasattr(symbol_layer, "setStrokeStyle"):
                symbol_layer.setStrokeStyle(Qt.NoPen)
            return symbol_layer

        def _marker_symbol(symbol_layers):
            symbol = QgsMarkerSymbol()
            symbol.changeSymbolLayer(0, symbol_layers[0])
            for symbol_layer in symbol_layers[1:]:
                symbol.appendSymbolLayer(symbol_layer)
            return symbol

        city_symbol = _marker_symbol(
            [
                _circle_layer(transparent, QColor("#000000"), 2.2, 0.1),
                _circle_layer(QColor("#000000"), transparent, 0.8, 0.0),
            ]
        )
        county_symbol = _marker_symbol(
            [
                _circle_layer(transparent, QColor("#000000"), 1.8, 0.1),
                _circle_layer(transparent, QColor("#000000"), 0.8, 0.1),
            ]
        )

        symbol = city_symbol if layer_type == "city" else county_symbol
        layer.setRenderer(QgsSingleSymbolRenderer(symbol))
        
        label_settings = QgsPalLayerSettings()

        if layer.labeling() and hasattr(layer.labeling(), "settings"):
            label_settings = layer.labeling().settings()
        label_settings.fieldName = label_field

        text_format = label_settings.format()
        text_format.setSize(4 if layer_type == "city" else 3)
        text_format.setSizeUnit(QgsUnitTypes.RenderMillimeters)
        label_settings.setFormat(text_format)
        label_properties = label_settings.dataDefinedProperties()
        if hasattr(label_properties, "removeProperty"):
            label_properties.removeProperty(QgsPalLayerSettings.Property.Size)
        else:
            label_properties.setProperty(
                QgsPalLayerSettings.Property.Size,
                QgsProperty(),
            )

        layer.setLabeling(QgsVectorLayerSimpleLabeling(label_settings))
        layer.setLabelsEnabled(True)

        layer.triggerRepaint()
        return {
            "success": True,
            "output": layer.name(),
            "info": "点图层符号已更新",
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"设置点图层符号失败: {str(e)}"}

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


_RIVER_LINE_COLOR = "#a6cee3"


def _line_symbol_by_type(
    line_type: str,
) -> Tuple[Optional[QgsLineSymbol], Optional[str]]:
    normalized_type = str(line_type or "").strip()
    if normalized_type == "铁路":
        return (
            _gb_line_symbol(
                [
                    _gb_line_layer("#000000", 0.4),
                    _gb_line_layer("#ffffff", 0.4, dash_pattern=[4.00, 4.00]),
                ]
            ),
            "铁路",
        )
    if normalized_type == "高速公路":
        return (
            _gb_line_symbol(
                [
                    _gb_line_layer("#000000", 0.4),
                    _gb_line_layer("#c0c0c0", 0.3),
                ]
            ),
            "高速公路",
        )
    if normalized_type == "国道":
        return (
            _gb_line_symbol(
                [
                    _gb_line_layer("#ffb957", 0.30),
                ]
            ),
            "国道",
        )
    if normalized_type == "省界":
        return (
            _gb_line_symbol(
                [
                    _gb_line_layer("#2399e9", 0.5),
                    _gb_line_layer("#a6cee3", 0.5, offset_mm = 0.4),
                ]
            ),
            "省界",
        )
    if normalized_type == "河流":
        return (
            _gb_line_symbol(
                [
                    _gb_line_layer(_RIVER_LINE_COLOR, 0.46),
                ]
            ),
            "河流",
        )
    return None, None


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
        # 河流名称字段是底图的固定约定。即使调用方漏传 label_field，
        # 也不要让河流图层继续沿用上一次运行留下的标注状态。
        if line_type == "河流" and not label_field:
            if layer.fields().indexFromName("NAME") >= 0:
                label_field = "NAME"
        if label_field and layer.fields().indexFromName(label_field) < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：图层字段 '{label_field}' 不存在",
            }

        layer.setRenderer(QgsSingleSymbolRenderer(symbol))

        label_updated = False
        if line_type == "河流" and label_field:
            # 河流线是线图层，必须使用 QGIS 对线图层的默认标注配置。
            # 裸 QgsPalLayerSettings() 的 placement 默认是 AroundPoint（点/面模式），
            # 用在线图层上会导致一个标签都不显示。这里直接取线图层默认设置，
            # 与用户在属性里手动打开标注的行为一致（placement = Line）。
            label_settings = QgsVectorLayerSimpleLabeling.defaultSettingsForLayer(layer)
            label_settings.fieldName = label_field
            # 河流线被拆成很多端点相接的小段，默认每段都会标一个，导致河名密密麻麻。
            # 合并相连线，让一条连续河段只标一个名字。
            label_settings.mergeLines = True
            river_text_format = label_settings.format()
            river_text_format.setColor(QColor(_RIVER_LINE_COLOR))
            label_settings.setFormat(river_text_format)
            layer.setLabeling(QgsVectorLayerSimpleLabeling(label_settings))
            layer.setLabelsEnabled(True)
            # 回读一次，避免工具返回“成功”但标注实际没有启用。
            applied_settings = layer.labeling().settings()
            if not layer.labelsEnabled() or applied_settings.fieldName != label_field:
                return {
                    "success": False,
                    "output": "",
                    "info": f"河流名称标注未成功写入图层：{layer.name()}（字段 {label_field}）",
                }
            QgsProject.instance().setDirty(True)
            label_updated = True

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

# 给面图层设置填充和标注
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

        layer_type_aliases = {
            "县": "county",
            "county": "county",
            "市": "city",
            "city": "city",
            "省": "province",
            "province": "province",
            "河流": "river",
            "river": "river",
        }
        layer_type_raw = str(params.get("layer_type") or "").strip()
        layer_type = layer_type_aliases.get(layer_type_raw.lower()) or layer_type_aliases.get(layer_type_raw)
        if layer_type not in {"county", "city", "province", "river"}:
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

        fill_layer = QgsSimpleFillSymbolLayer()
        fill_layer.setBrushStyle(Qt.SolidPattern)
        if layer_type == "river":
            fill_layer.setFillColor(QColor("#a6cee3"))
            fill_layer.setStrokeColor(QColor("#a6cee3"))
            fill_layer.setStrokeWidth(0.26)
            fill_layer.setStrokeStyle(Qt.SolidLine)
        else:
            fill_layer.setFillColor(QColor("#ffffff"))
            fill_layer.setStrokeColor(QColor("#707070"))
            fill_layer.setStrokeWidth(0.5 if layer_type == "province" else 0.2)
            fill_layer.setStrokeStyle(Qt.DashDotLine)

        if hasattr(fill_layer, "setStrokeWidthUnit"):
            fill_layer.setStrokeWidthUnit(QgsUnitTypes.RenderMillimeters)

        symbol = QgsFillSymbol()
        symbol.changeSymbolLayer(0, fill_layer)
        layer.setRenderer(QgsSingleSymbolRenderer(symbol))

        label_updated = False
        if label_field:
            label_size_mm = {
                "county": 2.2,
                "city": 3.0,
                "province": 3.8,
                "river": 2.6,
            }[layer_type]
            label_settings = QgsPalLayerSettings()
            if layer.labeling() and hasattr(layer.labeling(), "settings"):
                label_settings = layer.labeling().settings()
            label_settings.fieldName = label_field
            try:
                label_settings.placement = Qgis.LabelPlacement.Horizontal
            except AttributeError:
                label_settings.placement = QgsPalLayerSettings.Horizontal

            text_format = label_settings.format()
            label_font = QFont()
            # 地图标注用“微软雅黑”本体，而不是 UI 专用字体；mac 上没有微软雅黑时
            # 回退到思源黑体/冬青黑体，避免悄悄退成系统默认字体。
            label_font.setFamilies(["Microsoft YaHei", "Noto Sans CJK SC", "Hiragino Sans GB"])
            label_font.setBold(True)
            text_format.setFont(label_font)
            text_format.setSize(label_size_mm)
            text_format.setSizeUnit(QgsUnitTypes.RenderMillimeters)
            text_format.setColor(QColor("#242424"))
            if layer_type == "province":
                # 省名是跨省底图的主层级标注，不能被县级地名或路线标注
                # 直接压掉；但不把省名设为其他标注的障碍，避免它们
                # 大面积消失。
                if hasattr(label_settings, "displayAll"):
                    label_settings.displayAll = True
                if hasattr(label_settings, "obstacle"):
                    label_settings.obstacle = False
                if hasattr(label_settings, "priority"):
                    label_settings.priority = 5
                if hasattr(label_settings, "allowDegradedPlacement"):
                    label_settings.allowDegradedPlacement = True
                text_format.setSize(5.2)
                province_buffer = QgsTextBufferSettings()
                province_buffer.setEnabled(True)
                province_buffer.setSize(0.7)
                province_buffer.setSizeUnit(QgsUnitTypes.RenderMillimeters)
                province_buffer.setColor(QColor("#ffffff"))
                text_format.setBuffer(province_buffer)
            label_settings.setFormat(text_format)
            label_properties = label_settings.dataDefinedProperties()
            if hasattr(label_properties, "removeProperty"):
                label_properties.removeProperty(QgsPalLayerSettings.Property.Size)
            else:
                label_properties.setProperty(
                    QgsPalLayerSettings.Property.Size,
                    QgsProperty(),
                )

            layer.setLabeling(QgsVectorLayerSimpleLabeling(label_settings))
            layer.setLabelsEnabled(True)
            applied_settings = layer.labeling().settings()
            if not layer.labelsEnabled() or applied_settings.fieldName != label_field:
                return {
                    "success": False,
                    "output": "",
                    "info": f"面图层标注未成功写入图层：{layer.name()}（字段 {label_field}）",
                }
            QgsProject.instance().setDirty(True)
            label_updated = True
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

# 给图层设置混合模式
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

# 给图层设置色带（仅支持分级渲染器）
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

# 获取图层样式信息，包括渲染器类型、符号颜色、透明度等基本属性，供前端展示和调试使用
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
        layer, error = _get_layer(params.get("layer"))
        if error:
            return error
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
        return {"success": True, "output": str(path), "info": "图层样式已保存"}
    except Exception as e:
        return {"success": False, "output": "", "info": f"保存图层样式失败: {str(e)}"}


def load_style_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer, error = _get_layer(params.get("layer"))
        if error:
            return error
        path = params.get("path") or params.get("style_path")
        if not path:
            return {"success": False, "output": "", "info": "错误：缺少 path 参数"}
        message, ok = layer.loadNamedStyle(str(path))
        if not ok:
            return {
                "success": False,
                "output": "",
                "info": f"加载图层样式失败: {message}",
            }
        layer.triggerRepaint()
        return {"success": True, "output": str(path), "info": "图层样式已加载"}
    except Exception as e:
        return {"success": False, "output": "", "info": f"加载图层样式失败: {str(e)}"}


def symbolize_points_by_mapping_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    """按点表与符号表映射，对指定点应用不同符号（其余点保持默认样式）。"""
    try:
        from qgis.core import (
            QgsMarkerSymbol,
            QgsRendererCategory,
            QgsRuleBasedRenderer,
            QgsSingleSymbolRenderer,
            QgsSvgMarkerSymbolLayer,
            QgsWkbTypes,
        )

        # 点图层名称（必填）
        layer_name = params.get("layer")
        # 点记录列表（必填）：来自业务点表，至少包含 name 与 symbol_type
        point_rows = params.get("point_rows") or []
        # 符号记录列表（必填）：来自符号表，至少包含 symbol_type 与 svg_path
        symbol_rows = params.get("symbol_rows") or []
        # 图层中的名称字段（默认 name）
        name_field = str(params.get("name_field", "name") or "name")
        # 点记录中的符号类型字段（默认 symbol_type）
        symbol_type_field = str(params.get("symbol_type_field", "symbol_type") or "symbol_type")
        # 视为默认样式、无需替换符号的 symbol_type 列表
        default_symbol_types = params.get("default_symbol_types") or []
        # 严格模式：匹配失败/符号缺失时直接报错
        strict = bool(params.get("strict", True))
        # 未匹配点是否隐藏（true=隐藏；false=保持当前图层默认渲染）
        hide_unmatched = bool(params.get("hide_unmatched", False))
        # 符号缺省大小（mm）
        fallback_size = float(params.get("fallback_size", 4) or 4)

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层参数"}
        if not isinstance(point_rows, list) or not point_rows:
            return {"success": False, "output": "", "info": "错误：point_rows 需为非空数组"}
        if not isinstance(symbol_rows, list) or not symbol_rows:
            return {"success": False, "output": "", "info": "错误：symbol_rows 需为非空数组"}

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {"success": False, "output": "", "info": f"错误：未找到图层 '{layer_name}'"}
        layer = layers[0]
        if layer.geometryType() != QgsWkbTypes.PointGeometry:
            return {"success": False, "output": "", "info": "错误：该工具仅支持点图层"}

        if layer.fields().indexFromName(name_field) < 0:
            return {"success": False, "output": "", "info": f"错误：图层字段 '{name_field}' 不存在"}

        # symbol_type -> symbol 配置映射
        symbol_map: Dict[str, Dict[str, Any]] = {}
        for row in symbol_rows:
            if not isinstance(row, dict):
                continue
            st = str(row.get("symbol_type", "") or "").strip()
            if not st:
                continue
            symbol_map[st] = row

        if not symbol_map:
            return {"success": False, "output": "", "info": "错误：symbol_rows 中没有有效的 symbol_type 配置"}

        # 按 symbol_type 聚合 name
        names_by_symbol_type: Dict[str, List[str]] = {}
        skipped_default = 0
        skipped_missing_symbol = 0
        skipped_empty_name = 0
        for row in point_rows:
            if not isinstance(row, dict):
                continue
            name_value = str(row.get("name", "") or "").strip()
            if not name_value:
                skipped_empty_name += 1
                continue
            st = str(row.get(symbol_type_field, "") or "").strip()
            if not st or st in default_symbol_types:
                skipped_default += 1
                continue
            if st not in symbol_map:
                skipped_missing_symbol += 1
                if strict:
                    return {"success": False, "output": "", "info": f"错误：symbol_type '{st}' 在符号表中不存在"}
                continue
            names_by_symbol_type.setdefault(st, []).append(name_value)

        if not names_by_symbol_type:
            return {
                "success": True,
                "output": layer_name,
                "info": "没有需要符号化的目标点（可能均为默认样式或匹配为空）",
                "data": {
                    "applied_rules": 0,
                    "skipped_default": skipped_default,
                    "skipped_missing_symbol": skipped_missing_symbol,
                    "skipped_empty_name": skipped_empty_name,
                },
            }

        # 记录当前默认符号，用作“其他点”样式
        current_renderer = layer.renderer()
        default_symbol = None
        if current_renderer is not None and hasattr(current_renderer, "symbol"):
            default_symbol = current_renderer.symbol().clone()
        if default_symbol is None:
            default_symbol = QgsMarkerSymbol.createSimple({"name": "circle", "size": "2", "color": "#999999"})

        renderer = QgsRuleBasedRenderer(default_symbol.clone())
        root = renderer.rootRule()

        # 清理初始模板规则
        while root.children():
            root.removeChildAt(0)

        def _quote(value: str) -> str:
            return value.replace("'", "''")

        # 按 priority 从大到小应用规则（优先级高的先匹配）
        sortable = []
        for st, names in names_by_symbol_type.items():
            cfg = symbol_map.get(st, {})
            try:
                pr = float(cfg.get("priority", 0) or 0)
            except (TypeError, ValueError):
                pr = 0.0
            sortable.append((pr, st, names, cfg))
        sortable.sort(key=lambda x: x[0], reverse=True)

        applied_rules = 0
        for _, st, names, cfg in sortable:
            svg_path = str(cfg.get("svg_path", "") or "").strip()
            if not svg_path:
                if strict:
                    return {"success": False, "output": "", "info": f"错误：symbol_type '{st}' 缺少 svg_path"}
                continue

            try:
                size = float(cfg.get("size", fallback_size) or fallback_size)
            except (TypeError, ValueError):
                size = fallback_size

            marker_symbol = QgsMarkerSymbol()
            svg_layer = QgsSvgMarkerSymbolLayer(svg_path)
            svg_layer.setSize(size)

            # 锚点偏移（单位通常与渲染单位相关；按可用 API 尝试设置）
            try:
                anchor_x = float(cfg.get("anchor_x", 0) or 0)
                anchor_y = float(cfg.get("anchor_y", 0) or 0)
                if hasattr(svg_layer, "setOffset"):
                    from qgis.core import QgsUnitTypes
                    from qgis.PyQt.QtCore import QPointF
                    svg_layer.setOffset(QPointF(anchor_x, anchor_y))
                    if hasattr(svg_layer, "setOffsetUnit"):
                        svg_layer.setOffsetUnit(QgsUnitTypes.RenderMillimeters)
            except Exception:
                pass

            marker_symbol.changeSymbolLayer(0, svg_layer)

            uniq_names = sorted(set(names))
            expr_values = ", ".join([f"'{_quote(n)}'" for n in uniq_names])
            expression = f'"{name_field}" IN ({expr_values})'

            rule = QgsRuleBasedRenderer.Rule(marker_symbol)
            rule.setLabel(
                {
                    "waypoint": "途经点",
                    "start_flag": "起点",
                    "battle": "战斗",
                    "meeting": "会议",
                    "rendezvous": "会师",
                }.get(st, st)
            )
            rule.setFilterExpression(expression)
            root.appendChild(rule)
            applied_rules += 1

        if applied_rules == 0:
            return {"success": False, "output": "", "info": "错误：没有可应用的符号规则"}

        if not hide_unmatched:
            else_rule = QgsRuleBasedRenderer.Rule(default_symbol.clone())
            if hasattr(else_rule, "setIsElse"):
                else_rule.setIsElse(True)
            elif hasattr(else_rule, "setElse"):
                else_rule.setElse(True)
            else:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：当前 QGIS 版本不支持 Else 规则标记（setIsElse/setElse 均不可用）",
                }
            else_rule.setLabel("其他点")
            root.appendChild(else_rule)

        layer.setRenderer(renderer)
        layer.triggerRepaint()

        return {
            "success": True,
            "output": layer_name,
            "info": f"已完成按映射符号化，共应用 {applied_rules} 条规则",
            "data": {
                "applied_rules": applied_rules,
                "matched_symbol_types": list(names_by_symbol_type.keys()),
                "skipped_default": skipped_default,
                "skipped_missing_symbol": skipped_missing_symbol,
                "skipped_empty_name": skipped_empty_name,
            },
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"按映射符号化失败: {str(e)}"}


def draw_legacy_arrows_from_points_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    """
    调用 QGIS 插件绘制传统军标箭头（细箭头/燕尾攻击箭头/双箭头/三箭头）。

    参数说明（中文）：
    - plugin_dir: 插件目录（必填）
    - points: 控制点数组（必填），支持 [[x,y], ...] 或 [{x,y}, ...]
    - type: 图元类型（必填）
        * fine_arrow（细箭头，至少2点）
        * tailed_attack_arrow（燕尾攻击箭头，至少3点）
        * double_arrow（双箭头，至少3点）
        * three_arrow（三箭头，建议5-7点）
    - input_crs: 输入点坐标系（可选，默认 EPSG:4326）
    """
    try:
        import importlib
        import sys
        from pathlib import Path

        plugin_dir = params.get("plugin_dir")
        points = params.get("points")
        plot_type = str(params.get("type", "") or "").strip()
        input_crs_authid = str(params.get("input_crs", "EPSG:4326") or "EPSG:4326").strip()

        if not plugin_dir:
            return {"success": False, "output": "", "info": "错误：缺少 plugin_dir 参数"}
        if not isinstance(points, list):
            return {"success": False, "output": "", "info": "错误：points 必须为数组"}

        type_to_style = {
            "fine_arrow": "fineArrow",
            "tailed_attack_arrow": "tailedAttackArrow",
            "double_arrow": "doubleArrow",
            "three_arrow": "threeArrow",
        }
        min_points = {
            "fine_arrow": 2,
            "tailed_attack_arrow": 3,
            "double_arrow": 3,
            "three_arrow": 5,
        }
        if plot_type not in type_to_style:
            return {"success": False, "output": "", "info": f"错误：不支持的 type: {plot_type}"}
        if len(points) < min_points[plot_type]:
            return {"success": False, "output": "", "info": f"错误：{plot_type} 至少需要 {min_points[plot_type]} 个控制点"}

        plugin_path = Path(str(plugin_dir))
        service_path = plugin_path / "services" / "combined_plot_service.py"
        if not service_path.exists():
            return {"success": False, "output": "", "info": f"错误：未找到插件服务文件 {service_path}"}

        plugin_parent = str(plugin_path.parent)
        plugin_pkg = plugin_path.name
        module_name = f"{plugin_pkg}.services.combined_plot_service"
        if plugin_parent not in sys.path:
            sys.path.insert(0, plugin_parent)
        mod = importlib.import_module(module_name)

        from qgis.utils import iface
        if iface is None:
            return {"success": False, "output": "", "info": "错误：当前不在QGIS界面上下文（iface为空）"}

        project_crs = iface.mapCanvas().mapSettings().destinationCrs()
        src_crs = QgsCoordinateReferenceSystem(input_crs_authid)
        if not src_crs.isValid():
            return {"success": False, "output": "", "info": f"错误：input_crs 非法: {input_crs_authid}"}

        xform = None
        if src_crs.authid() != project_crs.authid():
            xform = QgsCoordinateTransform(src_crs, project_crs, QgsProject.instance())

        def _convert_point_like(p: Any):
            if isinstance(p, dict) and ("x" in p and "y" in p):
                qpt = QgsPointXY(float(p["x"]), float(p["y"]))
                if xform is not None:
                    qpt = xform.transform(qpt)
                return [qpt.x(), qpt.y()]
            if isinstance(p, (list, tuple)) and len(p) >= 2:
                qpt = QgsPointXY(float(p[0]), float(p[1]))
                if xform is not None:
                    qpt = xform.transform(qpt)
                return [qpt.x(), qpt.y()]
            raise ValueError("点坐标格式非法，需为 [x,y] 或 {x,y}")

        points_project = [_convert_point_like(p) for p in points]

        service = mod.CombinedPlotService(iface, str(plugin_path))
        result = service.add_plot_from_spec({
            "type": "legacy_arrow",
            "points": points_project,
            "style_id": type_to_style[plot_type],
            "options": {},
        })

        ok = bool((result or {}).get("ok"))
        return {
            "success": ok,
            "output": str((result or {}).get("feature_id", "")),
            "info": str((result or {}).get("message", "ok" if ok else "生成失败")),
            "data": result or {},
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"绘制传统军标箭头失败: {str(e)}"}


def draw_bezier_arrow_from_points_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    """
    调用 QGIS 插件绘制“经过点贝塞尔箭头”。

    参数说明（中文）：
    - plugin_dir: 插件目录（必填），例如 C:/.../CombinedMilitaryPlotPluginBranchBezierForkNatural
    - points: 经过点数组（必填），支持 [[x,y], ...] 或 [{x:..., y:...}, ...]，至少 2 个点
    - style_id: 样式ID（必填），例如 bezier_red_gradient / bezier_blue_gradient / route_red_reference
    - type: 图元类型（可选，默认 bezier_arrow），当前支持 bezier_arrow / branch_bezier / multi_start_bezier
    - input_crs: 输入点坐标系（可选，默认 EPSG:4326）
    - options: 扩展参数（可选）
        * width_scale: 样式宽度缩放比例（可选），例如 0.4
        * branch_bezier: 需提供 attach_point 与 branch_points
        * multi_start_bezier: 需提供 starts

    坐标处理逻辑（关键）：
    1) 先读取当前工程 CRS；
    2) 再读取输入点 CRS（默认 EPSG:4326）；
    3) 若两者不同，先转换到工程 CRS 再调用插件。
    """
    try:
        import importlib
        import sys
        from pathlib import Path

        plugin_dir = params.get("plugin_dir")
        points = params.get("points")
        style_id = str(params.get("style_id", "") or "").strip()
        plot_type = str(params.get("type", "bezier_arrow") or "bezier_arrow").strip()
        input_crs_authid = str(params.get("input_crs", "EPSG:4326") or "EPSG:4326").strip()
        options = params.get("options") or {}

        if not plugin_dir:
            return {"success": False, "output": "", "info": "错误：缺少 plugin_dir 参数"}
        if not isinstance(points, list) or len(points) < 2:
            return {"success": False, "output": "", "info": "错误：points 必须为至少2个点的数组"}
        if not style_id:
            return {"success": False, "output": "", "info": "错误：缺少 style_id 参数"}

        plugin_path = Path(str(plugin_dir))
        service_path = plugin_path / "services" / "combined_plot_service.py"
        if not service_path.exists():
            return {"success": False, "output": "", "info": f"错误：未找到插件服务文件 {service_path}"}

        plugin_parent = str(plugin_path.parent)
        plugin_pkg = plugin_path.name
        module_name = f"{plugin_pkg}.services.combined_plot_service"

        if plugin_parent not in sys.path:
            sys.path.insert(0, plugin_parent)

        # 强制从 plugin_dir 重新加载，避免 QGIS 会话内一直用旧版 combined_plot_service（改插件后“毫无变化”）
        for stale in list(sys.modules.keys()):
            if stale == module_name or stale.startswith(plugin_pkg + "."):
                del sys.modules[stale]
        mod = importlib.import_module(module_name)
        mod = importlib.reload(mod)

        try:
            _svc_mtime = int(service_path.stat().st_mtime)
        except Exception:
            _svc_mtime = None
        _fork_gap = hasattr(mod.CombinedPlotService, "_build_fork_gap_work") and hasattr(
            mod.CombinedPlotService, "_build_merge_controls_to_attach"
        )

        from qgis.utils import iface
        if iface is None:
            return {"success": False, "output": "", "info": "错误：当前不在QGIS界面上下文（iface为空）"}

        # 1) 读取工程 CRS
        project_crs = iface.mapCanvas().mapSettings().destinationCrs()
        # 2) 读取输入 CRS（默认 EPSG:4326）
        src_crs = QgsCoordinateReferenceSystem(input_crs_authid)
        if not src_crs.isValid():
            return {"success": False, "output": "", "info": f"错误：input_crs 非法: {input_crs_authid}"}

        # 3) CRS 不同则转换到工程 CRS
        xform = None
        if src_crs.authid() != project_crs.authid():
            xform = QgsCoordinateTransform(src_crs, project_crs, QgsProject.instance())

        def _convert_point_like(p: Any):
            if isinstance(p, dict) and ("x" in p and "y" in p):
                qpt = QgsPointXY(float(p["x"]), float(p["y"]))
                if xform is not None:
                    qpt = xform.transform(qpt)
                return [qpt.x(), qpt.y()]
            if isinstance(p, (list, tuple)) and len(p) >= 2:
                qpt = QgsPointXY(float(p[0]), float(p[1]))
                if xform is not None:
                    qpt = xform.transform(qpt)
                return [qpt.x(), qpt.y()]
            raise ValueError("点坐标格式非法，需为 [x,y] 或 {x,y}")

        points_project = [_convert_point_like(p) for p in points]

        options_project = options
        width_scale = None
        if isinstance(options, dict):
            options_project = dict(options)
            if "attach_point" in options_project and options_project.get("attach_point") is not None:
                options_project["attach_point"] = _convert_point_like(options_project.get("attach_point"))
            if "branch_points" in options_project and isinstance(options_project.get("branch_points"), list):
                options_project["branch_points"] = [_convert_point_like(p) for p in options_project.get("branch_points", [])]
            if "main_paths" in options_project and isinstance(options_project.get("main_paths"), list):
                main_paths_new = []
                for path_points in options_project.get("main_paths", []):
                    if isinstance(path_points, list):
                        main_paths_new.append([_convert_point_like(p) for p in path_points])
                options_project["main_paths"] = main_paths_new
            if "branches" in options_project and isinstance(options_project.get("branches"), list):
                branches_new = []
                for b in options_project.get("branches", []):
                    if not isinstance(b, dict):
                        continue
                    b2 = dict(b)
                    if "main_points" in b2 and isinstance(b2.get("main_points"), list):
                        b2["main_points"] = [_convert_point_like(p) for p in b2.get("main_points", [])]
                    if "attach_point" in b2 and b2.get("attach_point") is not None:
                        b2["attach_point"] = _convert_point_like(b2.get("attach_point"))
                    if "branch_points" in b2 and isinstance(b2.get("branch_points"), list):
                        b2["branch_points"] = [_convert_point_like(p) for p in b2.get("branch_points", [])]
                    branches_new.append(b2)
                options_project["branches"] = branches_new
            if "starts" in options_project and isinstance(options_project.get("starts"), list):
                starts_new = []
                for s in options_project.get("starts", []):
                    if not isinstance(s, dict):
                        continue
                    s2 = dict(s)
                    if "attach_point" in s2 and s2.get("attach_point") is not None:
                        s2["attach_point"] = _convert_point_like(s2.get("attach_point"))
                    if "start_points" in s2 and isinstance(s2.get("start_points"), list):
                        s2["start_points"] = [_convert_point_like(p) for p in s2.get("start_points", [])]
                    starts_new.append(s2)
                options_project["starts"] = starts_new
            if "width_scale" in options_project:
                try:
                    width_scale = float(options_project.get("width_scale"))
                except Exception:
                    return {"success": False, "output": "", "info": "错误：options.width_scale 必须为数字"}
                if width_scale <= 0:
                    return {"success": False, "output": "", "info": "错误：options.width_scale 必须大于 0"}

        # 可选：按比例缩放样式宽度参数（tail/neck/head/head_length）
        scaled_style_key = None
        original_style_snapshot = None
        if width_scale is not None and not getattr(mod.CombinedPlotService, "SUPPORTS_GROUP_WIDTH", False):
            scaled_style_key = style_id
            if plot_type in ("branch_bezier", "branch_network_bezier"):
                scaled_style_key = (getattr(mod, "BRANCH_BEZIER_SAME_STYLE_MAP", {}) or {}).get(style_id, style_id)
            elif plot_type == "multi_start_bezier":
                scaled_style_key = (getattr(mod, "MULTI_START_BEZIER_SAME_STYLE_MAP", {}) or {}).get(style_id, style_id)

            style_table = getattr(mod, "BEZIER_ARROW_STYLES", None)
            if not isinstance(style_table, dict) or scaled_style_key not in style_table:
                return {"success": False, "output": "", "info": f"错误：无法定位样式以应用 width_scale（style_id={style_id}）"}

            style_obj = style_table[scaled_style_key]
            original_style_snapshot = {
                "tail_width": float(style_obj.get("tail_width", 0.0)),
                "neck_width": float(style_obj.get("neck_width", 0.0)),
                "head_width": float(style_obj.get("head_width", 0.0)),
                "head_length": float(style_obj.get("head_length", 0.0)),
            }
            style_obj["tail_width"] = original_style_snapshot["tail_width"] * width_scale
            style_obj["neck_width"] = original_style_snapshot["neck_width"] * width_scale
            style_obj["head_width"] = original_style_snapshot["head_width"] * width_scale
            style_obj["head_length"] = original_style_snapshot["head_length"] * width_scale

        service = mod.CombinedPlotService(iface, str(plugin_path))
        try:
            spec = {
                "route_id": params.get("route_id", ""),
                "type": plot_type,
                "points": points_project,
                "style_id": style_id,
                "options": options_project,
            }
            if params.get("_prepare_only"):
                prepare = getattr(service, "prepare_plot_from_spec", None)
                result = prepare(spec) if prepare else {
                    "ok": True, "message": "当前插件保留原绘制入口",
                    "width_group_supported": False,
                }
            else:
                result = service.add_plot_from_spec(spec)
        finally:
            if scaled_style_key is not None and original_style_snapshot is not None:
                try:
                    style_obj = mod.BEZIER_ARROW_STYLES.get(scaled_style_key)
                    if isinstance(style_obj, dict):
                        style_obj.update(original_style_snapshot)
                except Exception:
                    pass

        ok = bool((result or {}).get("ok"))
        info = str((result or {}).get("message", "ok" if ok else "生成失败"))
        if xform is not None:
            info = f"{info}（已从 {src_crs.authid()} 转换到工程 {project_crs.authid()}）"
        # 便于确认是否加载到最新插件（fork_gap=true 表示分叉/汇入新逻辑已生效）
        ver_bits = []
        if _svc_mtime is not None:
            ver_bits.append(f"plugin_mtime={_svc_mtime}")
        ver_bits.append(f"fork_gap={'yes' if _fork_gap else 'NO-OLD-PLUGIN'}")
        ver_bits.append(f"plot={plot_type}")
        info = f"{info} [{' '.join(ver_bits)}]"

        return {
            "success": ok,
            "output": str((result or {}).get("feature_id", "")),
            "info": info,
            "data": {
                **(result or {}),
                "plugin_verify": {
                    "plugin_dir": str(plugin_path),
                    "service_mtime": _svc_mtime,
                    "fork_gap_logic": bool(_fork_gap),
                    "plot_type": plot_type,
                },
            },
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"绘制经过点贝塞尔箭头失败: {str(e)}"}


def _draw_bezier_route_batch(payloads: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """Choose one safe width per semantic route group before writing features."""
    responses = [None] * len(payloads)
    groups = {}
    for index, payload in enumerate(payloads):
        options = payload.get("options") or {}
        style = str(payload.get("style_id") or "")
        for prefix in ("branch_", "multi_start_"):
            if style.startswith(prefix):
                style = style[len(prefix):]
        explicit = options.get("width_group")
        # Compiled main paths retain their source route ID before this suffix.
        # Unrelated enemy arrows keep their existing two-point symbol sizing.
        source = str(payload.get("route_id") or "").split("__render_", 1)[0]
        if not explicit and ("red" not in style or not source):
            continue
        scale = float(options.get("width_scale", 1.0))
        key = (str(payload.get("plugin_dir") or ""), str(explicit or source), style, scale)
        groups.setdefault(key, []).append(index)

    for key, members in groups.items():
        explicit = any((payloads[i].get("options") or {}).get("width_group") for i in members)
        if len(members) < 2 and not explicit:
            continue
        factor = 1.0
        prepared = {}
        selected = {}
        supported = True
        for attempt in range(6):
            prepared, selected, reductions = {}, {}, []
            for index in members:
                payload = dict(payloads[index])
                options = dict(payload.get("options") or {})
                requested_scale = float(options.get("width_scale", 1.0))
                options.update(
                    width_scale=requested_scale * factor,
                    lock_body_width=True,
                    width_group={"id": key[1], "factor": factor,
                                 "requested_scale": requested_scale,
                                 "applied_scale": requested_scale * factor},
                )
                payload["options"] = options
                result = draw_bezier_arrow_from_points_handler(dict(payload, _prepare_only=True))
                data = result.get("data") or {}
                if data.get("width_group_supported") is False:
                    supported = False
                    break
                prepared[index], selected[index] = result, payload
                reduction = data.get("recommended_width_scale")
                if reduction is not None:
                    reduction = float(reduction)
                    if not math.isfinite(reduction) or not 0 < reduction < 1:
                        raise ValueError("插件返回的整组宽度缩放比例无效")
                    reductions.append(reduction)
            if not supported or not reductions:
                break
            if attempt < 5:
                # Leave a small numerical margin when rechecking the entire group.
                factor *= min(reductions) * 0.995
        if not supported:
            continue
        for index in members:
            result = prepared[index]
            if result.get("success"):
                responses[index] = draw_bezier_arrow_from_points_handler(selected[index])
            else:
                responses[index] = result
    for index, payload in enumerate(payloads):
        if responses[index] is None:
            responses[index] = draw_bezier_arrow_from_points_handler(payload)
    return responses


set_layer_style_handler = _wrap_main_thread(set_layer_style_handler)
add_labels_handler = _wrap_main_thread(add_labels_handler)
graduated_style_handler = _wrap_main_thread(graduated_style_handler)
rule_based_style_handler = _wrap_main_thread(rule_based_style_handler)
heatmap_style_handler = _wrap_main_thread(heatmap_style_handler)
point_displacement_style_handler = _wrap_main_thread(point_displacement_style_handler)
style_25d_handler = _wrap_main_thread(style_25d_handler)
set_marker_symbol_handler = _wrap_main_thread(set_marker_symbol_handler)
set_line_symbol_handler = _wrap_main_thread(set_line_symbol_handler)
set_fill_symbol_handler = _wrap_main_thread(set_fill_symbol_handler)
set_layer_opacity_handler = _wrap_main_thread(set_layer_opacity_handler)
set_layer_blend_mode_handler = _wrap_main_thread(set_layer_blend_mode_handler)
set_color_ramp_handler = _wrap_main_thread(set_color_ramp_handler)
set_symbol_size_scale_handler = _wrap_main_thread(set_symbol_size_scale_handler)
get_style_info_handler = _wrap_main_thread(get_style_info_handler)
save_style_handler = _wrap_main_thread(save_style_handler)
load_style_handler = _wrap_main_thread(load_style_handler)
symbolize_points_by_mapping_handler = _wrap_main_thread(symbolize_points_by_mapping_handler)


def _to_bool(v: Any) -> bool:
    if isinstance(v, bool):
        return v
    s = str(v or "").strip().lower()
    return s in {"1", "true", "yes", "y", "on"}


def _read_csv_rows(csv_path: Path) -> List[Dict[str, Any]]:
    with csv_path.open("r", encoding="utf-8-sig", newline="") as f:
        return list(csv.DictReader(f))


def _find_col(row: Dict[str, Any], aliases: List[str]) -> Optional[str]:
    key_map = {str(k).strip().lower(): k for k in row.keys()}
    for a in aliases:
        if a.lower() in key_map:
            return key_map[a.lower()]
    return None


def _get_col_value(row: Dict[str, Any], aliases: List[str], required: bool = False, default: Any = None) -> Any:
    col = _find_col(row, aliases)
    if col is None:
        if required:
            raise ValueError(f"缺少列: {aliases[0]}")
        return default
    return row.get(col, default)

# 根据路线类型 + 绘制方式，判断这条路线是：主线 / 支线 / 敌方路线
def _route_style_kind(draw_type: str, route_type: str = "") -> str:
    route_type = str(route_type or "").strip().lower()
    draw_type = str(draw_type or "").strip().lower()
    if route_type == "enemy" or draw_type == "multi_start_bezier":
        return "enemy"
    if route_type == "branch" or draw_type == "branch_bezier":
        return "branch"
    return "main"


def _route_curve_params(kind: str) -> Dict[str, float]:
    # 只做很轻的手绘感修正，避免主线被揉成波浪。
    if kind == "enemy":
        return {"amp": 0.075, "max": 0.24, "min_len": 0.42}
    if kind == "branch":
        return {"amp": 0.045, "max": 0.14, "min_len": 0.30}
    return {"amp": 0.028, "max": 0.09, "min_len": 0.34}

# 根据路线类型 + 绘制样式，自动匹配「参考线 / 基准线样式 ID」的工具函数
def _route_reference_style_id(style_id: str, draw_type: str, route_type: str = "") -> str:
    style_id = str(style_id or "").strip()
    draw_type = str(draw_type or "").strip().lower()
    route_type = str(route_type or "").strip().lower()
    if route_type == "enemy" or draw_type == "multi_start_bezier":
        if style_id in ("bezier_blue_gradient", "multi_start_bezier_blue_gradient"):
            return "multi_start_route_blue_reference" if draw_type == "multi_start_bezier" else "route_blue_reference"
        if style_id in ("bezier_red_gradient", "multi_start_bezier_red_gradient"):
            return "multi_start_route_red_reference" if draw_type == "multi_start_bezier" else "route_red_reference"
    if route_type == "branch" or draw_type == "branch_bezier":
        if style_id == "branch_bezier_red_gradient":
            return "branch_route_red_reference"
        if style_id == "branch_bezier_blue_gradient":
            return "branch_route_blue_reference"
    if style_id == "bezier_red_gradient":
        return "route_red_reference"
    if style_id == "bezier_blue_gradient":
        return "route_blue_reference"
    return style_id

# 移除重复或过近的控制点，避免后续绘制时出现异常或性能问题（例如贝塞尔曲线对重复点敏感会抖动）
def _remove_repeated_control_coords(points: List[List[float]], eps: float = 1e-9) -> List[List[float]]:
    cleaned: List[List[float]] = []
    for point in points or []:
        if not point or len(point) < 2:
            continue
        coordinate = [float(point[0]), float(point[1])]
        if cleaned:
            dx = coordinate[0] - cleaned[-1][0]
            dy = coordinate[1] - cleaned[-1][1]
            if dx * dx + dy * dy <= eps * eps:
                continue
        cleaned.append(coordinate)
    return cleaned

# 沿着直线前进 → 取左侧垂直方向 → 在中间轻轻挪一下 → 生成新的控制点 → 变成手绘弧线
def _build_handdrawn_route_controls(points: List[List[float]], route_id: str, kind: str = "main") -> List[List[float]]:
    """Return route control points unchanged; smoothing is handled in the plugin."""
    return [[float(p[0]), float(p[1])] for p in (points or []) if p and len(p) >= 2]


def _build_route_payloads_from_human_tables(dataset_dir: Path, plugin_dir: str, strict: bool = True) -> List[Dict[str, Any]]:
    # 人类可维护路线数据的三张核心表：地点字典、路线配置、路径明细
    places_csv = dataset_dir / "places.csv"
    routes_csv = dataset_dir / "routes.csv"
    route_paths_csv = dataset_dir / "route_paths.csv"

    # 三张表缺一不可，先做文件存在性检查
    for p in [places_csv, routes_csv, route_paths_csv]:
        if not p.exists():
            raise ValueError(f"缺少文件: {p}")

    # 读取 CSV 原始行，后续按列名兼容不同写法
    places_rows = _read_csv_rows(places_csv)
    routes_rows = _read_csv_rows(routes_csv)
    path_rows = _read_csv_rows(route_paths_csv)

    # 第一步：建立“地名 -> 坐标”字典
    # 后面所有路线都不直接写经纬度，而是通过地名查坐标
    place_xy: Dict[str, Tuple[float, float]] = {}
    for r in places_rows:
        name = str(_get_col_value(r, ["name", "place_name"], required=True)).strip()
        if not name:
            continue
        # 严格模式下不允许重名地名，避免歧义
        if name in place_xy and strict:
            raise ValueError(f"places.csv 存在重名地点: {name}")
        x = float(_get_col_value(r, ["x", "lon", "lng"], required=True))
        y = float(_get_col_value(r, ["y", "lat"], required=True))
        place_xy[name] = (x, y)

    # 第二步：把 route_paths.csv 按 route_id 分组
    # 一个 route_id 对应一组主干/分叉/起点路径
    path_by_route: Dict[str, List[Dict[str, Any]]] = {}
    for r in path_rows:
        rid = str(_get_col_value(r, ["route_id"], required=True)).strip()
        path_by_route.setdefault(rid, []).append(r)

    payloads: List[Dict[str, Any]] = []
    branch_network_groups: Dict[Tuple[str, str], Dict[str, Any]] = {}
    # 第三步：遍历 routes.csv 中启用的路线配置
    for rr in routes_rows:
        enabled = _to_bool(_get_col_value(rr, ["enabled"], default="1"))
        if not enabled:
            continue

        # 读取路线级配置
        rid = str(_get_col_value(rr, ["route_id"], required=True)).strip()
        draw_type = str(_get_col_value(rr, ["draw_type", "type"], required=True)).strip()
        route_type = str(_get_col_value(rr, ["route_type"], default="")).strip()
        style_id = str(_get_col_value(rr, ["style_id"], required=True)).strip()
        style_id = _route_reference_style_id(style_id, draw_type, route_type)
        input_crs = str(_get_col_value(rr, ["input_crs"], default="EPSG:4326")).strip() or "EPSG:4326"

        width_scale_raw = _get_col_value(rr, ["width_scale"], default="")
        width_scale = None
        if str(width_scale_raw).strip() != "":
            width_scale = float(width_scale_raw)

        # 从 route_paths.csv 中取出该 route 的所有路径行
        rows = path_by_route.get(rid, [])
        if not rows:
            if strict:
                raise ValueError(f"route_paths.csv 中未找到 route_id={rid}")
            continue

        # 工具函数：按 seq 排序并筛选某类路径行
        def _sorted_rows(filter_fn):
            tmp = [x for x in rows if filter_fn(x)]
            tmp.sort(key=lambda z: int(float(_get_col_value(z, ["seq"], default=0) or 0)))
            return tmp

        # 主干路径是所有路线的基础，至少要有 2 个点
        main_rows = _sorted_rows(lambda x: str(_get_col_value(x, ["path_type"], default="main")).strip() == "main")
        if len(main_rows) < 2:
            if strict:
                raise ValueError(f"route_id={rid} 的 main 段点数不足2")
            continue

        # 把主干上的地名转换成坐标序列
        main_points: List[List[float]] = []
        for mr in main_rows:
            pname = str(_get_col_value(mr, ["place_name", "name"], required=True)).strip()
            if pname not in place_xy:
                if strict:
                    raise ValueError(f"route_id={rid} main 地点不存在: {pname}")
                continue
            x, y = place_xy[pname]
            main_points.append([x, y])

        # 只把真实经过点交给插件。路线尽量保持原样，避免主线被揉成波浪。
        route_kind = _route_style_kind(draw_type, route_type)
        smooth_points = _remove_repeated_control_coords(main_points)

        # 统一构建 options，后面不同 draw_type 会往里补充参数
        options: Dict[str, Any] = {}
        # Text belongs to an individual path, including each incoming branch.
        # Old tables without label_text retain a route-name label on the main only.
        enemy_route = route_type == "enemy" or "side=enemy" in str(rr.get("notes", ""))
        path_labels: List[Dict[str, Any]] = []

        def _path_label(path_rows, role, index=0):
            if not enemy_route or not path_rows:
                return
            first = path_rows[0]
            value = str(first.get("label_text") or "").strip()
            if "label_text" not in first and role == "main":
                value = str(rr.get("route_name") or "").strip()
            if value:
                path_labels.append({"path_id": str(first.get("path_id") or role),
                                    "role": role, "index": index, "text": value})

        _path_label(main_rows, "main")
        if width_scale is not None:
            options["width_scale"] = width_scale
        width_group = str(_get_col_value(rr, ["width_group"], default="")).strip()
        if width_group:
            options["width_group"] = width_group
        target_scale_raw = _get_col_value(rr, ["target_scale_denominator", "target_scale", "scale_denominator"], default="")
        if str(target_scale_raw).strip() != "":
            options["target_scale_denominator"] = float(target_scale_raw)

        # 分叉贝塞尔：需要 attach_point + branch_points
        if draw_type == "branch_bezier":
            branch_rows = _sorted_rows(lambda x: str(_get_col_value(x, ["path_type"], default="")).strip() == "branch")
            if len(branch_rows) < 1:
                if strict:
                    raise ValueError(f"route_id={rid} 缺少 branch 段")
                continue
            attach_place = str(_get_col_value(branch_rows[0], ["attach_place"], required=True)).strip()
            if attach_place not in place_xy:
                if strict:
                    raise ValueError(f"route_id={rid} attach_place 不存在: {attach_place}")
                continue
            # 分叉点（从主干上分出去的锚点）
            options["attach_point"] = list(place_xy[attach_place])
            # 分叉路径点序列
            bp_raw: List[List[float]] = []
            for br in branch_rows:
                pname = str(_get_col_value(br, ["place_name", "name"], required=True)).strip()
                if pname not in place_xy:
                    if strict:
                        raise ValueError(f"route_id={rid} branch 地点不存在: {pname}")
                    continue
                bp_raw.append(list(place_xy[pname]))
            if len(bp_raw) < 1 and strict:
                raise ValueError(f"route_id={rid} branch_points 为空")
            options["branch_points"] = _remove_repeated_control_coords(bp_raw)
            _path_label(branch_rows, "branch")
        # 多起点贝塞尔：需要 starts=[{attach_point,start_points}, ...]
        elif draw_type == "multi_start_bezier":
            start_rows = [x for x in rows if str(_get_col_value(x, ["path_type"], default="")).strip() == "start"]
            start_groups: Dict[str, List[Dict[str, Any]]] = {}
            for sr in start_rows:
                pid = str(_get_col_value(sr, ["path_id"], default="start")).strip()
                start_groups.setdefault(pid, []).append(sr)
            starts: List[Dict[str, Any]] = []
            main_tangent_cache = _nearest_seg_tangent(smooth_points, smooth_points[len(smooth_points) // 2]) if len(smooth_points) >= 2 else [1.0, 0.0]
            for pid, grp in start_groups.items():
                grp.sort(key=lambda z: int(float(_get_col_value(z, ["seq"], default=0) or 0)))
                attach_place = str(_get_col_value(grp[0], ["attach_place"], required=True)).strip()
                if attach_place not in place_xy:
                    if strict:
                        raise ValueError(f"route_id={rid} start={pid} attach_place 不存在: {attach_place}")
                    continue
                attach_pt = list(place_xy[attach_place])
                sp_raw: List[List[float]] = []
                for g in grp:
                    pname = str(_get_col_value(g, ["place_name", "name"], required=True)).strip()
                    if pname not in place_xy:
                        if strict:
                            raise ValueError(f"route_id={rid} start={pid} 地点不存在: {pname}")
                        continue
                    sp_raw.append(list(place_xy[pname]))
                if len(sp_raw) < 1:
                    if strict:
                        raise ValueError(f"route_id={rid} start={pid} start_points 为空")
                    continue
                mt = _nearest_seg_tangent(smooth_points, attach_pt) if len(smooth_points) >= 2 else main_tangent_cache
                start_seed = sp_raw + [attach_pt]
                sp = start_seed[:-1]
                sp = _remove_repeated_control_coords(sp)
                starts.append({"attach_point": attach_pt, "start_points": sp})
                _path_label(grp, "start", len(starts) - 1)
            if len(starts) < 1 and strict:
                raise ValueError(f"route_id={rid} 缺少 starts")
            options["starts"] = starts

        if path_labels:
            options["path_labels"] = path_labels

        # 最终组装成底层绘制工具需要的 payload
        payloads.append({
            "route_id": rid,
            "plugin_dir": plugin_dir,
            "type": draw_type,
            "style_id": style_id,
            "points": smooth_points,
            "input_crs": input_crs,
            "options": options,
        })

    for (_style_id, _input_crs), group in branch_network_groups.items():
        main_paths: List[List[List[float]]] = []
        edge_map: Dict[Tuple[Tuple[float, float], Tuple[float, float]], Dict[str, Any]] = {}
        connect_specs = group.get("main_attach_nodes", []) or []
        node_sequences: List[List[List[float]]] = []
        for spec in connect_specs:
            if not isinstance(spec, dict):
                continue
            nodes = spec.get("nodes") or []
            attach = spec.get("attach_point")
            if not isinstance(nodes, list) or len(nodes) < 2 or not attach:
                continue
            attach_idx = None
            for idx, node in enumerate(nodes):
                if math.hypot(float(node[0]) - float(attach[0]), float(node[1]) - float(attach[1])) <= 1e-8:
                    attach_idx = idx
                    break
            if attach_idx is None:
                continue
            if attach_idx > 0:
                node_sequences.append(nodes[:attach_idx + 1])
        if not node_sequences:
            node_sequences = [nodes for nodes in (group.get("main_nodes", []) or []) if isinstance(nodes, list)]

        for nodes in node_sequences:
            if not isinstance(nodes, list) or len(nodes) < 2:
                continue
            for idx in range(len(nodes) - 1):
                a = nodes[idx]
                b = nodes[idx + 1]
                edge_key = tuple(sorted((
                    (round(float(a[0]), 6), round(float(a[1]), 6)),
                    (round(float(b[0]), 6), round(float(b[1]), 6)),
                )))
                item = edge_map.setdefault(edge_key, {
                    "a": list(a),
                    "b": list(b),
                    "count": 0,
                    "dist": math.hypot(float(b[0]) - float(a[0]), float(b[1]) - float(a[1])),
                })
                item["count"] += 1
        parent: Dict[Tuple[float, float], Tuple[float, float]] = {}

        def _find_node(node: Tuple[float, float]) -> Tuple[float, float]:
            parent.setdefault(node, node)
            while parent[node] != node:
                parent[node] = parent[parent[node]]
                node = parent[node]
            return node

        def _union_node(a_node: Tuple[float, float], b_node: Tuple[float, float]) -> bool:
            ra = _find_node(a_node)
            rb = _find_node(b_node)
            if ra == rb:
                return False
            parent[rb] = ra
            return True

        for edge_key, item in sorted(edge_map.items(), key=lambda kv: (-int(kv[1]["count"]), float(kv[1]["dist"]))):
            if not _union_node(edge_key[0], edge_key[1]):
                continue
            main_paths.append(_build_handdrawn_route_controls(
                [item["a"], item["b"]],
                f"branch-network:{edge_key}",
                "branch",
            ))
        if not main_paths:
            main_paths = [p for p in group.get("main_paths", []) if isinstance(p, list) and len(p) >= 2]
        branches = [b for b in group.get("branches", []) if isinstance(b, dict)]
        if not main_paths or not branches:
            continue
        width_scales = group.get("width_scales", []) or []
        width_scale = max(width_scales) * 0.88 if width_scales else None
        options: Dict[str, Any] = {
            "main_paths": main_paths,
            "branches": branches,
        }
        if width_scale is not None:
            options["width_scale"] = width_scale
        route_ids = group.get("route_ids", [])
        payloads.append({
            "route_id": "BRANCH_NETWORK_" + "_".join(route_ids),
            "plugin_dir": plugin_dir,
            "type": "branch_network_bezier",
            "style_id": group["style_id"],
            "points": main_paths[0],
            "input_crs": group["input_crs"],
            "options": options,
        })

    return payloads

# 同一条路线的控制点生成要稳定随机但不完全固定，避免每次都一模一样又不至于完全一样。根据 route_id 生成一个稳定的随机种子，保证同一条路线的控制点在不同机器上、不同时间点生成时有一定的随机性但又不会完全一样。
def _stable_seed_from_text(text: str) -> int:
    s = str(text or "")
    h = hashlib.sha256(s.encode("utf-8")).hexdigest()
    return int(h[:16], 16)


# 在起点过来 25% 的位置打个点
# 在终点过来 25% 的位置打个点
# 把原来的尖角删掉，用这两个新点连起来
def _nearest_seg_tangent(points: List[List[float]], anchor: List[float]) -> List[float]:
    """计算主线在锚点附近的切线方向（单位向量）。"""
    if len(points) < 2:
        return [1.0, 0.0]

    ax, ay = float(anchor[0]), float(anchor[1])
    best_i = 0
    best_d2 = float("inf")
    for i in range(len(points) - 1):
        x1, y1 = float(points[i][0]), float(points[i][1])
        x2, y2 = float(points[i + 1][0]), float(points[i + 1][1])
        vx, vy = x2 - x1, y2 - y1
        vv = vx * vx + vy * vy
        if vv <= 1e-12:
            continue
        t = ((ax - x1) * vx + (ay - y1) * vy) / vv
        t = max(0.0, min(1.0, t))
        px = x1 + vx * t
        py = y1 + vy * t
        d2 = (ax - px) * (ax - px) + (ay - py) * (ay - py)
        if d2 < best_d2:
            best_d2 = d2
            best_i = i

    tx = float(points[best_i + 1][0]) - float(points[best_i][0])
    ty = float(points[best_i + 1][1]) - float(points[best_i][1])
    n = math.hypot(tx, ty)
    if n <= 1e-12:
        return [1.0, 0.0]
    return [tx / n, ty / n]

def _label_enemy_route_paths(route_items, scope):
    """Label the plugin's final curves, with source IDs retained for relayout."""
    project = QgsProject.instance()
    features, sources, warnings = [], {}, []
    for item in route_items:
        if not item.get("success"):
            continue
        source = project.mapLayer(item.get("layer_id", ""))
        if source is None:
            candidates = project.mapLayersByName(item.get("layer_name", ""))
            source = candidates[0] if candidates else None
        if source is None or source.fields().indexFromName("label_paths") < 0:
            continue  # Older drawing plugins have no final-curve label interface.
        feature = source.getFeature(int(item["feature_id"]))
        raw = feature["label_paths"] if feature.isValid() else None
        if not raw or str(raw) in ("NULL", "None"):
            continue
        for path_id, record in json.loads(str(raw)).items():
            if not record.get("text"):
                continue
            geometry = QgsGeometry.fromWkt(record.get("wkt") or "")
            if geometry.isNull() or geometry.isEmpty():
                warnings.append(f"{record['text']}：没有可独立标注的箭身，保留路线")
                continue
            features.append((source.id(), feature.id(), path_id, record, geometry))
            sources[source.id()] = source

    owned = [layer for layer in project.mapLayers().values()
             if layer.customProperty("autocarto/route_labels_scope", "") == scope]
    if not features:
        for layer in owned:
            if not layer.isEditable():
                project.removeMapLayer(layer.id())
        return {"count": 0, "warnings": warnings}
    layer = owned[0] if owned else QgsVectorLayer("LineString", "国军路线名称", "memory")
    if layer.isEditable():
        return {"count": 0, "warnings": ["路线名称图层正在编辑，已保留当前标注"]}
    if not owned:
        layer.setCustomProperty("autocarto/route_labels_scope", scope)
        layer.dataProvider().addAttributes([
            QgsField("source_layer", QVariant.String), QgsField("source_fid", QVariant.LongLong),
            QgsField("path_id", QVariant.String), QgsField("label_text", QVariant.String),
            QgsField("half_width_map", QVariant.Double),
            QgsField("label_length_m", QVariant.Double),
        ])
        layer.updateFields()
        project.addMapLayer(layer)
    layer.setCrs(next(iter(sources.values())).crs())
    provider = layer.dataProvider()
    if layer.fields().indexFromName("label_length_m") < 0:
        provider.addAttributes([QgsField("label_length_m", QVariant.Double)])
        layer.updateFields()
    provider.deleteFeatures([feature.id() for feature in layer.getFeatures()])
    rows = []
    for source_id, fid, path_id, record, geometry in features:
        row = QgsFeature(layer.fields())
        row.setGeometry(geometry)
        row.setAttributes([source_id, fid, path_id, record["text"], record.get("half_width_map", 0.),
                           record.get("label_length_m", 0.)])
        rows.append(row)
    ok, _ = provider.addFeatures(rows)
    if not ok:
        raise RuntimeError("路线名称图层写入失败")
    layer.setRenderer(QgsSingleSymbolRenderer(QgsLineSymbol.createSimple({"line_style": "no"})))
    # Keep real line geometry so map extent filtering and layout exports work.
    # The backup plugin updates these source-linked rows when it moves a route.
    settings = QgsPalLayerSettings()
    settings.fieldName = "label_text"
    settings.placement = Qgis.LabelPlacement.Curved
    settings.priority = 7
    settings.distUnits = QgsUnitTypes.RenderMapUnits
    properties = settings.dataDefinedProperties()
    properties.setProperty(QgsPalLayerSettings.LabelDistance,
                           QgsProperty.fromExpression('"half_width_map" * 1.3'))
    settings.setDataDefinedProperties(properties)
    line_settings = settings.lineSettings()
    line_settings.setPlacementFlags(Qgis.LabelLinePlacementFlag.AboveLine |
                                   Qgis.LabelLinePlacementFlag.BelowLine |
                                   Qgis.LabelLinePlacementFlag.MapOrientation)
    line_settings.setOverrunDistance(2.0)
    line_settings.setOverrunDistanceUnit(QgsUnitTypes.RenderMillimeters)
    settings.setLineSettings(line_settings)
    text_format = QgsTextFormat()
    font = QFont(); font.setFamilies(["Microsoft YaHei", "Noto Sans CJK SC", "Hiragino Sans GB"])
    text_format.setFont(font); text_format.setSize(2.2)
    text_format.setSizeUnit(QgsUnitTypes.RenderMillimeters)
    text_format.setColor(QColor("#343d76"))
    buffer = QgsTextBufferSettings(); buffer.setEnabled(True); buffer.setSize(0.4)
    buffer.setSizeUnit(QgsUnitTypes.RenderMillimeters); buffer.setColor(QColor("white"))
    text_format.setBuffer(buffer); settings.setFormat(text_format)
    # At small scales a long name cannot fit on a short curve. Place it beside
    # the same curve's midpoint instead of silently losing the name.
    short = ('coalesce("label_length_m" * 1000 / nullif(@map_scale, 0) '
             '< length("label_text") * 2.2, false)')
    root = QgsRuleBasedLabeling.Rule(QgsPalLayerSettings())
    curved = QgsRuleBasedLabeling.Rule(settings)
    curved.setFilterExpression(f"NOT ({short})"); root.appendChild(curved)
    compact = QgsPalLayerSettings(settings)
    compact.geometryGeneratorEnabled = True
    compact.geometryGeneratorType = QgsWkbTypes.PointGeometry
    compact.geometryGenerator = 'line_interpolate_point($geometry, length($geometry) / 2)'
    compact.placement = Qgis.LabelPlacement.OrderedPositionsAroundPoint
    compact.wrapChar = "、"
    adjacent = QgsRuleBasedLabeling.Rule(compact)
    adjacent.setFilterExpression(short); root.appendChild(adjacent)
    layer.setLabeling(QgsRuleBasedLabeling(root)); layer.setLabelsEnabled(True)
    layer.setDependencies({QgsMapLayerDependency(source_id) for source_id in sources})
    layer.updateExtents(); layer.triggerRepaint()
    return {"count": len(rows), "layer_id": layer.id(), "warnings": warnings}


def _label_meeting_units(point_layer):
    """Keep place-name labeling and add participant names only for real rendezvous."""
    if point_layer.fields().indexFromName("meeting_label") < 0:
        return {"count": 0}
    count = sum(bool(str(feature["meeting_label"]).strip()) and
                str(feature["meeting_label"]) not in ("NULL", "None")
                for feature in point_layer.getFeatures())
    if not count:
        return {"count": 0}
    existing = point_layer.labeling()
    if isinstance(existing, QgsRuleBasedLabeling):
        root = existing.rootRule().clone()
        for index in range(len(root.children()) - 1, -1, -1):
            if root.children()[index].description() == "会师军队名称":
                root.removeChildAt(index)
    else:
        root = QgsRuleBasedLabeling.Rule(QgsPalLayerSettings())
        if existing and point_layer.labelsEnabled():
            root.appendChild(QgsRuleBasedLabeling.Rule(existing.settings()))
    settings = QgsPalLayerSettings(); settings.fieldName = "meeting_label"
    settings.placement = Qgis.LabelPlacement.OrderedPositionsAroundPoint
    settings.dist = 4.0; settings.distUnits = QgsUnitTypes.RenderMillimeters
    settings.priority = 9; settings.wrapChar = "\n"
    text_format = QgsTextFormat()
    font = QFont(); font.setFamilies(["Microsoft YaHei", "Noto Sans CJK SC", "Hiragino Sans GB"])
    text_format.setFont(font); text_format.setSize(2.3)
    text_format.setSizeUnit(QgsUnitTypes.RenderMillimeters); text_format.setColor(QColor("#9c2929"))
    buffer = QgsTextBufferSettings(); buffer.setEnabled(True); buffer.setSize(0.5)
    buffer.setSizeUnit(QgsUnitTypes.RenderMillimeters); buffer.setColor(QColor("white"))
    text_format.setBuffer(buffer); settings.setFormat(text_format)
    rule = QgsRuleBasedLabeling.Rule(settings)
    rule.setDescription("会师军队名称")
    rule.setFilterExpression('trim(coalesce("meeting_label", \'\')) <> \'\'')
    root.appendChild(rule)
    point_layer.setLabeling(QgsRuleBasedLabeling(root)); point_layer.setLabelsEnabled(True)
    point_layer.triggerRepaint()
    return {"count": count}


def cartography_build_and_draw_from_tables_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    """单入口：先准备专题点图层，再执行 路线/点符号化/图文注记。"""
    try:
        from qgis.core import QgsFeature, QgsGeometry, QgsVectorLayer, QgsField
        from qgis.PyQt.QtCore import QVariant

        try:
            resolved_dataset = resolve_marching_route_dataset(params)
        except DatasetHandoffError as exc:
            error_payload = exc.to_dict()
            return {
                "success": False,
                "output": "",
                "info": str(exc),
                "message": str(exc),
                "error_type": exc.code,
                "error": json.dumps(error_payload, ensure_ascii=False),
                "errors": [
                    {
                        "stage": "dataset_handoff",
                        "message": str(exc),
                        "error_type": exc.code,
                        "detail": "",
                        "debug": exc.details,
                    }
                ],
                "failed_stages": ["dataset_handoff"],
                "data": {"dataset": error_payload},
            }

        dataset_dir = resolved_dataset.dataset_dir
        plugin_dir = str(params.get("plugin_dir") or "").strip()
        if not plugin_dir:
            return {"success": False, "output": "", "info": "错误：缺少 plugin_dir 参数"}

        strict = _to_bool(params.get("strict", True))
        run_routes = _to_bool(params.get("run_routes", True))
        run_symbols = _to_bool(params.get("run_symbols", True))
        run_callouts = _to_bool(params.get("run_callouts", True))

        points_layer_name = str(params.get("points_layer") or params.get("layer") or "points").strip() or "points"
        overwrite_points_layer = _to_bool(params.get("overwrite_points_layer", True))
        points_input_crs = str(params.get("input_crs", "EPSG:4326") or "EPSG:4326").strip() or "EPSG:4326"

        results: Dict[str, Any] = {
            "dataset": resolved_dataset.to_dict(),
            "points_layer": {"success": True, "executed": False, "layer_name": points_layer_name, "created_count": 0, "skipped_count": 0, "warnings": []},
            "routes": {"success": True, "count": 0, "items": [], "failed_count": 0},
            "symbols": {"success": True, "executed": False},
            "callouts": {"success": True, "executed": False},
        }
        errors: List[Dict[str, Any]] = []

        def _append_error(stage: str, message: str, error_type: str = "stage_failed", detail: str = "", debug: Optional[Dict[str, Any]] = None):
            errors.append({
                "stage": stage,
                "message": str(message),
                "error_type": str(error_type or "stage_failed"),
                "detail": str(detail or ""),
                "debug": debug or {},
            })

        # 0) 专题点图层准备：优先以 dataset_dir/points.csv 为准
        points_csv = dataset_dir / "points.csv"
        if points_csv.exists():
            point_rows = _read_csv_rows(points_csv)

            # 最小必需列检查
            required_cols = ["name", "x", "y", "symbol_type", "priority"]
            if point_rows:
                sample = point_rows[0]
                missing = [c for c in required_cols if _find_col(sample, [c]) is None]
                if missing:
                    raise ValueError(f"points.csv 缺少必需列: {', '.join(missing)}")

            proj = QgsProject.instance()
            existing = proj.mapLayersByName(points_layer_name)
            if existing and overwrite_points_layer:
                for lyr in existing:
                    proj.removeMapLayer(lyr.id())
            if existing and (not overwrite_points_layer):
                results["points_layer"].update({
                    "success": True,
                    "executed": True,
                    "layer_name": points_layer_name,
                    "created_count": 0,
                    "skipped_count": 0,
                    "warnings": [f"图层 {points_layer_name} 已存在且 overwrite_points_layer=False，复用现有图层"],
                })
            else:
                vlayer = QgsVectorLayer(f"Point?crs={points_input_crs}", points_layer_name, "memory")
                provider = vlayer.dataProvider()

                # 保留 points.csv 原字段
                all_field_names: List[str] = []
                for r in point_rows:
                    for k in (r or {}).keys():
                        kn = str(k or "").strip()
                        if kn and kn not in all_field_names:
                            all_field_names.append(kn)

                qfields = [QgsField(fn, QVariant.String) for fn in all_field_names]
                provider.addAttributes(qfields)
                vlayer.updateFields()

                created_count = 0
                skipped_count = 0
                feats: List[QgsFeature] = []
                for row in point_rows:
                    try:
                        x = float(_get_col_value(row, ["x"], required=True))
                        y = float(_get_col_value(row, ["y"], required=True))
                    except Exception:
                        skipped_count += 1
                        continue

                    feat = QgsFeature(vlayer.fields())
                    feat.setGeometry(QgsGeometry.fromPointXY(QgsPointXY(x, y)))
                    attrs = []
                    for fn in all_field_names:
                        attrs.append(row.get(fn, ""))
                    feat.setAttributes(attrs)
                    feats.append(feat)
                    created_count += 1

                if feats:
                    provider.addFeatures(feats)
                vlayer.updateExtents()
                proj.addMapLayer(vlayer)

                results["points_layer"].update({
                    "success": True,
                    "executed": True,
                    "layer_name": points_layer_name,
                    "created_count": created_count,
                    "skipped_count": skipped_count,
                    "warnings": [],
                })
        else:
            warn = f"未找到 points.csv: {points_csv}"
            warnings = [warn]
            if run_symbols:
                warnings.append("run_symbols=True 但缺少 points.csv，符号化将失败")
            if run_routes:
                warnings.append("run_routes=True：路线仍基于 places/routes/route_paths 生成，但缺少专题点图层参照")
            results["points_layer"].update({
                "success": True,
                "executed": False,
                "warnings": warnings,
            })

        # A) 路线绘制
        if run_routes:
            try:
                route_payloads = _build_route_payloads_from_human_tables(dataset_dir, plugin_dir, strict=strict)
                route_responses = _draw_bezier_route_batch(route_payloads)
                for p, res in zip(route_payloads, route_responses):
                    item = {
                        "route_id": p.get("route_id"),
                        "success": bool((res or {}).get("success")),
                        "info": (res or {}).get("info", ""),
                        "feature_id": (res or {}).get("output", ""),
                        "layer_name": ((res or {}).get("data", {}) or {}).get("layer_name", ""),
                        "layer_id": ((res or {}).get("data", {}) or {}).get("layer_id", ""),
                        "payload": {"type": p.get("type"), "style_id": p.get("style_id")},
                    }
                    if not item["success"]:
                        _append_error(
                            stage="routes",
                            message=item.get("info", "路线绘制失败"),
                            error_type=str((res or {}).get("error_type") or "route_draw_failed"),
                            detail=str((res or {}).get("detail") or ""),
                            debug={
                                "route_id": p.get("route_id"),
                                "payload": item.get("payload", {}),
                                "response": res or {},
                            },
                        )
                    results["routes"]["items"].append(item)
                results["routes"]["count"] = len(results["routes"]["items"])
                results["routes"]["failed_count"] = sum(1 for x in results["routes"]["items"] if not x.get("success"))
                results["routes"]["success"] = results["routes"]["failed_count"] == 0
            except Exception as e:
                import traceback

                tb = traceback.format_exc()
                results["routes"] = {
                    "success": False,
                    "count": len(results.get("routes", {}).get("items", [])),
                    "items": results.get("routes", {}).get("items", []),
                    "failed_count": max(1, len(results.get("routes", {}).get("items", []))),
                    "info": f"路线阶段失败: {str(e)}",
                }
                _append_error(
                    stage="routes",
                    message=f"路线阶段失败: {str(e)}",
                    error_type="routes_stage_failed",
                    detail=tb,
                    debug={"exception": repr(e)},
                )

        if run_routes:
            try:
                results["route_labels"] = _label_enemy_route_paths(results["routes"]["items"], str(dataset_dir))
            except Exception as exc:
                results["route_labels"] = {"count": 0, "warnings": [f"路线名称未完成：{exc}"]}

        # B) 点符号化
        if run_symbols:
            try:
                symbols_csv = dataset_dir / "symbol_mapping.csv"
                if not points_csv.exists() or not symbols_csv.exists():
                    msg = f"缺少点符号化所需文件: {points_csv} 或 {symbols_csv}"
                    results["symbols"] = {
                        "success": False,
                        "executed": False,
                        "info": msg,
                    }
                    _append_error(
                        stage="symbols",
                        message=msg,
                        error_type="symbols_required_files_missing",
                        debug={"points_csv": str(points_csv), "symbols_csv": str(symbols_csv)},
                    )
                else:
                    point_rows = _read_csv_rows(points_csv)
                    symbol_rows_raw = _read_csv_rows(symbols_csv)
                    symbol_rows: List[Dict[str, Any]] = []
                    for srow in symbol_rows_raw:
                        s2 = dict(srow or {})
                        svg_path_raw = str(s2.get("svg_path", "") or "").strip()
                        if svg_path_raw and (not Path(svg_path_raw).is_absolute()):
                            s2["svg_path"] = str((dataset_dir / svg_path_raw).resolve())
                        symbol_rows.append(s2)

                    spayload = {
                        "layer": points_layer_name,
                        "point_rows": point_rows,
                        "symbol_rows": symbol_rows,
                        "name_field": "name",
                        "symbol_type_field": str(params.get("symbol_type_field", "symbol_type") or "symbol_type"),
                        "default_symbol_types": params.get("default_symbol_types") or [],
                        "strict": strict,
                        "hide_unmatched": _to_bool(params.get("hide_unmatched", True)),
                        "fallback_size": float(params.get("fallback_size", 4) or 4),
                    }
                    sres = symbolize_points_by_mapping_handler(spayload)
                    results["symbols"] = {
                        "success": bool((sres or {}).get("success")),
                        "executed": True,
                        "info": (sres or {}).get("info", ""),
                        "output": (sres or {}).get("output", ""),
                        "data": (sres or {}).get("data") or {},
                    }
                    if bool((sres or {}).get("success")) and any(
                        str(row.get("symbol_type") or "") == "waypoint"
                        for row in point_rows
                    ):
                        point_layers = QgsProject.instance().mapLayersByName(points_layer_name)
                        if point_layers:
                            point_layer = point_layers[0]
                            label_settings = QgsPalLayerSettings()
                            label_settings.fieldName = "name"
                            label_settings.priority = 10
                            label_format = QgsTextFormat()
                            label_font = QFont()
                            label_font.setFamilies(["Microsoft YaHei", "Noto Sans CJK SC", "Hiragino Sans GB"])
                            label_format.setFont(label_font)
                            label_format.setSize(2.0)
                            label_format.setSizeUnit(QgsUnitTypes.RenderMillimeters)
                            label_format.setColor(QColor("#202020"))
                            label_buffer = QgsTextBufferSettings()
                            label_buffer.setEnabled(True)
                            label_buffer.setSize(0.5)
                            label_buffer.setSizeUnit(QgsUnitTypes.RenderMillimeters)
                            label_buffer.setColor(QColor("#ffffff"))
                            label_format.setBuffer(label_buffer)
                            label_settings.setFormat(label_format)
                            point_layer.setLabeling(QgsVectorLayerSimpleLabeling(label_settings))
                            point_layer.setLabelsEnabled(True)

                            # 路线插件可能在点层之后新建箭头面层，须让小圆点压在箭头上。
                            root = QgsProject.instance().layerTreeRoot()
                            node = root.findLayer(point_layer.id())
                            if node is not None:
                                node.setItemVisibilityChecked(True)
                                if node.parent() == root and root.children()[0] != node:
                                    root.insertChildNode(0, node.clone())
                                    root.removeChildNode(node)
                                if root.hasCustomLayerOrder():
                                    layer_order = list(root.customLayerOrder() or [])
                                    root.setCustomLayerOrder(
                                        [point_layer]
                                        + [layer for layer in layer_order if layer.id() != point_layer.id()]
                                    )
                            point_layer.triggerRepaint()
                            results["symbols"]["data"]["route_point_labels_enabled"] = True
                    if bool((sres or {}).get("success")):
                        point_layers = QgsProject.instance().mapLayersByName(points_layer_name)
                        if point_layers:
                            try:
                                results["meeting_labels"] = _label_meeting_units(point_layers[0])
                            except Exception as exc:
                                results["meeting_labels"] = {"count": 0, "warnings": [f"会师名称未完成：{exc}"]}
                    if not bool((sres or {}).get("success")):
                        _append_error(
                            stage="symbols",
                            message=str((sres or {}).get("info") or "点符号化失败"),
                            error_type=str((sres or {}).get("error_type") or "symbolize_points_failed"),
                            detail=str((sres or {}).get("detail") or ""),
                            debug={"payload": spayload, "response": sres or {}},
                        )
            except Exception as e:
                import traceback

                tb = traceback.format_exc()
                results["symbols"] = {
                    "success": False,
                    "executed": True,
                    "info": f"点符号化阶段失败: {str(e)}",
                }
                _append_error(
                    stage="symbols",
                    message=f"点符号化阶段失败: {str(e)}",
                    error_type="symbols_stage_failed",
                    detail=tb,
                    debug={"exception": repr(e)},
                )

        # C) 图文注记
        if run_callouts:
            try:
                callouts_csv = dataset_dir / "callouts.csv"
                if not callouts_csv.exists():
                    msg = f"缺少注记文件: {callouts_csv}"
                    results["callouts"] = {
                        "success": False,
                        "executed": False,
                        "info": msg,
                    }
                    _append_error(
                        stage="callouts",
                        message=msg,
                        error_type="callouts_required_file_missing",
                        debug={"callouts_csv": str(callouts_csv)},
                    )
                else:
                    raw_rows = _read_csv_rows(callouts_csv)
                    callouts: List[Dict[str, Any]] = []
                    for row in raw_rows:
                        image_path_raw = str(_get_col_value(row, ["image_path"], default="") or "").strip()
                        image_path_resolved = image_path_raw
                        if image_path_raw and (not Path(image_path_raw).is_absolute()):
                            image_path_resolved = str((dataset_dir / image_path_raw).resolve())

                        callouts.append({
                            "id": str(_get_col_value(row, ["id", "callout_id"], default="")).strip() or None,
                            "anchor": {
                                "x": float(_get_col_value(row, ["anchor_x", "x"], required=True)),
                                "y": float(_get_col_value(row, ["anchor_y", "y"], required=True)),
                            },
                            "body": str(_get_col_value(row, ["body", "desc", "description"], default="") or "").strip(),
                            "priority": float(_get_col_value(row, ["priority"], default=0) or 0),
                            "template": str(_get_col_value(row, ["template"], default="image_top_text_bottom") or "image_top_text_bottom").strip(),
                            "box": {
                                "width_mm": float(_get_col_value(row, ["width_mm"], default=42) or 42),
                            },
                            "position": {
                                "preferred_side": str(_get_col_value(row, ["preferred_side"], default="right_top") or "right_top").strip(),
                                "offset_mm": [
                                    float(_get_col_value(row, ["offset_x_mm"], default=18) or 18),
                                    float(_get_col_value(row, ["offset_y_mm"], default=-12) or -12),
                                ],
                            },
                            "leader_line": {
                                "style": str(_get_col_value(row, ["leader_style"], default="double") or "double").strip(),
                            },
                            "image": {
                                "enabled": _to_bool(_get_col_value(row, ["image_enabled"], default=False)),
                                "path": image_path_resolved,
                            },
                        })

                    cpayload: Dict[str, Any] = {
                        "input_crs": str(params.get("input_crs", "EPSG:4326") or "EPSG:4326"),
                        "callouts": callouts,
                    }
                    if params.get("layout_name"):
                        cpayload["layout_name"] = params.get("layout_name")
                    if params.get("map_item_id"):
                        cpayload["map_item_id"] = params.get("map_item_id")

                    cres = cartography_add_callouts_handler(cpayload)
                    results["callouts"] = {
                        "success": bool((cres or {}).get("success")),
                        "executed": True,
                        "info": (cres or {}).get("info", ""),
                        "created_count": (cres or {}).get("created_count", 0),
                        "failed_count": (cres or {}).get("failed_count", 0),
                        "items": (cres or {}).get("items", []),
                        "debug": {
                            "layout_name": cpayload.get("layout_name", ""),
                            "map_item_id": cpayload.get("map_item_id", ""),
                            "input_crs": cpayload.get("input_crs", "EPSG:4326"),
                            "callouts_count": len(callouts),
                            "sample_anchor": (callouts[0].get("anchor") if callouts else {}),
                            "sample_image_path": ((callouts[0].get("image") or {}).get("path", "") if callouts else ""),
                        },
                    }
                    if not bool((cres or {}).get("success")):
                        _append_error(
                            stage="callouts",
                            message=str((cres or {}).get("info") or "图文注记失败"),
                            error_type=str((cres or {}).get("error_type") or "callouts_failed"),
                            detail=str((cres or {}).get("detail") or ""),
                            debug={"payload": cpayload, "response": cres or {}},
                        )
            except Exception as e:
                import traceback

                tb = traceback.format_exc()
                results["callouts"] = {
                    "success": False,
                    "executed": True,
                    "info": f"图文注记阶段失败: {str(e)}",
                }
                _append_error(
                    stage="callouts",
                    message=f"图文注记阶段失败: {str(e)}",
                    error_type="callouts_stage_failed",
                    detail=tb,
                    debug={"exception": repr(e)},
                )

        overall_success = bool(results["points_layer"].get("success", True)) and bool(results["routes"].get("success", True)) and bool(results["symbols"].get("success", True)) and bool(results["callouts"].get("success", True))

        if not bool(results["points_layer"].get("success", True)):
            _append_error(
                stage="points_layer",
                message=str(results["points_layer"].get("info") or "专题点图层阶段失败"),
                error_type="points_layer_failed",
                debug={"result": results["points_layer"]},
            )

        if errors:
            stages = sorted(set(str(e.get("stage") or "") for e in errors if e.get("stage")))
            fail_summary = "；".join([f"{i+1}. [{err.get('stage')}] {err.get('message')}" for i, err in enumerate(errors)])
            info_text = f"执行完成（含失败项）：{fail_summary}"
        else:
            stages = []
            info_text = "执行完成"

        label_warnings = [message for key in ("route_labels", "meeting_labels")
                          for message in results.get(key, {}).get("warnings", [])]
        if label_warnings:
            info_text += "；标注提示：" + "；".join(label_warnings)

        return {
            "success": overall_success,
            "output": "",
            "info": info_text,
            "error_type": "partial_failed" if (not overall_success and errors) else None,
            "message": info_text,
            "failed_stages": stages,
            "errors": errors,
            "data": results,
        }
    except Exception as e:
        import traceback
        detail = traceback.format_exc()
        return {
            "success": False,
            "output": "",
            "info": f"从人类表构建执行失败: {str(e)}",
            "error_type": "build_and_draw_failed",
            "message": str(e),
            "detail": detail,
            "debug": {"traceback": detail},
        }


def _cartography_add_callouts_impl(params: Dict[str, Any]) -> Dict[str, Any]:
    """cartography_add_callouts 的集中实现。"""
    defaults = {
        "box": {
            "shape": "rectangle",
            "fill_color": "#fff7e6",
            "fill_opacity": 0.94,
            "border_color": "#d9822b",
            "border_width_mm": 0.3,
            "padding_mm": 1.8,
            "corner_radius_mm": 1.2,
            "width_mm": 42.0,
            "height_mm": "auto",
        },
        "text": {
            "body_font_family": "SimSun",
            "body_font_size_pt": 5.5,
            "line_spacing": 1.15,
            "body_color": "#333333",
        },
        "image": {
            "enabled": False,
            "height_mm": 18.0,
            "gap_mm": 1.5,
            "path": "",
        },
        "leader": {
            "enabled": True,
            "style": "double",
            "color": "#d9822b",
            "width_mm": 0.25,
            "anchor_marker": "dot",
            "anchor_marker_size_mm": 1.0,
        },
        "position": {
            "mode": "auto",
            "preferred_side": "right_top",
            "offset_mm": [18.0, -12.0],
            "allow_sides": ["right_top", "right", "right_bottom", "left_top", "left", "left_bottom", "top", "bottom"],
        },
    }

    from qgis.utils import iface
    from qgis.PyQt.QtGui import QFont, QFontMetricsF, QImageReader, QPolygonF, QPen
    from qgis.PyQt.QtCore import QPointF
    from qgis.core import QgsProject

    if iface is None:
        message = "当前不在 QGIS 界面上下文（iface为空）"
        return {
            "success": False,
            "info": message,
            "error_type": "layout_not_found",
            "message": message,
            "warnings": [],
        }

    project = QgsProject.instance()
    lm = project.layoutManager()
    layout_name = str(params.get("layout_name", "") or "").strip()
    layout = lm.layoutByName(layout_name) if layout_name else lm.layoutByName(lm.layouts()[0].name()) if lm.layouts() else None
    if layout is None:
        message = "未找到可用布局"
        return {
            "success": False,
            "info": message,
            "error_type": "layout_not_found",
            "message": message,
            "warnings": [],
        }

    map_item_id = str(params.get("map_item_id", "") or "").strip()
    map_item = None
    for item in layout.items():
        if isinstance(item, QgsLayoutItemMap):
            if (not map_item_id) or item.id() == map_item_id:
                map_item = item
                break
    if map_item is None:
        message = "未找到布局地图对象"
        return {
            "success": False,
            "info": message,
            "error_type": "map_item_not_found",
            "message": message,
            "warnings": [],
        }

    input_crs_authid = str(params.get("input_crs", "EPSG:4326") or "EPSG:4326").strip()
    src_crs = QgsCoordinateReferenceSystem(input_crs_authid)
    if not src_crs.isValid():
        message = f"input_crs 非法: {input_crs_authid}"
        return {
            "success": False,
            "info": message,
            "error_type": "invalid_anchor",
            "message": message,
            "warnings": [],
        }

    map_crs = None
    try:
        if hasattr(map_item, "crs"):
            map_crs = map_item.crs()
    except Exception:
        map_crs = None
    if map_crs is None or not map_crs.isValid():
        map_crs = project.crs()
    xform = QgsCoordinateTransform(src_crs, map_crs, project) if src_crs.authid() != map_crs.authid() else None

    callouts = params.get("callouts") or []
    if not isinstance(callouts, list) or not callouts:
        message = "callouts 必须为非空数组"
        return {
            "success": False,
            "info": message,
            "error_type": "invalid_params",
            "message": message,
            "warnings": [],
            "debug": {"field": "callouts", "received_type": type(callouts).__name__},
        }

    # 地图框在布局页面坐标系(mm)下的矩形范围，用于后续标注框候选位置/避让判断。
    # 注意：rectWithFrame() 是地图项自身局部坐标，不是布局页面坐标；
    # 所以必须叠加 map_item.pos()，保证 map_rect 与锚点 layout_pt 在同一坐标系。
    from qgis.PyQt.QtCore import QRectF

    item_rect = map_item.rectWithFrame()
    item_pos = map_item.pos()
    map_rect = QRectF(
        float(item_pos.x()) + float(item_rect.x()),
        float(item_pos.y()) + float(item_rect.y()),
        float(item_rect.width()),
        float(item_rect.height()),
    )

    def map_point_to_layout_point(x: float, y: float):
        """
        将 callouts.csv / callouts 参数中的锚点坐标转换为布局页面坐标。

        正确链路：
        1) 原始 anchor.x / anchor.y 按 input_crs 解释，默认 EPSG:4326；
        2) 转到布局地图项自己的 CRS，即 map_item.crs()；
        3) 使用 QGIS 3.44 公开接口 layoutToMapCoordsTransform()；
        4) 取反后得到 map 坐标 -> layout 页面坐标。

        这里不再调用 mapToItemCoords()，也不再使用 extent 手动比例换算。
        如果转换失败，直接返回错误，让外层输出 debug，而不是画一个可能偏移的牵引线。
        """
        try:
            src_pt = QgsPointXY(float(x), float(y))
        except Exception as e:
            return None, f"锚点坐标非法: {e}"

        # input_crs -> map_item.crs()
        try:
            map_pt = xform.transform(src_pt) if xform is not None else src_pt
        except Exception as e:
            return None, f"锚点 CRS 转换失败: {e}"

        # QGIS 公开 API：layout 坐标 -> map 坐标
        try:
            layout_to_map = map_item.layoutToMapCoordsTransform()
        except Exception as e:
            return None, f"获取 layoutToMapCoordsTransform 失败: {e}"

        # 反转：map 坐标 -> layout 坐标
        try:
            inv_result = layout_to_map.inverted()

            # PyQt 通常返回 (QTransform, bool)，不同绑定也可能只返回 QTransform。
            if isinstance(inv_result, tuple):
                map_to_layout, invertible = inv_result
            else:
                map_to_layout = inv_result
                invertible = True

            if not invertible:
                return None, "layoutToMapCoordsTransform 不可逆，无法计算锚点布局坐标"

            layout_pt = map_to_layout.map(QPointF(float(map_pt.x()), float(map_pt.y())))
            return QPointF(float(layout_pt.x()), float(layout_pt.y())), None
        except Exception as e:
            return None, f"地图坐标转布局坐标失败: {e}"

    def wrap_chinese_text(text: str, font: QFont, max_width_mm: float, max_lines: Optional[int] = None, ellipsis: bool = True):
        return wrap_chinese_text_to_mm(
            text,
            QFontMetricsF(font),
            max_width_mm,
            max_lines=max_lines,
            ellipsis=ellipsis,
        )

    def intersect(a, b):
        return not (a[0] + a[2] <= b[0] or b[0] + b[2] <= a[0] or a[1] + a[3] <= b[1] or b[1] + b[3] <= a[1])

    placed = []
    items_out = []
    warnings = []
    sorted_callouts = sorted(callouts, key=lambda c: float((c or {}).get("priority", 0) or 0), reverse=True)

    for c in sorted_callouts:
        cid = str((c or {}).get("id") or f"callout_{len(items_out)+1}")
        item_warnings = []
        if not isinstance(c, dict):
            items_out.append({"id": cid, "success": False, "error_type": "invalid_callout_item", "message": "callouts 中每一项必须是对象(dict)", "warnings": [], "debug": {"received_type": type(c).__name__}})
            continue

        anchor = c.get("anchor") or {}
        if "x" not in anchor or "y" not in anchor:
            items_out.append({"id": cid, "success": False, "error_type": "invalid_anchor", "message": "anchor 缺少 x/y", "warnings": [], "debug": {"anchor": anchor}})
            continue
        anchor_layout, err = map_point_to_layout_point(anchor.get("x"), anchor.get("y"))
        if anchor_layout is None:
            items_out.append({"id": cid, "success": False, "error_type": "anchor_transform_failed", "message": err or "锚点转换失败", "warnings": [], "debug": {"anchor": anchor, "input_crs": input_crs_authid}})
            continue

        template = str(c.get("template") or "image_top_text_bottom")
        allowed_templates = {"image_top_text_bottom", "image_left_text_right", "text_only", "image_only_ellipse", "ellipse_badge"}
        if template not in allowed_templates:
            items_out.append({"id": cid, "success": False, "error_type": "invalid_template", "message": f"template 不支持: {template}", "warnings": [], "debug": {"allowed_templates": sorted(list(allowed_templates))}})
            continue

        box_cfg = dict(defaults["box"]); box_cfg.update(c.get("box") or {})
        txt_cfg = dict(defaults["text"]); txt_cfg.update(c.get("text_style") or {})
        img_cfg = dict(defaults["image"]); img_cfg.update(c.get("image") or {}); img_cfg.update(c.get("image_style") or {})
        pos_cfg = dict(defaults["position"]); pos_cfg.update(c.get("position") or {})
        leader_cfg = dict(defaults["leader"]); leader_cfg.update(c.get("leader_line") or {})

        # Older reviews and direct callers may still contain a title. It is
        # compatibility metadata only and must not reserve space or render.
        body = str(c.get("body", "") or "")
        width_mm = float(box_cfg.get("width_mm", 42.0) or 42.0)
        if not math.isfinite(width_mm) or width_mm <= 0:
            items_out.append({"id": cid, "success": False, "error_type": "invalid_box_width", "message": f"box.width_mm 必须为正数，当前: {width_mm}", "warnings": [], "debug": {"box": box_cfg}})
            continue

        shape_name = str(box_cfg.get("shape", "rectangle") or "rectangle")
        allowed_shapes = {"rectangle", "rounded_rectangle", "ellipse", "image_only_ellipse", "ellipse_badge"}
        if shape_name not in allowed_shapes:
            items_out.append({"id": cid, "success": False, "error_type": "invalid_shape", "message": f"box.shape 不支持: {shape_name}", "warnings": [], "debug": {"allowed_shapes": sorted(list(allowed_shapes))}})
            continue

        pad = float(box_cfg.get("padding_mm", 1.8) or 1.8)
        gap = float(img_cfg.get("gap_mm", 1.5) or 1.5)
        inner_w = max(width_mm - 2 * pad, 6.0)

        body_font = QFont(str(txt_cfg.get("body_font_family", "SimSun")))
        body_font.setPointSizeF(float(txt_cfg.get("body_font_size_pt", 5.5)))

        image_path = str(img_cfg.get("path", "") or "").strip()
        img_enabled = template != "text_only" and bool(img_cfg.get("enabled", False))
        image_size = None
        if img_enabled:
            reader = QImageReader(image_path)
            source_image = reader.read() if image_path else None
            if source_image is None or source_image.isNull():
                reason = reader.errorString() if image_path else "图片路径为空"
                item_warnings.append(f"图片不可读，已降级为纯文本：{reason}")
                img_enabled = False
            else:
                image_size = source_image.size()
        if not img_enabled and template != "text_only":
            template = "text_only"

        image_w = inner_w * 0.38 if template == "image_left_text_right" else inner_w
        img_h = (
            image_w * image_size.height() / image_size.width()
            if img_enabled and image_size is not None
            else 0.0
        )
        text_w = inner_w - image_w - gap if template == "image_left_text_right" else inner_w
        # Imported body text must remain complete. Legacy max_body_lines and
        # ellipsis options no longer silently discard the end of the content.
        body_lines, _, body_h = wrap_chinese_text(body, body_font, text_w)
        body_item = None
        if body_lines and template != "image_only_ellipse":
            body_item = QgsLayoutItemLabel(layout)
            body_item.setId(f"callout_{cid}_body")
            body_item.setText("\n".join(body_lines))
            body_item.setMargin(0)
            body_format = QgsTextFormat()
            body_format.setFont(body_font)
            body_format.setSize(body_font.pointSizeF())
            body_format.setColor(_color(txt_cfg.get("body_color"), "#333333"))
            body_format.setLineHeight(float(txt_cfg.get("line_spacing", 1.15)))
            body_item.setTextFormat(body_format)
            # Ask the renderer for its actual line height, including leading,
            # rather than assuming QFontMetrics and exported text are identical.
            body_h = max(body_h, float(body_item.sizeForText().height()))

        if template == "image_left_text_right":
            box_h = pad * 2 + max(img_h, body_h)
        elif template in ("image_only_ellipse", "ellipse_badge"):
            requested_h = box_cfg.get("height_mm")
            box_h = max(float(requested_h) if requested_h not in (None, "auto", "") else 24.0, img_h + pad * 2)
        else:
            box_h = pad * 2 + img_h + (gap if img_enabled and body_lines else 0) + body_h

        sides = pos_cfg.get("allow_sides") or defaults["position"]["allow_sides"]
        off = pos_cfg.get("offset_mm") or [18.0, -12.0]
        dx, dy = float(off[0]), float(off[1])
        candidates = {
            "right_top": (anchor_layout.x() + dx, anchor_layout.y() + dy - box_h),
            "right": (anchor_layout.x() + dx, anchor_layout.y() - box_h * 0.5),
            "right_bottom": (anchor_layout.x() + dx, anchor_layout.y() - dy),
            "left_top": (anchor_layout.x() - dx - width_mm, anchor_layout.y() + dy - box_h),
            "left": (anchor_layout.x() - dx - width_mm, anchor_layout.y() - box_h * 0.5),
            "left_bottom": (anchor_layout.x() - dx - width_mm, anchor_layout.y() - dy),
            "top": (anchor_layout.x() - width_mm * 0.5, anchor_layout.y() - abs(dy) - box_h),
            "bottom": (anchor_layout.x() - width_mm * 0.5, anchor_layout.y() + abs(dy)),
        }

        pref = str(pos_cfg.get("preferred_side", "right_top") or "right_top")
        best = None
        best_cost = 1e18
        for s in sides:
            if s not in candidates:
                continue
            x, y = candidates[s]
            rect = [x, y, width_mm, box_h]
            cost = 0.0
            if x < map_rect.x() or y < map_rect.y() or x + width_mm > map_rect.x() + map_rect.width() or y + box_h > map_rect.y() + map_rect.height():
                cost += 10000.0
            for pr in placed:
                if intersect(rect, pr):
                    cost += 8000.0
            if s != pref:
                cost += 5.0
            cx, cy = x + width_mm * 0.5, y + box_h * 0.5
            cost += ((cx - anchor_layout.x()) ** 2 + (cy - anchor_layout.y()) ** 2) ** 0.5 * 0.2
            if cost < best_cost:
                best_cost, best = cost, (s, rect)

        if best is None:
            items_out.append({"id": cid, "success": False, "error_type": "placement_failed", "message": "无法找到候选位置", "warnings": item_warnings, "debug": {"allow_sides": sides, "preferred_side": pref}})
            continue
        side, rect = best
        if (
            rect[0] < map_rect.x()
            or rect[1] < map_rect.y()
            or rect[0] + rect[2] > map_rect.x() + map_rect.width()
            or rect[1] + rect[3] > map_rect.y() + map_rect.height()
        ):
            item_warnings.append("标注框超出地图范围；已保留完整内容，请调整注记宽度或位置")
        placed.append(rect)

        box_item = QgsLayoutItemShape(layout)
        box_item.setId(f"callout_{cid}_box")
        box_item.attemptMove(QgsLayoutPoint(rect[0], rect[1], QgsUnitTypes.LayoutMillimeters))
        box_item.attemptResize(QgsLayoutSize(rect[2], rect[3], QgsUnitTypes.LayoutMillimeters))
        if str(box_cfg.get("shape", "rectangle")) in ("ellipse", "image_only_ellipse", "ellipse_badge"):
            box_item.setShapeType(QgsLayoutItemShape.Shape.Ellipse)
        else:
            box_item.setShapeType(QgsLayoutItemShape.Shape.Rectangle)
        sym = box_item.symbol()
        if sym is not None and sym.symbolLayerCount() > 0:
            sl = sym.symbolLayer(0)
            sl.setFillColor(_color(box_cfg.get("fill_color"), "#fff7e6"))
            pen = QPen(_color(box_cfg.get("border_color"), "#d9822b"))
            pen.setWidthF(float(box_cfg.get("border_width_mm", 0.3) or 0.3))
            sl.setStrokeColor(pen.color())
            sl.setStrokeWidth(pen.widthF())
        layout.addLayoutItem(box_item)

        created = {"box": box_item.id()}
        cursor_y = rect[1] + pad
        if template in ("image_top_text_bottom", "image_left_text_right") and img_enabled:
            pic = QgsLayoutItemPicture(layout)
            pic.setId(f"callout_{cid}_image")
            pic.setResizeMode(QgsLayoutItemPicture.Zoom)
            pic.setPictureAnchor(QgsLayoutItem.UpperLeft)
            pic.setPicturePath(str(img_cfg.get("path")))
            pic.attemptMove(QgsLayoutPoint(rect[0] + pad, cursor_y, QgsUnitTypes.LayoutMillimeters))
            pic.attemptResize(QgsLayoutSize(image_w, img_h, QgsUnitTypes.LayoutMillimeters))
            layout.addLayoutItem(pic)
            created["image"] = pic.id()
            if template == "image_top_text_bottom":
                cursor_y += img_h + (gap if body_lines else 0)

        if body_item is not None:
            # This label was constructed early for measurement, before the
            # background shape. Keep it above that later-created background.
            body_item.setZValue(box_item.zValue() + 2)
            text_x = rect[0] + pad + (image_w + gap if template == "image_left_text_right" else 0)
            body_item.attemptMove(QgsLayoutPoint(text_x, cursor_y, QgsUnitTypes.LayoutMillimeters))
            body_item.attemptResize(QgsLayoutSize(text_w, max(body_h, 1.0), QgsUnitTypes.LayoutMillimeters))
            layout.addLayoutItem(body_item)
            created["body"] = body_item.id()

        if bool(leader_cfg.get("enabled", True)):
            try:
                bx, by, bw, bh = rect
                cx = bx + bw * 0.5
                cy = by + bh * 0.5
                ax = anchor_layout.x(); ay = anchor_layout.y()
                dxl = ax - cx; dyl = ay - cy
                tvals = []
                if abs(dxl) > 1e-8:
                    tvals.extend([(bx - cx) / dxl, (bx + bw - cx) / dxl])
                if abs(dyl) > 1e-8:
                    tvals.extend([(by - cy) / dyl, (by + bh - cy) / dyl])
                tpos = [t for t in tvals if t > 0]
                t = min(tpos) if tpos else 1.0
                sx = cx + dxl * t
                sy = cy + dyl * t

                leader_style = str(leader_cfg.get("style", "double") or "double").strip().lower()
                leader_color = _color(leader_cfg.get("color"), "#d9822b")
                leader_width = float(leader_cfg.get("width_mm", 0.25) or 0.25)

                def _build_line_symbol():
                    line_symbol = QgsLineSymbol()
                    if line_symbol.symbolLayerCount() > 0:
                        lsl = line_symbol.symbolLayer(0)
                        if hasattr(lsl, "setColor"):
                            lsl.setColor(leader_color)
                        elif hasattr(lsl, "setStrokeColor"):
                            lsl.setStrokeColor(leader_color)
                        if hasattr(lsl, "setWidth"):
                            lsl.setWidth(leader_width)
                        elif hasattr(lsl, "setStrokeWidth"):
                            lsl.setStrokeWidth(leader_width)
                        if hasattr(lsl, "setWidthUnit"):
                            lsl.setWidthUnit(QgsUnitTypes.RenderMillimeters)
                        if hasattr(lsl, "setFillColor"):
                            lsl.setFillColor(QColor(0, 0, 0, 0))
                    return line_symbol

                if leader_style == "double":
                    # 从框中心朝锚点发射射线：先碰到哪条边，就从哪条边双出线
                    dx_anchor = ax - cx
                    dy_anchor = ay - cy
                    gap = max(1.5, min(8.0, min(bw, bh) * 0.2))

                    hit_candidates = []
                    if abs(dx_anchor) > 1e-9:
                        t_left = (bx - cx) / dx_anchor
                        y_left = cy + t_left * dy_anchor
                        if t_left > 0 and by <= y_left <= by + bh:
                            hit_candidates.append((t_left, "left"))

                        t_right = (bx + bw - cx) / dx_anchor
                        y_right = cy + t_right * dy_anchor
                        if t_right > 0 and by <= y_right <= by + bh:
                            hit_candidates.append((t_right, "right"))

                    if abs(dy_anchor) > 1e-9:
                        t_top = (by - cy) / dy_anchor
                        x_top = cx + t_top * dx_anchor
                        if t_top > 0 and bx <= x_top <= bx + bw:
                            hit_candidates.append((t_top, "top"))

                        t_bottom = (by + bh - cy) / dy_anchor
                        x_bottom = cx + t_bottom * dx_anchor
                        if t_bottom > 0 and bx <= x_bottom <= bx + bw:
                            hit_candidates.append((t_bottom, "bottom"))

                    if hit_candidates:
                        _, hit_edge = min(hit_candidates, key=lambda it: it[0])
                    else:
                        # 退化兜底：沿主轴方向选择
                        hit_edge = "right" if abs(dx_anchor) >= abs(dy_anchor) and dx_anchor >= 0 else (
                            "left" if abs(dx_anchor) >= abs(dy_anchor) else ("bottom" if dy_anchor >= 0 else "top")
                        )

                    if hit_edge in ("left", "right"):
                        x_edge = bx if hit_edge == "left" else (bx + bw)
                        y1 = cy - gap * 0.5
                        y2 = cy + gap * 0.5
                        starts = [QPointF(x_edge, y1), QPointF(x_edge, y2)]
                    else:
                        y_edge = by if hit_edge == "top" else (by + bh)
                        x1 = cx - gap * 0.5
                        x2 = cx + gap * 0.5
                        starts = [QPointF(x1, y_edge), QPointF(x2, y_edge)]

                    leader_ids = []
                    for idx, st in enumerate(starts):
                        line = QgsLayoutItemPolyline(layout)
                        line.setId(f"callout_{cid}_leader_{idx + 1}")
                        nodes = QPolygonF([st, QPointF(ax, ay)])
                        if hasattr(line, "setNodes"):
                            line.setNodes(nodes)
                        else:
                            raise AttributeError("QgsLayoutItemPolyline 缺少 setNodes")
                        if hasattr(line, "setSymbol"):
                            line.setSymbol(_build_line_symbol())
                        layout.addLayoutItem(line)
                        leader_ids.append(line.id())
                    created["leader"] = leader_ids
                else:
                    nodes = [QPointF(sx, sy), QPointF(ax, ay)]
                    if leader_style == "curved":
                        ctrl_x = (sx + ax) * 0.5
                        ctrl_y = (sy + ay) * 0.5 - max(2.0, min(10.0, abs(ax - sx) * 0.15 + abs(ay - sy) * 0.15))
                        nodes = []
                        seg_n = 14
                        for i in range(seg_n + 1):
                            u = i / float(seg_n)
                            x = (1 - u) * (1 - u) * sx + 2 * (1 - u) * u * ctrl_x + u * u * ax
                            y = (1 - u) * (1 - u) * sy + 2 * (1 - u) * u * ctrl_y + u * u * ay
                            nodes.append(QPointF(x, y))
                    elif leader_style != "straight":
                        item_warnings.append(f"未知 leader_line.style={leader_style}，当前支持 straight / curved / double，已按 straight 处理")

                    line = QgsLayoutItemPolyline(layout)
                    line.setId(f"callout_{cid}_leader")
                    polyline_nodes = QPolygonF(nodes)
                    if hasattr(line, "setNodes"):
                        line.setNodes(polyline_nodes)
                    else:
                        raise AttributeError("QgsLayoutItemPolyline 缺少 setNodes")
                    if hasattr(line, "setSymbol"):
                        line.setSymbol(_build_line_symbol())
                    layout.addLayoutItem(line)
                    created["leader"] = line.id()
            except Exception as le:
                item_warnings.append(f"牵引线绘制失败（已忽略）: {str(le)}")

        warnings.extend(f"{cid}: {warning}" for warning in item_warnings)
        items_out.append({
            "id": cid,
            "success": True,
            "error_type": None,
            "message": "ok",
            "created_item_ids": created,
            "anchor_layout": [float(anchor_layout.x()), float(anchor_layout.y())],
            "box_rect_mm": [float(rect[0]), float(rect[1]), float(rect[2]), float(rect[3])],
            "template": template,
            "wrapped_body_lines": body_lines,
            "warnings": item_warnings,
        })

    created_count = len([i for i in items_out if i.get("success")])
    failed_count = len(items_out) - created_count
    overall_success = created_count > 0

    if failed_count == 0:
        message = "全部标注创建成功"
        error_type = None
    elif created_count > 0:
        message = f"部分标注失败: {failed_count} 个，成功: {created_count} 个"
        error_type = "partial_failed"
    else:
        message = "标注创建失败：没有任何标注创建成功"
        error_type = "all_failed"

    return {
        "success": overall_success,
        "output": layout.name(),
        "info": message,
        "error_type": error_type,
        "message": message,
        "layout_name": layout.name(),
        "created_count": created_count,
        "failed_count": failed_count,
        "items": items_out,
        "warnings": warnings,
        "debug": {
            "input_crs": input_crs_authid,
            "project_crs": map_crs.authid() if map_crs is not None else None,
            "map_item_id": map_item.id(),
        },
    }

# 图文标注框的异常捕获
def cartography_add_callouts_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    """在 QGIS Layout 中批量创建图文标注框（callout）。"""
    try:
        return _cartography_add_callouts_impl(params)
    except Exception as e:
        import traceback

        tb = traceback.format_exc()
        return {
            "success": False,
            "output": "",
            "info": f"callout 绘制失败: {str(e)}",
            "error_type": "draw_failed",
            "message": str(e),
            "warnings": [],
            "debug": {"exception": repr(e), "traceback": tb},
            "detail": f"{str(e)} | traceback: {tb}",
        }


draw_bezier_arrow_from_points_handler = _wrap_main_thread(draw_bezier_arrow_from_points_handler)
cartography_build_and_draw_from_tables_handler = _wrap_main_thread(cartography_build_and_draw_from_tables_handler)
cartography_add_callouts_handler = _wrap_main_thread(cartography_add_callouts_handler)
