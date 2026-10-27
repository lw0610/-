import hashlib
import json
import logging
import math
import os
import uuid
from datetime import datetime, timezone
from typing import Any, Dict, List, Mapping, Optional

from qgis.PyQt.QtCore import Qt
from qgis.PyQt.QtGui import QColor, QFont, QFontMetricsF
from qgis.core import (
    Qgis,
    QgsFillSymbol,
    QgsLayerTreeLayer,
    QgsLayerTreeModelLegendNode,
    QgsLayoutExporter,
    QgsLayoutItemGroup,
    QgsLayoutItemLabel,
    QgsLayoutItemLegend,
    QgsLayoutItemMap,
    QgsLayoutItem,
    QgsLayoutItemPage,
    QgsLayoutItemPicture,
    QgsLayoutItemShape,
    QgsLayoutItemScaleBar,
    QgsMarkerLineSymbolLayer,
    QgsLayoutPoint,
    QgsLegendStyle,
    QgsMarkerSymbol,
    QgsProject,
    QgsRasterMarkerSymbolLayer,
    QgsSimpleLineSymbolLayer,
    QgsSingleSymbolRenderer,
    QgsSvgMarkerSymbolLayer,
    QgsApplication,
    QgsTextFormat,
    QgsUnitTypes,
    QgsVectorLayer,
)

from autocarto.tools.system._shared import output_manager
from autocarto.tools.system._shared.canvas_refresh import refresh_canvas
from autocarto.tools.system._shared.layout_export_artifacts import ArtifactLayoutExporter
from autocarto.tools.system._shared.layout_export_impl import (
    _export_result_message,
    _get_layout,
    _image_export_dpi,
    _resolve_export_path,
    _validate_layout_for_export,
)
from autocarto.tools.system._shared.layout_runtime import run_in_main_thread

logger = logging.getLogger(__name__)

LAYOUT_SNAPSHOT_SCHEMA_VERSION = "quality-feedback-layout-snapshot/v1"


def _call_if_supported(target, method_name: str, *args, default=None):
    method = getattr(target, method_name, None)
    if not callable(method):
        return default
    try:
        return method(*args)
    except Exception:
        logger.debug("%s.%s failed", type(target).__name__, method_name)
        return default


def _safe_value(fn, default=None):
    try:
        return fn()
    except Exception:
        return default


def _string_leaves(value: Any) -> List[str]:
    leaves: List[str] = []
    if isinstance(value, Mapping):
        for nested in value.values():
            leaves.extend(_string_leaves(nested))
    elif isinstance(value, list):
        for nested in value:
            leaves.extend(_string_leaves(nested))
    elif isinstance(value, str) and value.strip():
        leaves.append(value.strip())
    return leaves


def _write_layout_snapshot(
    params: Dict[str, Any],
    *,
    layout_name: str,
    layout_item_ids: Mapping[str, Any],
    layout_item_state: Mapping[str, Any],
) -> Dict[str, str]:
    """Persist an immutable quality-review snapshot in the current session."""

    handed_off_ids = list(dict.fromkeys(_string_leaves(layout_item_ids)))
    state_items = layout_item_state.get("items")
    if not handed_off_ids or not isinstance(state_items, Mapping):
        raise ValueError("layout_item_ids 中没有可保存的真实元素 ID")

    handed_off_set = set(handed_off_ids)
    state_id_set = {str(item_id) for item_id in state_items}
    missing_states = sorted(handed_off_set - state_id_set)
    unregistered_states = sorted(state_id_set - handed_off_set)
    if missing_states:
        raise ValueError(
            "布局快照缺少元素状态: " + ", ".join(missing_states)
        )
    if unregistered_states:
        raise ValueError(
            "布局快照包含未登记元素状态: " + ", ".join(unregistered_states)
        )

    output_manager.sync_session_context_from_params(params)
    snapshot_dir = os.path.join(
        output_manager.get_output_subdir("intermediate"), "quality_feedback"
    )
    os.makedirs(snapshot_dir, exist_ok=True)

    snapshot_id = uuid.uuid4().hex
    snapshot_path = os.path.abspath(
        os.path.join(snapshot_dir, f"layout_snapshot_{snapshot_id}.json")
    )
    payload = {
        "schema_version": LAYOUT_SNAPSHOT_SCHEMA_VERSION,
        "snapshot_id": snapshot_id,
        "captured_at": datetime.now(timezone.utc).isoformat(),
        "session_id": str(params.get("session_id") or "").strip(),
        "project_path": str(
            _safe_value(lambda: QgsProject.instance().fileName(), "") or ""
        ),
        "layout_name": layout_name,
        "layout_item_ids": dict(layout_item_ids),
        "layout_item_state": dict(layout_item_state),
    }
    encoded = json.dumps(
        payload, ensure_ascii=False, indent=2, sort_keys=True
    ).encode("utf-8")

    created = False
    try:
        with open(snapshot_path, "xb") as handle:
            created = True
            handle.write(encoded)
            handle.flush()
            os.fsync(handle.fileno())
    except Exception:
        if created:
            try:
                os.unlink(snapshot_path)
            except FileNotFoundError:
                pass
        raise

    return {
        "layout_snapshot_path": snapshot_path,
        "layout_snapshot_sha256": hashlib.sha256(encoded).hexdigest(),
    }


def _result(
    success: bool, output: Any, info: str, data: Optional[Dict[str, Any]] = None
):
    payload = {"success": success, "output": output, "info": info}
    if data is not None:
        payload["data"] = data
    return payload


def _resolve_color(value: Optional[str], default: str) -> QColor:
    candidate = QColor(str(value or default).strip() or default)
    if not candidate.isValid():
        candidate = QColor(default)
    return candidate


def _build_mm_text_format(
    family: str,
    size_mm: float,
    bold: bool = False,
    italic: bool = False,
    color: Optional[str] = None,
) -> QgsTextFormat:
    text_format = QgsTextFormat()
    font = QFont(str(family or "Arial"))
    font.setBold(bool(bold))
    font.setItalic(bool(italic))
    text_format.setFont(font)
    text_format.setSize(float(size_mm))
    text_format.setSizeUnit(Qgis.RenderUnit.Millimeters)
    if color:
        text_format.setColor(_resolve_color(color, "#000000"))
    return text_format


def _layout_dpi(layout: Any) -> float:
    dpi = _safe_value(lambda: float(layout.renderContext().dpi()), 300.0)
    return dpi if dpi and dpi > 0 else 300.0


def _font_for_text_format(text_format: QgsTextFormat, dpi: float) -> QFont:
    font = QFont(text_format.font())
    size = _safe_value(lambda: float(text_format.size()), 0.0)
    if size and size > 0:
        unit = _safe_value(lambda: text_format.sizeUnit())
        if unit == Qgis.RenderUnit.Millimeters:
            font.setPixelSize(max(1, round(size * dpi / 25.4)))
        else:
            font.setPointSizeF(size)
    return font


def _wrap_text_to_width(text: str, text_format: QgsTextFormat, width_mm: float, dpi: float) -> str:
    metrics = QFontMetricsF(_font_for_text_format(text_format, dpi))
    max_width_px = max(1.0, float(width_mm) * dpi / 25.4)
    wrapped_lines = []

    for paragraph in str(text).splitlines():
        line = ""
        for ch in paragraph:
            candidate = f"{line}{ch}"
            if not line or metrics.horizontalAdvance(candidate) <= max_width_px:
                line = candidate
            else:
                wrapped_lines.append(line.rstrip())
                line = ch.lstrip() if ch.isspace() else ch
        wrapped_lines.append(line.rstrip())

    return "\n".join(wrapped_lines)


def _has_value(params: Dict[str, Any], key: str) -> bool:
    if key not in params:
        return False
    value = params.get(key)
    return value is not None and value != ""


def _default_unit_label(value: Optional[str]) -> str:
    unit_name = str(value or "meters").strip().lower()
    if unit_name in {"m", "meter", "meters"}:
        unit_name = "meters"
    elif unit_name in {"km", "kilometer", "kilometers"}:
        unit_name = "kilometers"
    mapping = {
        "meters": "m",
        "kilometers": "km",
        "feet": "ft",
        "miles": "mi",
        "nautical_miles": "nm",
    }
    return mapping.get(unit_name, "")


def _distance_unit_name_from_enum(value: Any) -> Optional[str]:
    if value is None:
        return None
    if Qgis is not None and hasattr(Qgis, "DistanceUnit"):
        qgis_mapping = {
            Qgis.DistanceUnit.Meters: "meters",
            Qgis.DistanceUnit.Kilometers: "kilometers",
            Qgis.DistanceUnit.Feet: "feet",
            Qgis.DistanceUnit.Miles: "miles",
            Qgis.DistanceUnit.NauticalMiles: "nautical_miles",
        }
        if value in qgis_mapping:
            return qgis_mapping.get(value)
    legacy_mapping = {
        QgsUnitTypes.DistanceMeters: "meters",
        QgsUnitTypes.DistanceKilometers: "kilometers",
        QgsUnitTypes.DistanceFeet: "feet",
        QgsUnitTypes.DistanceMiles: "miles",
        QgsUnitTypes.DistanceNauticalMiles: "nautical_miles",
    }
    if value in legacy_mapping:
        return legacy_mapping.get(value)
    return None


def _get_layout_item(
    layout_name: Optional[str], item_id: Optional[str], expected_type=None
):
    if not layout_name:
        return None, None, _result(False, "", "错误：缺少布局名称参数")
    layout = _get_layout(layout_name)
    if not layout:
        return None, None, _result(False, "", f"错误：未找到布局 '{layout_name}'")
    item = layout.itemById(item_id)
    if not item:
        return layout, None, _result(False, "", f"错误：未找到元素 '{item_id}'")
    if expected_type is not None and not isinstance(item, expected_type):
        return layout, None, _result(
            False, "", f"错误：元素 '{item_id}' 类型不匹配"
        )
    return layout, item, None


def _item_rect(item) -> Dict[str, float]:
    if isinstance(item, QgsLayoutItem):
        rect = item.mapRectToScene(item.rect())
        layout = item.layout()
        return {key: float(layout.convertFromLayoutUnits(value, QgsUnitTypes.LayoutMillimeters).length())
                for key, value in (("x", rect.x()), ("y", rect.y()), ("width", rect.width()), ("height", rect.height()))}
    rect = _safe_value(lambda: item.sceneRect())
    if rect is None:
        rect = _safe_value(lambda: item.rect())
    if rect is None:
        return {"x": 0.0, "y": 0.0, "width": 0.0, "height": 0.0}
    pos = _safe_value(lambda: item.pos())
    x = _safe_value(lambda: rect.x(), 0.0)
    y = _safe_value(lambda: rect.y(), 0.0)
    if pos is not None:
        x = _safe_value(lambda: pos.x(), x)
        y = _safe_value(lambda: pos.y(), y)
    return {
        "x": x,
        "y": y,
        "width": _safe_value(lambda: rect.width(), 0.0),
        "height": _safe_value(lambda: rect.height(), 0.0),
    }
    

def _item_properties(item, parent_map=None) -> Dict[str, Any]:
    raw_anchor = _safe_value(lambda: item.customProperty("autocarto/anchor", ""), "")
    try:
        anchor = raw_anchor if isinstance(raw_anchor, Mapping) else json.loads(raw_anchor) if raw_anchor else None
    except (TypeError, ValueError):
        anchor = None
    data = {
        "item_id": _safe_value(lambda: item.id(), ""),
        "type": type(item).__name__,
        "rect_mm": _item_rect(parent_map if parent_map is not None else item),
        "z_value": _safe_value(lambda: item.zValue(), 0),
        "anchor": dict(anchor) if isinstance(anchor, Mapping) else None,
        "font_locked": bool(_safe_value(lambda: item.customProperty("autocarto/font_locked", False), False)),
    }
    if isinstance(item, QgsLayoutItemMap):
        data.update(
            {
                "map_rotation": _safe_value(lambda: item.mapRotation(), None),
                "scale": _safe_value(lambda: item.scale(), None),
                "layers": _safe_value(
                    lambda: [layer.name() for layer in (item.layers() or [])], []
                ),
                "layer_ids": _safe_value(
                    lambda: [layer.id() for layer in (item.layers() or [])], []
                ),
                "keep_layer_set": _safe_value(lambda: item.keepLayerSet(), False),
                "crs": _safe_value(lambda: item.crs().authid(), ""),
                "crs_wkt": _safe_value(lambda: item.crs().toWkt(), ""),
                "extent": _safe_value(lambda: {
                    "xmin": item.extent().xMinimum(), "ymin": item.extent().yMinimum(),
                    "xmax": item.extent().xMaximum(), "ymax": item.extent().yMaximum(),
                }, {}),
            }
        )
    if isinstance(item, QgsLayoutItemLegend):
        data.update(
            {
                "title": _safe_value(lambda: item.title(), None),
                "auto_update_model": _safe_value(lambda: item.autoUpdateModel(), None),
                "resize_to_contents": _safe_value(lambda: item.resizeToContents(), None),
                "linked_map": _safe_value(lambda: item.linkedMap().id(), None),
                "legend_layers": _safe_value(lambda: [{
                    "id": node.layerId(), "name": node.name(),
                    "items": [{
                        "label": str(entry.data(Qt.DisplayRole) or ""),
                        "rule_key": str(entry.data(QgsLayerTreeModelLegendNode.RuleKeyRole) or ""),
                    } for entry in item.model().layerLegendNodes(node)],
                } for node in item.model().rootGroup().findLayers()], []),
            }
        )
    if isinstance(item, QgsLayoutItemLabel):
        data.update({"text": _safe_value(lambda: item.text(), None)})
    if isinstance(item, QgsLayoutItemScaleBar):
        unit_name = _distance_unit_name_from_enum(_safe_value(lambda: item.units(), None))
        unit_label = _safe_value(lambda: item.unitLabel(), None)
        if not unit_label and unit_name:
            unit_label = _default_unit_label(unit_name)
        data.update(
            {
                "units": unit_name,
                "unit_label": unit_label,
                "units_per_segment": _safe_value(lambda: item.unitsPerSegment(), None),
                "segments": _safe_value(lambda: item.numberOfSegments(), None),
                "segments_left": _safe_value(lambda: item.numberOfSegmentsLeft(), None),
            }
        )
    outer_rect_mm = _decorative_border_outer_rect(item, data["rect_mm"])
    if outer_rect_mm is not None:
        data["outer_rect_mm"] = outer_rect_mm
    return data


def _to_float(value: Any, default: float = 0.0) -> float:
    try:
        return float(value)
    except Exception:
        return float(default)


def _decorative_border_outer_rect(item, rect_mm: Dict[str, float]) -> Optional[Dict[str, float]]:
    if not isinstance(item, QgsLayoutItemShape):
        return None
    role = str(_safe_value(lambda: item.customProperty("autocarto/item_role", ""), "") or "").strip().lower()

    symbol = _safe_value(lambda: item.symbol())
    if symbol is None:
        return None

    has_marker_line = False
    has_offset_line = False
    expand_mm = 0.0
    layer_count = int(_safe_value(lambda: symbol.symbolLayerCount(), 0) or 0)
    for i in range(layer_count):
        layer = _safe_value(lambda idx=i: symbol.symbolLayer(idx))
        if layer is None:
            continue

        if isinstance(layer, QgsSimpleLineSymbolLayer):
            offset = abs(_to_float(_safe_value(lambda l=layer: l.offset(), 0.0), 0.0))
            width = _to_float(_safe_value(lambda l=layer: l.width(), 0.0), 0.0)
            if offset > 0:
                has_offset_line = True
            expand_mm = max(expand_mm, offset + width / 2.0)
            continue

        if isinstance(layer, QgsMarkerLineSymbolLayer):
            has_marker_line = True
            sub_symbol = _safe_value(lambda l=layer: l.subSymbol())
            if sub_symbol is None:
                continue
            sub_count = int(_safe_value(lambda s=sub_symbol: s.symbolLayerCount(), 0) or 0)
            for j in range(sub_count):
                sub_layer = _safe_value(lambda idx=j, s=sub_symbol: s.symbolLayer(idx))
                if not isinstance(sub_layer, QgsSvgMarkerSymbolLayer):
                    continue
                size_mm = _to_float(_safe_value(lambda sl=sub_layer: sl.size(), 0.0), 0.0)
                expand_mm = max(expand_mm, size_mm / 2.0)

    # 优先使用显式角色标记；对历史数据按符号结构兜底识别
    if role != "decorative_border" and not (has_marker_line and has_offset_line):
        return None

    if expand_mm <= 0:
        return None

    x = _to_float(rect_mm.get("x"), 0.0) - expand_mm
    y = _to_float(rect_mm.get("y"), 0.0) - expand_mm
    width = _to_float(rect_mm.get("width"), 0.0) + 2.0 * expand_mm
    height = _to_float(rect_mm.get("height"), 0.0) + 2.0 * expand_mm
    return {
        "x": x,
        "y": y,
        "width": width,
        "height": height,
    }


def _legend_styles():
    return [
        QgsLegendStyle.Title,
        QgsLegendStyle.Group,
        QgsLegendStyle.Subgroup,
        QgsLegendStyle.SymbolLabel,
    ]


def _apply_legend_text_format(legend, styles, text_format: QgsTextFormat):
    for style in styles:
        style_obj = legend.style(style)
        style_obj.setTextFormat(text_format)
        legend.setStyle(style, style_obj)


def _apply_legend_margin(legend, margin: float):
    set_style_margin = getattr(legend, "setStyleMargin", None)
    if callable(set_style_margin):
        try:
            for style in _legend_styles():
                set_style_margin(style, margin)
            return
        except Exception:
            logger.debug("legend.setStyleMargin failed, skipping margin update")


def _font_lock_error(item, params, size_keys):
    if item.customProperty("autocarto/font_locked", False) and any(_has_value(params, key) for key in size_keys) and params.get("unlock_font") is not True:
        return _result(False, "", "该元素字号由版式锁定，如需修改请传 unlock_font=true")
    return None


def _updated_text_format(current, params, *, prefix=""):
    result = QgsTextFormat(current)
    font = QFont(result.font())
    family_key, size_key = prefix+"font_family", prefix+"font_size"
    if _has_value(params, family_key):
        font.setFamily(str(params[family_key]))
    for key, setter in (("font_bold", font.setBold), ("font_italic", font.setItalic)):
        if params.get(prefix+key) is not None:
            setter(bool(params[prefix+key]))
    result.setFont(font)
    if _has_value(params, size_key):
        size = float(params[size_key])
        if not math.isfinite(size) or size <= 0:
            raise ValueError("字号必须是有限正数（毫米）")
        result.setSize(size)
        result.setSizeUnit(Qgis.RenderUnit.Millimeters)
    if _has_value(params, prefix+"font_color"):
        result.setColor(_resolve_color(params[prefix+"font_color"], "#202020"))
    return result


def _is_extra_legend_layer(layer):
    return bool(layer and (layer.customProperty("autocarto/legend_extra_item", False)
                          or layer.customProperty("autocarto/military_arrow_legend", False)
                          or layer.customProperty("autocarto/compose_legend_layout", "")))


def _prepare_legend_extras(params, current_layers, project):
    from autocarto.tools.system._shared.resource_handle import legend_extra_spec
    explicit = params.get("legend_extra_items") is not None
    extras = params.get("legend_extra_items") if explicit else []
    if not isinstance(extras, list):
        raise ValueError("legend_extra_items 必须是额外条目数组")
    normalized = []
    for extra in extras:
        path, size, label = legend_extra_spec(extra, params.get("legend_symbols"))
        key = json.dumps({"label": extra["label"], "image_path": str(path), "size_mm": size}, ensure_ascii=False, sort_keys=True)
        normalized.append((key, path, size, label))
    layers = [] if explicit else [layer for layer in current_layers if _is_extra_legend_layer(layer)
                                  and not layer.customProperty("autocarto/military_arrow_legend", False)
                                  and not layer.customProperty("autocarto/legend_compat_arrow", False)]
    created = []
    try:
        for key, path, size, label in normalized:
            layer = next((layer for layer in project.mapLayers().values() if layer.customProperty("autocarto/legend_extra_item", "") == key), None)
            if layer is None:
                marker = QgsSvgMarkerSymbolLayer(str(path)) if path.suffix.lower() == ".svg" else QgsRasterMarkerSymbolLayer(str(path))
                marker.setSize(size)
                marker.setSizeUnit(Qgis.RenderUnit.Millimeters)
                symbol = QgsMarkerSymbol()
                symbol.changeSymbolLayer(0, marker)
                layer = QgsVectorLayer("Point", label, "memory")
                layer.setRenderer(QgsSingleSymbolRenderer(symbol))
                layer.setCustomProperty("autocarto/legend_extra_item", key)
                project.addMapLayer(layer, False)
                created.append(layer.id())
            if layer not in layers:
                layers.append(layer)
        return layers
    except Exception:
        for layer_id in created:
            project.removeMapLayer(layer_id)
        raise


def set_legend_style_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout, item, error = _get_layout_item(
        params.get("layout"), params.get("item_id"), QgsLayoutItemLegend
    )
    if error:
        return error
    legend = item
    locked = _font_lock_error(legend, params, ("title_font_size", "body_font_size"))
    if locked:
        return locked
    try:
        title_style = legend.style(QgsLegendStyle.Title)
        body_style = legend.style(QgsLegendStyle.SymbolLabel)
        title_format = _updated_text_format(title_style.textFormat(), params, prefix="title_")
        body_format = _updated_text_format(body_style.textFormat(), params, prefix="body_")
    except (TypeError, ValueError) as exc:
        return _result(False, "", f"错误：{exc}")

    project = QgsProject.instance()
    root = legend.model().rootGroup()
    current = [node.layer() for node in root.findLayers() if node.layer() is not None]
    selected = [layer for layer in current if not _is_extra_legend_layer(layer)]
    if params.get("legend_layers") is not None:
        if not isinstance(params["legend_layers"], list):
            return _result(False, "", "错误：legend_layers 必须为图层 ID 或唯一名称数组")
        selected = []
        for ref in params["legend_layers"]:
            layer = project.mapLayer(str(ref))
            if layer is None:
                matches = project.mapLayersByName(str(ref))
                if len(matches) != 1:
                    return _result(False, "", f"错误：图层“{ref}”不存在或名称不唯一，请使用图层 ID")
                layer = matches[0]
            if layer not in selected:
                selected.append(layer)
    try:
        extras = _prepare_legend_extras(params, current, project)
    except (TypeError, ValueError) as exc:
        return _result(False, "", f"错误：{exc}")
    if params.get("legend_layers") is not None or params.get("legend_extra_items") is not None or extras or any(_is_extra_legend_layer(layer) for layer in current):
        legend.setAutoUpdateModel(False)
        root = legend.model().rootGroup()
        root.removeAllChildren()
        for layer in dict.fromkeys(selected+extras):
            root.addLayer(layer)

    if _has_value(params, "title"):
        legend.setTitle(str(params["title"]))
    if any(_has_value(params, key) for key in ("title_font_family", "title_font_size")):
        _apply_legend_text_format(legend, [QgsLegendStyle.Title], title_format)
    if any(_has_value(params, key) for key in ("body_font_family", "body_font_size")):
        for kind in (QgsLegendStyle.Group, QgsLegendStyle.Subgroup, QgsLegendStyle.SymbolLabel):
            current_style = legend.style(kind)
            current_format = _updated_text_format(current_style.textFormat(), params, prefix="body_")
            _apply_legend_text_format(legend, [kind], current_format)

    if _has_value(params, "content_margin"):
        _apply_legend_margin(legend, float(params.get("content_margin")))

    if _has_value(params, "symbol_size"):
        symbol_size = float(params.get("symbol_size"))
        _call_if_supported(legend, "setSymbolWidth", symbol_size)
        _call_if_supported(legend, "setSymbolHeight", symbol_size)

    if "background_enabled" in params:
        legend.setBackgroundEnabled(params.get("background_enabled", True))
    if _has_value(params, "background_color"):
        legend.setBackgroundColor(
            _resolve_color(params.get("background_color"), "#ffffff")
        )
    legend.setFrameEnabled(True)
    if _has_value(params, "frame_color"):
        legend.setFrameStrokeColor(_resolve_color(params.get("frame_color"), "#000000"))
    if _has_value(params, "frame_width"):
        _call_if_supported(
            legend, "setFrameStrokeWidth", float(params.get("frame_width"))
        )
    _call_if_supported(legend, "setTitleAlignment", Qt.AlignHCenter)

    legend.updateLegend()
    QgsLayoutExporter(layout).renderPageToImage(0)
    QgsApplication.processEvents()
    if legend.resizeToContents():
        legend.adjustBoxSize()
    refresh_canvas()
    return _result(True, legend.id(), "图例样式已更新", _item_properties(legend))


def set_scalebar_style_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout, item, error = _get_layout_item(
        params.get("layout"), params.get("item_id"), QgsLayoutItemScaleBar
    )
    if error:
        return error
    scalebar = item
    locked = _font_lock_error(scalebar, params, ("font_size",))
    if locked:
        return locked
    try:
        text_format = _updated_text_format(scalebar.textFormat(), params)
    except (TypeError, ValueError) as exc:
        return _result(False, "", f"错误：{exc}")
    if _has_value(params, "style"):
        style_map = {
            "single_box": "Single Box",
            "double_box": "Double Box",
            "line": "Line Ticks Middle",
            "numeric": "Single Box",
            "ratio": "Numeric",
        }
        style_name = style_map.get(str(params.get("style")).strip().lower(), None)
        if style_name:
            _call_if_supported(scalebar, "setStyle", style_name)

    _call_if_supported(scalebar, "setNumberOfSegments", 4)
    _call_if_supported(scalebar, "setNumberOfSegmentsLeft", 0)
    if any(_has_value(params, key) for key in ("font_family", "font_size", "font_bold", "font_italic", "font_color")):
        scalebar.setTextFormat(text_format)
    _call_if_supported(scalebar, "update")
    _call_if_supported(scalebar, "refresh")
    refresh_canvas()
    return _result(True, scalebar.id(), "比例尺样式已更新", _item_properties(scalebar))


def set_north_arrow_style_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout, item, error = _get_layout_item(
        params.get("layout"), params.get("item_id"), QgsLayoutItemPicture
    )
    if error:
        return error
    picture = item

    if _has_value(params, "style"):
        arrow_styles = [
            "arrows/NorthArrow_10.svg",
            "arrows/NorthArrow_02.svg",
            "arrows/NorthArrow_03.svg",
            "arrows/NorthArrow_04.svg",
            "arrows/NorthArrow_05.svg",
            "arrows/NorthArrow_06.svg",
            "arrows/NorthArrow_07.svg",
            "arrows/NorthArrow_08.svg",
        ]
        try:
            style_idx = int(params.get("style"))
        except Exception:
            return _result(False, "", "错误：style 必须为 0-7 的整数")
        if style_idx < 0 or style_idx >= len(arrow_styles):
            return _result(False, "", "错误：style 必须为 0-7 的整数")

        svg_paths = QgsApplication.svgPaths()
        north_arrow_path = None
        for svg_path in svg_paths:
            candidate = os.path.join(svg_path, arrow_styles[style_idx])
            if os.path.exists(candidate):
                north_arrow_path = candidate
                break

        if not north_arrow_path:
            return _result(False, "", f"错误：未找到 style={style_idx} 的指北针 SVG")

        picture.setPicturePath(north_arrow_path)
        _call_if_supported(picture, "refresh")
        refresh_canvas()
        return _result(True, picture.id(), "指北针样式已更新", _item_properties(picture))

    return _result(False, "", "错误：请提供 style 参数")


def get_item_properties_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout, item, error = _get_layout_item(params.get("layout"), params.get("item_id"))
    if error:
        return error
    data = _item_properties(item)   
    return _result(True, item.id(), "获取布局元素属性成功", data)


def get_items_properties_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout_name = str(params.get("layout") or "").strip()
    item_ids = params.get("item_ids")
    if not layout_name:
        return _result(False, "", "错误：缺少布局名称参数")
    if item_ids is not None and (not isinstance(item_ids, list) or not item_ids):
        return _result(False, "", "错误：item_ids 必须是非空元素 ID 数组")

    layout = _get_layout(layout_name)
    if not layout:
        return _result(False, "", f"错误：未找到布局 '{layout_name}'")
    objects = {}
    for item in layout.items():
        if not isinstance(item, QgsLayoutItem) or isinstance(item, QgsLayoutItemPage):
            continue
        item_id = str(item.id() or "").strip()
        if not item_id or item_id in objects:
            item_id = "layout_item_"+uuid.uuid4().hex
            item.setId(item_id)
        objects[item_id] = (item, None)
        if isinstance(item, QgsLayoutItemMap):
            for grid in item.grids().asList():
                objects[grid.id()] = (grid, item)
    normalized_ids = []
    for value in list(objects) if item_ids is None else item_ids:
        item_id = str(value or "").strip()
        if item_id and item_id not in normalized_ids:
            normalized_ids.append(item_id)
    if not normalized_ids:
        return _result(False, "", "错误：item_ids 中没有有效元素 ID")

    properties: Dict[str, Any] = {}
    missing_ids = []
    grouped_ids = {"standard": {}, "callouts": {}}
    def register(mapping, role, item_id):
        if role not in mapping:
            mapping[role] = item_id
        elif isinstance(mapping[role], list):
            mapping[role].append(item_id)
        else:
            mapping[role] = [mapping[role], item_id]
    for item_id in normalized_ids:
        found = objects.get(item_id)
        if found is None:
            missing_ids.append(item_id)
            continue
        item, parent = found
        properties[item_id] = _item_properties(item, parent)
        group = str(item.customProperty("autocarto/callout_group", "") or "").strip()
        if group:
            group_items = grouped_ids["callouts"].setdefault(group, {})
            role = str(item.customProperty("autocarto/callout_role", "") or "items")
            if role == "items":
                group_items.setdefault("items", []).append(item_id)
            else:
                register(group_items, role, item_id)
        else:
            role = str(item.customProperty("autocarto/compose_role", "") or item_id)
            register(grouped_ids["standard"], role, item_id)

    page_size = {"width": 0.0, "height": 0.0}
    page_collection = _safe_value(lambda: layout.pageCollection())
    if page_collection is not None and _safe_value(
        lambda: page_collection.pageCount(), 0
    ):
        page = _safe_value(lambda: page_collection.page(0))
        size = _safe_value(lambda: page.pageSize()) if page is not None else None
        if size is not None:
            page_size = {
                "width": _item_rect(page)["width"],
                "height": _item_rect(page)["height"],
            }

    data = {
        "layout": layout_name,
        "page_size_mm": page_size,
        "items": properties,
        "missing_item_ids": missing_ids,
    }
    if missing_ids:
        return _result(
            False,
            "",
            "错误：部分布局元素不存在: " + ", ".join(missing_ids),
            data,
        )

    layout_item_ids = params.get("layout_item_ids")
    if layout_item_ids is None:
        layout_item_ids = grouped_ids
    if layout_item_ids is not None:
        if not isinstance(layout_item_ids, Mapping) or not layout_item_ids:
            return _result(
                False,
                "",
                "错误：layout_item_ids 必须是非空对象",
                data,
            )
        try:
            snapshot = _write_layout_snapshot(
                params,
                layout_name=layout_name,
                layout_item_ids=layout_item_ids,
                layout_item_state=data,
            )
        except Exception as exc:
            return _result(False, "", f"错误：保存布局快照失败：{exc}", data)
        data.update(snapshot)
        data["layout_item_ids"] = dict(layout_item_ids)

    return _result(
        True,
        layout_name,
        f"已批量获取 {len(properties)} 个布局元素属性",
        data,
    )


def set_item_background_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout, item, error = _get_layout_item(params.get("layout"), params.get("item_id"))
    if error:
        return error
    color = _resolve_color(params.get("color"), "#ffffff")
    raw_opacity = params.get("opacity", 1.0)
    opacity = 1.0 if raw_opacity in (None, "") else _to_float(raw_opacity, 1.0)
    color.setAlphaF(max(0.0, min(1.0, opacity)))
    item.setBackgroundEnabled(bool(params.get("enabled", True)))
    item.setBackgroundColor(color)
    refresh_canvas()
    return _result(True, item.id(), "布局元素背景已更新", _item_properties(item))


def set_item_frame_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout, item, error = _get_layout_item(params.get("layout"), params.get("item_id"))
    if error:
        return error
    item.setFrameEnabled(bool(params.get("enabled", True)))
    item.setFrameStrokeColor(_resolve_color(params.get("color"), "#000000"))
    item.setFrameStrokeWidth(float(params.get("width", 0.3) or 0.3))
    refresh_canvas()
    return _result(True, item.id(), "布局元素边框已更新", _item_properties(item))


def set_item_rotation_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout, item, error = _get_layout_item(params.get("layout"), params.get("item_id"))
    if error:
        return error
    rotation = float(params.get("rotation", 0) or 0)
    if hasattr(item, "setItemRotation"):
        item.setItemRotation(rotation)
    else:
        item.setRotation(rotation)
    refresh_canvas()
    return _result(True, item.id(), "布局元素旋转角度已更新", _item_properties(item))


def set_label_style_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout, item, error = _get_layout_item(
        params.get("layout"), params.get("item_id"), QgsLayoutItemLabel
    )
    if error:
        return error
    label = item
    locked = _font_lock_error(label, params, ("font_size",))
    if locked:
        return locked
    try:
        text_format = _updated_text_format(label.textFormat(), params)
    except (TypeError, ValueError) as exc:
        return _result(False, "", f"错误：{exc}")
    if params.get("text") is not None:
        label.setText(str(params.get("text")))
    if any(_has_value(params, key) for key in ("font_family", "font_size", "font_bold", "font_italic", "font_color")):
        label.setTextFormat(text_format)
    if params.get("background_color"):
        label.setBackgroundEnabled(True)
        label.setBackgroundColor(
            _resolve_color(params.get("background_color"), "#ffffff")
        )
    if "frame_enabled" in params:
        label.setFrameEnabled(params.get("frame_enabled", True))
    if _has_value(params, "frame_color"):
        label.setFrameStrokeColor(_resolve_color(params.get("frame_color"), "#000000"))
    if _has_value(params, "frame_width"):
        _call_if_supported(label, "setFrameStrokeWidth", float(params.get("frame_width")))
    if _has_value(params, "width"):
        try:
            width = float(params.get("width"))
        except (TypeError, ValueError):
            return _result(False, "", "错误：width 参数必须是数字（单位 mm）")
        if width <= 0:
            return _result(False, "", "错误：width 参数必须大于 0（单位 mm）")
        label.setText(
            _wrap_text_to_width(
                label.text(),
                label.textFormat(),
                width,
                _layout_dpi(layout),
            )
        )
    _call_if_supported(label, "adjustSizeToText")
    refresh_canvas()
    return _result(True, label.id(), "标签样式已更新", _item_properties(label))


def set_map_rotation_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout, item, error = _get_layout_item(
        params.get("layout"), params.get("item_id"), QgsLayoutItemMap
    )
    if error:
        return error
    item.setMapRotation(float(params.get("rotation", 0) or 0))
    refresh_canvas()
    return _result(True, item.id(), "地图框旋转角度已更新", _item_properties(item))

def set_map_layers_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout, item, error = _get_layout_item(
        params.get("layout"), params.get("item_id"), QgsLayoutItemMap
    )
    if error:
        return error
    layer_names = params.get("layers") or []
    if not isinstance(layer_names, list) or not layer_names:
        return _result(False, "", "错误：layers 必须为非空数组")
    layers = []
    missing = []
    for name in layer_names:
        layer = QgsProject.instance().mapLayer(str(name))
        if layer is None:
            matched = QgsProject.instance().mapLayersByName(str(name))
            if len(matched) != 1:
                missing.append(str(name))
                continue
            layer = matched[0]
        if layer not in layers:
            layers.append(layer)
    if missing:
        return _result(False, "", f"错误：图层不存在或名称不唯一 {missing}，请使用图层 ID")
    item.setKeepLayerSet(True)
    item.setLayers(layers)
    refresh_canvas()
    return _result(True, item.id(), "地图框图层列表已更新", _item_properties(item))


def export_image_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    output_manager.sync_session_context_from_params(params)
    layout_name = str(
        params.get("LAYOUT") or params.get("layout") or params.get("name") or ""
    )
    output = params.get("OUTPUT") or params.get("output")
    image_format = str(params.get("format") or "png").strip().lower()
    extension = ".jpg" if image_format in {"jpg", "jpeg"} else ".png"
    layout = _get_layout(layout_name)
    if not layout:
        return _result(False, "", f"错误：未找到布局 '{layout_name}'")
    validation_error = _validate_layout_for_export(layout)
    if validation_error:
        return _result(False, "", f"导出前校验失败：{validation_error}")
    try:
        dpi, purpose = _image_export_dpi(layout, params)
    except (TypeError, ValueError) as exc:
        return _result(False, "", f"导出前校验失败：{exc}")
    output_path = _resolve_export_path(output, f"{layout_name}{extension}", extension)
    exporter = ArtifactLayoutExporter(layout)
    settings = QgsLayoutExporter.ImageExportSettings()
    settings.dpi = dpi
    result = exporter.exportToImage(output_path, settings)
    if result != QgsLayoutExporter.Success:
        return _result(False, "", f"布局导出图片失败：{_export_result_message(result)}")
    try:
        paths = exporter.artifact_paths(output_path)
    except ValueError as exc:
        return _result(False, "", str(exc))
    refresh_canvas()
    result = _result(True, paths[0], "布局图片导出成功", {"dpi": dpi, "purpose": purpose})
    result["artifact_paths"] = paths
    return result


def export_svg_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    output_manager.sync_session_context_from_params(params)
    layout_name = str(
        params.get("LAYOUT") or params.get("layout") or params.get("name") or ""
    )
    output = params.get("OUTPUT") or params.get("output")
    dpi = params.get("DPI") or params.get("dpi")
    layout = _get_layout(layout_name)
    if not layout:
        return _result(False, "", f"错误：未找到布局 '{layout_name}'")
    validation_error = _validate_layout_for_export(layout)
    if validation_error:
        return _result(False, "", f"导出前校验失败：{validation_error}")
    output_path = _resolve_export_path(output, f"{layout_name}.svg", ".svg")
    exporter = ArtifactLayoutExporter(layout)
    settings = QgsLayoutExporter.SvgExportSettings()
    if dpi is not None:
        settings.dpi = float(dpi)
    if params.get("export_as_layers") is not None:
        settings.exportAsLayers = bool(params.get("export_as_layers"))
    result = exporter.exportToSvg(output_path, settings)
    if result != QgsLayoutExporter.Success:
        return _result(False, "", f"布局导出SVG失败：{_export_result_message(result)}")
    try:
        paths = exporter.artifact_paths(output_path)
    except ValueError as exc:
        return _result(False, "", str(exc))
    refresh_canvas()
    result = _result(True, paths[0], "布局 SVG 导出成功")
    result["artifact_paths"] = paths
    return result


def set_page_background_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout_name = str(params.get("layout") or "")
    layout = _get_layout(layout_name)
    if not layout:
        return _result(False, "", f"错误：未找到布局 '{layout_name}'")
    page_collection = layout.pageCollection()
    if page_collection.pageCount() == 0:
        return _result(False, "", "错误：布局没有页面")
    page = page_collection.page(0)
    symbol = QgsFillSymbol.createSimple(
        {
            "color": _resolve_color(params.get("color"), "#ffffff").name(),
            "outline_color": _resolve_color(
                params.get("outline_color"), "#000000"
            ).name(),
            "outline_width": str(params.get("outline_width", 0.1) or 0.1),
        }
    )
    page.setPageStyleSymbol(symbol)
    refresh_canvas()
    return _result(True, layout_name, "页面背景已更新")


def distribute_items_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout_name = str(params.get("layout") or "")
    layout = _get_layout(layout_name)
    if not layout:
        return _result(False, "", f"错误：未找到布局 '{layout_name}'")
    item_ids = params.get("item_ids") or []
    direction = str(params.get("direction") or "horizontal").strip().lower()
    if not isinstance(item_ids, list) or len(item_ids) < 2:
        return _result(False, "", "错误：item_ids 至少需要两个元素")
    items = []
    for item_id in item_ids:
        item = layout.itemById(str(item_id))
        if item is None:
            return _result(False, "", f"错误：未找到元素 '{item_id}'")
        items.append(item)
    horizontal = direction != "vertical"
    items.sort(
        key=lambda item: item.sceneRect().x() if horizontal else item.sceneRect().y()
    )
    first_rect = items[0].sceneRect()
    last_rect = items[-1].sceneRect()
    total_span = (
        (last_rect.x() + last_rect.width() - first_rect.x())
        if horizontal
        else (last_rect.y() + last_rect.height() - first_rect.y())
    )
    occupied = sum(
        item.sceneRect().width() if horizontal else item.sceneRect().height()
        for item in items
    )
    spacing = params.get("spacing")
    if spacing is None:
        spacing = (
            0
            if len(items) <= 1
            else max(0.0, (total_span - occupied) / (len(items) - 1))
        )
    spacing = float(spacing)
    cursor = first_rect.x() if horizontal else first_rect.y()
    for item in items:
        rect = item.sceneRect()
        if horizontal:
            item.attemptMove(
                QgsLayoutPoint(cursor, rect.y(), QgsUnitTypes.LayoutMillimeters)
            )
            cursor += rect.width() + spacing
        else:
            item.attemptMove(
                QgsLayoutPoint(rect.x(), cursor, QgsUnitTypes.LayoutMillimeters)
            )
            cursor += rect.height() + spacing
    refresh_canvas()
    return _result(True, layout_name, "布局元素已完成等间距分布")


def group_items_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout_name = str(params.get("layout") or "")
    layout = _get_layout(layout_name)
    if not layout:
        return _result(False, "", f"错误：未找到布局 '{layout_name}'")
    item_ids = params.get("item_ids") or []
    if not isinstance(item_ids, list) or len(item_ids) < 2:
        return _result(False, "", "错误：item_ids 至少需要两个元素")
    items = []
    for item_id in item_ids:
        item = layout.itemById(str(item_id))
        if item is None:
            return _result(False, "", f"错误：未找到元素 '{item_id}'")
        items.append(item)
    group = layout.groupItems(items)
    if group is None:
        return _result(False, "", "错误：元素分组失败")
    group_id = str(params.get("group_id") or f"group_{len(item_ids)}")
    group.setId(group_id)
    refresh_canvas()
    return _result(True, group_id, "布局元素已分组", _item_properties(group))


def ungroup_items_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout, item, error = _get_layout_item(
        params.get("layout"), params.get("item_id"), QgsLayoutItemGroup
    )
    if error:
        return error
    ungrouped = layout.ungroupItems(item)
    if not ungrouped:
        return _result(False, "", "错误：取消分组失败")
    refresh_canvas()
    return _result(True, params.get("item_id"), "布局元素已取消分组")


def set_item_z_order_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    layout, item, error = _get_layout_item(params.get("layout"), params.get("item_id"))
    if error:
        return error
    item.setZValue(float(params.get("z_order", 0) or 0))
    refresh_canvas()
    return _result(True, item.id(), "布局元素层级已更新", _item_properties(item))


def _wrap(handler):
    def wrapped(params: Dict[str, Any]) -> Dict[str, Any]:
        return run_in_main_thread(lambda: handler(params))

    wrapped.__name__ = handler.__name__
    return wrapped


set_item_background_handler = _wrap(set_item_background_handler)
set_item_frame_handler = _wrap(set_item_frame_handler)
set_item_rotation_handler = _wrap(set_item_rotation_handler)
set_legend_style_handler = _wrap(set_legend_style_handler)
set_scalebar_style_handler = _wrap(set_scalebar_style_handler)
set_north_arrow_style_handler = _wrap(set_north_arrow_style_handler)
set_label_style_handler = _wrap(set_label_style_handler)
set_map_rotation_handler = _wrap(set_map_rotation_handler)
set_map_layers_handler = _wrap(set_map_layers_handler)
get_item_properties_handler = _wrap(get_item_properties_handler)
get_items_properties_handler = _wrap(get_items_properties_handler)
export_image_handler = _wrap(export_image_handler)
export_svg_handler = _wrap(export_svg_handler)
set_page_background_handler = _wrap(set_page_background_handler)
distribute_items_handler = _wrap(distribute_items_handler)
group_items_handler = _wrap(group_items_handler)
ungroup_items_handler = _wrap(ungroup_items_handler)
set_item_z_order_handler = _wrap(set_item_z_order_handler)
