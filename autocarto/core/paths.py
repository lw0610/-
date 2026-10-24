"""Shared project-relative paths for development, plugins and child processes."""

import os
from pathlib import Path
from typing import Optional

DEFAULT_RUNTIME_DIR = "autocarto_data"


def get_plugin_root() -> Path:
    return Path(__file__).resolve().parents[1]


def get_project_root() -> Path:
    explicit = (os.environ.get("AUTOCARTO_PROJECT_ROOT") or "").strip()
    if explicit:
        value = Path(explicit).expanduser()
        if not value.is_absolute():
            raise ValueError("AUTOCARTO_PROJECT_ROOT 必须为绝对路径")
        return value.resolve()
    assets = (os.environ.get("AUTOCARTO_ASSETS_ROOT") or "").strip()
    if assets:
        value = Path(assets).expanduser()
        if value.is_absolute() and (value / "skills" / "system").is_dir():
            return value.resolve().parent
    return get_plugin_root().parent


def resolve_project_path(raw: str) -> Path:
    value = str(raw).strip()
    if not value or "\x00" in value:
        raise ValueError("路径为空或包含非法字符")
    path = Path(value).expanduser()
    if not path.is_absolute():
        path = get_project_root() / path
    return path.resolve()


def prefer_project_relative_path(raw: str) -> str:
    path = resolve_project_path(raw)
    try:
        return path.relative_to(get_project_root()).as_posix() or "."
    except ValueError:
        return str(path)


def get_runtime_home_env() -> Optional[Path]:
    env_home = (os.environ.get("AUTOCARTO_HOME") or "").strip()
    return resolve_project_path(env_home) if env_home else None


def get_autocarto_home() -> Path:
    return get_runtime_home_env() or (get_project_root() / DEFAULT_RUNTIME_DIR)


def get_plugin_data_dir() -> Path:
    return get_autocarto_home() / "data"


def get_user_config_path() -> Path:
    return get_autocarto_home() / "config" / "config.yaml"
