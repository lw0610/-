#!/usr/bin/env python3
"""Start only test-owned servers; never evict existing listeners or deploy QGIS."""
import argparse
import os
import signal
import socket
import subprocess
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser()
parser.add_argument("surface", choices=("backend", "frontend"))
args = parser.parse_args()
port = int(os.environ.get("AUTOCARTO_TEST_RUNTIME_PORT" if args.surface == "backend" else "AUTOCARTO_PLAYWRIGHT_FRONTEND_PORT", "50141" if args.surface == "backend" else "50142"))
# Report occupied ports instead of stopping another application.
with socket.socket() as probe:
    probe.bind(("127.0.0.1", port))
with tempfile.TemporaryDirectory(prefix="autocarto-playwright-") as runtime:
    env = os.environ.copy()
    env.update(AUTOCARTO_HOME=runtime, AUTOCARTO_PROJECT_ROOT=str(ROOT), AUTOCARTO_ASSETS_ROOT=str(ROOT / "autocarto"), AUTOCARTO_DISABLE_KEYRING="1")
    if args.surface == "backend":
        # Build once so termination owns the actual service, rather than go run's child.
        binary = Path(runtime) / "agent"
        subprocess.run(["go", "build", "-o", str(binary), "./cmd/agent"], cwd=ROOT / "carto_agent", env=env, check=True)
        command = [str(binary), "-port", str(port)]
        cwd = ROOT / "carto_agent"
    else:
        cwd = ROOT / "autocarto-web"
        if env.get("AUTOCARTO_PLAYWRIGHT_USE_DEV_SERVER") == "1":
            subprocess.run([str(cwd / "node_modules/.bin/next"), "build"], cwd=cwd, env=env, check=True)
        command = [os.environ.get("AUTOCARTO_HTTP_PYTHON", "python3"), "scripts/playwright_refactor_ui_static.py", str(port)]
    child = subprocess.Popen(command, cwd=cwd, env=env)
    def stop(*_):
        if child.poll() is None:
            child.terminate()
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    try:
        code = child.wait()
    finally:
        stop()
        try:
            child.wait(timeout=10)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait()
    raise SystemExit(code)
