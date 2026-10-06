import copy
from pathlib import Path
from typing import Any, Dict, Optional

import yaml

from ..utils.logger import get_logger

logger = get_logger(__name__)


class ConfigProxy:
    def __init__(self, config_dir: Path):
        self.config_dir = config_dir
        self.config_file = config_dir / "plugin_config.yaml"
        self.plugin_dir = config_dir.parent  # 插件根目录（autocarto/）
        self._config: Dict[str, Any] = {}
        self._load_config()

    def _load_config(self):
        raw_config: Dict[str, Any] = {}

        if self.config_file.exists():
            try:
                with open(self.config_file, "r", encoding="utf-8") as f:
                    raw_config = yaml.safe_load(f) or {}
            except Exception as e:
                logger.warning(f"读取默认配置文件失败: {e}")
                raw_config = {}

        if not raw_config:
            raw_config = self._get_default_config()

        self._config = self._normalize_config(raw_config)

    def _get_default_config(self) -> Dict[str, Any]:
        return {
            "backend": {
                "reconnect_max_sec": 8,
            },
            "log": {
                "level": "INFO",
                "dir": "data/logs",
                "max_size": 10485760,
                "max_backups": 5,
                "max_age": 30,
                "max_total_size": 104857600,
                "session_max_size": 5242880,
                "session_max_backups": 2,
            },
            "data_dir": "data",
        }

    def _normalize_config(self, config: Dict[str, Any]) -> Dict[str, Any]:
        if not isinstance(config, dict):
            config = {}

        default_config = self._get_default_config()
        normalized = copy.deepcopy(default_config)
        self._merge_config(normalized, config)

        normalized.pop("middleware", None)

        ports = normalized.get("ports")
        if isinstance(ports, dict):
            ports.pop("agent_system", None)
            ports.pop("qgis_tools", None)

        backend = normalized.get("backend")
        if isinstance(backend, dict):
            backend.pop("ws_url", None)
            backend.pop("api_base_url", None)
            backend.pop("middleware_ws_url", None)

        return normalized

    def get(self, key: str, default: Any = None) -> Any:
        keys = key.split(".")
        value: Any = self._config
        for k in keys:
            if isinstance(value, dict):
                value = value.get(k)
            else:
                return default
        return value if value is not None else default

    def resolve_path(self, key: str) -> Optional[Path]:
        value = self.get(key)
        if not value or not isinstance(value, str):
            return None

        path = Path(value)

        if value.startswith("~"):
            return Path(value).expanduser()

        if path.is_absolute():
            return path

        return (self.plugin_dir / path).resolve()

    def _merge_config(self, target: Dict[str, Any], source: Dict[str, Any]):
        for key, value in source.items():
            if (
                key in target
                and isinstance(target[key], dict)
                and isinstance(value, dict)
            ):
                self._merge_config(target[key], value)
            else:
                target[key] = value
