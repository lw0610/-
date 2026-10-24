"""样式条目匹配、QML 工具检查与兼容预设索引。"""

import json
import ntpath
from pathlib import Path
import xml.etree.ElementTree as ET

from autocarto.tools.system._shared.tool_assets import tool_asset_path


class StyleSetError(ValueError):
    def __init__(self, code, message, details=None):
        super().__init__(message)
        self.code = code
        self.details = details or {}


def preset_catalog():
    path = tool_asset_path("cartography", "basemap-legacy-presets.json")
    value = json.loads(path.read_text(encoding="utf-8"))
    if value.get("schema_version") != "basemap-presets/v1" or not isinstance(value.get("entries"), list):
        raise StyleSetError("style_set_invalid", "内置预设索引格式无效")
    return value


def legacy_preset(kind, value):
    normalized = str(value or "").strip().lower()
    for entry in preset_catalog()["entries"]:
        values = entry.get("legacy_aliases") or [entry["legacy_type"]]
        if entry["legacy_handler"] == kind and normalized in [str(item).lower() for item in values]:
            return entry
    return None


def style_file_path(root, raw):
    if not isinstance(raw, str) or not raw.strip() or ntpath.isabs(raw):
        raise StyleSetError("style_set_invalid", "样式文件须为样式集目录中的相对 QML 路径")
    root = Path(root).resolve()
    path = (root / raw.replace("\\", "/")).resolve()
    if not path.is_relative_to(root) or path.suffix.lower() != ".qml":
        raise StyleSetError("style_set_invalid", "样式文件须为样式集目录中的相对 QML 路径")
    return path


def matching_style(entries, layer_name):
    exact = [entry for entry in entries if entry["layer"] == layer_name]
    matches = exact or [entry for entry in entries if layer_name in entry["aliases"]]
    if len(matches) > 1:
        raise StyleSetError("style_set_ambiguous", "图层别名同时匹配多个样式: " + layer_name)
    return matches[0] if matches else None


def qml_geometry_type(path):
    if not Path(path).is_file():
        raise StyleSetError("style_file_missing", "样式文件不存在: " + str(path))
    try:
        root = ET.parse(path).getroot()
        if root.tag != "qgis":
            raise ValueError("根节点不是 qgis")
        raw = root.findtext("layerGeometryType")
        return int(raw) if raw is not None else None
    except (OSError, ET.ParseError, ValueError) as exc:
        raise StyleSetError("style_file_invalid", "无法读取 QML 样式: " + str(exc)) from exc


def qml_renderer_type(path):
    root = ET.parse(path).getroot()
    renderer = root.find("renderer-v2")
    return renderer.get("type") if renderer is not None else None
