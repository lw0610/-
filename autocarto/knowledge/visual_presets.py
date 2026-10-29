"""默认使用内置制图规则；仅在明确选用外部表时读取对应配置。"""
import hashlib
import json
from collections import Counter
from pathlib import Path

from autocarto.tools.system._shared.resource_handle import require_handle, handle_entries
from autocarto.tools.system._shared.tool_assets import tool_asset_path
from .vocabulary import (
    DEFAULT_ROUTE_STYLE,
    EVENT_SYMBOLS,
    ROUTE_STYLE_PRESETS,
    SYMBOL_PRESETS,
)


def renderer_defaults():
    return json.loads(tool_asset_path("route-cartography", "renderer-defaults.json").read_text(encoding="utf-8"))


def load_route_styles(handle=None):
    if handle is None or (isinstance(handle, str) and not handle.strip()):
        presets = {key: dict(value) for key, value in ROUTE_STYLE_PRESETS.items()}
        return presets, dict(DEFAULT_ROUTE_STYLE)
    rows = handle_entries(require_handle(handle, "route_style"))
    presets, defaults = {}, {}
    for row in rows:
        key = row["preset_ref"]
        if key in presets:
            raise ValueError("路线样式键重复: " + key)
        presets[key] = dict(row)
        for side in row["default_sides"]:
            pair = (row["topology"], side)
            if pair in defaults:
                raise ValueError("路线拓扑与阵营重复: " + str(pair))
            defaults[pair] = key
    return presets, defaults


def load_symbol_styles(handle=None):
    if handle is None or (isinstance(handle, str) and not handle.strip()):
        source_dir = tool_asset_path("route-cartography", "resources/symbols/军标")
        labels = {
            "waypoint": "途经点", "start_flag": "起点", "battle": "战斗",
            "meeting": "会议", "rendezvous": "会师",
        }
        rows = [
            {
                **preset, "symbol_type": kind,
                "path": str(source_dir / Path(preset["svg_path"]).name),
                "description": labels[kind],
                "event_types": [event for event, symbol in EVENT_SYMBOLS.items() if symbol == kind],
            }
            for kind, preset in SYMBOL_PRESETS.items()
        ]
    else:
        rows = handle_entries(require_handle(handle, "symbols"))
    filenames = Counter(Path(row["path"]).name.casefold() for row in rows)
    presets, events, sources = {}, {}, {}
    for row in rows:
        kind = row["symbol_type"]
        source = Path(row["path"])
        filename = source.name
        if filenames[filename.casefold()] > 1:
            # 同资源内不同子目录的同名图片不能在运行数据集中互相覆盖。
            filename = hashlib.sha256(kind.encode("utf-8")).hexdigest()[:16] + source.suffix
        preset = {key: row[key] for key in ("size", "priority", "anchor_x", "anchor_y")}
        preset.update(svg_path="symbols/" + filename, description=row.get("description", ""))
        presets[kind], sources[kind] = preset, source
        for event in row["event_types"]:
            if event in events:
                raise ValueError("符号事件类型重复: " + event)
            events[event] = kind
    return presets, events, sources, rows
