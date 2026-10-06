import logging
from typing import Any, Dict, Optional

from autocarto.tools.system._shared.pyqgis_context_impl import QGISContext
from autocarto.tools.system._shared.pyqgis_executor_impl import ScriptExecutor
from autocarto.tools.system._shared.pyqgis_validator_impl import ScriptValidator

logger = logging.getLogger(__name__)

_executor = None
_validator = None
_context = None


def get_executor() -> ScriptExecutor:
    global _executor
    if _executor is None:
        # Keep PyQGIS script execution serialized to reduce QGIS re-entrancy risk.
        _executor = ScriptExecutor(max_concurrent=1)
    return _executor


def get_validator() -> ScriptValidator:
    global _validator
    if _validator is None:
        _validator = ScriptValidator()
    return _validator


def get_context() -> QGISContext:
    global _context
    if _context is None:
        _context = QGISContext()
    return _context


def pyqgis_execute_script(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    params = params or {}
    script = params.get("script", "")
    timeout = params.get("timeout", 300.0)

    if not script:
        return {"success": False, "output": "脚本不能为空", "info": ""}

    try:
        validator = get_validator()
        validation = validator.validate(script)

        if not validation.is_valid:
            return {
                "success": False,
                "output": "脚本验证失败",
                "info": "; ".join(validation.errors),
            }

        executor = get_executor()
        task_id = executor.submit(script, timeout=timeout)
        result = executor.wait_for_task(task_id, timeout=timeout + 10)

        if result is None:
            return {
                "success": False,
                "output": "任务未找到",
                "info": f"task_id: {task_id}",
            }

        if not isinstance(result, dict):
            return {
                "success": False,
                "output": f"执行返回了非预期的数据类型: {type(result).__name__}",
                "info": f"task_id: {task_id}, raw: {str(result)[:500]}",
            }

        status = result.get("status", "unknown")
        return {
            "success": status == "succeeded",
            "output": str(result.get("result", "")),
            "info": result.get("error") or result.get("message", ""),
        }

    except Exception as e:
        logger.exception("脚本执行失败")
        return {"success": False, "output": str(e), "info": "执行异常"}


def pyqgis_submit_script(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    params = params or {}
    script = params.get("script", "")
    timeout = params.get("timeout", 300.0)

    if not script:
        return {"success": False, "output": "脚本不能为空", "info": ""}

    try:
        validator = get_validator()
        validation = validator.validate(script)

        if not validation.is_valid:
            return {
                "success": False,
                "output": "脚本验证失败",
                "info": "; ".join(validation.errors),
            }

        executor = get_executor()
        task_id = executor.submit(script, timeout=timeout)

        return {"success": True, "output": task_id, "info": "任务已提交"}

    except Exception as e:
        return {"success": False, "output": str(e), "info": "提交失败"}


def pyqgis_get_task_status(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    params = params or {}
    task_id = params.get("task_id", "")

    if not task_id:
        return {"success": False, "output": "task_id 不能为空", "info": ""}

    try:
        executor = get_executor()
        status = executor.get_task_status(task_id)

        if status is None:
            return {
                "success": False,
                "output": "任务未找到",
                "info": f"task_id: {task_id}",
            }

        return {
            "success": True,
            "output": str(status),
            "info": status.get("status", "unknown"),
        }

    except Exception as e:
        return {"success": False, "output": str(e), "info": "查询失败"}


def pyqgis_get_context(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    try:
        ctx = get_context()
        full_context = ctx.get_full_context()
        return {"success": True, "output": str(full_context), "info": "上下文获取成功"}
    except Exception as e:
        return {"success": False, "output": str(e), "info": "获取失败"}


def pyqgis_validate_script(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    params = params or {}
    script = params.get("script", "")

    if not script:
        return {"success": False, "output": "脚本不能为空", "info": ""}

    try:
        validator = get_validator()
        result = validator.validate(script)
        return {
            "success": result.is_valid,
            "output": "验证通过" if result.is_valid else "验证失败",
            "info": "; ".join(result.errors + result.warnings),
        }
    except Exception as e:
        return {"success": False, "output": str(e), "info": "验证异常"}


def pyqgis_cancel_task(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    params = params or {}
    task_id = params.get("task_id", "")

    if not task_id:
        return {"success": False, "output": "task_id 不能为空", "info": ""}

    try:
        executor = get_executor()
        success = executor.cancel_task(task_id)
        return {
            "success": success,
            "output": "任务已取消" if success else "取消失败",
            "info": f"task_id: {task_id}",
        }
    except Exception as e:
        return {"success": False, "output": str(e), "info": "取消异常"}
