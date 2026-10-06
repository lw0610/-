import logging
import math
import os
from typing import Any, Dict, Optional, Tuple

from qgis.PyQt.QtCore import QRectF
from qgis.PyQt.QtGui import QColor, QFont
from qgis.core import (
    Qgis,
    QgsCoordinateTransform,
    QgsFeatureRequest,
    QgsFillSymbol,
    QgsGeometry,
    QgsLayoutFrame,
    QgsLayoutItemAttributeTable,
    QgsLayoutItemLabel,
    QgsLayoutItemLegend,
    QgsLayoutItemMap,
    QgsLayoutItemPicture,
    QgsLayoutItemScaleBar,
    QgsLayoutItemShape,
    QgsLayoutMeasurement,
    QgsMarkerLineSymbolLayer,
    QgsSimpleLineSymbolLayer,
    QgsSvgMarkerSymbolLayer,
    QgsLineSymbol,
    QgsMarkerSymbol,
    QgsLayoutPoint,
    QgsLayoutSize,
    QgsLayerTreeModel,
    QgsMapThemeCollection,
    QgsProject,
    QgsRectangle,
    QgsTextFormat,
    QgsUnitTypes,
    QgsVectorLayer,
    QgsApplication,
    QgsLayoutExporter,
    QgsLayoutItemMapGrid,
)

from autocarto.tools.system._shared.canvas_refresh import refresh_canvas
from autocarto.tools.system._shared.layout_runtime import (
    get_item_rect_mm,
    run_in_main_thread,
    success_with_layout_data,
    validate_layout_item,
)

logger = logging.getLogger(__name__)


def _get_transformed_vector_extent(
    layer: QgsVectorLayer,
    transformer: QgsCoordinateTransform,
) -> Optional[QgsRectangle]:
    """Return the target-CRS extent of the layer's actual geometries.

    Transforming only ``layer.extent()`` transforms the four corners of the
    source-CRS envelope.  For a nonlinear CRS transform that rectangle is not
    the envelope of the transformed features and can introduce large phantom
    margins.  Layout fitting needs the latter.
    """

    transformed_extent = None
    skipped_features = 0
    request = QgsFeatureRequest()
    try:
        request.setNoAttributes()
    except Exception:
        pass

    for feature in layer.getFeatures(request):
        source_geometry = feature.geometry()
        if source_geometry is None or source_geometry.isNull() or source_geometry.isEmpty():
            continue

        try:
            geometry = QgsGeometry(source_geometry)
            geometry.transform(transformer)
            geometry_extent = geometry.boundingBox()
        except Exception:
            skipped_features += 1
            continue

        if geometry_extent.isEmpty():
            continue
        if transformed_extent is None:
            transformed_extent = QgsRectangle(geometry_extent)
        else:
            transformed_extent.combineExtentWith(geometry_extent)

    if skipped_features:
        logger.warning(
            "图层 '%s' 有 %d 个要素无法转换坐标，已跳过",
            layer.name(),
            skipped_features,
        )
    return transformed_extent


def _get_project_combined_extent() -> Optional[QgsRectangle]:
    project = QgsProject.instance()
    project_crs = project.crs()
    combined_extent = None

    for layer in project.mapLayers().values():
        if not layer or not layer.isValid():
            continue

        layer_extent = layer.extent()
        if not layer_extent or layer_extent.isEmpty():
            continue

        try:
            layer_crs = layer.crs()
            if (
                project_crs.isValid()
                and layer_crs.isValid()
                and layer_crs != project_crs
            ):
                transformer = QgsCoordinateTransform(layer_crs, project_crs, project)
                if isinstance(layer, QgsVectorLayer):
                    exact_extent = _get_transformed_vector_extent(layer, transformer)
                    if exact_extent is not None and not exact_extent.isEmpty():
                        layer_extent = exact_extent
                    else:
                        layer_extent = transformer.transformBoundingBox(layer_extent)
                else:
                    layer_extent = transformer.transformBoundingBox(layer_extent)
        except Exception as e:
            logger.warning("图层范围坐标转换失败，跳过图层 '%s': %s", layer.name(), e)
            continue

        if layer_extent.isEmpty():
            continue

        if combined_extent is None:
            combined_extent = QgsRectangle(layer_extent)
        else:
            combined_extent.combineExtentWith(layer_extent)

    return combined_extent


def _layout_page_size_mm(layout) -> Optional[Tuple[float, float]]:
    try:
        pages = layout.pageCollection()
        if pages.pageCount() <= 0:
            return None
        page = pages.page(0)
        size = page.pageSize()
        return float(size.width()), float(size.height())
    except Exception:
        return None


def _map_frame_size_for_extent(
    page_width_mm: float,
    page_height_mm: float,
    extent: QgsRectangle,
    page_fraction: float = 0.85,
) -> Tuple[float, float]:
    """Fit a map frame to the source extent without adding blank bands."""
    max_width = max(1.0, min(page_width_mm * page_fraction, page_width_mm))
    max_height = max(1.0, min(page_height_mm * page_fraction, page_height_mm))

    extent_width = float(extent.width())
    extent_height = float(extent.height())
    if extent_width <= 0 or extent_height <= 0:
        return max_width, max_height

    extent_ratio = extent_width / extent_height
    available_ratio = max_width / max_height
    if extent_ratio >= available_ratio:
        return max_width, max(1.0, max_width / extent_ratio)
    return max(1.0, max_height * extent_ratio), max_height


# def _default_map_theme_name(project: QgsProject) -> Optional[str]:
#     try:
#         collection = project.mapThemeCollection()
#     except Exception:
#         return None

#     try:
#         themes = list(collection.mapThemes() or [])
#     except Exception:
#         themes = []

#     if themes:
#         return themes[0]

#     try:
#         root = project.layerTreeRoot()
#         if root is None:
#             return None

#         theme_name = "AutoCarto_Default_Theme"
#         if hasattr(collection, "hasMapTheme") and collection.hasMapTheme(theme_name):
#             return theme_name

#         model = QgsLayerTreeModel(root)
#         theme_state = QgsMapThemeCollection.createThemeFromCurrentState(root, model)
#         collection.insert(theme_name, theme_state)
#         return theme_name
#     except Exception as e:
#         logger.warning("创建默认地图主题失败: %s", e)
#         return None


# def _visible_layers_in_project_draw_order(project: QgsProject) -> list:
#     root = project.layerTreeRoot()
#     if root is None:
#         return []

#     ordered_layers = []
#     try:
#         if root.hasCustomLayerOrder():
#             ordered_layers = list(root.customLayerOrder() or [])
#         else:
#             ordered_layers = list(root.layerOrder() or [])
#     except Exception:
#         ordered_layers = []

#     if not ordered_layers:
#         try:
#             ordered_layers = [node.layer() for node in root.findLayers() if node.layer()]
#         except Exception:
#             ordered_layers = []

#     visible_layers = []
#     for layer in ordered_layers:
#         if layer is None:
#             continue

#         node = root.findLayer(layer.id())
#         if node is None:
#             visible_layers.append(layer)
#             continue

#         try:
#             if node.isVisible():
#                 visible_layers.append(layer)
#         except Exception:
#             visible_layers.append(layer)

#     return visible_layers


def _item_scene_rect_mm(item) -> QRectF:
    """
    Return an item rectangle in layout scene coordinates (mm), while
    staying compatible with different QGIS/PyQGIS versions.
    """
    scene_rect_fn = getattr(item, "sceneRect", None)
    if callable(scene_rect_fn):
        rect = scene_rect_fn()
        if rect is not None:
            return QRectF(rect)

    rect_fn = getattr(item, "rect", None)
    pos_fn = getattr(item, "pos", None)
    rect = rect_fn() if callable(rect_fn) else QRectF()
    pos = pos_fn() if callable(pos_fn) else None
    if pos is None:
        return QRectF(rect)
    return QRectF(float(pos.x()), float(pos.y()), float(rect.width()), float(rect.height()))


def _default_distance_unit_label(unit: Any) -> str:
    mapping = {
        Qgis.DistanceUnit.Meters: "m",
        Qgis.DistanceUnit.Kilometers: "km",
        Qgis.DistanceUnit.Feet: "ft",
        Qgis.DistanceUnit.Miles: "mi",
        Qgis.DistanceUnit.NauticalMiles: "nm",
        Qgis.DistanceUnit.Degrees: "°",
    }
    return str(mapping.get(unit, ""))


def _nice_distance(value: float) -> float:
    if value <= 0:
        return 1.0
    exponent = int(math.floor(math.log10(value)))
    fraction = value / (10**exponent)
    if fraction <= 1:
        nice_fraction = 1
    elif fraction <= 2:
        nice_fraction = 2
    elif fraction <= 5:
        nice_fraction = 5
    else:
        nice_fraction = 10
    return float(nice_fraction * (10**exponent))


def _set_scalebar_units_from_map_scale(
    scalebar, map_item, scalebar_width_mm: float, segments: int
) -> None:
    map_unit = map_item.crs().mapUnits()

    # QGIS 3.40+ measures a scale bar linked to a geographic map with
    # QgsDistanceArea.  Keeping the visible map in its geographic CRS avoids
    # the tilted bounding box and blank wedges caused by forcing it to UTM,
    # while the scale bar still reports ellipsoidal distances in kilometres.
    try:
        if map_item.crs().isGeographic():
            scalebar.setUnits(Qgis.DistanceUnit.Kilometers)
            scalebar.setMapUnitsPerScaleBarUnit(1.0)
            # FitWidth recalculates this value after the size mode is applied.
            scalebar.setUnitsPerSegment(1.0)
            scalebar.setUnitLabel("km")
            return
    except Exception:
        pass

    segment_count = max(int(segments), 1)
    width_mm = max(float(scalebar_width_mm), 1.0)
    segment_width_mm = width_mm / float(segment_count)

    units_per_segment_map = None
    try:
        map_scale = float(map_item.scale())
        if map_scale > 0:
            # 地图比例分母 N: 1mm 纸面 = N mm 地面。
            # 先换算为米，再换算成地图单位。
            map_units_per_meter = float(
                QgsUnitTypes.fromUnitToUnitFactor(Qgis.DistanceUnit.Meters, map_unit)
            )
            if map_units_per_meter > 0:
                map_units_per_mm = (map_scale / 1000.0) * map_units_per_meter
                units_per_segment_map = map_units_per_mm * segment_width_mm
    except Exception:
        units_per_segment_map = None

    # 兜底：根据可视范围与地图框宽度估算
    if units_per_segment_map is None or units_per_segment_map <= 0:
        try:
            map_units_to_layout_units = float(map_item.mapUnitsToLayoutUnits())
            if map_units_to_layout_units > 0:
                units_per_segment_map = segment_width_mm / map_units_to_layout_units
        except Exception:
            units_per_segment_map = None

    if units_per_segment_map is None or units_per_segment_map <= 0:
        units_per_segment_map = 1.0

    display_unit = map_unit
    # QGIS 会根据 setUnits(...) 自动做单位换算。
    # 这里的 mapUnitsPerScaleBarUnit 在界面中对应“标注单位乘数”，
    # 若单位已是 km 再设成 1000 会导致标签被额外缩小 1000 倍（出现 0.05、0.1 这类值）。
    map_units_per_scale_unit = 1.0
    display_units_per_segment = float(units_per_segment_map)

    # 主要优化路径：米系坐标自动切公里，避免大图幅显示成超长米值。
    if map_unit == Qgis.DistanceUnit.Meters and units_per_segment_map >= 1000.0:
        display_unit = Qgis.DistanceUnit.Kilometers
        display_units_per_segment = units_per_segment_map / 1000.0

    display_units_per_segment = _nice_distance(float(display_units_per_segment))

    scalebar.setUnits(display_unit)
    scalebar.setMapUnitsPerScaleBarUnit(map_units_per_scale_unit)
    scalebar.setUnitsPerSegment(display_units_per_segment)

    unit_label = ""
    try:
        unit_label = str(QgsUnitTypes.toAbbreviatedString(display_unit) or "")
    except Exception:
        unit_label = ""
    if not unit_label:
        unit_label = _default_distance_unit_label(display_unit)
    if unit_label:
        scalebar.setUnitLabel(unit_label)


def add_map_item_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        def _execute():
            def _fail(info: str) -> Dict[str, Any]:
                return {"success": False, "output": "", "info": info}

            layout_name = params.get("layout")
            item_id = params.get("item_id")

            if not layout_name:
                return _fail("错误：缺少布局名称参数")
            if not item_id:
                return _fail("错误：缺少地图名称参数")
            
            project = QgsProject.instance()
            layout = project.layoutManager().layoutByName(layout_name)

            if not layout:
                return _fail(f"错误：未找到布局 '{layout_name}'")
            
            # 获取地图范围大小
            project_extent = _get_project_combined_extent()
            if project_extent is None or project_extent.isEmpty():
                return _fail("错误：无法获取有效地图范围，请确认工程中存在可用图层")

            # 获取页面大小，根据页面大小来自动确定地图框位置
            page_size = _layout_page_size_mm(layout)
            if not page_size:
                return _fail("错误：无法获取布局页面大小")

            page_w, page_h = page_size
            if page_w <= 0 or page_h <= 0:
                return _fail("错误：布局页面大小无效")

            # 在页面 85% 的可用区域内保留完整地图范围，同时让地图框
            # 与数据范围使用同一长宽比，避免 QGIS 为了适配框体而在
            # 主图四周自动增加大块空白。
            width, height = _map_frame_size_for_extent(
                page_width_mm=page_w,
                page_height_mm=page_h,
                extent=project_extent,
            )
            x = page_w / 2.0 - width / 2.0
            y = page_h / 2.0 - height / 2.0

            map_item = QgsLayoutItemMap(layout)
            map_item.setId(item_id)
            map_item.setCrs(project.crs())

            # theme_name = _default_map_theme_name(project)
            # if theme_name and hasattr(map_item, "setFollowVisibilityPreset"):
            #     map_item.setFollowVisibilityPreset(True)
            #     if hasattr(map_item, "setFollowVisibilityPresetName"):
            #         map_item.setFollowVisibilityPresetName(theme_name)
            #     if hasattr(map_item, "setKeepLayerSet"):
            #         map_item.setKeepLayerSet(False)
            #     if hasattr(map_item, "setKeepLayerStyles"):
            #         map_item.setKeepLayerStyles(False)
            #     if hasattr(map_item, "setLayerStyleOverrides"):
            #         map_item.setLayerStyleOverrides({})
            # else:
            #     visible_layers = _visible_layers_in_project_draw_order(project)
            #     if visible_layers and hasattr(map_item, "setLayers"):
            #         map_item.setLayers(visible_layers)
            #         if hasattr(map_item, "setKeepLayerSet"):
            #             map_item.setKeepLayerSet(True)

            map_item.attemptResize(
                QgsLayoutSize(width, height, QgsUnitTypes.LayoutMillimeters)
            )
            map_item.attemptMove(
                QgsLayoutPoint(x, y, QgsUnitTypes.LayoutMillimeters)
            )
            layout.addLayoutItem(map_item)
            map_item.zoomToExtent(project_extent)

            refresh_canvas()

            logger.info(
                "layout_add_map 完成: layout=%s, item_id=%s",
                layout_name,
                item_id,
            )
            return success_with_layout_data(
                output=item_id,
                info=f"地图框已添加到布局 '{layout_name}'，位置 ({x:.2f}, {y:.2f})，尺寸 {width:.2f}x{height:.2f}mm",
                data=None,
            )
        return run_in_main_thread(_execute)

    except Exception as e:
        return {"success": False, "output": "", "info": f"添加地图框失败: {str(e)}"}


def add_legend_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:

        def _execute():
            layout_name = params.get("layout")
            map_id = params.get("map_id")
            item_id = params.get("item_id", "legend1")
            title = params.get("title", "图例")
            x = params.get("x", 10)
            y = params.get("y", 150)
            width = params.get("width", 30)
            height = params.get("height", 60)
            if not layout_name:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：缺少布局名称参数",
                }

            project = QgsProject.instance()
            layout = project.layoutManager().layoutByName(layout_name)

            if not layout:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到布局 '{layout_name}'",
                }
            
            map_item = layout.itemById(map_id)
            if not map_item or not isinstance(map_item, QgsLayoutItemMap):
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到地图框 '{map_id}'",
                }

            legend = QgsLayoutItemLegend(layout)
            legend.attemptSetSceneRect(QRectF(x, y, width, height))
            legend.setId(item_id)
            legend.setLinkedMap(map_item)
            legend.setTitle(title)
            auto_update = params.get("auto_update_model")
            resize_to_contents = params.get("resize_to_contents")
            legend.setAutoUpdateModel(True if auto_update is None else bool(auto_update))
            legend.setResizeToContents(False if resize_to_contents is None else bool(resize_to_contents))
            layout.addLayoutItem(legend)


            legend.updateLegend()
            QgsLayoutExporter(layout).renderPageToImage(0)
            QgsApplication.processEvents()
            if legend.resizeToContents():
                legend.adjustBoxSize()

            logger.info(
                "layout_add_legend 完成: layout=%s, item_id=%s",
                layout_name,
                item_id,
            )
            return success_with_layout_data(
                output=item_id,
                info=f"图例已添加到布局 '{layout_name}'，关联地图框: {map_id}",
                data=None,
            )

        return run_in_main_thread(_execute)

    except Exception as e:
        return {"success": False, "output": "", "info": f"添加图例失败: {str(e)}"}


def set_map_extent_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    """Fit the map view to a selected layer or explicit map-CRS bounds."""
    def _execute():
        from autocarto.tools.system._shared.layout_module_loader import load_layout_handler
        _item_properties = load_layout_handler(
            "autocarto.tools.system._shared.print_layout_advanced_impl", "_item_properties",
            os.path.join(os.path.dirname(__file__), "print_layout_advanced_impl.py"),
        )

        project = QgsProject.instance()
        layout = project.layoutManager().layoutByName(str(params.get("layout") or ""))
        if layout is None:
            raise ValueError("未找到指定布局")
        item = layout.itemById(str(params.get("item_id") or ""))
        if not isinstance(item, QgsLayoutItemMap):
            raise ValueError("未找到指定地图框")
        if not item.crs().isValid():
            raise ValueError("地图框 CRS 无效")

        keys = ("xmin", "ymin", "xmax", "ymax")
        explicit = [params.get(key) is not None for key in keys]
        layer_ref = str(params.get("layer") or "").strip()
        use_canvas = bool(params.get("use_canvas", False))
        if sum((any(explicit), bool(layer_ref), use_canvas)) != 1:
            raise ValueError("请只选择一种范围来源：四个坐标、layer 或 use_canvas=true")
        if any(explicit):
            if not all(explicit):
                raise ValueError("显式范围必须同时提供 xmin、ymin、xmax、ymax")
            values = [float(params[key]) for key in keys]
            if not all(math.isfinite(value) for value in values):
                raise ValueError("范围坐标必须为有限数值")
            if values[0] >= values[2] or values[1] >= values[3]:
                raise ValueError("范围必须满足 xmin < xmax 且 ymin < ymax")
            extent = QgsRectangle(*values)
        elif layer_ref:
            layer = project.mapLayer(layer_ref)
            if layer is None:
                matches = project.mapLayersByName(layer_ref)
                if len(matches) != 1:
                    raise ValueError("图层名称不存在或不唯一，请使用图层 ID")
                layer = matches[0]
            if not layer.isValid() or not layer.crs().isValid():
                raise ValueError("图层或图层 CRS 无效")
            transformer = QgsCoordinateTransform(layer.crs(), item.crs(), project)
            if isinstance(layer, QgsVectorLayer):
                extent = _get_transformed_vector_extent(layer, transformer)
            else:
                extent = transformer.transformBoundingBox(layer.extent())
        else:
            # Backward-compatible opt-in: the project union is in project CRS.
            extent = _get_project_combined_extent()
            if extent is not None and project.crs() != item.crs():
                extent = QgsCoordinateTransform(project.crs(), item.crs(), project).transformBoundingBox(extent)
        if extent is None or extent.isEmpty() or not extent.isFinite():
            raise ValueError("没有可用的二维地图范围")
        padding = float(params.get("padding") if params.get("padding") is not None else 0.03)
        if not math.isfinite(padding) or padding < 0 or padding > 1:
            raise ValueError("padding 必须为 0 到 1 的范围外扩比例")
        padded = QgsRectangle(extent)
        padded.scale(1 + 2 * padding)
        # zoomToExtent expands the view for the frame aspect ratio without
        # changing the frame; setExtent permits QGIS to adapt the frame height.
        if params.get("preserve_frame_size") is False:
            item.setExtent(padded)
        else:
            item.zoomToExtent(padded)
        item.refresh()
        refresh_canvas()
        data = _item_properties(item)
        data["requested_extent"] = dict(zip(keys, [padded.xMinimum(), padded.yMinimum(), padded.xMaximum(), padded.yMaximum()]))
        data["padding"] = padding
        return success_with_layout_data(output=item.id(), info="地图框范围已更新", data=data)

    try:
        return run_in_main_thread(_execute)
    except Exception as exc:
        return {"success": False, "output": "", "info": f"设置地图框范围失败: {exc}"}


def add_scalebar_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:

        def _execute():
            layout_name = params.get("layout")
            map_id = params.get("map_id")
            item_id = params.get("item_id", "scalebar1")
            x = params.get("x", 150)
            y = params.get("y", 170)
            width = float(params.get("width", 40) or 40)
            height = float(params.get("height", 12) or 12)
            segments = 4

            if not layout_name:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：缺少布局名称参数",
                }

            project = QgsProject.instance()
            layout = project.layoutManager().layoutByName(layout_name)

            if not layout:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到布局 '{layout_name}'",
                }

            map_item = layout.itemById(map_id)
            if not map_item or not isinstance(map_item, QgsLayoutItemMap):
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到地图 '{map_id}'",
                }

            scalebar = QgsLayoutItemScaleBar(layout)
            scalebar.attemptSetSceneRect(QRectF(x, y, width, height))
            scalebar.setId(item_id)
            scalebar.setLinkedMap(map_item)
            style_map = {
                "single_box": "Single Box",
                "double_box": "Double Box",
                "line": "Line Ticks Middle",
                "numeric": "Numeric",
            }
            style_name = style_map.get(
                str(params.get("style") or "single_box").strip().lower(),
                "Single Box",
            )
            scalebar.setStyle(style_name)
            scalebar.setNumberOfSegments(segments)
            scalebar.setNumberOfSegmentsLeft(0)

            layout.addLayoutItem(scalebar)

            _set_scalebar_units_from_map_scale(
                scalebar=scalebar,
                map_item=map_item,
                scalebar_width_mm=width,
                segments=segments,
            )

            # 让 QGIS 在目标宽度范围内选择合适的整值分段距离。width 是
            # 比例尺条带的目标上限，不是比例尺元素（含文字和边距）的最终外框宽度。
            # 最终定位必须使用 QGIS 返回的实际 rect_mm。
            fit_width_mode = getattr(
                getattr(Qgis, "ScaleBarSegmentSizeMode", None),
                "FitWidth",
                None,
            )
            if fit_width_mode is not None:
                scalebar.setSegmentSizeMode(fit_width_mode)
                scalebar.setMinimumBarWidth(max(width * 0.75, 1.0))
                scalebar.setMaximumBarWidth(max(width, 1.0))

            scalebar.update()
            refresh = getattr(scalebar, "refresh", None)
            if callable(refresh):
                refresh()
            QgsApplication.processEvents()

            _, data, validation_error = validate_layout_item(
                layout=layout,
                layout_name=layout_name,
                item_id=item_id,
                expected_type=QgsLayoutItemScaleBar,
                item_type="比例尺",
                auto_adjust=False,
            )
            if validation_error:
                return {"success": False, "output": "", "info": validation_error}
            data.update(
                {
                    "target_bar_width_mm": width,
                    "bar_width_range_mm": {
                        "minimum": round(max(width * 0.75, 1.0), 3),
                        "maximum": round(max(width, 1.0), 3),
                    },
                    "units_per_segment": float(scalebar.unitsPerSegment()),
                    "segments": int(scalebar.numberOfSegments()),
                }
            )

            logger.info(
                "layout_add_scalebar 完成: layout=%s, item_id=%s",
                layout_name,
                item_id,
            )
            return success_with_layout_data(
                output=item_id,
                info=f"比例尺已添加到布局 '{layout_name}'",
                data=data,
            )

        return run_in_main_thread(_execute)

    except Exception as e:
        return {"success": False, "output": "", "info": f"添加比例尺失败: {str(e)}"}


def add_north_arrow_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:

        def _execute():
            layout_name = params.get("layout")
            item_id = params.get("item_id", "north_arrow1")
            x = params.get("x", 250)
            y = params.get("y", 10)
            width = params.get("width", 20)
            height = params.get("height", 30)

            if not layout_name:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：缺少布局名称参数",
                }

            project = QgsProject.instance()
            layout = project.layoutManager().layoutByName(layout_name)
            if not layout:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到布局 '{layout_name}'",
                }
            svg_paths = QgsApplication.svgPaths()
            north_arrow_path = None
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
            style_idx = 2

            for svg_path in svg_paths:
                candidate = os.path.join(svg_path, arrow_styles[style_idx])
                if os.path.exists(candidate):
                    north_arrow_path = candidate
                    break

            if not north_arrow_path:
                for svg_path in svg_paths:
                    for arrow in arrow_styles:
                        candidate = os.path.join(svg_path, arrow)
                        if os.path.exists(candidate):
                            north_arrow_path = candidate
                            break
                    if north_arrow_path:
                        break

            picture = QgsLayoutItemPicture(layout)
            picture.attemptSetSceneRect(QRectF(x, y, width, height))
            picture.setId(item_id)

            if north_arrow_path:
                picture.setPicturePath(north_arrow_path)

            layout.addLayoutItem(picture)

            logger.info(
                "layout_add_north_arrow 完成: layout=%s, item_id=%s",
                layout_name,
                item_id
            )
            return success_with_layout_data(
                output=item_id,
                info=f"指北针已添加到布局 '{layout_name}'，位置 ({x}, {y})",
                data=None,
            )

        return run_in_main_thread(_execute)

    except Exception as e:
        return {"success": False, "output": "", "info": f"添加指北针失败: {str(e)}"}
    

def move_item_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layout_name = params.get("layout")
        item_id = params.get("item_id")
        x = params.get("x")
        y = params.get("y")

        if not layout_name or not item_id or x is None or y is None:
            return {"success": False, "output": "", "info": "错误：缺少必填参数"}

        project = QgsProject.instance()
        layout = project.layoutManager().layoutByName(layout_name)
        if not layout:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到布局 '{layout_name}'",
            }

        item = layout.itemById(item_id)
        if not item:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到元素 '{item_id}'",
            }

        item.attemptMove(QgsLayoutPoint(x, y, QgsUnitTypes.LayoutMillimeters))
        return {
            "success": True,
            "output": item_id,
            "info": f"元素 '{item_id}' 已移动到位置 ({x}, {y})",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"移动元素失败: {str(e)}"}


def add_label_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:

        def _execute():
            layout_name = params.get("layout")
            text = params.get("text")
            item_id = params.get("item_id")
            font_size = 4
            font_color = "#000000"

            if not layout_name:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：缺少布局名称参数",
                }

            project = QgsProject.instance()
            layout = project.layoutManager().layoutByName(layout_name)
            if not layout:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到布局 '{layout_name}'",
                }
            if text is None or str(text) == "":
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：缺少标签文本参数",
                }

            label = QgsLayoutItemLabel(layout)
            label.setId(item_id)

            text_format = QgsTextFormat()
            font = QFont()
            font.setPointSize(font_size)
            text_format.setFont(font)
            text_format.setSize(font_size)
            text_format.setColor(QColor(font_color))
            try:
                label.setTextFormat(text_format)
            except AttributeError:
                label.setFont(font)
                if hasattr(label, "setFontColor"):
                    label.setFontColor(QColor(font_color))
                if hasattr(label, "refresh"):
                    label.refresh()

            # 先加入布局，再按文本自动计算并应用尺寸，最后移动到目标位置
            layout.addLayoutItem(label)
            label.setText(str(text))
            label.adjustSizeToText()
            label.setFrameEnabled(False)

            _, data, validate_error = validate_layout_item(
                layout=layout,
                layout_name=layout_name,
                item_id=item_id,
                expected_type=QgsLayoutItemLabel,
                item_type="label",
            )
            if validate_error:
                logger.warning(
                    "layout_add_label 校验失败: layout=%s, item=%s, error=%s",
                    layout_name,
                    item_id,
                    validate_error,
                )
                return {"success": False, "output": "", "info": validate_error}

            logger.info(
                "layout_add_label 完成: layout=%s, item_id=%s, rect=%s",
                layout_name,
                item_id,
                data.get("rect_mm", {})
            )
            return success_with_layout_data(
                output=item_id,
                info=f"标签已添加到布局 '{layout_name}'，文本 {text}",
                data=data,
            )

        return run_in_main_thread(_execute)

    except Exception as e:
        return {"success": False, "output": "", "info": f"添加标签失败: {str(e)}"}


def add_decorative_border_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:

        def _execute():
            layout_name = params.get("layout")
            def _parse_number(name: str, default: float) -> tuple[bool, float, str]:
                raw = params.get(name, default)
                if raw is None or raw == "":
                    return True, float(default), ""
                try:
                    return True, float(raw), ""
                except (TypeError, ValueError):
                    return False, 0.0, f"错误：参数 '{name}' 必须是数字，当前值为 '{raw}'"

            # 布局元素 ID（用于后续精准查询/调整）
            item_id = str(params.get("item_id", "decorative_border_1") or "decorative_border_1")

            # SVG 图标路径：固定使用内置默认样式
            default_relative_path = None
            svg_paths = QgsApplication.svgPaths()
            for svg_path in svg_paths:
                candidate = os.path.join(svg_path, "arrows", "NorthArrow_01.svg")
                if os.path.exists(candidate):
                    default_relative_path = candidate
                    break
            svg_path = default_relative_path
            if not svg_path:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：未找到内置 SVG 样式（arrows/NorthArrow_01.svg）",
                }

            # SVG 标记宽度（单位 mm）
            ok, marker_width, error = _parse_number("marker_width", 4)
            if not ok:
                return {"success": False, "output": "", "info": error}
            # SVG 标记高度（单位 mm）
            ok, marker_height, error = _parse_number("marker_height", 4)
            if not ok:
                return {"success": False, "output": "", "info": error}
            # 标记线间隔（单位 mm）；为空时默认等于标记高度
            marker_interval = params.get("marker_interval")
            if marker_interval is None or marker_interval == "":
                marker_interval = marker_height
            else:
                try:
                    marker_interval = float(marker_interval)
                except (TypeError, ValueError):
                    return {
                        "success": False,
                        "output": "",
                        "info": f"错误：参数 'marker_interval' 必须是数字，当前值为 '{marker_interval}'",
                    }
            # 线颜色（HEX）
            line_color = str(params.get("line_color", "#000000") or "#000000")
            # 线宽（单位 mm）
            ok, line_width, error = _parse_number("line_width", 0.3)
            if not ok:
                return {"success": False, "output": "", "info": error}
            # 双线偏移量（单位 mm）：将自动生成 +offset 与 -offset 两条线
            ok, offset_value, error = _parse_number("offset", 3)
            if not ok:
                return {"success": False, "output": "", "info": error}
            offset = abs(offset_value)
            # 填充颜色（默认透明）
            fill_color = str(params.get("fill_color", "#FFFFFF00") or "#FFFFFF00")
            if not layout_name:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：缺少布局名称参数",
                }

            project = QgsProject.instance()
            layout = project.layoutManager().layoutByName(layout_name)
            if not layout:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到布局 '{layout_name}'",
                }

            map_items = [item for item in layout.items() if isinstance(item, QgsLayoutItemMap)]
            if not map_items:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：布局 '{layout_name}' 中未找到地图框，无法自动放置装饰边框",
                }
            target_map = max(map_items, key=lambda item: _item_scene_rect_mm(item).width() * _item_scene_rect_mm(item).height())
            map_rect = _item_scene_rect_mm(target_map)
            x = float(map_rect.x())
            y = float(map_rect.y())
            width = float(map_rect.width())
            height = float(map_rect.height())
            if width <= 0 or height <= 0:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：地图框尺寸无效，无法自动放置装饰边框",
                }

            # 线符号默认以路径中线向两侧渲染。这里把基准矩形继续外推，
            # 保证渲染“最内缘”也在地图框外侧（而不是仅仅贴边）。
            inward_bleed = max(
                max(0.0, offset) + max(0.0, line_width) / 2.0,
                max(0.0, marker_width, marker_height) / 2.0,
            )
            outward_clearance = max(0.2, max(0.0, line_width) / 2.0)
            base_expand = inward_bleed + outward_clearance
            x -= base_expand
            y -= base_expand
            width += 2.0 * base_expand
            height += 2.0 * base_expand

            shape = QgsLayoutItemShape(layout)
            shape.attemptSetSceneRect(QRectF(x, y, width, height))
            shape.setId(item_id)
            shape.setShapeType(QgsLayoutItemShape.Rectangle)

            symbol = QgsFillSymbol.createSimple({"color": fill_color, "outline_style": "no"})
            while symbol.symbolLayerCount() > 0:
                symbol.deleteSymbolLayer(0)

            marker_symbol = QgsMarkerSymbol.createSimple({"name": "circle", "color": line_color})
            if marker_symbol is None:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：无法创建 QgsMarkerSymbol（createSimple 返回 None）",
                }
            marker_symbol.deleteSymbolLayer(0)

            try:
                marker_svg_layer = QgsSvgMarkerSymbolLayer(svg_path)
            except Exception as svg_error:
                logger.exception(
                    "layout_add_decorative_border 创建 QgsSvgMarkerSymbolLayer 失败: svg_path=%s",
                    svg_path,
                )
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：创建 SVG 标记图层失败: {str(svg_error)}",
                }

            if marker_svg_layer is None:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：无法创建 SVG 标记图层（路径: {svg_path}）",
                }

            marker_svg_layer.setSize(max(marker_width, marker_height))
            if hasattr(marker_svg_layer, "setSizeUnit"):
                marker_svg_layer.setSizeUnit(QgsUnitTypes.RenderMillimeters)
            marker_symbol.appendSymbolLayer(marker_svg_layer)

            marker_line_layer = QgsMarkerLineSymbolLayer()
            marker_line_layer.setSubSymbol(marker_symbol)
            marker_line_layer.setInterval(max(0.1, marker_interval))
            if hasattr(marker_line_layer, "setIntervalUnit"):
                marker_line_layer.setIntervalUnit(QgsUnitTypes.RenderMillimeters)
            symbol.appendSymbolLayer(marker_line_layer)

            plus_line_layer = QgsSimpleLineSymbolLayer.create(
                {
                    "line_color": line_color,
                    "line_width": str(line_width),
                    "offset": str(offset),
                }
            )
            minus_line_layer = QgsSimpleLineSymbolLayer.create(
                {
                    "line_color": line_color,
                    "line_width": str(line_width),
                    "offset": str(-offset),
                }
            )
            if plus_line_layer is not None:
                symbol.appendSymbolLayer(plus_line_layer)
            if minus_line_layer is not None:
                symbol.appendSymbolLayer(minus_line_layer)

            shape.setSymbol(symbol)
            if hasattr(shape, "setCustomProperty"):
                shape.setCustomProperty("autocarto/item_role", "decorative_border")
            layout.addLayoutItem(shape)

            _, data, validate_error = validate_layout_item(
                layout=layout,
                layout_name=layout_name,
                item_id=item_id,
                expected_type=QgsLayoutItemShape,
                item_type="decorative_border",
            )
            if validate_error:
                logger.warning(
                    "layout_add_decorative_border 校验失败: layout=%s, item=%s, error=%s",
                    layout_name,
                    item_id,
                    validate_error,
                )
                return {"success": False, "output": "", "info": validate_error}

            logger.info(
                "layout_add_decorative_border 完成: layout=%s, item_id=%s, map_id=%s, rect=%s, auto_adjusted=%s",
                layout_name,
                item_id,
                target_map.id(),
                data.get("rect_mm", {}),
                data.get("auto_adjusted", False),
            )
            return success_with_layout_data(
                output=item_id,
                info=f"装饰边框已添加到布局 '{layout_name}'，已自动包裹地图框 '{target_map.id()}'",
                data=data,
            )

        return run_in_main_thread(_execute)

    except Exception as e:
        logger.exception("layout_add_decorative_border 异常")
        return {"success": False, "output": "", "info": f"添加装饰边框失败: {str(e)}"}
    

def add_picture_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:

        def _execute():
            layout_name = params.get("layout")
            picture_path = params.get("path")
            x = params.get("x", 10)
            y = params.get("y", 10)
            width = params.get("width", 50)
            height = params.get("height", 50)
            item_id = params.get("item_id", "picture1")

            if not layout_name or not picture_path:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：缺少布局名称或图片路径",
                }

            project = QgsProject.instance()
            layout = project.layoutManager().layoutByName(layout_name)
            if not layout:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到布局 '{layout_name}'",
                }

            picture = QgsLayoutItemPicture(layout)
            picture.attemptSetSceneRect(QRectF(x, y, width, height))
            picture.setId(item_id)
            picture.setPicturePath(picture_path)
            layout.addLayoutItem(picture)

            _, data, validate_error = validate_layout_item(
                layout=layout,
                layout_name=layout_name,
                item_id=item_id,
                expected_type=QgsLayoutItemPicture,
                item_type="picture",
            )
            if validate_error:
                logger.warning(
                    "layout_add_picture 校验失败: layout=%s, item=%s, error=%s",
                    layout_name,
                    item_id,
                    validate_error,
                )
                return {"success": False, "output": "", "info": validate_error}

            logger.info(
                "layout_add_picture 完成: layout=%s, item_id=%s, rect=%s, auto_adjusted=%s",
                layout_name,
                item_id,
                data.get("rect_mm", {}),
                data.get("auto_adjusted", False),
            )
            return success_with_layout_data(
                output=item_id,
                info=f"图片已添加到布局 '{layout_name}'，路径 {picture_path}",
                data=data,
            )

        return run_in_main_thread(_execute)

    except Exception as e:
        return {"success": False, "output": "", "info": f"添加图片失败: {str(e)}"}


def add_shape_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:

        def _execute():
            layout_name = params.get("layout")
            shape_type = params.get("shape_type", "rectangle")
            x = params.get("x", 10)
            y = params.get("y", 10)
            width = params.get("width", 50)
            height = params.get("height", 50)
            item_id = params.get("item_id", "shape1")
            fill_color = params.get("fill_color", "#FFFFFF")
            stroke_color = params.get("stroke_color", "#000000")

            if not layout_name:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：缺少布局名称参数",
                }

            project = QgsProject.instance()
            layout = project.layoutManager().layoutByName(layout_name)
            if not layout:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到布局 '{layout_name}'",
                }

            shape = QgsLayoutItemShape(layout)
            shape.attemptSetSceneRect(QRectF(x, y, width, height))
            shape.setId(item_id)

            shape_map = {
                "rectangle": QgsLayoutItemShape.Rectangle,
                "ellipse": QgsLayoutItemShape.Ellipse,
                "triangle": QgsLayoutItemShape.Triangle,
            }
            shape.setShapeType(shape_map.get(shape_type, QgsLayoutItemShape.Rectangle))

            symbol = QgsFillSymbol.createSimple(
                {
                    "color": fill_color,
                    "outline_color": stroke_color,
                    "outline_width": "0.5",
                }
            )
            shape.setSymbol(symbol)
            layout.addLayoutItem(shape)

            _, data, validate_error = validate_layout_item(
                layout=layout,
                layout_name=layout_name,
                item_id=item_id,
                expected_type=QgsLayoutItemShape,
                item_type="shape",
            )
            if validate_error:
                logger.warning(
                    "layout_add_shape 校验失败: layout=%s, item=%s, error=%s",
                    layout_name,
                    item_id,
                    validate_error,
                )
                return {"success": False, "output": "", "info": validate_error}

            logger.info(
                "layout_add_shape 完成: layout=%s, item_id=%s, rect=%s, auto_adjusted=%s",
                layout_name,
                item_id,
                data.get("rect_mm", {}),
                data.get("auto_adjusted", False),
            )
            return success_with_layout_data(
                output=item_id,
                info=f"形状已添加到布局 '{layout_name}'，类型 {shape_type}",
                data=data,
            )

        return run_in_main_thread(_execute)

    except Exception as e:
        return {"success": False, "output": "", "info": f"添加形状失败: {str(e)}"}


def remove_item_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layout_name = params.get("layout")
        item_id = params.get("item_id")

        if not layout_name or not item_id:
            return {
                "success": False,
                "output": "",
                "info": "错误：缺少布局名称或元素编号参数",
            }

        project = QgsProject.instance()
        layout = project.layoutManager().layoutByName(layout_name)
        if not layout:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到布局 '{layout_name}'",
            }

        item = layout.itemById(item_id)
        if not item:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到元素 '{item_id}'",
            }

        layout.removeLayoutItem(item)
        return {
            "success": True,
            "output": item_id,
            "info": f"元素 '{item_id}' 已从布局 '{layout_name}' 移除",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"删除元素失败: {str(e)}"}


def resize_item_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layout_name = params.get("layout")
        item_id = params.get("item_id")
        width = params.get("width")
        height = params.get("height")

        if not layout_name or not item_id or width is None or height is None:
            return {"success": False, "output": "", "info": "错误：缺少必填参数"}

        project = QgsProject.instance()
        layout = project.layoutManager().layoutByName(layout_name)
        if not layout:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到布局 '{layout_name}'",
            }

        item = layout.itemById(item_id)
        if not item:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到元素 '{item_id}'",
            }

        item.attemptResize(QgsLayoutSize(width, height, QgsUnitTypes.LayoutMillimeters))
        QgsApplication.processEvents()
        actual_rect = get_item_rect_mm(item)
        size_matched = (
            abs(float(actual_rect["width"]) - float(width)) <= 0.1
            and abs(float(actual_rect["height"]) - float(height)) <= 0.1
        )
        info = (
            f"元素 '{item_id}' 大小已调整为 {actual_rect['width']}x"
            f"{actual_rect['height']}mm"
            if size_matched
            else (
                f"元素 '{item_id}' 已请求调整为 {width}x{height}mm，"
                f"QGIS 实际尺寸为 {actual_rect['width']}x{actual_rect['height']}mm"
            )
        )
        return success_with_layout_data(
            output=item_id,
            info=info,
            data={
                "item_id": item_id,
                "requested_size_mm": {
                    "width": float(width),
                    "height": float(height),
                },
                "rect_mm": actual_rect,
                "auto_adjusted": not size_matched,
            },
        )

    except Exception as e:
        return {"success": False, "output": "", "info": f"调整元素大小失败: {str(e)}"}


def add_grid_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:

        def _execute():
            layout_name = params.get("layout")
            map_id = params.get("map_id")
            interval_x = params.get("interval_x")
            interval_y = params.get("interval_y")
            line_color = QColor(str(params.get("line_color", "#89DEEA") or "#89DEEA"))
            if not line_color.isValid():
                line_color = QColor("#000000")
            try:
                opacity = max(0.0, min(1.0, float(params.get("opacity", 0.7))))
            except (TypeError, ValueError):
                opacity = 0.7

            if not layout_name:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：缺少布局名称参数",
                }

            project = QgsProject.instance()
            layout = project.layoutManager().layoutByName(layout_name)
            if not layout:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到布局 '{layout_name}'",
                }

            map_item = layout.itemById(map_id)
            if not map_item or not isinstance(map_item, QgsLayoutItemMap):
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到地图框 '{map_id}'",
                }


            grid = QgsLayoutItemMapGrid(f"{map_id}_grid", map_item)
            grid.setStyle(QgsLayoutItemMapGrid.Solid)
            grid.setIntervalX(interval_x)
            grid.setIntervalY(interval_y)
            grid.setGridLineColor(line_color)

            line_symbol = QgsLineSymbol.createSimple({"line_color": line_color.name()})
            if line_symbol is not None:
                line_symbol.setOpacity(opacity)
                grid.setLineSymbol(line_symbol)

            # 如果指定了 grid_crs 则使用指定的 CRS；否则沿用工程 CRS。
            # 主图为了正确计算比例尺可能使用米制投影，但经纬网仍应保持
            # 工程原有的经纬度坐标，避免标注突然变成米制坐标。
            grid_crs = params.get("grid_crs", None)
            default_grid_crs = project.crs()
            if not default_grid_crs.isValid():
                default_grid_crs = map_item.crs()
            if grid_crs:
                from qgis.core import QgsCoordinateReferenceSystem
                crs = QgsCoordinateReferenceSystem(grid_crs)
                if crs.isValid():
                    grid.setCrs(crs)
                else:
                    grid.setCrs(default_grid_crs)
            else:
                grid.setCrs(default_grid_crs)

            grid.setAnnotationEnabled(True)
            grid.setAnnotationFrameDistance(2)
            grid.setAnnotationPrecision(2)

            # 上边框
            grid.setAnnotationPosition(QgsLayoutItemMapGrid.InsideMapFrame, QgsLayoutItemMapGrid.Top)
            grid.setAnnotationDirection(QgsLayoutItemMapGrid.Horizontal, QgsLayoutItemMapGrid.Top)

            # 下边框
            grid.setAnnotationPosition(QgsLayoutItemMapGrid.InsideMapFrame, QgsLayoutItemMapGrid.Bottom)
            grid.setAnnotationDirection(QgsLayoutItemMapGrid.Horizontal, QgsLayoutItemMapGrid.Bottom)

            # 右边框
            grid.setAnnotationPosition(QgsLayoutItemMapGrid.InsideMapFrame, QgsLayoutItemMapGrid.Right)
            grid.setAnnotationDirection(QgsLayoutItemMapGrid.Vertical, QgsLayoutItemMapGrid.Right)

            # 左边框
            grid.setAnnotationPosition(QgsLayoutItemMapGrid.InsideMapFrame, QgsLayoutItemMapGrid.Left)
            grid.setAnnotationDirection(QgsLayoutItemMapGrid.Vertical, QgsLayoutItemMapGrid.Left)
            
            map_item.grids().addGrid(grid)
            refresh_canvas()

            data = {
                "layout": layout_name,
                "item_id": f"{map_id}_grid",
                "item_type": "map_grid",
                "rect_mm": {},
                "auto_adjusted": False,
                "line_color": line_color.name(),
                "opacity": opacity,
            }
            logger.info(
                "layout_add_grid 完成: layout=%s, map_id=%s, interval=(%s,%s), line_color=%s, opacity=%s",
                layout_name,
                map_id,
                interval_x,
                interval_y,
                line_color.name(),
                opacity,
            )
            return success_with_layout_data(
                output=f"{map_id}_grid",
                info=f"线网格已添加到地图框 '{map_id}'，间隔 {interval_x}x{interval_y}，颜色 {line_color.name()}，透明度 {opacity}",
                data=data,
            )

        return run_in_main_thread(_execute)

    except Exception as e:
        return {"success": False, "output": "", "info": f"添加网格失败: {str(e)}"}


def add_attribute_table_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:

        def _execute():
            layout_name = params.get("layout")
            layer_name = params.get("layer_name")
            x = params.get("x", 10)
            y = params.get("y", 180)
            width = params.get("width", 200)
            height = params.get("height", 50)
            item_id = params.get("item_id", "table1")
            max_features = params.get("max_features", 10)

            if not layout_name or not layer_name:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：缺少布局名称或图层名称",
                }

            project = QgsProject.instance()
            layout = project.layoutManager().layoutByName(layout_name)
            if not layout:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到布局 '{layout_name}'",
                }

            layers = project.mapLayersByName(layer_name)
            if not layers:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到图层 '{layer_name}'",
                }

            layer = layers[0]
            table = QgsLayoutItemAttributeTable(layout)
            table.setVectorLayer(layer)
            table.setMaximumNumberOfFeatures(max_features)

            frame = QgsLayoutFrame(layout, table)
            frame.attemptSetSceneRect(QRectF(x, y, width, height))
            frame.setId(item_id)
            table.addFrame(frame)
            layout.addMultiFrame(table)

            def _validate_attribute_table_frame(item) -> Optional[str]:
                if not isinstance(item, QgsLayoutFrame):
                    return f"错误：属性表框架 '{item_id}' 类型异常"
                if item.multiFrame() != table:
                    return f"错误：属性表框架 '{item_id}' 未正确挂载到属性表"
                if item not in table.frames():
                    return f"错误：属性表框架 '{item_id}' 未注册到属性表框架列表"
                if item.rect().width() <= 0 or item.rect().height() <= 0:
                    return f"错误：属性表框架 '{item_id}' 不可见（尺寸无效）"
                return None

            _, data, validate_error = validate_layout_item(
                layout=layout,
                layout_name=layout_name,
                item_id=item_id,
                expected_type=QgsLayoutFrame,
                item_type="attribute_table_frame",
                extra_validator=_validate_attribute_table_frame,
            )
            if validate_error:
                logger.warning(
                    "layout_add_attribute_table 校验失败: layout=%s, item=%s, error=%s",
                    layout_name,
                    item_id,
                    validate_error,
                )
                return {"success": False, "output": "", "info": validate_error}

            logger.info(
                "layout_add_attribute_table 完成: layout=%s, item_id=%s, rect=%s, auto_adjusted=%s",
                layout_name,
                item_id,
                data.get("rect_mm", {}),
                data.get("auto_adjusted", False),
            )
            return success_with_layout_data(
                output=item_id,
                info=f"属性表已添加到布局 '{layout_name}'，图层 {layer_name}，最大要素数: {max_features}",
                data=data,
            )

        return run_in_main_thread(_execute)

    except Exception as e:
        return {"success": False, "output": "", "info": f"添加属性表失败: {str(e)}"}


def lock_item_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layout_name = params.get("layout")
        item_id = params.get("item_id")
        locked = params.get("locked", True)

        if not layout_name or not item_id:
            return {
                "success": False,
                "output": "",
                "info": "错误：缺少布局名称或元素编号参数",
            }

        project = QgsProject.instance()
        layout = project.layoutManager().layoutByName(layout_name)
        if not layout:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到布局 '{layout_name}'",
            }

        item = layout.itemById(item_id)
        if not item:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到元素 '{item_id}'",
            }

        item.setLocked(locked)
        status = "已锁定" if locked else "已解锁"
        return {
            "success": True,
            "output": item_id,
            "info": f"元素 '{item_id}' {status}",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"锁定/解锁元素失败: {str(e)}"}


def align_items_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layout_name = params.get("layout")
        item_ids = params.get("item_ids", [])
        alignment = params.get("alignment", "left")
        reference_item_id = str(params.get("reference_item_id") or "").strip()

        minimum_items = 1 if reference_item_id else 2
        if not layout_name or not item_ids or len(item_ids) < minimum_items:
            return {
                "success": False,
                "output": "",
                "info": (
                    "错误：指定 reference_item_id 时至少需要一个待对齐元素"
                    if reference_item_id
                    else "错误：需要布局名称和至少两个元素编号"
                ),
            }

        project = QgsProject.instance()
        layout = project.layoutManager().layoutByName(layout_name)
        if not layout:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到布局 '{layout_name}'",
            }

        items = []
        for item_id in item_ids:
            item = layout.itemById(item_id)
            if item:
                items.append(item)

        if len(items) < minimum_items:
            return {
                "success": False,
                "output": "",
                "info": (
                    "错误：未找到待对齐元素"
                    if reference_item_id
                    else "错误：找到的元素少于 2 个"
                ),
            }

        if reference_item_id:
            reference_item = layout.itemById(reference_item_id)
            if reference_item is None:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到参考元素 '{reference_item_id}'",
                }

            reference_rect = get_item_rect_mm(reference_item)
            for item in items:
                rect = get_item_rect_mm(item)
                new_x = rect["x"]
                new_y = rect["y"]
                if alignment == "left":
                    new_x = reference_rect["x"]
                elif alignment == "right":
                    new_x = (
                        reference_rect["x"]
                        + reference_rect["width"]
                        - rect["width"]
                    )
                elif alignment == "top":
                    new_y = reference_rect["y"]
                elif alignment == "bottom":
                    new_y = (
                        reference_rect["y"]
                        + reference_rect["height"]
                        - rect["height"]
                    )
                elif alignment == "center_h":
                    new_x = (
                        reference_rect["x"]
                        + (reference_rect["width"] - rect["width"]) / 2.0
                    )
                elif alignment == "center_v":
                    new_y = (
                        reference_rect["y"]
                        + (reference_rect["height"] - rect["height"]) / 2.0
                    )
                else:
                    return {
                        "success": False,
                        "output": "",
                        "info": f"错误：不支持的对齐方式 '{alignment}'",
                    }
                item.attemptMove(
                    QgsLayoutPoint(new_x, new_y, QgsUnitTypes.LayoutMillimeters)
                )

            QgsApplication.processEvents()
            return success_with_layout_data(
                output=",".join(item_ids),
                info=(
                    f"{len(items)} 个元素已相对参考元素 '{reference_item_id}' "
                    f"按 '{alignment}' 方式对齐"
                ),
                data={
                    "reference_item_id": reference_item_id,
                    "reference_rect_mm": reference_rect,
                    "alignment": alignment,
                    "items": {
                        item.id(): get_item_rect_mm(item) for item in items
                    },
                },
            )

        if alignment == "left":
            ref = min(item.pos().x() for item in items)
            for item in items:
                item.attemptMove(
                    QgsLayoutPoint(ref, item.pos().y(), QgsUnitTypes.LayoutMillimeters)
                )
        elif alignment == "right":
            ref = max(item.pos().x() + item.rect().width() for item in items)
            for item in items:
                new_x = ref - item.rect().width()
                item.attemptMove(
                    QgsLayoutPoint(
                        new_x, item.pos().y(), QgsUnitTypes.LayoutMillimeters
                    )
                )
        elif alignment == "top":
            ref = min(item.pos().y() for item in items)
            for item in items:
                item.attemptMove(
                    QgsLayoutPoint(item.pos().x(), ref, QgsUnitTypes.LayoutMillimeters)
                )
        elif alignment == "bottom":
            ref = max(item.pos().y() + item.rect().height() for item in items)
            for item in items:
                new_y = ref - item.rect().height()
                item.attemptMove(
                    QgsLayoutPoint(
                        item.pos().x(), new_y, QgsUnitTypes.LayoutMillimeters
                    )
                )
        elif alignment == "center_h":
            center = sum(
                item.pos().x() + item.rect().width() / 2 for item in items
            ) / len(items)
            for item in items:
                new_x = center - item.rect().width() / 2
                item.attemptMove(
                    QgsLayoutPoint(
                        new_x, item.pos().y(), QgsUnitTypes.LayoutMillimeters
                    )
                )
        elif alignment == "center_v":
            center = sum(
                item.pos().y() + item.rect().height() / 2 for item in items
            ) / len(items)
            for item in items:
                new_y = center - item.rect().height() / 2
                item.attemptMove(
                    QgsLayoutPoint(
                        item.pos().x(), new_y, QgsUnitTypes.LayoutMillimeters
                    )
                )
        else:
            return {
                "success": False,
                "output": "",
                "info": f"错误：不支持的对齐方式 '{alignment}'",
            }

        return {
            "success": True,
            "output": ",".join(item_ids),
            "info": f"{len(items)} 个元素已按 '{alignment}' 方式对齐",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"对齐元素失败: {str(e)}"}


def add_overview_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:

        def _execute():
            layout_name = params.get("layout")
            main_map_id = params.get("main_map_id")
            x = params.get("x", 220)
            y = params.get("y", 120)
            width = params.get("width", 60)
            height = params.get("height", 50)
            item_id = params.get("item_id", "overview1")
            frame_color = params.get("frame_color", "#FF0000")

            if not layout_name or not main_map_id:
                return {
                    "success": False,
                    "output": "",
                    "info": "错误：缺少布局名称或主地图框编号参数",
                }

            project = QgsProject.instance()
            layout = project.layoutManager().layoutByName(layout_name)
            if not layout:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到布局 '{layout_name}'",
                }

            main_map = layout.itemById(main_map_id)
            if not main_map or not isinstance(main_map, QgsLayoutItemMap):
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到主地图框 '{main_map_id}'",
                }

            overview_map = QgsLayoutItemMap(layout)
            overview_map.attemptSetSceneRect(QRectF(x, y, width, height))
            overview_map.setId(item_id)
            main_extent = main_map.extent()
            overview_extent = main_extent.buffered(main_extent.width() * 0.5)
            overview_map.setExtent(overview_extent)
            layout.addLayoutItem(overview_map)

            from qgis.core import QgsLayoutItemMapOverview

            overview = QgsLayoutItemMapOverview(f"{item_id}_overview", overview_map)
            overview.setLinkedMap(main_map)
            overview.setEnabled(True)

            symbol = QgsFillSymbol.createSimple(
                {
                    "color": "255,255,255,0",
                    "outline_color": frame_color.replace("#", ""),
                    "outline_width": "0.5",
                }
            )
            overview.setFrameSymbol(symbol)
            overview_map.overviews().addOverview(overview)

            _, data, validate_error = validate_layout_item(
                layout=layout,
                layout_name=layout_name,
                item_id=item_id,
                expected_type=QgsLayoutItemMap,
                item_type="overview_map",
            )
            if validate_error:
                logger.warning(
                    "layout_add_overview 校验失败: layout=%s, item=%s, error=%s",
                    layout_name,
                    item_id,
                    validate_error,
                )
                return {"success": False, "output": "", "info": validate_error}

            logger.info(
                "layout_add_overview 完成: layout=%s, item_id=%s, rect=%s, auto_adjusted=%s",
                layout_name,
                item_id,
                data.get("rect_mm", {}),
                data.get("auto_adjusted", False),
            )
            return success_with_layout_data(
                output=item_id,
                info=f"鹰眼图已添加到布局 '{layout_name}'，关联主地图框 '{main_map_id}'",
                data=data,
            )

        return run_in_main_thread(_execute)

    except Exception as e:
        return {"success": False, "output": "", "info": f"添加鹰眼图失败: {str(e)}"}


def _wrap_main_thread(handler):
    def _wrapped(params: Dict[str, Any]) -> Dict[str, Any]:
        return run_in_main_thread(lambda: handler(params))

    _wrapped.__name__ = handler.__name__
    _wrapped.__doc__ = handler.__doc__
    return _wrapped


remove_item_handler = _wrap_main_thread(remove_item_handler)
move_item_handler = _wrap_main_thread(move_item_handler)
resize_item_handler = _wrap_main_thread(resize_item_handler)
lock_item_handler = _wrap_main_thread(lock_item_handler)
align_items_handler = _wrap_main_thread(align_items_handler)
