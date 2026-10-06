import unittest
from pathlib import Path

from autocarto.core.tool_engine.definitions import ToolDefinitionLoader


class DefinitionValidationTestCase(unittest.TestCase):
    def _authority_manifest(self):
        return [
            {
                "name": "buffer",
                "description": "buffer",
                "category": "vector_geometry",
                "runtime": "qgis",
                "execution": {"backend_tool": "native:buffer"},
            },
            {
                "name": "add_field_to_attributes_table",
                "description": "add field",
                "category": "vector_attributes",
                "runtime": "qgis",
                "execution": {"backend_tool": "native:addfieldtoattributestable"},
            },
            {
                "name": "open_project",
                "description": "open project",
                "category": "project-state",
                "runtime": "qgis",
                "execution": {"handler": "handlers.project.open_project_handler"},
            },
            {
                "name": "close_instance",
                "description": "close instance",
                "category": "project-state",
                "runtime": "internal",
                "execution": {"handler": "handlers.instance.close_instance_handler"},
            },
            {
                "name": "export_print_layout",
                "description": "export print layout",
                "category": "layout",
                "runtime": "qgis",
                "execution": {
                    "handler": "handlers.layout_export.export_print_layout_handler"
                },
            },
            {
                "name": "cartography_set_style",
                "description": "style",
                "category": "cartography",
                "runtime": "qgis",
                "execution": {"handler": "handlers.style.set_layer_style_handler"},
            },
            {
                "name": "export_layout_pdf",
                "description": "layout pdf",
                "category": "layout",
                "runtime": "qgis",
                "execution": {
                    "handler": "handlers.layout_export.export_layout_pdf_handler"
                },
            },
            {
                "name": "layout_create",
                "description": "layout create",
                "category": "layout",
                "runtime": "qgis",
                "execution": {"handler": "handlers.layout.create_layout_handler"},
            },
            {
                "name": "layout_add_map",
                "description": "layout add map",
                "category": "layout",
                "runtime": "qgis",
                "execution": {"handler": "handlers.layout_items.add_map_item_handler"},
            },
            {
                "name": "layout_set_item_background",
                "description": "layout bg",
                "category": "layout",
                "runtime": "qgis",
                "execution": {
                    "handler": "handlers.layout_advanced.set_item_background_handler"
                },
            },
            {
                "name": "bar_chart",
                "description": "bar chart",
                "category": "charts",
                "runtime": "qgis",
                "execution": {"handler": "handlers.charts.bar_chart_handler"},
            },
            {
                "name": "pyqgis_execute_script",
                "description": "exec script",
                "category": "tools",
                "runtime": "qgis",
                "execution": {"handler": "handlers.tools.pyqgis_execute_script"},
            },
            {
                "name": "pyqgis_submit_script",
                "description": "submit script",
                "category": "tools",
                "runtime": "qgis",
                "execution": {"handler": "handlers.tools.pyqgis_submit_script"},
            },
            {
                "name": "pyqgis_get_task_status",
                "description": "task status",
                "category": "tools",
                "runtime": "qgis",
                "execution": {"handler": "handlers.tools.pyqgis_get_task_status"},
            },
            {
                "name": "pyqgis_get_context",
                "description": "context",
                "category": "tools",
                "runtime": "qgis",
                "execution": {"handler": "handlers.tools.pyqgis_get_context"},
            },
            {
                "name": "pyqgis_validate_script",
                "description": "validate script",
                "category": "tools",
                "runtime": "qgis",
                "execution": {"handler": "handlers.tools.pyqgis_validate_script"},
            },
            {
                "name": "pyqgis_cancel_task",
                "description": "cancel task",
                "category": "tools",
                "runtime": "qgis",
                "execution": {"handler": "handlers.tools.pyqgis_cancel_task"},
            },
            {
                "name": "line_of_sight",
                "description": "line of sight",
                "category": "visibility",
                "runtime": "qgis",
                "execution": {"handler": "handlers.visibility.line_of_sight_handler"},
            },
        ]

    def test_autocarto_skills_have_no_legacy_cli_runtime_left(self):
        skills_dir = Path(__file__).resolve().parents[1] / "skills"

        remaining = []
        for path in skills_dir.rglob("*.yaml"):
            text = path.read_text(encoding="utf-8")
            if "runtime: cli" in text:
                remaining.append(path)

        self.assertEqual(remaining, [])

    def test_definition_loader_can_load_key_skill_tools(self):
        loader = ToolDefinitionLoader(self._authority_manifest())
        definitions = loader.load_all()

        for tool_name in (
            "buffer",
            "add_field_to_attributes_table",
            "open_project",
            "close_instance",
            "export_print_layout",
        ):
            self.assertIn(tool_name, definitions)

    def test_definition_loader_has_only_plugin_side_handlers(self):
        loader = ToolDefinitionLoader(self._authority_manifest())
        definitions = loader.load_all()

        offenders = {
            name: definition.get("handler")
            for name, definition in definitions.items()
            if definition.get("handler")
            and not (
                str(definition.get("handler") or "").startswith(
                    "autocarto.core.tool_engine."
                )
                or str(definition.get("handler") or "").startswith("handlers.")
            )
        }

        self.assertEqual(offenders, {})

    def test_definition_loader_loads_migrated_plugin_handlers(self):
        loader = ToolDefinitionLoader(self._authority_manifest())
        definitions = loader.load_all()

        expected_handlers = {
            "cartography_set_style": "handlers.style.set_layer_style_handler",
            "export_layout_pdf": "handlers.layout_export.export_layout_pdf_handler",
            "layout_create": "handlers.layout.create_layout_handler",
            "layout_add_map": "handlers.layout_items.add_map_item_handler",
            "layout_set_item_background": "handlers.layout_advanced.set_item_background_handler",
            "export_print_layout": "handlers.layout_export.export_print_layout_handler",
            "bar_chart": "handlers.charts.bar_chart_handler",
            "pyqgis_execute_script": "handlers.tools.pyqgis_execute_script",
            "pyqgis_submit_script": "handlers.tools.pyqgis_submit_script",
            "pyqgis_get_task_status": "handlers.tools.pyqgis_get_task_status",
            "pyqgis_get_context": "handlers.tools.pyqgis_get_context",
            "pyqgis_validate_script": "handlers.tools.pyqgis_validate_script",
            "pyqgis_cancel_task": "handlers.tools.pyqgis_cancel_task",
            "line_of_sight": "handlers.visibility.line_of_sight_handler",
        }

        for tool_name, handler_path in expected_handlers.items():
            self.assertIn(tool_name, definitions)
            self.assertEqual(definitions[tool_name].get("handler"), handler_path)


if __name__ == "__main__":
    unittest.main()
