"""Label and font handling shared by single and batch QML application."""
from qgis.core import QgsVectorLayerSimpleLabeling
from qgis.PyQt.QtGui import QFontDatabase, QFont

def resolve_name_field(fields, requested):
    """Keep preview and actual QML application on the same name-field fallback."""
    return requested if requested in fields else next(
        (field for field in ("NAME", "name", "name:zh", "名称", "地名") if field in fields), "")

def select_label_font(requested="auto"):
    """Choose an installed CJK sans family rather than a platform UI alias."""
    database = QFontDatabase()
    families = set(database.families())
    if requested not in ("auto", "", ".AppleSystemUIFont") and requested in families:
        return requested, []
    preferred = ("Microsoft YaHei", "微软雅黑", "PingFang SC", "苹方-简",
                 "Source Han Sans SC", "思源黑体", "Noto Sans CJK SC")
    selected = next((name for name in preferred if name in families), "")
    warnings = []
    if not selected:
        chinese = database.families(QFontDatabase.SimplifiedChinese)
        selected = chinese[0] if chinese else QFont().family()
        warnings.append("没有规范指定的中文黑体，使用 " + selected if chinese else "本机没有可用中文字体，请安装思源黑体或 Noto Sans CJK")
    if requested not in ("auto", "") and requested != selected:
        warnings.append("字体 " + str(requested) + " 已回退为 " + selected)
    return selected, warnings


def restore_qml_font_families(layer):
    """Restore explicit QML fonts, replacing missing/system UI fonts with CJK."""
    labeling = layer.labeling() if hasattr(layer, "labeling") else None
    if labeling is None:
        return []
    warnings = []
    def restore(settings):
        text_format = settings.format()
        font = text_format.font()
        family, notes = select_label_font(font.family())
        warnings.extend(notes)
        font.setFamily(family)
        font.setFamilies([family])
        text_format.setFont(font)
        text_format.setFamilies([family])
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
    return list(dict.fromkeys(warnings))


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


