import os
import shutil
import unittest
from pathlib import Path
from typing import Optional
from unittest import mock
import uuid

from autocarto.core.instance_ws_client import InstanceWSClient


def _make_workspace_temp_dir() -> Path:
    root = Path(__file__).resolve().parents[2] / ".tmp-test"
    path = root / f"instance-runtime-port-{uuid.uuid4().hex}"
    path.mkdir(parents=True, exist_ok=False)
    return path


class _FakeConfigManager:
    def get(self, _key: str, default: object = None) -> object:
        return default


class _FakeToolEngine:
    def list_tools(self) -> list[dict[str, str]]:
        return []

    def invoke(
        self, tool_name: str, params: Optional[dict[str, object]] = None
    ) -> dict[str, object]:
        return {
            "success": True,
            "output": {"tool": tool_name, "params": params or {}},
            "error": "",
            "output_layers": [],
        }


class InstanceWSClientRuntimePortFileTestCase(unittest.TestCase):
    def _client(self) -> InstanceWSClient:
        return InstanceWSClient(
            config_manager=_FakeConfigManager(),
            tool_engine=_FakeToolEngine(),
            instance_id="inst-1",
            qgis_version="3.40",
            skills_dir="/tmp/plugin/skills",
        )

    def test_plugin_ws_url_comes_from_runtime_port_file(self):
        tmp_dir = _make_workspace_temp_dir()
        try:
            port_file = Path(tmp_dir, "data", ".port")
            port_file.parent.mkdir(parents=True, exist_ok=True)
            port_file.write_text("62101", encoding="utf-8")

            with mock.patch.dict(os.environ, {"AUTOCARTO_HOME": str(tmp_dir)}, clear=False):
                self.assertEqual(
                    self._client().backend_url(),
                    "ws://127.0.0.1:62101/ws?role=plugin",
                )
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)

    def test_plugin_ws_url_is_empty_until_port_file_exists(self):
        tmp_dir = _make_workspace_temp_dir()
        try:
            with mock.patch.dict(os.environ, {"AUTOCARTO_HOME": str(tmp_dir)}, clear=False):
                self.assertEqual(self._client().backend_url(), "")
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
