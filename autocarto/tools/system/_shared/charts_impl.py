from __future__ import annotations

import html
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

from qgis.core import QgsProject, QgsVectorLayer

from autocarto.tools.system._shared import output_manager


def _coerce_float(value: Any) -> float:
    if value is None:
        return 0.0
    if isinstance(value, (int, float)):
        return float(value)
    text = str(value).strip().replace(",", "")
    if not text:
        return 0.0
    return float(text)


def _build_bar_chart_svg(rows: List[Tuple[str, float]]) -> str:
    width = 1000
    row_height = 34
    chart_left = 210
    chart_right = 940
    top = 70
    height = top + len(rows) * row_height + 40
    max_value = max((value for _, value in rows), default=1.0) or 1.0

    svg_rows = []
    for idx, (label, value) in enumerate(rows):
        y = top + idx * row_height
        bar_width = (
            0 if max_value <= 0 else (value / max_value) * (chart_right - chart_left)
        )
        label_text = html.escape(label)
        value_text = f"{value:,.2f}".rstrip("0").rstrip(".")
        svg_rows.append(
            f"<text x='24' y='{y + 18}' font-size='14' fill='#111827'>{label_text}</text>"
            f"<rect x='{chart_left}' y='{y + 4}' width='{bar_width:.2f}' height='20' rx='4' fill='#2563eb'></rect>"
            f"<text x='{chart_left + bar_width + 8:.2f}' y='{y + 18}' font-size='13' fill='#1f2937'>{value_text}</text>"
        )

    axis = (
        f"<line x1='{chart_left}' y1='{top - 14}' x2='{chart_left}' y2='{height - 24}' stroke='#9ca3af' stroke-width='1' />"
        f"<line x1='{chart_left}' y1='{height - 24}' x2='{chart_right}' y2='{height - 24}' stroke='#9ca3af' stroke-width='1' />"
    )

    return f"<svg viewBox='0 0 {width} {height}' xmlns='http://www.w3.org/2000/svg'>{axis}{''.join(svg_rows)}</svg>"


def _build_bar_chart_html(
    title: str, rows: List[Tuple[str, float]], svg_markup: str
) -> str:
    return f"""<!DOCTYPE html>
<html lang='zh-CN'>
<head>
  <meta charset='utf-8' />
  <title>{html.escape(title)}</title>
  <style>
    body {{ font-family: Arial, sans-serif; margin: 0; background: #ffffff; }}
    .wrap {{ padding: 24px; }}
    h1 {{ margin: 0 0 16px; font-size: 24px; color: #111827; }}
    p {{ margin: 0 0 12px; color: #4b5563; font-size: 13px; }}
    svg {{ width: 100%; height: auto; border: 1px solid #e5e7eb; background: #fff; }}
  </style>
</head>
<body>
  <div class='wrap'>
    <h1>{html.escape(title)}</h1>
    <p>基于当前图层数据自动生成的 GDP 条形图。</p>
    {svg_markup}
  </div>
</body>
</html>"""


def _resolve_vector_layer(layer_ref: Any) -> Optional[QgsVectorLayer]:
    name = str(layer_ref or "").strip()
    if not name:
        return None

    project = QgsProject.instance()
    by_id = project.mapLayer(name)
    if isinstance(by_id, QgsVectorLayer):
        return by_id

    by_name = project.mapLayersByName(name)
    if by_name and isinstance(by_name[0], QgsVectorLayer):
        return by_name[0]

    return None


def _fallback_selected_layer(
    name_field: str, value_field: str
) -> Optional[QgsVectorLayer]:
    candidates: List[QgsVectorLayer] = []
    for layer in QgsProject.instance().mapLayers().values():
        if not isinstance(layer, QgsVectorLayer):
            continue
        if (
            layer.fields().indexFromName(name_field) < 0
            or layer.fields().indexFromName(value_field) < 0
        ):
            continue
        if layer.selectedFeatureCount() <= 0:
            continue
        candidates.append(layer)
    if len(candidates) == 1:
        return candidates[0]
    return None


def _chart_rows(
    layer: QgsVectorLayer, name_field: str, value_field: str, selected_only: bool
) -> List[Tuple[str, float]]:
    use_selected = bool(selected_only) or layer.selectedFeatureCount() > 0
    features = layer.selectedFeatures() if use_selected else layer.getFeatures()
    rows: List[Tuple[str, float]] = []
    for feature in features:
        rows.append((str(feature[name_field]), _coerce_float(feature[value_field])))
    rows.sort(key=lambda item: item[1], reverse=True)
    return rows


def bar_chart_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        output_manager.sync_session_context_from_params(params)
        layer_ref = params.get("INPUT")
        name_field = str(params.get("NAME_FIELD") or "").strip()
        value_field = str(params.get("VALUE_FIELD") or "").strip()
        output_path = params.get("OUTPUT")
        selected_only = bool(params.get("SELECTED_ONLY", False))

        if not layer_ref or not name_field or not value_field:
            return {"success": False, "output": "", "info": "错误：缺少条形图所需参数"}

        layer = _resolve_vector_layer(layer_ref)
        if layer is None:
            layer = _fallback_selected_layer(name_field, value_field)
        if layer is None:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_ref}'",
            }
        if layer.fields().indexFromName(name_field) < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：字段 '{name_field}' 不存在",
            }
        if layer.fields().indexFromName(value_field) < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：字段 '{value_field}' 不存在",
            }

        rows = _chart_rows(layer, name_field, value_field, selected_only)
        if not rows:
            return {
                "success": False,
                "output": "",
                "info": "错误：图层中没有可用于制图的数据",
            }

        resolved_path = output_manager.resolve_output_path(
            output_path, f"{layer.name()}_bar_chart.html", "exports"
        )
        svg_markup = _build_bar_chart_svg(rows)
        content = _build_bar_chart_html(f"{layer.name()} GDP 条形图", rows, svg_markup)
        svg_path = str(Path(resolved_path).with_suffix(".svg"))
        Path(resolved_path).write_text(content, encoding="utf-8")
        Path(svg_path).write_text(svg_markup, encoding="utf-8")
        return {
            "success": True,
            "output": resolved_path,
            "info": {
                "message": f"条形图导出成功: HTML={resolved_path}; 可直接用于 layout_add_picture 的 SVG={svg_path}",
                "svg_path": svg_path,
                "picture_path": svg_path,
                "layer_name": layer.name(),
                "row_count": len(rows),
                "used_selected_features": selected_only
                or layer.selectedFeatureCount() > 0,
            },
            "output_files": [resolved_path, svg_path],
        }
    except Exception as exc:
        return {"success": False, "output": "", "info": f"条形图导出失败: {exc}"}
