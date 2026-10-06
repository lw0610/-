"""Safe, atomic movement of composite layout callouts.

A callout created by the marching-route cartography tools is composed of a
shape, optional picture/title/body items, and one or more leader polylines.
Moving those items as a normal QGIS group would also move the geographic
anchor at the far end of every leader.  This module instead translates only
the visual box contents and rebuilds the leader geometry while preserving the
anchor point in layout coordinates.
"""

from __future__ import annotations

import math
from typing import Any, Dict, List, Mapping, Optional, Sequence, Tuple

from qgis.PyQt.QtCore import QPointF
from qgis.PyQt.QtGui import QPolygonF
from qgis.core import (
    QgsLayoutItem,
    QgsLayoutItemLabel,
    QgsLayoutItemPicture,
    QgsLayoutItemPolyline,
    QgsLayoutItemShape,
    QgsProject,
)

from autocarto.tools.system._shared.canvas_refresh import refresh_canvas
from autocarto.tools.system._shared.layout_runtime import run_in_main_thread


Point = Tuple[float, float]
Rect = Tuple[float, float, float, float]
_TOLERANCE_MM = 0.05


def _result(
    success: bool,
    *,
    output: str = "",
    message: str,
    error_code: str = "",
    data: Optional[Dict[str, Any]] = None,
) -> Dict[str, Any]:
    """Return a complete tool envelope so failure details survive every layer."""

    return {
        "success": bool(success),
        "output": output,
        "info": message,
        "message": message,
        "error": "" if success else message,
        "error_code": "" if success else error_code,
        "output_layers": [],
        "data": data or {},
    }


def _finite_number(value: Any, field_name: str) -> float:
    if isinstance(value, bool):
        raise ValueError(f"{field_name} 必须是有限数字")
    try:
        parsed = float(value)
    except (TypeError, ValueError) as exc:
        raise ValueError(f"{field_name} 必须是有限数字") from exc
    if not math.isfinite(parsed):
        raise ValueError(f"{field_name} 必须是有限数字")
    return parsed


def _item_top_left(item: QgsLayoutItem) -> Point:
    point = item.positionAtReferencePoint(QgsLayoutItem.ReferencePoint.UpperLeft)
    return float(point.x()), float(point.y())


def _item_rect(item: QgsLayoutItem) -> Rect:
    x, y = _item_top_left(item)
    size = item.sizeWithUnits()
    return x, y, float(size.width()), float(size.height())


def _rect_dict(rect: Rect) -> Dict[str, float]:
    return {
        "x": float(rect[0]),
        "y": float(rect[1]),
        "width": float(rect[2]),
        "height": float(rect[3]),
    }


def _scene_nodes(line: QgsLayoutItemPolyline) -> List[Point]:
    origin_x, origin_y = _item_top_left(line)
    return [
        (origin_x + float(node.x()), origin_y + float(node.y()))
        for node in line.nodes()
    ]


def _set_scene_nodes(line: QgsLayoutItemPolyline, nodes: Sequence[Point]) -> None:
    if len(nodes) < 2:
        raise ValueError(f"牵引线 '{line.id()}' 至少需要两个节点")
    origin_x, origin_y = _item_top_left(line)
    local_nodes = QPolygonF(
        [QPointF(x - origin_x, y - origin_y) for x, y in nodes]
    )
    line.setNodes(local_nodes)


def _distance(first: Point, second: Point) -> float:
    return math.hypot(second[0] - first[0], second[1] - first[1])


def _polyline_length(nodes: Sequence[Point]) -> float:
    return sum(_distance(nodes[index - 1], nodes[index]) for index in range(1, len(nodes)))


def _point_inside_rect(point: Point, rect: Rect, tolerance: float = 0.0) -> bool:
    x, y, width, height = rect
    return (
        x - tolerance <= point[0] <= x + width + tolerance
        and y - tolerance <= point[1] <= y + height + tolerance
    )


def _rect_within(container: Rect, candidate: Rect, margin: float) -> bool:
    cx, cy, cw, ch = container
    x, y, width, height = candidate
    return (
        x >= cx + margin - _TOLERANCE_MM
        and y >= cy + margin - _TOLERANCE_MM
        and x + width <= cx + cw - margin + _TOLERANCE_MM
        and y + height <= cy + ch - margin + _TOLERANCE_MM
    )


def _boundary_hit(rect: Rect, anchor: Point) -> Tuple[str, Point]:
    """Intersect the center-to-anchor ray with the callout box boundary."""

    x, y, width, height = rect
    center_x = x + width * 0.5
    center_y = y + height * 0.5
    dx = anchor[0] - center_x
    dy = anchor[1] - center_y
    if abs(dx) <= 1e-9 and abs(dy) <= 1e-9:
        raise ValueError("地图锚点与标注框中心重合，无法重建牵引线")

    hits: List[Tuple[float, str, Point]] = []
    if abs(dx) > 1e-9:
        for edge_x, edge_name in ((x, "left"), (x + width, "right")):
            factor = (edge_x - center_x) / dx
            hit_y = center_y + factor * dy
            if factor > 0 and y - _TOLERANCE_MM <= hit_y <= y + height + _TOLERANCE_MM:
                hits.append((factor, edge_name, (edge_x, hit_y)))
    if abs(dy) > 1e-9:
        for edge_y, edge_name in ((y, "top"), (y + height, "bottom")):
            factor = (edge_y - center_y) / dy
            hit_x = center_x + factor * dx
            if factor > 0 and x - _TOLERANCE_MM <= hit_x <= x + width + _TOLERANCE_MM:
                hits.append((factor, edge_name, (hit_x, edge_y)))

    if not hits:
        raise ValueError("无法计算标注框与地图锚点之间的牵引线交点")
    _, edge, point = min(hits, key=lambda value: value[0])
    return edge, point


def _double_starts(rect: Rect, anchor: Point, old_starts: Sequence[Point]) -> List[Point]:
    edge, hit = _boundary_hit(rect, anchor)
    x, y, width, height = rect
    old_gap = _distance(old_starts[0], old_starts[1]) if len(old_starts) >= 2 else 0.0
    default_gap = max(1.5, min(8.0, min(width, height) * 0.2))
    gap = old_gap if old_gap > _TOLERANCE_MM else default_gap

    if edge in {"left", "right"}:
        max_gap = max(0.5, height - 1.0)
        gap = min(gap, max_gap)
        center = min(max(hit[1], y + gap * 0.5), y + height - gap * 0.5)
        return [(hit[0], center - gap * 0.5), (hit[0], center + gap * 0.5)]

    max_gap = max(0.5, width - 1.0)
    gap = min(gap, max_gap)
    center = min(max(hit[0], x + gap * 0.5), x + width - gap * 0.5)
    return [(center - gap * 0.5, hit[1]), (center + gap * 0.5, hit[1])]


def _curved_nodes(start: Point, anchor: Point) -> List[Point]:
    """Recreate the same quadratic curve convention used by callout creation."""

    control_x = (start[0] + anchor[0]) * 0.5
    control_y = (start[1] + anchor[1]) * 0.5 - max(
        2.0,
        min(10.0, abs(anchor[0] - start[0]) * 0.15 + abs(anchor[1] - start[1]) * 0.15),
    )
    nodes: List[Point] = []
    segment_count = 14
    for index in range(segment_count + 1):
        ratio = index / float(segment_count)
        inverse = 1.0 - ratio
        nodes.append(
            (
                inverse * inverse * start[0]
                + 2 * inverse * ratio * control_x
                + ratio * ratio * anchor[0],
                inverse * inverse * start[1]
                + 2 * inverse * ratio * control_y
                + ratio * ratio * anchor[1],
            )
        )
    return nodes


def _normalize_callout_ids(value: Any) -> Tuple[Dict[str, str], List[str]]:
    if not isinstance(value, Mapping):
        raise ValueError("callout_item_ids 必须是包含 box/image/title/body/leader 的对象")

    normalized: Dict[str, str] = {}
    box_id = str(value.get("box") or "").strip()
    if not box_id:
        raise ValueError("callout_item_ids.box 不能为空")
    normalized["box"] = box_id

    for role in ("image", "title", "body"):
        item_id = str(value.get(role) or "").strip()
        if item_id:
            normalized[role] = item_id

    leader_value = value.get("leader")
    if leader_value in (None, "", []):
        leader_ids: List[str] = []
    elif isinstance(leader_value, str):
        leader_ids = [leader_value.strip()] if leader_value.strip() else []
    elif isinstance(leader_value, list):
        leader_ids = [str(item_id or "").strip() for item_id in leader_value]
        if any(not item_id for item_id in leader_ids):
            raise ValueError("callout_item_ids.leader 中不能包含空 ID")
    else:
        raise ValueError("callout_item_ids.leader 必须是字符串或字符串数组")

    all_ids = list(normalized.values()) + leader_ids
    if len(all_ids) != len(set(all_ids)):
        raise ValueError("callout_item_ids 中存在重复元素 ID")
    return normalized, leader_ids


def _move_callout(params: Dict[str, Any]) -> Dict[str, Any]:
    layout_name = str(params.get("layout") or "").strip()
    if not layout_name:
        return _result(False, message="缺少布局名称", error_code="layout_missing")

    try:
        target_x = _finite_number(params.get("x"), "x")
        target_y = _finite_number(params.get("y"), "y")
        margin = _finite_number(params.get("margin_mm", 0.0), "margin_mm")
        if margin < 0:
            raise ValueError("margin_mm 不能小于 0")
        component_ids, leader_ids = _normalize_callout_ids(params.get("callout_item_ids"))
    except ValueError as exc:
        return _result(False, message=str(exc), error_code="invalid_params")

    container_item_id = str(params.get("container_item_id") or "").strip()
    if not container_item_id:
        return _result(
            False,
            message="container_item_id 不能为空；请传入主地图框的真实元素 ID",
            error_code="invalid_params",
        )

    layout = QgsProject.instance().layoutManager().layoutByName(layout_name)
    if layout is None:
        return _result(
            False,
            message=f"未找到布局 '{layout_name}'",
            error_code="layout_not_found",
        )

    container_item = layout.itemById(container_item_id)
    if container_item is None:
        return _result(
            False,
            message=f"未找到容器元素 '{container_item_id}'",
            error_code="container_not_found",
        )

    expected_types = {
        "box": QgsLayoutItemShape,
        "image": QgsLayoutItemPicture,
        "title": QgsLayoutItemLabel,
        "body": QgsLayoutItemLabel,
    }
    components: Dict[str, QgsLayoutItem] = {}
    for role, item_id in component_ids.items():
        item = layout.itemById(item_id)
        if item is None:
            return _result(
                False,
                message=f"未找到图文标注元素 '{item_id}'",
                error_code="callout_item_not_found",
            )
        if not isinstance(item, expected_types[role]):
            return _result(
                False,
                message=f"图文标注元素 '{item_id}' 的类型与角色 {role} 不匹配",
                error_code="callout_item_type_mismatch",
            )
        components[role] = item

    leaders: List[QgsLayoutItemPolyline] = []
    for item_id in leader_ids:
        item = layout.itemById(item_id)
        if item is None:
            return _result(
                False,
                message=f"未找到牵引线元素 '{item_id}'",
                error_code="leader_not_found",
            )
        if not isinstance(item, QgsLayoutItemPolyline):
            return _result(
                False,
                message=f"元素 '{item_id}' 不是布局折线，不能作为牵引线移动",
                error_code="leader_type_mismatch",
            )
        leaders.append(item)

    all_items: List[QgsLayoutItem] = list(components.values()) + list(leaders)
    for item in all_items:
        if item.isGroupMember():
            return _result(
                False,
                message=f"元素 '{item.id()}' 已属于 QGIS 布局分组，请先取消分组",
                error_code="grouped_callout_not_supported",
            )
        if item.isLocked():
            return _result(
                False,
                message=f"元素 '{item.id()}' 已锁定，不能移动",
                error_code="callout_item_locked",
            )

    old_box_rect = _item_rect(components["box"])
    new_box_rect: Rect = (target_x, target_y, old_box_rect[2], old_box_rect[3])
    container_rect = _item_rect(container_item)
    if not _rect_within(container_rect, new_box_rect, margin):
        return _result(
            False,
            message="目标标注框超出指定容器范围",
            error_code="callout_outside_container",
            data={
                "target_box_rect": _rect_dict(new_box_rect),
                "container_rect": _rect_dict(container_rect),
                "margin_mm": margin,
            },
        )

    delta_x = target_x - old_box_rect[0]
    delta_y = target_y - old_box_rect[1]
    old_component_rects = {role: _item_rect(item) for role, item in components.items()}
    old_leader_nodes: Dict[str, List[Point]] = {}
    leader_plan: Dict[str, List[Point]] = {}
    leader_styles: Dict[str, str] = {}

    if leaders:
        for line in leaders:
            nodes = _scene_nodes(line)
            if len(nodes) < 2:
                return _result(
                    False,
                    message=f"牵引线 '{line.id()}' 节点不足，无法安全重建",
                    error_code="invalid_leader_geometry",
                )
            old_leader_nodes[line.id()] = nodes

        anchors = [nodes[-1] for nodes in old_leader_nodes.values()]
        anchor = anchors[0]
        if any(_distance(anchor, other) > _TOLERANCE_MM for other in anchors[1:]):
            return _result(
                False,
                message="同一图文标注的多条牵引线没有共同地图锚点",
                error_code="leader_anchor_mismatch",
            )
        if _point_inside_rect(anchor, new_box_rect, _TOLERANCE_MM):
            return _result(
                False,
                message="目标标注框覆盖了自身地图锚点，无法生成有效牵引线",
                error_code="anchor_inside_callout",
                data={"anchor": {"x": anchor[0], "y": anchor[1]}},
            )

        try:
            if len(leaders) == 2:
                old_starts = [old_leader_nodes[line.id()][0] for line in leaders]
                starts = _double_starts(new_box_rect, anchor, old_starts)
                for index, line in enumerate(leaders):
                    leader_plan[line.id()] = [starts[index], anchor]
                    leader_styles[line.id()] = "double"
            elif len(leaders) == 1:
                line = leaders[0]
                _, start = _boundary_hit(new_box_rect, anchor)
                if len(old_leader_nodes[line.id()]) > 2:
                    leader_plan[line.id()] = _curved_nodes(start, anchor)
                    leader_styles[line.id()] = "curved"
                else:
                    leader_plan[line.id()] = [start, anchor]
                    leader_styles[line.id()] = "straight"
            else:
                return _result(
                    False,
                    message="当前只支持零条、一条或两条牵引线的图文标注",
                    error_code="unsupported_leader_count",
                )
        except ValueError as exc:
            return _result(False, message=str(exc), error_code="invalid_leader_geometry")

    rollback_errors: List[str] = []
    try:
        for item in components.values():
            item.attemptMoveBy(delta_x, delta_y)

        for line in leaders:
            _set_scene_nodes(line, leader_plan[line.id()])

        actual_box_rect = _item_rect(components["box"])
        if (
            abs(actual_box_rect[0] - target_x) > _TOLERANCE_MM
            or abs(actual_box_rect[1] - target_y) > _TOLERANCE_MM
        ):
            raise RuntimeError("QGIS 未把标注框移动到请求位置")

        for role, item in components.items():
            old_rect = old_component_rects[role]
            new_rect = _item_rect(item)
            if (
                abs((new_rect[0] - old_rect[0]) - delta_x) > _TOLERANCE_MM
                or abs((new_rect[1] - old_rect[1]) - delta_y) > _TOLERANCE_MM
                or abs(new_rect[2] - old_rect[2]) > _TOLERANCE_MM
                or abs(new_rect[3] - old_rect[3]) > _TOLERANCE_MM
            ):
                raise RuntimeError(f"元素 '{item.id()}' 未保持整组相对位置或尺寸")

        for line in leaders:
            actual_nodes = _scene_nodes(line)
            expected_nodes = leader_plan[line.id()]
            if len(actual_nodes) != len(expected_nodes):
                raise RuntimeError(f"牵引线 '{line.id()}' 节点数校验失败")
            if any(
                _distance(actual, expected) > _TOLERANCE_MM
                for actual, expected in zip(actual_nodes, expected_nodes)
            ):
                raise RuntimeError(f"牵引线 '{line.id()}' 几何校验失败")
            if _distance(actual_nodes[-1], old_leader_nodes[line.id()][-1]) > _TOLERANCE_MM:
                raise RuntimeError(f"牵引线 '{line.id()}' 的地图锚点发生变化")

        refresh_canvas()
    except Exception as exc:
        for role, item in reversed(list(components.items())):
            try:
                current_x, current_y = _item_top_left(item)
                old_rect = old_component_rects[role]
                item.attemptMoveBy(old_rect[0] - current_x, old_rect[1] - current_y)
            except Exception as rollback_exc:  # pragma: no cover - QGIS failure path
                rollback_errors.append(f"恢复 '{item.id()}' 失败: {rollback_exc}")
        for item in reversed(leaders):
            try:
                _set_scene_nodes(item, old_leader_nodes[item.id()])
            except Exception as rollback_exc:  # pragma: no cover - QGIS failure path
                rollback_errors.append(f"恢复牵引线 '{item.id()}' 失败: {rollback_exc}")
        refresh_canvas()
        message = f"整组移动图文标注失败，已恢复原位置: {exc}"
        if rollback_errors:
            message += "；但回滚存在异常: " + "；".join(rollback_errors)
        return _result(
            False,
            message=message,
            error_code="callout_move_failed" if not rollback_errors else "callout_rollback_failed",
            data={"rollback_errors": rollback_errors},
        )

    leader_results = []
    for line in leaders:
        old_nodes = old_leader_nodes[line.id()]
        new_nodes = _scene_nodes(line)
        leader_results.append(
            {
                "item_id": line.id(),
                "style": leader_styles[line.id()],
                "anchor": {"x": new_nodes[-1][0], "y": new_nodes[-1][1]},
                "anchor_preserved": _distance(old_nodes[-1], new_nodes[-1]) <= _TOLERANCE_MM,
                "old_length_mm": _polyline_length(old_nodes),
                "new_length_mm": _polyline_length(new_nodes),
                "old_start": {"x": old_nodes[0][0], "y": old_nodes[0][1]},
                "new_start": {"x": new_nodes[0][0], "y": new_nodes[0][1]},
            }
        )

    return _result(
        True,
        output=component_ids["box"],
        message="图文标注整组移动成功，牵引线地图锚点保持不变",
        data={
            "layout": layout_name,
            "container_item_id": container_item_id,
            "callout_item_ids": {
                **component_ids,
                "leader": leader_ids[0] if len(leader_ids) == 1 else leader_ids,
            },
            "old_box_rect": _rect_dict(old_box_rect),
            "new_box_rect": _rect_dict(_item_rect(components["box"])),
            "delta_mm": {"x": delta_x, "y": delta_y},
            "moved_item_ids": list(component_ids.values()),
            "updated_leader_ids": leader_ids,
            "anchor_preserved": all(item["anchor_preserved"] for item in leader_results),
            "leaders": leader_results,
        },
    )


def move_callout_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    """QGIS tool entrypoint; all layout mutations run on the GUI thread."""

    return run_in_main_thread(lambda: _move_callout(params))


__all__ = ["move_callout_handler"]
