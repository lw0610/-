"""按资源句柄批量加载 QML 与覆盖显式标注字段。"""

from qgis.core import QgsApplication, QgsMapLayerStyle, QgsProject, QgsVectorLayerSimpleLabeling

from autocarto.tools.system._shared.canvas_refresh import refresh_canvas
from autocarto.tools.system._shared.data_loading_support import DataLoadingError, boolean_parameter
from autocarto.tools.system._shared.style_common import _wrap_main_thread
from autocarto.tools.system._shared.resource_handle import (
    ResourceHandleError, require_handle, handle_entries, handle_summary,
)
from autocarto.tools.system._shared.style_set_support import (
    StyleSetError, matching_style, qml_geometry_type, qml_renderer_type,
)


def restore_qml_font_families(layer):
    """让 QML 的标准 families 列表也恢复到 Qt 实际绘制使用的 QFont。"""
    labeling = layer.labeling() if hasattr(layer, "labeling") else None
    if labeling is None:
        return
    def restore(settings):
        text_format = settings.format()
        requested = text_format.families()
        if requested:
            font = text_format.font()
            font.setFamilies(requested)
            text_format.setFont(font)
            settings.setFormat(text_format)
        return settings
    if hasattr(labeling, "rootRule"):
        def visit(rule):
            settings = rule.settings()
            if settings is not None:
                rule.setSettings(restore(settings))
            for child in rule.children():
                visit(child)
        copied = labeling.clone()
        visit(copied.rootRule())
        layer.setLabeling(copied)
    elif hasattr(labeling, "settings"):
        layer.setLabeling(QgsVectorLayerSimpleLabeling(restore(labeling.settings())))


def _selected_layers(selectors):
    project = QgsProject.instance()
    if selectors is None or selectors == []:
        return list(project.mapLayers().values()), []
    if not isinstance(selectors, list) or any(not isinstance(value, str) or not value.strip() for value in selectors):
        raise StyleSetError("invalid_arguments", "layers 须为图层名称或 ID 组成的数组")
    layers, missing, seen = [], [], set()
    for raw in selectors:
        value = raw.strip()
        layer = project.mapLayer(value)
        matches = [layer] if layer is not None else project.mapLayersByName(value)
        if not matches:
            missing.append(value)
        elif len(matches) > 1:
            raise StyleSetError("layer_ambiguous", "图层名称不唯一，请改用 ID: " + value,
                                {"candidates": [{"layer": item.name(), "layer_id": item.id()} for item in matches]})
        elif matches[0].id() not in seen:
            seen.add(matches[0].id())
            layers.append(matches[0])
    return layers, missing


def _override_label_field(layer, field):
    labeling = layer.labeling()
    if labeling is not None and hasattr(labeling, "rootRule"):
        copied = labeling.clone()
        def visit(rule):
            settings = rule.settings()
            if settings is not None and (settings.fieldName or not rule.children()):
                settings.fieldName = field
                settings.isExpression = False
                rule.setSettings(settings)
            for child in rule.children():
                visit(child)
        visit(copied.rootRule())
        layer.setLabeling(copied)
    else:
        settings = labeling.settings() if labeling is not None and hasattr(labeling, "settings") else QgsVectorLayerSimpleLabeling.defaultSettingsForLayer(layer)
        settings.fieldName = field
        settings.isExpression = False
        layer.setLabeling(QgsVectorLayerSimpleLabeling(settings))
    layer.setLabelsEnabled(True)


def _actual_label_field(layer):
    labeling = layer.labeling() if hasattr(layer, "labeling") else None
    if labeling is None:
        return ""
    fields = set()
    if hasattr(labeling, "rootRule"):
        def visit(rule):
            settings = rule.settings()
            if settings is not None and settings.fieldName:
                fields.add(settings.fieldName)
            for child in rule.children():
                visit(child)
        visit(labeling.rootRule())
    elif hasattr(labeling, "settings"):
        fields.add(labeling.settings().fieldName)
    return next(iter(fields)) if len(fields) == 1 else ""


def apply_style_set_handler(params):
    try:
        handle = require_handle(params.get("style_set"), "style_set")
        entries = handle_entries(handle)
        strict = boolean_parameter(params.get("strict"), False)
        layers, unmatched = _selected_layers(params.get("layers"))
        plans, used = [], set()
        for layer in layers:
            entry = matching_style(entries, layer.name())
            if entry is None:
                unmatched.append(layer.name())
                continue
            geometry = qml_geometry_type(entry["style_path"])
            renderer_type = qml_renderer_type(entry["style_path"])
            if renderer_type and QgsApplication.rendererRegistry().rendererMetadata(renderer_type) is None:
                raise StyleSetError("style_renderer_unavailable", "QML 渲染器不可用: " + renderer_type)
            if geometry is not None and hasattr(layer, "geometryType") and geometry != int(layer.geometryType()):
                raise StyleSetError("style_geometry_mismatch", "样式几何类型与图层不符: " + layer.name())
            field = entry["label_field"]
            if entry["label_mode"] == "field" and (not hasattr(layer, "fields") or layer.fields().indexFromName(field) < 0):
                raise StyleSetError("label_field_missing", f"图层 '{layer.name()}' 缺少标注字段 '{field}'")
            plans.append((layer, entry))
            used.add(entry["layer"])
        unused = [{"layer": entry["layer"], "style_file": str(entry["style_path"])} for entry in entries if entry["layer"] not in used]
        if strict and unmatched:
            raise StyleSetError("style_layers_unmatched", "指定图层存在未匹配项，未修改样式", {"applied": [], "unmatched_layers": unmatched, "unused_styles": unused})
        applied, backups = [], []
        try:
            for layer, entry in plans:
                previous = QgsMapLayerStyle()
                previous.readFromLayer(layer)
                renderer = layer.renderer().clone() if hasattr(layer, "renderer") and layer.renderer() is not None else None
                labeling = layer.labeling().clone() if hasattr(layer, "labeling") and layer.labeling() is not None else None
                enabled = layer.labelsEnabled() if hasattr(layer, "labelsEnabled") else False
                backups.append((layer, previous, renderer, labeling, enabled))
                message, ok = layer.loadNamedStyle(str(entry["style_path"]))
                if not ok:
                    raise StyleSetError("style_load_failed", "QML 应用失败: " + message)
                restore_qml_font_families(layer)
                field = entry["label_field"]
                if entry["label_mode"] == "off" and hasattr(layer, "setLabelsEnabled"):
                    layer.setLabelsEnabled(False)
                    layer.setLabeling(None)
                elif entry["label_mode"] == "field":
                    _override_label_field(layer, field)
                layer.triggerRepaint()
                applied.append({"layer": layer.name(), "layer_id": layer.id(), "style_file": str(entry["style_path"]), "label_field": _actual_label_field(layer)})
        except Exception:
            for layer, previous, renderer, labeling, enabled in reversed(backups):
                previous.writeToLayer(layer)
                if renderer is not None:
                    layer.setRenderer(renderer)
                if hasattr(layer, "setLabeling"):
                    layer.setLabeling(labeling)
                    layer.setLabelsEnabled(enabled)
                layer.triggerRepaint()
            raise
        refresh_canvas()
        return {"success": True, "output": "", "info": f"样式集已应用到 {len(applied)} 个图层",
                "resource": handle_summary(handle), "warnings": handle.get("warnings", []),
                "data": {"applied": applied, "unmatched_layers": unmatched, "unused_styles": unused}}
    except (StyleSetError, DataLoadingError, ResourceHandleError) as exc:
        return {"success": False, "output": "", "info": str(exc), "message": str(exc),
                "error_type": exc.code, "data": exc.details}
    except Exception as exc:
        return {"success": False, "output": "", "info": "应用样式集失败: " + str(exc),
                "error_type": "style_set_failed", "message": str(exc)}


apply_style_set_handler = _wrap_main_thread(apply_style_set_handler)
