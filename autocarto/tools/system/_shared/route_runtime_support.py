"""路线工具共同的资源、运行环境和回执机制。"""
import json
from pathlib import Path

from autocarto.core.paths import get_plugin_root, resolve_project_path
from autocarto.knowledge.visual_presets import renderer_defaults


def dataset_failure(exc):
    payload = exc.to_dict()
    return {"success": False, "output": "", "info": str(exc), "message": str(exc),
            "error_type": exc.code, "error": json.dumps(payload, ensure_ascii=False),
            "errors": [{"stage": "dataset_handoff", "message": str(exc), "error_type": exc.code,
                        "detail": "", "debug": exc.details}],
            "failed_stages": ["dataset_handoff"], "data": {"dataset": payload}}


def renderer_directory(params):
    supplied = params.get("renderer_plugin_dir") or params.get("plugin_dir")
    if supplied:
        path = resolve_project_path(str(supplied))
    else:
        from qgis.core import QgsApplication
        from qgis.utils import plugin_paths
        from autocarto.core.config_manager import ConfigProxy
        config = ConfigProxy(get_plugin_root() / "config")
        name = str(config.get("route_renderer.plugin_name") or renderer_defaults()["renderer_plugin_name"])
        if Path(name).name != name:
            raise ValueError("route_renderer.plugin_name 须为插件目录名")
        path = Path(QgsApplication.qgisSettingsDirPath()) / "python/plugins" / name
        for directory in [path.parent, *map(Path, plugin_paths)]:
            candidate = directory / name
            if (candidate / "services/combined_plot_service.py").is_file():
                path = candidate
                break
    if not (path / "services/combined_plot_service.py").is_file():
        raise ValueError("未找到路线渲染服务: " + str(path))
    return str(path)


def label_format(kind):
    from qgis.PyQt.QtGui import QColor, QFont
    from qgis.core import QgsTextBufferSettings, QgsTextFormat, QgsUnitTypes
    cfg = renderer_defaults()["labels"][kind]
    font = QFont()
    font.setFamilies(cfg["font_families"])
    result = QgsTextFormat()
    result.setFont(font)
    result.setSize(cfg["size_mm"])
    result.setSizeUnit(QgsUnitTypes.RenderMillimeters)
    result.setColor(QColor(cfg["color"]))
    buffer = QgsTextBufferSettings()
    buffer.setEnabled(True)
    buffer.setSize(cfg["buffer_mm"])
    buffer.setSizeUnit(QgsUnitTypes.RenderMillimeters)
    buffer.setColor(QColor(cfg["buffer_color"]))
    result.setBuffer(buffer)
    return result
