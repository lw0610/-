import tempfile
import unittest
import uuid
from pathlib import Path
from unittest.mock import patch

try:
    from qgis.PyQt.QtGui import QColor, QImage
    from qgis.core import (
        QgsApplication, QgsCoordinateReferenceSystem, QgsLayoutItemMap,
        QgsLayoutItemPicture, QgsLayoutPoint, QgsLayoutSize, QgsPrintLayout,
        QgsProject, QgsRectangle,
    )
    QGIS_AVAILABLE = True
except ImportError:
    QGIS_AVAILABLE = False


@unittest.skipUnless(QGIS_AVAILABLE, "QGIS Python runtime is unavailable")
class CalloutRenderingQgisTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.owns_application = QgsApplication.instance() is None
        cls.application = QgsApplication.instance() or QgsApplication([], False)
        if cls.owns_application:
            cls.application.initQgis()
        from autocarto.tools.system._shared.callouts_impl import _cartography_add_callouts_impl
        cls.render_callouts = staticmethod(_cartography_add_callouts_impl)

    @classmethod
    def tearDownClass(cls):
        if cls.owns_application:
            cls.application.exitQgis()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.project = QgsProject.instance()
        self.layout = QgsPrintLayout(self.project)
        self.layout.initializeDefaults()
        self.layout.setName("callout_rendering_" + uuid.uuid4().hex)
        self.project.layoutManager().addLayout(self.layout)
        self.map_item = QgsLayoutItemMap(self.layout)
        self.map_item.setId("map")
        self.map_item.setCrs(QgsCoordinateReferenceSystem("EPSG:4326"))
        self.map_item.attemptMove(QgsLayoutPoint(0, 0))
        self.map_item.attemptResize(QgsLayoutSize(260, 180))
        self.map_item.zoomToExtent(QgsRectangle(0, 0, 26, 18))
        self.layout.addLayoutItem(self.map_item)

    def tearDown(self):
        self.project.layoutManager().removeLayout(self.layout)

    def image(self, width, height):
        path = Path(self.temp.name) / f"{width}x{height}.png"
        image = QImage(width, height, QImage.Format_RGB32)
        image.fill(QColor("#2a74ad"))
        self.assertTrue(image.save(str(path)))
        return str(path)

    def render(self, **overrides):
        callout = {
            "id": uuid.uuid4().hex, "anchor": {"x": 10, "y": 9},
            "title": "旧标题不应输出", "body": "正文完整保留，图片保持比例。",
            "box": {"width_mm": 42},
            **overrides,
        }
        with patch("qgis.utils.iface", object()):
            result = self.render_callouts({"layout_name": self.layout.name(), "map_item_id": "map", "callouts": [callout]})
        self.assertTrue(result["success"], result)
        item = result["items"][0]
        self.assertNotIn("title", item["created_item_ids"])
        self.assertEqual(len(item["created_item_ids"]["leader"]), 2)
        return item, {role: self.layout.itemById(item_id) for role, item_id in item["created_item_ids"].items() if isinstance(item_id, str)}

    def test_landscape_and_portrait_images_fill_body_width_without_distortion(self):
        for width, height in ((400, 176), (400, 575)):
            with self.subTest(size=(width, height)):
                result, items = self.render(image={"enabled": True, "path": self.image(width, height)})
                image, body, box = items["image"], items["body"], items["box"]
                self.assertEqual(result["template"], "image_top_text_bottom")
                self.assertEqual(image.resizeMode(), QgsLayoutItemPicture.Zoom)
                self.assertAlmostEqual(image.pos().x(), body.pos().x())
                self.assertAlmostEqual(image.rect().width(), body.rect().width())
                self.assertAlmostEqual(image.rect().width(), 38.4)
                self.assertAlmostEqual(image.rect().height(), 38.4 * height / width)
                self.assertAlmostEqual(body.pos().y() - image.pos().y() - image.rect().height(), 1.5)
                self.assertAlmostEqual(box.pos().y() + box.rect().height() - body.pos().y() - body.rect().height(), 1.8)
                self.assertGreater(body.zValue(), box.zValue())
                self.assertGreaterEqual(body.rect().height(), body.sizeForText().height())

    def test_missing_and_unreadable_images_fall_back_without_reserved_gap(self):
        broken = Path(self.temp.name) / "broken.png"
        broken.write_text("not an image")
        for image in ({"enabled": False}, {"enabled": True, "path": ""}, {"enabled": True, "path": str(broken)}):
            with self.subTest(image=image):
                result, items = self.render(template="image_top_text_bottom", image=image)
                self.assertEqual(result["template"], "text_only")
                self.assertNotIn("image", items)
                self.assertAlmostEqual(items["body"].pos().y() - items["box"].pos().y(), 1.8)
                self.assertAlmostEqual(items["box"].rect().height(), items["body"].rect().height() + 3.6)
                if image["enabled"]:
                    self.assertTrue(any("图片不可读" in warning for warning in result["warnings"]))

    def test_long_body_is_not_truncated_at_eight_lines(self):
        body = "这段导入正文需要完整呈现，不能因为排版而省略。" * 18 + "最后一句必须保留。"
        result, items = self.render(body=body, text_style={"max_body_lines": 8, "ellipsis": True})
        self.assertGreater(len(result["wrapped_body_lines"]), 8)
        self.assertEqual("".join(result["wrapped_body_lines"]), body)
        self.assertTrue(items["body"].text().endswith("最后一句必须保留。"))
        self.assertGreaterEqual(items["body"].rect().height(), items["body"].sizeForText().height())

    def test_left_image_template_places_text_in_its_own_column(self):
        result, items = self.render(template="image_left_text_right", image={"enabled": True, "path": self.image(400, 575)})
        image, body, box = items["image"], items["body"], items["box"]
        self.assertAlmostEqual(body.pos().x() - image.pos().x() - image.rect().width(), 1.5)
        self.assertAlmostEqual(body.pos().y(), image.pos().y())
        self.assertLessEqual(body.pos().x() + body.rect().width(), box.pos().x() + box.rect().width() - 1.8 + 1e-7)


if __name__ == "__main__":
    unittest.main()
