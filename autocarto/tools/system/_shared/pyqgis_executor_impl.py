import builtins
import importlib
import logging
import threading
import time
import uuid
import warnings
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from typing import Any, Callable, Dict, Optional

from autocarto.core.tool_engine.runtime import qgis_json_default

logger = logging.getLogger(__name__)


def _json_safe_result(value: Any) -> Any:
    if value is None or isinstance(value, (bool, int, float, str)):
        return value
    if isinstance(value, dict):
        return {str(k): _json_safe_result(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [_json_safe_result(v) for v in value]

    converted = qgis_json_default(value)
    if isinstance(converted, dict):
        return {str(k): _json_safe_result(v) for k, v in converted.items()}
    if isinstance(converted, list):
        return [_json_safe_result(v) for v in converted]
    return converted


class ScriptStatus(Enum):
    PENDING = "pending"
    RUNNING = "running"
    CANCEL_REQUESTED = "cancel_requested"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELED = "canceled"


@dataclass
class ScriptTask:
    task_id: str
    script: str
    status: ScriptStatus = ScriptStatus.PENDING
    result: Optional[Any] = None
    error: Optional[str] = None
    progress: float = 0.0
    message: str = ""
    created_at: datetime = field(default_factory=datetime.now)
    started_at: Optional[datetime] = None
    completed_at: Optional[datetime] = None
    timeout: float = 300.0
    _cancel_event: threading.Event = field(default_factory=threading.Event)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "task_id": self.task_id,
            "status": self.status.value,
            "progress": self.progress,
            "message": self.message,
            "result": _json_safe_result(self.result),
            "error": self.error,
            "created_at": self.created_at.isoformat() if self.created_at else None,
            "started_at": self.started_at.isoformat() if self.started_at else None,
            "completed_at": self.completed_at.isoformat()
            if self.completed_at
            else None,
        }


class ScriptExecutor:
    _matplotlib_config_lock = threading.Lock()
    _matplotlib_configured = False
    _main_thread_runner_lock = threading.Lock()
    _main_thread_runner: Optional[Callable[[Callable[[], Any]], Any]] = None
    _main_thread_runner_failed = False
    _cjk_fallback_fonts = [
        "PingFang SC",
        "Hiragino Sans GB",
        "Heiti SC",
        "STHeiti",
        "Arial Unicode MS",
        "Noto Sans CJK SC",
        "Source Han Sans SC",
        "SimHei",
        "Microsoft YaHei",
    ]
    _safe_builtin_names = {
        "abs",
        "all",
        "any",
        "bool",
        "dict",
        "enumerate",
        "float",
        "int",
        "len",
        "list",
        "max",
        "min",
        "pow",
        "range",
        "round",
        "set",
        "sorted",
        "str",
        "sum",
        "tuple",
        "zip",
        "print",
        "isinstance",
        "type",
        "Exception",
        "ValueError",
        "TypeError",
        "RuntimeError",
    }
    _allowed_import_prefixes = {"qgis", "processing", "math", "json", "re", "datetime"}

    @classmethod
    def _resolve_main_thread_runner(
        cls,
    ) -> Optional[Callable[[Callable[[], Any]], Any]]:
        with cls._main_thread_runner_lock:
            if cls._main_thread_runner is not None:
                return cls._main_thread_runner
            if cls._main_thread_runner_failed:
                return None

            try:
                from autocarto.tools.system._shared.layout_runtime import (
                    run_in_main_thread,
                )

                cls._main_thread_runner = run_in_main_thread
            except Exception:
                cls._main_thread_runner_failed = True
                logger.debug(
                    "Main-thread runner unavailable in ScriptExecutor; "
                    "falling back to direct execution.",
                    exc_info=True,
                )
                return None

            return cls._main_thread_runner

    @staticmethod
    def _is_qt_main_thread() -> bool:
        try:
            qt_core = importlib.import_module("qgis.PyQt.QtCore")
            QCoreApplication = getattr(qt_core, "QCoreApplication", None)
            QThread = getattr(qt_core, "QThread", None)
            if QCoreApplication is None or QThread is None:
                return False
            app = QCoreApplication.instance()
            if app is None:
                return False
            return QThread.currentThread() == app.thread()
        except Exception:
            return False

    @staticmethod
    def _pump_qt_events() -> None:
        try:
            qt_core = importlib.import_module("qgis.PyQt.QtCore")
            QCoreApplication = getattr(qt_core, "QCoreApplication", None)
            if QCoreApplication is None:
                return
            process_events = getattr(QCoreApplication, "processEvents", None)
            if callable(process_events):
                process_events()
        except Exception:
            return

    def __init__(self, max_concurrent: int = 3):
        self._tasks: Dict[str, ScriptTask] = {}
        self._lock = threading.Lock()
        self._max_concurrent = max_concurrent
        self._running_count = 0
        self._progress_callbacks: Dict[str, Callable[[str, float, str], None]] = {}
        logger.info(f"ScriptExecutor 初始化完成，最大并发数: {max_concurrent}")

    def submit(
        self,
        script: str,
        timeout: float = 300.0,
        progress_callback: Optional[Callable[[str, float, str], None]] = None,
    ) -> str:
        task_id = str(uuid.uuid4())[:8]
        task = ScriptTask(task_id=task_id, script=script, timeout=timeout)

        with self._lock:
            self._tasks[task_id] = task
            if progress_callback:
                self._progress_callbacks[task_id] = progress_callback

        thread = threading.Thread(
            target=self._execute_task, args=(task_id,), daemon=True
        )
        thread.start()

        logger.info(f"任务 {task_id} 已提交，超时: {timeout}秒")
        return task_id

    def _execute_task(self, task_id: str) -> None:
        with self._lock:
            task = self._tasks.get(task_id)
        if not task:
            return

        while True:
            with self._lock:
                if self._running_count < self._max_concurrent:
                    self._running_count += 1
                    break
            time.sleep(0.1)
            if task._cancel_event.is_set():
                self._update_task_status(task_id, ScriptStatus.CANCELED)
                return

        try:
            if task._cancel_event.is_set():
                self._update_task_status(task_id, ScriptStatus.CANCELED)
                return
            self._update_task_status(task_id, ScriptStatus.RUNNING)
            with self._lock:
                task.started_at = datetime.now()
            self._report_progress(task_id, 0.0, "开始执行脚本...")

            result = self._run_script_with_timeout(task, task.timeout)

            if task._cancel_event.is_set():
                self._update_task_status(task_id, ScriptStatus.CANCELED)
            elif result.get("timeout"):
                self._update_task_status(task_id, ScriptStatus.FAILED)
                with self._lock:
                    task.error = "脚本执行超时"
            elif result.get("error"):
                self._update_task_status(task_id, ScriptStatus.FAILED)
                with self._lock:
                    task.error = result["error"]
            else:
                self._update_task_status(task_id, ScriptStatus.SUCCEEDED)
                with self._lock:
                    task.result = result.get("result")
                self._report_progress(task_id, 1.0, "执行完成")

        except Exception as e:
            logger.exception(f"任务 {task_id} 执行异常")
            self._update_task_status(task_id, ScriptStatus.FAILED)
            with self._lock:
                task.error = str(e)
        finally:
            with self._lock:
                task.completed_at = datetime.now()
                self._running_count -= 1

    def _run_script_with_timeout(
        self, task: ScriptTask, timeout: float
    ) -> Dict[str, Any]:
        result_container: Dict[str, Any] = {
            "result": None,
            "error": None,
            "timeout": False,
        }
        execution_done = threading.Event()

        def execute_script_body() -> Any:
            self._configure_matplotlib_runtime()
            exec_globals = self._create_exec_environment(task.task_id)
            exec_locals: Dict[str, Any] = {}
            exec(task.script, exec_globals, exec_locals)
            return exec_locals.get("result", None)

        def run_script():
            try:
                runner = type(self)._resolve_main_thread_runner()
                if runner is None:
                    result_container["result"] = execute_script_body()
                else:
                    logger.info(
                        "Dispatching PyQGIS task %s to Qt main thread "
                        "(caller_thread=%s/%s)",
                        task.task_id,
                        threading.current_thread().name,
                        threading.get_ident(),
                    )
                    result_container["result"] = runner(execute_script_body)
            except Exception as e:
                result_container["error"] = str(e)
            finally:
                execution_done.set()

        exec_thread = threading.Thread(target=run_script, daemon=True)
        exec_thread.start()

        if not execution_done.wait(timeout=timeout):
            result_container["timeout"] = True
            logger.warning(f"任务 {task.task_id} 执行超时 ({timeout}秒)")

        return result_container

    def _configure_matplotlib_runtime(self) -> None:
        executor_cls = type(self)
        with executor_cls._matplotlib_config_lock:
            if executor_cls._matplotlib_configured:
                return

            try:
                matplotlib = importlib.import_module("matplotlib")
                font_manager = importlib.import_module("matplotlib.font_manager")
            except ImportError:
                executor_cls._matplotlib_configured = True
                return

            try:
                available_fonts = {
                    getattr(font, "name", "")
                    for font in getattr(font_manager.fontManager, "ttflist", [])
                }
                available_fonts.discard("")

                preferred_fonts = [
                    font_name
                    for font_name in executor_cls._cjk_fallback_fonts
                    if font_name in available_fonts
                ]

                existing_fonts = list(matplotlib.rcParams.get("font.sans-serif", []))
                merged_fonts = preferred_fonts + [
                    font_name
                    for font_name in existing_fonts
                    if font_name not in preferred_fonts
                ]
                if merged_fonts:
                    matplotlib.rcParams["font.sans-serif"] = merged_fonts
                matplotlib.rcParams["axes.unicode_minus"] = False

                warnings.filterwarnings(
                    "ignore",
                    message=r"Glyph \d+ missing from current font\.",
                    category=RuntimeWarning,
                    module=r"matplotlib\.backends\.backend_agg",
                )
            except Exception:
                logger.exception("配置 matplotlib 字体回退失败")
            finally:
                executor_cls._matplotlib_configured = True

    @classmethod
    def _safe_import(cls, name: str, globals_=None, locals_=None, fromlist=(), level=0):
        root_module = str(name or "").split(".")[0]
        if root_module not in cls._allowed_import_prefixes:
            raise ImportError(f"禁止导入模块: {name}")
        return builtins.__import__(name, globals_, locals_, fromlist, level)

    def _build_safe_builtins(self) -> Dict[str, Any]:
        safe_builtins: Dict[str, Any] = {}
        for name in self._safe_builtin_names:
            value = getattr(builtins, name, None)
            if value is not None:
                safe_builtins[name] = value
        safe_builtins["__import__"] = self._safe_import
        return safe_builtins

    def _create_exec_environment(self, task_id: str) -> Dict[str, Any]:
        def report_progress(progress: float, message: str = ""):
            self._report_progress(task_id, progress, message)

        env: Dict[str, Any] = {
            "__builtins__": self._build_safe_builtins(),
            "report_progress": report_progress,
            "task_id": task_id,
        }

        try:
            qgis_core = importlib.import_module("qgis.core")
            qgis_analysis = importlib.import_module("qgis.analysis")
            processing = importlib.import_module("processing")

            env.update(
                {
                    "QgsProject": getattr(qgis_core, "QgsProject"),
                    "QgsVectorLayer": getattr(qgis_core, "QgsVectorLayer"),
                    "QgsRasterLayer": getattr(qgis_core, "QgsRasterLayer"),
                    "QgsFeature": getattr(qgis_core, "QgsFeature"),
                    "QgsGeometry": getattr(qgis_core, "QgsGeometry"),
                    "QgsField": getattr(qgis_core, "QgsField"),
                    "QgsProcessingFeedback": getattr(
                        qgis_core, "QgsProcessingFeedback"
                    ),
                    "QgsCoordinateReferenceSystem": getattr(
                        qgis_core, "QgsCoordinateReferenceSystem"
                    ),
                    "QgsNativeAlgorithms": getattr(
                        qgis_analysis, "QgsNativeAlgorithms"
                    ),
                    "processing": processing,
                }
            )
        except ImportError:
            logger.warning("QGIS 模块未找到，脚本将在受限环境中执行")

        return env

    def _update_task_status(self, task_id: str, status: ScriptStatus) -> None:
        with self._lock:
            task = self._tasks.get(task_id)
        if task:
            task.status = status
            logger.debug(f"任务 {task_id} 状态更新: {status.value}")

    def _report_progress(self, task_id: str, progress: float, message: str) -> None:
        with self._lock:
            task = self._tasks.get(task_id)
            callback = self._progress_callbacks.get(task_id)
            if task is None:
                return
            if task.status in (
                ScriptStatus.CANCEL_REQUESTED,
                ScriptStatus.CANCELED,
                ScriptStatus.FAILED,
                ScriptStatus.SUCCEEDED,
            ):
                return
            clamped_progress = min(max(progress, 0.0), 1.0)
            task.progress = clamped_progress
            task.message = message
        if callback:
            try:
                callback(task_id, clamped_progress, message)
            except Exception as e:
                logger.warning(f"进度回调执行失败: {e}")

    def get_task(self, task_id: str) -> Optional[ScriptTask]:
        with self._lock:
            return self._tasks.get(task_id)

    def get_task_status(self, task_id: str) -> Optional[Dict[str, Any]]:
        with self._lock:
            task = self._tasks.get(task_id)
        return task.to_dict() if task else None

    def cancel_task(self, task_id: str) -> bool:
        with self._lock:
            task = self._tasks.get(task_id)
            if not task:
                return False
            if task.status == ScriptStatus.CANCEL_REQUESTED:
                return True
            if task.status in (ScriptStatus.PENDING, ScriptStatus.RUNNING):
                task.status = ScriptStatus.CANCEL_REQUESTED
                task._cancel_event.set()
                logger.debug(f"任务 {task_id} 状态更新: {task.status.value}")
                logger.info(f"任务 {task_id} 已请求取消")
                return True
        return False

    def list_tasks(
        self, status_filter: Optional[ScriptStatus] = None
    ) -> list[Dict[str, Any]]:
        tasks = []
        with self._lock:
            task_list = list(self._tasks.values())

        for task in task_list:
            if status_filter is None or task.status == status_filter:
                tasks.append(task.to_dict())
        return tasks

    def wait_for_task(
        self, task_id: str, timeout: Optional[float] = None
    ) -> Optional[Dict[str, Any]]:
        with self._lock:
            task = self._tasks.get(task_id)
        if not task:
            return None

        start_time = time.time()
        while True:
            with self._lock:
                task = self._tasks.get(task_id)
                status = task.status if task else None

            if task is None:
                return None
            if status not in (
                ScriptStatus.PENDING,
                ScriptStatus.RUNNING,
                ScriptStatus.CANCEL_REQUESTED,
            ):
                break
            if timeout and (time.time() - start_time) > timeout:
                break
            if self._is_qt_main_thread():
                self._pump_qt_events()
                time.sleep(0.01)
            else:
                time.sleep(0.1)

        with self._lock:
            task = self._tasks.get(task_id)
            return task.to_dict() if task else None

    def cleanup_completed(self, max_age_seconds: float = 3600) -> int:
        now = datetime.now()
        to_remove = []

        with self._lock:
            snapshot = list(self._tasks.items())

        for task_id, task in snapshot:
            if task.status in (
                ScriptStatus.SUCCEEDED,
                ScriptStatus.FAILED,
                ScriptStatus.CANCELED,
            ):
                if task.completed_at:
                    age = (now - task.completed_at).total_seconds()
                    if age > max_age_seconds:
                        to_remove.append(task_id)

        if to_remove:
            with self._lock:
                for task_id in to_remove:
                    self._tasks.pop(task_id, None)
                    self._progress_callbacks.pop(task_id, None)

        if to_remove:
            logger.info(f"清理了 {len(to_remove)} 个已完成任务")
        return len(to_remove)
