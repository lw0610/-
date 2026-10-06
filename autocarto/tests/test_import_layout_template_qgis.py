import tempfile
import unittest
import uuid
from pathlib import Path

try:
    from qgis.core import (
        QgsApplication,
        QgsLayoutItemLabel,
        QgsPrintLayout,
        QgsProject,
        QgsReadWriteContext,
    )

    QGIS_AVAILABLE = True
except ImportError:
    QGIS_AVAILABLE = False


@unittest.skipUnless(QGIS_AVAILABLE, "QGIS Python runtime is unavailable")
class ImportLayoutTemplateQgisIntegrationTestCase(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.owns_application = QgsApplication.instance() is None
        if cls.owns_application:
            cls.application = QgsApplication([], False)
            cls.application.initQgis()
        else:
            cls.application = QgsApplication.instance()

    @classmethod
    def tearDownClass(cls):
        if cls.owns_application:
            cls.application.exitQgis()

    def setUp(self):
        self.project = QgsProject.instance()
        self.layout_name = f"template_test_{uuid.uuid4().hex}"
        self.temp_dir = tempfile.TemporaryDirectory()
        self.template_path = Path(self.temp_dir.name) / "layout.qpt"

    def tearDown(self):
        layout = self.project.layoutManager().layoutByName(self.layout_name)
        if layout is not None:
            self.project.layoutManager().removeLayout(layout)
        self.temp_dir.cleanup()

    def _create_template(self):
        source_layout = QgsPrintLayout(self.project)
        source_layout.initializeDefaults()
        source_layout.setName("source_template")
        title = QgsLayoutItemLabel(source_layout)
        title.setId("title")
        title.setText("模板标题")
        source_layout.addLayoutItem(title)
        source_layout.saveAsTemplate(
            str(self.template_path), QgsReadWriteContext()
        )
        self.assertTrue(self.template_path.is_file())

    def test_real_qpt_import_and_existing_layout_overwrite(self):
        self._create_template()
        from autocarto.tools.system._shared.print_layout_impl import (
            import_layout_template_handler,
        )

        first = import_layout_template_handler(
            {"template": str(self.template_path), "name": self.layout_name}
        )

        self.assertTrue(first["success"], first)
        self.assertFalse(first["data"]["replaced"])
        imported = self.project.layoutManager().layoutByName(self.layout_name)
        self.assertIsNotNone(imported)
        self.assertIsNotNone(imported.itemById("title"))

        old_item = QgsLayoutItemLabel(imported)
        old_item.setId("old_item")
        old_item.setText("应被模板覆盖")
        imported.addLayoutItem(old_item)

        second = import_layout_template_handler(
            {"template": str(self.template_path), "name": self.layout_name}
        )

        self.assertTrue(second["success"], second)
        self.assertTrue(second["data"]["replaced"])
        self.assertIsNone(imported.itemById("old_item"))
        self.assertIsNotNone(imported.itemById("title"))


if __name__ == "__main__":
    unittest.main()
