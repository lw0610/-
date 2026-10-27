import importlib
import json
import logging
import math
from typing import Any, Callable, Dict, Optional, Tuple
from autocarto.utils.redaction import redact_sensitive

logger = logging.getLogger(__name__)

_main_thread_runner: Optional[Callable[[Callable[[], Any]], Any]] = None
_main_thread_runner_failed = False


def qgis_json_default(obj: Any) -> Any:
    if hasattr(obj, "isNull") and callable(obj.isNull):
        if obj.isNull():
            return None
        if hasattr(obj, "value") and callable(obj.value):
            return obj.value()
        if hasattr(obj, "toPyObject") and callable(obj.toPyObject):
            return obj.toPyObject()

    if hasattr(obj, "toPyDateTime") and callable(obj.toPyDateTime):
        value = obj.toPyDateTime()
        isoformat = getattr(value, "isoformat", None)
        return isoformat() if callable(isoformat) else str(value)
    if hasattr(obj, "toPyDate") and callable(obj.toPyDate):
        value = obj.toPyDate()
        isoformat = getattr(value, "isoformat", None)
        return isoformat() if callable(isoformat) else str(value)
    if hasattr(obj, "toPyTime") and callable(obj.toPyTime):
        value = obj.toPyTime()
        isoformat = getattr(value, "isoformat", None)
        return isoformat() if callable(isoformat) else str(value)

    import datetime as _dt

    if isinstance(obj, (_dt.datetime, _dt.date, _dt.time)):
        return obj.isoformat()

    if hasattr(obj, "data") and hasattr(obj, "size") and callable(obj.size):
        try:
            size = obj.size()
            return f"<binary:{size}bytes>"
        except Exception:
            pass

    if hasattr(obj, "value") and not callable(obj.value):
        return obj.value
    try:
        return int(obj)
    except (TypeError, ValueError):
        pass

    if isinstance(obj, bytes):
        return f"<binary:{len(obj)}bytes>"

    if isinstance(obj, (set, frozenset)):
        return list(obj)

    return str(obj)


def _sanitize_for_json(value: Any) -> Tuple[Any, int]:
    replaced = 0

    def _walk(node: Any) -> Any:
        nonlocal replaced

        if node is None or isinstance(node, (bool, str)):
            return node

        if isinstance(node, float):
            if not math.isfinite(node):
                replaced += 1
                return None
            return node

        if isinstance(node, int):
            return node

        if isinstance(node, dict):
            return {str(key): _walk(child) for key, child in node.items()}

        if isinstance(node, (list, tuple)):
            return [_walk(child) for child in node]

        replaced += 1
        converted = qgis_json_default(node)
        if isinstance(converted, (dict, list)):
            return _walk(converted)
        return converted

    return _walk(value), replaced


def safe_json_dumps(payload: Any) -> Tuple[str, int]:
    try:
        return json.dumps(payload, ensure_ascii=False, allow_nan=False), 0
    except (ValueError, TypeError):
        sanitized, replaced = _sanitize_for_json(payload)
        return json.dumps(sanitized, ensure_ascii=False, allow_nan=False), replaced


def extract_tool_error(result: Dict[str, Any]) -> str:
    """Keep explicit errors, or both a legacy summary and its exception detail."""
    for key in ("error", "message"):
        value = result.get(key)
        if isinstance(value, str):
            if value.strip():
                return value.strip()
            continue
        if value:
            return str(value)

    info = result.get("info")
    summary = str(info).strip() if info else ""
    output = result.get("output")
    detail = output.strip() if isinstance(output, str) else ""
    if summary and detail and detail not in summary:
        # Executors traditionally put the exception in output and only a
        # generic "handler 执行失败" in info. Neither field should hide the other.
        return f"{summary}: {detail}" if summary not in detail else detail
    return summary or detail or "工具执行失败"


def normalize_tool_result(result: Any) -> Any:
    result = redact_sensitive(result)
    if not isinstance(result, dict):
        return result

    if "success" not in result:
        return result

    normalized = dict(result)
    success = normalized.get("success") is True
    if success:
        normalized.setdefault("error", "")
        return normalized

    normalized["error"] = extract_tool_error(normalized)
    return normalized


def _resolve_main_thread_runner() -> Optional[Callable[[Callable[[], Any]], Any]]:
    global _main_thread_runner, _main_thread_runner_failed
    if _main_thread_runner is not None:
        return _main_thread_runner
    if _main_thread_runner_failed:
        return None

    try:
        from autocarto.tools.system._shared.layout_runtime import run_in_main_thread

        _main_thread_runner = run_in_main_thread
    except Exception:
        logger.debug("主线程执行器加载失败，回退为直接调用", exc_info=True)
        _main_thread_runner_failed = True
        return None
    return _main_thread_runner


def call_tool_handler(
    handler: Callable[[Dict[str, Any]], Any], params: Dict[str, Any]
) -> Any:
    runner = _resolve_main_thread_runner()
    if runner is None:
        try:
            qt_core = importlib.import_module("qgis.PyQt.QtCore")
            QCoreApplication = getattr(qt_core, "QCoreApplication")
            QThread = getattr(qt_core, "QThread")
            app = QCoreApplication.instance()
            if app is not None and QThread.currentThread() != app.thread():
                raise RuntimeError("主线程执行器不可用，已拒绝在后台线程执行 QGIS 工具")
        except ImportError:
            pass
        return normalize_tool_result(handler(params))

    return normalize_tool_result(runner(lambda: handler(params)))
