import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from autocarto.core.instance_ws_client import InstanceWSClient
from autocarto.core.tool_engine.engine import QGISToolEngine, execute_tool
from autocarto.core.tool_engine.executor import ProcessingToolExecutor
from autocarto.core.tool_engine.registry import ToolDefinition
from autocarto.core.tool_engine.runtime import normalize_tool_result


class ToolFailurePropagationTestCase(unittest.TestCase):
    def test_failure_normalization_preserves_error_message_and_legacy_details(self):
        cases = [
            ({"error": "具体错误", "info": "概括", "output": "原始输出"}, "具体错误"),
            ({"message": "布局不存在", "info": "概括"}, "布局不存在"),
            ({"error": "  ", "message": " ", "output": "具体错误"}, "具体错误"),
            (
                {"output": "QgsLayoutItemLabel: invalid argument", "info": "handler 执行失败"},
                "handler 执行失败: QgsLayoutItemLabel: invalid argument",
            ),
            ({"info": "图层不存在", "output": ""}, "图层不存在"),
            ({"info": "图层不存在", "output": "图层不存在"}, "图层不存在"),
            ({"output": {"data": [1, 2, 3]}}, "工具执行失败"),
        ]
        for fields, expected in cases:
            with self.subTest(fields=fields):
                original = {"success": False, **fields}
                result = normalize_tool_result(original)
                self.assertFalse(result["success"])
                self.assertEqual(result["error"], expected)
                self.assertEqual(original, {"success": False, **fields})
                self.assertEqual(result.get("output"), fields.get("output"))

    def test_execute_tool_wraps_failure_without_losing_legacy_detail(self):
        failure = {"success": False, "output": "缺少布局对象", "info": "handler 执行失败"}
        engine = mock.Mock()
        engine.invoke.return_value = failure

        result = execute_tool("layout_add_map", {}, engine=engine)

        self.assertFalse(result["success"])
        self.assertEqual(result["error"], "handler 执行失败: 缺少布局对象")
        self.assertEqual(result["output"], failure)

    def test_handler_exception_reaches_failed_ws_terminal_result(self):
        detail = "QgsLayoutItemLabel: invalid argument type"
        with tempfile.TemporaryDirectory() as temp_dir:
            handler_path = Path(temp_dir) / "failure_handler.py"
            handler_path.write_text(
                f"def run(params):\n    raise RuntimeError({detail!r})\n",
                encoding="utf-8",
            )
            executor = ProcessingToolExecutor({
                "layout_add_label": {
                    "handler_file": str(handler_path),
                    "handler_function": "run",
                }
            })
            engine = QGISToolEngine(load_interactive_tools=False)
            engine._registry.register(ToolDefinition(
                name="layout_add_label",
                description="test failure propagation",
                category="print-layout",
                handler=lambda params: executor.execute("layout_add_label", params),
                inputSchema={"type": "object"},
            ))
            client = InstanceWSClient(
                config_manager=SimpleNamespace(get=lambda _key, default=None: default),
                tool_engine=engine,
                instance_id="failure-test",
                qgis_version="test",
                skills_dir="/unused",
            )
            sent = []
            client._send = lambda kind, data=None: sent.append((kind, data)) or True
            client._invoke_on_main_thread = lambda callback: callback()
            with mock.patch(
                "autocarto.core.tool_engine.runtime._resolve_main_thread_runner",
                return_value=lambda callback: callback(),
            ):
                client._handle_tool_invoke({
                    "request_id": "failure-request",
                    "tool_name": "layout_add_label",
                    "params": {},
                })

        self.assertEqual(len(sent), 1)
        kind, result = sent[0]
        self.assertEqual(kind, "tool_result")
        self.assertFalse(result["success"])
        self.assertEqual(result["status"], "failed")
        self.assertEqual(result["terminal_state"], "failed")
        self.assertEqual(result["progress"], 0.0)
        self.assertIn(detail, result["error"])
        self.assertIn(detail, result["message"])
        self.assertEqual(result["output"]["output"], detail)
        self.assertEqual(result["output"]["info"], "handler 执行失败")

    def test_ws_fallback_uses_same_failure_extraction(self):
        self.assertEqual(
            InstanceWSClient._extract_tool_error({"success": False, "message": "布局不存在"}),
            "布局不存在",
        )
        self.assertIn(
            "具体异常",
            InstanceWSClient._extract_tool_error({
                "success": False, "info": "handler 执行失败", "output": "具体异常"
            }),
        )


if __name__ == "__main__":
    unittest.main()
