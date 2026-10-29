"""Exercise field preparation and retries in an isolated real QGIS project."""
import os
from pathlib import Path
import tempfile
import unittest

try:
    from qgis.core import QgsApplication, QgsFeature, QgsGeometry, QgsProject, QgsVectorLayer
    QGIS_AVAILABLE = True
except ImportError:
    QGIS_AVAILABLE = False


@unittest.skipUnless(QGIS_AVAILABLE, "requires QGIS Python")
class VectorAttributesQgisTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if QgsApplication.instance() is not None:
            raise unittest.SkipTest("run in an isolated process")
        os.environ["QT_QPA_PLATFORM"] = "offscreen"
        cls.profile = tempfile.TemporaryDirectory(prefix="vector-fields-profile-")
        cls.app = QgsApplication([], False, cls.profile.name)
        cls.app.initQgis()

    @classmethod
    def tearDownClass(cls):
        QgsProject.instance().clear()
        cls.app.exitQgis()
        cls.profile.cleanup()

    def setUp(self):
        from autocarto.core.tool_engine.engine import QGISToolEngine
        self.engine = QGISToolEngine(load_interactive_tools=False)
        handlers = ["join_attributes_by_field_value", "field_calculator", "retain_fields", "text_to_float"]
        wrapper = Path(__file__).resolve().parents[1] / "tools/system/vector-fields/handlers/attributes.py"
        self.engine.update_contract(execution_manifest=[{
            "name": name, "runtime": "qgis", "domain": "vector-fields",
            "handler_file": str(wrapper), "handler_function": name + "_handler",
        } for name in handlers])
        self.owner = {"session_id": "test", "flow_run_id": "run", "step_id": "prepare"}
        self.regions = self.make_layer("regions", "Point?crs=EPSG:4326&field=adcode:string&field=name:string", [["4101", "甲"], ["4102", "乙"]])
        self.table = self.make_layer("table", "None?field=adcode:string&field=population:string", [["4101", "12.5"], ["4102", "0"]])

    def tearDown(self):
        QgsProject.instance().clear()

    def make_layer(self, name, uri, rows):
        layer = QgsVectorLayer(uri, name, "memory")
        self.assertTrue(layer.isValid())
        features = []
        for row in rows:
            feature = QgsFeature(layer.fields())
            feature.setAttributes(row)
            if layer.isSpatial():
                feature.setGeometry(QgsGeometry.fromWkt("POINT(0 0)"))
            features.append(feature)
        self.assertTrue(layer.dataProvider().addFeatures(features)[0])
        QgsProject.instance().addMapLayer(layer)
        return layer

    def invoke(self, name, **params):
        from autocarto.core.tool_engine.engine import execute_tool
        return execute_tool(name, {**params, "_autocarto_output_owner": self.owner}, engine=self.engine)

    def join(self):
        return self.invoke("join_attributes_by_field_value", INPUT=self.regions.id(), FIELD="adcode",
                           INPUT_2=self.table.id(), FIELD_2="adcode", OUTPUT="memory:thematic")

    def test_repeated_join_calculate_retain_replaces_only_owned_result(self):
        # The user's similarly named layer must survive every retry.
        original = self.make_layer("thematic", "Point?field=original:string", [["keep"]])
        original_id = original.id()
        for _ in range(2):
            joined = self.join()
            self.assertTrue(joined["success"], joined)
            ref = joined["output"]
            self.assertEqual(joined["output_layers"][0], ref)
            self.assertEqual(ref["reference_type"], "project_layer")
            self.assertFalse(ref["auto_load"])
            calculated = self.invoke("field_calculator", INPUT=ref["layer_id"],
                                     FIELD_NAME="pop", FIELD_TYPE=1, FORMULA='to_real("population")',
                                     OUTPUT="memory:thematic")
            self.assertTrue(calculated["success"], calculated)
            self.assertIsNone(QgsProject.instance().mapLayer(ref["layer_id"]))
            retained = self.invoke("retain_fields", INPUT=calculated["output"]["layer_id"],
                                  FIELDS="name;adcode;pop", OUTPUT="memory:thematic")
            self.assertTrue(retained["success"], retained)
            target = QgsProject.instance().mapLayer(retained["output"]["layer_id"])
            self.assertEqual(target.fields().names(), ["adcode", "name", "pop"])
            self.assertEqual([f["pop"] for f in target.getFeatures()], [12.5, 0])
            self.assertEqual(len(QgsProject.instance().mapLayers()), 4)
            self.assertIsNotNone(QgsProject.instance().mapLayer(original_id))
        from autocarto.tools.system._shared.project_state_impl import get_active_layer_info
        self.assertFalse(get_active_layer_info({"layer_name": "thematic"})["success"])
        self.assertTrue(get_active_layer_info({"layer_name": target.id()})["success"])

    def test_invalid_fields_and_formula_leave_no_output_or_partial_edit(self):
        joined = self.join()
        layer_id = joined["output"]["layer_id"]
        layer = QgsProject.instance().mapLayer(layer_id)
        before = layer.fields().names()
        failed = self.invoke("retain_fields", INPUT=layer_id, FIELDS="name;missing", OUTPUT="memory:broken")
        self.assertFalse(failed["success"])
        for output in ("memory:broken", "memory:"):
            failed = self.invoke("field_calculator", INPUT=layer_id, FIELD_NAME="new_field", FIELD_TYPE=1,
                                 FORMULA='to_real("missing_field")', OUTPUT=output)
            self.assertFalse(failed["success"], failed)
            self.assertEqual(layer.fields().names(), before)
            self.assertFalse(layer.isEditable())
        self.assertEqual(len(QgsProject.instance().mapLayers()), 3)

    def test_ambiguous_name_requires_id_and_other_step_output_is_preserved(self):
        first = self.join()
        first_id = first["output"]["layer_id"]
        self.owner = {**self.owner, "step_id": "other"}
        second = self.join()
        self.assertTrue(second["success"])
        self.assertIsNotNone(QgsProject.instance().mapLayer(first_id))
        failed = self.invoke("text_to_float", INPUT="thematic", FIELD="population", OUTPUT="memory:converted")
        self.assertFalse(failed["success"])
        self.assertIn("不唯一", failed["error"])
        converted = self.invoke("text_to_float", INPUT=second["output"]["layer_id"], FIELD="population", OUTPUT="memory:thematic")
        self.assertTrue(converted["success"], converted)
        layer = QgsProject.instance().mapLayer(converted["output"]["layer_id"])
        self.assertTrue(layer.fields().field("population").isNumeric())
        self.assertEqual(len(QgsProject.instance().mapLayers()), 4)


if __name__ == "__main__":
    unittest.main()
