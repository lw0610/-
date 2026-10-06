import json
import math
import unittest
from unittest import mock

import autocarto.core.tool_engine.runtime as runtime


class _FakeQVariant:
    def __init__(self, value=None, is_null=False):
        self._value = value
        self._is_null = is_null

    def isNull(self):
        return self._is_null

    def value(self):
        return self._value


class _FakeEnum:
    def __int__(self):
        return 7


class ToolServiceJsonSafetyTestCase(unittest.TestCase):
    def test_safe_json_dumps_sanitizes_non_finite_numbers(self):
        payload = {
            "ok": True,
            "nested": {
                "nan": float("nan"),
                "inf": float("inf"),
                "neg_inf": float("-inf"),
            },
            "arr": [1, float("nan"), 2],
            "text": "NaN should stay string",
        }

        encoded, replaced = runtime.safe_json_dumps(payload)
        decoded = json.loads(encoded)

        self.assertEqual(replaced, 4)
        self.assertIsNone(decoded["nested"]["nan"])
        self.assertIsNone(decoded["nested"]["inf"])
        self.assertIsNone(decoded["nested"]["neg_inf"])
        self.assertIsNone(decoded["arr"][1])
        self.assertEqual(decoded["text"], "NaN should stay string")

    def test_safe_json_dumps_without_non_finite_keeps_payload(self):
        payload = {"value": 1.5, "label": "ok", "flag": False}
        encoded, replaced = runtime.safe_json_dumps(payload)
        decoded = json.loads(encoded)

        self.assertEqual(replaced, 0)
        self.assertEqual(decoded, payload)
        self.assertTrue(math.isfinite(decoded["value"]))

    def test_safe_json_dumps_handles_qvariant_like_values(self):
        payload = {
            "null_attr": _FakeQVariant(is_null=True),
            "value_attr": _FakeQVariant(42),
            "enum_attr": _FakeEnum(),
        }

        encoded, replaced = runtime.safe_json_dumps(payload)
        decoded = json.loads(encoded)

        self.assertGreaterEqual(replaced, 3)
        self.assertIsNone(decoded["null_attr"])
        self.assertEqual(decoded["value_attr"], 42)
        self.assertEqual(decoded["enum_attr"], 7)

    def test_normalize_tool_result_sets_error_for_failed_payload(self):
        result = runtime.normalize_tool_result(
            {"success": False, "info": "字段计算失败"}
        )

        self.assertFalse(result["success"])
        self.assertEqual(result["error"], "字段计算失败")

    @mock.patch("importlib.import_module")
    def test_call_tool_handler_fallback_without_main_thread_runner(self, mock_import):
        # mock importlib to raise ImportError so it falls back cleanly without QThread checks
        mock_import.side_effect = ImportError("mocked import error")
        original_resolver = runtime._resolve_main_thread_runner
        try:
            runtime._resolve_main_thread_runner = lambda: None
            result = runtime.call_tool_handler(
                lambda params: {"echo": params["v"]}, {"v": 3}
            )
        finally:
            runtime._resolve_main_thread_runner = original_resolver

        self.assertEqual(result, {"echo": 3})

    def test_call_tool_handler_uses_main_thread_runner(self):
        called = {"runner": False, "handler": False}
        original_resolver = runtime._resolve_main_thread_runner

        def fake_runner(fn):
            called["runner"] = True
            return fn()

        def fake_handler(params):
            called["handler"] = True
            return {"ok": params["v"]}

        try:
            runtime._resolve_main_thread_runner = lambda: fake_runner
            result = runtime.call_tool_handler(fake_handler, {"v": 5})
        finally:
            runtime._resolve_main_thread_runner = original_resolver

        self.assertTrue(called["runner"])
        self.assertTrue(called["handler"])
        self.assertEqual(result, {"ok": 5})


if __name__ == "__main__":
    unittest.main()
