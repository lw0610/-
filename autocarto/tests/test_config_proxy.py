import tempfile
import unittest
from pathlib import Path
from unittest import mock

from autocarto.core.config_manager import ConfigProxy


class ConfigProxyTestCase(unittest.TestCase):
    def test_loads_default_when_config_missing(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            plugin_root = Path(tmp_dir) / "autocarto"
            config_dir = plugin_root / "config"
            config_dir.mkdir(parents=True, exist_ok=True)
            proxy = ConfigProxy(config_dir)
            self.assertEqual(proxy.get("log.level"), "INFO")
            self.assertIsNone(proxy.get("backend.ws_url"))
            self.assertEqual(proxy.get("backend.reconnect_max_sec"), 8)
            self.assertEqual(proxy.get("log.dir"), "data/logs")
            self.assertEqual(proxy.get("data_dir"), "data")
            self.assertIsNone(proxy.get("middleware.enabled"))
            self.assertIsNone(proxy.get("ports.qgis_tools"))

    def test_resolve_path_supports_plugin_relative_paths(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            plugin_root = Path(tmp_dir) / "autocarto"
            config_dir = plugin_root / "config"
            config_dir.mkdir(parents=True, exist_ok=True)
            proxy = ConfigProxy(config_dir)

            self.assertEqual(
                proxy.resolve_path("log.dir"), plugin_root / "data" / "logs"
            )
            resolved_data_dir = proxy.resolve_path("data_dir")
            self.assertIsNotNone(resolved_data_dir)
            self.assertEqual(resolved_data_dir, plugin_root / "data")

    def test_config_proxy_exposes_read_only_bootstrap_view(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            plugin_root = Path(tmp_dir) / "autocarto"
            config_dir = plugin_root / "config"
            config_dir.mkdir(parents=True, exist_ok=True)
            proxy = ConfigProxy(config_dir)

            self.assertTrue(callable(getattr(proxy, "get", None)))
            self.assertTrue(callable(getattr(proxy, "resolve_path", None)))
            self.assertFalse(hasattr(proxy, "set"))
            self.assertFalse(hasattr(proxy, "save"))
            self.assertFalse(hasattr(proxy, "batch_set"))
            self.assertFalse(hasattr(proxy, "update"))

    def test_user_config_is_not_merged_into_bootstrap_view(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            plugin_root = Path(tmp_dir) / "autocarto"
            config_dir = plugin_root / "config"
            config_dir.mkdir(parents=True, exist_ok=True)
            (config_dir / "plugin_config.yaml").write_text(
                "backend:\n  reconnect_max_sec: 8\n",
                encoding="utf-8",
            )

            user_config_path = Path(tmp_dir) / "config" / "config.yaml"
            user_config_path.parent.mkdir(parents=True, exist_ok=True)
            user_config_path.write_text(
                "backend:\n  reconnect_max_sec: 30\nlog:\n  level: DEBUG\n",
                encoding="utf-8",
            )

            with mock.patch(
                "autocarto.core.paths.get_autocarto_home",
                return_value=Path(tmp_dir),
            ):
                proxy = ConfigProxy(config_dir)

            self.assertEqual(proxy.get("backend.reconnect_max_sec"), 8)
            self.assertEqual(proxy.get("log.level"), "INFO")

    def test_bootstrap_view_strips_legacy_backend_discovery_fields(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            plugin_root = Path(tmp_dir) / "autocarto"
            config_dir = plugin_root / "config"
            config_dir.mkdir(parents=True, exist_ok=True)
            (config_dir / "plugin_config.yaml").write_text(
                "backend:\n"
                "  reconnect_max_sec: 12\n"
                "  ws_url: ws://127.0.0.1:3000/ws\n"
                "  api_base_url: http://127.0.0.1:3000\n",
                encoding="utf-8",
            )

            proxy = ConfigProxy(config_dir)

            self.assertEqual(proxy.get("backend.reconnect_max_sec"), 12)
            self.assertIsNone(proxy.get("backend.ws_url"))
            self.assertIsNone(proxy.get("backend.api_base_url"))

    def test_bootstrap_view_strips_legacy_agent_system_port_policy(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            plugin_root = Path(tmp_dir) / "autocarto"
            config_dir = plugin_root / "config"
            config_dir.mkdir(parents=True, exist_ok=True)
            (config_dir / "plugin_config.yaml").write_text(
                "ports:\n"
                "  agent_system:\n"
                "    default: 3000\n"
                "    auto_fallback: true\n"
                "  qgis_tools:\n"
                "    default: 50201\n",
                encoding="utf-8",
            )

            proxy = ConfigProxy(config_dir)

            self.assertIsNone(proxy.get("ports.agent_system.default"))
            self.assertIsNone(proxy.get("ports.agent_system.auto_fallback"))
            self.assertIsNone(proxy.get("ports.qgis_tools"))


if __name__ == "__main__":
    unittest.main()
