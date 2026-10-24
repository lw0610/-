"""从已解析资源句柄构造绘图配置；不查找资源或解析资源对照表。"""
import hashlib
import json
from collections import Counter
from pathlib import Path

from autocarto.tools.system._shared.resource_handle import require_handle, handle_entries
from autocarto.tools.system._shared.tool_assets import tool_asset_path


def renderer_defaults():
    return json.loads(tool_asset_path("route-cartography", "renderer-defaults.json").read_text(encoding="utf-8"))


def load_route_styles(handle):
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


def load_symbol_styles(handle):
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
