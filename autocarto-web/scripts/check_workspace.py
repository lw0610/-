#!/usr/bin/env python3
"""Build checks in a disposable source tree, leaving a running dev tree intact.

Next 15's static export resets its internal cache to .next even with distDir set.
The source snapshot, rather than distDir, therefore provides the isolation.
"""
import argparse
import os
from pathlib import Path
import shutil
import signal
import subprocess


EXCLUDED = shutil.ignore_patterns(
    "node_modules", ".git", ".next", ".next-*", "out", "target", "binaries",
    ".tools", ".env", ".env.*", "*.tsbuildinfo", "test-results",
    "playwright-report", "__pycache__", "*.pyc", ".deps-lock.sha256",
)


def prepare_workspace(source: Path, destination: Path) -> Path:
    """Copy editable frontend/Tauri sources; share only dependencies and inputs."""
    source = source.resolve()
    destination.mkdir(parents=True, exist_ok=False)
    web = destination / "autocarto-web"
    shutil.copytree(source / "autocarto-web", web, ignore=EXCLUDED)
    (web / "node_modules").symlink_to(source / "autocarto-web/node_modules", target_is_directory=True)
    # build.rs reads the Go module and the bundle's asset resources. Its binary,
    # generated schemas, and Cargo/Next output all belong to the copied frontend.
    for name in ("carto_agent", "autocarto"):
        (destination / name).symlink_to(source / name, target_is_directory=True)
    return web


def prepare_assets(source: Path, destination: Path) -> Path:
    """Settings endpoints can edit built-in package config: never share it."""
    destination.mkdir(parents=True, exist_ok=False)
    for name in ("skills", "tools", "flows", "resources"):
        shutil.copytree(source / name, destination / name, ignore=EXCLUDED)
    return destination


def run_owned(command, *, cwd, env) -> int:
    """Terminate only this invocation's subprocess tree, including build workers."""
    child = subprocess.Popen(command, cwd=cwd, env=env, start_new_session=os.name != "nt")
    previous = {}

    def interrupted(signum, _frame):
        raise SystemExit(128 + signum)

    def terminate(sig):
        try:
            if os.name != "nt":
                os.killpg(child.pid, sig)
            elif child.poll() is None:
                child.terminate() if sig == signal.SIGTERM else child.kill()
        except ProcessLookupError:
            pass

    try:
        for sig in (signal.SIGINT, signal.SIGTERM):
            previous[sig] = signal.signal(sig, interrupted)
        return child.wait()
    finally:
        # Include workers even if the direct child has already exited.
        terminate(signal.SIGTERM)
        try:
            child.wait(timeout=10)
        except subprocess.TimeoutExpired:
            terminate(signal.SIGKILL)
            child.wait()
        for sig, handler in previous.items():
            signal.signal(sig, handler)


def build_frontend(web: Path, env=None) -> None:
    env = dict(os.environ if env is None else env)
    # With output:export this variable changes the export directory, not the
    # build cache. The scratch tree owns both the default .next and out folders.
    env.pop("AUTOCARTO_NEXT_DIST_DIR", None)
    command = [str(web / "node_modules/.bin/next"), "build"]
    code = run_owned(command, cwd=web, env=env)
    if code:
        raise subprocess.CalledProcessError(code, command)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    prepare = sub.add_parser("prepare")
    prepare.add_argument("source", type=Path)
    prepare.add_argument("destination", type=Path)
    build = sub.add_parser("build")
    build.add_argument("web", type=Path)
    args = parser.parse_args()
    if args.action == "prepare":
        prepare_workspace(args.source, args.destination)
    else:
        build_frontend(args.web.resolve())


if __name__ == "__main__":
    main()
