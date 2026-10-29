from __future__ import annotations

import logging
from pathlib import Path
from typing import Any, Dict, Iterable, List, Mapping, Optional, Sequence, Union

from ..paths import get_plugin_root


logger = logging.getLogger(__name__)

PARAM_TYPE_ALIASES = {
    "multilayer": "multi_layer",
    "mesh_layer": "layer",
}


class ToolDefinitionLoader:
    def __init__(
        self,
        manifest_entries: Optional[
            Union[Sequence[Mapping[str, Any]], Mapping[str, Any]]
        ] = None,
    ):
        self._manifest_entries = manifest_entries
        self._definitions: Dict[str, Dict[str, Any]] = {}

    def load_all(self) -> Dict[str, Dict[str, Any]]:
        for entry in self._iter_definition_entries():
            try:
                self._load_entry(entry)
            except Exception as exc:
                logger.error("加载执行绑定失败 %s: %s", entry, exc)

        logger.info("已加载 %d 个执行绑定定义", len(self._definitions))
        return dict(self._definitions)

    def _iter_definition_entries(self) -> Iterable[Mapping[str, Any]]:
        source = self._manifest_entries
        if source is None:
            return []

        if isinstance(source, Mapping):
            manifest = source.get("execution_manifest")
            if isinstance(manifest, list):
                return [item for item in manifest if isinstance(item, Mapping)]
            if self._looks_like_manifest_entry(source):
                return [source]
            return []

        if isinstance(source, Sequence) and not isinstance(
            source, (str, bytes, bytearray)
        ):
            return [item for item in source if isinstance(item, Mapping)]

        return []

    def _looks_like_manifest_entry(self, entry: Mapping[str, Any]) -> bool:
        return bool(str(entry.get("name") or "").strip())

    def _load_entry(self, entry: Mapping[str, Any]) -> None:
        execution = entry.get("execution")
        execution_data = execution if isinstance(execution, Mapping) else {}

        tool_name = str(entry.get("name") or "").strip()
        if not tool_name:
            return

        category = str(entry.get("category") or entry.get("domain") or "execution")
        category = category.strip() or "execution"
        tool: Dict[str, Any] = {
            "name": tool_name,
            "description": str(entry.get("description") or "").strip(),
            "category": category,
            "domain": category,
            "runtime": str(entry.get("runtime") or entry.get("backend") or "").strip(),
            "parameters": self._normalize_parameters(entry.get("parameters")),
        }

        output = execution_data.get("output")
        if "auto_load" in entry:
            tool["auto_load"] = entry["auto_load"]
        elif isinstance(output, Mapping) and "auto_load" in output:
            tool["auto_load"] = output["auto_load"]

        input_schema = entry.get("inputSchema") or entry.get("input_schema")
        if isinstance(input_schema, Mapping):
            tool["inputSchema"] = dict(input_schema)

        source_path = str(
            entry.get("source_path") or execution_data.get("source_path") or ""
        ).strip()
        if source_path:
            tool["source_path"] = source_path

        backend_tool = str(
            entry.get("backend_tool")
            or entry.get("algorithm_id")
            or execution_data.get("backend_tool")
            or ""
        ).strip()
        if backend_tool:
            tool["backend_tool"] = backend_tool
            tool["algorithm_id"] = backend_tool

        handler_path = str(
            entry.get("handler") or execution_data.get("handler") or ""
        ).strip()
        if handler_path:
            tool["handler"] = handler_path

        handler_file = str(
            entry.get("handler_file") or execution_data.get("handler_file") or ""
        ).strip()
        handler_function = str(
            entry.get("handler_function")
            or execution_data.get("handler_function")
            or ""
        ).strip()
        if handler_file and handler_function:
            tool["handler_file"] = self._resolve_handler_file(handler_file)
            tool["handler_function"] = handler_function

        self._definitions[tool_name] = tool

    def _resolve_handler_file(self, handler_file: str) -> str:
        candidate = Path(handler_file)
        if candidate.is_absolute():
            return str(candidate)
        return str(get_plugin_root() / candidate)

    def _normalize_parameters(self, parameters: Any) -> List[Dict[str, Any]]:
        if not isinstance(parameters, list):
            return []

        normalized: List[Dict[str, Any]] = []
        for item in parameters:
            if not isinstance(item, Mapping):
                continue
            parameter = dict(item)
            param_type = str(parameter.get("type") or "string").strip() or "string"
            parameter["type"] = PARAM_TYPE_ALIASES.get(param_type, param_type)
            normalized.append(parameter)
        return normalized

    def get_definition(self, name: str) -> Optional[Dict[str, Any]]:
        return self._definitions.get(name)

    def list_definitions(self) -> List[Dict[str, Any]]:
        return list(self._definitions.values())

    def to_json_schema(self, tool_def: Dict[str, Any]) -> Dict[str, Any]:
        properties: Dict[str, Any] = {}
        required: List[str] = []

        for param in tool_def.get("parameters", []):
            prop = self._param_to_schema(param)
            properties[param["name"]] = prop
            if param.get("required", False):
                required.append(param["name"])

        return {
            "type": "object",
            "properties": properties,
            "required": required,
        }

    def _param_to_schema(self, param: Dict[str, Any]) -> Dict[str, Any]:
        param_type = param.get("type", "string")
        schema: Dict[str, Any] = {"description": param.get("description", "")}

        type_mapping = {
            "string": {"type": "string"},
            "number": {"type": "number"},
            "integer": {"type": "integer"},
            "boolean": {"type": "boolean"},
            "object": {"type": "object"},
            "layer": {"type": "string", "description": "图层名称或路径"},
            "raster_layer": {"type": "string", "description": "栅格图层名称或路径"},
            "field": {"type": "string", "description": "字段名称"},
            "expression": {"type": "string", "description": "QGIS 表达式"},
            "crs": {"type": "string", "description": "坐标参考系统 (如 EPSG:4326)"},
            "extent": {"type": "string", "description": "范围 (xmin,ymin,xmax,ymax)"},
            "output_layer": {"type": "string", "description": "输出图层路径"},
            "output_raster": {"type": "string", "description": "输出栅格路径"},
            "file": {"type": "string", "description": "文件路径"},
            "folder": {"type": "string", "description": "文件夹路径"},
            "enum": {"type": "string"},
            "multi_layer": {"type": "array", "items": {"type": "string"}},
            "multilayer": {"type": "array", "items": {"type": "string"}},
            "mesh_layer": {"type": "string", "description": "网格图层名称或路径"},
            "fields": {"type": "array", "items": {"type": "string"}},
            "point": {"type": "string", "description": "点坐标 (x,y)"},
            "range": {"type": "string", "description": "数值范围 (min,max)"},
            "matrix": {"type": "array", "items": {"type": "array"}},
            "color": {"type": "string", "description": "颜色值 (如 #FF0000)"},
        }

        schema.update(type_mapping.get(param_type, {"type": "string"}))

        if param_type == "enum" and "values" in param:
            values = param["values"]
            if values and all(
                isinstance(v, int) and not isinstance(v, bool) for v in values
            ):
                schema["type"] = "integer"
                schema["enum"] = [int(v) for v in values]
            elif values and all(
                isinstance(v, (int, float)) and not isinstance(v, bool) for v in values
            ):
                schema["type"] = "number"
                schema["enum"] = values
            else:
                schema["type"] = "string"
                schema["enum"] = [str(v) for v in values]

        if "default" in param:
            if param_type == "enum" and schema.get("type") == "string":
                schema["default"] = str(param["default"])
            else:
                schema["default"] = param["default"]

        if "min" in param:
            schema["minimum"] = param["min"]
        if "max" in param:
            schema["maximum"] = param["max"]

        return schema
