#!/usr/bin/env python3
"""Start only test-owned servers; never evict existing listeners or deploy QGIS."""
import argparse
import os
import socket
import subprocess
import sys
import tempfile
from pathlib import Path

from check_workspace import build_frontend, prepare_assets, prepare_workspace, run_owned

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
        env["AUTOCARTO_ASSETS_ROOT"] = str(prepare_assets(ROOT / "autocarto", Path(runtime) / "assets"))
        # Build once so termination owns the actual service, rather than go run's child.
        binary = Path(runtime) / "agent"
        build = ["go", "build", "-o", str(binary), "./cmd/agent"]
        code = run_owned(build, cwd=ROOT / "carto_agent", env=env)
        if code:
            raise subprocess.CalledProcessError(code, build)
        command = [str(binary), "-port", str(port)]
        cwd = ROOT / "carto_agent"
    else:
        cwd = ROOT / "autocarto-web"
        if env.get("AUTOCARTO_PLAYWRIGHT_USE_DEV_SERVER") == "1" and not env.get("AUTOCARTO_REFACTOR_UI_EXPORT_DIR"):
            snapshot = prepare_workspace(ROOT, Path(runtime) / "project")
            build_frontend(snapshot, env)
            env["AUTOCARTO_REFACTOR_UI_EXPORT_DIR"] = str(snapshot / "out")
        command = [env.get("AUTOCARTO_HTTP_PYTHON", sys.executable), "scripts/playwright_refactor_ui_static.py", str(port)]
    code = run_owned(command, cwd=cwd, env=env)
    raise SystemExit(code)
