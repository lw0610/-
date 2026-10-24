import unittest
from pathlib import Path
from typing import Any, Dict, Set, Union, cast

import yaml


HANDLER_PACKAGE_EXPECTATIONS = {
    "cartography-styling": {
        "domain_name": "cartography",
        "handler_tools": "all",
    },
    "print-layout": {
        "domain_name": "print-layout",
        "handler_tools": "all",
    },
    "route-cartography": {"domain_name": "cartography", "handler_tools": "all"},
    "charts": {"domain_name": "misc-tools", "handler_tools": {"bar_chart"}},
    "mesh-analysis": {"domain_name": "misc-tools", "handler_tools": set()},
}


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[2]


def _load_yaml(path: Path) -> Dict[str, Any]:
    loaded = yaml.safe_load(path.read_text(encoding="utf-8"))
    return cast(Dict[str, Any], loaded)


def _load_skill_front_matter(path: Path) -> Dict[str, Any]:
    content = path.read_text(encoding="utf-8")
    if not content.startswith("---\n"):
        raise AssertionError(f"{path} 缺少 front matter")
    _, front_matter, _ = content.split("---\n", 2)
    loaded = yaml.safe_load(front_matter)
    return cast(Dict[str, Any], loaded)


def _load_tool_index(path: Path) -> Dict[str, Dict[str, Any]]:
    tool_index: Dict[str, Dict[str, Any]] = {}
    for tool_file in sorted(path.rglob("*.yaml")):
        definition = _load_yaml(tool_file)
        metadata = definition.get("metadata") or {}
        tool_name = str(metadata.get("name") or "").strip()
        if tool_name:
            if metadata.get("domain") != tool_file.parent.name:
                raise AssertionError(f"工具 domain 与定义目录不一致: {tool_file}")
            tool_index[tool_name] = definition
    return tool_index


class HandlerToolPackageMigrationTestCase(unittest.TestCase):
    def test_handler_packages_resolve_tool_refs_from_tools_tree(self):
        root = _repo_root()

        for package_name, package_expectation in HANDLER_PACKAGE_EXPECTATIONS.items():
            domain_name = cast(str, package_expectation["domain_name"])
            handler_tools = cast(
                Union[str, Set[str]], package_expectation["handler_tools"]
            )
            new_tools_dir = root / "autocarto" / "tools" / "system" / domain_name
            skill_md_path = (
                root / "autocarto" / "skills" / "system" / package_name / "SKILL.md"
            )

            self.assertTrue(
                new_tools_dir.is_dir(), f"缺少迁移后的工具目录: {new_tools_dir}"
            )

            front_matter = _load_skill_front_matter(skill_md_path)
            declared_tools = sorted(front_matter.get("tools") or [])
            # Tool ownership can span source directories; YAML locations stay unchanged.
            tool_index = _load_tool_index(root / "autocarto/tools/system")
            self.assertTrue(declared_tools, f"{skill_md_path} 未声明 tools")

            for tool_name in declared_tools:
                self.assertIn(
                    tool_name, tool_index, f"{package_name} 缺少工具定义: {tool_name}"
                )
                migrated_definition = tool_index[tool_name]
                self.assertEqual(migrated_definition.get("apiVersion"), "tool/v1")
                self.assertEqual(migrated_definition.get("kind"), "Tool")

                migrated_metadata = migrated_definition.get("metadata") or {}
                self.assertEqual(migrated_metadata.get("name"), tool_name)
                self.assertTrue(migrated_metadata.get("domain"))
                self.assertNotIn("category", migrated_metadata)

                migrated_execution = migrated_definition.get("execution") or {}
                is_handler_tool = handler_tools == "all" or tool_name in handler_tools
                if is_handler_tool:
                    handler_path = str(migrated_execution.get("handler") or "")
                    self.assertTrue(
                        handler_path.startswith("handlers."),
                        f"{tool_name} 应迁移为相对 handler wrapper，当前为: {handler_path}",
                    )
                    self.assertNotIn("backend_tool", migrated_execution)
                else:
                    self.assertTrue(
                        migrated_execution.get("backend_tool"),
                        f"{tool_name} 应保留 backend_tool 执行入口",
                    )
                    self.assertNotIn("handler", migrated_execution)


if __name__ == "__main__":
    unittest.main()
