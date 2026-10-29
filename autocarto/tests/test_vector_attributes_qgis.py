"""Exercise field preparation and retries in an isolated real QGIS project."""
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
        from autocarto.tests.golden.qgis_bootstrap import isolated_qgis
        cls.profile = tempfile.TemporaryDirectory(prefix="vector-fields-profile-")
        cls.bootstrap = isolated_qgis(Path(cls.profile.name))
        cls.bootstrap.__enter__()

    @classmethod
    def tearDownClass(cls):
        cls.bootstrap.__exit__(None, None, None)
        cls.profile.cleanup()

    def setUp(self):
        from autocarto.core.tool_engine.engine import QGISToolEngine
        self.engine = QGISToolEngine(load_interactive_tools=False)
        handlers = ["join_attributes_by_field_value", "field_calculator", "advanced_python_field_calculator", "retain_fields", "text_to_float"]
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

    def test_processing_outputs_keep_ids_and_replace_only_the_same_target(self):
        import yaml
        from autocarto.core.tool_engine.definitions import ToolDefinitionLoader
        from autocarto.core.tool_engine.executor import ProcessingToolExecutor
        from autocarto.tools.system._shared.output_manager import set_session_context, clear_session_context
        from types import SimpleNamespace

        definitions = {}
        root = Path(__file__).resolve().parents[1] / "tools/system"
        for package, name in [("vector-geometry", "buffer"), ("misc-tools", "merge_vector_layers"),
                              ("vector-geometry", "clip"), ("vector-geometry", "check_validity")]:
            data = yaml.safe_load((root / package / (name + ".yaml")).read_text())
            definitions.update(ToolDefinitionLoader([{
                "name": name, "runtime": "qgis", "parameters": data["spec"]["parameters"],
                "description": data["spec"]["description"], "execution": data["execution"],
            }]).load_all())
        executor = ProcessingToolExecutor(definitions)
        point = self.make_layer("station", "Point?crs=EPSG:3857", [[]])
        boundary = QgsVectorLayer("Polygon?crs=EPSG:3857", "city", "memory")
        feature = QgsFeature()
        feature.setGeometry(QgsGeometry.fromWkt("POLYGON((-20 -20,-20 20,20 20,20 -20,-20 -20))"))
        self.assertTrue(boundary.dataProvider().addFeatures([feature])[0])
        QgsProject.instance().addMapLayer(boundary)
        owner = {"session_id": "offline", "flow_run_id": "", "step_id": ""}
        with tempfile.TemporaryDirectory(prefix="processing-output-targets-") as directory:
            set_session_context(SimpleNamespace(session_id="offline", session_dir=directory,
                                project_path="", session_subdirs={"temp": directory, "exports": directory}))
            self.addCleanup(clear_session_context)
            buffer_args = {"INPUT": point.id(), "DISTANCE": 10, "SEGMENTS": 8, "DISSOLVE": True,
                           "OUTPUT": str(Path(directory) / "service.gpkg"), "_autocarto_output_owner": owner}
            buffered = executor.execute("buffer", buffer_args)
            self.assertTrue(buffered["success"], buffered)
            buffer_id = buffered["output"]["OUTPUT"]["layer_id"]
            self.assertEqual(buffered["output"]["OUTPUT"]["name"], "为要素创建缓冲区")
            self.assertTrue(buffered["output"]["OUTPUT"]["source"].endswith("/service.gpkg"))
            merged = executor.execute("merge_vector_layers", {"LAYERS": [boundary.id()],
                        "OUTPUT": str(Path(directory) / "city.gpkg"), "_autocarto_output_owner": owner})
            self.assertTrue(merged["success"], merged)
            self.assertIsNotNone(QgsProject.instance().mapLayer(buffer_id))
            clipped = executor.execute("clip", {"INPUT": buffer_id,
                        "OVERLAY": merged["output"]["OUTPUT"]["layer_id"],
                        "OUTPUT": str(Path(directory) / "clipped.gpkg"), "_autocarto_output_owner": owner})
            self.assertTrue(clipped["success"], clipped)
            before_check = set(QgsProject.instance().mapLayers())
            before_tree = [node.layerId() for node in QgsProject.instance().layerTreeRoot().findLayers()]
            for _ in range(2):
                checked = executor.execute("check_validity", {"INPUT_LAYER": clipped["output"]["OUTPUT"]["layer_id"],
                            "METHOD": 2, "_autocarto_output_owner": owner})
                self.assertTrue(checked["success"], checked)
                self.assertEqual([checked["output"][key] for key in ("VALID_COUNT", "INVALID_COUNT", "ERROR_COUNT")], [1, 0, 0])
                self.assertEqual(checked["output_layers"], [])
                self.assertTrue(all(key not in checked["output"] for key in ("VALID_OUTPUT", "INVALID_OUTPUT", "ERROR_OUTPUT")))
                self.assertEqual(set(QgsProject.instance().mapLayers()), before_check)
                self.assertEqual([node.layerId() for node in QgsProject.instance().layerTreeRoot().findLayers()], before_tree)
            checked = executor.execute("check_validity", {
                "INPUT_LAYER": clipped["output"]["OUTPUT"]["layer_id"], "METHOD": 2,
                "VALID_OUTPUT": str(Path(directory) / "有效要素.gpkg"),
                "INVALID_OUTPUT": str(Path(directory) / "无效要素.gpkg"),
            })
            self.assertTrue(checked["success"], checked)
            self.assertEqual(checked["output"]["VALID_COUNT"], 1)
            self.assertEqual(checked["output"]["INVALID_COUNT"], 0)
            for key, expected_count in (("VALID_OUTPUT", 1), ("INVALID_OUTPUT", 0)):
                path = checked["output"][key]
                self.assertTrue(Path(path).is_file())
                output = QgsVectorLayer(path, "检查文件", "ogr")
                self.assertTrue(output.isValid())
                self.assertEqual(output.featureCount(), expected_count)
            self.assertTrue(all(not item["auto_load"] for item in checked["output_layers"]))
            self.assertEqual(set(QgsProject.instance().mapLayers()), before_check)
            # The same real destination replaces the previous owned result.
            replaced = executor.execute("buffer", buffer_args)
            self.assertTrue(replaced["success"], replaced)
            self.assertIsNone(QgsProject.instance().mapLayer(buffer_id))
            new_id = replaced["output"]["OUTPUT"]["layer_id"]
            # Another file with the same basename is a different target.
            (Path(directory) / "other").mkdir()
            another = executor.execute("buffer", {**buffer_args, "OUTPUT": "other/service.gpkg"})
            self.assertTrue(another["success"], another)
            self.assertIsNotNone(QgsProject.instance().mapLayer(new_id))
            self.assertNotEqual(new_id, another["output"]["OUTPUT"]["layer_id"])
            named = executor.execute("buffer", {**buffer_args, "OUTPUT": "站点服务范围.gpkg"})
            self.assertTrue(named["success"], named)
            self.assertEqual(named["output"]["OUTPUT"]["name"], "站点服务范围")
            self.assertTrue(named["output"]["OUTPUT"]["source"].endswith("/站点服务范围.gpkg"))

    def test_field_working_copy_export_and_reload_keep_one_business_layer(self):
        from qgis.core import QgsVectorFileWriter, QgsProviderRegistry
        from qgis.PyQt.QtGui import QColor
        from autocarto.tools.system._shared.data_loading_impl import load_vector_handler, export_vector_handler
        from autocarto.tools.system._shared.output_manager import set_session_context, clear_session_context
        from types import SimpleNamespace

        project = QgsProject.instance()
        with tempfile.TemporaryDirectory(prefix="field-business-layer-") as directory:
            original_path = str(Path(directory) / "原始站点.geojson")
            QgsVectorFileWriter.writeAsVectorFormat(self.regions, original_path, "UTF-8", self.regions.crs(), "GeoJSON")
            original_bytes = Path(original_path).read_bytes()
            project.clear()
            loaded = load_vector_handler({"path": original_path, "layer_name": "地铁站点", "show": True})
            source_id = loaded["data"]["layer_id"]
            project.mapLayer(source_id).renderer().symbol().setColor(QColor("#cb1234"))
            set_session_context(SimpleNamespace(session_id="test", session_dir=directory,
                                project_path="", session_subdirs={"exports": directory}))
            self.addCleanup(clear_session_context)
            for output, expression in (("memory:站点规范化", '"name"'), ("memory:重新整理", '"name" || \'站\'')):
                calculated = self.invoke("field_calculator", INPUT=source_id, FIELD_NAME="station_name",
                                         FIELD_TYPE=2, FORMULA=expression, OUTPUT=output)
                self.assertTrue(calculated["success"], calculated)
                source_id = calculated["output"]["layer_id"]
                QgsApplication.processEvents()
                self.assertEqual(calculated["output"]["name"], "地铁站点")
                self.assertEqual(project.mapLayer(source_id).renderer().symbol().color().name(), "#cb1234")
                self.assertEqual(len(project.layerTreeRoot().findLayers()), 1)
                self.assertEqual(len(project.mapLayers()), 1)
            advanced = self.invoke("advanced_python_field_calculator", INPUT=source_id,
                                   FIELD_NAME="label", FIELD_TYPE=2, FIELD_LENGTH=40,
                                   FIELD_PRECISION=0, GLOBAL="", FORMULA="value = <station_name>",
                                   OUTPUT="memory:Python规范化")
            self.assertTrue(advanced["success"], advanced)
            source_id = advanced["output"]["layer_id"]
            QgsApplication.processEvents()
            self.assertEqual(advanced["output"]["name"], "地铁站点")
            self.assertEqual(len(project.layerTreeRoot().findLayers()), 1)
            self.assertEqual(len(project.mapLayers()), 1)
            exported_path = str(Path(directory) / "地铁站点.gpkg")
            exported = export_vector_handler({"layer": source_id, "output": exported_path, "format": "GPKG"})
            self.assertTrue(exported["success"], exported)
            self.assertEqual(exported["data"]["layer_id"], source_id)
            self.assertEqual(project.mapLayer(source_id).providerType(), "ogr")
            reloaded = load_vector_handler({"path": exported_path, "reuse_existing": False})
            self.assertTrue(reloaded["success"], reloaded)
            self.assertEqual(reloaded["data"]["layer_id"], source_id)
            sublayer = QgsProviderRegistry.instance().querySublayers(exported_path)[0]
            explicit = load_vector_handler({"path": exported_path + "|layername=" + sublayer.name()})
            self.assertTrue(explicit["success"], explicit)
            self.assertEqual(explicit["data"]["layer_id"], source_id)
            self.assertEqual(len(project.layerTreeRoot().findLayers()), 1)
            self.assertEqual(len(project.mapLayers()), 1)
            self.assertEqual(Path(original_path).read_bytes(), original_bytes)
            self.assertEqual([f["station_name"] for f in project.mapLayer(source_id).getFeatures()], ["甲站", "乙站"])

    def test_analysis_chain_keeps_working_layers_out_of_the_layer_tree(self):
        import yaml
        from qgis.core import QgsVectorFileWriter

        project = QgsProject.instance()
        project.clear()
        root = Path(__file__).resolve().parents[1] / "tools/system"
        entries = []
        for package, name in [("crs-projection", "reproject_layer"),
                              ("vector-geometry", "buffer"), ("vector-geometry", "clip"),
                              ("vector-selection", "extract_by_attribute"),
                              ("vector-fields", "field_calculator")]:
            path = root / package / (name + ".yaml")
            definition = yaml.safe_load(path.read_text())
            entry = {**definition["metadata"], "parameters": definition["spec"]["parameters"],
                     "execution": definition["execution"]}
            if entry["execution"].get("handler"):
                module, function = entry["execution"]["handler"].rsplit(".", 1)
                entry["handler_file"] = str(path.parent / (module.replace(".", "/") + ".py"))
                entry["handler_function"] = function
            entries.append(entry)
        self.engine.update_contract(execution_manifest=entries)

        with tempfile.TemporaryDirectory(prefix="background-analysis-") as directory:
            source = QgsVectorLayer("Point?crs=EPSG:4326&field=code:string", "原始站点", "memory")
            feature = QgsFeature(source.fields())
            feature.setAttributes(["A"])
            feature.setGeometry(QgsGeometry.fromWkt("POINT(114.3 30.6)"))
            source.dataProvider().addFeatures([feature])
            filename = str(Path(directory) / "原始站点.geojson")
            QgsVectorFileWriter.writeAsVectorFormat(source, filename, "UTF-8", source.crs(), "GeoJSON")
            original_bytes = Path(filename).read_bytes()
            source = QgsVectorLayer(filename, "原始站点", "ogr")
            project.addMapLayer(source, False)

            projected = self.invoke("reproject_layer", INPUT=source.id(), TARGET_CRS="EPSG:32650",
                                    OUTPUT="memory:投影站点")
            self.assertTrue(projected["success"], projected)
            projected_id = projected["output"]["OUTPUT"]["layer_id"]
            self.assertEqual(projected["output_layers"], [projected["output"]["OUTPUT"]])
            self.assertFalse(projected["output_layers"][0]["auto_load"])
            QgsApplication.processEvents()
            self.assertEqual(project.layerTreeRoot().findLayers(), [])
            calculated = self.invoke("field_calculator", INPUT=projected_id, FIELD_NAME="label",
                                     FIELD_TYPE=2, FORMULA='"code"', OUTPUT="memory:站名整理")
            self.assertTrue(calculated["success"], calculated)
            station_id = calculated["output"]["layer_id"]
            QgsApplication.processEvents()
            self.assertEqual(project.layerTreeRoot().findLayers(), [])
            buffered = self.invoke("buffer", INPUT=station_id, DISTANCE=800, SEGMENTS=20,
                                   DISSOLVE=False, OUTPUT="memory:初始缓冲")
            self.assertTrue(buffered["success"], buffered)
            buffer_id = buffered["output"]["OUTPUT"]["layer_id"]
            self.assertEqual(buffered["output_layers"], [buffered["output"]["OUTPUT"]])
            self.assertFalse(buffered["output_layers"][0]["auto_load"])
            QgsApplication.processEvents()
            self.assertEqual(project.layerTreeRoot().findLayers(), [])

            extent = project.mapLayer(buffer_id).extent()
            extent.scale(0.5)
            boundary = QgsVectorLayer("Polygon?crs=EPSG:32650", "分析边界", "memory")
            feature = QgsFeature()
            feature.setGeometry(QgsGeometry.fromRect(extent))
            boundary.dataProvider().addFeatures([feature])
            project.addMapLayer(boundary, False)
            clipped = self.invoke("clip", INPUT=buffer_id, OVERLAY=boundary.id(), OUTPUT="memory:裁剪服务范围")
            self.assertTrue(clipped["success"], clipped)
            clip_id = clipped["output"]["OUTPUT"]["layer_id"]
            self.assertEqual(clipped["output_layers"], [clipped["output"]["OUTPUT"]])
            self.assertFalse(clipped["output_layers"][0]["auto_load"])
            QgsApplication.processEvents()
            self.assertEqual(project.layerTreeRoot().findLayers(), [])
            selected = self.invoke("extract_by_attribute", INPUT=clip_id, FIELD="code", VALUE="A",
                                   OUTPUT=str(Path(directory) / "分区提取.gpkg"))
            self.assertTrue(selected["success"], selected)
            self.assertEqual(selected["output_layers"], [selected["output"]])
            QgsApplication.processEvents()
            self.assertEqual(project.layerTreeRoot().findLayers(), [])
            result = project.mapLayer(selected["output"]["layer_id"])
            self.assertEqual(result.featureCount(), 1)
            self.assertEqual(result.crs().authid(), "EPSG:32650")
            self.assertTrue(Path(result.source().split("|", 1)[0]).is_file())
            self.assertFalse(selected["output"]["auto_load"])
            self.assertEqual(Path(filename).read_bytes(), original_bytes)

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
