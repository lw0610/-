"""按外部版式声明创建版面；纯几何函数可在没有 QGIS 的环境测试。"""
from __future__ import annotations

import html
import json
import math
from pathlib import Path
import re
from uuid import uuid4

from autocarto.tools.system._shared.resource_handle import require_handle, handle_entry_path, handle_summary, legend_extra_spec


PAPERS_MM = {"A0": (841, 1189), "A1": (594, 841), "A2": (420, 594),
             "A3": (297, 420), "A4": (210, 297)}
ROLES = ("main_map", "border", "grid", "title", "overview", "legend", "scalebar", "north_arrow")
TEXT_ROLES = ("title", "overview", "legend", "scalebar", "north_arrow")


def finite_number(value, name, *, minimum=None):
    if isinstance(value, bool):
        raise ValueError(f"{name} 必须是有限数值")
    try:
        number = float(value)
    except (TypeError, ValueError) as exc:
        raise ValueError(f"{name} 必须是有限数值") from exc
    if not math.isfinite(number) or minimum is not None and number < minimum:
        raise ValueError(f"{name} 必须是大于等于 {minimum} 的有限数值")
    return number


def anchor_position(reference, size, anchor):
    """根据九宫格锚点计算左上角；外侧角点位于相应上/下边的外侧。"""
    at = anchor.get("at", "top_left")
    placement = anchor.get("placement", "inside")
    parts = at.split("_")
    if at == "center":
        vertical, horizontal = "middle", "center"
    elif len(parts) == 2 and parts[0] in ("top", "middle", "bottom") and parts[1] in ("left", "center", "right"):
        vertical, horizontal = parts
    else:
        raise ValueError(f"无法识别锚点位置：{at}")
    if placement not in ("inside", "outside"):
        raise ValueError("锚点 placement 必须为 inside 或 outside")
    x, y, rw, rh = (finite_number(reference[key], key) for key in ("x", "y", "width", "height"))
    width, height = (finite_number(size[key], key, minimum=0) for key in ("width", "height"))
    x += {"left": 0, "center": (rw-width)/2, "right": rw-width}[horizontal]
    y += {"top": 0, "middle": (rh-height)/2, "bottom": rh-height}[vertical]
    if placement == "outside":
        if vertical == "top":
            y = reference["y"]-height
        elif vertical == "bottom":
            y = reference["y"]+rh
        elif horizontal == "left":
            x = reference["x"]-width
        elif horizontal == "right":
            x = reference["x"]+rw
        else:
            raise ValueError("中心锚点不能放在参照框外侧")
    offset = anchor.get("offset_mm", {})
    if not isinstance(offset, dict):
        raise ValueError("锚点 offset_mm 必须是 x/y 对象")
    return x+finite_number(offset.get("x", 0), "锚点横向偏移"), y+finite_number(offset.get("y", 0), "锚点纵向偏移")


def sparse_interval(span, count=4):
    value = finite_number(span, "网格范围", minimum=0.000000000001)/count
    power = 10**math.floor(math.log10(value))
    return next(factor*power for factor in (1, 2, 5, 10) if factor*power >= value)


def page_geometry(spec, paper=None, orientation=None):
    page = spec["page"]
    paper = str(paper or page.get("default_paper", "A4")).strip().upper()
    orientation = str(orientation or page.get("default_orientation", "landscape")).lower()
    if orientation not in ("landscape", "portrait"):
        raise ValueError("纸张方向必须为 landscape 或 portrait")
    font_paper = paper
    if paper in PAPERS_MM:
        width, height = PAPERS_MM[paper]
    else:
        match = re.fullmatch(r"\s*([0-9]+(?:\.[0-9]+)?)\s*[X×]\s*([0-9]+(?:\.[0-9]+)?)\s*", paper)
        if not match:
            raise ValueError("纸张必须为 A0–A4 或 宽x高（毫米）")
        width, height = (finite_number(value, "纸张尺寸", minimum=1) for value in match.groups())
        font_paper = str(page.get("default_paper", "A4")).upper()
    width, height = (max(width, height), min(width, height)) if orientation == "landscape" else (min(width, height), max(width, height))
    if width > 5000 or height > 5000:
        raise ValueError("纸张宽高不能超过 5000 毫米")
    margins = page.get("margins_mm", {})
    reference = page.get("reference_size_mm", {"width": width, "height": height})
    sx, sy = width/finite_number(reference["width"], "基准纸张宽", minimum=1), height/finite_number(reference["height"], "基准纸张高", minimum=1)
    left, right = (finite_number(margins.get(key, 0), key, minimum=0)*sx for key in ("left", "right"))
    top, bottom = (finite_number(margins.get(key, 0), key, minimum=0)*sy for key in ("top", "bottom"))
    if left+right >= width or top+bottom >= height:
        raise ValueError("页边距没有留下有效的主图区域")
    fonts = spec["fonts_mm"].get(font_paper)
    if not isinstance(fonts, dict):
        raise ValueError(f"版式缺少 {font_paper} 的毫米字号表")
    for key in ("title", "legend_title", "legend_body", "scalebar", "overview_title", "overview_body"):
        finite_number(fonts.get(key), key, minimum=0.1)
    return {"width": width, "height": height}, {"x": left, "y": top, "width": width-left-right, "height": height-top-bottom}, fonts, (sx, sy)


def _read_spec(value):
    import yaml
    handle = require_handle(value, "layout", formats=("layout_yaml",))
    path = handle_entry_path(handle)
    if not path.is_file() or path.stat().st_size > 1 << 20:
        raise ValueError("版式文件不存在或超过 1 MiB")
    spec = yaml.safe_load(path.read_text(encoding="utf-8-sig"))
    if not isinstance(spec, dict) or not isinstance(spec.get("name"), str) or not spec["name"].strip():
        raise ValueError("版式必须包含名称")
    for key in ("page", "fonts_mm", "elements"):
        if not isinstance(spec.get(key), dict):
            raise ValueError(f"版式缺少 {key} 对象")
    if not isinstance(spec["elements"].get("main_map"), dict):
        raise ValueError("版式必须包含 main_map 元素")
    for role, configuration in spec["elements"].items():
        if role not in ROLES or not isinstance(configuration, dict):
            raise ValueError(f"无法识别的版式元素：{role}")
    return spec


def _resolve_layers(refs, project, *, visible=False):
    if refs is None:
        return [node.layer() for node in project.layerTreeRoot().findLayers()
                if not visible or node.isVisible()]
    if not isinstance(refs, list) or not refs:
        raise ValueError("图层参数必须是非空名称或 ID 数组")
    layers = []
    for ref in refs:
        layer = project.mapLayer(str(ref))
        if layer is None:
            matches = project.mapLayersByName(str(ref))
            if len(matches) != 1:
                raise ValueError(f"图层“{ref}”不存在或名称不唯一，请使用 ID")
            layer = matches[0]
        if not layer.isValid() or not layer.crs().isValid():
            raise ValueError(f"图层“{ref}”或其坐标系无效")
        if layer not in layers:
            layers.append(layer)
    return layers


def _map_extent(params, layers, crs, project):
    from qgis.core import QgsCoordinateReferenceSystem, QgsCoordinateTransform, QgsRectangle, QgsVectorLayer
    from autocarto.tools.system._shared.print_layout_items_impl import _get_transformed_vector_extent
    if params.get("extent") is not None:
        if params.get("extent_layers") is not None:
            raise ValueError("extent 与 extent_layers 只能提供一个")
        value = params["extent"]
        if not isinstance(value, dict) or not value.get("crs"):
            raise ValueError("显式范围必须包含 xmin/ymin/xmax/ymax 和 crs")
        coordinates = [finite_number(value.get(key), key) for key in ("xmin", "ymin", "xmax", "ymax")]
        if coordinates[0] >= coordinates[2] or coordinates[1] >= coordinates[3]:
            raise ValueError("显式范围必须满足 xmin < xmax、ymin < ymax")
        extent = QgsRectangle(*coordinates)
        source_crs = QgsCoordinateReferenceSystem(str(value["crs"]))
        if not source_crs.isValid():
            raise ValueError("显式范围坐标系无效")
        if source_crs != crs:
            extent = QgsCoordinateTransform(source_crs, crs, project).transformBoundingBox(extent)
    else:
        extent = None
        for layer in _resolve_layers(params["extent_layers"], project) if params.get("extent_layers") is not None else layers:
            transformer = QgsCoordinateTransform(layer.crs(), crs, project)
            current = _get_transformed_vector_extent(layer, transformer) if isinstance(layer, QgsVectorLayer) else transformer.transformBoundingBox(layer.extent())
            if current is None or current.isEmpty() or not current.isFinite():
                continue
            if extent is None:
                extent = QgsRectangle(current)
            else:
                extent.combineExtentWith(current)
    if extent is None or extent.isEmpty() or not extent.isFinite():
        raise ValueError("没有有效的二维图层范围")
    padding = finite_number(params.get("padding", 0.03), "范围留白", minimum=0)
    if padding > 1:
        raise ValueError("范围留白比例不能大于 1")
    extent.scale(1+2*padding)
    return extent


def _rect(item):
    from autocarto.tools.system._shared.print_layout_advanced_impl import _item_rect
    return _item_rect(item)


def _move(item, x, y):
    from qgis.core import QgsLayoutPoint, QgsUnitTypes
    item.attemptMove(QgsLayoutPoint(x, y, QgsUnitTypes.LayoutMillimeters))


def _resize(item, width, height):
    from qgis.core import QgsLayoutSize, QgsUnitTypes
    item.attemptResize(QgsLayoutSize(width, height, QgsUnitTypes.LayoutMillimeters))


def _properties(item, role, spec, anchor):
    item.setCustomProperty("autocarto/compose_role", role)
    item.setCustomProperty("autocarto/anchor", json.dumps(anchor, ensure_ascii=False, sort_keys=True))
    item.setCustomProperty("autocarto/font_locked", True)
    item.setCustomProperty("autocarto/compose_spec", spec["name"])


def layout_geometry_warnings(layout):
    """Read current geometry using compose's page and foreground-role rules."""
    from qgis.core import QgsLayoutItem, QgsLayoutItemPage
    from autocarto.tools.system._shared.print_layout_advanced_impl import _decorative_border_outer_rect
    pages = [_rect(page) for page in layout.pageCollection().pages()]
    warnings, foreground = [], []
    for item in layout.items():
        if not isinstance(item, QgsLayoutItem) or isinstance(item, QgsLayoutItemPage) or not item.isVisible():
            continue
        role = str(item.customProperty("autocarto/compose_role", "")
                   or item.customProperty("autocarto/item_role", "") or item.id() or item.uuid())
        rect = _rect(item)
        boundary = _decorative_border_outer_rect(item, rect) or rect
        if not any(boundary["x"] >= page["x"]-0.001 and boundary["y"] >= page["y"]-0.001
                   and boundary["x"]+boundary["width"] <= page["x"]+page["width"]+0.001
                   and boundary["y"]+boundary["height"] <= page["y"]+page["height"]+0.001 for page in pages):
            warnings.append(f"{role} 超出页面边界。")
        # A map, its grid/border, and foreground items intentionally overlap.
        # Retain the existing check only between the five foreground roles.
        if role in TEXT_ROLES:
            foreground.append((role, rect))
    foreground.sort(key=lambda entry: TEXT_ROLES.index(entry[0]))
    for i, (left, a) in enumerate(foreground):
        for right, b in foreground[i+1:]:
            if min(a["x"]+a["width"], b["x"]+b["width"])-max(a["x"], b["x"]) > 0.01 and min(a["y"]+a["height"], b["y"]+b["height"])-max(a["y"], b["y"]) > 0.01:
                warnings.append(f"{left} 与 {right} 相交，请核对内容与版式。")
    return warnings


def _svg(relative):
    from qgis.core import QgsApplication
    for base in QgsApplication.svgPaths():
        path = Path(base)/str(relative)
        if path.is_file():
            return str(path)
    raise ValueError(f"未找到 QGIS SVG：{relative}")


def _border(layout, main, config):
    from qgis.core import QgsLayoutItemShape, QgsFillSymbol, QgsMarkerSymbol, QgsSvgMarkerSymbolLayer, QgsMarkerLineSymbolLayer, QgsSimpleLineSymbolLayer, QgsUnitTypes
    from autocarto.tools.system._shared.print_layout_advanced_impl import _decorative_border_outer_rect
    shape = QgsLayoutItemShape(layout)
    shape.setId("border")
    shape.setShapeType(QgsLayoutItemShape.Rectangle)
    symbol = QgsFillSymbol.createSimple({"color": "#FFFFFF00", "outline_style": "no"})
    while symbol.symbolLayerCount():
        symbol.deleteSymbolLayer(0)
    marker = QgsMarkerSymbol.createSimple({"name": "circle", "color": config.get("line_color", "#000000")})
    marker.deleteSymbolLayer(0)
    svg = QgsSvgMarkerSymbolLayer(_svg(config.get("marker_svg", "arrows/NorthArrow_01.svg")))
    svg.setSize(finite_number(config.get("marker_size_mm", 4), "装饰标记大小", minimum=0.1))
    svg.setSizeUnit(QgsUnitTypes.RenderMillimeters)
    marker.appendSymbolLayer(svg)
    marker_line = QgsMarkerLineSymbolLayer()
    marker_line.setSubSymbol(marker)
    marker_line.setInterval(finite_number(config.get("marker_interval_mm", 4), "装饰标记间距", minimum=0.1))
    marker_line.setIntervalUnit(QgsUnitTypes.RenderMillimeters)
    symbol.appendSymbolLayer(marker_line)
    for sign in (1, -1):
        symbol.appendSymbolLayer(QgsSimpleLineSymbolLayer.create({"line_color": config.get("line_color", "#000000"),
                                 "line_width": str(config.get("line_width_mm", 0.3)), "offset": str(sign*finite_number(config.get("line_offset_mm", 3), "装饰线偏移", minimum=0))}))
    shape.setSymbol(symbol)
    shape.setCustomProperty("autocarto/item_role", "decorative_border")
    layout.addLayoutItem(shape)
    initial = _rect(main)
    _resize(shape, initial["width"], initial["height"])
    _move(shape, initial["x"], initial["y"])
    raw = _rect(shape)
    outer = _decorative_border_outer_rect(shape, raw)
    bleed = (outer["width"]-raw["width"])/2
    offset = finite_number(config.get("offset_mm", 5), "装饰边框外扩", minimum=0)
    _resize(shape, initial["width"]+2*offset-2*bleed, initial["height"]+2*offset-2*bleed)
    _move(shape, initial["x"]-offset+bleed, initial["y"]-offset+bleed)
    return shape


def _label(layout, role, text, font, config, width=None, heading=None, heading_font=None):
    from qgis.core import QgsLayoutItemLabel, QgsLayoutMeasurement, QgsUnitTypes
    from qgis.PyQt.QtGui import QColor
    from autocarto.tools.system._shared.print_layout_advanced_impl import _wrap_text_to_width, _layout_dpi
    label = QgsLayoutItemLabel(layout)
    label.setId(role)
    label.setTextFormat(font)
    if width:
        text = _wrap_text_to_width(str(text), font, width, _layout_dpi(layout))
    if heading:
        label.setMode(QgsLayoutItemLabel.ModeHtml)
        family = html.escape(font.font().family(), quote=True)
        label.setText(f'<div style="font-family:{family};font-size:{font.size()}mm;color:{font.color().name()};">'
                      f'<div style="font-weight:bold;font-size:{heading_font}mm;">{html.escape(str(heading))}</div>'
                      f'{html.escape(str(text)).replace(chr(10), "<br/>")}</div>')
    else:
        label.setText(str(text))
    layout.addLayoutItem(label)
    label.setFrameEnabled(bool(config.get("frame", False)))
    label.setBackgroundEnabled(bool(config.get("background", False)))
    if config.get("background"):
        label.setBackgroundColor(QColor(config["background"] if isinstance(config["background"], str) else "#ffffff"))
    if config.get("frame_width_mm") is not None:
        label.setFrameStrokeWidth(QgsLayoutMeasurement(float(config["frame_width_mm"]), QgsUnitTypes.LayoutMillimeters))
    label.adjustSizeToText()
    return label


def _legend(layout, main, layers, params, fonts, config, project, created_layers):
    from qgis.core import QgsLayoutItemLegend, QgsLegendStyle, QgsVectorLayer, QgsRasterMarkerSymbolLayer, QgsSvgMarkerSymbolLayer, QgsMarkerSymbol, QgsSingleSymbolRenderer, QgsUnitTypes, QgsLayoutExporter, QgsApplication
    from qgis.PyQt.QtCore import Qt
    from autocarto.tools.system._shared.print_layout_advanced_impl import _build_mm_text_format, _apply_legend_text_format, _resolve_legend_labels, _apply_legend_labels
    labels = _resolve_legend_labels(params.get("legend_labels") or {}, project)
    legend = QgsLayoutItemLegend(layout)
    legend.setId("legend")
    legend.setLinkedMap(main)
    legend.setTitle(str(params.get("legend_title", "图例")))
    legend.setAutoUpdateModel(False)
    legend.setResizeToContents(True)
    _resize(legend, config.get("width_mm", 55), config.get("height_mm", 50))
    layout.addLayoutItem(legend)
    root = legend.model().rootGroup()
    root.removeAllChildren()
    for layer in layers:
        root.addLayer(layer)
    extras = params.get("legend_extra_items", [])
    if not isinstance(extras, list):
        raise ValueError("额外图例条目必须是数组")
    for extra in extras:
        path, size, label = legend_extra_spec(extra, params.get("legend_symbols"))
        symbol_layer = QgsSvgMarkerSymbolLayer(str(path)) if path.suffix.lower() == ".svg" else QgsRasterMarkerSymbolLayer(str(path))
        symbol_layer.setSize(size)
        symbol_layer.setSizeUnit(QgsUnitTypes.RenderMillimeters)
        symbol = QgsMarkerSymbol()
        symbol.changeSymbolLayer(0, symbol_layer)
        placeholder = QgsVectorLayer("Point", label, "memory")
        placeholder.setCustomProperty("autocarto/compose_legend_layout", str(params["layout"]))
        placeholder.setRenderer(QgsSingleSymbolRenderer(symbol))
        project.addMapLayer(placeholder, False)
        created_layers.append(placeholder.id())
        root.addLayer(placeholder)
    _apply_legend_labels(legend, labels)
    family = config.get("font_family", "Arial")
    _apply_legend_text_format(legend, [QgsLegendStyle.Title], _build_mm_text_format(family, fonts["legend_title"]))
    _apply_legend_text_format(legend, [QgsLegendStyle.Group, QgsLegendStyle.Subgroup, QgsLegendStyle.SymbolLabel], _build_mm_text_format(family, fonts["legend_body"]))
    legend.setTitleAlignment(Qt.AlignHCenter)
    legend.setFrameEnabled(bool(config.get("frame", True)))
    legend.updateLegend()
    QgsLayoutExporter(layout).renderPageToImage(0)
    QgsApplication.processEvents()
    legend.adjustBoxSize()
    return legend


def _scalebar(layout, main, font, config):
    from qgis.core import Qgis, QgsLayoutItemScaleBar, QgsApplication
    from autocarto.tools.system._shared.print_layout_items_impl import _set_scalebar_units_from_map_scale
    scalebar = QgsLayoutItemScaleBar(layout)
    scalebar.setId("scalebar")
    width = finite_number(config.get("max_width_mm", 80), "比例尺条带宽度", minimum=1)
    _resize(scalebar, width, 12)
    scalebar.setLinkedMap(main)
    scalebar.setStyle("Single Box")
    scalebar.setNumberOfSegments(4)
    scalebar.setNumberOfSegmentsLeft(0)
    layout.addLayoutItem(scalebar)
    _set_scalebar_units_from_map_scale(scalebar, main, width, 4)
    scalebar.setSegmentSizeMode(Qgis.ScaleBarSegmentSizeMode.FitWidth)
    scalebar.setMinimumBarWidth(max(width*0.75, 1))
    scalebar.setMaximumBarWidth(width)
    scalebar.update()
    scalebar.refresh()
    QgsApplication.processEvents()
    scalebar.setTextFormat(font)
    scalebar.update()
    scalebar.refresh()
    QgsApplication.processEvents()
    return scalebar


def compose_layout_handler(params):
    try:
        handle = require_handle(params.get("spec"), "layout")
        if handle.get("format") == "qpt":
            from autocarto.tools.system._shared.print_layout_impl import import_layout_template_handler
            mapped = dict(params, template=handle, name=params.get("layout"), texts={"标题": params.get("title", ""), "title": params.get("title", ""), "综述": params.get("overview_text", "")})
            return import_layout_template_handler(mapped)
        from autocarto.tools.system._shared.layout_runtime import run_in_main_thread
        return run_in_main_thread(lambda: _compose(params))
    except Exception as exc:
        return {"success": False, "output": "", "info": f"按版式排版失败：{exc}",
                "error_type": getattr(exc, "code", "layout_compose_failed")}


def _compose(params):
    from qgis.core import QgsProject, QgsPrintLayout, QgsLayoutSize, QgsLayoutItemMap, QgsLayoutItemMapGrid, QgsLineSymbol, QgsLayoutItemPicture, QgsUnitTypes
    from autocarto.tools.system._shared.print_layout_advanced_impl import _build_mm_text_format, _decorative_border_outer_rect
    name = str(params.get("layout") or "").strip()
    if not name or not str(params.get("title") or "").strip():
        raise ValueError("版面名称和标题不能为空")
    spec = _read_spec(params.get("spec"))
    page, map_rect, fonts, factors = page_geometry(spec, params.get("paper"), params.get("orientation"))
    configuration = spec["elements"]
    overrides = params.get("elements", {})
    if not isinstance(overrides, dict) or any(key not in ROLES or not isinstance(value, bool) for key, value in overrides.items()):
        raise ValueError("elements 必须是元素语义名到布尔值的开关对象")
    if any(value and key not in configuration for key, value in overrides.items()):
        raise ValueError("不能启用版式中未定义的元素")
    enabled = {role: overrides.get(role, config.get("enabled", True)) for role, config in configuration.items()}
    if not enabled.get("main_map", False):
        raise ValueError("不能关闭主地图框")
    project = params.get("_project") or QgsProject.instance()
    manager = project.layoutManager()
    existing = manager.layoutByName(name)
    if existing is not None and params.get("replace_existing", True) is not True:
        raise ValueError("同名版面已存在，replace_existing=false 禁止替换")
    layers = _resolve_layers(params.get("map_layers"), project, visible=True)
    if not layers:
        raise ValueError("工程没有可见的有效图层")
    crs = project.crs()
    if not crs.isValid():
        raise ValueError("工程坐标系无效")
    extent = _map_extent(params, layers, crs, project)
    legend_layers = _resolve_layers(params["legend_layers"], project) if params.get("legend_layers") is not None else layers
    layout = QgsPrintLayout(project)
    layout.initializeDefaults()
    layout.setName(name)
    layout.pageCollection().page(0).setPageSize(QgsLayoutSize(page["width"], page["height"], QgsUnitTypes.LayoutMillimeters))
    objects, anchors, warnings, created_layers = {}, {}, [], []
    previous_layers = [layer.id() for layer in project.mapLayers().values() if layer.customProperty("autocarto/compose_legend_layout", "") == name]
    try:
        main = QgsLayoutItemMap(layout)
        main.setId("main_map")
        main.setCrs(crs)
        _resize(main, map_rect["width"], map_rect["height"])
        _move(main, map_rect["x"], map_rect["y"])
        layout.addLayoutItem(main)
        main.setLayers(layers)
        main.setKeepLayerSet(True)
        main.zoomToExtent(extent)
        objects["main_map"] = main
        anchors["main_map"] = {"to": "page", "at": "top_left", "placement": "inside", "offset_mm": {"x": map_rect["x"], "y": map_rect["y"]}}
        if enabled.get("border"):
            objects["border"] = _border(layout, main, configuration["border"])
            anchors["border"] = {"to": "main_map", "at": "center", "placement": "inside"}
        if enabled.get("grid"):
            config = configuration["grid"]
            grid = QgsLayoutItemMapGrid("grid", main)
            grid.setStyle(QgsLayoutItemMapGrid.Solid)
            grid.setCrs(crs)
            grid.setIntervalX(sparse_interval(main.extent().width()))
            grid.setIntervalY(sparse_interval(main.extent().height()))
            symbol = QgsLineSymbol.createSimple({"line_color": config.get("color", "#89deea"), "line_width": str(config.get("width_mm", 0.1))})
            symbol.setOpacity(float(config.get("opacity", 0.5)))
            grid.setLineSymbol(symbol)
            main.grids().addGrid(grid)
            objects["grid"] = grid
            anchors["grid"] = {"to": "main_map", "at": "center", "placement": "inside"}
        for role in ("title", "overview"):
            text = str(params.get("title" if role == "title" else "overview_text") or "")
            if not enabled.get(role) or role == "overview" and not text:
                continue
            config = configuration[role]
            family = config.get("font_family", spec.get("font_family", "Noto Sans CJK SC"))
            font = _build_mm_text_format(family, fonts["title" if role == "title" else "overview_body"], bool(config.get("bold", role == "title")), color=config.get("color", "#202020"))
            ratio = params.get("overview_max_width_ratio", config.get("max_width_ratio")) if role == "overview" else None
            width = None if ratio is None else map_rect["width"]*finite_number(ratio, "综述宽度比例", minimum=0.01)
            if ratio is not None and float(ratio) > 1:
                raise ValueError("综述宽度比例不能大于 1")
            objects[role] = _label(layout, role, text, font, config, width, params.get("overview_title") if role == "overview" else None, fonts["overview_title"])
        if enabled.get("legend"):
            objects["legend"] = _legend(layout, main, legend_layers, params, fonts, configuration["legend"], project, created_layers)
        if enabled.get("scalebar"):
            config = configuration["scalebar"]
            objects["scalebar"] = _scalebar(layout, main, _build_mm_text_format(config.get("font_family", spec.get("font_family", "Noto Sans CJK SC")), fonts["scalebar"]), config)
        if enabled.get("north_arrow"):
            config = configuration["north_arrow"]
            arrow = QgsLayoutItemPicture(layout)
            arrow.setId("north_arrow")
            _resize(arrow, config.get("width_mm", 20), config.get("height_mm", 30))
            arrow.setPicturePath(_svg(config.get("svg", "arrows/NorthArrow_03.svg")))
            layout.addLayoutItem(arrow)
            objects["north_arrow"] = arrow
        rects = {"page": {"x": 0, "y": 0, **page}, "main_map": _rect(main)}
        if "border" in objects:
            rects["border"] = _decorative_border_outer_rect(objects["border"], _rect(objects["border"]))
        pending = [role for role in TEXT_ROLES if role in objects]
        while pending:
            progressed = False
            for role in pending[:]:
                anchor = json.loads(json.dumps(configuration[role].get("anchor", {"to": "main_map", "at": "top_left", "placement": "inside"})))
                target = anchor.get("to", "main_map")
                if target == "border" and not enabled.get("border"):
                    anchor["to"] = target = "main_map"
                    warnings.append(f"{role} 的装饰框锚点已随装饰框关闭而改用主图。")
                if target not in rects:
                    continue
                if anchor.get("scale_offset_with_page", False):
                    offset = anchor.get("offset_mm", {})
                    anchor["offset_mm"] = {"x": offset.get("x", 0)*factors[0], "y": offset.get("y", 0)*factors[1]}
                x, y = anchor_position(rects[target], _rect(objects[role]), anchor)
                _move(objects[role], x, y)
                anchors[role] = anchor
                rects[role] = _rect(objects[role])
                pending.remove(role)
                progressed = True
            if not progressed:
                raise ValueError("版式锚点存在循环、引用不存在的元素或关闭的元素")
        result_rects = {}
        for role, item in objects.items():
            _properties(item, role, spec, anchors[role])
            if role == "grid":
                result_rects[item.id()] = _rect(main)
                continue
            rect = _rect(item)
            result_rects[item.id()] = rect
        warnings.extend(layout_geometry_warnings(layout))
        if existing is not None:
            existing.setName("__compose_old_"+uuid4().hex)
        try:
            if not manager.addLayout(layout):
                raise ValueError("QGIS 未能注册新建版面")
        except Exception:
            if existing is not None:
                existing.setName(name)
            raise
        if existing is not None:
            manager.removeLayout(existing)
        for layer_id in previous_layers:
            project.removeMapLayer(layer_id)
        item_ids = {role: item.id() for role, item in objects.items()}
        return {"success": True, "output": name, "info": "版面已按版式生成，请检查布局警告。" if warnings else "版面已按版式生成。",
                "resource": handle_summary(params["spec"]),
                "data": {"layout": name, "page_size_mm": page, "item_ids": item_ids, "rects_mm": result_rects, "warnings": warnings,
                         "map_layer_ids": [layer.id() for layer in layers], "map_layers": [layer.name() for layer in layers]}}
    except Exception:
        for layer_id in created_layers:
            project.removeMapLayer(layer_id)
        raise
