"""任务知识图谱的受控语义词表与运行数据表字段。"""

from __future__ import annotations

from typing import Any, Dict


SCHEMA_VERSION = "1.0"
MAP_TYPES = {"marching_route"}
ENTITY_KINDS = {"place", "region", "cartographic_control"}
MAP_ROLES = {"route_node", "event_anchor", "background", "control"}
COORDINATE_SOURCES = {
    "source_text",
    "llm_estimated",
    "manual",
    "gazetteer",
    "derived_control",
}
EVENT_TYPES = {
    "departure",
    "arrival",
    "passage",
    "battle",
    "meeting",
    "rendezvous",
    "base",
    "turn",
    "other",
}
TOPOLOGIES = {"standard", "branch", "multi_start"}
SIDES = {"friendly", "enemy", "neutral"}
PATH_ROLES = {"main", "branch", "incoming"}
DIRECTIONS = {"forward", "away_from_attach", "toward_attach"}
NODE_REF_TYPES = {"spatial_entity", "event"}
ANCHOR_REF_TYPES = {"spatial_entity", "event"}
CALLOUT_TEMPLATES = {
    "text_only",
    "image_top_text_bottom",
    "image_left_text_right",
    "image_only_ellipse",
    "ellipse_badge",
}
SELF_INTERSECTION_POLICIES = {"allow", "allow_with_overpass", "avoid"}
SYMBOL_TYPES = {
    "start_flag",
    "battle",
    "meeting",
    "rendezvous",
}



CSV_HEADERS = {
    "places.csv": [
        "place_id",
        "name",
        "x",
        "y",
        "symbol_type",
        "priority",
        "place_type",
        "date",
        "desc",
    ],
    "routes.csv": [
        "route_id",
        "route_name",
        "route_type",
        "draw_type",
        "style_id",
        "width_scale",
        "input_crs",
        "enabled",
        "notes",
    ],
    "route_paths.csv": [
        "route_id",
        "path_id",
        "path_type",
        "attach_place",
        "seq",
        "place_name",
    ],
    "points.csv": ["name", "x", "y", "symbol_type", "priority"],
    "symbol_mapping.csv": [
        "symbol_type",
        "svg_path",
        "size",
        "priority",
        "anchor_x",
        "anchor_y",
    ],
    "callouts.csv": [
        "id",
        "place_name",
        "x",
        "y",
        "title",
        "body",
        "template",
        "image_enabled",
        "image_path",
        "priority",
        "preferred_side",
        "width_mm",
        "leader_style",
    ],
}
