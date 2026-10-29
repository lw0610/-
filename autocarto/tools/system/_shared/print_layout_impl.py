from typing import Any, Dict, Tuple
import html
import re
from uuid import uuid4

from qgis.PyQt.QtXml import QDomDocument
from qgis.core import (
    QgsLayoutSize,
    QgsLayoutItem,
    QgsLayoutItemMap,
    QgsPrintLayout,
    QgsProject,
    QgsReadWriteContext,
    QgsUnitTypes,
)

from autocarto.tools.system._shared.layout_runtime import run_in_main_thread


def create_layout_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layout_name = params.get("name")
        page_width = params.get("page_width", 297)
        page_height = params.get("page_height", 210)
        units = params.get("units", "mm")

        if not layout_name:
            return {"success": False, "output": "", "info": "错误：缺少布局名称参数"}

        project = QgsProject.instance()
        existing_layout = project.layoutManager().layoutByName(layout_name)
        if existing_layout:
            return {
                "success": False,
                "output": "",
                "info": f"错误：布局 '{layout_name}' 已存在",
            }

        layout = QgsPrintLayout(project)
        layout.initializeDefaults()
        layout.setName(layout_name)

        unit_map = {
            "mm": QgsUnitTypes.LayoutMillimeters,
            "cm": QgsUnitTypes.LayoutCentimeters,
            "inches": QgsUnitTypes.LayoutInches,
            "pixels": QgsUnitTypes.LayoutPixels,
        }

        unit_type = unit_map.get(units, QgsUnitTypes.LayoutMillimeters)
        page_collection = layout.pageCollection()

        if page_collection.pageCount() > 0:
            page = page_collection.page(0)
            page.setPageSize(QgsLayoutSize(page_width, page_height, unit_type))

        project.layoutManager().addLayout(layout)

        return {
            "success": True,
            "output": layout_name,
            "info": f"布局创建成功。尺寸: {page_width}x{page_height} {units}",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"创建布局失败: {str(e)}"}


def delete_layout_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layout_name = params.get("name")

        if not layout_name:
            return {"success": False, "output": "", "info": "错误：缺少布局名称参数"}

        project = QgsProject.instance()
        layout = project.layoutManager().layoutByName(layout_name)

        if not layout:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到布局 '{layout_name}'",
            }

        project.layoutManager().removeLayout(layout)

        return {
            "success": True,
            "output": layout_name,
            "info": f"布局 '{layout_name}' 已删除",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"删除布局失败: {str(e)}"}


def list_layouts_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        from autocarto.tools.system._shared.layout_template_save_impl import layout_map_items
        project = QgsProject.instance()
        layouts = project.layoutManager().layouts()

        layout_list = []
        for layout in layouts:
            page_collection = layout.pageCollection()
            page_info = ""

            if page_collection.pageCount() > 0:
                page = page_collection.page(0)
                size = page.pageSize()
                page_info = f"{size.width():.1f}x{size.height():.1f}mm"

            layout_list.append(
                {
                    "name": layout.name(),
                    "page_count": page_collection.pageCount(),
                    "page_size": page_info,
                    "map_items": layout_map_items(layout),
                }
            )

        if not layout_list:
            return {"success": True, "output": "无布局", "info": "项目中没有打印布局", "data": {"layouts": []}}

        output_lines = [f"共 {len(layout_list)} 个布局:"]
        for layout_info in layout_list:
            output_lines.append(
                f"  - {layout_info['name']}: {layout_info['page_count']}页, {layout_info['page_size']}"
            )

        return {
            "success": True,
            "output": "\n".join(output_lines),
            "info": f"找到 {len(layout_list)} 个布局",
            "data": {"layouts": layout_list},
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"列出布局失败: {str(e)}"}


def duplicate_layout_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        source_name = params.get("source")
        target_name = params.get("target")

        if not source_name or not target_name:
            return {
                "success": False,
                "output": "",
                "info": "错误：缺少源布局或目标布局名称",
            }

        project = QgsProject.instance()
        source_layout = project.layoutManager().layoutByName(source_name)

        if not source_layout:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到源布局 '{source_name}'",
            }

        if project.layoutManager().layoutByName(target_name):
            return {
                "success": False,
                "output": "",
                "info": f"错误：目标布局 '{target_name}' 已存在",
            }

        new_layout = source_layout.clone()
        new_layout.setName(target_name)
        project.layoutManager().addLayout(new_layout)

        return {
            "success": True,
            "output": target_name,
            "info": f"布局已从 '{source_name}' 复制到 '{target_name}'",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"复制布局失败: {str(e)}"}


def rename_layout_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        old_name = params.get("old_name")
        new_name = params.get("new_name")

        if not old_name or not new_name:
            return {
                "success": False,
                "output": "",
                "info": "错误：缺少旧名称或新名称参数",
            }

        project = QgsProject.instance()
        layout = project.layoutManager().layoutByName(old_name)

        if not layout:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到布局 '{old_name}'",
            }

        if project.layoutManager().layoutByName(new_name):
            return {
                "success": False,
                "output": "",
                "info": f"错误：布局名称 '{new_name}' 已存在",
            }

        layout.setName(new_name)

        return {
            "success": True,
            "output": new_name,
            "info": f"布局已从 '{old_name}' 重命名为 '{new_name}'",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"重命名布局失败: {str(e)}"}


def get_layout_info_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layout_name = params.get("name")

        if not layout_name:
            return {"success": False, "output": "", "info": "错误：缺少布局名称参数"}

        project = QgsProject.instance()
        layout = project.layoutManager().layoutByName(layout_name)

        if not layout:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到布局 '{layout_name}'",
            }

        page_collection = layout.pageCollection()
        info_lines = [
            f"布局名称: {layout.name()}",
            f"页面数量: {page_collection.pageCount()}",
        ]

        if page_collection.pageCount() > 0:
            page = page_collection.page(0)
            size = page.pageSize()
            info_lines.append(f"页面尺寸: {size.width():.1f} x {size.height():.1f} mm")

        items = layout.items()
        item_types: Dict[str, int] = {}
        for item in items:
            item_type = type(item).__name__
            item_types[item_type] = item_types.get(item_type, 0) + 1

        info_lines.append(f"布局元素数量: {len(items)}")

        type_names = {
            "QgsLayoutItemMap": "地图框",
            "QgsLayoutItemLegend": "图例",
            "QgsLayoutItemLabel": "标签",
            "QgsLayoutItemScaleBar": "比例尺",
            "QgsLayoutItemPicture": "图片",
            "QgsLayoutItemShape": "形状",
            "QgsLayoutItemPolyline": "折线",
            "QgsLayoutItemPolygon": "多边形",
        }

        for item_type, count in item_types.items():
            type_name = type_names.get(item_type, f"类型{item_type}")
            info_lines.append(f"  - {type_name}: {count}")

        # Keep the human summary and expose actual items for deterministic plan
        # checks. A textual count cannot prove a title, north arrow or source note.
        from autocarto.tools.system._shared.print_layout_advanced_impl import _item_properties
        structured_items = []
        for item in items:
            if not isinstance(item, QgsLayoutItem):
                continue
            details = _item_properties(item)
            details["role"] = str(item.customProperty("autocarto/compose_role", "")
                                  or item.customProperty("autocarto/item_role", ""))
            if isinstance(item, QgsLayoutItemMap):
                details["grids"] = [{"name": grid.name(), "enabled": grid.enabled()}
                                    for grid in item.grids().asList()]
            structured_items.append(details)
        return {
            "success": True,
            "output": "\n".join(info_lines),
            "info": "布局信息获取成功",
            "layout": {"name": layout.name(), "items": structured_items},
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"获取布局信息失败: {str(e)}"}


def _qdom_parse_result(result: Any) -> Tuple[bool, str]:
    """Normalize Qt5/Qt6 ``QDomDocument.setContent`` return values."""

    if isinstance(result, tuple):
        success = bool(result[0]) if result else False
        if success:
            return True, ""

        message = str(result[1] or "") if len(result) > 1 else ""
        line = result[2] if len(result) > 2 else None
        column = result[3] if len(result) > 3 else None
        location = ""
        if line is not None and column is not None:
            location = f"（第 {line} 行，第 {column} 列）"
        return False, f"{message}{location}".strip()

    success = bool(result)
    if success:
        return True, ""

    message = ""
    error_message = getattr(result, "errorMessage", None)
    if callable(error_message):
        try:
            message = str(error_message() or "")
        except Exception:
            message = ""
    return False, message


_TEMPLATE_PLACEHOLDER = re.compile(r"\{\{\s*([^{}]+?)\s*\}\}")


def _template_texts(value):
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ValueError("texts 必须是占位符名称到文字的对象")
    texts = {}
    for key, text in value.items():
        if not isinstance(key, str) or not key.strip() or any(char in key for char in "{}\r\n"):
            raise ValueError("占位符名称必须是非空文字，不含花括号或换行")
        if not isinstance(text, str):
            raise ValueError(f"占位符“{key}”的替换值必须是文字")
        if key.strip() in texts:
            raise ValueError(f"占位符名称重复：{key.strip()}")
        texts[key.strip()] = text
    return texts


def _replace_template_text(text, texts, *, html_mode=False):
    """只替换一遍模板中的字面占位符，不再次解释调用者提供的文字。"""
    def replace(match):
        key = match.group(1).strip()
        if key not in texts:
            return match.group(0)
        value = texts[key]
        return html.escape(value).replace("\n", "<br/>") if html_mode else value
    return _TEMPLATE_PLACEHOLDER.sub(replace, text)


def _prepare_template_html_labels(document):
    """避免 QGIS 把 HTML 标签迁为不受当前运行时支持的 HTML 多框架项。"""
    from qgis.core import QgsLayoutItemRegistry
    restored_ids = {}
    nodes = document.elementsByTagName("LayoutItem")
    for index in range(nodes.count()):
        element = nodes.at(index).toElement()
        if element.attribute("type") != str(int(QgsLayoutItemRegistry.LayoutLabel)) or element.attribute("htmlState") != "1":
            continue
        temporary_id = "__template_html_"+uuid4().hex
        restored_ids[temporary_id] = element.attribute("id")
        element.setAttribute("id", temporary_id)
        element.setAttribute("htmlState", "0")
    return restored_ids


def _configure_template(layout, params, project):
    from qgis.core import QgsLayoutItemLabel, QgsLayoutItemMap, QgsLayoutItemLegend
    from autocarto.tools.system._shared.layout_compose_impl import _resolve_layers, _map_extent

    texts = _template_texts(params.get("texts"))
    warnings, unresolved = [], set()
    for item in layout.items():
        if isinstance(item, QgsLayoutItemLabel):
            text = _replace_template_text(item.text(), texts, html_mode=item.mode() == QgsLayoutItemLabel.ModeHtml)
            item.setText(text)
            remaining = sorted(set(match.group(1).strip() for match in _TEMPLATE_PLACEHOLDER.finditer(text)))
            if remaining:
                unresolved.update(remaining)
                placeholders = "、".join("{{"+key+"}}" for key in remaining)
                warnings.append(f"文本项“{item.id() or item.uuid()}”仍含未替换占位符：{placeholders}")

    map_requested = any(params.get(key) is not None for key in ("map_item_id", "map_layers", "extent_layers", "extent", "padding"))
    main_id = params.get("map_item_id")
    if main_id is None:
        main_id = "main_map"
    if not isinstance(main_id, str) or not main_id.strip():
        raise ValueError("主地图项 ID 必须是非空文字")
    main = layout.itemById(main_id.strip())
    if map_requested:
        if not isinstance(main, QgsLayoutItemMap):
            raise ValueError(f"模板中未找到地图项“{main_id}”")
        layers = _resolve_layers(params.get("map_layers"), project, visible=True)
        if not layers:
            raise ValueError("工程没有可见的有效图层")
        crs = main.crs() if main.crs().isValid() else project.crs()
        if not crs.isValid():
            raise ValueError("主图与工程的坐标系均无效")
        extent = _map_extent(params, layers, crs, project)
        main.setCrs(crs)
        main.setLayers(layers)
        main.setKeepLayerSet(True)
        main.zoomToExtent(extent)
    if params.get("legend_layers") is not None:
        layers = _resolve_layers(params["legend_layers"], project)
        legends = [item for item in layout.items() if isinstance(item, QgsLayoutItemLegend)]
        if not legends:
            warnings.append("模板中没有图例，legend_layers 未应用。")
        for legend in legends:
            legend.setAutoUpdateModel(False)
            if isinstance(main, QgsLayoutItemMap):
                legend.setLinkedMap(main)
            root = legend.model().rootGroup()
            root.removeAllChildren()
            for layer in layers:
                root.addLayer(layer)
            legend.updateLegend()
    return warnings, sorted(unresolved)


def _template_snapshot(layout):
    from qgis.core import QgsLayoutItem
    item_ids, rectangles, used = [], {}, set()
    for item in layout.items():
        if not isinstance(item, QgsLayoutItem):
            continue
        item_id = str(item.id() or "").strip()
        if not item_id or item_id in used:
            item_id = "template_item_"+uuid4().hex
            item.setId(item_id)
        used.add(item_id)
        item_ids.append(item_id)
        # 包含旋转/分组变换后的场景矩形；布局原生单位不一定是毫米。
        rect = item.mapRectToScene(item.rect())
        rectangles[item_id] = {
            key: float(layout.convertFromLayoutUnits(value, QgsUnitTypes.LayoutMillimeters).length())
            for key, value in (("x", rect.x()), ("y", rect.y()), ("width", rect.width()), ("height", rect.height()))
        }
    return sorted(item_ids), rectangles


def import_layout_template_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    """按 QPT 模板排版，在候选版面完成文字和主图设置后再替换。"""

    try:
        from autocarto.tools.system._shared.resource_handle import require_handle, handle_entry_path, handle_summary
        handle = require_handle(params.get("template"), "layout")
        if handle.get("format") == "layout_yaml":
            from autocarto.tools.system._shared.layout_compose_impl import compose_layout_handler
            texts = params.get("texts") or {}
            return compose_layout_handler(dict(params, spec=handle, layout=params.get("name"), title=texts.get("标题", texts.get("title", params.get("name"))), overview_text=texts.get("综述", texts.get("overview", ""))))
        layout_name = str(params.get("name") or "").strip()

        if not layout_name:
            return {"success": False, "output": "", "info": "错误：缺少布局名称参数"}

        template_path = handle_entry_path(handle)
        if template_path.suffix.lower() != ".qpt":
            return {
                "success": False,
                "output": "",
                "info": f"错误：模板文件必须是 .qpt 格式: {template_path}",
            }
        if not template_path.is_file():
            return {
                "success": False,
                "output": "",
                "info": f"错误：模板文件不存在: {template_path}",
            }
        if template_path.stat().st_size > 10 << 20:
            return {"success": False, "output": "", "info": "错误：模板文件超过 10 MiB"}

        try:
            xml_text = template_path.read_text(encoding="utf-8-sig")
        except UnicodeDecodeError as exc:
            return {
                "success": False,
                "output": "",
                "info": f"错误：模板文件不是有效的 UTF-8 文本: {exc}",
            }
        except Exception as exc:
            return {
                "success": False,
                "output": "",
                "info": f"读取模板文件失败: {exc}",
            }

        document = QDomDocument()
        try:
            parse_result = document.setContent(xml_text)
        except Exception as exc:
            return {
                "success": False,
                "output": "",
                "info": f"解析模板文件失败: {exc}",
            }
        parse_ok, parse_error = _qdom_parse_result(parse_result)
        if not parse_ok:
            detail = f": {parse_error}" if parse_error else ""
            return {
                "success": False,
                "output": "",
                "info": f"错误：解析模板文件失败{detail}",
            }
        if document.documentElement().tagName() != "Layout":
            return {"success": False, "output": "", "info": "错误：模板根元素必须为 Layout"}
        html_label_ids = _prepare_template_html_labels(document)

        project = QgsProject.instance()
        manager = project.layoutManager()
        existing = manager.layoutByName(layout_name)

        # 模板相对资源路径以 QPT 文件为基准，而不是当前工作目录。
        from qgis.core import QgsPathResolver
        context = QgsReadWriteContext()
        context.setPathResolver(QgsPathResolver(str(template_path)))
        candidate = QgsPrintLayout(project)
        try:
            loaded = candidate.loadFromTemplate(document, context, clearExisting=True)
            if isinstance(loaded, tuple) and len(loaded) == 2 and loaded[1] is False:
                raise ValueError("QGIS 未能加载模板")
            if html_label_ids:
                from qgis.core import QgsLayoutItemLabel
                for temporary_id, original_id in html_label_ids.items():
                    item = candidate.itemById(temporary_id)
                    if not isinstance(item, QgsLayoutItemLabel):
                        raise ValueError("模板 HTML 文字项未能加载")
                    item.setId(original_id)
                    item.setMode(QgsLayoutItemLabel.ModeHtml)
        except Exception as exc:
            return {
                "success": False,
                "output": "",
                "info": f"加载布局模板失败: {exc}",
            }

        if candidate.pageCollection().pageCount() <= 0:
            return {
                "success": False,
                "output": "",
                "info": "错误：模板中没有可用的布局页面",
            }

        warnings, unresolved = _configure_template(candidate, params, project)
        item_ids, rectangles = _template_snapshot(candidate)
        replaced = existing is not None
        candidate.setName(layout_name)
        if existing is not None:
            existing.setName("__template_old_"+uuid4().hex)
        try:
            added = manager.addLayout(candidate)
            if added is False:
                raise ValueError(f"无法将布局“{layout_name}”添加到当前工程")
        except Exception:
            if existing is not None:
                existing.setName(layout_name)
            raise
        if existing is not None:
            manager.removeLayout(existing)

        data = {
            "layout": layout_name,
            "template": str(template_path.resolve()),
            "replaced": replaced,
            "page_count": candidate.pageCollection().pageCount(),
            "item_count": len(item_ids),
            "item_ids": item_ids,
            "rects_mm": rectangles,
            "warnings": warnings,
            "unreplaced_placeholders": unresolved,
        }
        action = "覆盖导入" if replaced else "导入"
        return {
            "success": True,
            "output": layout_name,
            "info": f"布局模板已{action}: {layout_name}",
            "resource": handle_summary(handle),
            "data": data,
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"导入布局模板失败: {str(e)}", "error_type": getattr(e, "code", "layout_template_failed")}


def set_page_size_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layout_name = params.get("name")
        width = params.get("width")
        height = params.get("height")
        units = params.get("units", "mm")
        orientation = params.get("orientation")

        if not layout_name:
            return {"success": False, "output": "", "info": "错误：缺少布局名称参数"}

        project = QgsProject.instance()
        layout = project.layoutManager().layoutByName(layout_name)

        if not layout:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到布局 '{layout_name}'",
            }

        page_collection = layout.pageCollection()
        if page_collection.pageCount() == 0:
            return {"success": False, "output": "", "info": "错误：布局没有页面"}

        page = page_collection.page(0)

        if orientation:
            current_size = page.pageSize()
            current_width = current_size.width()
            current_height = current_size.height()

            if orientation == "landscape":
                width = max(current_width, current_height)
                height = min(current_width, current_height)
            elif orientation == "portrait":
                width = min(current_width, current_height)
                height = max(current_width, current_height)

        if width is None or height is None:
            return {"success": False, "output": "", "info": "错误：缺少宽度或高度参数"}

        unit_map = {
            "mm": QgsUnitTypes.LayoutMillimeters,
            "cm": QgsUnitTypes.LayoutCentimeters,
            "inches": QgsUnitTypes.LayoutInches,
            "pixels": QgsUnitTypes.LayoutPixels,
        }

        unit_type = unit_map.get(units, QgsUnitTypes.LayoutMillimeters)
        page.setPageSize(QgsLayoutSize(width, height, unit_type))

        return {
            "success": True,
            "output": layout_name,
            "info": f"页面大小已设置为 {width}x{height} {units}",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"设置页面大小失败: {str(e)}"}


def _wrap_main_thread(handler):
    def _wrapped(params: Dict[str, Any]) -> Dict[str, Any]:
        return run_in_main_thread(lambda: handler(params))

    _wrapped.__name__ = handler.__name__
    _wrapped.__doc__ = handler.__doc__
    return _wrapped


create_layout_handler = _wrap_main_thread(create_layout_handler)
delete_layout_handler = _wrap_main_thread(delete_layout_handler)
list_layouts_handler = _wrap_main_thread(list_layouts_handler)
duplicate_layout_handler = _wrap_main_thread(duplicate_layout_handler)
rename_layout_handler = _wrap_main_thread(rename_layout_handler)
get_layout_info_handler = _wrap_main_thread(get_layout_info_handler)
import_layout_template_handler = _wrap_main_thread(import_layout_template_handler)
set_page_size_handler = _wrap_main_thread(set_page_size_handler)
