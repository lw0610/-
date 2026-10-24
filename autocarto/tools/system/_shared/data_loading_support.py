"""不依赖 QGIS 的数据源身份与工具参数校验。"""

import ntpath
import os

from autocarto.core.paths import resolve_project_path


class DataLoadingError(ValueError):
    def __init__(self, code, message, details=None):
        super().__init__(message)
        self.code = code
        self.details = details or {}


def boolean_parameter(value, default=True):
    if value is None:
        return default
    if isinstance(value, bool):
        return value
    if isinstance(value, (str, int)):
        text = str(value).strip().lower()
        if text in {"true", "1", "yes", "是"}:
            return True
        if text in {"false", "0", "no", "否"}:
            return False
    raise DataLoadingError("invalid_arguments", "布尔参数须为 true/false、是/否或 1/0")


def split_data_source(raw):
    if not isinstance(raw, str) or not raw.strip() or "\x00" in raw:
        raise DataLoadingError("invalid_arguments", "文件路径须为非空字符串，且不含空字符")
    parts = raw.strip().split("|")
    if not parts[0].strip():
        raise DataLoadingError("invalid_arguments", "数据源缺少文件路径")
    return parts[0].strip(), parts[1:]


def normalized_data_source(raw, windows=None):
    """只保留文件身份和子图层名，忽略编码等 provider 选项。"""
    filename, options = split_data_source(raw)
    windows = os.name == "nt" if windows is None else windows
    if windows:
        path = ntpath.normcase(ntpath.abspath(os.path.expanduser(filename)))
    else:
        path = str(resolve_project_path(filename))
    names = [option.partition("=")[2].strip() for option in options
             if option.partition("=")[0].strip().lower() == "layername"]
    if len(names) > 1:
        raise DataLoadingError("invalid_arguments", "数据源只能指定一个 layername")
    layer_name = names[0] if names else ""
    return path, layer_name.casefold() if windows else layer_name
