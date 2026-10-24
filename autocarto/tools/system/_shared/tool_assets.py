"""工具私有资产：相对工具实现目录定位，不参与公共资源解析。"""

from pathlib import Path
import re


def tool_asset_path(domain: str, *parts: str) -> Path:
    if not isinstance(domain, str) or not re.fullmatch(r"[a-z][a-z0-9-]*", domain):
        raise ValueError("工具域须为有效目录名")
    root = (Path(__file__).resolve().parents[1] / domain / "assets").resolve()
    path = root
    for part in parts:
        if not isinstance(part, str) or not part or "\\" in part or "\x00" in part:
            raise ValueError("工具资产路径无效")
        item = Path(part)
        if item.is_absolute() or ".." in item.parts:
            raise ValueError("工具资产路径须位于工具目录内")
        path /= item
    path = path.resolve()
    if not path.is_relative_to(root):
        raise ValueError("工具资产路径须位于工具目录内")
    return path
