import logging
import math
import os
from typing import Any, Dict, List, Optional

from qgis.core import QgsLayoutExporter, QgsLayoutItemMap, QgsProject

from autocarto.tools.system._shared.canvas_refresh import refresh_canvas
from autocarto.tools.system._shared.layout_export_artifacts import ArtifactLayoutExporter
from autocarto.tools.system._shared import output_manager

logger = logging.getLogger(__name__)


def _get_layout(layout_name: str):
    project = QgsProject.instance()
    return project.layoutManager().layoutByName(layout_name)


def _is_valid_extent(extent) -> bool:
    return (
        extent is not None
        and not extent.isEmpty()
        and extent.width() > 0
        and extent.height() > 0
    )


def _validate_layout_for_export(layout) -> Optional[str]:
    page_collection = layout.pageCollection()
    if page_collection.pageCount() == 0:
        return "布局没有页面，无法导出"

    page = page_collection.page(0)
    size = page.pageSize()
    if size.width() <= 0 or size.height() <= 0:
        return "页面尺寸无效，无法导出"

    map_items: List[QgsLayoutItemMap] = [
        item for item in layout.items() if isinstance(item, QgsLayoutItemMap)
    ]
    if not map_items:
        return "布局中没有地图框，无法导出"

    for map_item in map_items:
        extent = map_item.extent()
        if not _is_valid_extent(extent):
            map_id = map_item.id() or "<unnamed>"
            return f"地图框 '{map_id}' 范围无效，请先设置有效范围"

    return None


def _resolve_export_path(
    user_path: Optional[str], default_filename: str, extension: str
) -> str:
    output_path = output_manager.resolve_output_path(
        user_path, default_filename, "exports"
    )
    base, ext = os.path.splitext(output_path)
    if not ext:
        output_path = base + extension
    return output_path


def _export_result_message(result: int) -> str:
    result_map = {
        getattr(QgsLayoutExporter, "Success", None): "成功",
        getattr(QgsLayoutExporter, "MemoryError", None): "内存不足",
        getattr(QgsLayoutExporter, "FileError", None): "文件写入失败",
        getattr(QgsLayoutExporter, "PrintError", None): "打印导出失败",
        getattr(QgsLayoutExporter, "SvgLayerError", None): "SVG 图层导出失败",
        getattr(QgsLayoutExporter, "IteratorError", None): "布局迭代失败",
    }
    return result_map.get(result, f"未知错误码: {result}")


def _image_export_dpi(layout, params):
    from qgis.core import QgsUnitTypes
    purpose = str(params.get("purpose") or "final").lower()
    if purpose not in ("final", "review"):
        raise ValueError("导出用途必须是 final 或 review")
    sizes = []
    pages = layout.pageCollection()
    for index in range(pages.pageCount()):
        page = pages.page(index)
        rect = page.rect()
        width = layout.convertFromLayoutUnits(float(rect.width()), QgsUnitTypes.LayoutMillimeters).length()
        height = layout.convertFromLayoutUnits(float(rect.height()), QgsUnitTypes.LayoutMillimeters).length()
        if not math.isfinite(width) or not math.isfinite(height) or width <= 0 or height <= 0:
            raise ValueError("页面尺寸必须是有限正数")
        sizes.append((width, height))
    if not sizes:
        raise ValueError("布局没有可导出的页面")
    raw = params.get("DPI") if params.get("DPI") is not None else params.get("dpi")
    if raw is not None:
        if isinstance(raw, bool):
            raise ValueError("导出 DPI 必须是有限正数")
        dpi = float(raw)
    elif purpose == "review":
        first = sorted(sizes[0])
        dpi = 100 if max(abs(first[0]-841), abs(first[1]-1189)) <= 1 else 140 if max(abs(first[0]-594), abs(first[1]-841)) <= 1 else 200
    else:
        dpi = float(layout.renderContext().dpi())
    if not math.isfinite(dpi) or dpi <= 0:
        raise ValueError("导出 DPI 必须是有限正数")
    if purpose == "review":
        def within_budget(value):
            return all(math.ceil(width*value/25.4)*math.ceil(height*value/25.4) <= 16_000_000 for width, height in sizes)
        if not within_budget(dpi):
            low, high = 0.0, dpi
            for _ in range(50):
                middle = (low+high)/2
                if within_budget(middle):
                    low = middle
                else:
                    high = middle
            dpi = math.floor(low*1000)/1000
            if dpi <= 0:
                raise ValueError("页面过大，无法在像素预算内导出")
    return dpi, purpose


def export_print_layout_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        output_manager.sync_session_context_from_params(params)
        layout_name = params.get("LAYOUT") or params.get("layout") or params.get("name")
        output = params.get("OUTPUT") or params.get("output")
        dpi = params.get("DPI") or params.get("dpi")

        if not layout_name:
            return {
                "success": False,
                "output": "",
                "info": "错误：缺少布局名称参数 (LAYOUT)",
            }

        layout = _get_layout(layout_name)
        if not layout:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到布局 '{layout_name}'",
            }

        validation_error = _validate_layout_for_export(layout)
        if validation_error:
            return {
                "success": False,
                "output": "",
                "info": f"导出前校验失败：{validation_error}",
            }

        output_path = _resolve_export_path(output, f"{layout_name}.png", ".png")
        exporter = ArtifactLayoutExporter(layout)
        settings = QgsLayoutExporter.ImageExportSettings()
        if dpi is not None:
            settings.dpi = float(dpi)

        result = exporter.exportToImage(output_path, settings)
        if result != QgsLayoutExporter.Success:
            error_msg = _export_result_message(result)
            return {
                "success": False,
                "output": "",
                "info": f"布局导出图片失败：{error_msg}",
            }

        paths = exporter.artifact_paths(output_path)
        refresh_canvas()
        return {
            "success": True,
            "output": paths[0],
            "artifact_paths": paths,
            "info": f"布局 '{layout_name}' 导出图片成功",
        }
    except Exception as e:
        logger.exception("导出布局图片失败")
        return {"success": False, "output": "", "info": f"导出布局图片失败: {e}"}


def export_layout_pdf_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        output_manager.sync_session_context_from_params(params)
        layout_name = params.get("LAYOUT") or params.get("layout") or params.get("name")
        output = params.get("OUTPUT") or params.get("output")
        dpi = params.get("DPI") or params.get("dpi")

        if not layout_name:
            return {
                "success": False,
                "output": "",
                "info": "错误：缺少布局名称参数 (LAYOUT)",
            }

        layout = _get_layout(layout_name)
        if not layout:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到布局 '{layout_name}'",
            }

        validation_error = _validate_layout_for_export(layout)
        if validation_error:
            return {
                "success": False,
                "output": "",
                "info": f"导出前校验失败：{validation_error}",
            }

        output_path = _resolve_export_path(output, f"{layout_name}.pdf", ".pdf")
        exporter = ArtifactLayoutExporter(layout)
        settings = QgsLayoutExporter.PdfExportSettings()
        if dpi is not None:
            settings.dpi = float(dpi)

        result = exporter.exportToPdf(output_path, settings)
        if result != QgsLayoutExporter.Success:
            error_msg = _export_result_message(result)
            return {
                "success": False,
                "output": "",
                "info": f"布局导出 PDF 失败：{error_msg}",
            }

        paths = exporter.artifact_paths(output_path)
        refresh_canvas()
        return {
            "success": True,
            "output": paths[0],
            "artifact_paths": paths,
            "info": f"布局 '{layout_name}' 导出 PDF 成功",
        }
    except Exception as e:
        logger.exception("导出布局 PDF 失败")
        return {"success": False, "output": "", "info": f"导出布局 PDF 失败: {e}"}
