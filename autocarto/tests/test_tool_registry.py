import unittest
import inspect
from unittest import mock

from autocarto.core.tool_engine.definitions import ToolDefinitionLoader
from autocarto.core.tool_engine.registry import ToolDefinition, ToolRegistry


def _noop_handler(_params):
    return {"success": True}


class ToolRegistryTestCase(unittest.TestCase):
    def test_definition_loader_preserves_object_parameter_schema(self):
        loader = ToolDefinitionLoader()

        schema = loader.to_json_schema(
            {
                "parameters": [
                    {
                        "name": "knowledge_json",
                        "type": "object",
                        "required": True,
                    }
                ]
            }
        )

        self.assertEqual(
            schema["properties"]["knowledge_json"]["type"],
            "object",
        )
        self.assertEqual(schema["required"], ["knowledge_json"])

    def test_processing_executor_is_owned_by_autocarto_tool_engine(self):
        from autocarto.core.tool_engine.executor import ProcessingToolExecutor

        self.assertEqual(
            ProcessingToolExecutor.__module__, "autocarto.core.tool_engine.executor"
        )

    def test_output_manager_helpers_are_owned_by_autocarto_shared_runtime(self):
        from autocarto.tools.system._shared import output_manager

        self.assertEqual(
            output_manager.resolve_output_path.__module__,
            "autocarto.tools.system._shared.output_manager",
        )
        self.assertEqual(
            output_manager.sync_session_context_from_params.__module__,
            "autocarto.tools.system._shared.output_manager",
        )

    def test_input_path_resolver_is_owned_by_autocarto_shared_runtime(self):
        from autocarto.tools.system._shared import input_path_resolver

        self.assertEqual(
            getattr(input_path_resolver, "resolve_input_path").__module__,
            "autocarto.tools.system._shared.input_path_resolver",
        )

    def test_executor_uses_autocarto_owned_state_helpers(self):
        from autocarto.core.tool_engine.executor import ProcessingToolExecutor

        output_path_source = inspect.getsource(
            ProcessingToolExecutor._resolve_output_path
        )
        input_path_source = inspect.getsource(
            ProcessingToolExecutor._resolve_input_layer_reference
        )

        self.assertIn(
            "autocarto.tools.system._shared.output_manager", output_path_source
        )
        self.assertIn(
            "autocarto.tools.system._shared.input_path_resolver", input_path_source
        )

    def test_register_overwrite_removes_old_category_residue(self):
        registry = ToolRegistry()

        registry.register(
            ToolDefinition(
                name="demo_tool",
                description="demo",
                category="state",
                handler=_noop_handler,
                inputSchema={"type": "object"},
            )
        )
        registry.register(
            ToolDefinition(
                name="demo_tool",
                description="demo new",
                category="canvas",
                handler=_noop_handler,
                inputSchema={"type": "object"},
            )
        )

        categories = registry.list_categories()
        self.assertNotIn("state", categories)
        self.assertEqual(categories["canvas"], ["demo_tool"])
        tool = registry.get("demo_tool")
        if tool is None:
            self.fail("expected demo_tool to remain registered")
        self.assertEqual(tool.category, "canvas")

    def test_register_overwrite_in_same_category_not_duplicate(self):
        registry = ToolRegistry()
        registry.register(
            ToolDefinition(
                name="same_name",
                description="v1",
                category="state",
                handler=_noop_handler,
                inputSchema={"type": "object"},
            )
        )
        registry.register(
            ToolDefinition(
                name="same_name",
                description="v2",
                category="state",
                handler=_noop_handler,
                inputSchema={"type": "object"},
            )
        )

        categories = registry.list_categories()
        self.assertEqual(categories["state"], ["same_name"])
        tool = registry.get("same_name")
        if tool is None:
            self.fail("expected same_name to remain registered")
        self.assertEqual(tool.description, "v2")

    @mock.patch("autocarto.core.tool_engine.registry.ProcessingToolExecutor")
    @mock.patch("autocarto.core.tool_engine.registry.ToolDefinitionLoader")
    def test_load_all_backend_authority_keeps_local_execution_bindings_out_of_catalog(
        self, loader_cls, executor_cls
    ):
        loader = loader_cls.return_value
        loader.load_all.return_value = {
            "add_field_to_attributes_table": {
                "name": "add_field_to_attributes_table",
                "description": "add field",
                "category": "vector_attributes",
                "algorithm_id": "native:addfieldtoattributestable",
                "parameters": [{"name": "INPUT", "type": "layer", "required": True}],
            },
            "buffer": {
                "name": "buffer",
                "description": "buffer",
                "category": "vector_geometry",
                "algorithm_id": "native:buffer",
                "parameters": [{"name": "INPUT", "type": "layer", "required": True}],
            },
        }
        loader.to_json_schema.side_effect = lambda definition: {
            "type": "object",
            "properties": {
                param["name"]: {"type": "string"}
                for param in definition.get("parameters", [])
            },
            "required": [
                param["name"]
                for param in definition.get("parameters", [])
                if param.get("required")
            ],
        }
        executor = executor_cls.return_value
        executor.resolve_tool_algorithm.return_value = {
            "available": True,
            "requested_id": "native:addfieldtoattributestable",
        }

        registry = ToolRegistry()
        loaded = registry.load_all()

        self.assertEqual(loaded, 2)
        self.assertIsNotNone(registry.get("add_field_to_attributes_table"))
        self.assertIsNotNone(registry.get("buffer"))
        self.assertEqual(registry.list_tools(), [])
        loader_cls.assert_called_once_with(None)
        executor_cls.assert_called_once_with(loader.load_all.return_value)
        executor.resolve_tool_algorithm.assert_not_called()

    def test_load_all_delegates_execution_binding_bootstrap_to_internal_helper(self):
        registry = ToolRegistry()

        bootstrap_bindings = [
            ToolDefinition(
                name="demo_tool",
                description="demo",
                category="demo",
                handler=_noop_handler,
                inputSchema={"type": "object"},
            )
        ]

        with mock.patch(
            "autocarto.core.tool_engine.registry._load_execution_bindings",
            return_value=bootstrap_bindings,
        ) as bootstrap_helper:
            execution_manifest = [{"name": "demo_tool"}]
            loaded = registry.load_all(execution_manifest)

        self.assertEqual(loaded, 1)
        bootstrap_helper.assert_called_once_with(execution_manifest)
        tool = registry.get("demo_tool")
        if tool is None:
            self.fail("expected demo_tool bootstrap binding to be registered")
        self.assertEqual(tool.description, "demo")

    @mock.patch("autocarto.core.tool_engine.registry.ProcessingToolExecutor")
    @mock.patch("autocarto.core.tool_engine.registry.ToolDefinitionLoader")
    def test_load_all_execution_denial_is_deferred_to_runtime_adapter(
        self, loader_cls, executor_cls
    ):
        loader = loader_cls.return_value
        loader.load_all.return_value = {
            "buffer": {
                "name": "buffer",
                "description": "buffer",
                "category": "vector_geometry",
                "algorithm_id": "native:buffer",
                "parameters": [{"name": "INPUT", "type": "layer", "required": True}],
            }
        }
        loader.to_json_schema.return_value = {
            "type": "object",
            "properties": {"INPUT": {"type": "string"}},
            "required": ["INPUT"],
        }
        executor = executor_cls.return_value
        executor.resolve_tool_algorithm.return_value = {
            "available": False,
            "requested_id": "native:buffer",
            "reason": "provider_missing",
        }
        executor.execute.return_value = {
            "success": False,
            "output": "provider_missing",
            "info": "runtime unavailable",
        }

        registry = ToolRegistry()
        loaded = registry.load_all()

        self.assertEqual(loaded, 1)
        tool = registry.get("buffer")
        if tool is None:
            self.fail("expected buffer execution binding to remain registered")
        self.assertEqual(
            tool.handler({"INPUT": "/tmp/demo.gpkg"}),
            {
                "success": False,
                "output": "provider_missing",
                "info": "runtime unavailable",
            },
        )
        executor.execute.assert_called_once_with("buffer", {"INPUT": "/tmp/demo.gpkg"})
        executor.resolve_tool_algorithm.assert_not_called()

    def test_legacy_load_from_definitions_alias_is_removed(self):
        self.assertFalse(hasattr(ToolRegistry, "load_from_definitions"))

    def test_definition_loader_without_manifest_returns_empty_definitions(self):
        loader = ToolDefinitionLoader()

        self.assertEqual(loader.load_all(), {})
        self.assertFalse(hasattr(loader, "definitions_dir"))

    def test_definition_loader_loads_manifest_handler_metadata(self):
        loader = ToolDefinitionLoader(
            [
                {
                    "name": "cartography_set_style",
                    "description": "设置图层样式",
                    "category": "cartography",
                    "runtime": "qgis",
                    "parameters": [{"name": "layer", "type": "string"}],
                    "execution": {
                        "handler": "handlers.style.set_layer_style_handler",
                        "handler_file": "/tmp/handlers/style.py",
                        "handler_function": "set_layer_style_handler",
                    },
                }
            ]
        )
        definitions = loader.load_all()

        self.assertIn("cartography_set_style", definitions)
        definition = definitions["cartography_set_style"]
        self.assertEqual(
            definition.get("handler"), "handlers.style.set_layer_style_handler"
        )
        self.assertEqual(definition.get("handler_file"), "/tmp/handlers/style.py")
        self.assertEqual(definition.get("handler_function"), "set_layer_style_handler")

    def test_definition_loader_collects_execution_definitions_from_manifest_entries(
        self,
    ):
        loader = ToolDefinitionLoader(
            [
                {
                    "name": "get_project_info",
                    "description": "系统项目信息",
                    "category": "project-state",
                    "runtime": "qgis",
                    "execution": {
                        "handler": "handlers.project.get_project_info_system",
                        "handler_file": "/tmp/project_system.py",
                        "handler_function": "get_project_info_system",
                    },
                },
                {
                    "name": "save_project",
                    "description": "保存项目",
                    "category": "project-state",
                    "runtime": "qgis",
                    "execution": {
                        "handler": "handlers.project.save_project_user",
                        "handler_file": "/tmp/project_user.py",
                        "handler_function": "save_project_user",
                    },
                },
            ]
        )
        definitions = loader.load_all()

        self.assertCountEqual(definitions.keys(), ["get_project_info", "save_project"])
        definition = definitions["save_project"]
        self.assertEqual(definition.get("category"), "project-state")
        self.assertEqual(definition.get("description"), "保存项目")
        self.assertEqual(
            definition.get("handler"), "handlers.project.save_project_user"
        )
        self.assertEqual(definition.get("handler_file"), "/tmp/project_user.py")


if __name__ == "__main__":
    unittest.main()
