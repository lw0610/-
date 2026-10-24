"""校验并消费 Go 资源服务交付的句柄；不查找资源或读取资源对照表。"""

from pathlib import Path


class ResourceHandleError(ValueError):
    def __init__(self, code: str, message: str, details: dict | None = None):
        super().__init__(message)
        self.code = code
        self.details = details or {}


def require_handle(value, expected_type: str, *, formats: tuple[str, ...] = ()) -> dict:
    if not isinstance(value, dict):
        raise ResourceHandleError("resource_handle_required", "缺少已解析的资源句柄，请通过资源服务选择资源")
    if value.get("api_version") != "resource-handle/v1":
        raise ResourceHandleError("resource_handle_invalid", "资源句柄版本无效")
    if value.get("type") != expected_type:
        raise ResourceHandleError("resource_type_mismatch", f"资源类型不匹配，需要 {expected_type}")
    if formats and value.get("format") not in formats:
        raise ResourceHandleError("resource_format_mismatch", "资源格式不匹配，需要 " + "、".join(formats))
    if any(not isinstance(value.get(key), str) or not value[key].strip() for key in ("id", "name", "version")):
        raise ResourceHandleError("resource_handle_invalid", "资源句柄缺少编号、名称或版本")
    if value.get("truncated"):
        raise ResourceHandleError("resource_handle_incomplete", "工具需要完整资源句柄，不能使用分页截取的条目")
    handle_entries(value)
    return value


def handle_entries(handle: dict) -> list[dict]:
    entries = handle.get("entries")
    if not isinstance(entries, list) or len(entries) > 10_000:
        raise ResourceHandleError("resource_handle_invalid", "资源句柄条目无效或超过 10000 项")
    for entry in entries:
        if (not isinstance(entry, dict) or not isinstance(entry.get("name"), str)
                or not entry["name"].strip() or not isinstance(entry.get("kind"), str)
                or not isinstance(entry.get("path"), str) or not isinstance(entry.get("exists"), bool)):
            raise ResourceHandleError("resource_handle_invalid", "资源句柄条目缺少规范化字段")
        if entry["path"]:
            # 数据源可带 provider 选项；选项仍由数据加载工具处理。
            _absolute_path(entry["path"].split("|", 1)[0])
    total = handle.get("total_entries", len(entries))
    if not isinstance(total, int) or isinstance(total, bool) or total != len(entries):
        raise ResourceHandleError("resource_handle_incomplete", "资源句柄条目数量不完整")
    return entries


def _absolute_path(value) -> Path:
    if not isinstance(value, str) or "\x00" in value or not Path(value).is_absolute():
        raise ResourceHandleError("resource_path_invalid", "资源句柄必须包含已解析的绝对路径")
    return Path(value)


def handle_entry_path(handle: dict) -> Path:
    return _absolute_path(handle.get("entry"))


def handle_summary(handle: dict) -> dict:
    return {key: handle[key] for key in ("id", "name", "version")}


def legend_extra_spec(extra, symbols=None):
    """选择已解析符号条目或显式图片，返回图例工具需要的路径与毫米尺寸。"""
    import math
    from autocarto.core.paths import resolve_project_path
    if not isinstance(extra, dict) or not isinstance(extra.get("label"), str) or not extra["label"].strip():
        raise ValueError("额外图例条目必须提供文字名称")
    default_size = 7
    if extra.get("symbol_type"):
        if extra.get("image_path"):
            raise ValueError("额外图例条目只能选择 symbol_type 或 image_path 之一")
        handle = require_handle(symbols, "symbols")
        matches = [row for row in handle_entries(handle) if row["symbol_type"] == extra["symbol_type"]]
        if len(matches) != 1:
            raise ValueError("图例符号类型不存在或不唯一: " + str(extra["symbol_type"]))
        path, default_size = Path(matches[0]["path"]), matches[0]["size"]
    else:
        if not isinstance(extra.get("image_path"), str) or not extra["image_path"].strip():
            raise ValueError("额外图例条目必须提供 symbol_type 或 image_path")
        path = resolve_project_path(extra["image_path"])
    size = float(extra.get("size_mm", default_size))
    if isinstance(extra.get("size_mm"), bool) or not math.isfinite(size) or size <= 0:
        raise ValueError("额外图例符号大小必须是有限正数（毫米）")
    if not path.is_file() or path.suffix.lower() not in (".svg", ".png", ".jpg", ".jpeg"):
        raise ValueError("额外图例图片不存在或格式不支持")
    return path, size, extra["label"]
