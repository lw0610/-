"""通用图层标注。"""
from typing import (
    Any,
    Dict,
)
from qgis.PyQt.QtGui import QColor
from qgis.core import (
    Qgis,
    QgsPalLayerSettings,
    QgsProject,
    QgsTextFormat,
    QgsVectorLayerSimpleLabeling,
)
from autocarto.tools.system._shared.style_common import _wrap_main_thread


def add_labels_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer_name = params.get("layer")
        field = params.get("field")
        font_size = params.get("font_size", 10)
        color = params.get("color", "#000000")

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层参数"}
        if not field:
            return {"success": False, "output": "", "info": "错误：缺少字段参数"}

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }

        layer = layers[0]
        field_index = layer.fields().indexFromName(field)
        if field_index < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：字段 '{field}' 不存在",
            }

        text_format = QgsTextFormat()
        text_format.setSize(font_size)
        text_format.setColor(QColor(color))

        label_settings = QgsPalLayerSettings()
        label_settings.fieldName = field
        label_settings.setFormat(text_format)
        try:
            label_settings.placement = Qgis.LabelPlacement.OverPoint
        except AttributeError:
            label_settings.placement = QgsPalLayerSettings.OverPoint

        labeling = QgsVectorLayerSimpleLabeling(label_settings)
        layer.setLabeling(labeling)
        layer.setLabelsEnabled(True)
        layer.triggerRepaint()

        return {
            "success": True,
            "output": layer_name,
            "info": f"标注添加完成。字段: {field}, 字体大小: {font_size}",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"添加标注失败: {str(e)}"}


add_labels_handler = _wrap_main_thread(add_labels_handler)
