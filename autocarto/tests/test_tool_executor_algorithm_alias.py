import unittest
from types import SimpleNamespace
from unittest import mock

from autocarto.core.tool_engine.executor import ProcessingToolExecutor


class ToolExecutorAlgorithmAliasTestCase(unittest.TestCase):
    def test_unqualified_unmatched_name_is_not_a_missing_provider(self):
        executor = ProcessingToolExecutor({})
        registry = SimpleNamespace(
            algorithmById=lambda _name: None,
            providers=lambda: [SimpleNamespace(id=lambda: "gdal")],
        )
        qgis = SimpleNamespace(QgsApplication=SimpleNamespace(processingRegistry=lambda: registry))
        executor._processing = SimpleNamespace(algorithmHelp=lambda _name: None)
        with mock.patch("autocarto.core.tool_engine.executor.importlib.import_module", return_value=qgis):
            self.assertEqual(executor._check_algorithm_availability("gdal_warp"), (False, "invalid_algorithm_id"))
            self.assertEqual(executor._check_algorithm_availability("missing:warp"), (False, "provider_missing"))
            self.assertEqual(executor._check_algorithm_availability("gdal:missing"), (False, "invalid_algorithm_id"))
        result = executor._build_unavailable_result({"requested_id": "gdal_warp", "reason": "invalid_algorithm_id"})
        self.assertNotIn("provider", result["error"])

    def test_explicit_algorithm_binding_does_not_search_aliases(self):
        executor = ProcessingToolExecutor({})
        with mock.patch.object(executor, "_check_algorithm_availability", return_value=(True, "exact_match")) as check:
            resolution = executor.resolve_tool_algorithm("gdal_warp", {"algorithm_id": "gdal:warpreproject"})
        self.assertEqual(resolution["resolved_id"], "gdal:warpreproject")
        check.assert_called_once_with("gdal:warpreproject")

    def test_resolve_buffer_alias_to_native_provider(self):
        executor = ProcessingToolExecutor({"buffer": {"algorithm_id": "buffer"}})

        def _fake_check(algorithm_id: str):
            if algorithm_id == "buffer":
                return False, "provider_missing"
            if algorithm_id == "native:buffer":
                return True, "exact_match"
            return False, "invalid_algorithm_id"

        executor._check_algorithm_availability = _fake_check  # type: ignore[method-assign]
        resolution = executor._resolve_algorithm_id("buffer", "buffer")

        self.assertTrue(resolution["available"])
        self.assertEqual(resolution["source"], "alias")
        self.assertEqual(resolution["resolved_id"], "native:buffer")
        self.assertEqual(resolution["requested_id"], "buffer")

    def test_resolve_minimum_bounding_geometry_alias(self):
        executor = ProcessingToolExecutor(
            {"minimum_bounding_geometry": {"algorithm_id": "minimum_bounding_geometry"}}
        )

        def _fake_check(algorithm_id: str):
            if algorithm_id == "minimum_bounding_geometry":
                return False, "provider_missing"
            if algorithm_id == "native:minimumboundinggeometry":
                return True, "exact_match"
            return False, "invalid_algorithm_id"

        executor._check_algorithm_availability = _fake_check  # type: ignore[method-assign]
        resolution = executor._resolve_algorithm_id(
            "minimum_bounding_geometry", "minimum_bounding_geometry"
        )

        self.assertTrue(resolution["available"])
        self.assertEqual(resolution["source"], "alias")
        self.assertEqual(resolution["resolved_id"], "native:minimumboundinggeometry")

    def test_resolve_grass_v_buffer_alias(self):
        executor = ProcessingToolExecutor(
            {"grass_v_buffer": {"algorithm_id": "grass_v_buffer"}}
        )

        def _fake_check(algorithm_id: str):
            if algorithm_id == "grass_v_buffer":
                return False, "provider_missing"
            if algorithm_id == "grass7:v.buffer":
                return True, "exact_match"
            return False, "invalid_algorithm_id"

        executor._check_algorithm_availability = _fake_check  # type: ignore[method-assign]
        resolution = executor._resolve_algorithm_id("grass_v_buffer", "grass_v_buffer")

        self.assertTrue(resolution["available"])
        self.assertEqual(resolution["source"], "alias")
        self.assertEqual(resolution["resolved_id"], "grass7:v.buffer")


if __name__ == "__main__":
    unittest.main()
