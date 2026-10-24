"""Resource fixtures come from Go; Python never interprets resource tables."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading

REPO = Path(__file__).resolve().parents[3]
_temporary = None
_binary = None
_lock = threading.Lock()


def load_handle(name):
    source = REPO / "autocarto/tests/fixtures/resource_handles" / (name + ".json")
    return json.loads(source.read_text(encoding="utf-8").replace("${REPO}", str(REPO)))


def _resolver_binary():
    global _temporary, _binary
    with _lock:
        if _binary is None:
            _temporary = tempfile.TemporaryDirectory(prefix="autocarto-resource-helper-")
            _binary = Path(_temporary.name) / ("resource-fixture.exe" if os.name == "nt" else "resource-fixture")
            result = subprocess.run(["go", "build", "-o", str(_binary), "./integration/resource-fixture"],
                                    cwd=REPO / "carto_agent", capture_output=True, text=True)
            if result.returncode:
                _binary = None
                raise RuntimeError("Go 资源测试辅助程序构建失败：" + result.stderr)
        return _binary


def resolve_handle(path, expected_type, *, builtin_dir=None, user_dir=None, flow_dir=None, flow_id="fixture-flow", base_dir=None):
    """Resolve dynamically generated test resources through the production Go resolver."""
    command = [str(_resolver_binary()), "-type", expected_type, "-path", str(path), "-flow-id", flow_id]
    for flag, value in (("builtin", builtin_dir), ("user", user_dir), ("flow-dir", flow_dir), ("base-dir", base_dir)):
        if value is not None:
            command += ["-" + flag, str(value)]
    env = os.environ.copy()
    with tempfile.TemporaryDirectory(prefix="autocarto-resource-fixture-home-") as runtime:
        env.update(AUTOCARTO_PROJECT_ROOT=str(REPO), AUTOCARTO_ASSETS_ROOT=str(REPO / "autocarto"),
                   AUTOCARTO_HOME=runtime, AUTOCARTO_DISABLE_KEYRING="1")
        result = subprocess.run(command, cwd=REPO, env=env, capture_output=True, text=True)
    if result.returncode:
        raise ValueError("Go 资源解析失败：" + result.stderr.strip())
    return json.loads(result.stdout)
