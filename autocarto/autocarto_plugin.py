"""AutoCarto AI 极简 QGIS 插件（反向连接模式）"""

import os
from qgis.PyQt.QtCore import Qt, QTimer
from qgis.PyQt.QtWidgets import QAction
from qgis.PyQt.QtGui import QIcon, QPixmap, QPainter, QColor
from qgis.core import QgsProject

from .core.plugin_manager import PluginManager
from .utils.logger import get_logger

logger = get_logger(__name__)

PROJECT_OPEN_SYNC_DELAY_MS = 250
PROJECT_CLOSE_CONFIRM_DELAY_MS = 1500


class AutoCartoPlugin:
    """AutoCarto AI 极简 QGIS 插件
    职责：
    1. 启动插件内工具执行引擎（不监听端口）
    2. 插件主动通过 WebSocket 反向连接 Go 后端
    3. 监听 QGIS 工程事件并通过 WebSocket 上报
    """

    def __init__(self, iface):
        self.iface = iface
        self.plugin_dir = os.path.dirname(__file__)
        self.plugin_manager = PluginManager(self.plugin_dir)
        self.actions = []
        self.menu = "AutoCarto AI"
        self.toolbar = None
        self.config_manager = None
        self._instance_id = self._build_instance_id()
        self._tool_engine = None
        self._ws_client = None
        self._status_dock = None
        self._status_action = None
        self._last_reported_project_path = ""
        self._project_state_timer = None
        self._project_close_timer = None

    def initGui(self):
        """初始化插件"""
        # 加载本地配置
        from pathlib import Path
        from .core.config_manager import ConfigProxy

        config_dir = Path(self.plugin_dir) / "config"
        config_dir.mkdir(exist_ok=True)
        self.config_manager = ConfigProxy(config_dir)

        # 初始化日志
        from .utils.logger import init_log_dir, configure_log_policy

        configure_log_policy(
            max_size=self.config_manager.get("log.max_size"),
            max_backups=self.config_manager.get("log.max_backups"),
            max_age=self.config_manager.get("log.max_age"),
            max_total_size=self.config_manager.get("log.max_total_size"),
            session_max_size=self.config_manager.get("log.session_max_size"),
            session_max_backups=self.config_manager.get("log.session_max_backups"),
        )
        configured_log_dir = self.config_manager.resolve_path("log.dir")
        if configured_log_dir:
            init_log_dir(str(configured_log_dir))

        # 创建工具栏（简单状态指示）
        self.toolbar = self.iface.addToolBar("AutoCarto AI")
        self.toolbar.setObjectName("AutoCartoToolbar")
        icon_path = os.path.join(self.plugin_dir, "resources", "logo.svg")
        action = QAction(
            QIcon(icon_path) if os.path.exists(icon_path) else QIcon(),
            "AutoCarto AI",
            self.iface.mainWindow(),
        )

        # action.triggered in PyQt5 emits a boolean (checked state)
        # we wrap it to match _update_status_tooltip's signature
        action.triggered.connect(lambda _: self._update_status_tooltip())
        self.toolbar.addAction(action)
        self.actions.append(action)
        self._status_action = action
        self.iface.addPluginToMenu(self.menu, action)

        # 初始化工具执行引擎（不再监听 HTTP 端口）
        from .core.tool_engine import create_engine

        self._tool_engine = create_engine()

        # 初始化反向连接客户端
        from .core.instance_ws_client import InstanceWSClient

        self._ws_client = InstanceWSClient(
            config_manager=self.config_manager,
            tool_engine=self._tool_engine,
            instance_id=self._instance_id,
            qgis_version=self._get_qgis_version(),
            session_manager=self.plugin_manager.get_session_manager(),
        )

        # Initial tooltip setup
        self._update_status_tooltip()

        self._last_reported_project_path = QgsProject.instance().fileName() or ""
        self._project_state_timer = QTimer()
        self._project_state_timer.setSingleShot(True)
        self._project_state_timer.timeout.connect(self._sync_project_state)
        self._project_close_timer = QTimer()
        self._project_close_timer.setSingleShot(True)
        self._project_close_timer.timeout.connect(self._confirm_project_closed)

        # 连接 QGIS 工程信号
        QgsProject.instance().readProject.connect(self._on_project_opened)
        QgsProject.instance().cleared.connect(self._on_project_closed)
        if hasattr(QgsProject.instance(), "projectSaved"):
            QgsProject.instance().projectSaved.connect(self._on_project_saved)

        # 连接状态更新信号
        if hasattr(self._ws_client, "connection_changed"):
            self._ws_client.connection_changed.connect(self._on_ws_connection_changed)

        # 启动反向连接
        self._ws_client.start()

        logger.info("AutoCarto AI 极简插件已加载")

    def unload(self):
        """卸载插件"""
        # 断开 QGIS 信号
        try:
            QgsProject.instance().readProject.disconnect(self._on_project_opened)
        except Exception:
            pass
        try:
            QgsProject.instance().cleared.disconnect(self._on_project_closed)
        except Exception:
            pass
        try:
            if hasattr(QgsProject.instance(), "projectSaved"):
                QgsProject.instance().projectSaved.disconnect(self._on_project_saved)
        except Exception:
            pass

        # 停止反向连接
        if self._ws_client:
            self._ws_client.stop()
            self._ws_client = None

        if self._project_state_timer:
            self._project_state_timer.stop()
            self._project_state_timer = None
        if self._project_close_timer:
            self._project_close_timer.stop()
            self._project_close_timer = None

        self._status_action = None
        try:
            self.plugin_manager.get_session_manager().clear()
        except Exception:
            pass

        # 清理 UI
        for action in self.actions:
            self.iface.removePluginMenu(self.menu, action)
            self.iface.removeToolBarIcon(action)
        if self.toolbar:
            del self.toolbar

        logger.info("AutoCarto AI 极简插件已卸载")

    def _build_instance_id(self) -> str:
        """构建实例 ID：machine_id + pid + start_time"""
        import socket
        import time
        import uuid

        machine_id = f"{socket.gethostname()}_{uuid.getnode():x}"
        return f"qgis_{machine_id}_{os.getpid()}_{int(time.time())}"

    def _get_qgis_version(self) -> str:
        """获取 QGIS 版本"""
        try:
            from qgis.core import Qgis

            return Qgis.version()
        except Exception:
            return "unknown"

    def _on_project_opened(self):
        self._schedule_project_state_sync(PROJECT_OPEN_SYNC_DELAY_MS)

    def _on_project_closed(self):
        if self._project_close_timer:
            self._project_close_timer.start(PROJECT_CLOSE_CONFIRM_DELAY_MS)

    def _on_project_saved(self):
        if not self._ws_client:
            return
        if self._project_close_timer:
            self._project_close_timer.stop()
        project_path = QgsProject.instance().fileName() or ""
        if not project_path:
            return
        self._last_reported_project_path = project_path
        self._update_status_tooltip()
        self._ws_client.report_project_event("project_saved", project_path=project_path)

    def _schedule_project_state_sync(self, delay_ms: int):
        if self._project_close_timer:
            self._project_close_timer.stop()
        if not self._project_state_timer:
            return
        self._project_state_timer.start(max(0, int(delay_ms)))

    def _confirm_project_closed(self):
        self._sync_project_state()

    def _update_status_tooltip(self, _=False):
        if not self._status_action or not self._ws_client:
            return

        is_connected = self._ws_client.is_connected()
        is_reconnecting = self._ws_client.is_reconnecting()

        status_text = (
            "已连接"
            if is_connected
            else ("正在重连..." if is_reconnecting else "未连接")
        )

        tooltip = (
            f"AutoCarto AI 状态: {status_text}\n"
            f"实例 ID: {self._instance_id}\n"
            f"当前工程: {self._last_reported_project_path}"
        )
        self._status_action.setToolTip(tooltip)

        color_name = (
            "green" if is_connected else ("yellow" if is_reconnecting else "red")
        )

        icon = self._create_status_icon(color_name)
        if icon:
            self._status_action.setIcon(icon)

    def _create_status_icon(self, color_name: str) -> QIcon:
        icon_path = os.path.join(self.plugin_dir, "resources", "logo.svg")

        if not os.path.exists(icon_path):
            pixmap = QPixmap(16, 16)
            pixmap.fill(Qt.transparent)
            painter = QPainter(pixmap)
            painter.setBrush(QColor(color_name))
            painter.setPen(Qt.NoPen)
            painter.drawEllipse(2, 2, 12, 12)
            painter.end()
            return QIcon(pixmap)

        pixmap = QIcon(icon_path).pixmap(24, 24)
        painter = QPainter(pixmap)

        painter.setBrush(QColor(color_name))
        painter.setPen(QColor("white"))
        painter.drawEllipse(14, 14, 10, 10)
        painter.end()

        return QIcon(pixmap)

    def _on_ws_connection_changed(self, connected: bool):
        self._update_status_tooltip()

    def _sync_project_state(self):
        if not self._ws_client:
            return

        project_path = (QgsProject.instance().fileName() or "").strip()
        last_reported = (self._last_reported_project_path or "").strip()
        if project_path == last_reported:
            return

        if project_path:
            self._ws_client.report_project_event(
                "project_opened", project_path=project_path
            )
        elif last_reported:
            self._ws_client.report_project_event("project_closed", project_path="")

        self._last_reported_project_path = project_path
        self._update_status_tooltip()
