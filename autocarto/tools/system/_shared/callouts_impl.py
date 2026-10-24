"""版面图文注记的创建。"""
import math
from pathlib import Path
from autocarto.knowledge.visual_presets import renderer_defaults
from autocarto.knowledge.dataset_handoff import DatasetHandoffError, resolve_route_dataset
from autocarto.core.paths import resolve_project_path
from autocarto.tools.system._shared.table_io import _read_csv_rows, _get_col_value, _to_bool
from autocarto.tools.system._shared.route_runtime_support import dataset_failure
from typing import (
    Any,
    Dict,
    Optional,
)
from qgis.PyQt.QtGui import (
    QColor,
    QFont,
)
from qgis.core import (
    QgsCoordinateReferenceSystem,
    QgsCoordinateTransform,
    QgsLayoutItem,
    QgsLayoutItemLabel,
    QgsLayoutItemMap,
    QgsLayoutItemPicture,
    QgsLayoutItemPolyline,
    QgsLayoutItemShape,
    QgsLayoutPoint,
    QgsLayoutSize,
    QgsUnitTypes,
    QgsLineSymbol,
    QgsProject,
    QgsPointXY,
    QgsTextFormat,
)
from autocarto.tools.system._shared.callout_text_layout import wrap_chinese_text_to_mm
from autocarto.tools.system._shared.style_common import (
    _color,
    _wrap_main_thread,
)


def _cartography_add_callouts_impl(params: Dict[str, Any]) -> Dict[str, Any]:
    """cartography_add_callouts 的集中实现。"""
    defaults = renderer_defaults()["callouts"]

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
    for item_id in params.get("avoid_item_ids") or []:
        item = layout.itemById(str(item_id))
        if item is not None and item is not map_item:
            rect = item.sceneBoundingRect()
            placed.append([float(rect.x()), float(rect.y()), float(rect.width()), float(rect.height())])
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
        width_mm = float(box_cfg.get("width_mm", defaults["box"]["width_mm"]) or 42.0)
        if not math.isfinite(width_mm) or width_mm <= 0:
            items_out.append({"id": cid, "success": False, "error_type": "invalid_box_width", "message": f"box.width_mm 必须为正数，当前: {width_mm}", "warnings": [], "debug": {"box": box_cfg}})
            continue

        shape_name = str(box_cfg.get("shape", defaults["box"]["shape"]) or "rectangle")
        allowed_shapes = {"rectangle", "rounded_rectangle", "ellipse", "image_only_ellipse", "ellipse_badge"}
        if shape_name not in allowed_shapes:
            items_out.append({"id": cid, "success": False, "error_type": "invalid_shape", "message": f"box.shape 不支持: {shape_name}", "warnings": [], "debug": {"allowed_shapes": sorted(list(allowed_shapes))}})
            continue

        pad = float(box_cfg.get("padding_mm", defaults["box"]["padding_mm"]) or 1.8)
        gap = float(img_cfg.get("gap_mm", defaults["image"]["gap_mm"]) or 1.5)
        inner_w = max(width_mm - 2 * pad, 6.0)

        body_font = QFont(str(txt_cfg.get("body_font_family", defaults["text"]["body_font_family"])))
        body_font.setPointSizeF(float(txt_cfg.get("body_font_size_pt", defaults["text"]["body_font_size_pt"])))

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
            body_format.setColor(_color(txt_cfg.get("body_color"), defaults["text"]["body_color"]))
            body_format.setLineHeight(float(txt_cfg.get("line_spacing", defaults["text"]["line_spacing"])))
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
        if str(box_cfg.get("shape", defaults["box"]["shape"])) in ("ellipse", "image_only_ellipse", "ellipse_badge"):
            box_item.setShapeType(QgsLayoutItemShape.Shape.Ellipse)
        else:
            box_item.setShapeType(QgsLayoutItemShape.Shape.Rectangle)
        sym = box_item.symbol()
        if sym is not None and sym.symbolLayerCount() > 0:
            sl = sym.symbolLayer(0)
            sl.setFillColor(_color(box_cfg.get("fill_color"), defaults["box"]["fill_color"]))
            pen = QPen(_color(box_cfg.get("border_color"), defaults["box"]["border_color"]))
            pen.setWidthF(float(box_cfg.get("border_width_mm", defaults["box"]["border_width_mm"]) or 0.3))
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
                leader_color = _color(leader_cfg.get("color"), defaults["leader"]["color"])
                leader_width = float(leader_cfg.get("width_mm", defaults["leader"]["width_mm"]) or 0.25)

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

        for role, values in created.items():
            for item_id in values if isinstance(values, list) else [values]:
                item = layout.itemById(item_id)
                if item is not None:
                    item.setCustomProperty("autocarto/callout_group", cid)
                    item.setCustomProperty("autocarto/callout_role", role)
                    item.setCustomProperty("autocarto/callout_part", role)
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


cartography_add_callouts_handler = _wrap_main_thread(cartography_add_callouts_handler)


def layout_add_callouts_handler(params):
    try:
        layout_name = str(params.get("layout") or params.get("layout_name") or "").strip()
        if not layout_name:
            raise ValueError("缺少 layout 版面名称")
        if params.get("callouts_csv"):
            if params.get("dataset_source") or params.get("dataset_dir"):
                raise ValueError("callouts_csv 与数据集参数不能同时提供")
            table = resolve_project_path(str(params["callouts_csv"]))
            base = resolve_project_path(str(params["image_base_dir"])) if params.get("image_base_dir") else table.parent
            dataset = None
        else:
            resolved = resolve_route_dataset(params)
            table, base, dataset = resolved.dataset_dir / "callouts.csv", resolved.dataset_dir, resolved.to_dict()
            if params.get("image_base_dir"):
                base = resolve_project_path(str(params["image_base_dir"]))
        rows = _read_csv_rows(table)
        callouts = []
        for row in rows:
            path = str(row.get("image_path") or "").strip()
            if path and not Path(path).is_absolute():
                path = str((base / path).resolve())
            callouts.append({"id": row.get("id") or row.get("callout_id"),
                "anchor": {"x": float(_get_col_value(row, ["anchor_x", "x"], required=True)), "y": float(_get_col_value(row, ["anchor_y", "y"], required=True))},
                "body": str(_get_col_value(row, ["body", "desc", "description"], default="") or "").strip(),
                "priority": float(row.get("priority") or 0), "template": row.get("template") or "image_top_text_bottom",
                "box": {"width_mm": float(row.get("width_mm") or renderer_defaults()["callouts"]["box"]["width_mm"])},
                "position": {"preferred_side": row.get("preferred_side") or "right_top", "offset_mm": [float(row.get("offset_x_mm") or 18), float(row.get("offset_y_mm") or -12)]},
                "leader_line": {"style": row.get("leader_style") or "double"},
                "image": {"enabled": _to_bool(row.get("image_enabled")), "path": path}})
        layout = QgsProject.instance().layoutManager().layoutByName(layout_name)
        if layout is None:
            raise ValueError("未找到版面: " + layout_name)
        avoid = params.get("avoid_item_ids")
        map_id = str(params.get("map_item_id") or "main_map")
        if avoid is None:
            avoid = [item.id() for item in layout.items() if hasattr(item, "id") and item.id() and item.id() != map_id]
        if not isinstance(avoid, list) or any(not isinstance(value, str) for value in avoid):
            raise ValueError("avoid_item_ids 须为元素 ID 数组")
        if not callouts:
            return {"success": True, "output": layout_name, "info": "没有待创建的图文标注", "data": {"items": [], "created_count": 0, "failed_count": 0, "dataset": dataset}}
        result = cartography_add_callouts_handler(dict(params, layout_name=layout_name, map_item_id=map_id, callouts=callouts, avoid_item_ids=avoid))
        data = {key: value for key, value in result.items() if key not in {"success", "output", "info", "error_type", "message"}}
        data["dataset"] = dataset
        return {"success": result["success"], "output": result.get("output", ""), "info": result.get("info", ""),
                "error_type": result.get("error_type"), "data": data}
    except DatasetHandoffError as exc:
        return dataset_failure(exc)
    except Exception as exc:
        return {"success": False, "output": "", "info": str(exc), "message": str(exc), "error_type": "callouts_failed"}


layout_add_callouts_handler = _wrap_main_thread(layout_add_callouts_handler)
