"""Offline smoke test of the statistical workflow through real tool bindings.

Run with QGIS Python in its own process. All inputs are synthetic; the user's
open project, statistics and credentials are never used.
"""
import hashlib
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace

try:
    import yaml
    from qgis.PyQt.QtGui import QColor, QImage
    from qgis.core import (
        QgsApplication, QgsCoordinateReferenceSystem, QgsProject, QgsVectorLayer,
    )
    QGIS_AVAILABLE = True
except ImportError:
    QGIS_AVAILABLE = False


@unittest.skipUnless(QGIS_AVAILABLE, "run with the QGIS Python runtime")
class StatisticalWorkflowQgisTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if QgsApplication.instance() is not None:
            raise unittest.SkipTest("use an isolated QGIS process")
        cls.profile = tempfile.TemporaryDirectory(prefix="statistical-profile-")
        cls.app = QgsApplication([], False, cls.profile.name)
        cls.app.initQgis()

    @classmethod
    def tearDownClass(cls):
        QgsProject.instance().clear()
        cls.app.exitQgis()
        cls.profile.cleanup()

    def tearDown(self):
        from autocarto.tools.system._shared import output_manager
        output_manager.clear_session_context()
        QgsProject.instance().clear()

    def make_engine(self):
        from autocarto.core.tool_engine.engine import QGISToolEngine
        from autocarto.core.paths import get_plugin_root
        tools = {
            "data_prepare_statistical", "data_load_vector", "cartography_set_style",
            "cartography_graduated_style", "layer_get_style_info", "layout_create",
            "layout_add_map", "layout_set_map_layers", "layout_set_map_extent",
            "layout_add_legend", "layout_set_legend_style", "layout_add_label",
            "layout_set_label_style", "layout_move_item", "layout_resize_item",
            "layout_get_items_properties", "export_print_layout", "export_layout_pdf",
        }
        root = get_plugin_root()
        entries = []
        for path in (root / "tools" / "system").glob("*/*.yaml"):
            spec = yaml.safe_load(path.read_text(encoding="utf-8"))
            meta = spec.get("metadata", {})
            if meta.get("name") not in tools:
                continue
            module, function = spec["execution"]["handler"].rsplit(".", 1)
            handler_file = path.parent / (module.replace(".", "/") + ".py")
            self.assertTrue(handler_file.is_file(), handler_file)
            entries.append({
                **meta, "parameters": spec["spec"].get("parameters", []),
                "handler_file": str(handler_file), "handler_function": function,
            })
        self.assertEqual({item["name"] for item in entries}, tools)
        self.assertEqual(len(entries), len(tools), "duplicate tool names must fail before binding")
        engine = QGISToolEngine()
        engine.update_contract(execution_manifest=entries)
        return engine

    def test_csv_to_classification_legend_and_export_preserves_sources(self):
        engine = self.make_engine()

        def invoke(tool_name, **params):
            result = engine.invoke(tool_name, params)
            self.assertTrue(result.get("success"), (tool_name, result))
            return result

        with tempfile.TemporaryDirectory(prefix="statistical-flow-") as directory:
            directory = Path(directory)
            from autocarto.tools.system._shared import output_manager
            output_manager.set_session_context(SimpleNamespace(
                session_id="statistical-test", session_dir=str(directory),
                session_subdirs={}, project_path="",
            ))
            source = directory / "regions.geojson"
            table = directory / "statistics.csv"
            features = []
            for index in range(6):
                x, y = 110 + index % 3, 30 + index // 3
                features.append({
                    "type": "Feature", "properties": {"name": f"Area {index + 1}"},
                    "geometry": {"type": "Polygon", "coordinates": [[
                        [x, y], [x + 1, y], [x + 1, y + 1], [x, y + 1], [x, y],
                    ]]},
                })
            source.write_text(json.dumps({"type": "FeatureCollection", "features": features}), encoding="utf-8")
            table.write_text(
                "年份,指标,单位,数据来源,Area 1,Area 2,Area 3,Area 4,Area 5,Area 6\n"
                "2024,Test indicator,units,Synthetic test,0,10,25,50,100,\n"
                "2023,Test indicator,units,Synthetic test,1,2,3,4,5,6\n",
                encoding="utf-8-sig",
            )
            hashes = {path: hashlib.sha256(path.read_bytes()).hexdigest() for path in (source, table)}
            prepared = invoke("data_prepare_statistical", geojson_path=str(source), csv_path=str(table),
                              year=2024, output_dir=str(directory / "derived"))["data"]
            derived = prepared["paths"]["polygons_geojson"]
            self.assertEqual(prepared["metadata"]["unit"], "units")
            polygons = json.loads(Path(derived).read_text(encoding="utf-8"))
            self.assertEqual([f["properties"]["stat_value"] for f in polygons["features"]], [0, 10, 25, 50, 100, None])
            invoke("data_load_vector", path=derived, layer_name="thematic values")
            invoke("data_load_vector", path=derived, layer_name="missing value background")
            invoke("cartography_set_style", layer="missing value background", style_type="simple", color="#e5e7eb")
            project = QgsProject.instance()
            project.setCrs(QgsCoordinateReferenceSystem("EPSG:3857"))
            layer = project.mapLayersByName("thematic values")[0]
            background = project.mapLayersByName("missing value background")[0]
            # An unrelated world-sized layer must not affect the final map or legend.
            unrelated = QgsVectorLayer("Polygon?crs=EPSG:4326", "unrelated", "memory")
            project.addMapLayer(unrelated)
            styled = invoke("cartography_graduated_style", layer=layer.name(), field="stat_value",
                            method="quantile", classes=3, color_ramp="Blues")["data"]
            self.assertEqual(styled["valid_value_count"], 5)
            self.assertEqual(styled["null_value_count"], 1)
            self.assertEqual(styled["ranges"][0]["lower"], 0)
            invoke("layout_create", name="Statistical smoke", page_width=297, page_height=210)
            common = {"layout": "Statistical smoke"}
            invoke("layout_add_map", **common, item_id="main")
            invoke("layout_move_item", **common, item_id="main", x=12, y=35)
            invoke("layout_resize_item", **common, item_id="main", width=205, height=150)
            invoke("layout_set_map_layers", **common, item_id="main", layers=[layer.id(), background.id()])
            invoke("layout_set_map_extent", **common, item_id="main", layer=layer.id(), padding=0.04)
            invoke("layout_add_legend", **common, item_id="legend", map_id="main",
                   title="Test indicator (units)", x=224, y=55, width=60, height=70,
                   auto_update_model=False, resize_to_contents=True)
            invoke("layout_set_legend_style", **common, item_id="legend", legend_layers=[layer.id()],
                   title_font_size=4.5, body_font_size=3.5)
            for item_id, text, y, size in [
                ("title", "2024 Test indicator — synthetic data", 8, 6),
                ("source", "Source: synthetic test only. Gray: missing data; zero is valid.", 192, 3.5),
            ]:
                invoke("layout_add_label", **common, item_id=item_id, text=text)
                invoke("layout_set_label_style", **common, item_id=item_id, font_size=size, width=275)
                invoke("layout_move_item", **common, item_id=item_id, x=12, y=y)
            properties = invoke("layout_get_items_properties", **common, item_ids=["main", "legend", "title", "source"])
            self.assertNotIn("军标箭头", json.dumps(properties, ensure_ascii=False))
            layout = project.layoutManager().layoutByName(common["layout"])
            map_item = layout.itemById("main")
            self.assertEqual([item.id() for item in map_item.layers()], [layer.id(), background.id()])
            self.assertTrue(map_item.keepLayerSet())
            self.assertEqual(map_item.crs().authid(), "EPSG:3857")
            self.assertGreater(map_item.extent().xMinimum(), 10_000_000)
            legend = layout.itemById("legend")
            nodes = legend.model().rootGroup().findLayers()
            self.assertEqual([node.layerId() for node in nodes], [layer.id()])
            legend_labels = [node.data(0) for node in legend.model().layerLegendNodes(nodes[0])]
            self.assertEqual(legend_labels, [item.label() for item in layer.renderer().ranges()])
            output = directory / "statistical.png"
            exported = invoke("export_print_layout", **common, output=str(output), dpi=100)
            output = Path(exported["output"])
            self.assertTrue(output.is_relative_to(directory))
            image = QImage(str(output))
            self.assertFalse(image.isNull())
            self.assertGreater(image.width(), 1000)
            self.assertGreater(output.stat().st_size, 10000)
            pixels = {image.pixelColor(x, y).name()
                      for x in range(0, image.width(), 4)
                      for y in range(0, image.height(), 4)}
            gray = background.renderer().symbol().color()
            opacity = background.renderer().symbol().opacity()
            visible_gray = QColor(*[round(channel * opacity + 255 * (1 - opacity))
                                   for channel in (gray.red(), gray.green(), gray.blue())]).name()
            self.assertIn(visible_gray, pixels, "the missing region must remain visible in gray")
            for item in styled["ranges"]:
                self.assertIn(QColor(item["color"]).name(), pixels, "each actual class must render")
            pdf = Path(invoke("export_layout_pdf", **common, output="statistical.pdf", dpi=100)["output"])
            self.assertTrue(pdf.is_relative_to(directory))
            self.assertTrue(pdf.read_bytes().startswith(b"%PDF-"))
            # Source tables/geometries stay byte-for-byte intact after all operations.
            self.assertEqual(hashes, {path: hashlib.sha256(path.read_bytes()).hexdigest() for path in hashes})


if __name__ == "__main__":
    unittest.main()
