import os
import shutil
import unittest
import uuid
from pathlib import Path
from unittest import mock

from autocarto.core import service_port_reader


def _make_workspace_temp_dir() -> Path:
    root = Path(__file__).resolve().parents[2] / ".tmp-test"
    path = root / f"service-port-reader-{uuid.uuid4().hex}"
    path.mkdir(parents=True, exist_ok=False)
    return path


class ServicePortReaderTestCase(unittest.TestCase):
    def test_read_runtime_port_uses_runtime_port_file(self):
        tmp_dir = _make_workspace_temp_dir()
        try:
            port_file = Path(tmp_dir, "data", ".port")
            port_file.parent.mkdir(parents=True, exist_ok=True)
            port_file.write_text("50321", encoding="utf-8")

            with mock.patch.dict(os.environ, {"AUTOCARTO_HOME": str(tmp_dir)}, clear=False):
                self.assertEqual(service_port_reader.get_runtime_port_file_path(), port_file)
                self.assertEqual(service_port_reader.read_runtime_port(), 50321)
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)

    def test_read_runtime_port_returns_none_without_runtime_home(self):
        with mock.patch.dict(os.environ, {"AUTOCARTO_HOME": ""}, clear=False):
            self.assertIsNone(service_port_reader.get_runtime_port_file_path())
            self.assertIsNone(service_port_reader.read_runtime_port())

    def test_read_runtime_port_returns_none_for_missing_or_invalid_file(self):
        tmp_dir = _make_workspace_temp_dir()
        try:
            with mock.patch.dict(os.environ, {"AUTOCARTO_HOME": str(tmp_dir)}, clear=False):
                self.assertIsNone(service_port_reader.read_runtime_port())

                port_file = Path(tmp_dir, "data", ".port")
                port_file.parent.mkdir(parents=True, exist_ok=True)
                port_file.write_text("70000", encoding="utf-8")
                self.assertIsNone(service_port_reader.read_runtime_port())

                port_file.write_text("not-a-port", encoding="utf-8")
                self.assertIsNone(service_port_reader.read_runtime_port())
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)

    def test_runtime_urls_are_derived_from_port_file(self):
        tmp_dir = _make_workspace_temp_dir()
        try:
            port_file = Path(tmp_dir, "data", ".port")
            port_file.parent.mkdir(parents=True, exist_ok=True)
            port_file.write_text("50322", encoding="utf-8")

            with mock.patch.dict(os.environ, {"AUTOCARTO_HOME": str(tmp_dir)}, clear=False):
                self.assertEqual(
                    service_port_reader.build_runtime_backend_url(),
                    "http://127.0.0.1:50322",
                )
                self.assertEqual(
                    service_port_reader.build_runtime_ws_url(),
                    "ws://127.0.0.1:50322/ws",
                )
                self.assertEqual(
                    service_port_reader.build_runtime_ws_url(role="plugin"),
                    "ws://127.0.0.1:50322/ws?role=plugin",
                )
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
