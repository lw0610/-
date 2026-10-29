"""Label and font handling shared by single and batch QML application."""
from qgis.core import QgsVectorLayerSimpleLabeling

def resolve_name_field(fields, requested):
    """Keep preview and actual QML application on the same name-field fallback."""
    return requested if requested in fields else next(
        (field for field in ("NAME", "name", "name:zh", "名称", "地名") if field in fields), "")

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


