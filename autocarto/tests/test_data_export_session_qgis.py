"""Actual QGIS export regression; run in a separate headless Python process."""
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest

try:
    from osgeo import gdal, osr
    from qgis.core import (
        QgsApplication,
        QgsFeature,
        QgsGeometry,
        QgsPointXY,
        QgsProject,
        QgsRasterLayer,
        QgsVectorLayer,
    )
    QGIS_AVAILABLE = True
except ImportError:
    QGIS_AVAILABLE = False


@unittest.skipUnless(QGIS_AVAILABLE, "QGIS Python runtime is unavailable")
class DataExportSessionQgisTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if QgsApplication.instance() is not None:
            raise unittest.SkipTest("Run this regression in its own process, outside any live QGIS project")
        cls.profile = tempfile.TemporaryDirectory(prefix="autocarto-export-qgis-")
        os.environ["QT_QPA_PLATFORM"] = "offscreen"
        os.environ["QGIS_CUSTOM_CONFIG_PATH"] = cls.profile.name
        cls.application = QgsApplication([], False, cls.profile.name, "external")
        prefix = os.environ.get("QGIS_PREFIX_PATH")
        if prefix:
            cls.application.setPrefixPath(prefix, True)
        cls.application.initQgis()
        from processing.core.Processing import Processing
        Processing.initialize()
        from autocarto.core.tool_engine.engine import QGISToolEngine, execute_tool
        from autocarto.tools.system._shared import output_manager
        cls.engine_class = QGISToolEngine
        cls.execute_tool = staticmethod(execute_tool)
        cls.output_manager = output_manager

    @classmethod
    def tearDownClass(cls):
        cls.output_manager.set_session_context(None)
        cls.application.exitQgis()
        cls.profile.cleanup()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="autocarto-export-session-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.project = QgsProject.instance()
        self.assertEqual(len(self.project.mapLayers()), 0)
        self.project_dir = self.root / "project"
        self.project_dir.mkdir()
        self.project.setFileName(str(self.project_dir / "test.qgz"))
        self.session_dir = self.root / "app-data" / "sessions" / "project-key" / "fresh-session"
        self.session_dir.mkdir(parents=True)
        self.stale_dir = self.root / "old-session"
        self.output_manager.set_session_context(None)
        handler = Path(__file__).resolve().parents[1] / "tools/system/data-loading/handlers/loader.py"
        self.engine = self.engine_class(load_interactive_tools=False)
        self.engine.update_contract(execution_manifest=[
            {
                "name": f"data_export_{kind}", "runtime": "qgis", "domain": "data-loading",
                "handler_file": str(handler), "handler_function": f"export_{kind}_handler",
            }
            for kind in ("vector", "raster")
        ])

    def tearDown(self):
        self.project.removeAllMapLayers()
        self.project.setFileName("")
        self.output_manager.set_session_context(None)

    def export(self, kind, layer_name, filename, stale=False):
        if stale:
            self.output_manager.set_session_context(SimpleNamespace(
                session_id="old-session", project_path=str(self.project_dir / "old.qgz"),
                session_dir=str(self.stale_dir), session_subdirs={},
            ))
        # Session metadata is the same trusted injection the bridge supplies;
        # it is not part of the model-visible tool schema.
        args = {
            "layer": layer_name, "output": str(self.session_dir / "output" / "henan_population" / filename),
            "session_id": "fresh-session", "session_dir": str(self.session_dir),
            "project_path": str(self.project_dir / "test.qgz"),
            "session_subdirs": {"exports": str(self.session_dir / "exports")},
        }
        if kind == "vector":
            args["format"] = "GPKG"
        result = self.execute_tool(f"data_export_{kind}", args, engine=self.engine)
        self.assertTrue(result["success"], result)
        payload = result["output"]
        output = Path(payload["output"]).resolve()
        self.assertTrue(output.is_file(), result)
        self.assertTrue(output.is_relative_to(self.session_dir.resolve()), result)
        self.assertEqual(output.parent, (self.session_dir / "exports").resolve())
        self.assertFalse((self.project_dir / "autoc").exists(), "No project-local fallback copy")
        self.assertFalse(self.stale_dir.exists(), "No export in the previous session")
        self.assertEqual([p.resolve() for p in self.root.rglob(filename)], [output])
        return output

    def test_vector_export_uses_injected_session_even_with_stale_state(self):
        for stale in (False, True):
            with self.subTest(stale=stale):
                layer = QgsVectorLayer("Point?crs=EPSG:4326&field=population:integer", f"vector-{stale}", "memory")
                feature = QgsFeature(layer.fields())
                feature.setGeometry(QgsGeometry.fromPointXY(QgsPointXY(113.6, 34.7)))
                feature["population"] = 100
                self.assertTrue(layer.dataProvider().addFeatures([feature])[0])
                self.project.addMapLayer(layer)
                output = self.export("vector", layer.name(), f"population-{stale}.gpkg", stale)
                exported = QgsVectorLayer(str(output), "verify", "ogr")
                self.assertTrue(exported.isValid())
                self.assertEqual(exported.featureCount(), 1)
                self.assertEqual(exported.crs().authid(), "EPSG:4326")
                self.assertEqual(next(exported.getFeatures())["population"], 100)

    def test_raster_export_uses_injected_session_even_with_stale_state(self):
        source = self.root / "source.tif"
        dataset = gdal.GetDriverByName("GTiff").Create(str(source), 8, 8, 1, gdal.GDT_UInt16)
        dataset.SetGeoTransform((250000, 10, 0, 3400000, 0, -10))
        crs = osr.SpatialReference()
        crs.ImportFromEPSG(32650)
        dataset.SetProjection(crs.ExportToWkt())
        dataset.GetRasterBand(1).Fill(123)
        dataset = None
        layer = QgsRasterLayer(str(source), "raster")
        self.assertTrue(layer.isValid())
        self.project.addMapLayer(layer)
        for stale in (False, True):
            with self.subTest(stale=stale):
                output = self.export("raster", layer.name(), f"raster-{stale}.tif", stale)
                exported = QgsRasterLayer(str(output), "verify")
                self.assertTrue(exported.isValid())
                self.assertEqual(exported.crs().authid(), "EPSG:32650")
                value, ok = exported.dataProvider().sample(QgsPointXY(250005, 3399995), 1)
                self.assertTrue(ok)
                self.assertEqual(value, 123)


if __name__ == "__main__":
    unittest.main()
