"""任务知识图谱的结构、证据、引用和路线拓扑校验。"""

from __future__ import annotations

from dataclasses import asdict, dataclass, field
from math import isfinite
from typing import Any, Dict, List, Mapping, Optional, Sequence, Tuple

from .review import VerifiedReview
from .schema import load_task_knowledge_schema, normalize_task_knowledge
from .vocabulary import (
    ANCHOR_REF_TYPES,
    CALLOUT_TEMPLATES,
    COORDINATE_SOURCES,
    DIRECTIONS,
    ENTITY_KINDS,
    EVENT_TYPES,
    MAP_ROLES,
    MAP_TYPES,
    NODE_REF_TYPES,
    PATH_ROLES,
    SCHEMA_VERSION,
    SELF_INTERSECTION_POLICIES,
    SIDES,
    SYMBOL_TYPES,
    TOPOLOGIES,
)


@dataclass(frozen=True)
class ValidationIssue:
    code: str
    path: str
    message: str
    severity: str = "error"

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)


@dataclass
class ValidationReport:
    issues: List[ValidationIssue] = field(default_factory=list)
    # Only the compiler enables this after verifying the saved review token.
    # Accepting extracted content is distinct from claiming it was corrected.
    accepted_extraction: bool = field(default=False, repr=False)

    @property
    def is_valid(self) -> bool:
        return not any(issue.severity == "error" for issue in self.issues)

    @property
    def errors(self) -> List[ValidationIssue]:
        return [issue for issue in self.issues if issue.severity == "error"]

    @property
    def warnings(self) -> List[ValidationIssue]:
        return [issue for issue in self.issues if issue.severity == "warning"]

    def add(
        self,
        code: str,
        path: str,
        message: str,
        severity: str = "error",
    ) -> None:
        if self.accepted_extraction and _is_content_diagnostic(code, path):
            severity = "warning"
        self.issues.append(
            ValidationIssue(
                code=code,
                path=path,
                message=message,
                severity=severity,
            )
        )

    def extend(self, other: "ValidationReport") -> None:
        self.issues.extend(other.issues)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "is_valid": self.is_valid,
            "error_count": len(self.errors),
            "warning_count": len(self.warnings),
            "issues": [issue.to_dict() for issue in self.issues],
        }


def _is_content_diagnostic(code: str, path: str) -> bool:
    """Content judgments are advisory once the saved extraction is accepted.

    Geometry, reference resolution and renderer structure still have to be
    usable. In particular, this must not hide a bad coordinate or silently
    discard a route. The Go adapter prepares unsupported path combinations.
    """

    if code in {
        "invalid_confidence", "evidence_required", "dangling_evidence_ref",
        "evidence_quote_not_in_source", "invalid_evidence_offset",
        "evidence_offset_mismatch", "background_entity_in_route",
        "invalid_coordinate_source", "coordinate_metadata_without_position",
        "invalid_factual_flag", "control_marked_factual",
        "invalid_control_coordinate_source", "control_reason_required",
        "factual_place_required", "event_anchored_to_control",
        "control_point_cannot_be_visible", "duplicated_coordinate_forbidden",
        "low_level_render_field_forbidden", "schema_unknown_field",
        "unknown_top_level_field", "dangling_actor_ref",
        "route_entities_insufficient", "route_required",
    }:
        return True
    if code.startswith("schema_") and code != "schema_type_mismatch":
        if code == "schema_min_items" and path in {"$.routes", "$.spatial_entities"}:
            return True
        field_name = path.rsplit(".", 1)[-1].split("[", 1)[0]
        return field_name in {
            "confidence", "coordinate_confidence", "evidence_refs",
            "coordinate_source", "factual", "generation_reason",
        } or path.startswith(("evidence[", "$.evidence["))
    if code == "required_string_missing" and path.startswith("evidence["):
        return True
    return False


def _is_number(value: Any) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def _require_nonempty_string(
    value: Any,
    path: str,
    report: ValidationReport,
    code: str = "required_string_missing",
) -> bool:
    if not isinstance(value, str) or not value.strip():
        report.add(code, path, "必须是非空字符串")
        return False
    return True


def _check_confidence(
    value: Any,
    path: str,
    report: ValidationReport,
    *,
    required: bool = True,
) -> None:
    if value is None and not required:
        return
    if not _is_number(value) or not 0 <= float(value) <= 1:
        report.add(
            "invalid_confidence",
            path,
            "置信度必须是 0 到 1 之间的数字",
        )


def _index_objects(
    values: Any,
    id_key: str,
    path: str,
    report: ValidationReport,
    global_ids: Dict[str, str],
) -> Dict[str, Dict[str, Any]]:
    index: Dict[str, Dict[str, Any]] = {}
    if not isinstance(values, list):
        report.add("invalid_collection", path, "必须是数组")
        return index
    for idx, item in enumerate(values):
        item_path = f"{path}[{idx}]"
        if not isinstance(item, dict):
            report.add("invalid_object", item_path, "必须是对象")
            continue
        object_id = item.get(id_key)
        if not _require_nonempty_string(
            object_id,
            f"{item_path}.{id_key}",
            report,
            "missing_id",
        ):
            continue
        object_id = str(object_id).strip()
        if object_id in index:
            report.add(
                "duplicate_id",
                f"{item_path}.{id_key}",
                f"ID 重复：{object_id}",
            )
            continue
        if object_id in global_ids:
            report.add(
                "duplicate_global_id",
                f"{item_path}.{id_key}",
                f"ID 与 {global_ids[object_id]} 重复：{object_id}",
            )
        else:
            global_ids[object_id] = f"{item_path}.{id_key}"
        index[object_id] = item
    return index


def _check_evidence_refs(
    refs: Any,
    path: str,
    evidence_index: Mapping[str, Dict[str, Any]],
    report: ValidationReport,
    *,
    required: bool,
) -> None:
    if not isinstance(refs, list):
        report.add("invalid_evidence_refs", path, "必须是证据 ID 数组")
        return
    if required and not refs:
        report.add("evidence_required", path, "事实对象必须引用至少一条原文证据")
    for idx, ref in enumerate(refs):
        if not isinstance(ref, str) or ref not in evidence_index:
            report.add(
                "dangling_evidence_ref",
                f"{path}[{idx}]",
                f"证据引用不存在：{ref}",
            )


def _schema_path(parent: str, key: Any) -> str:
    if isinstance(key, int):
        return f"{parent}[{key}]"
    return str(key) if parent == "$" else f"{parent}.{key}"


def _matches_schema_type(value: Any, expected: str) -> bool:
    if expected == "object":
        return isinstance(value, Mapping)
    if expected == "array":
        return isinstance(value, list)
    if expected == "string":
        return isinstance(value, str)
    if expected == "boolean":
        return isinstance(value, bool)
    if expected == "integer":
        return isinstance(value, int) and not isinstance(value, bool)
    if expected == "number":
        return _is_number(value)
    return True


def _validate_schema_value(
    value: Any,
    schema: Mapping[str, Any],
    path: str,
    report: ValidationReport,
    reviewed_paths: frozenset[str] = frozenset(),
) -> None:
    """执行本项目 schema 使用到的 JSON Schema 子集，不引入 QGIS 外部依赖。"""

    expected_type = schema.get("type")
    if isinstance(expected_type, str) and not _matches_schema_type(
        value,
        expected_type,
    ):
        report.add(
            "schema_type_mismatch",
            path,
            f"字段类型应为 {expected_type}",
        )
        return

    if "const" in schema and value != schema["const"]:
        report.add(
            "schema_const_mismatch",
            path,
            f"字段值必须为 {schema['const']!r}",
        )
    enum_values = schema.get("enum")
    if isinstance(enum_values, list) and value not in enum_values:
        report.add(
            "schema_enum_mismatch",
            path,
            f"字段值不在受控词表中：{value!r}",
        )

    if isinstance(value, Mapping):
        properties = schema.get("properties")
        properties = properties if isinstance(properties, Mapping) else {}
        required = schema.get("required")
        if isinstance(required, list):
            for key in required:
                if path in reviewed_paths and key in {
                    "confidence",
                    "coordinate_confidence",
                    "evidence_refs",
                }:
                    continue
                if key not in value:
                    report.add(
                        "schema_required_field",
                        _schema_path(path, key),
                        "缺少 JSON Schema 必填字段",
                    )
        if schema.get("additionalProperties") is False:
            for key in value:
                if key not in properties:
                    report.add(
                        (
                            "unknown_top_level_field"
                            if path == "$"
                            else "schema_unknown_field"
                        ),
                        _schema_path(path, key),
                        f"JSON Schema 不允许字段：{key}",
                    )
        for key, child_schema in properties.items():
            if key in value and isinstance(child_schema, Mapping):
                if path in reviewed_paths and key == "evidence_refs":
                    # A human supplement has explicit provenance rather than an
                    # invented source quote. Any supplied references still must
                    # exist, and all unreviewed records retain the strict schema.
                    child_schema = {**child_schema, "minItems": 0}
                _validate_schema_value(
                    value[key],
                    child_schema,
                    _schema_path(path, key),
                    report,
                    reviewed_paths,
                )

    if isinstance(value, list):
        minimum_items = schema.get("minItems")
        if isinstance(minimum_items, int) and len(value) < minimum_items:
            report.add(
                "schema_min_items",
                path,
                f"数组至少需要 {minimum_items} 项",
            )
        item_schema = schema.get("items")
        if isinstance(item_schema, Mapping):
            for idx, item in enumerate(value):
                _validate_schema_value(
                    item,
                    item_schema,
                    _schema_path(path, idx),
                    report,
                    reviewed_paths,
                )

    if isinstance(value, str):
        minimum_length = schema.get("minLength")
        if isinstance(minimum_length, int) and len(value) < minimum_length:
            report.add(
                "schema_min_length",
                path,
                f"字符串长度至少为 {minimum_length}",
            )

    if _is_number(value):
        number = float(value)
        if not isfinite(number):
            report.add("schema_nonfinite_number", path, "数值必须有限")
            return
        minimum = schema.get("minimum")
        maximum = schema.get("maximum")
        exclusive_minimum = schema.get("exclusiveMinimum")
        if _is_number(minimum) and number < float(minimum):
            report.add(
                "schema_minimum",
                path,
                f"数值不能小于 {minimum}",
            )
        if _is_number(maximum) and number > float(maximum):
            report.add(
                "schema_maximum",
                path,
                f"数值不能大于 {maximum}",
            )
        if _is_number(exclusive_minimum) and number <= float(exclusive_minimum):
            report.add(
                "schema_exclusive_minimum",
                path,
                f"数值必须大于 {exclusive_minimum}",
            )


def _validate_spatial_constraints(
    route_path: Mapping[str, Any],
    resolved_nodes: Sequence[str],
    entity_index: Mapping[str, Mapping[str, Any]],
    evidence_index: Mapping[str, Mapping[str, Any]],
    path: str,
    report: ValidationReport,
) -> None:
    # Controls in a rendering copy are not part of the semantic adjacency.
    factual_nodes = [
        ref for ref in resolved_nodes
        if entity_index.get(ref, {}).get("entity_kind") != "cartographic_control"
    ]
    attach = route_path.get("attach_ref")
    if attach in entity_index and entity_index[attach].get("entity_kind") != "cartographic_control":
        if route_path.get("role") == "branch" and (not factual_nodes or factual_nodes[0] != attach):
            factual_nodes.insert(0, str(attach))
        elif route_path.get("role") == "incoming" and (not factual_nodes or factual_nodes[-1] != attach):
            factual_nodes.append(str(attach))
    for idx, constraint in enumerate(route_path.get("spatial_constraints", [])):
        target = f"{path}.spatial_constraints[{idx}]"
        if not isinstance(constraint, Mapping):
            continue  # Already diagnosed by the schema validator.
        kind = constraint.get("kind")
        if "evidence_refs" in constraint:
            _check_evidence_refs(constraint["evidence_refs"], f"{target}.evidence_refs", evidence_index, report, required=False)
        keys = ("target_ref",) if kind == "approach_from" else ("after_ref", "before_ref")
        for key in keys:
            ref = constraint.get(key)
            if ref not in entity_index:
                report.add("spatial_constraint_reference", f"{target}.{key}", "方位约束必须引用已有空间实体")
            elif entity_index[str(ref)].get("entity_kind") == "cartographic_control":
                report.add("spatial_constraint_reference", f"{target}.{key}", "方位约束须引用实际地点，不能引用制图控制点")
        if kind == "approach_from":
            if route_path.get("role") == "branch":
                report.add("spatial_constraint_role", target, "来向约束用于 main 或 incoming，已知连接点出发的 branch 不添加示意箭尾")
            for key in ("after_ref", "before_ref", "relation", "reference", "occurrence"):
                if key in constraint:
                    report.add("invalid_spatial_constraint", f"{target}.{key}", f"来向约束不使用 {key}")
            if not factual_nodes or constraint.get("target_ref") != factual_nodes[0]:
                report.add("spatial_constraint_target", f"{target}.target_ref", "来向约束必须指向本路径第一个实际地点")
        elif kind == "via_relative":
            if "target_ref" in constraint:
                report.add("invalid_spatial_constraint", f"{target}.target_ref", "途经方位约束不使用 target_ref")
            if constraint.get("relation") not in {"within", "outside"}:
                report.add("invalid_spatial_constraint", f"{target}.relation", "须区分区域内部 within 和区域之外 outside")
            matches = sum(
                left == constraint.get("after_ref") and right == constraint.get("before_ref")
                for left, right in zip(factual_nodes, factual_nodes[1:])
            )
            occurrence = constraint.get("occurrence", 1)
            if not isinstance(occurrence, int) or isinstance(occurrence, bool) or occurrence < 1:
                report.add("invalid_spatial_constraint", f"{target}.occurrence", "路段出现次数 occurrence 必须为正整数")
            elif occurrence > matches:
                report.add("spatial_constraint_segment", target, "途经方位约束须引用本路径实际存在的相邻路段及出现次数")
            reference = constraint.get("reference")
            if not isinstance(reference, Mapping) or not str(reference.get("name", "")).strip():
                report.add("invalid_spatial_constraint", f"{target}.reference", "途经方位约束需要 reference.name 定位参照")
                continue
            if "lon" in reference or "lat" in reference:
                for key, limit in (("lon", 180), ("lat", 90)):
                    coordinate = reference.get(key)
                    if not _is_number(coordinate) or not -limit <= float(coordinate) <= limit:
                        report.add("invalid_spatial_constraint_coordinate", f"{target}.reference.{key}", "定位参照须成对提供合法经纬度")
                if reference.get("coordinate_source") not in {"source_text", "manual", "gazetteer", "llm_estimated"}:
                    report.add("invalid_spatial_constraint_coordinate", f"{target}.reference.coordinate_source", "定位参照须标明坐标来源")
                if reference.get("coordinate_source") == "llm_estimated":
                    _check_confidence(reference.get("coordinate_confidence"), f"{target}.reference.coordinate_confidence", report, required=True)


def validate_task_knowledge(
    raw: Mapping[str, Any] | Any,
    *,
    source_text: Optional[str] = None,
    review: Optional[VerifiedReview] = None,
    accept_extraction: bool = False,
    route_style_presets=None,
) -> ValidationReport:
    """校验模型任务图；函数不会修改调用方传入的数据。"""

    style_presets = route_style_presets
    report = ValidationReport()
    if not isinstance(raw, Mapping):
        report.add("invalid_root", "$", "任务知识图谱顶层必须是对象")
        return report
    if review is not None and not isinstance(review, VerifiedReview):
        report.add(
            "unverified_review_context", "$", "人工修改必须经过本地核对凭据验证"
        )
        return report
    report.accepted_extraction = accept_extraction and isinstance(review, VerifiedReview)
    reviewed_paths = (
        review.object_paths(raw) if review is not None else frozenset()
    )

    schema = load_task_knowledge_schema()
    if route_style_presets is not None:
        schema["properties"]["routes"]["items"]["properties"]["style_ref"]["enum"] = list(style_presets)
    else:
        schema["properties"]["routes"]["items"]["properties"]["style_ref"].pop("enum", None)
    _validate_schema_value(
        raw,
        schema,
        "$",
        report,
        reviewed_paths,
    )
    # An editor may temporarily submit an empty/incorrectly typed collection.
    # Return field diagnostics before semantic checks dereference those values.
    if any(
        issue.code in {"schema_type_mismatch", "schema_enum_mismatch"}
        for issue in report.errors
    ):
        return report

    graph = normalize_task_knowledge(raw)
    if "schema_version" not in raw:
        report.add(
            "missing_schema_version",
            "schema_version",
            "必须显式声明 schema_version",
        )
    if graph.get("schema_version") != SCHEMA_VERSION:
        report.add(
            "unsupported_schema_version",
            "schema_version",
            f"只支持 schema_version={SCHEMA_VERSION}",
        )

    task = graph.get("task")
    if not isinstance(task, dict):
        report.add("invalid_task", "task", "task 必须是对象")
    else:
        _require_nonempty_string(task.get("task_id"), "task.task_id", report)
        _require_nonempty_string(task.get("title"), "task.title", report)
        if task.get("map_type") not in MAP_TYPES:
            report.add(
                "invalid_map_type",
                "task.map_type",
                f"只支持：{sorted(MAP_TYPES)}",
            )
        _require_nonempty_string(task.get("input_crs"), "task.input_crs", report)

    global_ids: Dict[str, str] = {}
    evidence_index = _index_objects(
        graph.get("evidence"),
        "evidence_id",
        "evidence",
        report,
        global_ids,
    )
    for idx, evidence in enumerate(graph.get("evidence", [])):
        if not isinstance(evidence, dict):
            continue
        path = f"evidence[{idx}]"
        quote = evidence.get("quote")
        if not _require_nonempty_string(quote, f"{path}.quote", report):
            continue
        quote = str(quote)
        if source_text is not None and quote not in source_text:
            report.add(
                "evidence_quote_not_in_source",
                f"{path}.quote",
                "证据片段未在原文中找到",
            )
        start = evidence.get("start")
        end = evidence.get("end")
        if start is not None or end is not None:
            if not isinstance(start, int) or not isinstance(end, int):
                report.add(
                    "invalid_evidence_offset",
                    path,
                    "start/end 必须同时为整数",
                )
            elif start < 0 or end < start:
                report.add(
                    "invalid_evidence_offset",
                    path,
                    "证据偏移范围无效",
                )
            elif source_text is not None and source_text[start:end] != quote:
                report.add(
                    "evidence_offset_mismatch",
                    path,
                    "start/end 与 quote 在原文中的位置不一致",
                )

    actor_index = _index_objects(
        graph.get("actors"),
        "actor_id",
        "actors",
        report,
        global_ids,
    )
    for idx, actor in enumerate(graph.get("actors", [])):
        if not isinstance(actor, dict):
            continue
        path = f"actors[{idx}]"
        _require_nonempty_string(actor.get("name"), f"{path}.name", report)
        _check_confidence(
            actor.get("confidence"),
            f"{path}.confidence",
            report,
            required=path not in reviewed_paths,
        )
        _check_evidence_refs(
            actor.get("evidence_refs"),
            f"{path}.evidence_refs",
            evidence_index,
            report,
            required=path not in reviewed_paths,
        )

    entity_index = _index_objects(
        graph.get("spatial_entities"),
        "entity_id",
        "spatial_entities",
        report,
        global_ids,
    )
    if len(entity_index) < 2:
        report.add(
            "route_entities_insufficient",
            "spatial_entities",
            "行军路线制图至少需要两个空间实体",
        )
    for idx, entity in enumerate(graph.get("spatial_entities", [])):
        if not isinstance(entity, dict):
            continue
        path = f"spatial_entities[{idx}]"
        kind = entity.get("entity_kind")
        if kind not in ENTITY_KINDS:
            report.add(
                "invalid_entity_kind",
                f"{path}.entity_kind",
                f"未知空间实体类型：{kind}",
            )
        _require_nonempty_string(entity.get("name"), f"{path}.name", report)
        role = entity.get("map_role")
        if role not in MAP_ROLES:
            report.add(
                "invalid_map_role",
                f"{path}.map_role",
                f"未知制图角色：{role}",
            )

        has_coordinates = "lon" in entity or "lat" in entity
        coordinate_source = entity.get("coordinate_source")
        if has_coordinates:
            for key, limit, label in (("lon", 180, "经度"), ("lat", 90, "纬度")):
                value = entity.get(key)
                if not _is_number(value) or not -limit <= float(value) <= limit:
                    report.add(
                        "invalid_longitude" if key == "lon" else "invalid_latitude",
                        f"{path}.{key}",
                        f"经纬度必须成对提供，{label}必须在 {-limit} 到 {limit} 之间",
                    )
            if coordinate_source not in COORDINATE_SOURCES:
                report.add(
                    "invalid_coordinate_source",
                    f"{path}.coordinate_source",
                    f"已有坐标必须提供有效来源：{coordinate_source}",
                )
            confidence = entity.get("coordinate_confidence")
            _check_confidence(
                confidence,
                f"{path}.coordinate_confidence",
                report,
                # Literal source coordinates and gazetteer matches do not need
                # an invented probability. Supplied confidence still validates;
                # model estimates must retain their explicit uncertainty.
                required=path not in reviewed_paths
                and coordinate_source not in {"source_text", "gazetteer"},
            )
            if coordinate_source == "llm_estimated" and _is_number(confidence) and 0 <= confidence < 0.5:
                report.add(
                    "low_coordinate_confidence",
                    f"{path}.coordinate_confidence",
                    f"地点 {entity.get('name')} 使用模型估算坐标（置信度 {confidence:g}），按估算位置参与制图",
                    severity="warning",
                )
        else:
            for key in ("coordinate_source", "coordinate_confidence"):
                if key in entity:
                    report.add(
                        "coordinate_metadata_without_position",
                        f"{path}.{key}",
                        "待定位地点应同时省略坐标来源和置信度",
                    )
            report.add(
                "coordinate_pending",
                f"{path}.lon",
                f"地点 {entity.get('name')} 已保留，坐标待定位；绘图使用该地点前须补齐经纬度",
                severity="warning",
            )

        factual = entity.get("factual")
        if not isinstance(factual, bool):
            report.add(
                "invalid_factual_flag",
                f"{path}.factual",
                "factual 必须是布尔值",
            )
        is_control = kind == "cartographic_control"
        if is_control:
            if factual is not False:
                report.add(
                    "control_marked_factual",
                    f"{path}.factual",
                    "制图控制点必须 factual=false",
                )
            if has_coordinates and coordinate_source != "derived_control":
                report.add(
                    "invalid_control_coordinate_source",
                    f"{path}.coordinate_source",
                    "制图控制点必须使用 derived_control",
                )
            _require_nonempty_string(
                entity.get("generation_reason"),
                f"{path}.generation_reason",
                report,
                "control_reason_required",
            )
        elif factual is not True:
            report.add(
                "factual_place_required",
                f"{path}.factual",
                "真实地点或区域必须 factual=true",
            )

        _check_evidence_refs(
            entity.get("evidence_refs"),
            f"{path}.evidence_refs",
            evidence_index,
            report,
            required=not is_control and path not in reviewed_paths,
        )

    event_index = _index_objects(
        graph.get("events"),
        "event_id",
        "events",
        report,
        global_ids,
    )
    for idx, event in enumerate(graph.get("events", [])):
        if not isinstance(event, dict):
            continue
        path = f"events[{idx}]"
        _require_nonempty_string(event.get("name"), f"{path}.name", report)
        event_type = event.get("event_type")
        if event_type not in EVENT_TYPES:
            report.add(
                "invalid_event_type",
                f"{path}.event_type",
                f"未知事件类型：{event_type}",
            )
        location_ref = event.get("location_ref")
        if location_ref not in entity_index:
            report.add(
                "dangling_location_ref",
                f"{path}.location_ref",
                f"事件地点引用不存在：{location_ref}",
            )
        elif entity_index[str(location_ref)].get("entity_kind") == "cartographic_control":
            report.add(
                "event_anchored_to_control",
                f"{path}.location_ref",
                "历史事件不能锚定到制图控制点",
            )
        actor_refs = event.get("actor_refs")
        if not isinstance(actor_refs, list):
            report.add(
                "invalid_actor_refs",
                f"{path}.actor_refs",
                "actor_refs 必须是数组",
            )
        else:
            for ref_idx, actor_ref in enumerate(actor_refs):
                if actor_ref not in actor_index:
                    report.add(
                        "dangling_actor_ref",
                        f"{path}.actor_refs[{ref_idx}]",
                        f"主体引用不存在：{actor_ref}",
                    )
        _check_confidence(
            event.get("confidence"),
            f"{path}.confidence",
            report,
            required=path not in reviewed_paths,
        )
        _check_evidence_refs(
            event.get("evidence_refs"),
            f"{path}.evidence_refs",
            evidence_index,
            report,
            required=path not in reviewed_paths,
        )
        for forbidden in ("x", "y", "lon", "lat"):
            if forbidden in event:
                report.add(
                    "duplicated_coordinate_forbidden",
                    f"{path}.{forbidden}",
                    "事件坐标必须由 location_ref 推导，不能重复填写",
                )

    def resolve_node(
        node: Any,
        path: str,
    ) -> Optional[str]:
        if not isinstance(node, dict):
            report.add("invalid_route_node", path, "路线节点必须是对象")
            return None
        ref_type = node.get("ref_type")
        ref_id = node.get("ref_id")
        if ref_type not in NODE_REF_TYPES:
            report.add(
                "invalid_node_ref_type",
                f"{path}.ref_type",
                f"未知节点引用类型：{ref_type}",
            )
            return None
        if ref_type == "spatial_entity":
            entity = entity_index.get(str(ref_id))
            if entity is None:
                report.add(
                    "dangling_node_ref",
                    f"{path}.ref_id",
                    f"空间实体引用不存在：{ref_id}",
                )
                return None
            if entity.get("map_role") == "background":
                report.add(
                    "background_entity_in_route",
                    f"{path}.ref_id",
                    "背景地点不能作为路线节点",
                )
            return str(ref_id)
        event = event_index.get(str(ref_id))
        if event is None:
            report.add(
                "dangling_node_ref",
                f"{path}.ref_id",
                f"事件引用不存在：{ref_id}",
            )
            return None
        location_ref = event.get("location_ref")
        entity = entity_index.get(str(location_ref))
        if entity is None:
            return None
        if entity.get("map_role") == "background":
            report.add(
                "background_entity_in_route",
                f"{path}.ref_id",
                "事件解析到的背景地点不能作为路线节点",
            )
        return str(location_ref)

    route_index = _index_objects(
        graph.get("routes"),
        "route_id",
        "routes",
        report,
        global_ids,
    )
    if not route_index:
        report.add(
            "route_required",
            "routes",
            "行军路线任务至少需要一条路线",
        )
    for idx, route in enumerate(graph.get("routes", [])):
        if not isinstance(route, dict):
            continue
        path = f"routes[{idx}]"
        _require_nonempty_string(route.get("name"), f"{path}.name", report)
        topology = route.get("topology")
        side = route.get("side")
        if topology not in TOPOLOGIES:
            report.add(
                "invalid_topology",
                f"{path}.topology",
                f"未知路线拓扑：{topology}",
            )
        if side not in SIDES:
            report.add(
                "invalid_side",
                f"{path}.side",
                f"未知敌我属性：{side}",
            )
        style_ref = route.get("style_ref")
        if style_presets is not None and style_ref is not None and style_ref not in style_presets:
            report.add(
                "unknown_style_preset",
                f"{path}.style_ref",
                f"未知视觉预设：{style_ref}",
            )
        elif style_presets is not None and style_ref is not None:
            style_preset = style_presets[str(style_ref)]
            if (
                style_preset.get("topology") != topology
                or style_preset.get("side") != side
            ):
                report.add(
                    "style_preset_semantic_mismatch",
                    f"{path}.style_ref",
                    "视觉预设与路线 topology/side 不一致",
                )
        if route.get("effective_width_scale") is not None:
            width = route.get("effective_width_scale")
            if not _is_number(width) or float(width) <= 0:
                report.add(
                    "invalid_width_scale",
                    f"{path}.effective_width_scale",
                    "最终宽度倍率必须大于 0",
                )
        for forbidden in ("draw_type", "route_type", "style_id", "width_scale"):
            if forbidden in route:
                report.add(
                    "low_level_render_field_forbidden",
                    f"{path}.{forbidden}",
                    "QGIS 底层字段由编译器确定，模型不得直接生成",
                )
        _check_confidence(
            route.get("confidence"),
            f"{path}.confidence",
            report,
            required=path not in reviewed_paths,
        )
        _check_evidence_refs(
            route.get("evidence_refs"),
            f"{path}.evidence_refs",
            evidence_index,
            report,
            required=path not in reviewed_paths,
        )

        paths = route.get("paths")
        if not isinstance(paths, list):
            report.add("invalid_paths", f"{path}.paths", "paths 必须是数组")
            continue
        path_ids: set[str] = set()
        path_records: List[Tuple[int, Dict[str, Any], List[str], List[int]]] = []
        role_counts = {role: 0 for role in PATH_ROLES}
        for path_idx, route_path in enumerate(paths):
            route_path_path = f"{path}.paths[{path_idx}]"
            if not isinstance(route_path, dict):
                report.add(
                    "invalid_path_object",
                    route_path_path,
                    "路径必须是对象",
                )
                continue
            path_id = route_path.get("path_id")
            if _require_nonempty_string(
                path_id,
                f"{route_path_path}.path_id",
                report,
                "missing_path_id",
            ):
                path_id = str(path_id)
                if path_id in path_ids:
                    report.add(
                        "duplicate_path_id",
                        f"{route_path_path}.path_id",
                        f"同一路线内 path_id 重复：{path_id}",
                    )
                path_ids.add(path_id)
            # Older saved graphs lack path-level provenance. Validate supplied
            # references without inventing a subject or copying route evidence.
            if "actor_refs" in route_path:
                for actor_idx, actor_ref in enumerate(route_path["actor_refs"]):
                    if actor_ref not in actor_index:
                        report.add(
                            "dangling_actor_ref",
                            f"{route_path_path}.actor_refs[{actor_idx}]",
                            f"路径主体引用不存在：{actor_ref}",
                        )
            if "evidence_refs" in route_path:
                _check_evidence_refs(
                    route_path["evidence_refs"],
                    f"{route_path_path}.evidence_refs",
                    evidence_index,
                    report,
                    required=False,
                )
            role = route_path.get("role")
            if role not in PATH_ROLES:
                report.add(
                    "invalid_path_role",
                    f"{route_path_path}.role",
                    f"未知路径角色：{role}",
                )
            else:
                role_counts[str(role)] += 1
            direction = route_path.get("direction")
            if direction not in DIRECTIONS:
                report.add(
                    "invalid_direction",
                    f"{route_path_path}.direction",
                    f"未知路径方向：{direction}",
                )
            expected_direction = {
                "main": "forward",
                "branch": "away_from_attach",
                "incoming": "toward_attach",
            }.get(str(role))
            if expected_direction and direction != expected_direction:
                report.add(
                    "path_direction_mismatch",
                    f"{route_path_path}.direction",
                    f"{role} 路径方向必须为 {expected_direction}",
                )

            nodes = route_path.get("nodes")
            resolved_node_indices: List[int] = []
            if not isinstance(nodes, list):
                report.add(
                    "invalid_route_nodes",
                    f"{route_path_path}.nodes",
                    "nodes 必须是数组",
                )
                resolved_nodes: List[str] = []
            else:
                resolved_nodes = []
                for node_idx, node in enumerate(nodes):
                    resolved = resolve_node(
                        node,
                        f"{route_path_path}.nodes[{node_idx}]",
                    )
                    if resolved:
                        if resolved_nodes and resolved_nodes[-1] == resolved:
                            report.add(
                                "adjacent_duplicate_route_node",
                                f"{route_path_path}.nodes[{node_idx}]",
                                "相邻路线节点解析到了同一地点",
                            )
                        resolved_nodes.append(resolved)
                        resolved_node_indices.append(node_idx)
            _validate_spatial_constraints(
                route_path, resolved_nodes, entity_index, evidence_index,
                route_path_path, report,
            )
            path_records.append(
                (path_idx, route_path, resolved_nodes, resolved_node_indices)
            )

        if role_counts["main"] != 1:
            report.add(
                "main_path_count_invalid",
                f"{path}.paths",
                "每条路线必须恰好有一条 main 路径",
            )
        main_record = next(
            (
                record
                for record in path_records
                if record[1].get("role") == "main"
            ),
            None,
        )
        main_nodes = main_record[2] if main_record else []
        if main_record and len(main_nodes) < 2:
            report.add(
                "main_path_too_short",
                f"{path}.paths[{main_record[0]}].nodes",
                "main 路径至少需要两个有效节点",
            )

        if topology == "standard" and (
            role_counts["branch"] or role_counts["incoming"]
        ):
            report.add(
                "standard_has_extra_paths",
                f"{path}.paths",
                "standard 路线只能包含 main",
            )
        if topology == "branch":
            if role_counts["branch"] < 1:
                report.add(
                    "branch_path_required",
                    f"{path}.paths",
                    "branch 路线必须包含一条 branch 路径",
                )
            if role_counts["branch"] > 1:
                report.add(
                    "adapter_capability_error",
                    f"{path}.paths",
                    "当前六表适配器一条 route 只能可靠表达一条 branch",
                )
            if role_counts["incoming"]:
                report.add(
                    "branch_has_incoming_paths",
                    f"{path}.paths",
                    "branch 路线不能同时包含 incoming",
                )
        if topology == "multi_start":
            if role_counts["incoming"] < 1:
                report.add(
                    "incoming_path_required",
                    f"{path}.paths",
                    "multi_start 路线必须包含至少一条 incoming",
                )
            if role_counts["branch"]:
                report.add(
                    "multi_start_has_branch_paths",
                    f"{path}.paths",
                    "multi_start 路线不能同时包含 branch",
                )

        for path_idx, route_path, resolved_nodes, node_indices in path_records:
            role = route_path.get("role")
            if role not in {"branch", "incoming"}:
                continue
            route_path_path = f"{path}.paths[{path_idx}]"
            attach_ref = route_path.get("attach_ref")
            if attach_ref not in entity_index:
                report.add(
                    "dangling_attach_ref",
                    f"{route_path_path}.attach_ref",
                    f"连接点不存在：{attach_ref}",
                )
                continue
            if str(attach_ref) not in main_nodes:
                report.add(
                    "attach_not_on_main",
                    f"{route_path_path}.attach_ref",
                    "branch/incoming 的连接点必须位于 main 路径上",
                )
            for node_idx, resolved in zip(node_indices, resolved_nodes):
                if resolved == str(attach_ref):
                    report.add(
                        "attach_duplicated_in_path",
                        f"{route_path_path}.nodes[{node_idx}]",
                        "连接点只写 attach_ref，不应在支路节点中重复；"
                        "真实再次经过连接点的行程需明确表示，不能自动删除",
                    )
            if not resolved_nodes:
                report.add(
                    "secondary_path_empty",
                    f"{route_path_path}.nodes",
                    "branch/incoming 至少需要一个有效节点",
                )

    presentation_index = _index_objects(
        graph.get("point_presentations"),
        "presentation_id",
        "point_presentations",
        report,
        global_ids,
    )
    del presentation_index
    for idx, presentation in enumerate(graph.get("point_presentations", [])):
        if not isinstance(presentation, dict):
            continue
        path = f"point_presentations[{idx}]"
        if "evidence_refs" in presentation:
            _check_evidence_refs(
                presentation["evidence_refs"],
                f"{path}.evidence_refs",
                evidence_index,
                report,
                required=False,
            )
        anchor_type = presentation.get("anchor_ref_type")
        anchor_ref = presentation.get("anchor_ref")
        if anchor_type not in ANCHOR_REF_TYPES:
            report.add(
                "invalid_anchor_ref_type",
                f"{path}.anchor_ref_type",
                f"未知锚点引用类型：{anchor_type}",
            )
        elif anchor_type == "event" and anchor_ref not in event_index:
            report.add(
                "dangling_anchor_ref",
                f"{path}.anchor_ref",
                f"事件锚点不存在：{anchor_ref}",
            )
        elif anchor_type == "spatial_entity" and anchor_ref not in entity_index:
            report.add(
                "dangling_anchor_ref",
                f"{path}.anchor_ref",
                f"空间实体锚点不存在：{anchor_ref}",
            )
        elif (
            anchor_type == "spatial_entity"
            and entity_index[str(anchor_ref)].get("entity_kind")
            == "cartographic_control"
            and presentation.get("visible") is not False
        ):
            report.add(
                "control_point_cannot_be_visible",
                f"{path}.anchor_ref",
                "制图控制点默认不可作为历史地点符号显示",
            )
        if presentation.get("symbol_type") not in SYMBOL_TYPES:
            report.add(
                "invalid_symbol_type",
                f"{path}.symbol_type",
                f"未知符号类型：{presentation.get('symbol_type')}",
            )
        if not _is_number(presentation.get("priority")):
            report.add(
                "invalid_priority",
                f"{path}.priority",
                "priority 必须是数字",
            )
        if not isinstance(presentation.get("visible"), bool):
            report.add(
                "invalid_visible_flag",
                f"{path}.visible",
                "visible 必须是布尔值",
            )
        for forbidden in ("x", "y", "lon", "lat"):
            if forbidden in presentation:
                report.add(
                    "duplicated_coordinate_forbidden",
                    f"{path}.{forbidden}",
                    "点符号坐标必须由锚点推导",
                )

    callout_index = _index_objects(
        graph.get("callouts"),
        "callout_id",
        "callouts",
        report,
        global_ids,
    )
    del callout_index
    for idx, callout in enumerate(graph.get("callouts", [])):
        if not isinstance(callout, dict):
            continue
        path = f"callouts[{idx}]"
        if "evidence_refs" in callout:
            _check_evidence_refs(
                callout["evidence_refs"],
                f"{path}.evidence_refs",
                evidence_index,
                report,
                required=False,
            )
        anchor_type = callout.get("anchor_ref_type")
        anchor_ref = callout.get("anchor_ref")
        if anchor_type not in ANCHOR_REF_TYPES:
            report.add(
                "invalid_anchor_ref_type",
                f"{path}.anchor_ref_type",
                f"未知锚点引用类型：{anchor_type}",
            )
        elif anchor_type == "event" and anchor_ref not in event_index:
            report.add(
                "dangling_anchor_ref",
                f"{path}.anchor_ref",
                f"事件锚点不存在：{anchor_ref}",
            )
        elif anchor_type == "spatial_entity" and anchor_ref not in entity_index:
            report.add(
                "dangling_anchor_ref",
                f"{path}.anchor_ref",
                f"空间实体锚点不存在：{anchor_ref}",
            )
        template = callout.get("template")
        if template not in CALLOUT_TEMPLATES:
            report.add(
                "invalid_callout_template",
                f"{path}.template",
                f"未知注记模板：{template}",
            )
        if not _is_number(callout.get("priority")):
            report.add(
                "invalid_priority",
                f"{path}.priority",
                "priority 必须是数字",
            )
        if not _is_number(callout.get("width_mm")) or float(
            callout.get("width_mm") or 0
        ) <= 0:
            report.add(
                "invalid_callout_width",
                f"{path}.width_mm",
                "width_mm 必须大于 0",
            )
        for forbidden in ("x", "y", "lon", "lat"):
            if forbidden in callout:
                report.add(
                    "duplicated_coordinate_forbidden",
                    f"{path}.{forbidden}",
                    "注记坐标必须由锚点推导",
                )

    constraints = graph.get("output_constraints")
    if not isinstance(constraints, dict):
        report.add(
            "invalid_output_constraints",
            "output_constraints",
            "output_constraints 必须是对象",
        )
    else:
        policy = constraints.get("self_intersection_policy")
        if policy not in SELF_INTERSECTION_POLICIES:
            report.add(
                "invalid_self_intersection_policy",
                "output_constraints.self_intersection_policy",
                f"未知自相交策略：{policy}",
            )

    return report
