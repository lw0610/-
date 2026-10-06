"""Real provider round trip in an isolated process; no live project is opened."""
import csv
import json
import os
from pathlib import Path
import tempfile
import unittest

try:
    from qgis.core import QgsApplication, QgsProject, QgsVariantUtils, QgsVectorLayer
    QGIS_AVAILABLE = True
except ImportError:
    QGIS_AVAILABLE = False

from autocarto.tools.system._shared.statistical_data_impl import prepare_statistical_handler


@unittest.skipUnless(QGIS_AVAILABLE, "QGIS Python runtime is unavailable")
class StatisticalDataQgisTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if QgsApplication.instance() is not None:
            raise unittest.SkipTest("run in a separate QGIS process")
        cls.profile = tempfile.TemporaryDirectory(prefix="autocarto-statistical-profile-")
        os.environ["QT_QPA_PLATFORM"] = "offscreen"
        cls.app = QgsApplication([], False, cls.profile.name)
        cls.app.initQgis()

    @classmethod
    def tearDownClass(cls):
        cls.app.exitQgis()
        cls.profile.cleanup()

    def test_geojson_provider_preserves_numeric_field_null_zero_and_crs(self):
        with tempfile.TemporaryDirectory(prefix="autocarto-statistical-qgis-") as raw_root:
            root = Path(raw_root)
            source = root / "regions.geojson"
            table = root / "values.csv"
            features = []
            for index, name in enumerate(["甲", "乙", "丙"]):
                features.append({"type": "Feature", "properties": {"name": name}, "geometry": {"type": "MultiPolygon", "coordinates": [[[[index, 0], [index + 1, 0], [index + 1, 1], [index, 1], [index, 0]]]]}})
            source.write_text(json.dumps({"type": "FeatureCollection", "features": features}, ensure_ascii=False), encoding="utf-8")
            with table.open("w", encoding="utf-8-sig", newline="") as handle:
                csv.writer(handle).writerows([["年份", "甲", "乙", "丙"], [2024, 0, "", 12.5]])
            from autocarto.core.tool_engine.engine import QGISToolEngine, execute_tool
            engine = QGISToolEngine(load_interactive_tools=False)
            wrapper = Path(__file__).resolve().parents[1] / "tools/system/statistical-data/handlers/preparation.py"
            engine.update_contract(execution_manifest=[{
                "name": "data_prepare_statistical", "runtime": "qgis", "domain": "statistical-data",
                "handler_file": str(wrapper), "handler_function": "prepare_statistical_handler",
            }])
            executed = execute_tool("data_prepare_statistical", {"geojson_path": str(source), "csv_path": str(table), "year": 2024, "output_dir": str(root / "outputs"), "unit": "人", "source": "synthetic test"}, engine=engine)
            self.assertTrue(executed["success"], executed)
            self.assertEqual(executed["output_layers"], [], "Preparation must not auto-load its derivative")
            self.assertEqual(len(QgsProject.instance().mapLayers()), 0)
            result = executed["output"]
            self.assertTrue(result["success"], result)
            self.assertTrue(result["output"].endswith("manifest.json"))
            layer = QgsVectorLayer(result["data"]["paths"]["polygons_geojson"], "statistics", "ogr")
            self.assertTrue(layer.isValid())
            self.assertEqual(layer.featureCount(), 3)
            self.assertEqual(layer.crs().authid(), "EPSG:4326")
            self.assertTrue(layer.fields().field("stat_value").isNumeric())
            loaded = {feature["name"]: feature for feature in layer.getFeatures()}
            self.assertEqual(loaded["甲"]["stat_value"], 0)
            self.assertTrue(QgsVariantUtils.isNull(loaded["乙"]["stat_value"]))
            self.assertEqual(loaded["乙"]["data_status"], "missing")
            self.assertEqual(loaded["丙"]["stat_value"], 12.5)
            self.assertEqual(loaded["甲"]["stat_unit"], "人")
            del layer


if __name__ == "__main__":
    unittest.main()
