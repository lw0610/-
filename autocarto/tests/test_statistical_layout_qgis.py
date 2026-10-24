"""Real QGIS layout regression; run only in an isolated headless process."""
from autocarto.tests.support.resource_handles import load_handle, resolve_handle
import importlib.util
from pathlib import Path
import tempfile
import unittest
import uuid

try:
    from qgis.PyQt.QtCore import QSize
    from qgis.core import (
        QgsApplication, QgsCoordinateReferenceSystem, QgsCoordinateTransform,
        QgsFeature, QgsGeometry, QgsLayoutExporter, QgsLayoutItemLegend,
        QgsLayoutItemMap, QgsLayoutPoint, QgsLayoutSize, QgsPrintLayout,
        QgsProject, QgsRectangle, QgsVectorLayer,
    )
    QGIS_AVAILABLE = True
except ImportError:
    QGIS_AVAILABLE = False


@unittest.skipUnless(QGIS_AVAILABLE, "QGIS Python runtime is unavailable")
class StatisticalLayoutQgisTestCase(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if QgsApplication.instance() is not None:
            raise unittest.SkipTest("run layout tests in an isolated QGIS process")
        cls.profile = tempfile.TemporaryDirectory(prefix="autocarto-stat-layout-")
        cls.app = QgsApplication([], False, cls.profile.name)
        cls.app.initQgis()
        from autocarto.tools.system._shared import print_layout_items_impl, print_layout_advanced_impl, style_thematic_impl
        cls.items = print_layout_items_impl
        cls.advanced = print_layout_advanced_impl
        cls.styles = style_thematic_impl

    @classmethod
    def tearDownClass(cls):
        QgsProject.instance().clear()
        cls.app.exitQgis()
        cls.profile.cleanup()

    def setUp(self):
        self.project = QgsProject.instance()
        self.project.setCrs(QgsCoordinateReferenceSystem("EPSG:3857"))
        self.layout = QgsPrintLayout(self.project)
        self.layout.initializeDefaults()
        self.layout.setName("stat_" + uuid.uuid4().hex)
        self.project.layoutManager().addLayout(self.layout)
        self.map = QgsLayoutItemMap(self.layout)
        self.map.setId("map")
        self.map.setCrs(self.project.crs())
        self.map.attemptResize(QgsLayoutSize(180, 130))
        self.map.attemptMove(QgsLayoutPoint(10, 20))
        self.layout.addLayoutItem(self.map)
        self.map.zoomToExtent(QgsRectangle(0, 0, 100, 100))

    def tearDown(self):
        self.project.clear()

    def layer(self, name="regions", x=105, y=25, values=(0, 2, 3, 7, 10, None)):
        layer = QgsVectorLayer("Polygon?crs=EPSG:4326&field=value:double", name, "memory")
        for index, value in enumerate(values):
            offset = index * 0.5
            feature = QgsFeature(layer.fields())
            feature.setAttributes([value])
            feature.setGeometry(QgsGeometry.fromWkt(
                f"POLYGON(({x+offset} {y},{x+offset+0.4} {y},{x+offset+0.4} {y+1},{x+offset} {y+1},{x+offset} {y}))"
            ))
            self.assertTrue(layer.dataProvider().addFeatures([feature])[0])
        layer.updateExtents()
        self.project.addMapLayer(layer)
        return layer

    def extent(self, **kwargs):
        return self.items.set_map_extent_handler({"layout": self.layout.name(), "item_id": "map", **kwargs})

    def legend(self):
        result = self.items.add_legend_handler({
            "layout": self.layout.name(), "map_id": "map", "item_id": "legend",
            "x": 200, "y": 30, "width": 70, "height": 100,
            "auto_update_model": False, "resize_to_contents": False,
        })
        self.assertTrue(result["success"], result)
        return self.layout.itemById("legend")

    def style_legend(self, **kwargs):
        return self.advanced.set_legend_style_handler({
            "layout": self.layout.name(), "item_id": "legend", **kwargs,
        })

    def test_layer_extent_transforms_geometry_ignores_other_layers_and_preserves_frame(self):
        layer = self.layer()
        self.layer("unrelated", x=-140, y=-40)
        frame = self.advanced._item_properties(self.map)["rect_mm"]
        result = self.extent(layer=layer.id(), padding=0)
        self.assertTrue(result["success"], result)
        self.assertEqual(result["data"]["rect_mm"], frame)
        self.assertEqual(result["data"]["crs"], "EPSG:3857")
        target = QgsCoordinateTransform(layer.crs(), self.map.crs(), self.project).transformBoundingBox(layer.extent())
        requested = result["data"]["requested_extent"]
        self.assertAlmostEqual(requested["xmin"], target.xMinimum(), places=5)
        self.assertAlmostEqual(requested["ymax"], target.yMaximum(), places=5)
        self.assertGreater(self.map.extent().xMinimum(), 10000000)
        actual = self.map.extent()
        self.assertLessEqual(actual.xMinimum(), target.xMinimum() + 1e-6)
        self.assertLessEqual(actual.yMinimum(), target.yMinimum() + 1e-6)
        self.assertGreaterEqual(actual.xMaximum(), target.xMaximum() - 1e-6)
        self.assertGreaterEqual(actual.yMaximum(), target.yMaximum() - 1e-6)
        padded = self.extent(layer=layer.id(), padding=0.1)
        self.assertAlmostEqual(padded["data"]["requested_extent"]["xmax"] - padded["data"]["requested_extent"]["xmin"], target.width() * 1.2)

    def test_explicit_extent_and_invalid_requests_do_not_mutate_map(self):
        layer = self.layer()
        self.layer(name=layer.name())
        result = self.extent(xmin=1000, ymin=2000, xmax=19000, ymax=15000, padding=0)
        self.assertTrue(result["success"], result)
        before = QgsRectangle(self.map.extent())
        for request in [
            {"xmin": 1}, {"xmin": 2, "ymin": 0, "xmax": 1, "ymax": 1},
            {"xmin": 0, "ymin": 0, "xmax": float("nan"), "ymax": 1},
            {"layer": layer.id(), "use_canvas": True}, {"layer": layer.name()},
            {"layer": layer.id(), "padding": -0.1}, {},
        ]:
            with self.subTest(request=request):
                self.assertFalse(self.extent(**request)["success"])
                self.assertEqual(self.map.extent(), before)

    def test_whitelist_restores_layers_by_id_rejects_ambiguity_and_has_no_implicit_arrow(self):
        first = self.layer("duplicate")
        second = self.layer("duplicate", x=110)
        legend = self.legend()
        self.assertFalse(legend.autoUpdateModel())
        self.assertFalse(legend.resizeToContents())
        initial_rect = self.advanced._item_properties(legend)["rect_mm"]
        first_result = self.style_legend(legend_layers=[first.id()])
        self.assertTrue(first_result["success"], first_result)
        self.assertEqual([node.layerId() for node in legend.model().rootGroup().findLayers()], [first.id()])
        restored = self.style_legend(legend_layers=[second.id(), first.id()])
        self.assertTrue(restored["success"], restored)
        self.assertEqual([node.layerId() for node in legend.model().rootGroup().findLayers()], [second.id(), first.id()])
        self.assertEqual(restored["data"]["rect_mm"], initial_rect)
        self.assertEqual(len(self.project.mapLayers()), 2)
        bad = self.style_legend(legend_layers=["duplicate"])
        self.assertFalse(bad["success"], bad)
        self.assertEqual([node.layerId() for node in legend.model().rootGroup().findLayers()], [second.id(), first.id()])
        with_arrow = self.style_legend(legend_layers=[first.id()], legend_symbols=load_handle("route-arrow"), legend_extra_items=[{"label": "路线箭头", "symbol_type": "route_arrow"}])
        self.assertTrue(with_arrow["success"], with_arrow)
        self.assertEqual([item["name"] for item in with_arrow["data"]["legend_layers"]], ["duplicate", "路线箭头"])
        self.assertTrue(self.style_legend(legend_layers=[first.id()], legend_symbols=load_handle("route-arrow"), legend_extra_items=[{"label": "路线箭头", "symbol_type": "route_arrow"}])["success"])
        self.assertEqual(len(self.project.mapLayers()), 3)
        without_arrow = self.style_legend(legend_extra_items=[])
        self.assertTrue(without_arrow["success"], without_arrow)
        self.assertEqual([item["id"] for item in without_arrow["data"]["legend_layers"]], [first.id()])

    def test_legend_can_explicitly_enable_auto_update_and_resize(self):
        self.layer()
        result = self.items.add_legend_handler({
            "layout": self.layout.name(), "map_id": "map", "item_id": "dynamic",
            "auto_update_model": True, "resize_to_contents": True,
        })
        self.assertTrue(result["success"], result)
        legend = self.layout.itemById("dynamic")
        self.assertTrue(legend.autoUpdateModel())
        self.assertTrue(legend.resizeToContents())

    def test_actual_graduated_legend_matches_renderer_and_layout_exports(self):
        layer = self.layer()
        rendered = self.styles.graduated_style_handler({"layer": layer.name(), "field": "value", "classes": 3, "method": "jenks", "color_ramp": "Blues"})
        self.assertTrue(rendered["success"], rendered)
        locked = self.advanced.set_map_layers_handler({"layout": self.layout.name(), "item_id": "map", "layers": [layer.id()]})
        self.assertTrue(locked["success"], locked)
        self.assertEqual(locked["data"]["layer_ids"], [layer.id()])
        self.assertTrue(locked["data"]["keep_layer_set"])
        self.assertTrue(self.extent(layer=layer.id())["success"])
        self.legend()
        styled = self.style_legend(legend_layers=[layer.id()], title="Test indicator (units)")
        self.assertTrue(styled["success"], styled)
        self.assertEqual(styled["data"]["linked_map"], "map")
        self.assertEqual([entry["label"] for entry in styled["data"]["legend_layers"][0]["items"]], [item.label() for item in layer.renderer().ranges()])
        image = QgsLayoutExporter(self.layout).renderPageToImage(0, QSize(1100, 780))
        self.assertFalse(image.isNull())
        with tempfile.TemporaryDirectory() as out:
            target = str(Path(out) / "statistical.png")
            settings = QgsLayoutExporter.ImageExportSettings()
            settings.dpi = 96
            self.assertEqual(QgsLayoutExporter(self.layout).exportToImage(target, settings), QgsLayoutExporter.Success)
            self.assertGreater(Path(target).stat().st_size, 1000)

    def test_extent_handler_is_exported_from_contract_module(self):
        path = Path(__file__).resolve().parents[1] / "tools/system/print-layout/handlers/layout_items.py"
        spec = importlib.util.spec_from_file_location("statistical_layout_handlers", path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        self.assertTrue(callable(module.set_map_extent_handler))
        self.assertIn("set_map_extent_handler", module.__all__)


if __name__ == "__main__":
    unittest.main()
