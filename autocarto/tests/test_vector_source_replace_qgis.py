"""Isolated real-QGIS replacement tests; never attach to the user's QGIS process."""

import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from autocarto.tests.golden.qgis_bootstrap import isolated_qgis, qgis_available


@unittest.skipUnless(qgis_available(), "需要 QGIS Python")
class VectorSourceReplaceQGISTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.runtime = tempfile.TemporaryDirectory(prefix="autocarto-replace-test-")
        cls.environment = mock.patch.dict(os.environ, {}, clear=False)
        cls.environment.start()
        cls.qgis = isolated_qgis(Path(cls.runtime.name))
        cls.qgis.__enter__()
        from autocarto.tools.system._shared import data_loading_impl, vector_source_replace
        cls.loader, cls.replacer = data_loading_impl, vector_source_replace

    @classmethod
    def tearDownClass(cls):
        cls.qgis.__exit__(None, None, None)
        cls.environment.stop()
        cls.runtime.cleanup()

    def setUp(self):
        from qgis.core import QgsProject, QgsVectorLayer, QgsFeatureRequest
        from qgis.PyQt.QtGui import QColor
        self.project = QgsProject.instance()
        self.project.clear()
        self.directory = tempfile.TemporaryDirectory(dir=self.runtime.name)
        self.root = Path(self.directory.name)
        self.session = self.root / "session"
        self.target = self.session / "output" / "osm" / "arbitrary-name.geojson"
        self.target.parent.mkdir(parents=True)
        self.target.write_text(json.dumps({"type": "FeatureCollection", "name": "stable-dataset", "features": [
            {"type": "Feature", "properties": {"name": "keep"}, "geometry": {"type": "LineString", "coordinates": [[110, 29], [111, 30]]}},
            {"type": "Feature", "properties": {"name": "drop"}, "geometry": {"type": "LineString", "coordinates": [[0, 0], [111, 30]]}},
        ]}), encoding="utf-8")
        self.sidecar = Path(str(self.target) + ".source.json")
        self.sidecar.write_text(json.dumps({"source": "OpenStreetMap", "license": "ODbL 1.0", "nodes": ["https://example.invalid"],
                                           "queries": ["original acquisition"], "feature_count": 2, "clipped": False,
                                           "custom_provenance": {"retain": True}}), encoding="utf-8")
        self.old_bytes, self.old_metadata = self.target.read_bytes(), self.sidecar.read_bytes()
        self.original = QgsVectorLayer(str(self.target), "original", "ogr")
        self.assertTrue(self.original.isValid())
        self.original.renderer().symbol().setColor(QColor("#d01020"))
        self.project.addMapLayer(self.original)
        self.original_id = self.original.id()
        self.corrected = self.original.materialize(QgsFeatureRequest().setFilterExpression('"name" = \'keep\''))
        self.corrected.setName("corrected")
        self.project.addMapLayer(self.corrected)
        self.params = {"session_id": "isolated", "session_dir": str(self.session),
                       "project_path": str(self.root / "test.qgz"), "layer": self.corrected.id(),
                       "replace_source": str(self.target), "format": "GeoJSON"}

    def tearDown(self):
        self.project.clear()
        from autocarto.tools.system._shared import output_manager
        output_manager.clear_session_context()
        self.directory.cleanup()

    def call(self, **updates):
        return self.loader.export_vector_handler(dict(self.params, **updates))

    def assert_original_intact(self):
        self.assertEqual(self.target.read_bytes(), self.old_bytes)
        self.assertEqual(self.sidecar.read_bytes(), self.old_metadata)
        self.assertTrue(self.original.isValid())
        self.assertEqual(self.original.id(), self.original_id)
        self.assertEqual(self.original.featureCount(), 2)
        self.assertEqual(self.original.renderer().symbol().color().name(), "#d01020")
        self.assertFalse(list(self.target.parent.glob(".autocarto-replace-*")))

    def test_replaces_explicit_source_and_preserves_loaded_ids_styles_and_provenance(self):
        from qgis.core import QgsVectorLayer, QgsPrintLayout, QgsLayoutItemMap
        from qgis.PyQt.QtGui import QColor
        other = QgsVectorLayer(str(self.target) + "|layername=stable-dataset", "another view", "ogr")
        self.assertTrue(other.isValid())
        other.renderer().symbol().setColor(QColor("#1020d0"))
        self.project.addMapLayer(other)
        other_id = other.id()
        layout = QgsPrintLayout(self.project)
        layout.initializeDefaults()
        map_item = QgsLayoutItemMap(layout)
        map_item.setLayers([self.original])
        layout.addLayoutItem(map_item)
        import yaml
        from autocarto.core.tool_engine.engine import QGISToolEngine, execute_tool
        folder = Path(__file__).resolve().parents[1] / "tools" / "system" / "data-loading"
        definition = yaml.safe_load((folder / "data_export_vector.yaml").read_text())
        module, function = definition["execution"]["handler"].rsplit(".", 1)
        self.assertIn("replace_source", definition["execution"]["param_mapping"])
        engine = QGISToolEngine(load_interactive_tools=False)
        engine.update_contract(execution_manifest=[{"name": "data_export_vector", "runtime": "qgis", "domain": "data-loading",
            "handler_file": str(folder / (module.replace(".", "/") + ".py")), "handler_function": function}])
        response = execute_tool("data_export_vector", dict(self.params, output=str(self.target)), engine=engine)
        self.assertTrue(response["success"], response)
        result = response["output"]
        self.assertTrue(result["success"], result)
        self.assertEqual(result["output"], str(self.target))
        self.assertEqual(result["data"]["feature_count"], 1)
        self.assertEqual(set(result["data"]["updated_layer_ids"]), {self.original_id, other_id})
        self.assertEqual(self.original.featureCount(), 1)
        self.assertEqual(other.featureCount(), 1)
        self.assertEqual(self.original.id(), self.original_id)
        self.assertEqual(other.id(), other_id)
        self.assertEqual(self.original.renderer().symbol().color().name(), "#d01020")
        self.assertEqual(other.renderer().symbol().color().name(), "#1020d0")
        self.assertEqual(map_item.layers()[0].id(), self.original_id)
        self.assertEqual(len(json.loads(self.target.read_text())["features"]), 1)
        self.assertEqual(json.loads(self.target.read_text())["name"], "stable-dataset")
        self.assertEqual(other.source(), str(self.target) + "|layername=stable-dataset")
        metadata = json.loads(self.sidecar.read_text())
        self.assertEqual(metadata["source"], "OpenStreetMap")
        self.assertEqual(metadata["license"], "ODbL 1.0")
        self.assertEqual(metadata["queries"], ["original acquisition"])
        self.assertEqual(metadata["custom_provenance"], {"retain": True})
        self.assertEqual(metadata["feature_count"], 1)
        self.assertEqual(metadata["corrections"][0]["previous_feature_count"], 2)
        self.assertEqual(metadata["corrections"][0]["input_layer_id"], self.corrected.id())
        self.assertFalse(list(self.target.parent.glob(".autocarto-replace-*")))

    def test_metadata_publish_failure_rolls_back_file_and_qgis_references(self):
        real_replace = os.replace
        failed = False
        def fail_sidecar_once(source, destination):
            nonlocal failed
            if Path(destination) == self.sidecar and not failed:
                failed = True
                raise OSError("simulated metadata write failure")
            return real_replace(source, destination)
        with mock.patch.object(self.replacer.os, "replace", side_effect=fail_sidecar_once):
            result = self.call()
        self.assertFalse(result["success"], result)
        self.assertEqual(result["error_type"], "replace_failed")
        self.assert_original_intact()

    def test_provider_rebind_failure_rolls_back_file_and_style(self):
        restore = self.replacer._restore_layer
        failed = False
        def fail_once(snapshot):
            nonlocal failed
            if not failed:
                failed = True
                raise RuntimeError("simulated provider failure")
            return restore(snapshot)
        with mock.patch.object(self.replacer, "_restore_layer", side_effect=fail_once):
            result = self.call()
        self.assertFalse(result["success"], result)
        self.assert_original_intact()

    def test_interrupted_commit_rolls_back_before_propagating_interrupt(self):
        real_replace = os.replace
        interrupted = False
        def interrupt_once(source, destination):
            nonlocal interrupted
            if Path(destination) == self.sidecar and not interrupted:
                interrupted = True
                raise KeyboardInterrupt()
            return real_replace(source, destination)
        with mock.patch.object(self.replacer.os, "replace", side_effect=interrupt_once):
            with self.assertRaises(KeyboardInterrupt):
                self.call()
        self.assert_original_intact()

    def test_interrupted_rollback_preserves_original_recovery_copy(self):
        real_replace = os.replace
        def interrupt_commit_and_rollback(source, destination):
            if Path(source).name in {"candidate.source.json", "original.geojson"}:
                raise KeyboardInterrupt("simulated second interruption")
            return real_replace(source, destination)
        with mock.patch.object(self.replacer.os, "replace", side_effect=interrupt_commit_and_rollback):
            with self.assertRaises(KeyboardInterrupt):
                self.call()
        backups = list(self.target.parent.glob(".autocarto-replace-*/original.geojson"))
        self.assertEqual(len(backups), 1)
        self.assertEqual(backups[0].read_bytes(), self.old_bytes)
        self.assertEqual((backups[0].parent / "original.source.json").read_bytes(), self.old_metadata)

    def test_candidate_validation_failure_never_changes_original(self):
        with mock.patch.object(self.replacer, "_write_candidate", side_effect=lambda layer, path, name: path.write_text("invalid")):
            result = self.call()
        self.assertFalse(result["success"], result)
        self.assert_original_intact()

    def test_rejects_outside_session_different_output_and_non_geojson(self):
        outside = self.root / "external.geojson"
        outside.write_bytes(self.old_bytes)
        package = self.target.with_suffix(".gpkg")
        package.write_bytes(self.old_bytes)
        for updates in [{"replace_source": str(outside)}, {"output": str(self.target.with_name("copy.geojson"))},
                        {"replace_source": str(package)}, {"format": "GPKG"}, {"replace_source": "arbitrary-name.geojson"}]:
            with self.subTest(updates=updates):
                self.assertFalse(self.call(**updates)["success"])
                self.assert_original_intact()
        self.assertEqual(outside.read_bytes(), self.old_bytes)

    def test_rejects_symlink_and_active_edits(self):
        link = self.target.with_name("link.geojson")
        link.symlink_to(self.target)
        self.assertEqual(self.call(replace_source=str(link))["error_type"], "replace_source_symlink")
        linked_dir = self.session / "output" / "alias"
        linked_dir.symlink_to(self.target.parent, target_is_directory=True)
        self.assertEqual(self.call(replace_source=str(linked_dir / self.target.name))["error_type"], "replace_source_symlink")
        self.assertTrue(self.original.startEditing())
        result = self.call()
        self.assertEqual(result["error_type"], "replace_layer_editing")
        self.original.rollBack()
        self.assert_original_intact()

    def test_same_name_requires_id_and_normal_export_still_works(self):
        self.original.setName("corrected")
        result = self.call(layer="corrected")
        self.assertEqual(result["error_type"], "ambiguous_layer")
        self.assert_original_intact()
        result = self.call(replace_source="", output="new.geojson")
        self.assertTrue(result["success"], result)
        self.assertNotEqual(result["output"], str(self.target))
        self.assertTrue(Path(result["output"]).is_file())
        self.assert_original_intact()

    def test_existing_metadata_must_be_valid_and_unrelated_stem_is_untouched(self):
        stem = self.target.with_suffix(".source.json")
        stem.write_text("unrelated", encoding="utf-8")
        self.sidecar.write_text("invalid", encoding="utf-8")
        self.assertEqual(self.call()["error_type"], "replace_metadata_invalid")
        self.assertEqual(self.target.read_bytes(), self.old_bytes)
        self.sidecar.write_bytes(self.old_metadata)
        result = self.call()
        self.assertTrue(result["success"], result)
        self.assertEqual(stem.read_text(), "unrelated")

    def test_trusted_ancestor_alias_and_unnamed_geojson(self):
        # The fixture lives under macOS /var -> /private/var. Both spellings
        # identify the same trusted session, unlike a symlink within it.
        from qgis.core import QgsVectorLayer
        self.original.setDataSource("", "original", "ogr", False)
        payload = json.loads(self.old_bytes)
        payload.pop("name")
        self.target.write_text(json.dumps(payload), encoding="utf-8")
        self.original.setDataSource(str(self.target), "original", "ogr", False)
        selector = QgsVectorLayer(str(self.target) + "|layername=" + self.target.stem, "unnamed view", "ogr")
        self.assertTrue(selector.isValid())
        self.project.addMapLayer(selector)
        result = self.call(replace_source=str(self.target.resolve()), session_dir=str(self.session.absolute()))
        self.assertTrue(result["success"], result)
        self.assertTrue(selector.isValid())
        self.assertEqual(selector.featureCount(), 1)
        self.assertEqual(json.loads(self.target.read_text())["name"], self.target.stem)


if __name__ == "__main__":
    unittest.main()
