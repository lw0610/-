"""任务知识图谱的受控词表和确定性视觉预设。"""

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


SYMBOL_PRESETS: Dict[str, Dict[str, Any]] = {
    "waypoint": {
        "svg_path": "symbols/waypoint.svg",
        "size": 2.8,
        "priority": 10,
        "anchor_x": 0,
        "anchor_y": 0,
    },
    "start_flag": {
        "svg_path": "symbols/start_flag.svg",
        "size": 6.5,
        "priority": 100,
        "anchor_x": 0,
        "anchor_y": 0,
    },
    "battle": {
        "svg_path": "symbols/battle.svg",
        "size": 5.5,
        "priority": 90,
        "anchor_x": 0,
        "anchor_y": 0,
    },
    "meeting": {
        "svg_path": "symbols/meeting.svg",
        "size": 5.2,
        "priority": 80,
        "anchor_x": 0,
        "anchor_y": 0,
    },
    "rendezvous": {
        "svg_path": "symbols/rendezvous.svg",
        "size": 5.2,
        "priority": 100,
        "anchor_x": 0,
        "anchor_y": 0,
    },
}


EVENT_SYMBOLS = {
    "departure": "start_flag",
    "battle": "battle",
    "meeting": "meeting",
    "rendezvous": "rendezvous",
}


ROUTE_STYLE_PRESETS: Dict[str, Dict[str, Any]] = {
    "friendly_main_red": {
        "topology": "standard",
        "side": "friendly",
        "route_type": "main",
        "draw_type": "bezier_arrow",
        "style_id": "bezier_red_gradient",
    },
    "enemy_main_blue": {
        "topology": "standard",
        "side": "enemy",
        "route_type": "enemy",
        "draw_type": "bezier_arrow",
        "style_id": "bezier_blue_gradient",
    },
    "friendly_branch_red": {
        "topology": "branch",
        "side": "friendly",
        "route_type": "branch",
        "draw_type": "branch_bezier",
        "style_id": "branch_bezier_red_gradient",
    },
    "enemy_branch_blue": {
        "topology": "branch",
        "side": "enemy",
        "route_type": "branch",
        "draw_type": "branch_bezier",
        "style_id": "branch_bezier_blue_gradient",
    },
    "friendly_multi_start_red": {
        "topology": "multi_start",
        "side": "friendly",
        "route_type": "main",
        "draw_type": "multi_start_bezier",
        "style_id": "multi_start_bezier_red_gradient",
    },
    "enemy_multi_start_blue": {
        "topology": "multi_start",
        "side": "enemy",
        "route_type": "enemy",
        "draw_type": "multi_start_bezier",
        "style_id": "multi_start_bezier_blue_gradient",
    },
}


DEFAULT_ROUTE_STYLE = {
    ("standard", "friendly"): "friendly_main_red",
    ("standard", "neutral"): "friendly_main_red",
    ("standard", "enemy"): "enemy_main_blue",
    ("branch", "friendly"): "friendly_branch_red",
    ("branch", "neutral"): "friendly_branch_red",
    ("branch", "enemy"): "enemy_branch_blue",
    ("multi_start", "friendly"): "friendly_multi_start_red",
    ("multi_start", "neutral"): "friendly_multi_start_red",
    ("multi_start", "enemy"): "enemy_multi_start_blue",
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
