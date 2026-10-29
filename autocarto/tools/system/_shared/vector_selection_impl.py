import os
import re
from typing import Any, Dict

from qgis.core import QgsExpression, QgsFeature, QgsProject, QgsVectorFileWriter, QgsVectorLayer, QgsWkbTypes

from autocarto.tools.system._shared.canvas_refresh import refresh_canvas
from autocarto.tools.system._shared.output_manager import resolve_output_path, sync_session_context_from_params
from autocarto.tools.system._shared.vector_attributes_impl import _build_output_layer_ref, _publish_target

ALLOWED_OPERATORS = {"=", "<>", ">", "<", ">=", "<=", "LIKE", "ILIKE"}


def _find_vector_layer(layer_name: Any):
    name = str(layer_name or "").strip()
    if not name:
        return None
    project = QgsProject.instance()
    layer = project.mapLayer(name)
    if layer is None:
        layers = project.mapLayersByName(name)
        layer = layers[0] if layers else None
    if not isinstance(layer, QgsVectorLayer):
        return None
    return layer


def _clone_selected_features(layer: QgsVectorLayer, params: Dict[str, Any]) -> QgsVectorLayer:
    output_text = str(params.get("OUTPUT") or "").strip()
    extension = os.path.splitext(output_text)[1].lstrip(".")
    file_output = not output_text.startswith("memory:") and (
        os.path.isabs(output_text) or "/" in output_text or "\\" in output_text
        or bool(QgsVectorFileWriter.driverForExtension(extension))
    )
    layer_name = (
        output_text.split(":", 1)[1].strip()
        if output_text.startswith("memory:")
        else output_text
    )
    if file_output:
        basename = os.path.splitext(os.path.basename(output_text))[0]
        layer_name = basename if re.search(r"[\u3400-\u9fff]", basename) else ""
    if not layer_name or layer_name == "TEMPORARY_OUTPUT":
        layer_name = f"{layer.name()}_提取结果"

    crs_auth = layer.crs().authid() if layer.crs() and layer.crs().isValid() else ""
    uri = QgsWkbTypes.displayString(layer.wkbType())
    if crs_auth:
        uri = f"{uri}?crs={crs_auth}"

    target = QgsVectorLayer(uri, layer_name, "memory")
    if not target.isValid():
        raise RuntimeError("创建内存图层失败")

    provider = target.dataProvider()
    provider.addAttributes(list(layer.fields()))
    target.updateFields()

    features = []
    selected_ids = set(layer.selectedFeatureIds())
    for feature in layer.getFeatures():
        if feature.id() not in selected_ids:
            continue
        cloned = QgsFeature(target.fields())
        cloned.setGeometry(feature.geometry())
        cloned.setAttributes(feature.attributes())
        features.append(cloned)
    if features:
        provider.addFeatures(features)
    if file_output:
        sync_session_context_from_params(params)
        path = resolve_output_path(output_text, "提取结果.gpkg", "output")
        options = QgsVectorFileWriter.SaveVectorOptions()
        options.driverName = QgsVectorFileWriter.driverForExtension(extension) or "GPKG"
        options.fileEncoding = "UTF-8"
        result = QgsVectorFileWriter.writeAsVectorFormatV3(
            target, path, QgsProject.instance().transformContext(), options
        )
        if result[0] != QgsVectorFileWriter.NoError:
            raise RuntimeError(f"保存提取结果失败: {result[1]}")
        target = QgsVectorLayer(result[2] or path, layer_name, "ogr")
        if not target.isValid():
            raise RuntimeError(f"无法读取已保存的提取结果: {path}")
    _publish_target(target, params)
    return target


def _field_preview(layer: QgsVectorLayer, limit: int = 6) -> str:
    field_names = [field.name() for field in layer.fields()]
    if not field_names:
        return "无字段"
    if len(field_names) <= limit:
        return ", ".join(field_names)
    return f"{', '.join(field_names[:limit])} 等 {len(field_names)} 个字段"


def select_by_expression_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer_name = params.get("layer")
        expression = params.get("expression")

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层名称参数"}

        if not expression:
            return {"success": False, "output": "", "info": "错误：缺少表达式参数"}

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }

        layer = layers[0]
        expr = QgsExpression(expression)
        if expr.hasParserError():
            return {
                "success": False,
                "output": "",
                "info": f"表达式错误: {expr.parserErrorString()}",
            }

        layer.selectByExpression(expression)
        selected_count = layer.selectedFeatureCount()

        return {
            "success": True,
            "output": layer_name,
            "info": f"按表达式选择完成，选中 {selected_count} 个要素",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"按表达式选择失败: {str(e)}"}


def select_by_attribute_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer_name = params.get("layer")
        field = params.get("field")
        value = params.get("value")
        operator = str(params.get("operator", "=")).strip().upper()

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层名称参数"}

        if not field:
            return {"success": False, "output": "", "info": "错误：缺少字段名称参数"}

        if operator not in ALLOWED_OPERATORS:
            return {
                "success": False,
                "output": "",
                "info": f"错误：不支持的比较运算符 '{operator}'",
            }

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }

        layer = layers[0]

        if layer.fields().indexFromName(field) < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：字段 '{field}' 不存在",
            }

        if isinstance(value, str):
            escaped = value.replace("'", "''")
            expression = f"\"{field}\" {operator} '{escaped}'"
        else:
            expression = f'"{field}" {operator} {value}'

        layer.selectByExpression(expression)
        selected_count = layer.selectedFeatureCount()

        return {
            "success": True,
            "output": layer_name,
            "info": f"按属性选择完成，选中 {selected_count} 个要素",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"按属性选择失败: {str(e)}"}


def clear_selection_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer_name = params.get("layer")

        if layer_name:
            layers = QgsProject.instance().mapLayersByName(layer_name)
            if not layers:
                return {
                    "success": False,
                    "output": "",
                    "info": f"错误：未找到图层 '{layer_name}'",
                }
            layers[0].removeSelection()
            return {
                "success": True,
                "output": layer_name,
                "info": f"已清除图层 '{layer_name}' 的选择",
            }
        else:
            for layer in QgsProject.instance().mapLayers().values():
                if hasattr(layer, "removeSelection"):
                    layer.removeSelection()
            return {"success": True, "output": "all", "info": "已清除所有图层的选择"}

    except Exception as e:
        return {"success": False, "output": "", "info": f"清除选择失败: {str(e)}"}


def extract_by_expression_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer = _find_vector_layer(params.get("INPUT"))
        expression = params.get("EXPRESSION")

        if layer is None:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{params.get('INPUT')}'",
            }
        if not expression:
            return {"success": False, "output": "", "info": "错误：缺少表达式参数"}

        expr = QgsExpression(expression)
        if expr.hasParserError():
            return {
                "success": False,
                "output": "",
                "info": f"表达式错误: {expr.parserErrorString()}",
            }

        layer.selectByExpression(expression)
        target = _clone_selected_features(layer, params)
        return {
            "success": True,
            "output": _build_output_layer_ref(target),
            "info": f"按表达式提取完成，输出图层 {target.name()}；字段: {_field_preview(target)}",
            "output_layers": [_build_output_layer_ref(target)],
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"按表达式提取失败: {str(e)}"}


def extract_selected_features_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer = _find_vector_layer(params.get("INPUT"))

        if layer is None:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{params.get('INPUT')}'",
            }
        if layer.selectedFeatureCount() <= 0:
            return {"success": False, "output": "", "info": "错误：当前没有选中的要素"}

        target = _clone_selected_features(layer, params)
        return {
            "success": True,
            "output": _build_output_layer_ref(target),
            "info": f"已提取选中要素到图层 {target.name()}；字段: {_field_preview(target)}",
            "output_layers": [_build_output_layer_ref(target)],
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"提取选中要素失败: {str(e)}"}


def extract_by_attribute_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer = _find_vector_layer(params.get("INPUT"))
        field = params.get("FIELD")
        value = params.get("VALUE")
        operator_code = int(params.get("OPERATOR", 0) or 0)

        if layer is None:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{params.get('INPUT')}'",
            }
        if not field:
            return {"success": False, "output": "", "info": "错误：缺少字段参数"}
        if layer.fields().indexFromName(str(field)) < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：字段 '{field}' 不存在",
            }

        operator_map = {
            0: "=",
            1: "<>",
            2: ">",
            3: ">=",
            4: "<",
            5: "<=",
            6: "LIKE",
            7: "ILIKE",
        }
        operator = operator_map.get(operator_code, "=")
        if isinstance(value, str):
            escaped = value.replace("'", "''")
            expression = f"\"{field}\" {operator} '{escaped}'"
        else:
            expression = f'"{field}" {operator} {value}'

        layer.selectByExpression(expression)
        target = _clone_selected_features(layer, params)
        return {
            "success": True,
            "output": _build_output_layer_ref(target),
            "info": f"按属性提取完成，输出图层 {target.name()}；字段: {_field_preview(target)}",
            "output_layers": [_build_output_layer_ref(target)],
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"按属性提取失败: {str(e)}"}
