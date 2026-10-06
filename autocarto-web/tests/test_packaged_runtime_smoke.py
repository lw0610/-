from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import pytest
import subprocess
import sys
import textwrap
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
MODULE_PATH = REPO_ROOT / "autocarto-web" / "scripts" / "packaged_runtime_smoke.py"


def load_module() -> Any:
    spec = importlib.util.spec_from_file_location("packaged_runtime_smoke", MODULE_PATH)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def create_fake_packaged_artifact(tmp_path: Path) -> Path:
    artifact_path = tmp_path / "fake-packaged-artifact.py"
    artifact_path.write_text(
        textwrap.dedent(
            """
            #!/usr/bin/env python3
            import json
            import os
            import signal
            import sys
            from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
            from pathlib import Path

            runtime_home = Path(os.environ["AUTOCARTO_HOME"])
            data_dir = runtime_home / "data"
            data_dir.mkdir(parents=True, exist_ok=True)

            class Handler(BaseHTTPRequestHandler):
                def log_message(self, format, *args):
                    return

                def do_GET(self):
                    if self.path != "/api/runtime/descriptor":
                        self.send_response(404)
                        self.end_headers()
                        return
                    requested = self.headers.get("X-AutoCarto-Contract-Version", "")
                    if requested == "1":
                        payload = {
                            "success": False,
                            "code": "HTTP_CONTRACT_VERSION_UNSUPPORTED",
                            "error": {
                                "code": "HTTP_CONTRACT_VERSION_UNSUPPORTED",
                                "message": "不支持的 HTTP runtime/control contract 版本",
                            },
                            "data": {
                                "contract_version": "v1",
                                "requested_contract_version": "1",
                                "supported_contract_versions": ["v1"],
                            },
                        }
                        status = 400
                    elif requested == "v1":
                        payload = {
                            "success": True,
                            "data": {
                                "version": "v1",
                                "readiness": "ready",
                                "degraded": False,
                                "port": server.server_address[1],
                                "backend_url": f"http://127.0.0.1:{server.server_address[1]}",
                                "ws_url": f"ws://127.0.0.1:{server.server_address[1]}/ws",
                            },
                        }
                        status = 200
                    else:
                        payload = {
                            "success": False,
                            "code": "HTTP_CONTRACT_VERSION_UNSUPPORTED",
                            "error": {
                                "code": "HTTP_CONTRACT_VERSION_UNSUPPORTED",
                                "message": "unexpected contract version",
                            },
                            "data": {
                                "contract_version": "v1",
                                "requested_contract_version": requested,
                                "supported_contract_versions": ["v1"],
                            },
                        }
                        status = 400
                    body = json.dumps(payload).encode("utf-8")
                    self.send_response(status)
                    self.send_header("Content-Type", "application/json")
                    self.send_header("Content-Length", str(len(body)))
                    self.send_header("X-AutoCarto-Contract-Version", "v1")
                    self.send_header("X-AutoCarto-Supported-Contract-Versions", "v1")
                    self.end_headers()
                    self.wfile.write(body)

            server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
            (data_dir / ".port").write_text(str(server.server_address[1]), encoding="utf-8")
            print("fake artifact started", flush=True)
            print("fake artifact stderr ready", file=sys.stderr, flush=True)

            def shutdown(*_args):
                server.shutdown()

            signal.signal(signal.SIGTERM, shutdown)
            signal.signal(signal.SIGINT, shutdown)
            server.serve_forever()
            """
        ).strip()
        + "\n",
        encoding="utf-8",
    )
    artifact_path.chmod(0o755)
    return artifact_path


def test_resolve_launch_command_uses_bundle_executable_for_app(tmp_path: Path) -> None:
    module = load_module()
    app_bundle = tmp_path / "AutoCarto AI.app"
    binary = app_bundle / "Contents" / "MacOS" / "AutoCarto AI"
    binary.parent.mkdir(parents=True)
    binary.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
    binary.chmod(0o755)

    command = module.resolve_launch_command(app_bundle)

    assert command == [str(binary)]


def test_validate_descriptor_payload_accepts_ready_v1_contract() -> None:
    module = load_module()
    descriptor = {
        "success": True,
        "data": {
            "version": "v1",
            "readiness": "ready",
            "degraded": False,
            "port": 3000,
            "backend_url": "http://127.0.0.1:3000",
            "ws_url": "ws://127.0.0.1:3000/ws",
        },
    }

    validated = module.validate_descriptor_payload(
        descriptor, expected_contract_version="v1"
    )

    assert validated["version"] == "v1"
    assert validated["readiness"] == "ready"
    assert validated["degraded"] is False


def test_validate_descriptor_payload_uses_shared_unready_message() -> None:
    module = load_module()
    descriptor = {
        "success": True,
        "data": {
            "version": "v1",
            "readiness": "degraded",
            "degraded": True,
            "port": 3000,
            "backend_url": "http://127.0.0.1:3000",
            "ws_url": "ws://127.0.0.1:3000/ws",
            "retry_hints": {"retryable": True, "after_ms": 2000},
        },
    }

    with pytest.raises(
        ValueError,
        match=r"discovery descriptor 未就绪: readiness=degraded, degraded=true, retryable=true, after_ms=2000",
    ):
        module.validate_descriptor_payload(descriptor, expected_contract_version="v1")


def test_validate_descriptor_payload_requires_authoritative_urls() -> None:
    module = load_module()
    descriptor = {
        "success": True,
        "data": {
            "version": "v1",
            "readiness": "ready",
            "degraded": False,
            "port": 3000,
            "backend_url": "",
            "ws_url": "",
        },
    }

    with pytest.raises(
        ValueError, match=r"discovery descriptor 缺少 authoritative urls"
    ):
        module.validate_descriptor_payload(descriptor, expected_contract_version="v1")


def test_validate_legacy_rejection_payload_requires_structured_unsupported_error() -> (
    None
):
    module = load_module()
    payload = {
        "success": False,
        "code": "HTTP_CONTRACT_VERSION_UNSUPPORTED",
        "error": {
            "code": "HTTP_CONTRACT_VERSION_UNSUPPORTED",
            "message": "不支持的 HTTP runtime/control contract 版本",
        },
        "data": {
            "contract_version": "v1",
            "requested_contract_version": "1",
            "supported_contract_versions": ["v1"],
        },
    }

    module.validate_legacy_rejection_payload(
        status_code=400,
        payload=payload,
        expected_contract_version="v1",
        legacy_request_version="1",
    )


def test_build_launch_env_enables_extract_and_run_for_appimage(tmp_path: Path) -> None:
    module = load_module()

    env = module.build_launch_env(
        runtime_home=tmp_path / "runtime-home",
        artifact_path=tmp_path / "AutoCarto-AI.AppImage",
    )

    assert env["AUTOCARTO_HOME"] == str((tmp_path / "runtime-home").resolve())
    assert env["APPIMAGE_EXTRACT_AND_RUN"] == "1"


def test_main_runs_fake_packaged_artifact_and_archives_evidence(tmp_path: Path) -> None:
    artifact_path = create_fake_packaged_artifact(tmp_path)
    runtime_home = tmp_path / "runtime-home"
    evidence_dir = tmp_path / "evidence"

    completed = subprocess.run(
        [
            sys.executable,
            str(MODULE_PATH),
            "--artifact-path",
            str(artifact_path),
            "--runtime-home",
            str(runtime_home),
            "--evidence-dir",
            str(evidence_dir),
            "--expected-contract-version",
            "v1",
            "--timeout-sec",
            "10",
        ],
        check=False,
        capture_output=True,
        text=True,
        timeout=20,
    )

    assert completed.returncode == 0, completed.stdout + completed.stderr
    assert (evidence_dir / "descriptor.json").exists()
    assert (evidence_dir / "legacy_rejection.json").exists()
    assert (evidence_dir / "stdout.log").exists()
    assert (evidence_dir / "stderr.log").exists()
    assert (evidence_dir / "smoke.log").exists()

    descriptor = json.loads(
        (evidence_dir / "descriptor.json").read_text(encoding="utf-8")
    )
    legacy = json.loads(
        (evidence_dir / "legacy_rejection.json").read_text(encoding="utf-8")
    )
    assert descriptor["status_code"] == 200
    assert descriptor["payload"]["data"]["version"] == "v1"
    assert descriptor["payload"]["data"]["readiness"] == "ready"
    assert descriptor["payload"]["data"]["degraded"] is False
    assert legacy["status_code"] == 400
    assert legacy["payload"]["code"] == "HTTP_CONTRACT_VERSION_UNSUPPORTED"


def test_main_creates_placeholder_logs_when_launch_fails(tmp_path: Path) -> None:
    evidence_dir = tmp_path / "failure-evidence"

    completed = subprocess.run(
        [
            sys.executable,
            str(MODULE_PATH),
            "--artifact-path",
            str(tmp_path / "missing-artifact"),
            "--runtime-home",
            str(tmp_path / "runtime-home"),
            "--evidence-dir",
            str(evidence_dir),
            "--expected-contract-version",
            "v1",
            "--timeout-sec",
            "5",
        ],
        check=False,
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert completed.returncode == 1
    assert (evidence_dir / "stdout.log").exists()
    assert (evidence_dir / "stderr.log").exists()
    assert (evidence_dir / "smoke.log").exists()


def test_main_rejects_non_v1_expected_contract_version(tmp_path: Path) -> None:
    artifact_path = create_fake_packaged_artifact(tmp_path)
    evidence_dir = tmp_path / "bad-version-evidence"
    launch_marker = tmp_path / "launched.txt"
    artifact_path.write_text(
        artifact_path.read_text(encoding="utf-8").replace(
            'print("fake artifact started", flush=True)',
            'Path(r"'
            + str(launch_marker)
            + '").write_text("launched", encoding="utf-8")\nprint("fake artifact started", flush=True)',
        ),
        encoding="utf-8",
    )

    completed = subprocess.run(
        [
            sys.executable,
            str(MODULE_PATH),
            "--artifact-path",
            str(artifact_path),
            "--runtime-home",
            str(tmp_path / "runtime-home"),
            "--evidence-dir",
            str(evidence_dir),
            "--expected-contract-version",
            "v2",
            "--timeout-sec",
            "5",
        ],
        check=False,
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert completed.returncode == 1
    assert not launch_marker.exists()
    assert "v1" in (evidence_dir / "smoke.log").read_text(encoding="utf-8")


def test_terminate_process_uses_taskkill_tree_on_windows(monkeypatch: Any) -> None:
    module = load_module()
    commands: list[list[str]] = []

    class FakeProcess:
        pid = 4321
        returncode = None

        def poll(self) -> None:
            return None

        def wait(self, timeout: int) -> int:
            return 0

    def fake_run(command: list[str], check: bool = False, **_kwargs: Any) -> None:
        assert check is False
        commands.append(command)

    monkeypatch.setattr(module, "is_windows_platform", lambda: True, raising=False)
    monkeypatch.setattr(module.subprocess, "run", fake_run)

    module.terminate_process(FakeProcess(), module.logging.getLogger("test"))

    assert commands == [["taskkill", "/PID", "4321", "/T", "/F"]]
