"""Project-relative path policy shared with the desktop and service."""
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from autocarto.core import paths


class ProjectPathsTest(unittest.TestCase):
    def test_defaults_and_overrides_do_not_depend_on_working_directory(self):
        with tempfile.TemporaryDirectory() as project, tempfile.TemporaryDirectory() as elsewhere:
            root = Path(project).resolve()
            with patch.dict(os.environ, {"AUTOCARTO_PROJECT_ROOT": str(root), "AUTOCARTO_HOME": ""}):
                self.assertEqual(paths.get_autocarto_home(), root / "autocarto_data")
                with patch.dict(os.environ, {"AUTOCARTO_HOME": "local/runtime"}):
                    previous = Path.cwd()
                    try:
                        os.chdir(elsewhere)
                        self.assertEqual(paths.get_autocarto_home(), root / "local/runtime")
                    finally:
                        os.chdir(previous)
                self.assertFalse((root / "autocarto_data").exists())
                self.assertEqual(paths.get_user_config_path(), root / "autocarto_data/config/config.yaml")

    def test_portable_paths_roundtrip_and_external_locations_are_preserved(self):
        with tempfile.TemporaryDirectory() as project, tempfile.TemporaryDirectory() as external:
            root = Path(project).resolve()
            with patch.dict(os.environ, {"AUTOCARTO_PROJECT_ROOT": str(root)}):
                internal = root / "resources" / "数据.csv"
                portable = paths.prefer_project_relative_path(str(internal))
                self.assertEqual(portable, "resources/数据.csv")
                self.assertEqual(paths.resolve_project_path(portable), internal)
                outside = Path(external).resolve() / "data.csv"
                self.assertEqual(paths.prefer_project_relative_path(str(outside)), str(outside))

    def test_invalid_paths_and_ambiguous_project_roots_are_rejected(self):
        for raw in ("", " ", "bad\x00name"):
            with self.assertRaises(ValueError):
                paths.resolve_project_path(raw)
        with patch.dict(os.environ, {"AUTOCARTO_PROJECT_ROOT": "relative"}):
            with self.assertRaises(ValueError):
                paths.get_project_root()

    def test_absolute_home_override_and_tilde(self):
        with tempfile.TemporaryDirectory() as project:
            root = Path(project).resolve()
            with patch.dict(os.environ, {"AUTOCARTO_PROJECT_ROOT": str(root), "AUTOCARTO_HOME": "~/autocarto-test"}):
                self.assertEqual(paths.get_autocarto_home(), Path.home() / "autocarto-test")
            with patch.dict(os.environ, {"AUTOCARTO_PROJECT_ROOT": str(root), "AUTOCARTO_HOME": str(root / "custom")}):
                self.assertEqual(paths.get_autocarto_home(), root / "custom")


if __name__ == "__main__":
    unittest.main()
