from pathlib import Path
from typing import Optional

from .paths import get_autocarto_home
from ..utils.logger import get_logger

logger = get_logger(__name__)

_RUNTIME_PORT_FILE = ".port"


def get_runtime_port_file_path() -> Path:
    runtime_home = get_autocarto_home()
    return runtime_home / "data" / _RUNTIME_PORT_FILE


def read_runtime_port() -> Optional[int]:
    port_file = get_runtime_port_file_path()
    if not port_file.exists():
        return None
    try:
        port = int(port_file.read_text(encoding="utf-8").strip())
    except Exception as exc:
        logger.debug("failed to read runtime port file: %s", exc)
        return None
    if 1 <= port <= 65535:
        return port
    logger.debug("invalid runtime port file content: %s", port)
    return None


def build_runtime_backend_url() -> str:
    port = read_runtime_port()
    if not port:
        return ""
    return f"http://127.0.0.1:{port}"


def build_runtime_ws_url(*, role: Optional[str] = None) -> str:
    port = read_runtime_port()
    if not port:
        return ""
    url = f"ws://127.0.0.1:{port}/ws"
    normalized_role = (role or "").strip()
    if normalized_role:
        return f"{url}?role={normalized_role}"
    return url
