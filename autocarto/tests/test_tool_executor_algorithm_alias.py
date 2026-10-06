import unittest

from autocarto.core.tool_engine.executor import ProcessingToolExecutor


class ToolExecutorAlgorithmAliasTestCase(unittest.TestCase):
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
