import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from autocarto.core.tool_engine.executor import ProcessingToolExecutor


class _FakeCrs:
    def __init__(self, value=""):
        self.value = str(value or "")

    def isValid(self):
        return self.value.startswith("EPSG:") or self.value.startswith("+proj=")

    def authid(self):
        return self.value if self.value.startswith("EPSG:") else ""

    def description(self):
        return self.value

    def toWkt(self):
        return f"WKT[{self.value}]" if self.isValid() else ""

    def __eq__(self, other):
        return isinstance(other, _FakeCrs) and self.value == other.value


class _FakeLayer:
    def __init__(self, crs, valid=True, feature_count=1):
        self._crs = crs
        self._valid = valid
        self._feature_count = feature_count

    def isValid(self):
        return self._valid

    def crs(self):
        return self._crs

    def featureCount(self):
        return self._feature_count


class CrsReprojectionGuardrailsTestCase(unittest.TestCase):
    def setUp(self):
        self.executor = ProcessingToolExecutor({})
        self.qgis_core = SimpleNamespace(
            QgsCoordinateReferenceSystem=_FakeCrs,
            QgsVectorLayer=lambda *_args: _FakeLayer(_FakeCrs("EPSG:3857")),
        )

    def _qgis_import(self):
        return mock.patch(
            "autocarto.core.tool_engine.executor.importlib.import_module",
            return_value=self.qgis_core,
        )

    def test_display_name_is_not_accepted_as_target_crs(self):
        with self._qgis_import(), self.assertRaisesRegex(
            ValueError, "目标 CRS 无法解析"
        ):
            self.executor._validate_reprojection_request(
                "reproject_layer",
                {"TARGET_CRS": "China_Lambert_Conformal_Conic"},
            )

    def test_output_without_valid_crs_is_rejected(self):
        target = _FakeCrs("EPSG:3857")
        output = _FakeLayer(_FakeCrs(""))

        with self._qgis_import(), self.assertRaisesRegex(
            RuntimeError, "输出图层没有有效 CRS"
        ):
            self.executor._validate_reprojection_result(
                "reproject_layer",
                {"OUTPUT": output},
                target,
            )

    def test_input_without_valid_crs_is_rejected(self):
        source = _FakeLayer(_FakeCrs(""))

        with self._qgis_import(), self.assertRaisesRegex(
            ValueError, "输入图层没有有效 CRS"
        ):
            self.executor._validate_reprojection_request(
                "reproject_layer",
                {"INPUT": source, "TARGET_CRS": "EPSG:3857"},
            )

    def test_output_crs_must_match_requested_target(self):
        target = _FakeCrs("EPSG:3857")
        output = _FakeLayer(_FakeCrs("EPSG:4490"))

        with self._qgis_import(), self.assertRaisesRegex(
            RuntimeError, "输出 CRS 与目标 CRS 不一致"
        ):
            self.executor._validate_reprojection_result(
                "crs_reproject_vector",
                {"OUTPUT": output},
                target,
            )

    def test_valid_output_is_accepted(self):
        target = _FakeCrs("EPSG:3857")
        output = _FakeLayer(_FakeCrs("EPSG:3857"))

        with self._qgis_import():
            note = self.executor._validate_reprojection_result(
                "reproject_layer",
                {"OUTPUT": output},
                target,
            )

        self.assertIn("output_validation=passed", note)
        self.assertIn("EPSG:3857", note)

    def test_output_feature_count_must_match_input(self):
        target = _FakeCrs("EPSG:3857")
        source = _FakeLayer(_FakeCrs("EPSG:4490"), feature_count=3)
        output = _FakeLayer(_FakeCrs("EPSG:3857"), feature_count=2)

        with self._qgis_import(), self.assertRaisesRegex(
            RuntimeError, "输出要素数与输入不一致"
        ):
            self.executor._validate_reprojection_result(
                "reproject_layer",
                {"OUTPUT": output},
                target,
                source_layer=source,
            )

    def test_unavailable_tool_result_preserves_user_facing_error(self):
        result = self.executor._build_unavailable_result(
            {
                "requested_id": "missing:algorithm",
                "reason": "provider_missing",
                "source": "exact",
            }
        )

        self.assertFalse(result["success"])
        self.assertEqual(result["error"], result["output"])

    def test_non_reprojection_tool_is_unchanged(self):
        self.assertIsNone(
            self.executor._validate_reprojection_request("buffer", {})
        )
        self.assertEqual(
            self.executor._validate_reprojection_result("buffer", {}, None),
            "",
        )

    def test_reprojection_tools_have_real_algorithm_contract(self):
        import yaml
        plugin_root = Path(__file__).resolve().parents[1]
        for filename in ("crs_reproject_vector.yaml", "reproject_layer.yaml"):
            definition = yaml.safe_load((plugin_root/"tools/system/crs-projection"/filename).read_text())
            self.assertEqual(definition["execution"]["backend_tool"], "native:reprojectlayer")
            parameters = {entry["name"]: entry for entry in definition["spec"]["parameters"]}
            self.assertTrue(parameters["TARGET_CRS"]["required"])
            self.assertEqual(parameters["TARGET_CRS"]["type"], "crs")
            self.assertEqual(definition["execution"]["param_mapping"]["TARGET_CRS"], "{{ TARGET_CRS }}")
        raw = (plugin_root/"skills/system/crs-projection/SKILL.md").read_text()
        front = yaml.safe_load(raw.split("---", 2)[1])
        self.assertIn("reproject_layer", front["tools"])
        self.assertIn("crs_reproject_vector", front["tools"])
        self.assertFalse((plugin_root/"skills/system/crs-diagnosis-transform").exists())


if __name__ == "__main__":
    unittest.main()
