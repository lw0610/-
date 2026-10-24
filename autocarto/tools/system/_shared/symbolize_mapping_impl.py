"""按数据行和符号对照表符号化点图层。"""
from typing import (
    Any,
    Dict,
    List,
)
from qgis.core import (
    QgsUnitTypes,
    QgsMarkerSymbol,
    QgsProject,
    QgsSvgMarkerSymbolLayer,
    QgsWkbTypes,
    QgsRuleBasedRenderer,
)
from autocarto.tools.system._shared.style_common import _wrap_main_thread
from autocarto.knowledge.visual_presets import renderer_defaults


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
            rule.setLabel(str(cfg.get("description") or st))
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


symbolize_points_by_mapping_handler = _wrap_main_thread(symbolize_points_by_mapping_handler)
