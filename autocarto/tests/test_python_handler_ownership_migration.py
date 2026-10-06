from pathlib import Path
import unittest


REPO_ROOT = Path(__file__).resolve().parents[2]
TOOLS_ROOT = REPO_ROOT / "autocarto" / "tools" / "system"
TARGET_PATTERNS = (
    "_shared/*.py",
    "cartography/handlers/*.py",
    "print-layout/handlers/*.py",
    "project-state/handlers/*.py",
    "canvas-navigation/handlers/*.py",
    "vector-selection/handlers/*.py",
    "data-loading/handlers/*.py",
    "pyqgis-scripting/handlers/*.py",
    "misc-tools/handlers/*.py",
    "visibility-analysis/handlers/*.py",
    "vector-fields/handlers/*.py",
)
LEGACY_TOOL_ENGINE_PREFIX = "qgis" + "_tool_" + "server."


class PythonHandlerOwnershipMigrationTestCase(unittest.TestCase):
    def test_target_handlers_do_not_import_legacy_tool_engine_modules(self):
        offending_files = []

        for pattern in TARGET_PATTERNS:
            for file_path in sorted(TOOLS_ROOT.glob(pattern)):
                source = file_path.read_text(encoding="utf-8")
                if LEGACY_TOOL_ENGINE_PREFIX in source:
                    offending_files.append(str(file_path.relative_to(REPO_ROOT)))

        self.assertEqual(offending_files, [])


if __name__ == "__main__":
    unittest.main()
