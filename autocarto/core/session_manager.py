"""会话上下文管理器。"""

from __future__ import annotations

import hashlib
import os
from dataclasses import dataclass
from typing import Any, Dict, Optional

from .paths import get_plugin_data_dir
from ..utils.logger import get_logger, switch_session_log

logger = get_logger(__name__)

SESSION_SUBDIRS = ("logs", "intermediate", "output", "temp", "snapshots", "exports")

_output_manager_import_warned = False


def _resolve_output_manager():
    """按需获取 output_manager，避免在模块导入阶段被永久卡死。"""
    global _output_manager_import_warned
    try:
        from ..tools.system._shared import output_manager

        _output_manager_import_warned = False
        return output_manager
    except Exception as exc:  # pragma: no cover
        if not _output_manager_import_warned:
            logger.warning("output_manager 模块暂不可用，会话上下文联动将延迟: %s", exc)
            _output_manager_import_warned = True
        return None


@dataclass
class SessionContext:
    session_id: str
    project_path: str
    session_dir: str
    session_subdirs: Dict[str, str]


class SessionManager:
    """维护当前活跃会话，并联动输出目录与会话日志。"""

    def __init__(self):
        self._current: Optional[SessionContext] = None

    def current(self) -> Optional[SessionContext]:
        return self._current

    def activate(
        self,
        session_id: str,
        project_path: str = "",
        session_dir: str = "",
        session_subdirs: Optional[Dict[str, str]] = None,
    ) -> Optional[SessionContext]:
        session_id = str(session_id or "").strip()
        if not session_id:
            return None

        context = self._build_context(
            session_id, project_path, session_dir, session_subdirs
        )
        if (
            self._current
            and self._current.session_id == context.session_id
            and self._current.session_dir == context.session_dir
        ):
            # 同会话重复激活时仍需同步一次，避免 output_manager 上下文丢失后无法自愈。
            self._current = context
            self._apply_context(context)
            return self._current

        self._current = context
        self._apply_context(context)
        logger.info("会话上下文已激活: %s", context.session_id)
        return context

    def activate_from_tool_params(
        self, params: Dict[str, Any]
    ) -> Optional[SessionContext]:
        if not isinstance(params, dict):
            return self._current

        project_path = str(params.get("project_path") or "").strip()
        session_dir = str(params.get("session_dir") or "").strip()

        raw_subdirs = params.get("session_subdirs")
        subdirs: Optional[Dict[str, str]] = None
        if isinstance(raw_subdirs, dict):
            if not session_dir:
                root_dir = str(raw_subdirs.get("root") or "").strip()
                if root_dir:
                    session_dir = root_dir
            subdirs = {}
            for key, value in raw_subdirs.items():
                normalized_key = key
                if (
                    normalized_key in SESSION_SUBDIRS
                    and isinstance(value, str)
                    and value.strip()
                ):
                    subdirs[normalized_key] = value.strip()

        session_id = str(params.get("session_id") or "").strip()
        if not session_id and session_dir:
            session_id = os.path.basename(os.path.normpath(session_dir))
        if not session_id:
            return self._current

        return self.activate(
            session_id, project_path, session_dir=session_dir, session_subdirs=subdirs
        )

    def ensure_runtime_context(
        self, project_path: str = ""
    ) -> Optional[SessionContext]:
        """当工具调用缺少 session_id 时，降级创建运行时会话上下文。"""
        if self._current and self._current.session_dir:
            self._apply_context(self._current)
            return self._current

        normalized_project = str(project_path or "").strip()
        seed = normalized_project or "unsaved_project"
        digest = hashlib.sha1(seed.encode("utf-8")).hexdigest()[:12]
        runtime_session_id = f"runtime_{digest}"
        logger.warning(
            "工具调用缺少会话元数据，已回退到运行时会话: %s", runtime_session_id
        )
        return self.activate(runtime_session_id, normalized_project)

    def clear(self):
        self._current = None
        output_manager = _resolve_output_manager()
        if output_manager is not None:
            try:
                output_manager.set_session_context(None)
            except Exception as exc:
                logger.debug("清理 output_manager 会话上下文失败: %s", exc)
        switch_session_log(None)

    def reset_session_log_if_active(self, session_id: str, logs_dir: str = "") -> bool:
        """仅在当前会话匹配时重建会话日志 handler。"""
        target_session_id = str(session_id or "").strip()
        if not target_session_id or self._current is None:
            return False
        if self._current.session_id != target_session_id:
            return False

        target_logs_dir = str(
            logs_dir or ""
        ).strip() or self._current.session_subdirs.get("logs", "")
        if target_logs_dir:
            self._current.session_subdirs["logs"] = target_logs_dir

        switch_session_log(None)
        if target_logs_dir:
            switch_session_log(target_logs_dir)

        logger.debug("会话日志句柄已重建: session_id=%s", target_session_id)
        return True

    def _build_context(
        self,
        session_id: str,
        project_path: str,
        session_dir: str,
        session_subdirs: Optional[Dict[str, str]],
    ) -> SessionContext:
        resolved_session_dir = session_dir.strip() if session_dir else ""
        if not resolved_session_dir:
            base_dir = self._resolve_sessions_base_dir(project_path)
            resolved_session_dir = os.path.join(base_dir, session_id)

        subdirs: Dict[str, str] = {}
        for name in SESSION_SUBDIRS:
            candidate = ""
            if session_subdirs:
                candidate = str(session_subdirs.get(name) or "").strip()
            if self._is_session_subdir_path(resolved_session_dir, candidate):
                subdirs[name] = candidate
            else:
                subdirs[name] = os.path.join(resolved_session_dir, name)

        return SessionContext(
            session_id=session_id,
            project_path=project_path,
            session_dir=resolved_session_dir,
            session_subdirs=subdirs,
        )

    def _resolve_sessions_base_dir(self, project_path: str) -> str:
        normalized = os.path.abspath(project_path) if project_path else ""
        if normalized:
            project_dir = os.path.dirname(normalized)
            project_name = (
                os.path.splitext(os.path.basename(normalized))[0].strip() or "project"
            )
            return os.path.join(project_dir, "autoc", project_name, "sessions")
        return str(get_plugin_data_dir() / "sessions" / "no_project")

    def _is_session_subdir_path(self, session_dir: str, candidate: str) -> bool:
        if not session_dir or not candidate:
            return False
        try:
            session_abs = os.path.abspath(session_dir)
            candidate_abs = os.path.abspath(candidate)
            return os.path.commonpath([session_abs, candidate_abs]) == session_abs
        except ValueError:
            return False

    def _apply_context(self, context: SessionContext):
        output_manager = _resolve_output_manager()
        if output_manager is not None:
            try:
                output_manager.set_session_context(context)
            except Exception as exc:
                logger.debug("更新 output_manager 会话上下文失败: %s", exc)

        switch_session_log(context.session_subdirs.get("logs"))
