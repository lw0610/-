"""Export a layout's serialized copy without changing its live item IDs."""
from pathlib import Path
import os
import re
import tempfile

from qgis.PyQt.QtXml import QDomDocument
from qgis.core import (
    QgsLayoutItemLabel, QgsLayoutItemMap, QgsPathResolver, QgsPrintLayout,
    QgsProject, QgsReadWriteContext,
)

from autocarto.tools.system._shared.style_common import _wrap_main_thread


def layout_map_items(layout):
    """Expose stable selectors even when a layout item has no user-assigned ID."""
    return [
        {"id": item.id(), "uuid": item.uuid(), "name": item.displayName()}
        for item in sorted(
            (item for item in layout.items() if isinstance(item, QgsLayoutItemMap)),
            key=lambda item: (item.id(), item.uuid()),
        )
    ]


@_wrap_main_thread
def save_layout_template_handler(params):
    temp_path = None
    try:
        layout_name = str(params.get("layout") or "").strip()
        path_text = str(params.get("path") or "").strip()
        if not layout_name or not path_text:
            raise ValueError("请提供版面名称和输出 .qpt 路径")
        layout = QgsProject.instance().layoutManager().layoutByName(layout_name)
        if not isinstance(layout, QgsPrintLayout):
            raise ValueError(f"未找到打印版面：{layout_name}")
        path = Path(path_text).expanduser().absolute()
        if path.suffix.lower() != ".qpt":
            raise ValueError("版式输出文件必须使用 .qpt 扩展名")
        choices = layout_map_items(layout)
        if not choices:
            raise ValueError("版面没有地图项，请先添加地图")
        selector = str(params.get("main_map_item") or "").strip()
        if selector:
            selected = [item for item in choices if selector in (item["id"], item["uuid"])]
        else:
            selected = choices if len(choices) == 1 else []
        if len(selected) != 1:
            return {
                "success": False, "output": "",
                "info": "请通过地图项 ID 或 UUID 明确选择一个主地图",
                "error_type": "main_map_required", "data": {"map_items": choices},
            }
        selected_uuid = selected[0]["uuid"]
        path.parent.mkdir(parents=True, exist_ok=True)
        descriptor, temp_path = tempfile.mkstemp(suffix=".qpt", dir=str(path.parent))
        os.close(descriptor)
        context = QgsReadWriteContext()
        context.setPathResolver(QgsPathResolver(str(path)))
        if not layout.saveAsTemplate(temp_path, context):
            raise ValueError("QGIS 无法保存版式模板")
        document = QDomDocument()
        parsed = document.setContent(Path(temp_path).read_text(encoding="utf-8"))
        if not (parsed[0] if isinstance(parsed, tuple) else parsed):
            raise ValueError("QGIS 导出的版式 XML 无法读取")
        nodes = document.elementsByTagName("LayoutItem")
        elements = [nodes.at(index).toElement() for index in range(nodes.count())]
        identifiers = {element.attribute("id") for element in elements}
        main_element = next((element for element in elements if element.attribute("uuid") == selected_uuid), None)
        if main_element is None:
            raise ValueError("版式副本中没有找到所选地图项")
        # Preserve a unique main_map even if another map or text item already uses it.
        for element in elements:
            if element != main_element and element.attribute("id") == "main_map":
                index = 2
                while f"main_map_{index}" in identifiers:
                    index += 1
                replacement = f"main_map_{index}"
                element.setAttribute("id", replacement)
                identifiers.add(replacement)
        main_element.setAttribute("id", "main_map")
        saved_ids = {element.attribute("uuid"): element.attribute("id") for element in elements}
        placeholders = sorted({
            match.strip()
            for item in layout.items() if isinstance(item, QgsLayoutItemLabel)
            for match in re.findall(r"\{\{([^{}]+)\}\}", item.text()) if match.strip()
        })
        Path(temp_path).write_text(document.toString(), encoding="utf-8")
        os.replace(temp_path, path)
        temp_path = None
        return {
            "success": True, "output": str(path), "info": "版式模板已保存，工程版面保持原样",
            "data": {"path": str(path), "placeholders": placeholders,
                     "map_items": [dict(item, id=saved_ids[item["uuid"]]) for item in choices]},
        }
    except Exception as exc:
        return {"success": False, "output": "", "info": f"保存版式模板失败：{exc}"}
    finally:
        if temp_path is not None:
            Path(temp_path).unlink(missing_ok=True)
