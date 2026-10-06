"""把任务知识图谱确定性编译为现有 QGIS 路线插件消费的六张 CSV。"""

from __future__ import annotations

import csv
import json
import os
import shutil
import tempfile
import uuid
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any, Dict, Iterable, List, Mapping, Optional, Sequence, Tuple

from .schema import normalize_task_knowledge
from .review import VerifiedReview
from .validator import ValidationReport, validate_task_knowledge
from .vocabulary import (
    CSV_HEADERS,
    DEFAULT_ROUTE_STYLE,
    EVENT_SYMBOLS,
    ROUTE_STYLE_PRESETS,
    SYMBOL_PRESETS,
)


# Optional display columns extend the existing six-table contract; old datasets
# still use CSV_HEADERS unchanged and need no label migration.
_LABEL_HEADERS = {
    "route_paths.csv": ["label_text"],
    "points.csv": ["meeting_label"],
}


@dataclass
class CompilationResult:
    success: bool
    dataset_dir: str
    files: Dict[str, str]
    counts: Dict[str, int]
    validation: Dict[str, Any]
    warnings: List[str] = field(default_factory=list)
    task_graph_file: str = ""
    source_file: str = ""
    review_provenance_file: str = ""

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)


class TaskKnowledgeCompilationError(ValueError):
    def __init__(
        self,
        message: str,
        *,
        report: Optional[ValidationReport] = None,
    ):
        super().__init__(message)
        self.report = report or ValidationReport()


def _format_number(value: Any) -> str:
    if isinstance(value, bool):
        return "1" if value else "0"
    number = float(value)
    if number == 0:
        return "0"
    return format(number, ".12g")


def _read_csv(path: Path) -> Tuple[List[str], List[Dict[str, str]]]:
    with path.open("r", encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        return list(reader.fieldnames or []), list(reader)


def _write_csv(
    path: Path,
    headers: Sequence[str],
    rows: Iterable[Mapping[str, Any]],
) -> int:
    path.parent.mkdir(parents=True, exist_ok=True)
    count = 0
    with path.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(
            stream,
            fieldnames=list(headers),
            extrasaction="ignore",
            lineterminator="\n",
        )
        writer.writeheader()
        for row in rows:
            writer.writerow(
                {
                    header: "" if row.get(header) is None else row.get(header)
                    for header in headers
                }
            )
            count += 1
    return count


def _resolve_node_entity_id(
    node: Mapping[str, Any],
    events: Mapping[str, Dict[str, Any]],
) -> str:
    ref_id = str(node.get("ref_id"))
    if node.get("ref_type") == "event":
        return str(events[ref_id]["location_ref"])
    return ref_id


def _resolve_anchor_entity_id(
    item: Mapping[str, Any],
    events: Mapping[str, Dict[str, Any]],
) -> str:
    anchor_ref = str(item.get("anchor_ref"))
    if item.get("anchor_ref_type") == "event":
        return str(events[anchor_ref]["location_ref"])
    return anchor_ref


def _make_render_names(
    entities: Sequence[Mapping[str, Any]],
) -> Dict[str, str]:
    name_counts: Dict[str, int] = {}
    for entity in entities:
        name = str(entity.get("name") or "").strip()
        name_counts[name] = name_counts.get(name, 0) + 1
    result: Dict[str, str] = {}
    for entity in entities:
        entity_id = str(entity["entity_id"])
        name = str(entity["name"]).strip()
        result[entity_id] = (
            name if name_counts.get(name, 0) == 1 else f"{name}@{entity_id}"
        )
    return result


def _choose_style(route: Mapping[str, Any]) -> Dict[str, Any]:
    topology = str(route.get("topology"))
    side = str(route.get("side"))
    style_ref = route.get("style_ref") or DEFAULT_ROUTE_STYLE[(topology, side)]
    return dict(ROUTE_STYLE_PRESETS[str(style_ref)])


def _actor_names(
    item: Mapping[str, Any],
    actors: Mapping[str, Mapping[str, Any]],
) -> List[str]:
    names: List[str] = []
    refs = item.get("actor_refs") or []
    if not isinstance(refs, (list, tuple)):
        return names
    for actor_id in refs:
        name = str(actors.get(str(actor_id), {}).get("name") or "").strip()
        if name and name not in names:
            names.append(name)
    return names


def _meeting_label(
    events: Sequence[Mapping[str, Any]],
    actors: Mapping[str, Mapping[str, Any]],
) -> str:
    groups: List[Tuple[Mapping[str, Any], str]] = []
    for event in events:
        event_type = event.get("event_type")
        description = " ".join(
            str(event.get(key) or "") for key in ("name", "title", "description")
        )
        # "meeting" also represents political conferences. Only an explicit
        # rendezvous event supplies participant labels; arrow joins never do.
        if event_type != "rendezvous" and not (
            event_type == "meeting" and "会师" in description
        ):
            continue
        names = _actor_names(event, actors)
        if names:
            groups.append((event, "\n".join(names)))
    if len(groups) == 1:
        return groups[0][1]
    # Separate distinct events at one location instead of implying that all
    # units rendezvoused together just because their coordinates coincide.
    return "\n\n".join(
        "\n".join(filter(None, [
            " ".join(
                str(event.get(key) or "").strip()
                for key in ("time_text", "name")
            ).strip(),
            names,
        ]))
        for event, names in groups
    )


def _build_point_choices(
    graph: Mapping[str, Any],
    events: Mapping[str, Dict[str, Any]],
    entities: Mapping[str, Dict[str, Any]],
    used_entity_ids: set[str],
    warnings: List[str],
) -> Dict[str, Tuple[str, float]]:
    """为真实路线地点选一个符号，途经点作为已有专题符号的兜底。"""

    candidates: Dict[str, List[Tuple[float, int, str, str]]] = {}
    route_entity_ids: set[str] = set()

    def add(
        entity_id: str,
        symbol_type: str,
        priority: Any,
        rank: int,
        source: str,
    ) -> None:
        if entity_id not in used_entity_ids:
            return
        if entities[entity_id].get("entity_kind") == "cartographic_control":
            return
        candidates.setdefault(entity_id, []).append(
            (float(priority), rank, symbol_type, source)
        )

    for route in graph.get("routes", []):
        if not isinstance(route, dict) or route.get("enabled") is False:
            continue
        side = str(route.get("side") or "friendly")
        for route_path in route.get("paths", []):
            if not isinstance(route_path, dict):
                continue
            if route_path.get("attach_ref"):
                route_entity_ids.add(str(route_path["attach_ref"]))
            nodes = route_path.get("nodes") or []
            for idx, node in enumerate(nodes):
                if not isinstance(node, dict):
                    continue
                entity_id = _resolve_node_entity_id(node, events)
                route_entity_ids.add(entity_id)
                # 非敌方主路径起点保留旗帜，其他真实节点稍后补途经点。
                if side == "enemy" or route_path.get("role") != "main" or idx != 0:
                    continue
                add(
                    entity_id,
                    "start_flag",
                    SYMBOL_PRESETS["start_flag"]["priority"],
                    1,
                    f"route:{route.get('route_id')}",
                )

    for event in graph.get("events", []):
        if not isinstance(event, dict):
            continue
        symbol = EVENT_SYMBOLS.get(str(event.get("event_type")))
        if not symbol or symbol not in SYMBOL_PRESETS:
            continue
        add(
            str(event.get("location_ref")),
            symbol,
            event.get("priority", SYMBOL_PRESETS[symbol]["priority"]),
            2,
            f"event:{event.get('event_id')}",
        )

    for presentation in graph.get("point_presentations", []):
        if not isinstance(presentation, dict) or presentation.get("visible") is False:
            continue
        symbol = str(presentation.get("symbol_type"))
        if symbol not in SYMBOL_PRESETS:
            continue
        entity_id = _resolve_anchor_entity_id(presentation, events)
        add(
            entity_id,
            symbol,
            presentation.get("priority", 50),
            3,
            f"presentation:{presentation.get('presentation_id')}",
        )

    chosen: Dict[str, Tuple[str, float]] = {}
    for entity_id, entity_candidates in candidates.items():
        ordered = sorted(
            entity_candidates,
            key=lambda item: (item[0], item[1], item[2], item[3]),
            reverse=True,
        )
        selected = ordered[0]
        tied_types = {
            item[2]
            for item in ordered
            if item[0] == selected[0] and item[1] == selected[1]
        }
        if len(tied_types) > 1:
            warnings.append(
                f"地点 {entities[entity_id].get('name')} 存在同级符号冲突，"
                f"已稳定选择 {selected[2]}"
            )
        chosen[entity_id] = (selected[2], selected[0])
    for entity_id in route_entity_ids:
        if (
            entity_id in used_entity_ids
            and entities[entity_id].get("entity_kind") != "cartographic_control"
        ):
            chosen.setdefault(
                entity_id,
                ("waypoint", float(SYMBOL_PRESETS["waypoint"]["priority"])),
            )
    return chosen


def _collect_used_entities(
    graph: Mapping[str, Any],
    events: Mapping[str, Dict[str, Any]],
) -> set[str]:
    used: set[str] = set()
    for route in graph.get("routes", []):
        if not isinstance(route, dict):
            continue
        for route_path in route.get("paths", []):
            if not isinstance(route_path, dict):
                continue
            attach_ref = route_path.get("attach_ref")
            if attach_ref:
                used.add(str(attach_ref))
            for node in route_path.get("nodes", []):
                if isinstance(node, dict):
                    used.add(_resolve_node_entity_id(node, events))
    for event in graph.get("events", []):
        if isinstance(event, dict) and event.get("location_ref"):
            used.add(str(event["location_ref"]))
    for key in ("point_presentations", "callouts"):
        for item in graph.get(key, []):
            if isinstance(item, dict):
                used.add(_resolve_anchor_entity_id(item, events))
    return used


def validate_task_knowledge_for_compile(
    raw: Mapping[str, Any],
    *,
    source_text: Optional[str] = None,
    review: Optional[VerifiedReview] = None,
) -> ValidationReport:
    """在语义校验之外，检查本次六表实际使用地点的坐标是否齐全。"""

    report = validate_task_knowledge(
        raw, source_text=source_text, review=review,
        accept_extraction=review is not None,
    )
    if not report.is_valid:
        return report
    graph = normalize_task_knowledge(raw)
    events = {str(event["event_id"]): event for event in graph["events"]}
    used = _collect_used_entities(graph, events)
    if review is not None and review.source_graph is not None:
        used.update(_collect_used_entities(review.source_graph, events))
    for index, entity in enumerate(graph["spatial_entities"]):
        if str(entity["entity_id"]) not in used:
            continue
        for key in ("lon", "lat"):
            if key not in entity:
                report.add(
                    "missing_coordinate",
                    f"spatial_entities[{index}].{key}",
                    f"绘图地点 {entity['name']} 缺少{'经度' if key == 'lon' else '纬度'}；"
                    "知识已保留，请补齐坐标后再编译",
                )
    return report


def _build_rows(
    graph: Mapping[str, Any],
    *,
    accepted_graph: Optional[Mapping[str, Any]] = None,
) -> Tuple[Dict[str, List[Dict[str, Any]]], List[str]]:
    warnings: List[str] = []
    entity_list = [
        entity
        for entity in graph.get("spatial_entities", [])
        if isinstance(entity, dict)
    ]
    entities = {
        str(entity["entity_id"]): entity
        for entity in entity_list
    }
    events = {
        str(event["event_id"]): event
        for event in graph.get("events", [])
        if isinstance(event, dict)
    }
    actors = {
        str(actor["actor_id"]): actor
        for actor in graph.get("actors", []) or []
        if isinstance(actor, dict) and actor.get("actor_id")
    }
    render_names = _make_render_names(entity_list)
    used_entity_ids = _collect_used_entities(graph, events)
    accepted_entity_ids = (
        _collect_used_entities(accepted_graph, events) & entities.keys()
        if accepted_graph is not None else set()
    )
    used_entity_ids.update(accepted_entity_ids)
    point_choices = _build_point_choices(
        graph,
        events,
        entities,
        used_entity_ids,
        warnings,
    )
    # A one-location movement cannot form a line. Keep its known location on
    # the map instead of losing it when the renderer omits only that segment.
    for entity_id in accepted_entity_ids:
        if entities[entity_id].get("entity_kind") != "cartographic_control":
            point_choices.setdefault(
                entity_id, ("waypoint", float(SYMBOL_PRESETS["waypoint"]["priority"]))
            )

    events_by_entity: Dict[str, List[Dict[str, Any]]] = {}
    for event in graph.get("events", []):
        if isinstance(event, dict):
            events_by_entity.setdefault(str(event["location_ref"]), []).append(event)

    places_rows: List[Dict[str, Any]] = []
    points_rows: List[Dict[str, Any]] = []
    for entity in entity_list:
        entity_id = str(entity["entity_id"])
        if entity_id not in used_entity_ids:
            continue
        symbol, priority = point_choices.get(entity_id, ("", 0))
        entity_events = events_by_entity.get(entity_id, [])
        time_text = next(
            (
                str(event.get("time_text") or "")
                for event in entity_events
                if event.get("time_text")
            ),
            "",
        )
        event_desc = next(
            (
                str(event.get("description") or "")
                for event in entity_events
                if event.get("description")
            ),
            "",
        )
        description = (
            entity.get("description")
            or entity.get("generation_reason")
            or event_desc
            or ""
        )
        row = {
            "place_id": entity_id,
            "name": render_names[entity_id],
            "x": _format_number(entity["lon"]),
            "y": _format_number(entity["lat"]),
            "symbol_type": symbol,
            "priority": _format_number(priority),
            "place_type": entity.get("entity_kind", "place"),
            "date": time_text,
            "desc": description,
        }
        places_rows.append(row)
        if symbol and entity.get("entity_kind") != "cartographic_control":
            points_rows.append(
                {
                    "name": row["name"],
                    "x": row["x"],
                    "y": row["y"],
                    "symbol_type": symbol,
                    "priority": row["priority"],
                    "meeting_label": _meeting_label(entity_events, actors),
                }
            )

    policy = (
        graph.get("output_constraints", {}).get(
            "self_intersection_policy",
            "allow_with_overpass",
        )
        if isinstance(graph.get("output_constraints"), dict)
        else "allow_with_overpass"
    )
    input_crs = str(graph.get("task", {}).get("input_crs") or "EPSG:4326")
    routes_rows: List[Dict[str, Any]] = []
    route_paths_rows: List[Dict[str, Any]] = []
    for route in graph.get("routes", []):
        if not isinstance(route, dict):
            continue
        preset = _choose_style(route)
        width_scale: Any = ""
        if route.get("effective_width_scale") is not None:
            effective = float(route["effective_width_scale"])
            width_scale = (
                effective / 0.66
                if route.get("topology") == "branch"
                else effective
            )
            width_scale = _format_number(width_scale)
        routes_rows.append(
            {
                "route_id": route["route_id"],
                "route_name": route["name"],
                "route_type": preset["route_type"],
                "draw_type": preset["draw_type"],
                "style_id": preset["style_id"],
                "width_scale": width_scale,
                "input_crs": input_crs,
                "enabled": "1" if route.get("enabled", True) else "0",
                "notes": (
                    f"side={route.get('side')};"
                    f"topology={route.get('topology')};"
                    f"self_intersection_policy={policy}"
                ),
            }
        )
        for route_path in route.get("paths", []):
            if not isinstance(route_path, dict):
                continue
            role = str(route_path.get("role"))
            path_type = "start" if role == "incoming" else role
            label_text = ""
            if route.get("side") == "enemy":
                label_text = "、".join(_actor_names(route_path, actors)) or str(
                    route.get("name") or ""
                )
            attach_name = ""
            if role in {"branch", "incoming"}:
                attach_name = render_names[str(route_path["attach_ref"])]
            for seq, node in enumerate(route_path.get("nodes", []), start=1):
                entity_id = _resolve_node_entity_id(node, events)
                route_paths_rows.append(
                    {
                        "route_id": route["route_id"],
                        "path_id": route_path["path_id"],
                        "path_type": path_type,
                        "attach_place": attach_name,
                        "seq": seq,
                        "place_name": render_names[entity_id],
                        "label_text": label_text,
                    }
                )

    symbol_rows = [
        {
            "symbol_type": symbol_type,
            **preset,
        }
        for symbol_type, preset in SYMBOL_PRESETS.items()
    ]

    callout_rows: List[Dict[str, Any]] = []
    for callout in graph.get("callouts", []):
        if not isinstance(callout, dict):
            continue
        entity_id = _resolve_anchor_entity_id(callout, events)
        entity = entities[entity_id]
        callout_rows.append(
            {
                "id": callout["callout_id"],
                "place_name": render_names[entity_id],
                "x": _format_number(entity["lon"]),
                "y": _format_number(entity["lat"]),
                # Keep the legacy CSV column, but callouts no longer render titles.
                "title": "",
                "body": callout.get("body", ""),
                "template": callout.get("template") or (
                    "image_top_text_bottom"
                    if callout.get("image_enabled") and str(callout.get("image_path") or "").strip()
                    else "text_only"
                ),
                "image_enabled": (
                    "TRUE" if callout.get("image_enabled") else "FALSE"
                ),
                "image_path": callout.get("image_path", ""),
                "priority": _format_number(callout.get("priority", 0)),
                "preferred_side": callout.get("preferred_side", "right_top"),
                "width_mm": _format_number(callout.get("width_mm", 42)),
                "leader_style": callout.get("leader_style", "double"),
            }
        )

    rows = {
        "places.csv": places_rows,
        "routes.csv": routes_rows,
        "route_paths.csv": route_paths_rows,
        "points.csv": points_rows,
        "symbol_mapping.csv": symbol_rows,
        "callouts.csv": callout_rows,
    }
    return rows, warnings


def _resolve_symbol_source(
    symbol_type: str,
    *,
    template_dataset_dir: Optional[Path],
    symbol_assets_dir: Optional[Path],
) -> Path:
    candidates = []
    if symbol_assets_dir is not None:
        candidates.append(symbol_assets_dir)
    if template_dataset_dir is not None:
        candidates.append(template_dataset_dir / "symbols")
    candidates.append(Path(__file__).resolve().parent / "assets" / "symbols")
    for candidate in candidates:
        source = candidate / f"{symbol_type}.svg"
        if source.is_file():
            return source
    searched = "、".join(str(path) for path in candidates)
    raise TaskKnowledgeCompilationError(
        f"未找到 {symbol_type} 符号资产；已检查：{searched}"
    )


def _copy_assets(
    graph: Mapping[str, Any],
    stage_dir: Path,
    *,
    template_dataset_dir: Optional[Path],
    symbol_assets_dir: Optional[Path],
    warnings: List[str],
) -> None:
    target_symbols = stage_dir / "symbols"
    target_symbols.mkdir(parents=True, exist_ok=True)
    for symbol_type in SYMBOL_PRESETS:
        shutil.copyfile(
            _resolve_symbol_source(
                symbol_type,
                template_dataset_dir=template_dataset_dir,
                symbol_assets_dir=symbol_assets_dir,
            ),
            target_symbols / f"{symbol_type}.svg",
        )

    for callout in graph.get("callouts", []):
        if not isinstance(callout, dict) or not callout.get("image_enabled"):
            continue
        image_path_text = str(callout.get("image_path") or "").strip()
        if not image_path_text:
            warnings.append(
                f"注记 {callout.get('callout_id')} 启用了图片但没有 image_path"
            )
            continue
        image_path = Path(image_path_text)
        if image_path.is_absolute():
            if not image_path.is_file():
                warnings.append(f"注记图片不存在：{image_path}")
            continue
        if template_dataset_dir is None:
            warnings.append(
                f"注记 {callout.get('callout_id')} 的相对图片无法复制："
                "未提供 template_dataset_dir"
            )
            continue
        source_image = template_dataset_dir / image_path
        if not source_image.is_file():
            warnings.append(f"注记图片不存在：{source_image}")
            continue
        target_image = stage_dir / image_path
        target_image.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source_image, target_image)


def _install_stage(
    stage_dir: Path,
    output_dir: Path,
    *,
    overwrite: bool,
) -> None:
    if not output_dir.exists():
        os.replace(stage_dir, output_dir)
        return

    if output_dir.is_file():
        raise TaskKnowledgeCompilationError(f"输出路径是文件：{output_dir}")
    has_content = any(output_dir.iterdir())
    if has_content and not overwrite:
        raise TaskKnowledgeCompilationError(
            f"输出目录非空，若确认替换请设置 overwrite=true：{output_dir}"
        )
    if not has_content:
        output_dir.rmdir()
        os.replace(stage_dir, output_dir)
        return

    backup_dir = output_dir.parent / (
        f".{output_dir.name}.backup-{uuid.uuid4().hex}"
    )
    os.replace(output_dir, backup_dir)
    try:
        os.replace(stage_dir, output_dir)
    except Exception:
        os.replace(backup_dir, output_dir)
        raise
    shutil.rmtree(backup_dir, ignore_errors=True)


def _is_relative_to(path: Path, parent: Path) -> bool:
    try:
        path.relative_to(parent)
        return True
    except ValueError:
        return False


def compile_task_knowledge(
    raw: Mapping[str, Any],
    output_dir: Path | str,
    *,
    source_text: Optional[str] = None,
    template_dataset_dir: Optional[Path | str] = None,
    symbol_assets_dir: Optional[Path | str] = None,
    overwrite: bool = False,
    review: Optional[VerifiedReview] = None,
) -> CompilationResult:
    """校验并原子生成六表、符号资产、原文和任务图谱。"""

    report = validate_task_knowledge_for_compile(
        raw, source_text=source_text, review=review
    )
    if not report.is_valid:
        raise TaskKnowledgeCompilationError(
            "任务知识图谱校验失败",
            report=report,
        )
    graph = normalize_task_knowledge(raw)

    output_path = Path(output_dir).expanduser().resolve()
    if output_path.name in {"", ".", ".."}:
        raise TaskKnowledgeCompilationError("output_dir 不能是宽泛目录")
    filesystem_root = Path(output_path.anchor)
    if (
        output_path == filesystem_root
        or output_path.parent == filesystem_root
        or output_path == Path.home().resolve()
    ):
        raise TaskKnowledgeCompilationError(
            f"output_dir 过于宽泛，拒绝写入或替换：{output_path}"
        )
    output_path.parent.mkdir(parents=True, exist_ok=True)
    template_path = (
        Path(template_dataset_dir).expanduser().resolve()
        if template_dataset_dir
        else None
    )
    symbol_path = (
        Path(symbol_assets_dir).expanduser().resolve()
        if symbol_assets_dir
        else None
    )
    if template_path is not None and (
        template_path == output_path
        or _is_relative_to(template_path, output_path)
    ):
        raise TaskKnowledgeCompilationError(
            "template_dataset_dir 不能位于待替换的 output_dir 内"
        )
    if symbol_path is not None and (
        symbol_path == output_path
        or _is_relative_to(symbol_path, output_path)
    ):
        raise TaskKnowledgeCompilationError(
            "symbol_assets_dir 不能位于待替换的 output_dir 内"
        )

    rows, warnings = _build_rows(
        # Rendering defaults can evolve without changing the normalized graph
        # used by signed, older reviews. Preserve whether template was omitted.
        {**graph, "callouts": raw.get("callouts", [])},
        accepted_graph=review.source_graph if review is not None else None,
    )
    warnings = [issue.message for issue in report.warnings] + warnings
    stage_dir = Path(
        tempfile.mkdtemp(
            prefix=f".{output_path.name}.stage-",
            dir=str(output_path.parent),
        )
    )
    try:
        counts: Dict[str, int] = {}
        for filename, headers in CSV_HEADERS.items():
            counts[filename] = _write_csv(
                stage_dir / filename,
                [*headers, *_LABEL_HEADERS.get(filename, [])],
                rows[filename],
            )

        _copy_assets(
            graph,
            stage_dir,
            template_dataset_dir=template_path,
            symbol_assets_dir=symbol_path,
            warnings=warnings,
        )

        task_graph_path = stage_dir / "task_graph.json"
        task_graph_path.write_text(
            json.dumps(graph, ensure_ascii=False, indent=2, sort_keys=True)
            + "\n",
            encoding="utf-8",
        )
        source_path = stage_dir / "source.txt"
        source_path.write_text(source_text or "", encoding="utf-8")
        (stage_dir / "validation_report.json").write_text(
            json.dumps(
                report.to_dict(),
                ensure_ascii=False,
                indent=2,
                sort_keys=True,
            )
            + "\n",
            encoding="utf-8",
        )
        manifest = {
            "schema_version": graph["schema_version"],
            "task_id": graph["task"]["task_id"],
            "title": graph["task"]["title"],
            "files": list(CSV_HEADERS),
            "counts": counts,
            "warnings": warnings,
        }
        if review is not None:
            provenance = review.to_dict()
            (stage_dir / "review_provenance.json").write_text(
                json.dumps(
                    provenance, ensure_ascii=False, indent=2, sort_keys=True
                ) + "\n",
                encoding="utf-8",
            )
            manifest["review"] = {
                "review_id": review.review_id,
                "revision": review.revision,
                "provenance_file": "review_provenance.json",
            }
            if review.source_graph is not None:
                (stage_dir / "accepted_graph.json").write_text(
                    json.dumps(review.source_graph, ensure_ascii=False, indent=2,
                               sort_keys=True) + "\n", encoding="utf-8",
                )
                manifest["review"]["accepted_graph_file"] = "accepted_graph.json"
        (stage_dir / "compile_manifest.json").write_text(
            json.dumps(
                manifest,
                ensure_ascii=False,
                indent=2,
                sort_keys=True,
            )
            + "\n",
            encoding="utf-8",
        )

        legacy_report = validate_legacy_route_dataset(stage_dir)
        if not legacy_report.is_valid:
            raise TaskKnowledgeCompilationError(
                "生成的六表未通过现有渲染契约校验",
                report=legacy_report,
            )
        warnings.extend(issue.message for issue in legacy_report.warnings)
        _install_stage(stage_dir, output_path, overwrite=overwrite)
    except Exception:
        if stage_dir.exists():
            shutil.rmtree(stage_dir)
        raise

    file_paths = {
        filename: str(output_path / filename)
        for filename in CSV_HEADERS
    }
    return CompilationResult(
        success=True,
        dataset_dir=str(output_path),
        files=file_paths,
        counts=counts,
        validation=report.to_dict(),
        warnings=warnings,
        task_graph_file=str(output_path / "task_graph.json"),
        source_file=str(output_path / "source.txt"),
        review_provenance_file=(
            str(output_path / "review_provenance.json")
            if review is not None
            else ""
        ),
    )


def validate_legacy_route_dataset(
    dataset_dir: Path | str,
) -> ValidationReport:
    """不依赖 QGIS，检查六表能否满足现有绘图入口的字段和外键契约。"""

    report = ValidationReport()
    root = Path(dataset_dir).expanduser()
    tables: Dict[str, List[Dict[str, str]]] = {}
    for filename, expected_headers in CSV_HEADERS.items():
        path = root / filename
        if not path.is_file():
            report.add(
                "legacy_file_missing",
                filename,
                f"缺少六表文件：{filename}",
            )
            continue
        try:
            headers, rows = _read_csv(path)
        except Exception as exc:
            report.add(
                "legacy_csv_unreadable",
                filename,
                f"CSV 无法读取：{exc}",
            )
            continue
        if headers[: len(expected_headers)] != list(expected_headers):
            report.add(
                "legacy_header_mismatch",
                filename,
                f"表头不匹配，期望 {expected_headers}，实际 {headers}",
            )
        tables[filename] = rows

    if any(filename not in tables for filename in CSV_HEADERS):
        return report

    places: Dict[str, Dict[str, str]] = {}
    for idx, row in enumerate(tables["places.csv"]):
        path = f"places.csv[{idx}]"
        name = str(row.get("name") or "").strip()
        if not name:
            report.add("legacy_place_name_missing", f"{path}.name", "地名为空")
            continue
        if name in places:
            report.add(
                "legacy_duplicate_place_name",
                f"{path}.name",
                f"严格模式下地名不能重复：{name}",
            )
        try:
            float(row.get("x") or "")
            float(row.get("y") or "")
        except ValueError:
            report.add(
                "legacy_invalid_place_coordinate",
                path,
                f"地点坐标无效：{name}",
            )
        places[name] = row

    routes: Dict[str, Dict[str, str]] = {}
    for idx, row in enumerate(tables["routes.csv"]):
        path = f"routes.csv[{idx}]"
        route_id = str(row.get("route_id") or "").strip()
        if not route_id:
            report.add(
                "legacy_route_id_missing",
                f"{path}.route_id",
                "route_id 为空",
            )
            continue
        if route_id in routes:
            report.add(
                "legacy_duplicate_route_id",
                f"{path}.route_id",
                f"route_id 重复：{route_id}",
            )
        if row.get("draw_type") not in {
            "bezier_arrow",
            "branch_bezier",
            "multi_start_bezier",
        }:
            report.add(
                "legacy_invalid_draw_type",
                f"{path}.draw_type",
                f"未知 draw_type：{row.get('draw_type')}",
            )
        if not str(row.get("style_id") or "").strip():
            report.add(
                "legacy_style_id_missing",
                f"{path}.style_id",
                "style_id 为空",
            )
        width = str(row.get("width_scale") or "").strip()
        if width:
            try:
                if float(width) <= 0:
                    raise ValueError
            except ValueError:
                report.add(
                    "legacy_invalid_width_scale",
                    f"{path}.width_scale",
                    "width_scale 必须大于 0",
                )
        routes[route_id] = row

    paths_by_route: Dict[str, List[Dict[str, str]]] = {}
    for idx, row in enumerate(tables["route_paths.csv"]):
        path = f"route_paths.csv[{idx}]"
        route_id = str(row.get("route_id") or "").strip()
        if route_id not in routes:
            report.add(
                "legacy_dangling_route_id",
                f"{path}.route_id",
                f"route_id 不存在：{route_id}",
            )
        place_name = str(row.get("place_name") or "").strip()
        if place_name not in places:
            report.add(
                "legacy_dangling_place_name",
                f"{path}.place_name",
                f"地点不存在：{place_name}",
            )
        attach_place = str(row.get("attach_place") or "").strip()
        if attach_place and attach_place not in places:
            report.add(
                "legacy_dangling_attach_place",
                f"{path}.attach_place",
                f"连接地点不存在：{attach_place}",
            )
        try:
            int(float(row.get("seq") or ""))
        except ValueError:
            report.add(
                "legacy_invalid_seq",
                f"{path}.seq",
                "seq 必须可转为整数",
            )
        paths_by_route.setdefault(route_id, []).append(row)

    for route_id, route in routes.items():
        rows = paths_by_route.get(route_id, [])
        main_rows = [row for row in rows if row.get("path_type") == "main"]
        if len(main_rows) < 2:
            report.add(
                "legacy_main_path_too_short",
                f"routes.{route_id}",
                "每条启用路线的 main 至少需要两个点",
            )
        main_names = {str(row.get("place_name")) for row in main_rows}
        draw_type = route.get("draw_type")
        if draw_type == "branch_bezier":
            branch_rows = [
                row for row in rows if row.get("path_type") == "branch"
            ]
            if not branch_rows:
                report.add(
                    "legacy_branch_missing",
                    f"routes.{route_id}",
                    "branch_bezier 缺少 branch 行",
                )
            elif str(branch_rows[0].get("attach_place")) not in main_names:
                report.add(
                    "legacy_attach_not_on_main",
                    f"routes.{route_id}",
                    "branch 连接点不在 main 上",
                )
        if draw_type == "multi_start_bezier":
            start_rows = [
                row for row in rows if row.get("path_type") == "start"
            ]
            if not start_rows:
                report.add(
                    "legacy_start_missing",
                    f"routes.{route_id}",
                    "multi_start_bezier 缺少 start 行",
                )
            for start_row in start_rows:
                if str(start_row.get("attach_place")) not in main_names:
                    report.add(
                        "legacy_attach_not_on_main",
                        f"routes.{route_id}",
                        "start 连接点不在 main 上",
                    )
                    break

    symbol_types: Dict[str, Dict[str, str]] = {}
    for idx, row in enumerate(tables["symbol_mapping.csv"]):
        path = f"symbol_mapping.csv[{idx}]"
        symbol_type = str(row.get("symbol_type") or "").strip()
        if not symbol_type:
            report.add(
                "legacy_symbol_type_missing",
                f"{path}.symbol_type",
                "symbol_type 为空",
            )
            continue
        symbol_types[symbol_type] = row
        svg_path = str(row.get("svg_path") or "").strip()
        resolved = Path(svg_path)
        if svg_path and not resolved.is_absolute():
            resolved = root / resolved
        if not svg_path or not resolved.is_file():
            report.add(
                "legacy_symbol_asset_missing",
                f"{path}.svg_path",
                f"符号资产不存在：{resolved}",
            )

    for idx, row in enumerate(tables["points.csv"]):
        path = f"points.csv[{idx}]"
        name = str(row.get("name") or "").strip()
        if name not in places:
            report.add(
                "legacy_point_place_missing",
                f"{path}.name",
                f"点地点不存在：{name}",
            )
        else:
            try:
                if abs(float(row["x"]) - float(places[name]["x"])) > 1e-9 or abs(
                    float(row["y"]) - float(places[name]["y"])
                ) > 1e-9:
                    report.add(
                        "legacy_point_coordinate_mismatch",
                        path,
                        f"点坐标与 places 不一致：{name}",
                    )
            except (KeyError, ValueError):
                report.add(
                    "legacy_invalid_point_coordinate",
                    path,
                    f"点坐标无效：{name}",
                )
        if row.get("symbol_type") not in symbol_types:
            report.add(
                "legacy_point_symbol_missing",
                f"{path}.symbol_type",
                f"符号映射不存在：{row.get('symbol_type')}",
            )

    for idx, row in enumerate(tables["callouts.csv"]):
        path = f"callouts.csv[{idx}]"
        name = str(row.get("place_name") or "").strip()
        if name and name not in places:
            report.add(
                "legacy_callout_place_missing",
                f"{path}.place_name",
                f"注记地点不存在：{name}",
            )
        elif name:
            try:
                if abs(float(row["x"]) - float(places[name]["x"])) > 1e-9 or abs(
                    float(row["y"]) - float(places[name]["y"])
                ) > 1e-9:
                    report.add(
                        "legacy_callout_coordinate_mismatch",
                        path,
                        f"注记坐标与 places 不一致：{name}",
                    )
            except (KeyError, ValueError):
                report.add(
                    "legacy_invalid_callout_coordinate",
                    path,
                    f"注记坐标无效：{name}",
                )
        enabled = str(row.get("image_enabled") or "").strip().lower() in {
            "1",
            "true",
            "yes",
            "y",
            "on",
        }
        if enabled:
            image_path = Path(str(row.get("image_path") or ""))
            if not image_path.is_absolute():
                image_path = root / image_path
            if not image_path.is_file():
                report.add(
                    "legacy_callout_image_missing",
                    f"{path}.image_path",
                    f"注记图片不存在：{image_path}",
                    severity="warning",
                )

    return report
