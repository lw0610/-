"""Reproduce updated wrappers invoking stale cached modules in isolated QGIS."""
import importlib.util
import os
from pathlib import Path
import sys
import tempfile
import types
import unittest

try:
    from qgis.core import (
        QgsApplication, QgsCoordinateReferenceSystem, QgsFeature, QgsGeometry,
        QgsLayoutItemLabel, QgsLayoutItemMap, QgsPrintLayout, QgsProject, QgsVectorLayer,
    )
    QGIS_AVAILABLE = True
except ImportError:
    QGIS_AVAILABLE = False


@unittest.skipUnless(QGIS_AVAILABLE, "requires isolated QGIS Python")
class LayoutModuleReloadQgisTest(unittest.TestCase):
    MODULES = (
        "autocarto.tools.system._shared.print_layout_items_impl",
        "autocarto.tools.system._shared.print_layout_advanced_impl",
    )

    @classmethod
    def setUpClass(cls):
        if QgsApplication.instance() is not None:
            raise unittest.SkipTest("run in a separate headless QGIS process")
        cls.profile = tempfile.TemporaryDirectory(prefix="layout-reload-profile-")
        cls.app = QgsApplication([], False, cls.profile.name)
        cls.app.initQgis()
        from autocarto.tools.system._shared import layout_module_loader
        cls.loader = layout_module_loader
        cls.system = Path(__file__).resolve().parents[1] / "tools/system"

    @classmethod
    def tearDownClass(cls):
        QgsProject.instance().clear()
        cls.app.exitQgis()
        cls.profile.cleanup()

    def setUp(self):
        self.saved_modules = {name: sys.modules.get(name) for name in self.MODULES}
        self.parent = sys.modules["autocarto.tools.system._shared"]
        self.saved_attrs = {name: getattr(self.parent, name.rsplit(".", 1)[1], None) for name in self.MODULES}

    def tearDown(self):
        QgsProject.instance().clear()
        for name in self.MODULES:
            if self.saved_modules[name] is None:
                sys.modules.pop(name, None)
            else:
                sys.modules[name] = self.saved_modules[name]
            child = name.rsplit(".", 1)[1]
            if self.saved_attrs[name] is None:
                if hasattr(self.parent, child):
                    delattr(self.parent, child)
            else:
                setattr(self.parent, child, self.saved_attrs[name])

    def wrapper(self, name):
        path = self.system / "print-layout/handlers" / f"{name}.py"
        spec = importlib.util.spec_from_file_location("reload_test_" + name, path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def test_existing_layout_survives_old_cached_modules_and_new_tools_work(self):
        project = QgsProject.instance()
        project.setCrs(QgsCoordinateReferenceSystem("EPSG:4326"))
        layer = QgsVectorLayer("Polygon?crs=EPSG:4326", "regions", "memory")
        feature = QgsFeature()
        feature.setGeometry(QgsGeometry.fromWkt("POLYGON((105 25,106 25,106 26,105 26,105 25))"))
        layer.dataProvider().addFeatures([feature])
        layer.updateExtents()
        project.addMapLayer(layer)
        layout = QgsPrintLayout(project)
        layout.initializeDefaults()
        layout.setName("existing user layout")
        project.layoutManager().addLayout(layout)
        retained = QgsLayoutItemLabel(layout)
        retained.setId("retained")
        retained.setText("Keep this existing element")
        layout.addLayoutItem(retained)
        for name in self.MODULES:
            stale = types.ModuleType(name)
            stale.__file__ = "/old/plugin/" + name.rsplit(".", 1)[1] + ".py"
            sys.modules[name] = stale
            setattr(self.parent, name.rsplit(".", 1)[1], stale)

        items, advanced = self.wrapper("layout_items"), self.wrapper("layout_advanced")
        self.assertFalse(hasattr(sys.modules[self.MODULES[0]], "set_map_extent_handler"))
        base = {"layout": layout.name()}
        result = items.add_map_item_handler({**base, "item_id": "map"})
        self.assertTrue(result["success"], result)
        self.assertIsInstance(layout.itemById("map"), QgsLayoutItemMap)
        loaded_items = sys.modules[self.MODULES[0]]
        result = items.add_label_handler({**base, "item_id": "title", "text": "Statistical map"})
        self.assertTrue(result["success"], result)
        self.assertIs(sys.modules[self.MODULES[0]], loaded_items)
        result = items.add_north_arrow_handler({**base, "item_id": "north", "x": 250, "y": 20, "width": 12, "height": 18})
        self.assertTrue(result["success"], result)
        result = items.set_map_extent_handler({**base, "item_id": "map", "layer": layer.id()})
        self.assertTrue(result["success"], result)
        self.assertEqual(result["data"]["crs"], "EPSG:4326")
        result = items.add_legend_handler({**base, "item_id": "legend", "map_id": "map", "auto_update_model": False})
        self.assertTrue(result["success"], result)
        result = advanced.set_legend_style_handler({**base, "item_id": "legend", "legend_layers": [layer.id()]})
        self.assertTrue(result["success"], result)
        self.assertEqual([item["id"] for item in result["data"]["legend_layers"]], [layer.id()])
        loaded_advanced = sys.modules[self.MODULES[1]]
        self.wrapper("layout_advanced").get_item_properties_handler({**base, "item_id": "map"})
        self.assertIs(sys.modules[self.MODULES[1]], loaded_advanced)
        self.assertIs(layout.itemById("retained"), retained)
        self.assertEqual(retained.text(), "Keep this existing element")
        self.assertEqual(project.mapLayers(), {layer.id(): layer})
        self.assertIs(project.layoutManager().layoutByName(layout.name()), layout)

    def test_content_hash_refreshes_same_mtime_size_and_does_not_mask_failure(self):
        name = self.MODULES[0]
        with tempfile.TemporaryDirectory(prefix="layout-reload-source-") as directory:
            path = Path(directory) / "print_layout_items_impl.py"
            path.write_text("def probe(params):\n    return 'version1'\n", encoding="utf-8")
            initial = self.loader.load_layout_handler(name, "probe", path)
            first_module = sys.modules[name]
            self.assertEqual(initial({}), "version1")
            self.assertIs(self.loader.load_layout_handler(name, "probe", path), initial)
            timestamp = path.stat().st_mtime_ns
            size = path.stat().st_size
            path.write_text("def probe(params):\n    return 'version2'\n", encoding="utf-8")
            os.utime(path, ns=(timestamp, timestamp))
            self.assertEqual(path.stat().st_size, size)
            second = self.loader.load_layout_handler(name, "probe", path)
            self.assertEqual(second({}), "version2")
            self.assertIsNot(sys.modules[name], first_module)
            second_module = sys.modules[name]
            path.write_text("raise RuntimeError('real source failure')\n", encoding="utf-8")
            with self.assertRaisesRegex(RuntimeError, "real source failure"):
                self.loader.load_layout_handler(name, "probe", path)
            self.assertIs(sys.modules[name], second_module)
            self.assertEqual(second({}), "version2")
            path.write_text("def probe(params):\n    raise ValueError('real handler failure')\n", encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "real handler failure"):
                self.loader.load_layout_handler(name, "probe", path)({})


if __name__ == "__main__":
    unittest.main()
