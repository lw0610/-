"""Named route presets, separate from the public style/layout library."""
import csv
import hashlib
import math
from pathlib import Path
from autocarto.core.paths import get_autocarto_home, get_plugin_root


def _parts(value):
    return [x.strip() for x in str(value or "").replace("；", "、").replace(";", "、").split("、") if x.strip()]


def _number(value, default):
    result = float(value) if str(value or "").strip() else default
    if result is None:
        return None
    if not math.isfinite(result):
        raise ValueError("预设中的数值必须有限")
    return result


def resolve_tool_preset(name, kind):
    if not isinstance(name, str) or not name.strip() or any(x in name for x in ("/", "\\", "..", "\x00")):
        raise ValueError("工具预设必须使用名称")
    name = name.strip().strip("「」")
    aliases = {"route_style.mono": "单色", "route_style.red-blue": "红蓝对比"}
    name = aliases.get(name, name)
    if kind == "route_style":
        name = name.removeprefix("路线样式-")
    group = "styles" if kind == "route_style" else "symbols"
    roots = [get_autocarto_home() / "presets" / "route-cartography" / group,
             get_plugin_root() / "tools" / "system" / "route-cartography" / "assets" / "resources" / group]
    found = []
    for root in roots:
        candidates = [root / name] if kind == "symbols" else [root / (name + ".csv"), root / ("路线样式-" + name + ".csv")]
        for path in candidates:
            if path.exists():
                if any(parent.is_symlink() for parent in [path, *path.parents]):
                    raise ValueError("工具预设不能通过符号链接读取")
                if path.resolve() not in found:
                    found.append(path.resolve())
    if len(found) != 1:
        raise ValueError("工具预设不存在或名称不唯一：" + name)
    root = found[0]
    table = root / "对照表.csv" if root.is_dir() else root
    if table.stat().st_size > 8 << 20:
        raise ValueError("工具预设超过读取上限")
    entries = []
    with table.open(encoding="utf-8-sig", newline="") as f:
        for row in csv.DictReader(f):
            if kind == "route_style":
                topology = row.get("拓扑", row.get("topology", ""))
                side = row.get("阵营", row.get("side", ""))
                entry = {"name": topology + "/" + side, "kind": kind, "path": "", "exists": True,
                         "topology": topology, "side": side, "route_type": row.get("路线类型", row.get("route_type", "")),
                         "draw_type": row.get("绘制方式", row.get("draw_type", "")), "style_id": row.get("样式编号", row.get("style_id", "")),
                         "width_scale": _number(row.get("线宽系数", row.get("width_scale")), None), "legend_label": row.get("图例名称", row.get("legend_label", "路线")),
                         "preset_ref": row.get("preset_ref") or topology + "_" + side,
                         "default_sides": _parts(row.get("default_sides")) or [side],
                         "reference_style_id": row.get("reference_style_id", ""),
                         "label_paths": str(row.get("label_paths", "false")).lower() in ("true", "是", "1"),
                         "auto_width_group": str(row.get("auto_width_group", "false")).lower() in ("true", "是", "1")}
            else:
                raw = row.get("文件", row.get("SVG路径", row.get("svg_path", "")))
                path = root / raw
                if Path(raw).is_absolute() or ".." in Path(raw).parts or not path.is_file() or any(parent.is_symlink() for parent in [path, *path.parents]):
                    raise ValueError("符号文件不存在或越出预设目录：" + raw)
                symbol_type = row.get("符号类型", row.get("symbol_type", ""))
                entry = {"name": symbol_type, "kind": "symbol", "path": str(path), "exists": True,
                         "symbol_type": symbol_type, "size": _number(row.get("大小", row.get("size")), 4),
                         "priority": _number(row.get("优先级", row.get("priority")), 0), "anchor_x": _number(row.get("锚点X", row.get("anchor_x")), 0),
                         "anchor_y": _number(row.get("锚点Y", row.get("anchor_y")), 0), "description": row.get("说明", row.get("description", "")),
                         "event_types": _parts(row.get("event_types")), "format": path.suffix.lstrip(".")}
            if not entry["name"] or len(entries) >= 10000:
                raise ValueError("工具预设条目无效或数量过多")
            entries.append(entry)
    return {"api_version": "resource-handle/v1", "id": kind + ".preset-" + hashlib.sha256(name.encode()).hexdigest()[:12],
            "name": name, "type": kind, "version": "1.0.0", "owner": "tool_preset", "root": str(root),
            "entry": str(table), "entries": entries, "total_entries": len(entries), "truncated": False}
