"""
统一的日志配置模块
支持全局系统日志 + 会话级日志动态切换

日志目录优先级：
1. init_log_dir() 显式设置（插件启动时从配置系统读取后调用）
2. get_logger() 首次调用时的默认目录 autocarto/data/logs/
"""

import logging
import os
import tempfile
import time
from logging.handlers import RotatingFileHandler

from ..core.paths import get_plugin_data_dir
from .redaction import RedactingFormatter


class _SafeRotatingFileHandler(RotatingFileHandler):
    def _open(self):
        log_dir = os.path.dirname(self.baseFilename)
        if log_dir:
            os.makedirs(log_dir, exist_ok=True)
        return super()._open()

    def emit(self, record):
        log_dir = os.path.dirname(self.baseFilename)
        if log_dir and not os.path.exists(log_dir):
            if self.stream is not None:
                try:
                    self.stream.close()
                except Exception:
                    pass
                self.stream = self._open()
        elif self.stream is not None and not os.path.exists(self.baseFilename):
            try:
                self.stream.close()
            except Exception:
                pass
            self.stream = self._open()
        return super().emit(record)


# 全局标志，确保只初始化一次
_logger_initialized = False

# 全局文件 handler 引用，用于 init_log_dir 切换
_global_file_handler = None
_global_log_dir = None

# 会话级日志 handler
_session_handler = None
_session_log_dir = None

# 默认日志目录（与 config_manager / plugin_manager 一致）
_DEFAULT_LOG_DIR = os.path.join(str(get_plugin_data_dir()), "logs")

# 日志轮转策略（可在插件启动后由配置覆盖）
_log_max_size = 10 * 1024 * 1024
_log_max_backups = 5
_log_max_age = 30
_log_max_total_size = 100 * 1024 * 1024

# 会话日志轮转策略（独立于全局日志）
_session_log_max_size = 5 * 1024 * 1024
_session_log_max_backups = 2


def _normalize_log_dir(log_dir):
    """标准化日志目录路径。"""
    if not log_dir:
        return ""
    return os.path.abspath(os.path.expanduser(log_dir))


def configure_log_policy(
    max_size=None,
    max_backups=None,
    max_age=None,
    max_total_size=None,
    session_max_size=None,
    session_max_backups=None,
):
    """配置日志轮转与清理策略（单位：字节 / 份数 / 天）。"""
    global _log_max_size, _log_max_backups, _log_max_age, _log_max_total_size
    global _session_log_max_size, _session_log_max_backups

    try:
        if max_size is not None:
            size = int(max_size)
            if size > 0:
                _log_max_size = size
    except (TypeError, ValueError):
        pass

    try:
        if max_backups is not None:
            backups = int(max_backups)
            if backups >= 0:
                _log_max_backups = backups
    except (TypeError, ValueError):
        pass

    try:
        if max_age is not None:
            age = int(max_age)
            if age > 0:
                _log_max_age = age
    except (TypeError, ValueError):
        pass

    try:
        if max_total_size is not None:
            total_size = int(max_total_size)
            if total_size > 0:
                _log_max_total_size = total_size
    except (TypeError, ValueError):
        pass

    try:
        if session_max_size is not None:
            size = int(session_max_size)
            if size > 0:
                _session_log_max_size = size
    except (TypeError, ValueError):
        pass

    try:
        if session_max_backups is not None:
            backups = int(session_max_backups)
            if backups >= 0:
                _session_log_max_backups = backups
    except (TypeError, ValueError):
        pass


def _create_file_handler(
    log_dir,
    filename,
    level,
    formatter,
    fallback_subdir="logs",
    max_size=None,
    max_backups=None,
):
    """创建文件日志 handler；首选目标目录，失败时回退到临时目录。"""
    preferred_dir = _normalize_log_dir(log_dir)
    fallback_dir = os.path.join(tempfile.gettempdir(), "autocarto", fallback_subdir)
    candidates = [preferred_dir, fallback_dir]
    resolved_max_size = _log_max_size if max_size is None else max_size
    resolved_max_backups = _log_max_backups if max_backups is None else max_backups

    # 去重并过滤空值
    unique_dirs = []
    for candidate in candidates:
        if candidate and candidate not in unique_dirs:
            unique_dirs.append(candidate)

    last_error = None
    for target_dir in unique_dirs:
        try:
            os.makedirs(target_dir, exist_ok=True)
            log_file = os.path.join(target_dir, filename)
            handler = _SafeRotatingFileHandler(
                log_file,
                mode="a",
                maxBytes=resolved_max_size,
                backupCount=resolved_max_backups,
                encoding="utf-8",
            )
            handler.setLevel(level)
            handler.setFormatter(formatter)
            return handler, target_dir, None
        except Exception as e:
            last_error = e

    return None, None, last_error


def _collect_log_files(log_dir, filename):
    if not log_dir:
        return []
    try:
        names = os.listdir(log_dir)
    except Exception:
        return []

    files = []
    for name in names:
        if name != filename and not name.startswith(f"{filename}."):
            continue
        path = os.path.join(log_dir, name)
        if not os.path.isfile(path):
            continue
        try:
            stat = os.stat(path)
        except Exception:
            continue
        files.append(
            {
                "path": path,
                "mtime": stat.st_mtime,
                "size": int(stat.st_size),
                "active": name == filename,
            }
        )
    return files


def _cleanup_old_logs(log_dir, filename):
    files = _collect_log_files(log_dir, filename)
    if not files:
        return

    if _log_max_age > 0:
        cutoff = time.time() - (_log_max_age * 24 * 60 * 60)
        for item in files:
            if item["active"]:
                continue
            if item["mtime"] < cutoff:
                try:
                    os.remove(item["path"])
                except Exception:
                    pass

    files = _collect_log_files(log_dir, filename)
    if not files or _log_max_total_size <= 0:
        return

    files.sort(
        key=lambda item: (1 if item["active"] else 0, item["mtime"]), reverse=True
    )
    total_size = 0
    for item in files:
        total_size += item["size"]
        if total_size <= _log_max_total_size:
            continue
        if item["active"]:
            continue
        try:
            os.remove(item["path"])
            total_size -= item["size"]
        except Exception:
            pass


def get_logger(name):
    """获取logger实例"""
    global _logger_initialized

    if not _logger_initialized:
        _init_root_logger(_DEFAULT_LOG_DIR)
        _logger_initialized = True

    return logging.getLogger(name)


def _init_root_logger(log_dir):
    """初始化根 logger（内部方法）"""
    global _global_file_handler, _global_log_dir

    root_logger = logging.getLogger()
    root_logger.setLevel(logging.DEBUG)

    # 清除已有的 handlers
    root_logger.handlers.clear()

    # 控制台 handler
    console_handler = logging.StreamHandler()
    console_handler.setLevel(logging.INFO)

    # 格式化
    formatter = RedactingFormatter("%(asctime)s [%(name)s] [%(levelname)s] %(message)s")
    console_handler.setFormatter(formatter)

    # 添加 handlers
    root_logger.addHandler(console_handler)
    _global_file_handler = None
    _global_log_dir = None

    # 文件 handler（全局系统日志，始终写入；失败时回退 /tmp）
    file_handler, actual_log_dir, err = _create_file_handler(
        log_dir, "autocarto.log", logging.DEBUG, formatter, fallback_subdir="logs"
    )
    if file_handler is not None:
        root_logger.addHandler(file_handler)
        _global_file_handler = file_handler
        _global_log_dir = actual_log_dir
        _cleanup_old_logs(actual_log_dir, "autocarto.log")
        if _normalize_log_dir(log_dir) != actual_log_dir:
            root_logger.warning(
                "日志目录不可写，已回退到临时目录: requested=%s, fallback=%s",
                _normalize_log_dir(log_dir),
                actual_log_dir,
            )
    else:
        root_logger.warning("日志文件初始化失败，将仅输出到控制台: %s", err)


def init_log_dir(log_dir):
    """
    重新设置全局日志目录（插件启动时由 initGui 调用）

    将全局文件 handler 切换到指定目录，已有的会话 handler 不受影响。
    如果 log_dir 与当前目录相同则跳过。

    Args:
        log_dir: 日志目录绝对路径
    """
    global _global_file_handler, _global_log_dir

    if not log_dir:
        return

    # 展开 ~ 路径
    log_dir = _normalize_log_dir(log_dir)

    # 与当前目录相同则跳过
    if _global_log_dir and os.path.abspath(_global_log_dir) == log_dir:
        return

    root_logger = logging.getLogger()
    root_logger.setLevel(logging.DEBUG)
    formatter = RedactingFormatter("%(asctime)s [%(name)s] [%(levelname)s] %(message)s")
    file_handler, actual_log_dir, err = _create_file_handler(
        log_dir, "autocarto.log", logging.DEBUG, formatter, fallback_subdir="logs"
    )
    if file_handler is None:
        logging.getLogger(__name__).warning(
            "切换全局日志目录失败，保留当前配置: requested=%s, error=%s",
            log_dir,
            err,
        )
        return

    # 移除旧的全局文件 handler
    if _global_file_handler is not None:
        root_logger.removeHandler(_global_file_handler)
        try:
            _global_file_handler.close()
        except Exception:
            pass

    root_logger.addHandler(file_handler)
    _global_file_handler = file_handler
    _global_log_dir = actual_log_dir
    _cleanup_old_logs(actual_log_dir, "autocarto.log")

    if actual_log_dir != log_dir:
        logging.getLogger(__name__).warning(
            "全局日志目录不可写，已回退到临时目录: requested=%s, fallback=%s",
            log_dir,
            actual_log_dir,
        )
    else:
        logging.getLogger(__name__).info(f"全局日志目录已切换到: {actual_log_dir}")


def switch_session_log(session_log_dir):
    """
    切换会话级日志输出目录

    Args:
        session_log_dir: 会话日志目录路径，为 None 时移除会话日志 handler
    """
    global _session_handler, _session_log_dir

    root_logger = logging.getLogger()

    # 移除旧的会话 handler
    if _session_handler is not None:
        root_logger.removeHandler(_session_handler)
        try:
            _session_handler.close()
        except Exception:
            pass
        _session_handler = None
        _session_log_dir = None

    if not session_log_dir:
        return

    formatter = RedactingFormatter(
        "%(asctime)s [SESSION] [%(name)s] [%(levelname)s] %(message)s"
    )
    handler, actual_session_log_dir, err = _create_file_handler(
        session_log_dir,
        "session.log",
        logging.DEBUG,
        formatter,
        fallback_subdir="session_logs",
        max_size=_session_log_max_size,
        max_backups=_session_log_max_backups,
    )
    if handler is None:
        logging.getLogger(__name__).warning(
            "会话日志目录不可写，已禁用会话日志: requested=%s, error=%s",
            _normalize_log_dir(session_log_dir),
            err,
        )
        return

    root_logger.addHandler(handler)
    _session_handler = handler
    _session_log_dir = actual_session_log_dir

    if _normalize_log_dir(session_log_dir) != actual_session_log_dir:
        logging.getLogger(__name__).warning(
            "会话日志目录不可写，已回退到临时目录: requested=%s, fallback=%s",
            _normalize_log_dir(session_log_dir),
            actual_session_log_dir,
        )
    else:
        session_log_path = (
            os.path.join(actual_session_log_dir, "session.log")
            if actual_session_log_dir
            else "session.log"
        )
        logging.getLogger(__name__).debug(f"会话日志已切换到: {session_log_path}")
