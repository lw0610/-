import logging
from collections import Counter
from dataclasses import dataclass
from typing import Any, Callable, Dict, List, Mapping, Optional, Sequence, Union

from autocarto.tools.system._shared.canvas_refresh import (
    refresh_canvas as _shared_refresh_canvas,
)

from .definitions import ToolDefinitionLoader
from .executor import ProcessingToolExecutor


def refresh_canvas():
    _shared_refresh_canvas()


@dataclass
class ToolDefinition:
    name: str
    description: str
    category: str
    handler: Callable[..., Any]
    inputSchema: Dict[str, Any]
    backend: str = ""
    available: bool = True
    unavailable_reason: str = ""


_logger = logging.getLogger(__name__)


def _make_definition_handler(
    executor: ProcessingToolExecutor, tool_name: str
) -> Callable[[Dict[str, Any]], Dict[str, Any]]:
    def handler(params: Dict[str, Any]):
        return executor.execute(tool_name, params or {})

    return handler


def _has_execution_binding(
    tool_name: str,
    tool_def: Dict[str, Any],
    skipped: Counter[str],
) -> bool:
    handler_path = tool_def.get("handler")
    handler_file = tool_def.get("handler_file")
    handler_function = tool_def.get("handler_function")
    if handler_file and handler_function:
        return True
    if handler_path:
        return True

    algorithm_id = tool_def.get("algorithm_id")
    if algorithm_id:
        return True

    skipped["missing_execution_binding"] += 1
    _logger.warning(
        "跳过缺少执行绑定的工具定义: tool=%s",
        tool_name,
    )
    return False


def _load_execution_bindings(
    execution_manifest: Optional[
        Union[Sequence[Mapping[str, Any]], Mapping[str, Any]]
    ] = None,
) -> List[ToolDefinition]:
    loader = ToolDefinitionLoader(execution_manifest)
    definitions = loader.load_all()
    executor = ProcessingToolExecutor(definitions)
    skipped: Counter[str] = Counter()
    bindings: List[ToolDefinition] = []

    for tool_name, tool_def in sorted(definitions.items()):
        if not _has_execution_binding(tool_name, tool_def, skipped):
            continue

        input_schema = tool_def.get("inputSchema") or loader.to_json_schema(tool_def)
        bindings.append(
            ToolDefinition(
                name=tool_name,
                description=tool_def.get("description", ""),
                category=tool_def.get("category", "unknown"),
                handler=_make_definition_handler(executor, tool_name),
                inputSchema=input_schema,
                backend="cli",
                available=True,
                unavailable_reason="",
            )
        )

    _logger.info(
        "插件端已建立 %d 个 execution manifest 绑定，跳过统计=%s",
        len(bindings),
        dict(skipped),
    )
    return bindings


class ToolRegistry:
    def __init__(self, public_tool_catalog: Optional[List[Dict[str, Any]]] = None):
        self._tools: Dict[str, ToolDefinition] = {}
        self._categories: Dict[str, List[str]] = {}
        self._definition_executor: Optional[ProcessingToolExecutor] = None
        self._public_tool_catalog: List[Dict[str, Any]] = [
            dict(tool) for tool in (public_tool_catalog or []) if isinstance(tool, dict)
        ]

    def register(self, tool: ToolDefinition):
        previous = self._tools.get(tool.name)
        if tool.name in self._tools:
            _logger.warning("工具名称重复，将覆盖: %s", tool.name)

        if previous is not None:
            old_names = self._categories.get(previous.category)
            if old_names and tool.name in old_names:
                old_names.remove(tool.name)
                if not old_names:
                    self._categories.pop(previous.category, None)

        self._tools[tool.name] = tool

        if tool.category not in self._categories:
            self._categories[tool.category] = []
        if tool.name not in self._categories[tool.category]:
            self._categories[tool.category].append(tool.name)

    def get(self, name: str) -> Optional[ToolDefinition]:
        return self._tools.get(name)

    def list_tools(self) -> List[Dict[str, Any]]:
        return [dict(tool) for tool in self._public_tool_catalog]

    def list_categories(self) -> Dict[str, List[str]]:
        return self._categories.copy()

    def get_all_tools(self) -> List[ToolDefinition]:
        return list(self._tools.values())

    def load_all(
        self,
        execution_manifest: Optional[
            Union[Sequence[Mapping[str, Any]], Mapping[str, Any]]
        ] = None,
    ):
        bindings = _load_execution_bindings(execution_manifest)
        loaded = 0

        for binding in bindings:
            tool_name = binding.name
            if self.get(tool_name) is not None:
                continue
            self.register(binding)
            loaded += 1

        return loaded
