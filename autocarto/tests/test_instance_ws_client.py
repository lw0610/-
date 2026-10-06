import importlib
import json
import os
import shutil
import tempfile
import threading
import time
import types
import unittest
import uuid
from pathlib import Path
from unittest import mock
from typing import Any, cast

import autocarto.core.instance_ws_client as instance_ws_client_module

from autocarto.core.instance_ws_client import InstanceWSClient


def _make_workspace_temp_dir() -> Path:
    root = Path(__file__).resolve().parents[2] / ".tmp-test"
    path = root / f"instance-ws-client-{uuid.uuid4().hex}"
    path.mkdir(parents=True, exist_ok=False)
    return path


class _FakeConfigManager:
    def get(self, _key, default=None):
        return default


class _FakeToolEngine:
    def __init__(self):
        self.list_calls = 0

    def list_tools(self):
        self.list_calls += 1
        return [{"name": "open_project", "category": "project", "description": "open"}]

    def invoke(self, tool_name, params=None):
        return {
            "success": True,
            "output": {"tool": tool_name, "params": params or {}},
            "error": "",
            "output_layers": [],
        }


class _FakeReconnectTimer:
    def __init__(self):
        self.active = False
        self.started_with = None

    def isActive(self):
        return self.active

    def start(self, interval):
        self.active = True
        self.started_with = interval

    def stop(self):
        self.active = False


class _CaptureSignal:
    def __init__(self, callback=None):
        self._callback = callback

    def connect(self, _callback):
        self._callback = _callback

    def emit(self, *args, **kwargs):
        if self._callback is not None:
            self._callback(*args, **kwargs)


class _FakeSessionManager:
    def __init__(self):
        self.reset_calls = []

    def reset_session_log_if_active(self, session_id, logs_dir):
        self.reset_calls.append((session_id, logs_dir))
        return True


class _ModuleTestSignal:
    def connect(self, _callback):
        return None

    def emit(self, *_args, **_kwargs):
        return None


class _ModuleTestTimer:
    def __init__(self):
        self.timeout = _ModuleTestSignal()

    def setSingleShot(self, _value):
        return None

    def setInterval(self, _value):
        return None

    def start(self, _interval=0):
        return None

    def stop(self):
        return None

    def isActive(self):
        return False


class _ModuleTestSocket:
    def __init__(self):
        self.connected = _ModuleTestSignal()
        self.disconnected = _ModuleTestSignal()
        self.textMessageReceived = _ModuleTestSignal()
        self.errorOccurred = _ModuleTestSignal()

    def state(self):
        return 0

    def open(self, _url):
        return None

    def close(self):
        return None


class _ModuleTestWebSocketApp:
    def __init__(
        self,
        url,
        on_open=None,
        on_message=None,
        on_error=None,
        on_close=None,
    ):
        self.url = url
        self.on_open = on_open
        self.on_message = on_message
        self.on_error = on_error
        self.on_close = on_close

    def run_forever(self, **_kwargs):
        return None

    def send(self, _payload):
        return None

    def close(self):
        return None


class _ModuleTestOpenWebSocketApp(_ModuleTestWebSocketApp):
    def run_forever(self, **_kwargs):
        if self.on_open is not None:
            self.on_open(self)
        return None


class _ModuleTestOpenThenReturnWebSocketApp(_ModuleTestWebSocketApp):
    def run_forever(self, **_kwargs):
        if self.on_open is not None:
            self.on_open(self)
        return None


class _ModuleTestLingeringWebSocketApp(_ModuleTestWebSocketApp):
    instances = []

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.release = threading.Event()
        self.closed = False
        type(self).instances.append(self)

    def run_forever(self, **_kwargs):
        if self.on_open is not None:
            self.on_open(self)
        self.release.wait(timeout=5)
        return None

    def close(self):
        self.closed = True


class InstanceWSClientTestCase(unittest.TestCase):
    def test_vendored_adapter_dispatches_open_callback_via_dispatcher(self):
        connected = []
        queued_callbacks = []

        def fake_dispatcher(callback):
            queued_callbacks.append(callback)

        adapter = instance_ws_client_module._VendoredWebSocketAdapter(
            websocket_app_cls=_ModuleTestOpenWebSocketApp,
            dispatcher=fake_dispatcher,
            on_connected=lambda: connected.append(True),
            on_disconnected=lambda: None,
            on_text_message=lambda _message: None,
            on_error=lambda _error=None: None,
        )

        adapter.open("ws://127.0.0.1:3000/ws?role=plugin")
        if adapter._thread is not None:
            adapter._thread.join(timeout=1)

        self.assertEqual(connected, [])
        self.assertEqual(len(queued_callbacks), 2)
        queued_callbacks[0]()
        self.assertEqual(connected, [True])
        queued_callbacks[1]()
        self.assertEqual(adapter.state(), 0)

    def test_vendored_adapter_treats_run_forever_return_as_disconnect(self):
        events = []
        queued_callbacks = []

        def fake_dispatcher(callback):
            queued_callbacks.append(callback)

        adapter = instance_ws_client_module._VendoredWebSocketAdapter(
            websocket_app_cls=_ModuleTestOpenThenReturnWebSocketApp,
            dispatcher=fake_dispatcher,
            on_connected=lambda: events.append("connected"),
            on_disconnected=lambda: events.append("disconnected"),
            on_text_message=lambda _message: None,
            on_error=lambda _error=None: None,
        )

        adapter.open("ws://127.0.0.1:3000/ws?role=plugin")
        if adapter._thread is not None:
            adapter._thread.join(timeout=1)

        self.assertEqual(len(queued_callbacks), 2)
        queued_callbacks[0]()
        queued_callbacks[1]()
        self.assertEqual(events, ["connected", "disconnected"])
        self.assertEqual(adapter.state(), 0)

    def test_vendored_adapter_ignores_stale_disconnect_from_previous_connection(self):
        _ModuleTestLingeringWebSocketApp.instances = []
        queued_callbacks = []
        events = []

        def fake_dispatcher(callback):
            queued_callbacks.append(callback)

        adapter = instance_ws_client_module._VendoredWebSocketAdapter(
            websocket_app_cls=_ModuleTestLingeringWebSocketApp,
            dispatcher=fake_dispatcher,
            on_connected=lambda: events.append("connected"),
            on_disconnected=lambda: events.append("disconnected"),
            on_text_message=lambda _message: None,
            on_error=lambda _error=None: None,
        )

        adapter.open("ws://127.0.0.1:3000/ws?role=plugin-1")
        first_app = _ModuleTestLingeringWebSocketApp.instances[0]
        while not queued_callbacks:
            time.sleep(0.01)
        queued_callbacks.pop(0)()
        self.assertEqual(events, ["connected"])
        self.assertEqual(
            adapter.state(), instance_ws_client_module.QAbstractSocket.ConnectedState
        )

        adapter.open("ws://127.0.0.1:3000/ws?role=plugin-2")
        second_app = _ModuleTestLingeringWebSocketApp.instances[1]
        while not queued_callbacks:
            time.sleep(0.01)
        queued_callbacks.pop(0)()
        self.assertEqual(events, ["connected", "connected"])
        self.assertEqual(
            adapter.state(), instance_ws_client_module.QAbstractSocket.ConnectedState
        )

        first_app.release.set()
        if adapter._thread is not None:
            adapter._thread.join(timeout=0.1)
        time.sleep(0.05)

        self.assertEqual(events, ["connected", "connected"])
        self.assertEqual(
            adapter.state(), instance_ws_client_module.QAbstractSocket.ConnectedState
        )

        second_app.release.set()
        time.sleep(0.05)
        self.assertEqual(len(queued_callbacks), 1)
        queued_callbacks.pop(0)()
        self.assertEqual(events, ["connected", "connected", "disconnected"])
        self.assertEqual(adapter.state(), 0)

    def test_invoke_on_main_thread_uses_invoker_for_cross_thread_callbacks(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        dispatched = []
        called = []

        class _FakeTrigger:
            def emit(self, callback):
                dispatched.append(callback)

        client._main_thread_invoker = cast(
            Any, types.SimpleNamespace(trigger=_FakeTrigger())
        )

        fake_app = types.SimpleNamespace(thread=lambda: object())
        fake_qcore = types.SimpleNamespace(instance=lambda: fake_app)
        fake_qthread = types.SimpleNamespace(currentThread=lambda: object())

        with (
            mock.patch.object(
                instance_ws_client_module, "QCoreApplication", fake_qcore
            ),
            mock.patch.object(instance_ws_client_module, "QThread", fake_qthread),
            mock.patch.object(
                instance_ws_client_module, "QTimer", types.SimpleNamespace()
            ),
        ):
            client._invoke_on_main_thread(lambda: called.append(True))

        self.assertEqual(called, [])
        self.assertEqual(len(dispatched), 1)
        dispatched[0]()
        self.assertEqual(called, [True])

    def test_connect_uses_vendored_websocket_fallback_when_qt_missing(self):
        fake_qtcore = types.SimpleNamespace(
            QObject=object,
            QTimer=_ModuleTestTimer,
            QUrl=str,
            QCoreApplication=None,
            QThread=None,
            pyqtSignal=lambda *_args, **_kwargs: _ModuleTestSignal(),
        )
        fake_qtnetwork = types.SimpleNamespace(
            QAbstractSocket=types.SimpleNamespace(ConnectedState=1)
        )
        fake_qgiscore = types.SimpleNamespace(
            QgsProject=types.SimpleNamespace(
                instance=lambda: types.SimpleNamespace(fileName=lambda: "")
            )
        )
        fake_vendored_websocket = types.SimpleNamespace(
            WebSocketApp=_ModuleTestWebSocketApp
        )

        real_import = importlib.import_module

        def fake_import(name, package=None):
            if name == "qgis.PyQt.QtCore":
                return fake_qtcore
            if name == "qgis.PyQt.QtNetwork":
                return fake_qtnetwork
            if name == "qgis.core":
                return fake_qgiscore
            if name in {"qgis.PyQt.QtWebSockets", "PyQt5.QtWebSockets"}:
                raise ModuleNotFoundError(name)
            if name == "autocarto.vendor.websocket":
                return fake_vendored_websocket
            return real_import(name, package)

        with mock.patch("importlib.import_module", side_effect=fake_import):
            reloaded_module = importlib.reload(instance_ws_client_module)
        try:
            client = reloaded_module.InstanceWSClient(
                config_manager=_FakeConfigManager(),
                tool_engine=_FakeToolEngine(),
                instance_id="inst-vendor",
                qgis_version="3.40",
                skills_dir="/tmp/plugin/skills",
            )
            self.assertFalse(client._use_qt_socket)
            self.assertTrue(client._use_fallback_socket)
            self.assertIsNotNone(client._socket)
        finally:
            importlib.reload(instance_ws_client_module)

    def test_connect_uses_pyqt5_qtwebsockets_fallback_when_qgis_wrapper_missing(self):
        fake_qtcore = types.SimpleNamespace(
            QObject=object,
            QTimer=_ModuleTestTimer,
            QUrl=str,
            QCoreApplication=None,
            QThread=None,
            pyqtSignal=lambda *_args, **_kwargs: _ModuleTestSignal(),
        )
        fake_qtnetwork = types.SimpleNamespace(
            QAbstractSocket=types.SimpleNamespace(ConnectedState=1)
        )
        fake_qgiscore = types.SimpleNamespace(
            QgsProject=types.SimpleNamespace(
                instance=lambda: types.SimpleNamespace(fileName=lambda: "")
            )
        )
        fake_pyqt5_qtwebsockets = types.SimpleNamespace(QWebSocket=_ModuleTestSocket)

        real_import = importlib.import_module

        def fake_import(name, package=None):
            if name == "qgis.PyQt.QtCore":
                return fake_qtcore
            if name == "qgis.PyQt.QtNetwork":
                return fake_qtnetwork
            if name == "qgis.core":
                return fake_qgiscore
            if name == "qgis.PyQt.QtWebSockets":
                raise ModuleNotFoundError(name)
            if name == "PyQt5.QtWebSockets":
                return fake_pyqt5_qtwebsockets
            return real_import(name, package)

        with mock.patch("importlib.import_module", side_effect=fake_import):
            reloaded_module = importlib.reload(instance_ws_client_module)
        try:
            client = reloaded_module.InstanceWSClient(
                config_manager=_FakeConfigManager(),
                tool_engine=_FakeToolEngine(),
                instance_id="inst-1",
                qgis_version="3.40",
                skills_dir="/tmp/plugin/skills",
            )
            self.assertTrue(client._use_qt_socket)
            self.assertIsNotNone(client._socket)
        finally:
            importlib.reload(instance_ws_client_module)

    def test_bootstrap_backend_url_uses_runtime_port_file(self):
        tmp_dir = _make_workspace_temp_dir()
        try:
            port_file = Path(tmp_dir, "data", ".port")
            port_file.parent.mkdir(parents=True, exist_ok=True)
            port_file.write_text("50321", encoding="utf-8")
            client = InstanceWSClient(
                config_manager=_FakeConfigManager(),
                tool_engine=_FakeToolEngine(),
                instance_id="inst-1",
                qgis_version="3.40",
                skills_dir="/tmp/plugin/skills",
            )

            with mock.patch.dict(os.environ, {"AUTOCARTO_HOME": str(tmp_dir)}, clear=False):
                self.assertEqual(
                    client.backend_url(), "ws://127.0.0.1:50321/ws?role=plugin"
                )
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)

    def test_bootstrap_backend_url_does_not_fallback_to_local_config_ws_url_when_port_missing(
        self,
    ):
        class _LegacyConfigManager:
            def get(self, key, default=None):
                if key == "backend.ws_url":
                    return "ws://127.0.0.1:3000/ws"
                return default

        client = InstanceWSClient(
            config_manager=_LegacyConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        tmp_dir = _make_workspace_temp_dir()
        try:
            with mock.patch.dict(os.environ, {"AUTOCARTO_HOME": str(tmp_dir)}, clear=False):
                self.assertEqual(client.backend_url(), "")
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)

    def test_bootstrap_connect_skips_socket_open_when_runtime_port_missing(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        socket_mock = mock.Mock()
        client._use_qt_socket = True
        client._socket = socket_mock
        client._reconnect_timer = _FakeReconnectTimer()

        tmp_dir = _make_workspace_temp_dir()
        try:
            with mock.patch.dict(os.environ, {"AUTOCARTO_HOME": str(tmp_dir)}, clear=False):
                client._connect()
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)

        socket_mock.open.assert_not_called()
        self.assertTrue(client._reconnect_timer.isActive())

    def test_bootstrap_connect_uses_runtime_port_file(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        socket_mock = mock.Mock()
        client._use_qt_socket = True
        client._socket = socket_mock
        client._reconnect_timer = _FakeReconnectTimer()

        tmp_dir = _make_workspace_temp_dir()
        try:
            port_file = Path(tmp_dir, "data", ".port")
            port_file.parent.mkdir(parents=True, exist_ok=True)
            port_file.write_text("50322", encoding="utf-8")
            with mock.patch.dict(os.environ, {"AUTOCARTO_HOME": str(tmp_dir)}, clear=False):
                client._connect()
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)

        socket_mock.open.assert_called_once()
        self.assertIn("ws://127.0.0.1:50322/ws?role=plugin", str(socket_mock.open.call_args))
        self.assertFalse(client._reconnect_timer.isActive())

    def test_connect_does_not_fallback_to_raw_websocket_when_qt_missing(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        client._use_qt_socket = False
        client._use_fallback_socket = False
        client._socket = None
        client._reconnect_timer = _FakeReconnectTimer()

        tmp_dir = _make_workspace_temp_dir()
        try:
            port_file = Path(tmp_dir, "data", ".port")
            port_file.parent.mkdir(parents=True, exist_ok=True)
            port_file.write_text("50321", encoding="utf-8")
            with mock.patch.dict(os.environ, {"AUTOCARTO_HOME": str(tmp_dir)}, clear=False):
                client._connect()
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)

        self.assertTrue(client._reconnect_timer.isActive())
        self.assertFalse(client.is_connected())

    def test_runtime_port_file_changes_are_used_without_descriptor_cache(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        tmp_dir = _make_workspace_temp_dir()
        try:
            port_file = Path(tmp_dir, "data", ".port")
            port_file.parent.mkdir(parents=True, exist_ok=True)
            with mock.patch.dict(os.environ, {"AUTOCARTO_HOME": str(tmp_dir)}, clear=False):
                port_file.write_text("50321", encoding="utf-8")
                self.assertEqual(
                    client.backend_url(), "ws://127.0.0.1:50321/ws?role=plugin"
                )
                port_file.write_text("50322", encoding="utf-8")
                self.assertEqual(
                    client.backend_url(), "ws://127.0.0.1:50322/ws?role=plugin"
                )
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)

    def test_send_instance_connect_omits_local_catalog_truth_fields(self):
        tool_engine = _FakeToolEngine()
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=tool_engine,
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        sent = {}

        def _capture(msg_type, data=None):
            sent["type"] = msg_type
            sent["data"] = data or {}
            return True

        client._send = _capture
        client._send_instance_connect()

        self.assertEqual(sent["type"], "instance_connect")
        self.assertNotIn("skills_dir", sent["data"])
        self.assertNotIn("tools", sent["data"])
        self.assertNotIn("ipc_socket_path", sent["data"])
        self.assertEqual(tool_engine.list_calls, 0)

    def test_on_text_message_handles_tool_invoke(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        sent = {}

        def _capture(msg_type, data=None):
            sent["type"] = msg_type
            sent["data"] = data or {}
            return True

        client._send = _capture
        client._on_text_message(
            json.dumps(
                {
                    "type": "tool_invoke",
                    "data": {
                        "request_id": "req-1",
                        "tool_name": "open_project",
                        "params": {"path": "/tmp/demo.qgz"},
                    },
                }
            )
        )

        self.assertEqual(sent["type"], "tool_result")
        self.assertEqual(sent["data"]["request_id"], "req-1")
        self.assertTrue(sent["data"]["success"])
        self.assertEqual(sent["data"]["output"]["tool"], "open_project")

    def test_on_text_message_ignores_session_log_reset_side_channel(self):
        session_manager = _FakeSessionManager()
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
            session_manager=session_manager,
        )

        sent = []

        def _capture(msg_type, data=None):
            sent.append((msg_type, data or {}))
            return True

        client._send = _capture

        client._on_text_message(
            json.dumps(
                {
                    "type": "session_log_reset",
                    "data": {
                        "session_id": "sid-1",
                        "logs_dir": "/tmp/logs",
                    },
                }
            )
        )

        self.assertEqual(session_manager.reset_calls, [])
        self.assertEqual(sent, [])

    def test_on_text_message_ignores_tool_list_request_catalog_truth_path(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        sent = []

        def _capture(msg_type, data=None):
            sent.append((msg_type, data or {}))
            return True

        client._send = _capture
        client._on_text_message(
            json.dumps(
                {
                    "type": "tool_list_request",
                    "data": {
                        "request_id": "req-list-1",
                    },
                }
            )
        )

        self.assertEqual(sent, [])

    def test_report_project_event_emits_open_save_close_payloads(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        sent = []

        def _capture(msg_type, data=None):
            sent.append((msg_type, data or {}))
            return True

        client._send = _capture

        client.report_project_event("project_opened", "/tmp/opened.qgz")
        client.report_project_event("project_saved", "/tmp/saved.qgz")
        client.report_project_event("project_closed", "")

        self.assertEqual(
            sent,
            [
                (
                    "instance_project_event",
                    {
                        "instance_id": "inst-1",
                        "event_type": "project_opened",
                        "data": {"project_path": "/tmp/opened.qgz"},
                    },
                ),
                (
                    "instance_project_event",
                    {
                        "instance_id": "inst-1",
                        "event_type": "project_saved",
                        "data": {"project_path": "/tmp/saved.qgz"},
                    },
                ),
                (
                    "instance_project_event",
                    {
                        "instance_id": "inst-1",
                        "event_type": "project_closed",
                        "data": {},
                    },
                ),
            ],
        )

    def test_on_text_message_handles_tool_cancel_and_suppresses_late_result(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        callbacks = []
        sent = []

        client._invoke_on_main_thread = lambda callback: callbacks.append(callback)

        def _capture(msg_type, data=None):
            sent.append((msg_type, data or {}))
            return True

        client._send = _capture

        with mock.patch(
            "autocarto.core.instance_ws_client.execute_tool",
            return_value={
                "success": True,
                "output": {"tool": "open_project"},
                "error": "",
                "output_layers": [],
            },
        ) as execute_tool_mock:
            client._on_text_message(
                json.dumps(
                    {
                        "type": "tool_invoke",
                        "data": {
                            "request_id": "req-cancel-1",
                            "tool_name": "open_project",
                            "params": {"path": "/tmp/demo.qgz"},
                        },
                    }
                )
            )
            client._on_text_message(
                json.dumps(
                    {
                        "type": "tool_cancel",
                        "data": {
                            "request_id": "req-cancel-1",
                            "tool_name": "open_project",
                        },
                    }
                )
            )

            self.assertEqual(
                client._active_requests["req-cancel-1"], "cancel_requested"
            )
            callbacks[0]()
            execute_tool_mock.assert_not_called()

        self.assertEqual(
            sent,
            [
                (
                    "tool_result",
                    {
                        "request_id": "req-cancel-1",
                        "success": False,
                        "output": None,
                        "error": "",
                        "output_layers": [],
                        "status": "canceled",
                        "terminal_state": "canceled",
                        "progress": 0.0,
                        "message": "取消已接受",
                    },
                )
            ],
        )
        self.assertNotIn("req-cancel-1", client._active_requests)

    def test_handle_tool_cancel_emits_single_canceled_terminal_state_and_late_result_cannot_overwrite(
        self,
    ):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        sent = []

        def _capture(msg_type, data=None):
            sent.append((msg_type, data or {}))
            return True

        client._send = _capture

        def _execute_tool(_tool_name, _params, engine=None):
            client._handle_tool_cancel(
                {"request_id": "req-cancel-running", "tool_name": "open_project"}
            )
            return {
                "success": True,
                "output": {"tool": "open_project", "engine": engine is not None},
                "error": "",
                "output_layers": [],
            }

        with mock.patch(
            "autocarto.core.instance_ws_client.execute_tool",
            side_effect=_execute_tool,
        ):
            client._on_text_message(
                json.dumps(
                    {
                        "type": "tool_invoke",
                        "data": {
                            "request_id": "req-cancel-running",
                            "tool_name": "open_project",
                            "params": {"path": "/tmp/demo.qgz"},
                        },
                    }
                )
            )

        self.assertEqual(
            sent,
            [
                (
                    "tool_result",
                    {
                        "request_id": "req-cancel-running",
                        "success": False,
                        "output": None,
                        "error": "",
                        "output_layers": [],
                        "status": "canceled",
                        "terminal_state": "canceled",
                        "progress": 0.0,
                        "message": "取消已接受",
                    },
                )
            ],
        )

    def test_handle_tool_invoke_sends_public_terminal_state_fields(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        sent = {}

        def _capture(msg_type, data=None):
            sent["type"] = msg_type
            sent["data"] = data or {}
            return True

        client._send = _capture

        with mock.patch(
            "autocarto.core.instance_ws_client.execute_tool",
            return_value={
                "success": True,
                "output": {"tool": "open_project"},
                "error": "",
                "output_layers": [],
            },
        ):
            client._on_text_message(
                json.dumps(
                    {
                        "type": "tool_invoke",
                        "data": {
                            "request_id": "req-success-1",
                            "tool_name": "open_project",
                            "params": {"path": "/tmp/demo.qgz"},
                        },
                    }
                )
            )

        self.assertEqual(sent["type"], "tool_result")
        self.assertEqual(sent["data"]["status"], "succeeded")
        self.assertEqual(sent["data"]["terminal_state"], "succeeded")
        self.assertEqual(sent["data"]["progress"], 1.0)
        self.assertEqual(sent["data"]["message"], "执行完成")

    def test_handle_tool_invoke_sends_failed_result_when_execute_tool_raises(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        sent = {}

        def _capture(msg_type, data=None):
            sent["type"] = msg_type
            sent["data"] = data or {}
            return True

        client._send = _capture

        with mock.patch(
            "autocarto.core.instance_ws_client.execute_tool",
            side_effect=RuntimeError("boom"),
        ):
            client._on_text_message(
                json.dumps(
                    {
                        "type": "tool_invoke",
                        "data": {
                            "request_id": "req-error-1",
                            "tool_name": "open_project",
                            "params": {"path": "/tmp/demo.qgz"},
                        },
                    }
                )
            )

        self.assertEqual(sent["type"], "tool_result")
        self.assertEqual(sent["data"]["request_id"], "req-error-1")
        self.assertFalse(sent["data"]["success"])
        self.assertEqual(sent["data"]["output"], None)
        self.assertIn("boom", sent["data"]["error"])

    def test_handle_tool_timeout_prevents_late_execution(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        callbacks = []
        sent = []

        client._invoke_on_main_thread = lambda callback: callbacks.append(callback)

        def _capture(msg_type, data=None):
            sent.append((msg_type, data or {}))
            return True

        client._send = _capture

        client._on_text_message(
            json.dumps(
                {
                    "type": "tool_invoke",
                    "data": {
                        "request_id": "req-timeout-1",
                        "tool_name": "open_project",
                        "params": {"path": "/tmp/demo.qgz"},
                    },
                }
            )
        )

        client._handle_tool_timeout("req-timeout-1", "open_project")

        with mock.patch(
            "autocarto.core.instance_ws_client.execute_tool",
            return_value={
                "success": True,
                "output": {"tool": "open_project"},
                "error": "",
                "output_layers": [],
            },
        ) as execute_tool_mock:
            callbacks[0]()
            execute_tool_mock.assert_not_called()

        self.assertEqual(len(sent), 1)
        self.assertEqual(sent[0][0], "tool_result")
        self.assertEqual(sent[0][1]["request_id"], "req-timeout-1")
        self.assertFalse(sent[0][1]["success"])
        self.assertIn("timed out", sent[0][1]["error"])

    def test_on_disconnected_emits_reconnecting_state(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        observed = []
        client._reconnect_timer = _FakeReconnectTimer()
        client.connection_changed = _CaptureSignal(
            lambda connected: observed.append((connected, client.is_reconnecting()))
        )

        client._on_disconnected()

        self.assertEqual(observed, [(False, True)])
        self.assertTrue(client._reconnect_timer.isActive())

    def test_send_heartbeat_failure_rearms_reconnect_state_machine(self):
        client = InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

        socket_mock = mock.Mock()
        socket_state = {
            "value": instance_ws_client_module.QAbstractSocket.ConnectedState
        }
        socket_mock.state.side_effect = lambda: socket_state["value"]
        socket_mock.sendTextMessage.side_effect = RuntimeError("broken pipe")
        socket_mock.close.side_effect = lambda: socket_state.__setitem__("value", 0)
        client._socket = socket_mock
        client._reconnect_timer = _FakeReconnectTimer()
        client._heartbeat_timer = mock.Mock()

        observed = []
        client.connection_changed = _CaptureSignal(
            lambda connected: observed.append((connected, client.is_reconnecting()))
        )

        client._send_heartbeat()

        socket_mock.close.assert_called_once()
        client._heartbeat_timer.stop.assert_called_once()
        self.assertTrue(client._reconnect_timer.isActive())
        self.assertEqual(observed, [(False, True)])

if __name__ == "__main__":
    unittest.main()
