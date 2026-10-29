"""专题渲染器、分类方法与色带。"""
import math
from typing import (
    Any,
    Dict,
)
from qgis.PyQt.QtCore import QVariant
from qgis.PyQt.QtGui import QColor
from qgis.core import (
    Qgis,
    QgsClassificationEqualInterval,
    QgsClassificationJenks,
    QgsClassificationQuantile,
    QgsClassificationStandardDeviation,
    QgsColorBrewerColorRamp,
    QgsFeatureRequest,
    QgsGradientColorRamp,
    QgsGradientStop,
    QgsGraduatedSymbolRenderer,
    QgsMarkerSymbol,
    QgsLineSymbol,
    QgsProject,
    QgsRendererRange,
    QgsSingleSymbolRenderer,
    QgsStyle,
    QgsSymbol,
    QgsVectorLayer,
    QgsRuleBasedRenderer,
)
from autocarto.tools.system._shared.style_common import (
    _get_layer,
    _wrap_main_thread,
)


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

        layer, error = _get_layer(layer_name)
        if error:
            return error
        scale = float(params.get("scale", 1))
        if not math.isfinite(scale) or scale <= 0:
            raise ValueError("scale 必须为正数")
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
            if "width" in rule_def:
                if not isinstance(rule.symbol(), QgsLineSymbol):
                    raise ValueError("width 仅适用于线图层，单位为毫米")
                width = float(rule_def["width"]) * scale
                if not math.isfinite(width) or width <= 0:
                    raise ValueError("规则线宽必须为正数")
                rule.symbol().setWidthUnit(Qgis.RenderUnit.Millimeters)
                rule.symbol().setWidth(width)
            root_rule.appendChild(rule)

        if root_rule.children():
            root_rule.removeChildAt(0)
        layer.setRenderer(renderer)
        layer.triggerRepaint()

        return {
            "success": True,
            "output": layer_name,
            "info": f"规则样式设置完成，共 {len(rules)} 条规则",
            "data": {"rules": [
                {"label": rule.label(), "expression": rule.filterExpression(),
                 "color": rule.symbol().color().name(),
                 **({"width_mm": rule.symbol().width()} if isinstance(rule.symbol(), QgsLineSymbol) else {})}
                for rule in layer.renderer().rootRule().children()
            ]},
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


graduated_style_handler = _wrap_main_thread(graduated_style_handler)
rule_based_style_handler = _wrap_main_thread(rule_based_style_handler)
heatmap_style_handler = _wrap_main_thread(heatmap_style_handler)
point_displacement_style_handler = _wrap_main_thread(point_displacement_style_handler)
style_25d_handler = _wrap_main_thread(style_25d_handler)
