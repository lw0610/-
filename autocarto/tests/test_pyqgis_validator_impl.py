import unittest

from autocarto.tools.system._shared.pyqgis_validator_impl import ScriptValidator


class ScriptValidatorSafetyTestCase(unittest.TestCase):
    def setUp(self):
        self.validator = ScriptValidator()

    def test_rejects_live_map_layers_iteration_with_removal(self):
        script = (
            "project = QgsProject.instance()\n"
            "for layer in project.mapLayers().values():\n"
            "    project.removeMapLayer(layer)\n"
        )

        result = self.validator.validate(script)

        self.assertFalse(result.is_valid)
        self.assertTrue(
            any("Dangerous script pattern" in message for message in result.errors)
        )

    def test_allows_snapshot_iteration_with_removal(self):
        script = (
            "project = QgsProject.instance()\n"
            "for layer in list(project.mapLayers().values()):\n"
            "    project.removeMapLayer(layer)\n"
        )

        result = self.validator.validate(script)

        self.assertTrue(result.is_valid, msg=str(result.errors))


if __name__ == "__main__":
    unittest.main()
