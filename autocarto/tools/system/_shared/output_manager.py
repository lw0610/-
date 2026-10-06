import importlib
import logging
import os
from types import SimpleNamespace
from typing import Any, Dict, List, Optional, Tuple

logger = logging.getLogger(__name__)

OUTPUT_DIR_NAME = "autoc"
SESSION_SUBDIRS = {"logs", "exports", "temp", "snapshots", "intermediate", "output"}

_current_session = None


def set_session_context(session):
    global _current_session
    _current_session = session
    if session and session.session_dir:
        logger.debug("output_manager 会话上下文已更新: %s", session.session_id)


def _build_session_subdirs(session_dir: str, raw_subdirs: Any) -> Dict[str, str]:
    subdirs: Dict[str, str] = {}

    if isinstance(raw_subdirs, dict):
        for key, value in raw_subdirs.items():
            normalized_key = _resolve_subdir_alias(str(key or ""))
            if not normalized_key:
                continue
            normalized_value = str(value or "").strip()
            if not normalized_value:
                continue
            subdirs[normalized_key] = normalized_value

    for name in SESSION_SUBDIRS:
        if name in subdirs and subdirs[name]:
            continue
        subdirs[name] = os.path.join(session_dir, name)

    return subdirs


def _recover_session_from_params(params: Dict[str, Any]):
    if not isinstance(params, dict):
        return None

    session_dir = str(params.get("session_dir") or "").strip()
    raw_subdirs = params.get("session_subdirs")

    if not session_dir and isinstance(raw_subdirs, dict):
        session_dir = str(raw_subdirs.get("root") or "").strip()

    if not session_dir:
        return None

    session_id = str(params.get("session_id") or "").strip()
    if not session_id:
        session_id = os.path.basename(os.path.normpath(session_dir)) or "runtime"

    project_path = str(params.get("project_path") or "").strip()
    session_subdirs = _build_session_subdirs(session_dir, raw_subdirs)

    return SimpleNamespace(
        session_id=session_id,
        project_path=project_path,
        session_dir=session_dir,
        session_subdirs=session_subdirs,
    )


def _ensure_active_session(params: Dict[str, Any]):
    restored = sync_session_context_from_params(params)
    if restored is not None:
        return restored

    if _current_session and getattr(_current_session, "session_dir", ""):
        return _current_session

    return None


def clear_session_context():
    global _current_session
    _current_session = None
    return _current_session


def sync_session_context_from_params(params: Dict[str, Any]):
    global _current_session
    restored = _recover_session_from_params(params)
    if restored is None:
        return _current_session

    current_session_id = str(getattr(_current_session, "session_id", "") or "").strip()
    current_session_dir = str(
        getattr(_current_session, "session_dir", "") or ""
    ).strip()
    if (
        current_session_id != restored.session_id
        or current_session_dir != restored.session_dir
    ):
        _current_session = restored
        logger.warning(
            "output_manager 从工具参数恢复会话上下文: %s", restored.session_id
        )
    return _current_session


def get_project_output_dir() -> str:
    project_path = ""
    try:
        qgis_core = importlib.import_module("qgis.core")
        project_class = getattr(qgis_core, "QgsProject", None)
        if project_class is not None:
            project = project_class.instance()
            if project is not None:
                project_path = project.fileName()
    except Exception:
        project_path = ""

    if project_path:
        project_dir = os.path.dirname(project_path)
        output_dir = os.path.join(project_dir, OUTPUT_DIR_NAME)
    else:
        home_dir = os.path.expanduser("~")
        output_dir = os.path.join(home_dir, OUTPUT_DIR_NAME)

    return output_dir


def ensure_output_dir() -> str:
    output_dir = get_project_output_dir()

    if not os.path.exists(output_dir):
        os.makedirs(output_dir, exist_ok=True)

    return output_dir


def _resolve_subdir_alias(subdir: Optional[str]) -> Optional[str]:
    candidate = str(subdir or "").strip().lower()
    if not candidate:
        return None
    if candidate in SESSION_SUBDIRS:
        return candidate
    return None


def get_output_subdir(subdir: str) -> str:
    normalized_subdir = _normalize_subdir(subdir)

    if not _current_session or not _current_session.session_dir:
        fallback_root = os.environ.get("AUTOCARTO_OUTPUT_ROOT", "").strip()
        if not fallback_root:
            fallback_root = os.path.join(ensure_output_dir(), "fallback")
        fallback_dir = os.path.join(os.path.abspath(fallback_root), normalized_subdir)
        if not os.path.exists(fallback_dir):
            os.makedirs(fallback_dir, exist_ok=True)
        return fallback_dir

    if _current_session.session_subdirs:
        session_sub = _current_session.session_subdirs.get(normalized_subdir)
        if session_sub:
            if not os.path.exists(session_sub):
                os.makedirs(session_sub, exist_ok=True)
            return session_sub

    sub_path = os.path.join(_current_session.session_dir, normalized_subdir)
    if not os.path.exists(sub_path):
        os.makedirs(sub_path, exist_ok=True)
    if _current_session.session_subdirs is not None:
        _current_session.session_subdirs[normalized_subdir] = sub_path
    return sub_path


def _split_path_parts(path: str) -> List[str]:
    normalized = path.replace("\\", "/")
    return [part for part in normalized.split("/") if part and part != "."]


def _sanitize_tail_parts(parts: List[str], fallback_name: str) -> List[str]:
    sanitized = [part for part in parts if part and part not in (".", "..")]
    if not sanitized:
        fallback = os.path.basename(fallback_name) or "output"
        sanitized = [fallback]
    return sanitized


def _normalize_subdir(subdir: Optional[str], default_subdir: str = "exports") -> str:
    resolved = _resolve_subdir_alias(subdir)
    if resolved:
        return resolved
    fallback = _resolve_subdir_alias(default_subdir)
    if fallback:
        return fallback
    return "exports"


def _infer_target_subdir_and_tail(
    user_path: str, default_subdir: str, default_filename: str
) -> Tuple[str, List[str]]:
    parts = _split_path_parts(user_path)
    default_sub = _normalize_subdir(default_subdir)
    is_abs = os.path.isabs(user_path)

    if not parts:
        return default_sub, [os.path.basename(default_filename) or "output"]

    if len(parts) >= 2 and parts[0] == "exports" and parts[1] == "temp":
        return "temp", _sanitize_tail_parts(parts[2:], default_filename)

    if len(parts) >= 2 and parts[0] == "autoc":
        resolved_subdir = _resolve_subdir_alias(parts[1])
        if resolved_subdir:
            return resolved_subdir, _sanitize_tail_parts(parts[2:], default_filename)

    if not is_abs:
        resolved_subdir = _resolve_subdir_alias(parts[0])
        if resolved_subdir:
            return resolved_subdir, _sanitize_tail_parts(parts[1:], default_filename)

    for index in range(len(parts) - 1):
        if parts[index] == "autoc":
            inferred_sub = _resolve_subdir_alias(parts[index + 1])
            if not inferred_sub:
                continue
            tail = parts[index + 2 :]
            return inferred_sub, _sanitize_tail_parts(tail, default_filename)

    for index in range(len(parts) - 3):
        if parts[index] == "sessions":
            inferred_sub = _resolve_subdir_alias(parts[index + 2])
            if not inferred_sub:
                continue
            tail = parts[index + 3 :]
            return inferred_sub, _sanitize_tail_parts(tail, default_filename)

    if is_abs:
        tail = [
            os.path.basename(user_path)
            or os.path.basename(default_filename)
            or "output"
        ]
    else:
        tail = parts

    return default_sub, _sanitize_tail_parts(tail, default_filename)


def resolve_output_path(
    user_path: Optional[str], default_filename: str, subdir: Optional[str] = None
) -> str:
    default_subdir = _normalize_subdir(subdir)

    if not user_path:
        base_dir = get_output_subdir(default_subdir)
        full_path = os.path.join(
            base_dir, os.path.basename(default_filename) or "output"
        )
        parent_dir = os.path.dirname(full_path)
        if parent_dir and not os.path.exists(parent_dir):
            os.makedirs(parent_dir, exist_ok=True)
        return full_path

    raw_path = str(user_path).strip()
    inferred_subdir, tail_parts = _infer_target_subdir_and_tail(
        raw_path, default_subdir, default_filename
    )
    base_dir = get_output_subdir(inferred_subdir)
    full_path = os.path.join(base_dir, *tail_parts)
    full_path = os.path.abspath(full_path)

    base_abs = os.path.abspath(base_dir)
    try:
        in_base = os.path.commonpath([base_abs, full_path]) == base_abs
    except ValueError:
        in_base = False
    if not in_base:
        fallback_name = os.path.basename(default_filename) or "output"
        full_path = os.path.join(base_abs, fallback_name)

    parent_dir = os.path.dirname(full_path)
    if parent_dir and not os.path.exists(parent_dir):
        os.makedirs(parent_dir, exist_ok=True)

    if os.path.abspath(raw_path) != full_path:
        logger.debug("输出路径规范化: %s -> %s", raw_path, full_path)

    return full_path


def get_output_info_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        session = _ensure_active_session(params)
        if not session or not session.session_dir:
            return {
                "success": False,
                "output": "",
                "info": "当前无活跃会话，无法获取会话输出目录信息",
            }

        output_dir = session.session_dir
        project_path = session.project_path or ""

        files = []
        if os.path.exists(output_dir):
            for file_name in os.listdir(output_dir):
                file_path = os.path.join(output_dir, file_name)
                if os.path.isfile(file_path):
                    size = os.path.getsize(file_path)
                    files.append(
                        {
                            "name": file_name,
                            "size": size,
                            "size_human": _format_size(size),
                        }
                    )
                elif os.path.isdir(file_path):
                    files.append({"name": file_name + "/", "type": "directory"})

        info = {
            "output_dir": output_dir,
            "project_saved": bool(project_path),
            "project_path": project_path or "未保存",
            "files_count": len(
                [
                    item
                    for item in files
                    if "type" not in item or item["type"] != "directory"
                ]
            ),
            "subdirs_count": len(
                [item for item in files if item.get("type") == "directory"]
            ),
            "contents": files[:20],
        }

        return {"success": True, "output": output_dir, "info": info}

    except Exception as exc:
        return {
            "success": False,
            "output": "",
            "info": f"获取输出目录信息失败: {str(exc)}",
        }


def init_output_dir_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        session = _ensure_active_session(params)
        if not session or not session.session_dir:
            return {
                "success": False,
                "output": "",
                "info": "当前无活跃会话，无法初始化输出目录",
            }

        session_dir = session.session_dir
        create_subdirs = bool(params.get("create_subdirs", True))
        subdirs = []

        if create_subdirs:
            for subdir in sorted(SESSION_SUBDIRS):
                subdir_path = os.path.join(session_dir, subdir)
                if not os.path.exists(subdir_path):
                    os.makedirs(subdir_path, exist_ok=True)
                subdirs.append(subdir)
                if session.session_subdirs is not None:
                    session.session_subdirs[subdir] = subdir_path

        return {
            "success": True,
            "output": session_dir,
            "info": {
                "message": f"会话输出目录已初始化: {session_dir}",
                "session_id": session.session_id,
                "subdirs_created": subdirs,
            },
        }

    except Exception as exc:
        return {
            "success": False,
            "output": "",
            "info": f"初始化输出目录失败: {str(exc)}",
        }


def clean_temp_files_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        _ensure_active_session(params)
        temp_dir = get_output_subdir("temp")
        keep_days = params.get("keep_days", 0)

        import time

        now = time.time()
        cutoff = now - (keep_days * 86400)

        removed_files = []
        errors = []

        if os.path.exists(temp_dir):
            for file_name in os.listdir(temp_dir):
                file_path = os.path.join(temp_dir, file_name)
                try:
                    if (
                        os.path.isfile(file_path)
                        and os.path.getmtime(file_path) < cutoff
                    ):
                        os.remove(file_path)
                        removed_files.append(file_name)
                except Exception as exc:
                    errors.append(f"{file_name}: {str(exc)}")

        return {
            "success": True,
            "output": str(len(removed_files)),
            "info": {
                "message": f"已清理 {len(removed_files)} 个临时文件",
                "temp_dir": temp_dir,
                "removed_files": removed_files,
                "errors": errors if errors else None,
            },
        }

    except Exception as exc:
        return {"success": False, "output": "", "info": f"清理临时文件失败: {str(exc)}"}


def _format_size(size: int) -> str:
    if size < 1024:
        return f"{int(size)} B"
    size_value = float(size)
    for unit in ["B", "KB", "MB", "GB"]:
        if size_value < 1024:
            return f"{size_value:.1f} {unit}"
        size_value /= 1024
    return f"{size_value:.1f} TB"
