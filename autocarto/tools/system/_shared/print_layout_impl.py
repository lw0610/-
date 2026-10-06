from pathlib import Path
from typing import Any, Dict, Tuple

from qgis.PyQt.QtXml import QDomDocument
from qgis.core import (
    QgsLayoutSize,
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
                }
            )

        if not layout_list:
            return {"success": True, "output": "无布局", "info": "项目中没有打印布局"}

        output_lines = [f"共 {len(layout_list)} 个布局:"]
        for layout_info in layout_list:
            output_lines.append(
                f"  - {layout_info['name']}: {layout_info['page_count']}页, {layout_info['page_size']}"
            )

        return {
            "success": True,
            "output": "\n".join(output_lines),
            "info": f"找到 {len(layout_list)} 个布局",
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

        return {
            "success": True,
            "output": "\n".join(info_lines),
            "info": "布局信息获取成功",
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


def _layout_item_ids(layout) -> list:
    item_ids = []
    for item in layout.items():
        item_id = ""
        try:
            item_id = str(item.id() or "").strip()
        except Exception:
            item_id = ""
        if item_id:
            item_ids.append(item_id)
    return sorted(set(item_ids))


def import_layout_template_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    """Import a QGIS ``.qpt`` template into a named project layout."""

    try:
        template_value = str(params.get("template") or "").strip()
        layout_name = str(params.get("name") or "").strip()

        if not template_value:
            return {"success": False, "output": "", "info": "错误：缺少模板文件路径参数"}
        if not layout_name:
            return {"success": False, "output": "", "info": "错误：缺少布局名称参数"}

        template_path = Path(template_value).expanduser()
        if not template_path.is_absolute():
            return {
                "success": False,
                "output": "",
                "info": "错误：模板文件必须使用绝对路径",
            }
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

        project = QgsProject.instance()
        manager = project.layoutManager()
        existing = manager.layoutByName(layout_name)

        # Load into an unregistered candidate first. This validates the template
        # before an existing layout is cleared and replaced.
        candidate = QgsPrintLayout(project)
        candidate.initializeDefaults()
        try:
            candidate.loadFromTemplate(
                document, QgsReadWriteContext(), clearExisting=True
            )
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

        replaced = existing is not None
        if existing is not None:
            try:
                existing.loadFromTemplate(
                    document, QgsReadWriteContext(), clearExisting=True
                )
                existing.setName(layout_name)
            except Exception as exc:
                return {
                    "success": False,
                    "output": "",
                    "info": f"覆盖现有布局失败: {exc}",
                }
            layout = existing
        else:
            candidate.setName(layout_name)
            added = manager.addLayout(candidate)
            if added is False:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：无法将布局 '{layout_name}' 添加到当前工程",
                }
            layout = candidate

        item_ids = _layout_item_ids(layout)
        data = {
            "layout": layout_name,
            "template": str(template_path.resolve()),
            "replaced": replaced,
            "page_count": layout.pageCollection().pageCount(),
            "item_count": len(layout.items()),
            "item_ids": item_ids,
        }
        action = "覆盖导入" if replaced else "导入"
        return {
            "success": True,
            "output": layout_name,
            "info": f"布局模板已{action}: {layout_name}",
            "data": data,
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"导入布局模板失败: {str(e)}"}


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
