import unittest
from pathlib import Path
from typing import Any, Dict, cast

import yaml


SAFE_PACKAGE_DOMAINS = {
    "build-capability-awareness": "build-capability-awareness",
    "creator-tools": "creator-tools",
    "crs-projection": "crs-projection",
    "data-conversion": "data-conversion",
    "file-operations": "file-operations",
    "gdal-processing": "gdal-processing",
    "geocoding": "geocoding",
    "gps-data": "gps-data",
    "grass-analysis": "grass-analysis",
    "network-analysis": "network-analysis",
    "point-cloud": "point-cloud",
    "raster-analysis": "raster-analysis",
    "spatial-analysis": "spatial-analysis",
    "vector-creation": "vector-creation",
    "vector-fields": "vector-fields",
    "vector-geometry": "vector-geometry",
    "runtime-control": "runtime-control",
}

VECTOR_FIELDS_CUSTOM_HANDLERS = {
    "field_calculator": "handlers.attributes.field_calculator_handler",
    "join_attributes_by_field_value": "handlers.attributes.join_attributes_by_field_value_handler",
    "rename_table_field": "handlers.attributes.rename_table_field_handler",
    "text_to_float": "handlers.attributes.text_to_float_handler",
    "retain_fields": "handlers.attributes.retain_fields_handler",
    "refactor_fields": "handlers.attributes.refactor_fields_handler",
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
    for tool_file in sorted(path.glob("*.yaml")):
        definition = _load_yaml(tool_file)
        metadata = definition.get("metadata") or {}
        tool_name = str(metadata.get("name") or "").strip()
        if tool_name:
            tool_index[tool_name] = definition
    return tool_index


class SafeToolPackageMigrationTestCase(unittest.TestCase):
    def test_safe_packages_use_tools_tree_and_skill_tool_refs(self):
        root = _repo_root()

        all_definitions = {}
        for folder in (root / "autocarto/tools/system").iterdir():
            if folder.is_dir():
                all_definitions.update(_load_tool_index(folder))
        all_references = set()
        for package in (root / "autocarto/skills/system").glob("*/SKILL.md"):
            all_references.update(_load_skill_front_matter(package).get("tools") or [])
        for package_name, domain_name in SAFE_PACKAGE_DOMAINS.items():
            new_tools_dir = root / "autocarto" / "tools" / "system" / domain_name
            skill_md_path = (
                root / "autocarto" / "skills" / "system" / package_name / "SKILL.md"
            )

            self.assertTrue(
                new_tools_dir.is_dir(), f"缺少迁移后的工具目录: {new_tools_dir}"
            )

            front_matter = _load_skill_front_matter(skill_md_path)
            declared_tools = sorted(front_matter.get("tools") or [])
            tool_index = _load_tool_index(new_tools_dir)
            # Packages may explicitly reuse a capability from another domain;
            # internal tools may belong to a hidden auto-mounted package.
            for name in declared_tools:
                self.assertIn(name, all_definitions, f"{package_name}: {name}")
            for name in tool_index:
                self.assertIn(name, all_references, f"没有工具包声明能力 {name}")

            for tool_name, definition in tool_index.items():
                self.assertEqual(definition.get("apiVersion"), "tool/v1")
                self.assertEqual(definition.get("kind"), "Tool")

                migrated_metadata = definition.get("metadata") or {}
                self.assertEqual(migrated_metadata.get("name"), tool_name)
                self.assertEqual(migrated_metadata.get("domain"), domain_name)
                self.assertNotIn("category", migrated_metadata)

                migrated_execution = definition.get("execution") or {}
                expected_handler = VECTOR_FIELDS_CUSTOM_HANDLERS.get(tool_name)
                if expected_handler:
                    self.assertEqual(
                        migrated_execution.get("handler"), expected_handler
                    )
                    self.assertNotIn("backend_tool", migrated_execution)
                else:
                    self.assertTrue(
                        migrated_execution.get("backend_tool")
                        or migrated_execution.get("handler"),
                        f"{tool_name} 缺少可执行入口",
                    )


if __name__ == "__main__":
    unittest.main()
