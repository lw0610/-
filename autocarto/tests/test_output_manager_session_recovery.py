import importlib
import os
import sys
import tempfile
import types
import unittest
from unittest.mock import patch


class _FakeQgsProject:
    @staticmethod
    def instance():
        return _FakeQgsProject()

    def fileName(self):
        return "/tmp/demo/demo.qgz"


def _install_fake_qgis_modules():
    fake_qgis = types.ModuleType("qgis")
    fake_core = types.ModuleType("qgis.core")
    setattr(fake_core, "QgsProject", _FakeQgsProject)
    setattr(fake_qgis, "core", fake_core)
    sys.modules.setdefault("qgis", fake_qgis)
    sys.modules["qgis.core"] = fake_core


class OutputManagerSessionRecoveryTestCase(unittest.TestCase):
    output_manager: types.ModuleType = types.ModuleType("output_manager")

    def setUp(self):
        module_patch=patch.dict(sys.modules)
        module_patch.start()
        self.addCleanup(module_patch.stop)
        _install_fake_qgis_modules()
        name="autocarto.tools.system._shared.output_manager"
        sys.modules.pop(name,None)
        self.output_manager=importlib.import_module(name)
        self.output_manager.set_session_context(None)

    def test_init_output_dir_handler_can_restore_session_from_params(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            session_dir = os.path.join(tmp_dir, "autoc", "sessions", "session-001")
            result = self.output_manager.init_output_dir_handler(
                {
                    "session_id": "session-001",
                    "session_dir": session_dir,
                    "create_subdirs": True,
                }
            )

            self.assertTrue(result["success"])
            self.assertEqual(result["output"], session_dir)
            self.assertTrue(os.path.isdir(os.path.join(session_dir, "output")))
            self.assertTrue(os.path.isdir(os.path.join(session_dir, "logs")))

    def test_get_output_info_handler_uses_restored_session(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            session_dir = os.path.join(tmp_dir, "autoc", "sessions", "session-002")
            os.makedirs(session_dir, exist_ok=True)
            marker = os.path.join(session_dir, "marker.txt")
            with open(marker, "w", encoding="utf-8") as handle:
                handle.write("ok")

            result = self.output_manager.get_output_info_handler(
                {
                    "session_id": "session-002",
                    "session_dir": session_dir,
                }
            )

            self.assertTrue(result["success"])
            self.assertEqual(result["output"], session_dir)
            self.assertEqual(result["info"]["files_count"], 1)

    def test_explicit_tool_params_replace_stale_session_context(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            stale_dir = os.path.join(tmp_dir, "autoc", "sessions", "stale-session")
            fresh_dir = os.path.join(
                tmp_dir, "autoc", "demo", "sessions", "fresh-session"
            )

            self.output_manager.set_session_context(
                types.SimpleNamespace(
                    session_id="stale-session",
                    project_path="/tmp/old.qgz",
                    session_dir=stale_dir,
                    session_subdirs={"exports": os.path.join(stale_dir, "exports")},
                )
            )

            resolved = self.output_manager.resolve_output_path(
                "/tmp/chart.svg",
                "chart.svg",
                "exports",
            )
            self.assertTrue(resolved.startswith(stale_dir))

            result = self.output_manager.init_output_dir_handler(
                {
                    "session_id": "fresh-session",
                    "project_path": "/tmp/demo.qgz",
                    "session_dir": fresh_dir,
                    "session_subdirs": {
                        "exports": os.path.join(fresh_dir, "exports"),
                        "logs": os.path.join(fresh_dir, "logs"),
                    },
                }
            )

            self.assertTrue(result["success"])
            self.assertEqual(result["output"], fresh_dir)

            fresh_resolved = self.output_manager.resolve_output_path(
                "/tmp/chart.svg",
                "chart.svg",
                "exports",
            )
            self.assertTrue(fresh_resolved.startswith(fresh_dir))
            self.assertFalse(fresh_resolved.startswith(stale_dir))


if __name__ == "__main__":
    unittest.main()
