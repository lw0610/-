"""Real QGIS regression tests; run in a separate headless Python process.

Only synthetic memory layers and a temporary QGIS profile are used. This test
does not connect to the user's QGIS instance or load/save their project.
"""
import math
import tempfile
import unittest
import uuid

try:
    from qgis.PyQt.QtCore import QSize
    from qgis.PyQt.QtGui import QColor
    from qgis.core import (
        QgsApplication, QgsFeature, QgsGeometry, QgsGradientColorRamp,
        QgsMapRendererParallelJob, QgsMapSettings, QgsProject, QgsRectangle,
        QgsRenderContext, QgsStyle, QgsVectorLayer,
    )
    QGIS_AVAILABLE = True
except ImportError:
    QGIS_AVAILABLE = False


@unittest.skipUnless(QGIS_AVAILABLE, "QGIS Python runtime is unavailable")
class ThematicStyleQgisTestCase(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # Refuse to run inside an existing GUI process: the handler resolves
        # QgsProject.instance(), so these tests require their own application.
        if QgsApplication.instance() is not None:
            raise unittest.SkipTest("run thematic tests in an isolated QGIS process")
        cls.profile = tempfile.TemporaryDirectory(prefix="autocarto-thematic-test-")
        cls.application = QgsApplication([], False, cls.profile.name)
        cls.application.initQgis()
        from autocarto.tools.system._shared import style_thematic_impl, style_basic_impl
        cls.handlers = style_thematic_impl
        cls.basic = style_basic_impl

    @classmethod
    def tearDownClass(cls):
        QgsProject.instance().removeAllMapLayers()
        cls.application.exitQgis()
        cls.profile.cleanup()

    def tearDown(self):
        QgsProject.instance().removeAllMapLayers()

    def make_layer(self, values, field_type="double"):
        layer = QgsVectorLayer(
            f"Polygon?crs=EPSG:4326&field=value:{field_type}&field=name:string",
            "thematic_" + uuid.uuid4().hex, "memory",
        )
        for index, value in enumerate(values):
            x, y = index % 6, index // 6
            feature = QgsFeature(layer.fields())
            feature.setAttributes([value, f"Area {index + 1}"])
            feature.setGeometry(QgsGeometry.fromWkt(
                f"POLYGON(({x} {y},{x+1} {y},{x+1} {y+1},{x} {y+1},{x} {y}))"
            ))
            self.assertTrue(layer.dataProvider().addFeatures([feature])[0])
        layer.updateExtents()
        QgsProject.instance().addMapLayer(layer)
        return layer

    def assert_rendered_colors(self, layer, expected_count):
        settings = QgsMapSettings()
        settings.setLayers([layer])
        settings.setDestinationCrs(layer.crs())
        settings.setExtent(QgsRectangle(0, 0, 6, 3))
        settings.setOutputSize(QSize(600, 300))
        settings.setBackgroundColor(QColor("white"))
        job = QgsMapRendererParallelJob(settings)
        job.start()
        job.waitForFinished()
        image = job.renderedImage()
        self.assertFalse(image.isNull())
        context = QgsRenderContext.fromMapSettings(settings)
        renderer = layer.renderer()
        renderer.startRender(context, layer.fields())
        colors = set()
        try:
            for index, feature in enumerate(layer.getFeatures()):
                context.expressionContext().setFeature(feature)
                symbol = renderer.symbolForFeature(feature, context)
                self.assertIsNotNone(symbol)
                pixel = image.pixelColor((index % 6) * 100 + 50, 250 - (index // 6) * 100)
                self.assertEqual(pixel.name(), symbol.color().name())
                colors.add(pixel.name())
        finally:
            renderer.stopRender(context)
        self.assertEqual(len(colors), expected_count)

    def test_methods_apply_real_ranges_colors_and_render_pixels(self):
        values = [(index + 1) ** 2 for index in range(18)]
        ranges_by_method = {}
        for method, actual_id in [("quantile", "Quantile"), ("jenks", "Jenks"), ("equal_interval", "EqualInterval")]:
            with self.subTest(method=method):
                layer = self.make_layer(values)
                result = self.handlers.graduated_style_handler({
                    "layer": layer.name(), "field": "value", "classes": 5,
                    "method": method, "color_ramp": "YlOrRd",
                })
                self.assertTrue(result["success"], result)
                data = result["data"]
                self.assertEqual(data["classification_method"], actual_id)
                self.assertEqual(data["renderer_type"], "graduatedSymbol")
                self.assertEqual(data["renderer_field"], "value")
                self.assertEqual(data["class_count"], 5)
                self.assertEqual(data["distinct_color_count"], 5)
                self.assertEqual(data["ranges"][0]["lower"], min(values))
                self.assertEqual(data["ranges"][-1]["upper"], max(values))
                self.assertEqual(data["ranges"][0]["color"], "#ffffffcc")
                self.assertEqual(data["ranges"][-1]["color"], "#ff800026")
                verified = self.basic.get_style_info_handler({"layer": layer.name()})
                self.assertTrue(verified["success"], verified)
                self.assertEqual(verified["data"]["ranges"], data["ranges"])
                self.assertEqual(verified["data"]["renderer_field"], "value")
                self.assert_rendered_colors(layer, 5)
                ranges_by_method[method] = data["ranges"]
        self.assertNotEqual(ranges_by_method["quantile"], ranges_by_method["jenks"])
        self.assertNotEqual(ranges_by_method["quantile"], ranges_by_method["equal_interval"])

    def test_defaults_nulls_and_non_finite_values(self):
        layer = self.make_layer([1, 2, 3, 4, 5, 6, None, float("inf"), float("-inf"), float("nan")])
        result = self.handlers.graduated_style_handler({"layer": layer.name(), "field": "value"})
        self.assertTrue(result["success"], result)
        data = result["data"]
        self.assertEqual(data["classification_method"], "Quantile")
        self.assertEqual(data["color_ramp"], "YlOrRd")
        self.assertEqual(data["requested_classes"], 5)
        self.assertEqual(data["valid_value_count"], 6)
        # QGIS providers may expose NaN as NULL; both are excluded, never zero.
        self.assertEqual(data["null_value_count"] + data["non_finite_value_count"], 4)
        self.assertGreaterEqual(data["null_value_count"], 1)
        self.assertGreaterEqual(data["non_finite_value_count"], 2)
        self.assertEqual(data["ranges"][0]["lower"], 1)
        self.assertEqual(data["ranges"][-1]["upper"], 6)
        self.assertTrue(data["warnings"])
        self.assertTrue(all(math.isfinite(item[side]) for item in data["ranges"] for side in ["lower", "upper"]))

    def test_invalid_inputs_and_monochrome_ramp_preserve_original_style(self):
        layer = self.make_layer(range(18))
        original_color = layer.renderer().symbol().color().name()
        ramp_name = "monochrome_" + uuid.uuid4().hex
        QgsStyle.defaultStyle().addColorRamp(ramp_name, QgsGradientColorRamp(QColor("red"), QColor("red")))
        for overrides in [
            {"field": "missing"}, {"field": "name"}, {"classes": 1},
            {"classes": 2.5}, {"classes": True}, {"classes": 21},
            {"method": "imaginary"}, {"color_ramp": "does-not-exist"},
            {"color_ramp": ramp_name},
        ]:
            with self.subTest(overrides=overrides):
                result = self.handlers.graduated_style_handler({"layer": layer.name(), "field": "value", **overrides})
                self.assertFalse(result["success"], result)
                self.assertEqual(layer.renderer().type(), "singleSymbol")
                self.assertEqual(layer.renderer().symbol().color().name(), original_color)
        for values in [[], [None, None], [5, 5, 5], [float("inf"), float("nan")]]:
            other = self.make_layer(values)
            result = self.handlers.graduated_style_handler({"layer": other.name(), "field": "value"})
            self.assertFalse(result["success"], result)
            self.assertEqual(other.renderer().type(), "singleSymbol")

    def test_requested_class_count_is_reduced_explicitly_for_two_values(self):
        layer = self.make_layer([1, 1, 9, 9])
        result = self.handlers.graduated_style_handler({"layer": layer.name(), "field": "value"})
        self.assertTrue(result["success"], result)
        self.assertEqual(result["data"]["class_count"], 2)
        self.assertEqual(result["data"]["distinct_color_count"], 2)
        self.assertTrue(result["data"]["warnings"])

    def test_stddev_reports_actual_class_count_and_honors_selected_ramp(self):
        layer = self.make_layer(range(18))
        result = self.handlers.graduated_style_handler({
            "layer": layer.name(), "field": "value", "classes": 5,
            "method": "stddev", "color_ramp": "Blues",
        })
        self.assertTrue(result["success"], result)
        data = result["data"]
        self.assertEqual(data["classification_method"], "StdDev")
        self.assertEqual(data["ranges"][0]["color"], "#fff7fbff")
        self.assertEqual(data["ranges"][-1]["color"], "#ff08306b")
        self.assertNotEqual(data["class_count"], data["requested_classes"])
        self.assertTrue(data["warnings"])
        self.assert_rendered_colors(layer, data["class_count"])


if __name__ == "__main__":
    unittest.main()
