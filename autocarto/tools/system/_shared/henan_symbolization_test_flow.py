from __future__ import annotations

from pathlib import Path
from typing import Any, Callable, Dict, List, Optional

from qgis.core import QgsProject

from autocarto.tools.system._shared.cartography_style_impl import (
    set_fill_symbol_handler,
    set_line_symbol_handler,
    set_marker_symbol_handler,
)
from autocarto.tools.system._shared.layout_runtime import run_in_main_thread
from autocarto.tools.system._shared.project_state_impl import open_project


DEFAULT_PROJECT_PATH = r"F:\qgis_project\henan-x.qgz"
DEFAULT_COUNTY_LABEL_FIELD = "name"
COUNTY_LABEL_FIELD_CANDIDATES = [
    "name",
    "Name",
    "NAME",
    "名称",
    "县名",
    "县城",
    "地名",
    "XZQMC",
]


def _line_step(layer: str, line_type: str) -> Dict[str, Any]:
    return {
        "layer": layer,
        "tool": "set_line_symbol_handler",
        "handler": set_line_symbol_handler,
        "params": {"layer": layer, "line_type": line_type},
    }


def _fill_step(layer: str, layer_type: str) -> Dict[str, Any]:
    return {
        "layer": layer,
        "tool": "set_fill_symbol_handler",
        "handler": set_fill_symbol_handler,
        "params": {"layer": layer, "layer_type": layer_type},
    }


def _marker_step(layer: str, layer_type: str, label_field: str) -> Dict[str, Any]:
    return {
        "layer": layer,
        "tool": "set_marker_symbol_handler",
        "handler": set_marker_symbol_handler,
        "params": {
            "layer": layer,
            "layer_type": layer_type,
            "label_field": label_field,
        },
    }


def _build_steps(county_label_field: str) -> List[Dict[str, Any]]:
    return [
        _line_step("河流线", "河流"),
        _line_step("河南铁路", "铁路"),
        _line_step("线状省界", "省界"),
        _fill_step("河流面", "河流"),
        _line_step("河南国道", "国道"),
        _line_step("河南高速公路", "高速公路"),
        _marker_step("河南省县城驻地", "county", county_label_field),
    ]


def _resolve_label_field(layer_name: str, requested_field: str) -> str:
    layers = QgsProject.instance().mapLayersByName(layer_name)
    if not layers:
        return requested_field

    fields = {field.name() for field in layers[0].fields()}
    if requested_field in fields:
        return requested_field

    for candidate in COUNTY_LABEL_FIELD_CANDIDATES:
        if candidate in fields:
            return candidate

    return requested_field


def _normalize_result(result: Any) -> Dict[str, Any]:
    if isinstance(result, dict):
        return result
    return {"success": False, "output": "", "info": f"工具返回了非 dict 结果: {result!r}"}


def _run_step(step: Dict[str, Any]) -> Dict[str, Any]:
    handler: Callable[[Dict[str, Any]], Dict[str, Any]] = step["handler"]
    result = _normalize_result(handler(step["params"]))
    return {
        "layer": step["layer"],
        "tool": step["tool"],
        "params": step["params"],
        "success": bool(result.get("success")),
        "output": result.get("output", ""),
        "info": result.get("info", ""),
        "raw": result,
    }


def _run_flow(params: Dict[str, Any]) -> Dict[str, Any]:
    project_path = str(params.get("project_path") or DEFAULT_PROJECT_PATH).strip()
    county_label_field = str(
        params.get("county_label_field") or DEFAULT_COUNTY_LABEL_FIELD
    ).strip()
    save_project = bool(params.get("save_project", False))

    project_file = Path(project_path).expanduser()
    if not project_file.exists():
        return {
            "success": False,
            "output": "",
            "info": f"工程文件不存在: {project_file}",
            "project_path": str(project_file),
            "results": [],
        }

    open_result = _normalize_result(open_project({"path": str(project_file)}))
    if not open_result.get("success"):
        return {
            "success": False,
            "output": "",
            "info": "打开测试工程失败",
            "project_path": str(project_file),
            "open_project": open_result,
            "results": [],
        }

    county_label_field = _resolve_label_field("河南省县城驻地", county_label_field)
    results = [_run_step(step) for step in _build_steps(county_label_field)]
    failed = [item for item in results if not item["success"]]

    saved = False
    save_error: Optional[str] = None
    if save_project and not failed:
        try:
            saved = bool(QgsProject.instance().write())
            if not saved:
                save_error = "QgsProject.write() 返回 False"
        except Exception as exc:
            save_error = str(exc)

    success = not failed and (not save_project or saved)
    if success:
        info = "河南符号化测试流程执行完成"
    elif failed:
        info = f"河南符号化测试流程有 {len(failed)} 个步骤失败"
    else:
        info = f"河南符号化测试流程执行完成，但保存工程失败: {save_error}"

    return {
        "success": success,
        "output": str(project_file),
        "info": info,
        "project_path": str(project_file),
        "open_project": open_result,
        "save_project": save_project,
        "project_saved": saved,
        "save_error": save_error,
        "total": len(results),
        "passed": len(results) - len(failed),
        "failed": len(failed),
        "results": results,
    }


def run_henan_symbolization_test_flow(
    params: Optional[Dict[str, Any]] = None,
) -> Dict[str, Any]:
    """Open the Henan QGIS project and apply the built-in point/line/fill styles."""

    return run_in_main_thread(lambda: _run_flow(params or {}))
