"""Resource tables and public library paths belong to the Go resolver."""

from pathlib import Path, PurePosixPath
import ast
import re
import tempfile
import unittest

REPO_ROOT = Path(__file__).resolve().parents[2]
PATTERNS = (
    "autocarto/tools/**/*.py", "autocarto/knowledge/**/*.py", "autocarto/core/**/*.py",
    "carto_agent/internal/**/*.go", "carto_agent/cmd/**/*.go",
)
LIBRARY_PATH = re.compile(r"resources[/\\]+system|[\"']resources[\"']\s*[,/+].{0,32}?[\"']system[\"']", re.S)
MAPPING_FILE = re.compile(r"[\"']对照表(?:\.(?:csv|xlsx))?[\"']", re.I)
# V31 creates a table from live QGIS layers. It must never read a resource table.
TABLE_WRITERS = set()
PRIVATE_PRESET_READER = "autocarto/tools/system/_shared/tool_presets.py"


def write_only_table_module(source):
    try:
        tree = ast.parse(source)
    except SyntaxError:
        return False
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        name = node.func.attr if isinstance(node.func, ast.Attribute) else getattr(node.func, "id", "")
        if name in {"reader", "DictReader", "load_workbook"} or name.startswith("read"):
            return False
        if name == "open":
            position = 0 if isinstance(node.func, ast.Attribute) else 1
            mode = next((kw.value for kw in node.keywords if kw.arg == "mode"), None)
            if mode is None and len(node.args) > position:
                mode = node.args[position]
            if not isinstance(mode, ast.Constant) or mode.value not in {"w", "wb", "x", "xb", "a", "ab"}:
                return False
    return True


def violations(root):
    result = []
    for path in sorted({p for pattern in PATTERNS for p in root.glob(pattern) if p.is_file()}):
        rel = PurePosixPath(path.relative_to(root).as_posix())
        if any(p in {"tests", "testdata", "__pycache__", "assets"} for p in rel.parts):
            continue
        if path.name.startswith("test_") or path.name.endswith("_test.go"):
            continue
        if rel.is_relative_to("carto_agent/internal/resource"):
            continue
        source = path.read_text(encoding="utf-8-sig")
        if LIBRARY_PATH.search(source):
            result.append(f"{rel}: 直接拼接公共资源库路径")
        if MAPPING_FILE.search(source) and str(rel) != PRIVATE_PRESET_READER and not (str(rel) in TABLE_WRITERS and write_only_table_module(source)):
            result.append(f"{rel}: 资源对照表文件名旁路")
        if path.suffix == ".py" and str(rel) != PRIVATE_PRESET_READER and "get_plugin_root(" in source and re.search(r"[\"']resources[\"']", source):
            result.append(f"{rel}: 插件根目录拼接资源路径")
    return result


class ResourceBypassGuardTest(unittest.TestCase):
    def test_retired_style_set_entry_points_are_absent(self):
        for path in ("tools/system/cartography/style_set_apply.yaml", "tools/system/cartography/style_set_export.yaml", "tools/system/cartography/handlers/style_sets.py", "tools/system/_shared/style_set_impl.py", "tools/system/_shared/style_set_export_impl.py"):
            self.assertFalse((REPO_ROOT / "autocarto" / path).exists(), path)
        for folder in (REPO_ROOT / "autocarto/tools", REPO_ROOT / "autocarto/knowledge"):
            for path in folder.rglob("*.py"):
                self.assertNotIn("autocarto.tests.support.legacy_style", path.read_text())

    def test_production_uses_resolver(self):
        self.assertEqual(violations(REPO_ROOT), [])

    def test_detects_python_and_go_bypasses(self):
        cases = {
            "autocarto/tools/system/example/tool.py": 'path = get_plugin_root() / "resources" / "system"\n',
            "autocarto/knowledge/table.py": 'read_table("对照表.xlsx")\n',
            "autocarto/core/path.py": 'path = "resources/system/styles"\n',
            "carto_agent/internal/example/tool.go": 'filepath.Join(root, "resources", "system")\n',
            "carto_agent/cmd/example/tool.go": 'const table = "对照表.csv"\n',
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name, content in cases.items():
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content, encoding="utf-8")
            found = violations(root)
            self.assertTrue(all(any(item.startswith(name+":") for item in found) for name in cases))

    def test_resolver_assets_and_runtime_dataset_tables_are_allowed(self):
        cases = {
            "carto_agent/internal/resource/table.go": 'const table = "对照表.csv"\n',
            "autocarto/tools/system/example/assets/generator.py": 'write("对照表.csv")\n',
            "autocarto/tools/system/example/handler.py": 'read_table("symbol_mapping.csv")\n',
            "autocarto/core/tests/test_table.py": 'read_table("对照表.csv")\n',
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name, content in cases.items():
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content, encoding="utf-8")
            self.assertEqual(violations(root), [])


if __name__ == "__main__":
    unittest.main()
