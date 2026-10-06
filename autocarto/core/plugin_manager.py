"""
插件管理器
"""

from pathlib import Path


class PluginManager:
    """插件管理器"""

    def __init__(self, plugin_dir: str):
        self.plugin_dir = Path(plugin_dir)
        self.config_dir = self.plugin_dir / "config"
        self.data_dir = self.plugin_dir / "data"
        self._session_manager = None
        self._ensure_directories()

    def _ensure_directories(self):
        """确保必要的目录存在"""
        directories = [
            self.config_dir,
            self.data_dir,
            self.data_dir / "logs",
            self.data_dir / "runtime",
        ]
        for directory in directories:
            directory.mkdir(parents=True, exist_ok=True)

    def get_data_dir(self) -> Path:
        """获取数据目录"""
        return self.data_dir

    def get_config_dir(self) -> Path:
        """获取配置目录"""
        return self.config_dir

    def get_log_dir(self) -> Path:
        """获取日志目录"""
        return self.data_dir / "logs"

    def get_session_manager(self):
        """获取会话管理器（延迟初始化）。"""
        if self._session_manager is None:
            from .session_manager import SessionManager

            self._session_manager = SessionManager()
        return self._session_manager
