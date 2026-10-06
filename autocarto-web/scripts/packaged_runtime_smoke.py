#!/usr/bin/env python3

from __future__ import annotations

import argparse
import json
import logging
import os
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any


RUNTIME_DESCRIPTOR_PATH = "/api/runtime/descriptor"
CONTRACT_VERSION_HEADER = "X-AutoCarto-Contract-Version"
SUPPORTED_CONTRACT_VERSIONS_HEADER = "X-AutoCarto-Supported-Contract-Versions"
LEGACY_REQUEST_VERSION = "1"
AUTHORITATIVE_CONTRACT_VERSION = "v1"
LEGACY_CONTRACT_REJECTION_CODE = "HTTP_CONTRACT_VERSION_UNSUPPORTED"
PORT_FILE_NAME = ".port"
SMOKE_LOG_NAME = "smoke.log"
STDOUT_LOG_NAME = "stdout.log"
STDERR_LOG_NAME = "stderr.log"
DESCRIPTOR_EVIDENCE_NAME = "descriptor.json"
LEGACY_REJECTION_EVIDENCE_NAME = "legacy_rejection.json"


class HTTPResponseRecord:
    def __init__(
        self,
        status_code: int,
        headers: dict[str, str],
        body_text: str,
        payload: Any | None,
    ) -> None:
        self.status_code = status_code
        self.headers = headers
        self.body_text = body_text
        self.payload = payload

    def as_evidence(self) -> dict[str, Any]:
        return {
            "status_code": self.status_code,
            "headers": self.headers,
            "body_text": self.body_text,
            "payload": self.payload,
        }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "启动真实打包桌面产物，并验证 authoritative runtime contract："
            "version=v1、readiness=ready、degraded=false、"
            "backend_url/ws_url 非空，且 legacy contract 请求被明确拒绝。"
        )
    )
    parser.add_argument("--artifact-path", required=True, help="真实打包产物路径")
    parser.add_argument("--runtime-home", required=True, help="AUTOCARTO_HOME 路径")
    parser.add_argument("--evidence-dir", required=True, help="证据输出目录")
    parser.add_argument(
        "--expected-contract-version",
        required=True,
        help="期望的 HTTP/discovery contract 版本",
    )
    parser.add_argument(
        "--timeout-sec",
        type=float,
        default=60.0,
        help="等待 runtime descriptor ready 的超时时间（秒）",
    )
    return parser.parse_args()


def configure_logger(smoke_log_path: Path) -> logging.Logger:
    logger = logging.getLogger("packaged_runtime_smoke")
    logger.setLevel(logging.INFO)
    logger.handlers.clear()
    logger.propagate = False

    formatter = logging.Formatter("%(asctime)s %(levelname)s %(message)s")

    file_handler = logging.FileHandler(smoke_log_path, encoding="utf-8")
    file_handler.setFormatter(formatter)
    logger.addHandler(file_handler)

    stream_handler = logging.StreamHandler(sys.stdout)
    stream_handler.setFormatter(formatter)
    logger.addHandler(stream_handler)

    return logger


def write_json(path: Path, payload: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as handle:
        json.dump(payload, handle, ensure_ascii=False, indent=2, sort_keys=True)
        handle.write("\n")


def ensure_file_exists(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.touch(exist_ok=True)


def is_windows_platform() -> bool:
    return os.name == "nt"


def resolve_launch_command(artifact_path: Path) -> list[str]:
    if not artifact_path.exists():
        raise FileNotFoundError(f"打包产物不存在：{artifact_path}")
    if artifact_path.suffix.lower() == ".app":
        return [str(resolve_macos_bundle_executable(artifact_path))]
    if artifact_path.is_dir():
        raise ValueError(
            f"artifact-path 必须指向可执行产物或 .app bundle，而不是目录：{artifact_path}"
        )
    if not os.access(artifact_path, os.X_OK):
        raise PermissionError(f"打包产物不可执行：{artifact_path}")
    return [str(artifact_path)]


def resolve_macos_bundle_executable(app_bundle: Path) -> Path:
    macos_dir = app_bundle / "Contents" / "MacOS"
    if not macos_dir.is_dir():
        raise FileNotFoundError(f".app 缺少 Contents/MacOS：{app_bundle}")

    preferred_name = app_bundle.stem
    candidates = [
        entry
        for entry in sorted(macos_dir.iterdir())
        if entry.is_file() and os.access(entry, os.X_OK)
    ]
    if not candidates:
        raise FileNotFoundError(f".app 内未找到可执行文件：{macos_dir}")

    for candidate in candidates:
        if candidate.name == preferred_name:
            return candidate
    return candidates[0]


def prepare_runtime_home(runtime_home: Path, logger: logging.Logger) -> Path:
    config_dir = runtime_home / "config"
    data_dir = runtime_home / "data"
    config_dir.mkdir(parents=True, exist_ok=True)
    data_dir.mkdir(parents=True, exist_ok=True)

    port_file = data_dir / PORT_FILE_NAME
    if port_file.exists():
        logger.info("移除旧的 runtime port 文件: %s", port_file)
        port_file.unlink()
    return port_file


def build_launch_env(runtime_home: Path, artifact_path: Path) -> dict[str, str]:
    env = os.environ.copy()
    env["AUTOCARTO_HOME"] = str(runtime_home.resolve())
    if artifact_path.suffix.lower() == ".appimage":
        env["APPIMAGE_EXTRACT_AND_RUN"] = "1"
    return env


def launch_artifact(
    command: list[str],
    artifact_path: Path,
    runtime_home: Path,
    stdout_path: Path,
    stderr_path: Path,
    logger: logging.Logger,
) -> tuple[subprocess.Popen[str], Any, Any]:
    env = build_launch_env(runtime_home=runtime_home, artifact_path=artifact_path)

    stdout_handle = stdout_path.open("wb")
    stderr_handle = stderr_path.open("wb")
    popen_kwargs: dict[str, Any] = {
        "stdin": subprocess.DEVNULL,
        "stdout": stdout_handle,
        "stderr": stderr_handle,
        "env": env,
        "cwd": str(Path(command[0]).resolve().parent),
    }
    if os.name == "nt":
        popen_kwargs["creationflags"] = getattr(
            subprocess, "CREATE_NEW_PROCESS_GROUP", 0
        )
    else:
        popen_kwargs["start_new_session"] = True

    logger.info("启动打包产物: %s", command[0])
    process = subprocess.Popen(command, **popen_kwargs)
    logger.info("打包产物进程已启动: pid=%s", process.pid)
    return process, stdout_handle, stderr_handle


def read_runtime_port(port_file: Path) -> int | None:
    if not port_file.exists():
        return None
    raw = port_file.read_text(encoding="utf-8").strip()
    if not raw:
        return None
    port = int(raw)
    if port <= 0:
        raise ValueError(f"runtime 端口无效：{raw}")
    return port


def decode_json_body(body_text: str) -> Any | None:
    if not body_text.strip():
        return None
    try:
        return json.loads(body_text)
    except json.JSONDecodeError:
        return None


def http_get_descriptor(port: int, contract_version: str) -> HTTPResponseRecord:
    request = urllib.request.Request(
        f"http://127.0.0.1:{port}{RUNTIME_DESCRIPTOR_PATH}",
        method="GET",
        headers={CONTRACT_VERSION_HEADER: contract_version},
    )
    try:
        with urllib.request.urlopen(request, timeout=3.0) as response:
            body_text = response.read().decode("utf-8", errors="replace")
            return HTTPResponseRecord(
                status_code=response.getcode(),
                headers={key: value for key, value in response.headers.items()},
                body_text=body_text,
                payload=decode_json_body(body_text),
            )
    except urllib.error.HTTPError as exc:
        body_text = exc.read().decode("utf-8", errors="replace")
        return HTTPResponseRecord(
            status_code=exc.code,
            headers={key: value for key, value in exc.headers.items()},
            body_text=body_text,
            payload=decode_json_body(body_text),
        )


def validate_descriptor_payload(
    payload: Any, expected_contract_version: str
) -> dict[str, Any]:
    if expected_contract_version != AUTHORITATIVE_CONTRACT_VERSION:
        raise ValueError(
            "packaged runtime smoke 当前只支持权威 contract 版本 "
            f"{AUTHORITATIVE_CONTRACT_VERSION}，收到 {expected_contract_version!r}"
        )
    if not isinstance(payload, dict):
        raise ValueError("descriptor 响应不是 JSON object")
    if payload.get("success") is not True:
        raise ValueError(f"descriptor 响应 success 不是 true: {payload!r}")
    data = payload.get("data")
    if not isinstance(data, dict):
        raise ValueError(f"descriptor 响应缺少 data object: {payload!r}")

    version = str(data.get("version", "")).strip()
    readiness = str(data.get("readiness", "")).strip()
    degraded = data.get("degraded")
    port = data.get("port")
    backend_url = str(data.get("backend_url", "")).strip()
    ws_url = str(data.get("ws_url", "")).strip()
    retry_hints = data.get("retry_hints")
    retryable = False
    after_ms = 0
    if isinstance(retry_hints, dict):
        retryable = bool(retry_hints.get("retryable"))
        raw_after_ms = retry_hints.get("after_ms")
        if isinstance(raw_after_ms, (int, float)):
            after_ms = int(raw_after_ms)

    if version != expected_contract_version:
        raise ValueError(
            f"discovery descriptor contract version 不受支持: {version or '<empty>'}"
        )
    if not isinstance(port, int) or port <= 0 or not backend_url or not ws_url:
        raise ValueError("discovery descriptor 缺少 authoritative urls")
    if readiness != "ready" or degraded is not False:
        raise ValueError(
            "discovery descriptor 未就绪: "
            f"readiness={readiness or '<empty>'}, "
            f"degraded={str(bool(degraded)).lower()}, "
            f"retryable={str(retryable).lower()}, "
            f"after_ms={after_ms}"
        )
    return data


def validate_legacy_rejection_payload(
    status_code: int,
    payload: Any,
    expected_contract_version: str,
    legacy_request_version: str,
) -> None:
    if expected_contract_version != AUTHORITATIVE_CONTRACT_VERSION:
        raise ValueError(
            "packaged runtime smoke 当前只支持权威 contract 版本 "
            f"{AUTHORITATIVE_CONTRACT_VERSION}，收到 {expected_contract_version!r}"
        )
    if status_code != 400:
        raise ValueError(f"legacy contract 请求未返回 400: status={status_code}")
    if not isinstance(payload, dict):
        raise ValueError("legacy contract 响应不是 JSON object")

    error = payload.get("error")
    error_code = None
    if isinstance(error, dict):
        error_code = error.get("code")
    if not error_code:
        error_code = payload.get("code")
    if error_code != LEGACY_CONTRACT_REJECTION_CODE:
        raise ValueError(f"legacy contract 响应 code 异常: {error_code!r}")

    data = payload.get("data")
    if not isinstance(data, dict):
        raise ValueError(f"legacy contract 响应缺少 data object: {payload!r}")
    if data.get("requested_contract_version") != legacy_request_version:
        raise ValueError(
            "legacy contract 响应未回显被拒绝的旧版本: "
            f"{data.get('requested_contract_version')!r}"
        )
    if data.get("contract_version") != expected_contract_version:
        raise ValueError(
            f"legacy contract 响应 contract_version 异常: {data.get('contract_version')!r}"
        )
    supported_versions = data.get("supported_contract_versions")
    if not isinstance(supported_versions, list):
        raise ValueError(
            f"legacy contract 响应缺少 supported_contract_versions: {supported_versions!r}"
        )
    if expected_contract_version not in supported_versions:
        raise ValueError(
            f"supported_contract_versions 未包含期望版本 {expected_contract_version}: {supported_versions!r}"
        )
    if legacy_request_version in supported_versions:
        raise ValueError(
            f"supported_contract_versions 错误地包含 legacy 版本 {legacy_request_version}: {supported_versions!r}"
        )


def wait_for_ready_descriptor(
    process: subprocess.Popen[str],
    port_file: Path,
    expected_contract_version: str,
    timeout_sec: float,
    logger: logging.Logger,
    descriptor_evidence_path: Path,
    legacy_rejection_evidence_path: Path,
) -> tuple[int, HTTPResponseRecord]:
    deadline = time.monotonic() + timeout_sec
    last_error = f"等待 {port_file} 就绪"
    last_response: HTTPResponseRecord | None = None
    launcher_exit_code: int | None = None
    last_reported_port: int | None = None
    legacy_checked = False

    while time.monotonic() < deadline:
        polled = process.poll()
        if polled is not None and launcher_exit_code is None:
            launcher_exit_code = polled
            logger.warning("打包产物进程提前退出: code=%s", polled)

        try:
            port = read_runtime_port(port_file)
        except Exception as exc:
            last_error = f"读取 runtime port 文件失败: {exc}"
            time.sleep(0.25)
            continue

        if port is None:
            if launcher_exit_code is not None:
                last_error = f"打包产物已退出且未写入 runtime port 文件: code={launcher_exit_code}"
            time.sleep(0.25)
            continue

        if port != last_reported_port:
            logger.info("发现 authoritative runtime port: %s", port)
            last_reported_port = port
        try:
            response = http_get_descriptor(port, expected_contract_version)
        except Exception as exc:
            last_error = f"请求 descriptor 失败: {exc}"
            time.sleep(0.25)
            continue

        last_response = response
        write_json(descriptor_evidence_path, response.as_evidence())

        if not legacy_checked:
            try:
                run_legacy_request_check(
                    port=port,
                    expected_contract_version=expected_contract_version,
                    evidence_path=legacy_rejection_evidence_path,
                    logger=logger,
                )
                legacy_checked = True
            except Exception as exc:
                raise RuntimeError(f"legacy request 校验失败: {exc}") from exc

        if response.status_code != 200:
            last_error = f"descriptor HTTP 状态未就绪: {response.status_code}"
            time.sleep(0.25)
            continue

        try:
            validate_descriptor_payload(response.payload, expected_contract_version)
            return port, response
        except Exception as exc:
            last_error = str(exc)
            time.sleep(0.25)

    if last_response is None:
        write_json(
            descriptor_evidence_path,
            {"error": last_error, "available": False, "port_file": str(port_file)},
        )
    if not legacy_checked:
        write_json(
            legacy_rejection_evidence_path,
            {
                "available": False,
                "error": "runtime descriptor 从未成功返回任何 HTTP 响应，无法执行 legacy request 校验",
                "port_file": str(port_file),
            },
        )
    raise RuntimeError(last_error)


def run_legacy_request_check(
    port: int,
    expected_contract_version: str,
    evidence_path: Path,
    logger: logging.Logger,
) -> None:
    logger.info(
        "执行 legacy request 校验: header %s=%s",
        CONTRACT_VERSION_HEADER,
        LEGACY_REQUEST_VERSION,
    )
    response = http_get_descriptor(port, LEGACY_REQUEST_VERSION)
    write_json(evidence_path, response.as_evidence())
    validate_legacy_rejection_payload(
        status_code=response.status_code,
        payload=response.payload,
        expected_contract_version=expected_contract_version,
        legacy_request_version=LEGACY_REQUEST_VERSION,
    )


def terminate_process(process: subprocess.Popen[str], logger: logging.Logger) -> None:
    if process.poll() is not None:
        logger.info("打包产物进程已结束: code=%s", process.returncode)
        return

    logger.info("准备清理打包产物进程: pid=%s", process.pid)
    try:
        if is_windows_platform():
            subprocess.run(
                ["taskkill", "/PID", str(process.pid), "/T", "/F"],
                check=False,
            )
        else:
            os.killpg(process.pid, signal.SIGTERM)
        process.wait(timeout=15)
        logger.info("打包产物进程已优雅退出")
        return
    except subprocess.TimeoutExpired:
        logger.warning("打包产物进程未在 15 秒内退出，尝试强制终止")
    except ProcessLookupError:
        logger.info("进程组已不存在，视为已清理")
        return

    if is_windows_platform():
        process.kill()
    else:
        os.killpg(process.pid, signal.SIGKILL)
    process.wait(timeout=15)
    logger.info("打包产物进程已强制终止")


def run_smoke(args: argparse.Namespace, logger: logging.Logger) -> None:
    artifact_path = Path(args.artifact_path).expanduser().resolve()
    runtime_home = Path(args.runtime_home).expanduser().resolve()
    evidence_dir = Path(args.evidence_dir).expanduser().resolve()
    evidence_dir.mkdir(parents=True, exist_ok=True)

    descriptor_evidence_path = evidence_dir / DESCRIPTOR_EVIDENCE_NAME
    legacy_evidence_path = evidence_dir / LEGACY_REJECTION_EVIDENCE_NAME
    stdout_path = evidence_dir / STDOUT_LOG_NAME
    stderr_path = evidence_dir / STDERR_LOG_NAME

    ensure_file_exists(stdout_path)
    ensure_file_exists(stderr_path)

    if args.expected_contract_version != AUTHORITATIVE_CONTRACT_VERSION:
        raise ValueError(
            "packaged runtime smoke 当前只支持权威 contract 版本 "
            f"{AUTHORITATIVE_CONTRACT_VERSION}，收到 {args.expected_contract_version!r}"
        )

    logger.info("artifact-path=%s", artifact_path)
    logger.info("runtime-home=%s", runtime_home)
    logger.info("evidence-dir=%s", evidence_dir)
    logger.info("expected-contract-version=%s", args.expected_contract_version)
    logger.info("timeout-sec=%s", args.timeout_sec)

    command = resolve_launch_command(artifact_path)
    port_file = prepare_runtime_home(runtime_home, logger)

    process: subprocess.Popen[str] | None = None
    stdout_handle = None
    stderr_handle = None
    try:
        process, stdout_handle, stderr_handle = launch_artifact(
            command=command,
            artifact_path=artifact_path,
            runtime_home=runtime_home,
            stdout_path=stdout_path,
            stderr_path=stderr_path,
            logger=logger,
        )
        _port, descriptor_response = wait_for_ready_descriptor(
            process=process,
            port_file=port_file,
            expected_contract_version=args.expected_contract_version,
            timeout_sec=args.timeout_sec,
            logger=logger,
            descriptor_evidence_path=descriptor_evidence_path,
            legacy_rejection_evidence_path=legacy_evidence_path,
        )
        write_json(descriptor_evidence_path, descriptor_response.as_evidence())
        logger.info("packaged runtime smoke 验证通过")
    finally:
        if process is not None:
            terminate_process(process, logger)
        if stdout_handle is not None:
            stdout_handle.close()
        if stderr_handle is not None:
            stderr_handle.close()


def main() -> int:
    args = parse_args()
    evidence_dir = Path(args.evidence_dir).expanduser().resolve()
    evidence_dir.mkdir(parents=True, exist_ok=True)
    logger = configure_logger(evidence_dir / SMOKE_LOG_NAME)

    try:
        run_smoke(args, logger)
        return 0
    except Exception as exc:
        logger.exception("packaged runtime smoke 失败: %s", exc)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
