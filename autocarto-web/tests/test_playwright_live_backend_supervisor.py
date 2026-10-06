from __future__ import annotations

import importlib.util
import signal
from pathlib import Path
from typing import Any, cast

from pytest import MonkeyPatch


REPO_ROOT = Path(__file__).resolve().parents[2]
MODULE_PATH = (
    REPO_ROOT / "autocarto-web" / "scripts" / "playwright_live_backend_supervisor.py"
)


def load_supervisor_module() -> Any:
    spec = importlib.util.spec_from_file_location(
        "playwright_live_backend_supervisor", MODULE_PATH
    )
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_start_child_launches_agent_in_new_session_and_records_pid(
    tmp_path: Path, monkeypatch: MonkeyPatch
) -> None:
    module = load_supervisor_module()
    module.LOG_FILE = tmp_path / "supervisor.log"
    module.CHILD_PID_FILE = tmp_path / "child.pid"

    calls: list[tuple[str, object, dict[str, Any]]] = []

    class FakeProcess:
        pid: int = 4321

        def poll(self) -> None:
            return None

    def fake_popen(command: object, **kwargs: Any) -> FakeProcess:
        calls.append(("popen", command, kwargs))
        return FakeProcess()

    monkeypatch.setattr(module.subprocess, "Popen", fake_popen)

    supervisor = module.BackendSupervisor()
    supervisor.start_child()

    assert len(calls) == 1
    kind, command, kwargs = calls[0]
    assert kind == "popen"
    assert command == ["go", "run", "./cmd/agent"]
    assert kwargs["start_new_session"] is True
    assert module.CHILD_PID_FILE.read_text(encoding="utf-8") == "4321"


def test_prepare_live_environment_deploys_plugin_after_evicting_qgis(
    tmp_path: Path, monkeypatch: MonkeyPatch
) -> None:
    module = load_supervisor_module()
    module.LOG_FILE = tmp_path / "supervisor.log"

    call_order: list[str] = []

    def fake_evict_qgis() -> None:
        call_order.append("evict_qgis")

    def fake_deploy() -> Path:
        call_order.append("deploy")
        return tmp_path / "plugins" / "autocarto"

    monkeypatch.setattr(module, "evict_qgis_processes", fake_evict_qgis)
    monkeypatch.setattr(module, "deploy_plugin_for_live_tests", fake_deploy)

    module.prepare_live_environment()

    assert call_order == ["evict_qgis", "deploy"]


def test_restart_if_requested_restarts_backend_without_redeploying_plugin(
    tmp_path: Path, monkeypatch: MonkeyPatch
) -> None:
    module = load_supervisor_module()
    module.LOG_FILE = tmp_path / "supervisor.log"
    module.CHILD_PID_FILE = tmp_path / "child.pid"
    module.RESTART_FILE = tmp_path / "restart.flag"
    module.RESTART_FILE.write_text("1", encoding="utf-8")

    class FakeProcess:
        pid: int = 9876

        def poll(self) -> None:
            return None

        def wait(self, timeout: int | None = None) -> int:
            return 0

    def fake_popen(command: object, **kwargs: Any) -> FakeProcess:
        return FakeProcess()

    monkeypatch.setattr(
        module,
        "evict_qgis_processes",
        lambda: (_ for _ in ()).throw(
            AssertionError("should not evict qgis on backend restart")
        ),
    )
    monkeypatch.setattr(
        module,
        "deploy_plugin_for_live_tests",
        lambda: (_ for _ in ()).throw(
            AssertionError("should not redeploy plugin on backend restart")
        ),
    )
    monkeypatch.setattr(module.subprocess, "Popen", fake_popen)
    monkeypatch.setattr(module.os, "killpg", lambda *_args, **_kwargs: None)

    supervisor = module.BackendSupervisor()
    supervisor.child = FakeProcess()
    supervisor.restart_if_requested()

    assert not module.RESTART_FILE.exists()


def test_evict_qgis_processes_invokes_process_cleanup(monkeypatch: MonkeyPatch) -> None:
    module = load_supervisor_module()
    calls: list[tuple[object, bool]] = []

    def fake_run(command: object, check: bool = False, **kwargs: Any) -> None:
        calls.append((command, check))

    monkeypatch.setattr(module.subprocess, "run", fake_run)

    module.evict_qgis_processes()

    assert len(calls) == 1
    command, check = calls[0]
    command = cast(list[str], command)
    assert check is False
    assert command[:2] == ["bash", "-lc"]
    assert "pgrep -x qgis.bin" in command[2]
    assert "kill -9" in command[2]


def test_evict_listeners_invokes_port_cleanup(monkeypatch: MonkeyPatch) -> None:
    module = load_supervisor_module()
    calls: list[tuple[object, bool]] = []

    def fake_run(command: object, check: bool = False, **kwargs: Any) -> None:
        calls.append((command, check))

    monkeypatch.setattr(module.subprocess, "run", fake_run)

    module.evict_listeners(50002)

    assert len(calls) == 1
    command, check = calls[0]
    command = cast(list[str], command)
    assert check is False
    assert command[:2] == ["bash", "-lc"]
    assert "lsof -ti:50002" in command[2]


def test_main_evicts_ports_before_binding_proxy(
    monkeypatch: MonkeyPatch, tmp_path: Path
) -> None:
    module = load_supervisor_module()
    events: list[str] = []

    class _StopEvent:
        def is_set(self) -> bool:
            return True

        def set(self) -> None:
            events.append("stop-event.set")

    class _FakeSupervisor:
        def __init__(self) -> None:
            self.stop_event = _StopEvent()

        def start_child(self) -> None:
            events.append("start_child")

        def cleanup(self) -> None:
            events.append("cleanup")

    class _FakeProxy:
        def __init__(self, *_args: Any, **_kwargs: Any) -> None:
            events.append("proxy-bind")

        def serve_forever(self) -> None:
            events.append("proxy-serve")

        def shutdown(self) -> None:
            events.append("proxy-shutdown")

        def server_close(self) -> None:
            events.append("proxy-close")

    class _FakeThread:
        def __init__(self, *args: Any, **kwargs: Any) -> None:
            self._target = kwargs.get("target")

        def start(self) -> None:
            events.append("thread-start")

    monkeypatch.setattr(module, "LOG_FILE", tmp_path / "live.log")
    monkeypatch.setattr(module, "SUPERVISOR_PID_FILE", tmp_path / "supervisor.pid")
    monkeypatch.setattr(module, "CHILD_PID_FILE", tmp_path / "child.pid")
    monkeypatch.setattr(module, "RESTART_FILE", tmp_path / "restart.flag")
    monkeypatch.setattr(module, "ensure_tmp_dir", lambda: None)
    monkeypatch.setattr(module, "install_signal_handlers", lambda _handler: None)
    monkeypatch.setattr(
        module, "evict_listeners", lambda port: events.append(f"evict:{port}")
    )
    monkeypatch.setattr(
        module, "prepare_live_environment", lambda: events.append("prepare")
    )
    monkeypatch.setattr(module, "BackendSupervisor", _FakeSupervisor)
    monkeypatch.setattr(module, "ThreadedTCPServer", _FakeProxy)
    monkeypatch.setattr(module.threading, "Thread", _FakeThread)

    module.main()

    assert events[:6] == [
        "evict:3000",
        "evict:50002",
        "prepare",
        "proxy-bind",
        "thread-start",
        "start_child",
    ]


def test_stop_child_terminates_child_process_group(
    tmp_path: Path, monkeypatch: MonkeyPatch
) -> None:
    module = load_supervisor_module()
    module.LOG_FILE = tmp_path / "supervisor.log"
    module.CHILD_PID_FILE = tmp_path / "child.pid"
    module.CHILD_PID_FILE.write_text("4321", encoding="utf-8")

    class FakeProcess:
        pid: int = 4321

        def __init__(self):
            self.wait_calls: list[int] = []

        def poll(self) -> None:
            return None

        def wait(self, timeout: int | None = None) -> int:
            self.wait_calls.append(cast(int, timeout))
            return 0

    fake_process = FakeProcess()
    killpg_calls: list[tuple[int, signal.Signals]] = []

    def fake_killpg(pgid: int, sig: signal.Signals) -> None:
        killpg_calls.append((pgid, sig))

    monkeypatch.setattr(module.os, "killpg", fake_killpg)

    supervisor = module.BackendSupervisor()
    supervisor.child = fake_process
    supervisor.stop_child()

    assert killpg_calls == [(4321, signal.SIGTERM)]
    assert fake_process.wait_calls == [15]
    assert supervisor.child is None
    assert not module.CHILD_PID_FILE.exists()


def test_install_signal_handlers_registers_sighup_when_available(
    monkeypatch: MonkeyPatch,
) -> None:
    module = load_supervisor_module()
    registered: list[tuple[object, object]] = []

    def fake_signal(sig: object, handler: object) -> None:
        registered.append((sig, handler))

    monkeypatch.setattr(module.signal, "signal", fake_signal)

    handler = lambda signum, frame: None
    module.install_signal_handlers(handler)

    expected = {module.signal.SIGINT, module.signal.SIGTERM}
    if hasattr(module.signal, "SIGHUP"):
        expected.add(module.signal.SIGHUP)

    assert {sig for sig, _ in registered} == expected
    assert {registered_handler for _, registered_handler in registered} == {handler}
