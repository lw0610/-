from __future__ import annotations

import logging
import time
from typing import Any, Dict, List, Optional, Tuple

from .registry import ToolRegistry, _load_execution_bindings
from .runtime import call_tool_handler, extract_tool_error
from autocarto.utils.redaction import redact_sensitive_text

logger = logging.getLogger(__name__)


def _read_runtime_tool_engine_contract() -> Tuple[
    List[Dict[str, Any]], List[Dict[str, Any]]
]:
    return [], []


class QGISToolEngine:
    def __init__(
        self,
        *,
        load_interactive_tools: bool = True,
        definitions_dir: Optional[str] = None,
    ):
        if definitions_dir is not None:
            raise ValueError(
                "definitions_dir 已移除；Python tool engine 只接受 authority 下发的 execution_manifest。"
            )

        self._load_interactive_tools = load_interactive_tools
        execution_manifest, public_tool_catalog = _read_runtime_tool_engine_contract()
        self.update_contract(
            execution_manifest=execution_manifest,
            public_tool_catalog=public_tool_catalog,
        )

    def update_contract(
        self,
        *,
        execution_manifest: Optional[List[Dict[str, Any]]] = None,
        public_tool_catalog: Optional[List[Dict[str, Any]]] = None,
    ) -> None:
        self._registry = ToolRegistry(public_tool_catalog=public_tool_catalog or [])
        self._execution_manifest = execution_manifest or []
        self._register_tools()

    def _register_tools(self):
        try:
            for binding in _load_execution_bindings(self._execution_manifest):
                self._registry.register(binding)
        except Exception as exc:
            logger.exception("工具注册失败: %s", exc)
            raise

    def list_tools(self) -> List[Dict[str, Any]]:
        return self._registry.list_tools()

    def invoke(self, tool_name: str, params: Optional[Dict[str, Any]] = None) -> Any:
        tool = self._registry.get(tool_name)
        if not tool:
            raise ValueError(f"未知工具: {tool_name}")
        return call_tool_handler(tool.handler, params or {})


def collect_output_layers(result: Any) -> List[Dict[str, Any]]:
    layers: List[Dict[str, Any]] = []
    if isinstance(result, dict):
        output = result.get("output")
        if isinstance(output, str) and output.endswith(
            (".gpkg", ".shp", ".geojson", ".tif", ".tiff")
        ):
            layers.append({"path": output, "auto_load": True})
        layer_path = result.get("layer_path")
        if isinstance(layer_path, str) and layer_path:
            layers.append({"path": layer_path, "auto_load": True})
        raw_layers = result.get("output_layers")
        if isinstance(raw_layers, list):
            for item in raw_layers:
                if not isinstance(item, dict):
                    continue
                path = item.get("path")
                if isinstance(path, str) and path:
                    layers.append(
                        {
                            "path": path,
                            "auto_load": bool(item.get("auto_load", True)),
                        }
                    )
        info = result.get("info")
        if isinstance(info, dict):
            info_layer_path = info.get("layer_path")
            if isinstance(info_layer_path, str) and info_layer_path:
                layers.append({"path": info_layer_path, "auto_load": True})
    return layers


def execute_tool(
    tool_name: str,
    params: Optional[Dict[str, Any]] = None,
    *,
    engine: Optional[QGISToolEngine] = None,
) -> Dict[str, Any]:
    active_engine = engine or QGISToolEngine(load_interactive_tools=False)
    started_at = time.monotonic()
    logger.info("开始执行工具: tool=%s", tool_name)
    try:
        output = active_engine.invoke(tool_name, params or {})
        if isinstance(output, dict) and {
            "success",
            "output",
            "error",
            "output_layers",
        }.issubset(output.keys()):
            logger.info(
                "工具执行完成: tool=%s success=%s elapsed_ms=%d",
                tool_name,
                bool(output.get("success")),
                int((time.monotonic() - started_at) * 1000),
            )
            return output
        output_layers = collect_output_layers(output)
        if isinstance(output, dict) and output.get("success") is False:
            logger.info(
                "工具执行完成: tool=%s success=%s elapsed_ms=%d",
                tool_name,
                False,
                int((time.monotonic() - started_at) * 1000),
            )
            return {
                "success": False,
                "output": output,
                "error": extract_tool_error(output),
                "output_layers": output_layers,
            }
        logger.info(
            "工具执行完成: tool=%s success=%s elapsed_ms=%d",
            tool_name,
            True,
            int((time.monotonic() - started_at) * 1000),
        )
        return {
            "success": True,
            "output": output,
            "error": "",
            "output_layers": output_layers,
        }
    except Exception as exc:
        logger.exception("工具执行失败: %s", tool_name)
        return {
            "success": False,
            "output": None,
            "error": redact_sensitive_text(exc),
            "output_layers": [],
        }


def create_engine(*, skill_dirs: Optional[List[str]] = None) -> QGISToolEngine:
    if skill_dirs is not None:
        raise ValueError(
            "skill_dirs 已移除；Python tool engine 不再消费本地 skill 目录。"
        )
    return QGISToolEngine()
