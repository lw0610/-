import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import mock

from autocarto.core import session_manager
from autocarto.core.session_manager import SessionManager


class SessionManagerTestCase(unittest.TestCase):
    def test_resolve_output_manager_uses_qgis_owned_module(self):
        output_manager = session_manager._resolve_output_manager()

        self.assertIsNotNone(output_manager)
        assert output_manager is not None
        self.assertEqual(
            output_manager.__name__,
            "autocarto.tools.system._shared.output_manager",
        )

    @mock.patch("autocarto.core.session_manager.switch_session_log")
    @mock.patch(
        "autocarto.core.session_manager._resolve_output_manager", return_value=None
    )
    def test_activate_from_tool_params_can_derive_session_id(
        self, _mock_output_manager, _mock_switch_log
    ):
        manager = SessionManager()
        params = {
            "project_path": "/tmp/demo/demo.qgz",
            "session_dir": "/tmp/demo/autoc/sessions/session-123",
            "session_subdirs": {
                "output": "/tmp/demo/autoc/sessions/session-123/output",
            },
        }

        context = manager.activate_from_tool_params(params)

        self.assertIsNotNone(context)
        assert context is not None
        self.assertEqual(context.session_id, "session-123")
        self.assertEqual(context.session_dir, "/tmp/demo/autoc/sessions/session-123")
        self.assertEqual(
            context.session_subdirs["output"],
            "/tmp/demo/autoc/sessions/session-123/output",
        )

    @mock.patch("autocarto.core.session_manager.switch_session_log")
    @mock.patch(
        "autocarto.core.session_manager._resolve_output_manager", return_value=None
    )
    def test_activate_reapplies_context_when_same_session(
        self, _mock_output_manager, _mock_switch_log
    ):
        manager = SessionManager()
        applied = []

        def capture(context: session_manager.SessionContext):
            applied.append(context.session_id)

        with mock.patch.object(manager, "_apply_context", side_effect=capture):
            manager.activate("session-a", project_path="/tmp/demo/demo.qgz")
            manager.activate("session-a", project_path="/tmp/demo/demo.qgz")

        self.assertEqual(applied, ["session-a", "session-a"])

    @mock.patch("autocarto.core.session_manager.switch_session_log")
    @mock.patch(
        "autocarto.core.session_manager._resolve_output_manager", return_value=None
    )
    def test_ensure_runtime_context_builds_deterministic_session(
        self, _mock_output_manager, _mock_switch_log
    ):
        manager = SessionManager()

        first = manager.ensure_runtime_context("/tmp/demo/demo.qgz")
        second = manager.ensure_runtime_context("/tmp/demo/demo.qgz")

        self.assertIsNotNone(first)
        self.assertIsNotNone(second)
        assert first is not None
        assert second is not None
        self.assertEqual(first.session_id, second.session_id)
        self.assertTrue(first.session_id.startswith("runtime_"))

    def test_resolve_sessions_base_dir_uses_project_name_directory(self):
        manager = SessionManager()

        base_dir = manager._resolve_sessions_base_dir("/tmp/demo/demo.qgz")

        self.assertEqual(base_dir, "/tmp/demo/autoc/demo/sessions")

    def test_resolve_sessions_base_dir_without_project_uses_plugin_data_dir(self):
        manager = SessionManager()

        with (
            TemporaryDirectory() as tmp_dir,
            mock.patch.dict("os.environ", {"AUTOCARTO_HOME": tmp_dir}, clear=False),
        ):
            base_dir = manager._resolve_sessions_base_dir("")
            expected = str(Path(tmp_dir).resolve() / "data/sessions/no_project")

        self.assertEqual(base_dir, expected)

    @mock.patch("autocarto.core.session_manager.switch_session_log")
    @mock.patch(
        "autocarto.core.session_manager._resolve_output_manager", return_value=None
    )
    def test_activate_from_tool_params_rewrites_stale_subdirs_to_session_root(
        self, _mock_output_manager, _mock_switch_log
    ):
        manager = SessionManager()
        params = {
            "project_path": "/tmp/demo/demo.qgz",
            "session_id": "session-123",
            "session_dir": "/tmp/demo/autoc/demo/sessions/session-123",
            "session_subdirs": {
                "logs": "/tmp/demo/autoc/sessions/session-123/logs",
                "exports": "/tmp/demo/autoc/sessions/session-123/exports",
            },
        }

        context = manager.activate_from_tool_params(params)

        self.assertIsNotNone(context)
        assert context is not None
        self.assertEqual(
            context.session_subdirs["logs"],
            "/tmp/demo/autoc/demo/sessions/session-123/logs",
        )
        self.assertEqual(
            context.session_subdirs["exports"],
            "/tmp/demo/autoc/demo/sessions/session-123/exports",
        )


if __name__ == "__main__":
    unittest.main()
