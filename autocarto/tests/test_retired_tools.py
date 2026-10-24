"""被下线的工具和模块不再被目录扫描或旧 handler 导出重新发现。"""
import ast
import importlib.util
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]


def test_retired_tools_have_no_definition_package_owner_or_module():
    retired = {"cartography_build_and_draw_from_tables", "layout_add_file_overview"}
    definitions = set()
    for path in (ROOT / "tools/system").rglob("*.yaml"):
        definition = yaml.safe_load(path.read_text(encoding="utf-8-sig"))
        # Tool packages also contain resource manifests and other YAML assets.
        # Classify by the declared kind, not by their location or file suffix.
        if not isinstance(definition, dict) or definition.get("kind") not in {"Tool", "ToolPackage"}:
            continue
        metadata = definition.get("metadata")
        assert isinstance(metadata, dict), f"工具定义缺少 metadata: {path}"
        name = metadata.get("name")
        assert isinstance(name, str) and name.strip(), f"工具定义缺少名称: {path}"
        definitions.add(name)
    assert definitions, "系统工具定义扫描结果不能为空"
    assert definitions.isdisjoint(retired), definitions & retired
    for path in (ROOT/"skills/system").glob("*/SKILL.md"):
        front = yaml.safe_load(path.read_text().split("---", 2)[1])
        assert retired.isdisjoint(front.get("tools", [])), path
    for name in ("cartography_style_impl", "layout_file_overview_impl"):
        assert importlib.util.find_spec("autocarto.tools.system._shared."+name) is None
    for relative in ("cartography/handlers/__init__.py", "cartography/handlers/style.py",
                     "print-layout/handlers/__init__.py", "print-layout/handlers/layout_items.py"):
        tree = ast.parse((ROOT/"tools/system"/relative).read_text())
        exports = next(node.value for node in tree.body if isinstance(node, ast.Assign)
                       and any(isinstance(target, ast.Name) and target.id == "__all__" for target in node.targets))
        assert {"cartography_build_and_draw_from_tables_handler", "add_file_overview_to_layout_handler"}.isdisjoint(ast.literal_eval(exports))
