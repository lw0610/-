"""Exercise real multi-page export names; never open the user's QGIS project."""
import os
from pathlib import Path
import tempfile
import unittest

from autocarto.tests.golden.qgis_bootstrap import isolated_qgis, qgis_available


@unittest.skipUnless(qgis_available(), "QGIS runtime unavailable")
class LayoutExportArtifactsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix="autocarto-layout-artifacts-")
        cls.root = Path(cls.temp.name)
        cls.qgis = isolated_qgis(cls.root)
        cls.qgis.__enter__()

    @classmethod
    def tearDownClass(cls):
        cls.qgis.__exit__(None, None, None)
        cls.temp.cleanup()

    def setUp(self):
        from qgis.core import (QgsProject, QgsPrintLayout, QgsLayoutItemMap,
                               QgsLayoutItemPage, QgsLayoutSize, QgsRectangle)
        self.project = QgsProject.instance()
        self.layout = QgsPrintLayout(self.project)
        self.layout.initializeDefaults()
        self.layout.setName(self._testMethodName)
        self.layout.pageCollection().page(0).setPageSize(QgsLayoutSize(60, 40))
        page = QgsLayoutItemPage(self.layout)
        page.setPageSize(QgsLayoutSize(60, 40))
        self.layout.pageCollection().addPage(page)
        item = QgsLayoutItemMap(self.layout)
        self.layout.addLayoutItem(item)
        item.attemptResize(QgsLayoutSize(50, 30))
        item.zoomToExtent(QgsRectangle(110, 30, 116, 36))
        self.project.layoutManager().addLayout(self.layout)

    def tearDown(self):
        self.project.clear()

    def check_export(self, handler, suffix, pages):
        folder = self.root / self._testMethodName
        folder.mkdir()
        # A similarly named stale file must not be gathered by a directory scan.
        stale = folder / ("map_99" + suffix)
        stale.write_text("unrelated")
        result = handler({"layout": self.layout.name(), "output": str(folder / ("map" + suffix)), "dpi": 36})
        self.assertTrue(result["success"], result)
        paths = [Path(path) for path in result["artifact_paths"]]
        self.assertEqual(len(paths), pages, result)
        self.assertEqual(result["output"], str(paths[0]))
        self.assertNotIn(stale, paths)
        for path in paths:
            self.assertTrue(path.is_absolute())
            self.assertTrue(path.is_file(), result)
            self.assertGreater(path.stat().st_size, 0)
        # Actual exporter files, not predicted suffixes, must all be registered.
        self.assertEqual(set(paths), {path for path in paths[0].parent.glob("map*" + suffix) if path != stale})

    def test_legacy_image_registers_every_page(self):
        from autocarto.tools.system._shared.layout_export_impl import export_print_layout_handler
        self.check_export(export_print_layout_handler, ".png", 2)

    def test_image_registers_every_page(self):
        from autocarto.tools.system._shared.print_layout_advanced_impl import export_image_handler
        self.check_export(export_image_handler, ".png", 2)

    def test_svg_registers_every_page(self):
        from autocarto.tools.system._shared.print_layout_advanced_impl import export_svg_handler
        self.check_export(export_svg_handler, ".svg", 2)

    def test_pdf_registers_one_multipage_document(self):
        from autocarto.tools.system._shared.layout_export_impl import export_layout_pdf_handler
        self.check_export(export_layout_pdf_handler, ".pdf", 1)

    def test_failed_export_has_no_artifact_paths(self):
        from autocarto.tools.system._shared.layout_export_impl import export_layout_pdf_handler
        result = export_layout_pdf_handler({"layout": "missing"})
        self.assertFalse(result["success"])
        self.assertNotIn("artifact_paths", result)


if __name__ == "__main__":
    unittest.main()
