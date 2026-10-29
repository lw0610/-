"""Real QGIS coverage for selected-layer ID input and the advertised save tool."""
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest

try:
    from qgis.core import QgsApplication, QgsFeature, QgsGeometry, QgsProject, QgsVectorLayer
    QGIS_AVAILABLE = True
except ImportError:
    QGIS_AVAILABLE = False


@unittest.skipUnless(QGIS_AVAILABLE, "requires QGIS Python")
class VectorSelectionQgisTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if QgsApplication.instance() is not None:
            raise unittest.SkipTest("run in an isolated process")
        os.environ["QT_QPA_PLATFORM"] = "offscreen"
        cls.profile = tempfile.TemporaryDirectory(prefix="vector-selection-profile-")
        cls.app = QgsApplication([], False, cls.profile.name)
        cls.app.initQgis()
        sys.path.insert(0, str(Path(QgsApplication.pkgDataPath()) / "python/plugins"))
        from processing.core.Processing import Processing
        Processing.initialize()

    @classmethod
    def tearDownClass(cls):
        QgsProject.instance().clear()
        cls.app.exitQgis()
        cls.profile.cleanup()

    def setUp(self):
        import yaml
        from autocarto.core.tool_engine.engine import QGISToolEngine
        root = Path(__file__).resolve().parents[1] / "tools/system"
        entries = []
        for relative in ["vector-selection/extract_by_attribute.yaml", "file-operations/save_vector_features_to_file.yaml", "file-operations/package_layers.yaml"]:
            path = root / relative
            definition = yaml.safe_load(path.read_text())
            entry = {**definition["metadata"], "parameters": definition["spec"]["parameters"], "execution": definition["execution"]}
            if entry["execution"].get("handler"):
                module, function = entry["execution"]["handler"].rsplit(".", 1)
                entry["handler_file"] = str(path.parent / (module.replace(".", "/") + ".py"))
                entry["handler_function"] = function
            entries.append(entry)
        self.engine = QGISToolEngine(load_interactive_tools=False)
        self.engine.update_contract(execution_manifest=entries)
        self.layer = QgsVectorLayer("Point?crs=EPSG:4326&field=name:string", "行政区样例", "memory")
        features = []
        for name in ["郑州市", "开封市"]:
            feature = QgsFeature(self.layer.fields())
            feature.setAttributes([name])
            feature.setGeometry(QgsGeometry.fromWkt("POINT(113.6 34.7)"))
            features.append(feature)
        self.assertTrue(self.layer.dataProvider().addFeatures(features)[0])
        QgsProject.instance().addMapLayer(self.layer)

    def tearDown(self):
        from autocarto.tools.system._shared.output_manager import set_session_context
        set_session_context(None)
        QgsProject.instance().clear()

    def test_extract_accepts_actual_layer_id_and_name(self):
        for reference, output in [(self.layer.id(), "memory:选中区域"), (self.layer.name(), "提取区域")]:
            result = self.engine.invoke("extract_by_attribute", {"INPUT": reference, "FIELD": "name", "OPERATOR": 0, "VALUE": "郑州市", "OUTPUT": output})
            self.assertTrue(result["success"], result)
            selected = QgsProject.instance().mapLayer(result["output"]["layer_id"])
            self.assertEqual(selected.featureCount(), 1)
            self.assertEqual(selected.providerType(), "memory")
            self.assertEqual(selected.name(), output.removeprefix("memory:"))
            self.assertEqual(next(selected.getFeatures())["name"], "郑州市")
        self.assertEqual(self.layer.featureCount(), 2)

    def test_extract_file_outputs_are_real_layers_with_readable_references(self):
        project = QgsProject.instance()
        source = QgsVectorLayer("Polygon?crs=EPSG:4326&field=adcode:string", "分区服务范围", "memory")
        features = []
        for index, code in enumerate(["420102", "420103"]):
            feature = QgsFeature(source.fields())
            feature.setAttributes([code])
            feature.setGeometry(QgsGeometry.fromWkt(
                f"POLYGON(({index} 0,{index + 1} 0,{index + 1} 1,{index} 1,{index} 0))"
            ))
            features.append(feature)
        self.assertTrue(source.dataProvider().addFeatures(features)[0])
        project.addMapLayer(source)
        with tempfile.TemporaryDirectory(prefix="selection-files-") as directory:
            for code, extension in [("420102", "shp"), ("420103", "gpkg")]:
                args = {
                    "INPUT": source.id(), "FIELD": "adcode", "VALUE": code,
                    "OUTPUT": str(Path(directory) / "output" / f"county_{code}.{extension}"),
                    "session_id": "selection-files", "session_dir": directory,
                    "_autocarto_output_owner": {"session_id": "selection-files", "step_id": "extract"},
                }
                result = self.engine.invoke("extract_by_attribute", args)
                self.assertTrue(result["success"], result)
                reference = result["output"]
                path = Path(reference["source"].split("|", 1)[0])
                self.assertTrue(path.is_file(), result)
                selected = project.mapLayer(reference["layer_id"])
                self.assertEqual(selected.providerType(), "ogr")
                self.assertEqual(selected.name(), "分区服务范围_提取结果")
                self.assertFalse(reference["auto_load"])
                # A downstream consumer can open the reported path outside the project.
                reopened = QgsVectorLayer(str(path), "读取结果", "ogr")
                self.assertTrue(reopened.isValid())
                self.assertEqual(reopened.featureCount(), 1)
                self.assertEqual(next(reopened.getFeatures())["adcode"], code)
                reopened = None
                previous_id = selected.id()
                selected = None
                count = len(project.mapLayers())
                again = self.engine.invoke("extract_by_attribute", args)
                self.assertTrue(again["success"], again)
                self.assertEqual(len(project.mapLayers()), count)
                self.assertIsNone(project.mapLayer(previous_id))
                self.assertEqual(again["output"]["source"], reference["source"])
                self.assertIsNotNone(project.mapLayer(again["output"]["layer_id"]))

    def test_advertised_save_tool_executes_real_native_algorithm(self):
        from autocarto.core.tool_engine.engine import execute_tool
        self.assertIsNotNone(QgsApplication.processingRegistry().algorithmById("native:savefeatures"))
        with tempfile.TemporaryDirectory(prefix="vector-save-") as directory:
            from autocarto.tools.system._shared.output_manager import set_session_context
            path = Path(directory) / "exports" / "actual.gpkg"
            set_session_context(SimpleNamespace(session_id="selection-test", project_path=str(Path(directory)/"project.qgz"), session_dir=directory, session_subdirs={"exports": str(path.parent)}))
            result = execute_tool("save_vector_features_to_file", {"INPUT": self.layer.id(), "OUTPUT": str(path)}, engine=self.engine)
            self.assertTrue(result["success"], result)
            self.assertEqual(result["output"]["OUTPUT"], str(path))
            self.assertEqual(result["output_layers"], [{"path": str(path), "auto_load": False}])
            self.assertTrue(path.is_file(), result)
            saved = QgsVectorLayer(str(path), "verify", "ogr")
            self.assertTrue(saved.isValid())
            self.assertEqual(saved.featureCount(), 2)
            self.assertEqual({feature["name"] for feature in saved.getFeatures()}, {"郑州市", "开封市"})

    def test_package_two_real_layers_exposes_file_and_sublayers(self):
        from autocarto.core.tool_engine.engine import execute_tool
        self.assertIsNotNone(QgsApplication.processingRegistry().algorithmById("native:package"))
        second = self.layer.clone()
        second.setName("第二个图层")
        QgsProject.instance().addMapLayer(second)
        with tempfile.TemporaryDirectory(prefix="vector-package-") as directory:
            from autocarto.tools.system._shared.output_manager import set_session_context
            path = Path(directory) / "exports" / "two.gpkg"
            set_session_context(SimpleNamespace(session_id="package-test", project_path="", session_dir=directory, session_subdirs={"exports": str(path.parent)}))
            result = execute_tool("package_layers", {"LAYERS": [self.layer.id(), second.id()], "OUTPUT": str(path)}, engine=self.engine)
            self.assertTrue(result["success"], result)
            self.assertEqual(result["output"]["OUTPUT"], str(path))
            self.assertEqual(result["output_layers"], [{"path": str(path), "auto_load": False}])
            self.assertEqual(len(result["output"]["OUTPUT_LAYERS"]), 2)
            for uri in result["output"]["OUTPUT_LAYERS"]:
                reopened = QgsVectorLayer(uri, "verify", "ogr")
                self.assertTrue(reopened.isValid(), uri)
                self.assertEqual(reopened.featureCount(), 2)
                self.assertEqual({feature["name"] for feature in reopened.getFeatures()}, {"郑州市", "开封市"})
