import unittest

from autocarto.core.tool_engine.project_metadata import (
    resolve_project_identity as resolve_qgis_project_identity,
)


class ProjectMetadataHelperTestCase(unittest.TestCase):
    def test_explicit_title_has_highest_priority(self):
        expected_name = "演示工程"
        result = resolve_qgis_project_identity("演示工程", "/tmp/demo_project.qgz")
        self.assertEqual(result["title"], expected_name)
        self.assertEqual(result["project_name"], expected_name)
        self.assertEqual(result["display_name"], expected_name)
        self.assertEqual(result["title_source"], "project_title")

    def test_saved_project_without_title_falls_back_to_file_name(self):
        result = resolve_qgis_project_identity("", "/tmp/city_analysis.qgz")
        self.assertEqual(result["title"], "")
        self.assertEqual(result["project_name"], "city_analysis")
        self.assertEqual(result["display_name"], "city_analysis")
        self.assertEqual(result["title_source"], "file_name")

    def test_unsaved_project_uses_default_name(self):
        result = resolve_qgis_project_identity(None, None)
        self.assertEqual(result["title"], "")
        self.assertEqual(result["project_name"], "未命名项目")
        self.assertEqual(result["display_name"], "未命名项目")
        self.assertEqual(result["title_source"], "unsaved_default")


if __name__ == "__main__":
    unittest.main()
