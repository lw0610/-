#!/usr/bin/env python3

import os
import selectors
import shutil
import signal
import socket
import socketserver
import subprocess
import threading
import time
from pathlib import Path
from typing import Any, Callable, Optional, cast


WEB_ROOT = Path(__file__).resolve().parent.parent
REPO_ROOT = WEB_ROOT.parent
PLUGIN_SOURCE_DIR = REPO_ROOT / "autocarto"
TMP_DIR = WEB_ROOT / ".tmp"
RESTART_FILE = TMP_DIR / "playwright-live-backend.restart"
SUPERVISOR_PID_FILE = TMP_DIR / "playwright-live-backend-supervisor.pid"
CHILD_PID_FILE = TMP_DIR / "playwright-live-backend-child.pid"
LOG_FILE = TMP_DIR / "playwright-live-backend-supervisor.log"
UPSTREAM_HOST = "127.0.0.1"
UPSTREAM_PORT = 3000
PROXY_PORT = 50002


def resolve_qgis_plugins_dir() -> Path:
    override = os.environ.get("AUTOCARTO_QGIS_PLUGINS_DIR", "").strip()
    if override:
        return Path(override).expanduser()
    return Path.home() / ".local/share/QGIS/QGIS3/profiles/default/python/plugins"


def deploy_plugin_for_live_tests() -> Path:
    target_plugins_dir = resolve_qgis_plugins_dir()
    if not target_plugins_dir.exists():
        raise FileNotFoundError(f"QGIS 插件目录不存在：{target_plugins_dir}")
    if not PLUGIN_SOURCE_DIR.exists():
        raise FileNotFoundError(f"AutoCarto 插件源码目录不存在：{PLUGIN_SOURCE_DIR}")

    target_plugin_dir = target_plugins_dir / "autocarto"
    if target_plugin_dir.exists():
        shutil.rmtree(target_plugin_dir)

    shutil.copytree(
        PLUGIN_SOURCE_DIR,
        target_plugin_dir,
        ignore=shutil.ignore_patterns("logs", ".session", "__pycache__", "*.pyc"),
    )
    log(f"deploy_plugin_for_live_tests: target={target_plugin_dir}")
    return target_plugin_dir


def evict_qgis_processes() -> None:
    log("evict_qgis_processes: begin")
    _ = subprocess.run(
        [
            "bash",
            "-lc",
            "pgrep -x qgis.bin | xargs -r kill >/dev/null 2>&1 || true; "
            "pgrep -x qgis | xargs -r kill >/dev/null 2>&1 || true; "
            "sleep 1; "
            "pgrep -x qgis.bin | xargs -r kill -9 >/dev/null 2>&1 || true; "
            "pgrep -x qgis | xargs -r kill -9 >/dev/null 2>&1 || true",
        ],
        check=False,
    )
    log("evict_qgis_processes: complete")


def prepare_live_environment() -> None:
    evict_qgis_processes()
    deploy_plugin_for_live_tests()


def ensure_tmp_dir() -> None:
    TMP_DIR.mkdir(parents=True, exist_ok=True)


def log(message: str) -> None:
    timestamp = time.strftime("%Y-%m-%d %H:%M:%S")
    LOG_FILE.parent.mkdir(parents=True, exist_ok=True)
    with LOG_FILE.open("a", encoding="utf-8") as fh:
        _ = fh.write(f"[{timestamp}] {message}\n")


def evict_listeners(port: int) -> None:
    log(f"evict_listeners: port={port}")
    _ = subprocess.run(
        [
            "bash",
            "-lc",
            f"lsof -ti:{port} | xargs -r kill >/dev/null 2>&1 || true",
        ],
        check=False,
    )


def install_signal_handlers(handler: Callable[[int, Any], None]) -> None:
    for sig in (signal.SIGINT, signal.SIGTERM):
        _ = signal.signal(sig, handler)
    if hasattr(signal, "SIGHUP"):
        _ = signal.signal(signal.SIGHUP, handler)


def copy_bidirectional(client: socket.socket, upstream: socket.socket) -> None:
    selector = selectors.DefaultSelector()
    selector.register(client, selectors.EVENT_READ, upstream)
    selector.register(upstream, selectors.EVENT_READ, client)

    try:
        while True:
            events = selector.select(timeout=1.0)
            if not events:
                continue
            for key, _ in events:
                source = cast(socket.socket, key.fileobj)
                target = cast(socket.socket, key.data)
                data = source.recv(65536)
                if not data:
                    return
                target.sendall(data)
    finally:
        try:
            selector.close()
        except Exception:
            pass
        for sock in (client, upstream):
            try:
                sock.shutdown(socket.SHUT_RDWR)
            except Exception:
                pass
            try:
                sock.close()
            except Exception:
                pass


class ProxyHandler(socketserver.BaseRequestHandler):
    def handle(self) -> None:
        request = cast(socket.socket, self.request)
        try:
            upstream = socket.create_connection(
                (UPSTREAM_HOST, UPSTREAM_PORT), timeout=2.0
            )
        except OSError:
            try:
                request.close()
            except Exception:
                pass
            return
        copy_bidirectional(request, upstream)


class ThreadedTCPServer(socketserver.ThreadingMixIn, socketserver.TCPServer):
    allow_reuse_address: bool = True
    daemon_threads: bool = True


class BackendSupervisor:
    def __init__(self) -> None:
        self.child: Optional[subprocess.Popen[bytes]] = None
        self.stop_event: threading.Event = threading.Event()

    def start_child(self) -> None:
        log("start_child: begin")
        self.stop_child()
        env = os.environ.copy()
        _ = env.setdefault("AUTOCARTO_HOME", str(Path.home() / ".autocarto"))
        child = subprocess.Popen(
            ["go", "run", "./cmd/agent"],
            cwd=WEB_ROOT.parent / "carto_agent",
            env=env,
            start_new_session=True,
        )
        self.child = child
        _ = CHILD_PID_FILE.write_text(str(child.pid), encoding="utf-8")
        log(f"start_child: pid={child.pid}")

    def stop_child(self) -> None:
        if self.child is None:
            return
        log(f"stop_child: pid={self.child.pid}")
        if self.child.poll() is None:
            try:
                os.killpg(self.child.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                _ = self.child.wait(timeout=15)
            except subprocess.TimeoutExpired:
                log(f"stop_child: pid={self.child.pid} timeout, killing")
                try:
                    os.killpg(self.child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                _ = self.child.wait(timeout=15)
        self.child = None
        if CHILD_PID_FILE.exists():
            CHILD_PID_FILE.unlink()
        log("stop_child: complete")

    def restart_if_requested(self) -> None:
        if RESTART_FILE.exists():
            log("restart_if_requested: flag detected")
            RESTART_FILE.unlink()
            self.start_child()

    def restart_if_crashed(self) -> None:
        if self.child is None or self.child.poll() is not None:
            log("restart_if_crashed: restarting child")
            self.start_child()

    def cleanup(self) -> None:
        log("cleanup: begin")
        self.stop_event.set()
        self.stop_child()
        for path in (RESTART_FILE, SUPERVISOR_PID_FILE, CHILD_PID_FILE):
            if path.exists():
                path.unlink()
        log("cleanup: complete")


def main() -> None:
    ensure_tmp_dir()
    _ = LOG_FILE.write_text("", encoding="utf-8")
    log("main: start")
    _ = SUPERVISOR_PID_FILE.write_text(str(os.getpid()), encoding="utf-8")
    supervisor = BackendSupervisor()
    evict_listeners(UPSTREAM_PORT)
    evict_listeners(PROXY_PORT)
    prepare_live_environment()
    proxy = ThreadedTCPServer((UPSTREAM_HOST, PROXY_PORT), ProxyHandler)
    proxy_thread = threading.Thread(target=proxy.serve_forever, daemon=True)
    proxy_thread.start()
    supervisor.start_child()

    def handle_signal(signum: int, _frame: Any) -> None:
        log(f"handle_signal: signum={signum}")
        supervisor.cleanup()
        proxy.shutdown()
        proxy.server_close()
        raise SystemExit(0)

    install_signal_handlers(handle_signal)

    try:
        while not supervisor.stop_event.is_set():
            supervisor.restart_if_requested()
            supervisor.restart_if_crashed()
            time.sleep(1)
    except Exception as exc:
        log(f"main: exception={exc!r}")
        raise
    finally:
        log("main: finally")
        supervisor.cleanup()
        proxy.shutdown()
        proxy.server_close()


if __name__ == "__main__":
    main()
