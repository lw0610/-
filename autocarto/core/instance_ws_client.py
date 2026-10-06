from __future__ import annotations

import hashlib
import importlib
import json
import os
import random
import socket
import sys
import threading
import time
import uuid
from typing import Any, Dict, Optional, cast

from .tool_engine.engine import execute_tool
from .tool_engine.runtime import extract_tool_error, safe_json_dumps

from ..utils.logger import get_logger

logger = get_logger(__name__)


_REQUEST_STATE_PENDING = "pending"
_REQUEST_STATE_RUNNING = "running"
_REQUEST_STATE_CANCEL_REQUESTED = "cancel_requested"
_REQUEST_STATE_TIMED_OUT = "timed_out"

_REQUEST_TERMINAL_STATE_CANCELED = "canceled"
_REQUEST_TERMINAL_STATE_FAILED = "failed"
_REQUEST_TERMINAL_STATE_SUCCEEDED = "succeeded"


class _FallbackSignal:
    def connect(self, *args: Any, **kwargs: Any):
        return None

    def emit(self, *args: Any, **kwargs: Any):
        return None


def _fallback_pyqt_signal(*args: Any, **kwargs: Any) -> _FallbackSignal:
    return _FallbackSignal()


class _FallbackTimer:
    def __init__(self):
        self.timeout = _FallbackSignal()

    def setSingleShot(self, value: bool):
        return None

    def setInterval(self, value: int):
        return None

    def start(self, interval: int = 0):
        return None

    def stop(self):
        return None

    def isActive(self) -> bool:
        return False


class _FallbackQUrl(str):
    def __new__(cls, value: str = ""):
        return str.__new__(cls, value)


class _FallbackQAbstractSocket:
    ConnectedState = 1


class _FallbackQt:
    QueuedConnection = 0


class _FallbackQObject:
    def __init__(self, *args: Any, **kwargs: Any):
        return None


class _FallbackProjectInstance:
    def fileName(self) -> str:
        return ""


class _FallbackQgsProject:
    @staticmethod
    def instance() -> _FallbackProjectInstance:
        return _FallbackProjectInstance()


QObjectBase: type = _FallbackQObject


try:
    _qtcore = importlib.import_module("qgis.PyQt.QtCore")
    QObjectBase = cast(type, _qtcore.QObject)
    QTimer = _qtcore.QTimer
    QUrl = _qtcore.QUrl
    QCoreApplication = getattr(_qtcore, "QCoreApplication", None)
    QThread = getattr(_qtcore, "QThread", None)
    Qt = getattr(_qtcore, "Qt", _FallbackQt)
    pyqtSignal = _qtcore.pyqtSignal
    _qtnetwork = importlib.import_module("qgis.PyQt.QtNetwork")
    QAbstractSocket = _qtnetwork.QAbstractSocket
    _qgiscore = importlib.import_module("qgis.core")
    QgsProject = _qgiscore.QgsProject
except Exception:
    QObjectBase = _FallbackQObject
    QTimer = _FallbackTimer
    QUrl = _FallbackQUrl
    QCoreApplication = None
    QThread = None
    Qt = _FallbackQt
    pyqtSignal = _fallback_pyqt_signal
    QAbstractSocket = _FallbackQAbstractSocket
    QgsProject = _FallbackQgsProject

try:
    _qtwebsockets = importlib.import_module("qgis.PyQt.QtWebSockets")
    QWebSocket = _qtwebsockets.QWebSocket
except Exception:
    try:
        _qtwebsockets = importlib.import_module("PyQt5.QtWebSockets")
        QWebSocket = _qtwebsockets.QWebSocket
    except Exception:
        QWebSocket = None

try:
    _vendored_websocket = importlib.import_module("autocarto.vendor.websocket")
    VendoredWebSocketApp = _vendored_websocket.WebSocketApp
except Exception as exc:
    VendoredWebSocketApp = None
    # If QtWebSockets exists, vendored websocket is optional.
    if QWebSocket is None:
        logger.warning("导入 vendored websocket 失败: %r", exc)
    else:
        logger.debug("vendored websocket 不可用，已使用 QtWebSockets: %r", exc)

class _VendoredWebSocketAdapter:
    def __init__(
        self,
        websocket_app_cls,
        dispatcher,
        on_connected,
        on_disconnected,
        on_text_message,
        on_error,
    ):
        self._websocket_app_cls = websocket_app_cls
        self._dispatcher = dispatcher
        self._on_connected = on_connected
        self._on_disconnected = on_disconnected
        self._on_text_message = on_text_message
        self._on_error = on_error
        self._app = None
        self._thread = None
        self._connected = False
        self._lock = threading.Lock()

    def state(self) -> int:
        return QAbstractSocket.ConnectedState if self._connected else 0

    def open(self, url: Any):
        self.close()
        ws_url = str(url)
        app = self._websocket_app_cls(
            ws_url,
            on_open=self._handle_open,
            on_message=self._handle_message,
            on_error=self._handle_error,
            on_close=self._handle_close,
        )
        thread = threading.Thread(
            target=self._run_forever,
            args=(app,),
            daemon=True,
            name="autocarto-vendored-ws",
        )
        with self._lock:
            self._app = app
            self._thread = thread
            self._connected = False
        thread.start()

    def close(self):
        with self._lock:
            app = self._app
            thread = self._thread
            self._app = None
            self._thread = None
            self._connected = False
        if app is not None:
            try:
                app.close()
            except Exception:
                pass
        if (
            thread is not None
            and thread.is_alive()
            and thread is not threading.current_thread()
        ):
            thread.join(timeout=1.0)

    def sendTextMessage(self, text: str):
        with self._lock:
            app = self._app
            connected = self._connected
        if app is None or not connected:
            return
        try:
            if hasattr(app, "send_text"):
                app.send_text(text)
            else:
                app.send(text)
        except Exception as exc:
            self._dispatch(self._on_error, exc)

    def _run_forever(self, app):
        try:
            app.run_forever(ping_interval=0)
        except Exception as exc:
            self._dispatch(self._on_error, exc)
        finally:
            with self._lock:
                if self._app is not app:
                    return
                was_connected = self._connected
                self._connected = False
            if was_connected:
                self._dispatch(self._on_disconnected)

    def _handle_open(self, _ws):
        with self._lock:
            if self._app is not _ws:
                return
            self._connected = True
        self._dispatch(self._on_connected)

    def _handle_message(self, _ws, message):
        self._dispatch(self._on_text_message, str(message))

    def _handle_error(self, _ws, error):
        with self._lock:
            if self._app is not _ws:
                return
        self._dispatch(self._on_error, error)

    def _handle_close(self, _ws, _status_code=None, _message=None):
        with self._lock:
            if self._app is not _ws:
                return
            was_connected = self._connected
            self._connected = False
        if was_connected:
            self._dispatch(self._on_disconnected)

    def _dispatch(self, callback, *args: Any):
        if callable(self._dispatcher):
            self._dispatcher(lambda: callback(*args))
            return
        callback(*args)


class _MainThreadInvoker(QObjectBase):
    trigger = pyqtSignal(object)

    def __init__(self):
        super().__init__()
        try:
            self.trigger.connect(self._invoke, Qt.QueuedConnection)
        except TypeError:
            self.trigger.connect(self._invoke)

    def _invoke(self, callback):
        callback()


class InstanceWSClient(QObjectBase):
    """插件反向连接客户端。"""

    connection_changed = pyqtSignal(bool)

    def __init__(
        self,
        config_manager,
        tool_engine,
        instance_id: str,
        qgis_version: str,
        skills_dir: Optional[str] = None,
        session_manager=None,
    ):
        super().__init__()
        self.config_manager = config_manager
        self.tool_engine = tool_engine
        self.instance_id = instance_id
        self.qgis_version = qgis_version
        self.skills_dir = (skills_dir or "").strip()
        self.pid = os.getpid()
        self.start_time = int(time.time())
        self.machine_fingerprint = f"{socket.gethostname()}-{uuid.getnode():x}"

        self._heartbeat_timer = QTimer()
        self._heartbeat_timer.timeout.connect(self._send_heartbeat)
        self._heartbeat_interval_ms = 10_000

        self._reconnect_timer = QTimer()
        self._reconnect_timer.setSingleShot(True)
        self._reconnect_timer.timeout.connect(self._connect)
        self._reconnect_attempt = 0
        # 避免指数退避过大导致“APP已恢复但插件长期不重连”。
        reconnect_max_sec = self.config_manager.get("backend.reconnect_max_sec", 8)
        try:
            reconnect_max_sec = int(reconnect_max_sec)
        except Exception:
            reconnect_max_sec = 8
        self._max_reconnect_sec = max(2, min(reconnect_max_sec, 15))
        self._stopped = False
        self._session_manager = session_manager

        self._use_qt_socket = QWebSocket is not None
        self._use_fallback_socket = False
        self._active_requests: Dict[str, str] = {}
        self._active_request_timers: Dict[str, threading.Timer] = {}
        self._active_request_lock = threading.Lock()
        self._tool_invoke_timeout_sec = 240.0
        self._main_thread_invoker: Any = _MainThreadInvoker()

        if self._use_qt_socket:
            assert QWebSocket is not None
            self._socket = QWebSocket()
            self._socket.connected.connect(self._on_connected)
            self._socket.disconnected.connect(self._on_disconnected)
            self._socket.textMessageReceived.connect(self._on_text_message)
            if hasattr(self._socket, "errorOccurred"):
                self._socket.errorOccurred.connect(self._on_error)
            elif hasattr(self._socket, "error"):
                self._socket.error.connect(self._on_error)
        elif VendoredWebSocketApp is not None:
            self._use_fallback_socket = True
            self._socket = _VendoredWebSocketAdapter(
                websocket_app_cls=VendoredWebSocketApp,
                dispatcher=self._invoke_on_main_thread,
                on_connected=self._on_connected,
                on_disconnected=self._on_disconnected,
                on_text_message=self._on_text_message,
                on_error=self._on_error,
            )
            logger.warning(
                "QtWebSockets 不可用，启用 vendored websocket transport fallback"
            )
        else:
            self._socket = None
            logger.warning("QtWebSockets 不可用，插件进入 transport degraded 模式")

    def start(self):
        self._stopped = False
        self._connect()

    def stop(self):
        self._stopped = True
        self._heartbeat_timer.stop()
        self._reconnect_timer.stop()
        if self._session_manager is not None:
            try:
                self._session_manager.clear()
            except Exception as exc:
                logger.debug("清理会话上下文失败: %s", exc)
        self._send(
            "instance_disconnect",
            {"instance_id": self.instance_id},
        )
        try:
            if self._socket is not None:
                self._socket.close()
        except Exception:
            pass

    def is_connected(self) -> bool:
        if self._socket is not None:
            return self._socket.state() == QAbstractSocket.ConnectedState
        return False

    def backend_url(self) -> str:
        return self._build_ws_url()

    def is_reconnecting(self) -> bool:
        """当前是否处于重连等待中。"""
        return (not self.is_connected()) and self._reconnect_timer.isActive()

    def report_project_event(self, event_type: str, project_path: str = ""):
        self._send(
            "instance_project_event",
            {
                "instance_id": self.instance_id,
                "event_type": event_type,
                "data": {"project_path": project_path} if project_path else {},
            },
        )

    def _build_ws_url(self) -> str:
        from .service_port_reader import build_runtime_ws_url

        return build_runtime_ws_url(role="plugin")

    def _connect(self):
        if self._stopped:
            return
        if self.is_connected():
            return
        if self._socket is None:
            self._schedule_reconnect("缺少可用的 WebSocket transport，无法建立反向连接")
            return
        ws_url = self._build_ws_url()
        if not ws_url:
            reason = "缺少 runtime port file，等待 carto-agent 写入端口后重连"
            logger.warning(reason)
            self._schedule_reconnect(reason)
            return
        logger.info("插件反向连接中: %s", ws_url)
        if self._use_qt_socket:
            self._socket.open(QUrl(ws_url))
            return
        self._socket.open(ws_url)

    def _on_connected(self):
        self._reconnect_attempt = 0
        self._reconnect_timer.stop()
        self._heartbeat_timer.start(self._heartbeat_interval_ms)
        logger.info("插件反向连接已建立")
        self.connection_changed.emit(True)
        self._send_instance_connect()

    def _on_disconnected(self):
        self._heartbeat_timer.stop()
        self._schedule_reconnect("反向连接断开")
        self.connection_changed.emit(False)

    def _on_error(self, error=None):
        if self._stopped:
            return
        if error:
            logger.warning("反向连接错误: %s", error)
        else:
            logger.debug("反向连接发生错误，等待断开回调触发重连")
        if not self.is_connected():
            self._schedule_reconnect("反向连接错误")

    def _schedule_reconnect(self, reason: str):
        if self._stopped:
            return
        if self._reconnect_timer.isActive():
            return
        # 封顶退避阶数，避免 2**n 出现极大值。
        step = min(self._reconnect_attempt, 6)
        delay_sec = min(2**step, self._max_reconnect_sec)
        jitter = 1.0 + random.random() * 0.2
        delay_ms = int(delay_sec * jitter * 1000)
        self._reconnect_attempt = min(self._reconnect_attempt + 1, 6)
        logger.warning("%s，%dms 后重连", reason, delay_ms)
        self._reconnect_timer.start(delay_ms)

    def _on_text_message(self, text: str):
        try:
            message = json.loads(text)
        except Exception as exc:
            logger.warning("解析反向连接消息失败: %s", exc)
            return

        msg_type = message.get("type")
        data = message.get("data") or {}
        if msg_type == "tool_invoke":
            self._handle_tool_invoke(data)
        elif msg_type == "tool_cancel":
            self._handle_tool_cancel(data)
        elif msg_type == "tool_engine_contract":
            self._handle_tool_engine_contract(data)

    def _handle_tool_engine_contract(self, data: Dict[str, Any]):
        execution_manifest = data.get("execution_manifest")
        public_tool_catalog = data.get("tool_catalog")
        if not isinstance(execution_manifest, list):
            execution_manifest = []
        if not isinstance(public_tool_catalog, list):
            public_tool_catalog = []
        updater = getattr(self.tool_engine, "update_contract", None)
        if not callable(updater):
            logger.warning("tool engine 不支持 contract 更新，忽略下发的工具契约")
            return
        updater(
            execution_manifest=execution_manifest,
            public_tool_catalog=public_tool_catalog,
        )
        logger.info(
            "已接收后端下发的 tool engine contract: execution_manifest=%d tool_catalog=%d",
            len(execution_manifest),
            len(public_tool_catalog),
        )

    def _send_instance_connect(self):
        project_path = QgsProject.instance().fileName() or ""
        project_id = self._build_project_id(project_path)
        payload = {
            "instance_id": self.instance_id,
            "pid": self.pid,
            "qgis_version": self.qgis_version,
            "python_path": sys.executable,
            "plugin_path": os.path.dirname(os.path.dirname(__file__)),
            "project_id": project_id,
            "project_path": project_path,
            "machine_fingerprint": self.machine_fingerprint,
            "start_time": self.start_time,
            "source": "reverse_ws",
        }
        self._send("instance_connect", payload)

    def _invoke_on_main_thread(self, callback):
        try:
            if QCoreApplication is not None and QThread is not None:
                app = QCoreApplication.instance()
                if app is not None and QThread.currentThread() == app.thread():
                    callback()
                    return
                if app is not None and self._main_thread_invoker is not None:
                    self._main_thread_invoker.trigger.emit(callback)
                    return
        except Exception:
            pass
        try:
            single_shot = getattr(QTimer, "singleShot", None)
            if callable(single_shot):
                single_shot(0, callback)
                return
        except Exception:
            pass
        callback()

    def _handle_tool_invoke(self, data: Dict[str, Any]):
        request_id = str(data.get("request_id") or "").strip()
        tool_name = str(data.get("tool_name") or "").strip()
        params = data.get("params") if isinstance(data.get("params"), dict) else {}

        logger.info(
            "收到 bridge tool_invoke: request_id=%s tool=%s", request_id, tool_name
        )
        if request_id and tool_name:
            self._register_active_request(request_id, tool_name)

        def _do_invoke():
            if not request_id or not tool_name:
                self._send(
                    "tool_result",
                    {
                        "request_id": request_id,
                        "success": False,
                        "output": None,
                        "error": "missing request_id or tool_name",
                        "output_layers": [],
                    },
                )
                return

            request_state = self._begin_request_execution(request_id)
            if request_state == _REQUEST_STATE_CANCEL_REQUESTED:
                logger.info(
                    "bridge 工具在执行前已收到取消请求，跳过执行: request_id=%s tool=%s",
                    request_id,
                    tool_name,
                )
                self._complete_active_request(request_id)
                return
            if request_state == _REQUEST_STATE_TIMED_OUT:
                logger.info(
                    "bridge 工具在执行前已超时，跳过执行: request_id=%s tool=%s",
                    request_id,
                    tool_name,
                )
                self._complete_active_request(request_id)
                return
            if request_state is None:
                logger.warning(
                    "忽略晚到的 bridge 工具执行回调: request_id=%s tool=%s",
                    request_id,
                    tool_name,
                )
                return

            logger.info(
                "开始执行 bridge 工具: request_id=%s tool=%s", request_id, tool_name
            )
            started_at = time.monotonic()
            try:
                result = execute_tool(tool_name, params, engine=self.tool_engine)
            except Exception as exc:
                logger.exception(
                    "执行 bridge 工具异常: request_id=%s tool=%s", request_id, tool_name
                )
                result = {
                    "success": False,
                    "output": None,
                    "error": str(exc),
                    "output_layers": [],
                }

            elapsed_ms = int((time.monotonic() - started_at) * 1000)
            request_state = self._complete_active_request(request_id)
            if request_state is None:
                logger.warning(
                    "忽略晚到的 bridge 工具结果: request_id=%s tool=%s elapsed_ms=%d",
                    request_id,
                    tool_name,
                    elapsed_ms,
                )
                return
            if request_state == _REQUEST_STATE_CANCEL_REQUESTED:
                logger.info(
                    "bridge 工具已收到取消请求，忽略结果: request_id=%s tool=%s elapsed_ms=%d",
                    request_id,
                    tool_name,
                    elapsed_ms,
                )
                return
            if request_state == _REQUEST_STATE_TIMED_OUT:
                logger.info(
                    "bridge 工具已超时，忽略结果: request_id=%s tool=%s elapsed_ms=%d",
                    request_id,
                    tool_name,
                    elapsed_ms,
                )
                return

            success = bool(result.get("success"))
            terminal_state = (
                _REQUEST_TERMINAL_STATE_SUCCEEDED
                if success
                else _REQUEST_TERMINAL_STATE_FAILED
            )
            error_message = str(result.get("error") or "").strip()
            if not success and not error_message:
                error_message = self._extract_tool_error(result)
            message = str(result.get("message") or result.get("info") or "").strip()
            if not message:
                message = "执行完成" if success else error_message

            logger.info(
                "bridge 工具执行完成，准备回传结果: request_id=%s tool=%s success=%s elapsed_ms=%d",
                request_id,
                tool_name,
                success,
                elapsed_ms,
            )
            self._send(
                "tool_result",
                self._build_terminal_tool_result_payload(
                    request_id=request_id,
                    success=success,
                    output=result.get("output"),
                    error=error_message,
                    output_layers=result.get("output_layers") or [],
                    terminal_state=terminal_state,
                    progress=1.0 if success else 0.0,
                    message=message,
                ),
            )

        self._invoke_on_main_thread(_do_invoke)

    def _handle_tool_cancel(self, data: Dict[str, Any]):
        request_id = str(data.get("request_id") or "").strip()
        tool_name = str(data.get("tool_name") or "").strip()
        if not request_id:
            logger.warning("收到缺少 request_id 的 tool_cancel: tool=%s", tool_name)
            return
        cancel_state = self._mark_request_cancelled(request_id)
        if cancel_state in (_REQUEST_STATE_PENDING, _REQUEST_STATE_RUNNING):
            logger.info(
                "已标记 bridge 工具取消请求: request_id=%s tool=%s",
                request_id,
                tool_name,
            )
            self._send_tool_canceled_result(request_id)
            return
        if cancel_state == _REQUEST_STATE_CANCEL_REQUESTED:
            logger.info(
                "重复收到 bridge 工具取消请求，保持已发布 canceled 终态: request_id=%s tool=%s",
                request_id,
                tool_name,
            )
            return
        logger.warning(
            "收到未知或已结束的 tool_cancel: request_id=%s tool=%s",
            request_id,
            tool_name,
        )

    @staticmethod
    def _build_project_id(project_path: str) -> str:
        value = (project_path or "unsaved").encode("utf-8")
        digest = hashlib.sha1(value).hexdigest()
        return f"project_{digest[:16]}"

    def _send_heartbeat(self):
        sent = self._send(
            "instance_heartbeat",
            {
                "instance_id": self.instance_id,
                "ts": int(time.time()),
            },
        )
        if sent:
            return
        try:
            if self._socket is not None:
                self._socket.close()
        except Exception:
            pass
        self._on_disconnected()

    @staticmethod
    def _extract_tool_error(result: Dict[str, Any]) -> str:
        """从工具返回结果中提取错误信息。"""
        return extract_tool_error(result)

    @staticmethod
    def _build_terminal_tool_result_payload(
        request_id: str,
        success: bool,
        output: Any,
        error: str,
        output_layers: Any,
        terminal_state: str,
        progress: float,
        message: str,
    ) -> Dict[str, Any]:
        return {
            "request_id": request_id,
            "success": bool(success),
            "output": output,
            "error": str(error or "").strip(),
            "output_layers": output_layers or [],
            "status": terminal_state,
            "terminal_state": terminal_state,
            "progress": min(max(float(progress), 0.0), 1.0),
            "message": str(message or "").strip(),
        }

    def _send(self, msg_type: str, data: Optional[Dict[str, Any]] = None):
        if not self.is_connected():
            return False
        payload = {"type": msg_type, "data": data or {}}
        text, replaced = safe_json_dumps(payload)
        if replaced > 0:
            logger.debug(
                "发送消息前已清理不可序列化值: type=%s count=%d", msg_type, replaced
            )
        try:
            if self._socket is not None:
                self._socket.sendTextMessage(text)
            return True
        except Exception as exc:
            logger.warning("发送反向连接消息失败: %s", exc)
            return False

    def _send_tool_canceled_result(self, request_id: str):
        self._send(
            "tool_result",
            self._build_terminal_tool_result_payload(
                request_id=request_id,
                success=False,
                output=None,
                error="",
                output_layers=[],
                terminal_state=_REQUEST_TERMINAL_STATE_CANCELED,
                progress=0.0,
                message="取消已接受",
            ),
        )

    def _register_active_request(self, request_id: str, tool_name: str):
        timer = threading.Timer(
            self._tool_invoke_timeout_sec,
            self._handle_tool_timeout,
            args=(request_id, tool_name),
        )
        timer.daemon = True
        with self._active_request_lock:
            self._active_requests[request_id] = _REQUEST_STATE_PENDING
            self._active_request_timers[request_id] = timer
        timer.start()

    def _mark_request_cancelled(self, request_id: str) -> Optional[str]:
        timer = None
        with self._active_request_lock:
            request_state = self._active_requests.get(request_id)
            if request_state is None:
                return None
            if request_state in (
                _REQUEST_STATE_CANCEL_REQUESTED,
                _REQUEST_STATE_TIMED_OUT,
            ):
                return request_state
            self._active_requests[request_id] = _REQUEST_STATE_CANCEL_REQUESTED
            timer = self._active_request_timers.pop(request_id, None)
        if timer is not None:
            timer.cancel()
        return request_state

    def _begin_request_execution(self, request_id: str) -> Optional[str]:
        with self._active_request_lock:
            request_state = self._active_requests.get(request_id)
            if request_state is None:
                return None
            if request_state in (
                _REQUEST_STATE_CANCEL_REQUESTED,
                _REQUEST_STATE_TIMED_OUT,
            ):
                return request_state
            self._active_requests[request_id] = _REQUEST_STATE_RUNNING
            return _REQUEST_STATE_RUNNING

    def _complete_active_request(self, request_id: str) -> Optional[str]:
        with self._active_request_lock:
            request_state = self._active_requests.pop(request_id, None)
            timer = self._active_request_timers.pop(request_id, None)
        if timer is not None:
            timer.cancel()
        return request_state

    def _handle_tool_timeout(self, request_id: str, tool_name: str):
        should_send_timeout = False
        with self._active_request_lock:
            request_state = self._active_requests.get(request_id)
            if request_state is None:
                return
            if request_state == _REQUEST_STATE_CANCEL_REQUESTED:
                self._active_request_timers.pop(request_id, None)
                return
            if request_state == _REQUEST_STATE_TIMED_OUT:
                return
            self._active_requests[request_id] = _REQUEST_STATE_TIMED_OUT
            timer = self._active_request_timers.pop(request_id, None)
            should_send_timeout = True
        if timer is not None:
            timer.cancel()
        if not should_send_timeout:
            return

        logger.warning(
            "bridge 工具执行超时: request_id=%s tool=%s timeout_sec=%.1f",
            request_id,
            tool_name,
            self._tool_invoke_timeout_sec,
        )
        self._send(
            "tool_result",
            self._build_terminal_tool_result_payload(
                request_id=request_id,
                success=False,
                output=None,
                error=f"tool execution timed out after {int(self._tool_invoke_timeout_sec)}s",
                output_layers=[],
                terminal_state=_REQUEST_TERMINAL_STATE_FAILED,
                progress=0.0,
                message="执行超时",
            ),
        )
