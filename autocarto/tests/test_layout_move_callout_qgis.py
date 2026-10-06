import math
import unittest
import uuid

try:
    from qgis.PyQt.QtCore import QPointF
    from qgis.PyQt.QtGui import QPolygonF
    from qgis.core import (
        QgsApplication,
        QgsLayoutItemLabel,
        QgsLayoutItemPicture,
        QgsLayoutItemPolyline,
        QgsLayoutItemShape,
        QgsLayoutPoint,
        QgsLayoutSize,
        QgsPrintLayout,
        QgsProject,
        QgsUnitTypes,
    )

    QGIS_AVAILABLE = True
except ImportError:
    QGIS_AVAILABLE = False


@unittest.skipUnless(QGIS_AVAILABLE, "QGIS Python runtime is unavailable")
class LayoutMoveCalloutQgisTestCase(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.owns_application = QgsApplication.instance() is None
        if cls.owns_application:
            cls.application = QgsApplication([], False)
            cls.application.initQgis()
        else:
            cls.application = QgsApplication.instance()

        from autocarto.tools.system._shared.print_layout_callout_impl import (
            _item_rect,
            _scene_nodes,
            move_callout_handler,
        )

        cls.item_rect = staticmethod(_item_rect)
        cls.scene_nodes = staticmethod(_scene_nodes)
        cls.move_callout = staticmethod(move_callout_handler)

    @classmethod
    def tearDownClass(cls):
        if cls.owns_application:
            cls.application.exitQgis()

    def setUp(self):
        self.project = QgsProject.instance()
        self.layout = QgsPrintLayout(self.project)
        self.layout.initializeDefaults()
        self.layout_name = f"move_callout_{uuid.uuid4().hex}"
        self.layout.setName(self.layout_name)
        self.assertTrue(self.project.layoutManager().addLayout(self.layout))
        self.container = self._shape("main_map", 10, 10, 260, 180)

    def tearDown(self):
        layout = self.project.layoutManager().layoutByName(self.layout_name)
        if layout is not None:
            self.project.layoutManager().removeLayout(layout)

    def _shape(self, item_id, x, y, width, height):
        item = QgsLayoutItemShape(self.layout)
        item.setId(item_id)
        item.attemptMove(QgsLayoutPoint(x, y, QgsUnitTypes.LayoutMillimeters))
        item.attemptResize(
            QgsLayoutSize(width, height, QgsUnitTypes.LayoutMillimeters)
        )
        self.layout.addLayoutItem(item)
        return item

    def _label(self, item_id, x, y, width, height):
        item = QgsLayoutItemLabel(self.layout)
        item.setId(item_id)
        item.setText(item_id)
        item.attemptMove(QgsLayoutPoint(x, y, QgsUnitTypes.LayoutMillimeters))
        item.attemptResize(
            QgsLayoutSize(width, height, QgsUnitTypes.LayoutMillimeters)
        )
        self.layout.addLayoutItem(item)
        return item

    def _picture(self, item_id, x, y, width, height):
        item = QgsLayoutItemPicture(self.layout)
        item.setId(item_id)
        item.attemptMove(QgsLayoutPoint(x, y, QgsUnitTypes.LayoutMillimeters))
        item.attemptResize(
            QgsLayoutSize(width, height, QgsUnitTypes.LayoutMillimeters)
        )
        self.layout.addLayoutItem(item)
        return item

    def _line(self, item_id, nodes):
        item = QgsLayoutItemPolyline(self.layout)
        item.setId(item_id)
        item.setNodes(QPolygonF([QPointF(x, y) for x, y in nodes]))
        self.layout.addLayoutItem(item)
        return item

    def _base_callout(self):
        box = self._shape("callout_box", 30, 40, 40, 30)
        image = self._picture("callout_image", 32, 42, 36, 10)
        title = self._label("callout_title", 32, 53, 36, 5)
        body = self._label("callout_body", 32, 59, 36, 9)
        return box, image, title, body

    @staticmethod
    def _distance(first, second):
        return math.hypot(second[0] - first[0], second[1] - first[1])

    def test_moves_all_content_and_rebuilds_double_leaders_with_fixed_anchor(self):
        box, image, title, body = self._base_callout()
        first = self._line("leader_1", [(70, 50), (200, 100)])
        second = self._line("leader_2", [(70, 60), (200, 100)])
        before = {
            item.id(): self.item_rect(item)
            for item in (box, image, title, body)
        }
        old_nodes = {
            first.id(): self.scene_nodes(first),
            second.id(): self.scene_nodes(second),
        }

        result = self.move_callout(
            {
                "layout": self.layout_name,
                "callout_item_ids": {
                    "box": box.id(),
                    "image": image.id(),
                    "title": title.id(),
                    "body": body.id(),
                    "leader": [first.id(), second.id()],
                },
                "x": 80,
                "y": 60,
                "container_item_id": self.container.id(),
            }
        )

        self.assertTrue(result["success"], result)
        self.assertTrue(result["data"]["anchor_preserved"])
        self.assertEqual(result["data"]["delta_mm"], {"x": 50.0, "y": 20.0})
        for item in (box, image, title, body):
            old_rect = before[item.id()]
            new_rect = self.item_rect(item)
            self.assertAlmostEqual(new_rect[0] - old_rect[0], 50.0)
            self.assertAlmostEqual(new_rect[1] - old_rect[1], 20.0)
            self.assertAlmostEqual(new_rect[2], old_rect[2])
            self.assertAlmostEqual(new_rect[3], old_rect[3])

        for line in (first, second):
            new_nodes = self.scene_nodes(line)
            self.assertEqual(len(new_nodes), 2)
            self.assertAlmostEqual(new_nodes[-1][0], old_nodes[line.id()][-1][0])
            self.assertAlmostEqual(new_nodes[-1][1], old_nodes[line.id()][-1][1])
            self.assertGreaterEqual(new_nodes[0][0], 80.0)
            self.assertLessEqual(new_nodes[0][0], 120.0)

        self.assertLess(
            result["data"]["leaders"][0]["new_length_mm"],
            result["data"]["leaders"][0]["old_length_mm"],
        )

    def test_rebuilds_straight_and_curved_leaders(self):
        for style, old_nodes, expected_count in (
            ("straight", [(70, 55), (200, 100)], 2),
            ("curved", [(70, 55), (110, 25), (155, 40), (200, 100)], 15),
        ):
            with self.subTest(style=style):
                box = self._shape(f"{style}_box", 30, 40, 40, 30)
                label = self._label(f"{style}_body", 32, 43, 36, 20)
                line = self._line(f"{style}_leader", old_nodes)
                old_anchor = self.scene_nodes(line)[-1]
                result = self.move_callout(
                    {
                        "layout": self.layout_name,
                        "callout_item_ids": {
                            "box": box.id(),
                            "body": label.id(),
                            "leader": line.id(),
                        },
                        "x": 85,
                        "y": 65,
                        "container_item_id": self.container.id(),
                    }
                )
                self.assertTrue(result["success"], result)
                self.assertEqual(result["data"]["leaders"][0]["style"], style)
                new_nodes = self.scene_nodes(line)
                self.assertEqual(len(new_nodes), expected_count)
                self.assertLess(self._distance(new_nodes[-1], old_anchor), 0.001)

    def test_moving_box_across_anchor_switches_the_attachment_edge(self):
        box = self._shape("box", 30, 40, 40, 30)
        body = self._label("body", 32, 43, 36, 20)
        line = self._line("leader", [(70, 55), (200, 100)])

        result = self.move_callout(
            {
                "layout": self.layout_name,
                "callout_item_ids": {
                    "box": box.id(),
                    "body": body.id(),
                    "leader": line.id(),
                },
                "x": 220,
                "y": 85,
                "container_item_id": self.container.id(),
            }
        )

        self.assertTrue(result["success"], result)
        nodes = self.scene_nodes(line)
        self.assertAlmostEqual(nodes[0][0], 220.0)
        self.assertAlmostEqual(nodes[-1][0], 200.0)
        self.assertAlmostEqual(nodes[-1][1], 100.0)

    def test_rejects_outside_target_without_changing_any_item(self):
        box, image, title, body = self._base_callout()
        line = self._line("leader", [(70, 55), (200, 100)])
        before_rects = {
            item.id(): self.item_rect(item)
            for item in (box, image, title, body)
        }
        before_nodes = self.scene_nodes(line)

        result = self.move_callout(
            {
                "layout": self.layout_name,
                "callout_item_ids": {
                    "box": box.id(),
                    "image": image.id(),
                    "title": title.id(),
                    "body": body.id(),
                    "leader": line.id(),
                },
                "x": 250,
                "y": 170,
                "container_item_id": self.container.id(),
            }
        )

        self.assertFalse(result["success"])
        self.assertEqual(result["error_code"], "callout_outside_container")
        for item in (box, image, title, body):
            self.assertEqual(self.item_rect(item), before_rects[item.id()])
        self.assertEqual(self.scene_nodes(line), before_nodes)

    def test_rejects_missing_or_mismatched_ids_before_mutation(self):
        box = self._shape("box", 30, 40, 40, 30)
        before = self.item_rect(box)
        result = self.move_callout(
            {
                "layout": self.layout_name,
                "callout_item_ids": {"box": box.id(), "title": "missing"},
                "x": 80,
                "y": 60,
                "container_item_id": self.container.id(),
            }
        )
        self.assertFalse(result["success"])
        self.assertEqual(result["error_code"], "callout_item_not_found")
        self.assertEqual(self.item_rect(box), before)


if __name__ == "__main__":
    unittest.main()
