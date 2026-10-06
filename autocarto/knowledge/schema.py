"""任务知识图谱 schema 的加载和无副作用规范化。"""

from __future__ import annotations

import copy
import json
from pathlib import Path
from typing import Any, Dict, Mapping


def load_task_knowledge_schema() -> Dict[str, Any]:
    schema_path = (
        Path(__file__).resolve().parent
        / "schemas"
        / "task_knowledge_graph_v1.schema.json"
    )
    return json.loads(schema_path.read_text(encoding="utf-8"))


def normalize_task_knowledge(raw: Mapping[str, Any]) -> Dict[str, Any]:
    """复制并补齐非事实性的安全默认值，不替模型编造实体或关系。"""

    graph = copy.deepcopy(dict(raw))
    task = graph.setdefault("task", {})
    if isinstance(task, dict):
        task.setdefault("map_type", "marching_route")
        task.setdefault("input_crs", "EPSG:4326")

    for key in (
        "actors",
        "spatial_entities",
        "events",
        "routes",
        "point_presentations",
        "callouts",
        "evidence",
    ):
        graph.setdefault(key, [])
    graph.setdefault("output_constraints", {})

    for entity in graph.get("spatial_entities", []):
        if not isinstance(entity, dict):
            continue
        if "longitude" in entity and "lon" not in entity:
            entity["lon"] = entity.pop("longitude")
        if "latitude" in entity and "lat" not in entity:
            entity["lat"] = entity.pop("latitude")
        kind = str(entity.get("entity_kind") or "place")
        entity.setdefault("entity_kind", kind)
        entity.setdefault(
            "factual",
            False if kind == "cartographic_control" else True,
        )
        entity.setdefault(
            "map_role",
            "control" if kind == "cartographic_control" else "route_node",
        )
        entity.setdefault("evidence_refs", [])

    for actor in graph.get("actors", []):
        if isinstance(actor, dict):
            actor.setdefault("aliases", [])
            actor.setdefault("evidence_refs", [])

    for event in graph.get("events", []):
        if not isinstance(event, dict):
            continue
        if "place_id" in event and "location_ref" not in event:
            event["location_ref"] = event.pop("place_id")
        event.setdefault("actor_refs", [])
        event.setdefault("evidence_refs", [])

    for route in graph.get("routes", []):
        if not isinstance(route, dict):
            continue
        route.setdefault("topology", "standard")
        route.setdefault("side", "friendly")
        route.setdefault("enabled", True)
        route.setdefault("paths", [])
        route.setdefault("evidence_refs", [])
        for path in route.get("paths", []):
            if not isinstance(path, dict):
                continue
            role = str(path.get("role") or "main")
            path.setdefault("role", role)
            if role == "branch":
                path.setdefault("direction", "away_from_attach")
            elif role == "incoming":
                path.setdefault("direction", "toward_attach")
            else:
                path.setdefault("direction", "forward")
            path.setdefault("nodes", [])

    for presentation in graph.get("point_presentations", []):
        if isinstance(presentation, dict):
            presentation.setdefault("visible", True)
            presentation.setdefault("priority", 50)

    for callout in graph.get("callouts", []):
        if not isinstance(callout, dict):
            continue
        callout.setdefault("template", "text_only")
        callout.setdefault("image_enabled", False)
        callout.setdefault("image_path", "")
        callout.setdefault("priority", 0)
        callout.setdefault("preferred_side", "right_top")
        callout.setdefault("width_mm", 42)
        callout.setdefault("leader_style", "double")

    constraints = graph.get("output_constraints")
    if isinstance(constraints, dict):
        constraints.setdefault("self_intersection_policy", "allow_with_overpass")

    return graph
