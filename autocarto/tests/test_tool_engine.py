import importlib.util
import unittest
from unittest import mock
from pathlib import Path
import tempfile
from types import ModuleType
from typing import Any, Mapping, Optional, Sequence

from autocarto.core.tool_engine import QGISToolEngine, create_engine
from autocarto.core.tool_engine import engine as tool_engine_module
from autocarto.core.tool_engine.definitions import ToolDefinitionLoader
from autocarto.core.tool_engine.engine import execute_tool
from autocarto.core.tool_engine import registry as tool_registry_module
from autocarto.core.tool_engine.registry import ToolDefinition


def _load_packaged_runtime_smoke_module() -> ModuleType:
    repo_root = Path(__file__).resolve().parents[2]
    module_path = repo_root / "autocarto-web" / "scripts" / "packaged_runtime_smoke.py"
    spec = importlib.util.spec_from_file_location("packaged_runtime_smoke", module_path)
    if spec is None or spec.loader is None:
        raise AssertionError(f"无法加载 packaged runtime smoke 脚本: {module_path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class ToolEngineImportSmokeTestCase(unittest.TestCase):
    def assert_parity_or_tolerated_diff(
        self,
        expected: Mapping[str, Any],
        actual: Mapping[str, Any],
        tolerated_diff_keys: Optional[Sequence[str]] = None,
    ) -> None:
        tolerated = set(tolerated_diff_keys or [])
        self.assertEqual(set(expected.keys()), set(actual.keys()))
        for key, expected_value in expected.items():
            if key in tolerated:
                continue
            self.assertEqual(actual[key], expected_value, key)

    def test_public_exports_are_available_from_autocarto_core_tool_engine(self):
        self.assertTrue(callable(create_engine))

    def test_engine_module_does_not_expose_legacy_local_directory_helpers(self):
        self.assertFalse(hasattr(tool_engine_module, "resolve_default_tool_dirs"))
        self.assertFalse(hasattr(tool_engine_module, "resolve_default_skill_dirs"))

    def test_registry_module_does_not_expose_legacy_definition_loading_alias(self):
        self.assertFalse(
            hasattr(tool_registry_module.ToolRegistry, "load_from_definitions")
        )

    def test_definition_loader_does_not_keep_removed_definitions_dir_state(self):
        loader = ToolDefinitionLoader([])

        self.assertFalse(hasattr(loader, "definitions_dir"))

    def test_init_does_not_fetch_runtime_descriptor_manifest(self):
        with (
            mock.patch(
                "autocarto.core.tool_engine.engine.ToolRegistry"
            ) as registry_cls,
            mock.patch(
                "autocarto.core.tool_engine.engine._load_execution_bindings",
                return_value=[
                    ToolDefinition(
                        name="demo_tool",
                        description="demo",
                        category="demo",
                        handler=lambda params: {"success": True, "output": params},
                        inputSchema={"type": "object"},
                    )
                ],
            ) as bootstrap_helper,
        ):
            registry = registry_cls.return_value

            _ = QGISToolEngine()

        bootstrap_helper.assert_called_once_with([])
        registry.register.assert_called_once()
        registry.load_all.assert_not_called()

    def test_invoke_uses_runtime_handler_for_registered_tool(self):
        with mock.patch.object(QGISToolEngine, "_register_tools", lambda self: None):
            engine = QGISToolEngine()

        engine._registry.register(
            ToolDefinition(
                name="demo_tool",
                description="demo",
                category="demo",
                handler=lambda params: {"success": True, "output": params},
                inputSchema={"type": "object"},
            )
        )

        with mock.patch(
            "autocarto.core.tool_engine.engine.call_tool_handler",
            return_value={"success": True, "output": {"name": "ok"}},
        ) as handler:
            result = engine.invoke("demo_tool", {"name": "ok"})

        handler.assert_called_once()
        self.assertEqual(result, {"success": True, "output": {"name": "ok"}})

    def test_list_tools_starts_empty_without_descriptor_catalog(self):
        with (
            mock.patch(
                "autocarto.core.tool_engine.engine._load_execution_bindings",
                return_value=[
                    ToolDefinition(
                        name="demo_tool",
                        description="demo",
                        category="demo",
                        handler=lambda params: {"success": True, "output": params},
                        inputSchema={"type": "object"},
                    )
                ],
            ),
        ):
            engine = QGISToolEngine()

        self.assertEqual(engine.list_tools(), [])

    def test_invoke_raises_for_unknown_tool(self):
        with mock.patch.object(QGISToolEngine, "_register_tools", lambda self: None):
            engine = QGISToolEngine()

        with self.assertRaisesRegex(ValueError, "未知工具"):
            engine.invoke("missing_tool", {})

    @mock.patch("autocarto.core.tool_engine.engine.QGISToolEngine")
    def test_create_engine_uses_default_constructor(self, engine_cls: Any):
        _ = create_engine()

        engine_cls.assert_called_once_with()

    @mock.patch("autocarto.core.tool_engine.engine.QGISToolEngine")
    def test_create_engine_rejects_explicit_skill_dirs(self, engine_cls: Any):
        with self.assertRaisesRegex(ValueError, "skill_dirs"):
            _ = create_engine(skill_dirs=["/tmp/plugin/skills", "/tmp/user/skills"])

        engine_cls.assert_not_called()

    def test_empty_execution_manifest_does_not_fallback_to_plugin_tool_tree(self):
        loader = ToolDefinitionLoader([])

        self.assertEqual(loader.load_all(), {})

    def test_engine_rejects_legacy_definitions_dir(self):
        with self.assertRaisesRegex(ValueError, "definitions_dir"):
            _ = QGISToolEngine(definitions_dir="/tmp/tools/system")

    def test_execute_tool_wraps_success_output_and_collects_output_layers(self):
        engine = mock.Mock()
        engine.invoke.return_value = {
            "output": "/tmp/demo.gpkg",
            "info": {"layer_path": "/tmp/preview.geojson"},
        }

        result = execute_tool("demo_tool", {"name": "ok"}, engine=engine)

        engine.invoke.assert_called_once_with("demo_tool", {"name": "ok"})
        self.assert_parity_or_tolerated_diff(
            {
                "success": True,
                "output": {
                    "output": "/tmp/demo.gpkg",
                    "info": {"layer_path": "/tmp/preview.geojson"},
                },
                "error": "",
                "output_layers": [
                    {"path": "/tmp/demo.gpkg", "auto_load": True},
                    {"path": "/tmp/preview.geojson", "auto_load": True},
                ],
            },
            result,
        )

    def test_execute_tool_preserves_complete_result_envelope(self):
        engine = mock.Mock()
        engine.invoke.return_value = {
            "success": True,
            "output": {"name": "ok"},
            "error": "",
            "output_layers": [{"path": "/tmp/demo.gpkg", "auto_load": True}],
        }

        result = execute_tool("demo_tool", {"name": "ok"}, engine=engine)

        self.assert_parity_or_tolerated_diff(engine.invoke.return_value, result)

    def test_execute_tool_resolves_manifest_handler_file_relative_to_plugin_root(self):
        with tempfile.TemporaryDirectory() as plugin_root:
            handler_path = (
                Path(plugin_root) / "tools" / "system" / "demo" / "handlers" / "demo.py"
            )
            handler_path.parent.mkdir(parents=True, exist_ok=True)
            handler_path.write_text(
                "def run(params):\n    return {'success': True, 'output': params, 'error': '', 'output_layers': []}\n",
                encoding="utf-8",
            )

            manifest = [
                {
                    "name": "demo_tool",
                    "description": "demo",
                    "domain": "demo",
                    "runtime": "qgis",
                    "backend": "qgis",
                    "handler_file": "tools/system/demo/handlers/demo.py",
                    "handler_function": "run",
                    "parameters": [],
                }
            ]
            with (
                mock.patch(
                    "autocarto.core.tool_engine.definitions.get_plugin_root",
                    return_value=Path(plugin_root),
                ),
                mock.patch(
                    "autocarto.core.tool_engine.engine.call_tool_handler",
                    side_effect=lambda handler, params: handler(params),
                ),
            ):
                engine = QGISToolEngine()
                engine._execution_manifest = manifest
                engine._register_tools()
                result = execute_tool("demo_tool", {"name": "ok"}, engine=engine)

        self.assertTrue(result["success"], result)
        self.assertEqual(result["output"], {"name": "ok"})

    def test_update_contract_replaces_registry_with_authority_manifest(self):
        manifest = [
            {
                "name": "demo_tool",
                "description": "demo",
                "domain": "demo",
                "runtime": "qgis",
                "backend": "qgis",
                "handler": "demo.run",
                "parameters": [],
            }
        ]
        binding = ToolDefinition(
            name="demo_tool",
            description="demo",
            category="demo",
            handler=lambda params: {"name": params.get("name")},
            inputSchema={"type": "object"},
        )

        with (
            mock.patch(
                "autocarto.core.tool_engine.engine._load_execution_bindings",
                side_effect=lambda incoming: [binding] if incoming == manifest else [],
            ) as loader,
            mock.patch(
                "autocarto.core.tool_engine.engine.call_tool_handler",
                side_effect=lambda handler, params: handler(params),
            ),
        ):
            engine = QGISToolEngine()
            engine.update_contract(
                execution_manifest=manifest,
                public_tool_catalog=[
                    {
                        "name": "demo_tool",
                        "description": "demo",
                        "runtime": "qgis",
                        "backend": "qgis",
                        "available": True,
                    }
                ],
            )
            result = execute_tool("demo_tool", {"name": "ok"}, engine=engine)

        self.assertTrue(result["success"], result)
        self.assertEqual(result["output"], {"name": "ok"})
        loader.assert_any_call(manifest)

    def test_packaged_smoke_evidence_contract_is_discoverable(self):
        smoke = _load_packaged_runtime_smoke_module()
        repo_root = Path(__file__).resolve().parents[2]
        build_script = (repo_root / "build.sh").read_text(encoding="utf-8")
        readme = (repo_root / "README.md").read_text(encoding="utf-8")
        plan = (
            repo_root
            / ".sisyphus"
            / "plans"
            / "agent-tool-skill-architecture-optimization.md"
        ).read_text(encoding="utf-8")

        self.assertEqual(smoke.DESCRIPTOR_EVIDENCE_NAME, "descriptor.json")
        self.assertEqual(smoke.LEGACY_REJECTION_EVIDENCE_NAME, "legacy_rejection.json")
        self.assertEqual(smoke.SMOKE_LOG_NAME, "smoke.log")
        self.assertEqual(smoke.STDOUT_LOG_NAME, "stdout.log")
        self.assertEqual(smoke.STDERR_LOG_NAME, "stderr.log")

        for needle in [
            ".sisyphus/evidence/task-6-packaged-runtime-smoke",
            ".sisyphus/evidence/task-7-release-gate",
        ]:
            self.assertIn(needle, build_script)

        for needle in [
            ".sisyphus/evidence/task-6-packaged-runtime-smoke/descriptor.json",
            ".sisyphus/evidence/task-6-packaged-runtime-smoke/stdout.log",
            ".sisyphus/evidence/task-6-packaged-runtime-smoke/stderr.log",
            ".sisyphus/evidence/task-6-packaged-runtime-smoke/smoke.log",
            ".sisyphus/evidence/task-7-release-gate/summary.txt",
        ]:
            self.assertIn(needle, readme)

        for needle in [
            ".sisyphus/evidence/task-8-cancel-live.txt",
            ".sisyphus/evidence/task-8-cancel-error.txt",
        ]:
            self.assertIn(needle, plan)


if __name__ == "__main__":
    unittest.main()
