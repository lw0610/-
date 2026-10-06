"""AutoCarto 路径解析辅助。"""

import os
from pathlib import Path
from typing import Optional

WINDOWS_DEFAULT_AUTOCARTO_HOME = Path(r"F:\AutoCarto")


def get_plugin_root() -> Path:
    return Path(__file__).resolve().parents[1]


def get_autocarto_home() -> Path:
    env_home = (os.environ.get("AUTOCARTO_HOME") or "").strip()
    if env_home:
        return Path(env_home).expanduser()
    if os.name == "nt":
        return WINDOWS_DEFAULT_AUTOCARTO_HOME
    return get_plugin_root()


def get_runtime_home_env() -> Optional[Path]:
    env_home = (os.environ.get("AUTOCARTO_HOME") or "").strip()
    if not env_home:
        return None
    return Path(env_home).expanduser()


def get_plugin_data_dir() -> Path:
    return get_autocarto_home() / "data"


def get_user_config_path() -> Path:
    return get_autocarto_home() / "config" / "config.yaml"
