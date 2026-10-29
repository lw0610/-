import unittest
from unittest.mock import MagicMock, patch
import sys

class PluginUIMigrationTestCase(unittest.TestCase):
    def setUp(self):
        # Scope Qt stand-ins to this test. Collection must retain real QGIS for
        # the rendering and geometry regression modules collected afterwards.
        core = MagicMock()
        core.Qt.RightDockWidgetArea = 2
        self.app_properties = {}
        core.QCoreApplication.instance.return_value.property.side_effect = self.app_properties.get
        core.QCoreApplication.instance.return_value.setProperty.side_effect = self.app_properties.__setitem__
        modules = {name: MagicMock() for name in (
            "qgis", "qgis.PyQt", "qgis.PyQt.QtWidgets", "qgis.PyQt.QtGui",
            "qgis.core", "qgis.gui",
        )}
        modules["qgis.PyQt.QtCore"] = core
        module_patch = patch.dict(sys.modules, modules)
        module_patch.start()
        self.addCleanup(module_patch.stop)
        sys.modules.pop("autocarto.autocarto_plugin", None)
        from autocarto.autocarto_plugin import AutoCartoPlugin
        self.plugin_type = AutoCartoPlugin
        self.mock_iface = MagicMock()
        self.plugin = self.plugin_type(self.mock_iface)

        # Mock what we need for testing
        self.plugin._ws_client = MagicMock()
        self.plugin._status_action = MagicMock()
        self.plugin._last_reported_project_path = "/path/to/project.qgz"
        self.plugin._instance_id = "test-instance-123"

        # Patch the color generator to verify what color it produces
        self.patcher = patch.object(self.plugin, "_create_status_icon")
        self.mock_create_icon = self.patcher.start()
        self.mock_create_icon.return_value = MagicMock()

    def tearDown(self):
        self.patcher.stop()

    def test_plugin_reload_keeps_qgis_instance_identity(self):
        self.app_properties.clear()
        with patch("time.time", side_effect=[100, 200]):
            first = self.plugin._build_instance_id()
            self.assertEqual(self.plugin_type(self.mock_iface)._instance_id, first)
            self.app_properties.clear()
            self.assertNotEqual(self.plugin._build_instance_id(), first)

    def test_update_status_tooltip_connected(self):
        ws_mock = MagicMock()
        setattr(ws_mock.is_connected, "return_value", True)
        setattr(ws_mock.is_reconnecting, "return_value", False)
        self.plugin._ws_client = ws_mock

        action_mock = MagicMock()
        self.plugin._status_action = action_mock

        self.plugin._update_status_tooltip()

        expected_tooltip = "AutoCarto AI 状态: 已连接\n实例 ID: test-instance-123\n当前工程: /path/to/project.qgz"
        action_mock.setToolTip.assert_called_with(expected_tooltip)

        self.mock_create_icon.assert_called_with("green")
        action_mock.setIcon.assert_called_with(self.mock_create_icon.return_value)

    def test_update_status_tooltip_reconnecting(self):
        ws_mock = MagicMock()
        setattr(ws_mock.is_connected, "return_value", False)
        setattr(ws_mock.is_reconnecting, "return_value", True)
        self.plugin._ws_client = ws_mock

        action_mock = MagicMock()
        self.plugin._status_action = action_mock

        self.plugin._update_status_tooltip()

        expected_tooltip = "AutoCarto AI 状态: 正在重连...\n实例 ID: test-instance-123\n当前工程: /path/to/project.qgz"
        action_mock.setToolTip.assert_called_with(expected_tooltip)

        self.mock_create_icon.assert_called_with("yellow")
        action_mock.setIcon.assert_called_with(self.mock_create_icon.return_value)

    def test_update_status_tooltip_disconnected(self):
        ws_mock = MagicMock()
        setattr(ws_mock.is_connected, "return_value", False)
        setattr(ws_mock.is_reconnecting, "return_value", False)
        self.plugin._ws_client = ws_mock

        action_mock = MagicMock()
        self.plugin._status_action = action_mock

        self.plugin._update_status_tooltip()

        expected_tooltip = "AutoCarto AI 状态: 未连接\n实例 ID: test-instance-123\n当前工程: /path/to/project.qgz"
        action_mock.setToolTip.assert_called_with(expected_tooltip)

        self.mock_create_icon.assert_called_with("red")
        action_mock.setIcon.assert_called_with(self.mock_create_icon.return_value)

    def test_init_gui_creates_no_dock_and_uncheckable_action(self):
        mock_action_instance = MagicMock()

        with (
            patch(
                "autocarto.autocarto_plugin.QAction", return_value=mock_action_instance
            ),
            patch("qgis.PyQt.QtGui.QIcon"),
            patch("autocarto.core.instance_ws_client.InstanceWSClient"),
            patch("autocarto.core.tool_engine.create_engine"),
            patch("autocarto.core.plugin_manager.PluginManager"),
            patch("autocarto.core.config_manager.ConfigProxy"),
            patch("autocarto.utils.logger.init_log_dir"),
            patch("autocarto.utils.logger.configure_log_policy"),
        ):
            plugin = self.plugin_type(self.mock_iface)

            with patch.object(plugin, "_create_status_icon"):
                plugin.initGui()

            self.mock_iface.addDockWidget.assert_not_called()
            self.assertIsNone(plugin._status_dock)

            mock_action_instance.setCheckable.assert_not_called()

            self.assertEqual(mock_action_instance.triggered.connect.call_count, 1)
            mock_action_instance.toggled.connect.assert_not_called()


if __name__ == "__main__":
    unittest.main()
