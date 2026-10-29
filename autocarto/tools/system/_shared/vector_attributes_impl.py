import json
import re
from autocarto.utils.redaction import redact_sensitive_text
from typing import Any, Dict, Optional

import processing
from qgis.PyQt.QtCore import QVariant
from qgis.core import (
    QgsExpression,
    QgsExpressionContext,
    QgsExpressionContextUtils,
    QgsFeature,
    QgsField,
    QgsProcessingContext,
    QgsProcessingFeedback,
    QgsProject,
    QgsVectorLayer,
    QgsWkbTypes,
)

from autocarto.tools.system._shared.canvas_refresh import refresh_canvas
from autocarto.tools.system._shared.project_layers import resolve_project_layer

FIELD_TYPE_MAP = {
    0: QVariant.Int,
    1: QVariant.Double,
    2: QVariant.String,
    3: QVariant.Date,
    4: QVariant.Time,
    5: QVariant.DateTime,
    6: QVariant.Bool,
}


def _find_vector_layer(layer_ref: Any) -> QgsVectorLayer | None:
    layer = resolve_project_layer(layer_ref)
    if not isinstance(layer, QgsVectorLayer):
        return None
    return layer


def _resolve_target_layer(source: QgsVectorLayer, output_ref: Any) -> QgsVectorLayer:
    output_text = str(output_ref or "").strip()
    if not output_text or output_text == "memory:":
        if source.isEditable():
            raise RuntimeError("图层已有未提交的编辑，请先完成编辑或指定新的输出图层")
        return source

    layer_name = output_text
    if output_text.startswith("memory:"):
        layer_name = output_text.split(":", 1)[1].strip() or source.name()

    crs_auth = source.crs().authid() if source.crs().isValid() else ""
    uri = QgsWkbTypes.displayString(source.wkbType())
    if crs_auth:
        uri = f"{uri}?crs={crs_auth}"

    target = QgsVectorLayer(uri, layer_name, "memory")
    if not target.isValid():
        raise RuntimeError("创建内存图层失败")

    provider = target.dataProvider()
    if not provider.addAttributes(list(source.fields())):
        raise RuntimeError("复制输出字段失败")
    target.updateFields()

    features = []
    for feature in source.getFeatures():
        cloned = QgsFeature(target.fields())
        cloned.setGeometry(feature.geometry())
        cloned.setAttributes(feature.attributes())
        features.append(cloned)
    if features and not provider.addFeatures(features)[0]:
        raise RuntimeError("复制输出要素失败")
    return target


def _publish_target(target: QgsVectorLayer, params: Dict[str, Any]) -> None:
    """Publish only completed results, replacing this step's own named output."""
    project = QgsProject.instance()
    if project.mapLayer(target.id()) is not None:
        refresh_canvas()
        return
    context = params.get("_autocarto_output_owner") or {}
    session_id = str(context.get("session_id") or "")
    owner = ""
    if session_id:
        owner = json.dumps([session_id, str(context.get("flow_run_id") or ""),
                            str(context.get("step_id") or "")], ensure_ascii=False)
    previous = []
    if owner:
        previous = [layer.id() for layer in project.mapLayersByName(target.name())
                    if layer.customProperty("autocarto/output_owner", "") == owner]
        target.setCustomProperty("autocarto/output_owner", owner)
    if project.addMapLayer(target) is None:
        raise RuntimeError("无法将输出图层加入当前工程")
    # A matching display name alone never authorizes removing a user layer.
    if previous:
        project.removeMapLayers(previous)
    refresh_canvas()


def _rollback_target(target: Optional[QgsVectorLayer]) -> None:
    if target is not None and target.isEditable():
        target.rollBack()


def _ensure_field(layer: QgsVectorLayer, params: Dict[str, Any]) -> int:
    field_name = str(params.get("FIELD_NAME") or "").strip()
    if not field_name:
        raise RuntimeError("缺少字段名称参数")

    field_index = layer.fields().indexFromName(field_name)
    if field_index >= 0:
        return field_index

    field_type = int(params.get("FIELD_TYPE", 0) or 0)
    field_length = int(params.get("FIELD_LENGTH", 10) or 10)
    field_precision = int(params.get("FIELD_PRECISION", 0) or 0)
    qvariant_type = FIELD_TYPE_MAP.get(field_type, QVariant.String)

    if not layer.isEditable() and not layer.startEditing():
        raise RuntimeError("图层不可编辑，无法添加字段")

    if not layer.addAttribute(QgsField(field_name, qvariant_type, len=field_length, prec=field_precision)):
        raise RuntimeError("添加字段失败")
    layer.updateFields()

    field_index = layer.fields().indexFromName(field_name)
    if field_index < 0:
        raise RuntimeError("字段创建后未找到")
    return field_index


def _build_output_layer_ref(layer: QgsVectorLayer) -> Dict[str, Any]:
    return {
        "layer_id": layer.id(),
        "name": layer.name(),
        "source": redact_sensitive_text(layer.source()).strip(),
        "feature_count": int(layer.featureCount()),
        "geometry_type": QgsWkbTypes.displayString(layer.wkbType()),
        "fields": [field.name() for field in layer.fields()],
        "provider": layer.providerType(),
        "reference_type": "project_layer",
        "auto_load": False,
    }


def _field_preview(layer: QgsVectorLayer, limit: int = 6) -> str:
    names = [field.name() for field in layer.fields()]
    if not names:
        return "无字段"
    if len(names) <= limit:
        return ", ".join(names)
    return f"{', '.join(names[:limit])} 等 {len(names)} 个字段"


def _coerce_float_value(value: Any) -> Optional[float]:
    if value is None:
        return None
    if isinstance(value, (int, float)):
        return float(value)

    text = str(value).strip()
    if not text:
        return None
    text = text.replace(",", "")
    return float(text)


def _clone_fields_with_float_override(
    source: QgsVectorLayer, field_name: str
) -> list[QgsField]:
    cloned_fields: list[QgsField] = []
    for field in source.fields():
        cloned = QgsField(field)
        if field.name() == field_name:
            cloned.setType(QVariant.Double)
            cloned.setTypeName("double")
            cloned.setLength(max(int(field.length() or 0), 20))
            cloned.setPrecision(max(int(field.precision() or 0), 6))
        cloned_fields.append(cloned)
    return cloned_fields


def _build_typed_memory_layer(
    source: QgsVectorLayer, output_ref: Any, fields: list[QgsField], fallback_name: str
) -> QgsVectorLayer:
    output_text = str(output_ref or "").strip()
    layer_name = fallback_name
    if output_text.startswith("memory:"):
        layer_name = output_text.split(":", 1)[1].strip() or fallback_name
    elif output_text:
        layer_name = output_text

    crs_auth = source.crs().authid() if source.crs().isValid() else ""
    uri = QgsWkbTypes.displayString(source.wkbType())
    if crs_auth:
        uri = f"{uri}?crs={crs_auth}"

    target = QgsVectorLayer(uri, layer_name, "memory")
    if not target.isValid():
        raise RuntimeError("创建内存图层失败")

    provider = target.dataProvider()
    if fields and not provider.addAttributes(fields):
        raise RuntimeError("创建输出字段失败")
    target.updateFields()
    return target


def field_calculator_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    target = None
    try:
        input_ref = params.get("INPUT")
        formula = str(params.get("FORMULA") or "").strip()
        if not input_ref:
            return {"success": False, "output": "", "info": "错误：缺少输入图层参数"}
        if not formula:
            return {"success": False, "output": "", "info": "错误：缺少公式参数"}

        source = _find_vector_layer(input_ref)
        if source is None:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{input_ref}'",
            }

        expression = QgsExpression(formula)
        if expression.hasParserError():
            return {
                "success": False,
                "output": "",
                "info": f"表达式错误: {expression.parserErrorString()}",
            }

        target = _resolve_target_layer(source, params.get("OUTPUT"))
        field_index = _ensure_field(target, params)

        if not target.isEditable() and not target.startEditing():
            raise RuntimeError("图层不可编辑，无法计算字段")

        context = QgsExpressionContext()
        context.appendScopes(QgsExpressionContextUtils.globalProjectLayerScopes(target))

        for feature in target.getFeatures():
            context.setFeature(feature)
            value = expression.evaluate(context)
            if expression.hasEvalError():
                raise RuntimeError(expression.evalErrorString())
            if not target.changeAttributeValue(feature.id(), field_index, value):
                raise RuntimeError(f"更新要素 {feature.id()} 字段失败")

        if target.isEditable() and not target.commitChanges():
            errors = "; ".join(target.commitErrors())
            raise RuntimeError(errors or "提交字段计算结果失败")

        _publish_target(target, params)
        return {
            "success": True,
            "output": _build_output_layer_ref(target),
            "info": f"字段计算完成: {target.name()}；字段: {_field_preview(target)}",
            "output_layers": [_build_output_layer_ref(target)],
        }
    except Exception as exc:
        _rollback_target(target)
        return {"success": False, "output": "", "info": f"字段计算失败: {str(exc)}"}


def text_to_float_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        source = _find_vector_layer(params.get("INPUT"))
        if source is None:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{params.get('INPUT')}'",
            }

        field_name = str(params.get("FIELD") or "").strip()
        if not field_name:
            return {"success": False, "output": "", "info": "错误：缺少字段参数"}

        field_index = source.fields().indexFromName(field_name)
        if field_index < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：字段 '{field_name}' 不存在",
            }

        target_name = str(params.get("OUTPUT") or "").strip()
        if not target_name:
            target_name = f"memory:{source.name()}_float"
        elif not target_name.startswith("memory:"):
            target_name = f"memory:{target_name}"

        target = _build_typed_memory_layer(
            source,
            target_name,
            _clone_fields_with_float_override(source, field_name),
            source.name(),
        )

        target_fields = target.fields()
        features = []
        for feature in source.getFeatures():
            cloned = QgsFeature(target_fields)
            cloned.setGeometry(feature.geometry())
            attrs = list(feature.attributes())
            try:
                attrs[field_index] = _coerce_float_value(attrs[field_index])
            except Exception as exc:
                raise RuntimeError(
                    f"字段 '{field_name}' 在要素 {feature.id()} 中无法转换为浮点数: {exc}"
                )
            cloned.setAttributes(attrs)
            features.append(cloned)

        if features and not target.dataProvider().addFeatures(features)[0]:
            raise RuntimeError("写入转换结果失败")

        _publish_target(target, params)
        return {
            "success": True,
            "output": _build_output_layer_ref(target),
            "info": f"文本字段转浮点完成: {target.name()}；字段 '{field_name}' 已转换；字段: {_field_preview(target)}",
            "output_layers": [_build_output_layer_ref(target)],
        }
    except Exception as exc:
        return {"success": False, "output": "", "info": f"文本转浮点失败: {str(exc)}"}


def join_attributes_by_field_value_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    target = None
    try:
        source = _find_vector_layer(params.get("INPUT"))
        join_layer = _find_vector_layer(params.get("INPUT_2"))
        if source is None:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{params.get('INPUT')}'",
            }
        if join_layer is None:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{params.get('INPUT_2')}'",
            }

        source_field = str(params.get("FIELD") or "").strip()
        join_field = str(params.get("FIELD_2") or "").strip()
        if not source_field or not join_field:
            return {"success": False, "output": "", "info": "错误：缺少连接字段参数"}
        if source.fields().indexFromName(source_field) < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：字段 '{source_field}' 不存在",
            }
        if join_layer.fields().indexFromName(join_field) < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：字段 '{join_field}' 不存在",
            }

        target_name = str(params.get("OUTPUT") or "").strip()
        if not target_name:
            target_name = f"memory:{source.name()}_joined"
        elif not target_name.startswith("memory:"):
            target_name = f"memory:{target_name}"
        target = _resolve_target_layer(source, target_name)
        if not target.isEditable() and not target.startEditing():
            raise RuntimeError("图层不可编辑，无法执行字段连接")

        join_lookup = {}
        for feature in join_layer.getFeatures():
            join_lookup.setdefault(feature[join_field], feature)

        added_fields = []
        field_name_map = {}
        for field in join_layer.fields():
            original_name = field.name()
            if original_name == join_field:
                continue
            final_name = original_name
            while target.fields().indexFromName(final_name) >= 0 or any(
                existing.name() == final_name for existing in added_fields
            ):
                final_name = f"{final_name}_2"
            cloned = QgsField(field)
            cloned.setName(final_name)
            added_fields.append(cloned)
            field_name_map[original_name] = final_name
        for field in added_fields:
            if not target.addAttribute(field):
                raise RuntimeError("新增连接字段失败")
        target.updateFields()

        matched_count = 0
        for feature in target.getFeatures():
            joined_feature = join_lookup.get(feature[source_field])
            if joined_feature is None:
                continue
            matched_count += 1
            for original_name, final_name in field_name_map.items():
                target_idx = target.fields().indexFromName(final_name)
                if target_idx < 0:
                    continue
                if not target.changeAttributeValue(
                    feature.id(), target_idx, joined_feature[original_name]
                ):
                    raise RuntimeError(f"更新要素 {feature.id()} 连接字段失败")

        if target.isEditable() and not target.commitChanges():
            errors = "; ".join(target.commitErrors())
            raise RuntimeError(errors or "提交字段连接结果失败")

        _publish_target(target, params)
        return {
            "success": True,
            "output": _build_output_layer_ref(target),
            "info": f"按字段连接完成: {target.name()}；匹配 {matched_count} 个要素；字段: {_field_preview(target)}",
            "output_layers": [_build_output_layer_ref(target)],
        }
    except Exception as exc:
        _rollback_target(target)
        return {"success": False, "output": "", "info": f"按字段连接失败: {str(exc)}"}


def rename_table_field_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    target = None
    try:
        source = _find_vector_layer(params.get("INPUT"))
        if source is None:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{params.get('INPUT')}'",
            }

        field_name = str(params.get("FIELD") or "").strip()
        new_name = str(params.get("NEW_NAME") or "").strip()
        if not field_name or not new_name:
            return {"success": False, "output": "", "info": "错误：缺少字段重命名参数"}

        field_index = source.fields().indexFromName(field_name)
        if field_index < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：字段 '{field_name}' 不存在",
            }
        if source.fields().indexFromName(new_name) >= 0 and new_name != field_name:
            return {
                "success": False,
                "output": "",
                "info": f"错误：字段 '{new_name}' 已存在",
            }

        target = _resolve_target_layer(source, params.get("OUTPUT"))
        if not target.isEditable() and not target.startEditing():
            raise RuntimeError("图层不可编辑，无法重命名字段")
        if not target.renameAttribute(field_index, new_name):
            raise RuntimeError("重命名字段失败")
        target.updateFields()
        if target.isEditable() and not target.commitChanges():
            errors = "; ".join(target.commitErrors())
            raise RuntimeError(errors or "提交字段重命名失败")

        _publish_target(target, params)
        return {
            "success": True,
            "output": _build_output_layer_ref(target),
            "info": f"字段 '{field_name}' 已重命名为 '{new_name}'；字段: {_field_preview(target)}",
            "output_layers": [_build_output_layer_ref(target)],
        }
    except Exception as exc:
        _rollback_target(target)
        return {"success": False, "output": "", "info": f"重命名字段失败: {str(exc)}"}


def retain_fields_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    target = None
    try:
        source = _find_vector_layer(params.get("INPUT"))
        if source is None:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{params.get('INPUT')}'",
            }

        raw_fields = params.get("FIELDS")
        if isinstance(raw_fields, str):
            keep_fields = [
                item.strip() for item in re.split(r"[,;]", raw_fields) if item.strip()
            ]
        else:
            keep_fields = [
                str(item).strip() for item in (raw_fields or []) if str(item).strip()
            ]
        if not keep_fields:
            return {
                "success": False,
                "output": "",
                "info": "错误：缺少要保留的字段列表",
            }

        existing_fields = [field.name() for field in source.fields()]
        missing = [field for field in keep_fields if field not in existing_fields]
        if missing:
            return {
                "success": False,
                "output": "",
                "info": f"错误：字段不存在 {', '.join(missing)}",
            }

        target = _resolve_target_layer(source, params.get("OUTPUT"))
        delete_indexes = [
            idx
            for idx, field in enumerate(target.fields())
            if field.name() not in set(keep_fields)
        ]
        if delete_indexes:
            if not target.isEditable() and not target.startEditing():
                raise RuntimeError("图层不可编辑，无法删除字段")
            if not target.deleteAttributes(delete_indexes):
                raise RuntimeError("删除字段失败")
            target.updateFields()
            if target.isEditable() and not target.commitChanges():
                errors = "; ".join(target.commitErrors())
                raise RuntimeError(errors or "提交字段保留结果失败")

        _publish_target(target, params)
        return {
            "success": True,
            "output": _build_output_layer_ref(target),
            "info": f"保留字段完成: {target.name()}；字段: {_field_preview(target)}",
            "output_layers": [_build_output_layer_ref(target)],
        }
    except Exception as exc:
        _rollback_target(target)
        return {"success": False, "output": "", "info": f"保留字段失败: {str(exc)}"}


def _normalize_fields_mapping(raw_mapping: Any) -> list[dict[str, Any]]:
    mapping = raw_mapping
    if isinstance(mapping, str):
        mapping = json.loads(mapping)
    if not isinstance(mapping, list):
        raise RuntimeError("FIELDS_MAPPING 必须是字段映射列表")

    normalized = []
    for item in mapping:
        if not isinstance(item, dict):
            raise RuntimeError("FIELDS_MAPPING 中每一项都必须是对象")
        row = {str(key).lower(): value for key, value in item.items()}
        name = str(row.get("name") or row.get("field_name") or "").strip()
        expression = str(
            row.get("expression")
            or row.get("formula")
            or row.get("source_expression")
            or ""
        ).strip()
        if not name:
            raise RuntimeError("FIELDS_MAPPING 缺少 name")
        if not expression:
            expression = f'"{name}"'
        normalized.append(
            {
                "name": name,
                "type": int(row.get("type", 2) or 2),
                "length": int(row.get("length", 0) or 0),
                "precision": int(row.get("precision", 0) or 0),
                "expression": expression,
            }
        )
    return normalized


def refactor_fields_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        source = _find_vector_layer(params.get("INPUT"))
        if source is None:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{params.get('INPUT')}'",
            }

        fields_mapping = _normalize_fields_mapping(params.get("FIELDS_MAPPING"))
        output_ref = str(params.get("OUTPUT") or "").strip()
        if not output_ref:
            output_ref = f"memory:{source.name()}_refactored"
        elif not output_ref.startswith("memory:"):
            output_ref = f"memory:{output_ref}"

        result = processing.run(
            "native:refactorfields",
            {"INPUT": source, "FIELDS_MAPPING": fields_mapping, "OUTPUT": output_ref},
            context=QgsProcessingContext(),
            feedback=QgsProcessingFeedback(),
        )
        output_layer = result.get("OUTPUT")
        if isinstance(output_layer, QgsVectorLayer):
            target = output_layer
        else:
            target_name = (
                output_ref.split(":", 1)[1].strip()
                if output_ref.startswith("memory:")
                else output_ref
            )
            target = _find_vector_layer(target_name)
            if target is None:
                raise RuntimeError("重构字段结果图层不可用")

        _publish_target(target, params)
        return {
            "success": True,
            "output": _build_output_layer_ref(target),
            "info": f"重构字段完成: {target.name()}；字段: {_field_preview(target)}",
            "output_layers": [_build_output_layer_ref(target)],
        }
    except Exception as exc:
        return {"success": False, "output": "", "info": f"重构字段失败: {str(exc)}"}
