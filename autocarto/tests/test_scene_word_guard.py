"""Keep scenario-specific text within its designated content/executor boundary."""

from pathlib import Path, PurePosixPath
import hashlib
import tempfile
import unittest


REPO_ROOT = Path(__file__).resolve().parents[2]
DEBT_PATH = REPO_ROOT / "docs/refactor-v2/scene_word_debt.txt"
SCENE_WORDS = (
    "行军",
    "河南",
    "军标",
    "国军",
    "红军",
    "敌军",
    "会师",
    "China_map",
    "/Users/",
    r"F:\qgis_project",
    "RouteBezierPlotPlugin",
)
ENGLISH_IDENTIFIERS = ("military", "marching", "henan")
SCAN_PATTERNS = (
    "autocarto/knowledge/**/*.py",
    "autocarto/resources/**/*.csv",
    "autocarto/resources/**/*.qml",
    "autocarto/resources/**/*.yaml",
    "autocarto/resources/**/*.json",
    "autocarto/resources/**/*.md",
    "autocarto/core/**/*",
    "autocarto/tools/**/*.py",
    "autocarto/tools/**/*.yaml",
    "carto_agent/internal/**/*.go",
    "carto_agent/cmd/**/*.go",
    "carto_agent/defaults/agent_configs/**/agent.yaml",
)
EXCLUDED_ROOTS = (PurePosixPath("autocarto/flows"),)


def excluded_source(relative):
    if any(relative.is_relative_to(root) for root in EXCLUDED_ROOTS):
        return True
    if any(part in {"__pycache__", "testdata", "tests", "docs"} for part in relative.parts):
        return True
    if relative.suffix in {".pyc", ".pyo", ".rst"} or (relative.suffix == ".md" and not relative.is_relative_to("autocarto/resources")):
        return True
    if relative.name.endswith("_test.go"):
        return True
    if relative.suffix == ".py" and (
        relative.name.startswith("test_") or relative.name.endswith("_test.py")
    ):
        return True
    return False


def collect_violations(root):
    """Return one entry per repository-relative file and literal forbidden word."""
    candidates = {
        path
        for pattern in SCAN_PATTERNS
        for path in root.glob(pattern)
        if path.is_file()
    }
    violations = set()
    for path in sorted(candidates):
        relative = PurePosixPath(path.relative_to(root).as_posix())
        if excluded_source(relative):
            continue
        source = path.read_text(encoding="utf-8-sig")
        code_file = relative.suffix in {".py", ".go"} or (relative.is_relative_to("autocarto/tools") and relative.suffix in {".yaml", ".yml"})
        for line in source.splitlines():
            fingerprint = hashlib.sha256(line.strip().encode()).hexdigest()[:12]
            for word in SCENE_WORDS + (ENGLISH_IDENTIFIERS if code_file else ()):
                forms = (word, word.replace("\\", "\\\\"))
                found = word.lower() in line.lower() if word in ENGLISH_IDENTIFIERS else any(form in line for form in forms)
                if found:
                    violations.add(f"{relative}:{word}:{fingerprint}")
    return violations


def read_debt(path):
    entries = set()
    for number, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        entry, _, fingerprint = line.rpartition(":")
        filename, separator, word = entry.partition(":")
        relative = PurePosixPath(filename)
        if (
            not separator
            or relative.is_absolute()
            or ".." in relative.parts
            or word not in SCENE_WORDS + ENGLISH_IDENTIFIERS
            or len(fingerprint) != 12
            or any(c not in "0123456789abcdef" for c in fingerprint)
        ):
            raise AssertionError(f"无效场景词债务项 {path}:{number}: {line}")
        if line in entries:
            raise AssertionError(f"重复场景词债务项 {path}:{number}: {line}")
        entries.add(line)
    return entries


def assert_debt_matches_source(root, debt_path):
    actual = collect_violations(root)
    recorded = read_debt(debt_path)
    added = sorted(actual - recorded)
    stale = sorted(recorded - actual)
    messages = []
    if added:
        messages.append("新增场景词违规（请迁移到规范允许的位置）：\n" + "\n".join(added))
    if stale:
        messages.append("已消失的场景词债务（请同步删除清单项）：\n" + "\n".join(stale))
    if messages:
        raise AssertionError("\n\n".join(messages))


class SceneWordGuardTestCase(unittest.TestCase):
    def test_repository_matches_recorded_debt(self):
        assert_debt_matches_source(REPO_ROOT, DEBT_PATH)

    def test_new_tool_scene_word_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tool = root / "autocarto/tools/system/example/tool.yaml"
            tool.parent.mkdir(parents=True)
            tool.write_text("description: 行军\n", encoding="utf-8")
            debt = root / "debt.txt"
            debt.write_text("", encoding="utf-8")
            with self.assertRaisesRegex(AssertionError, "新增场景词违规"):
                assert_debt_matches_source(root, debt)

    def test_removed_debt_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            debt = root / "debt.txt"
            debt.write_text(
                "autocarto/tools/system/example/tool.yaml:行军:0123456789ab\n", encoding="utf-8"
            )
            with self.assertRaisesRegex(AssertionError, "已消失的场景词债务"):
                assert_debt_matches_source(root, debt)

    def test_only_flows_and_test_documents_are_excluded(self):
        paths = (
            "carto_agent/internal/example/example_test.go",
            "carto_agent/internal/example/testdata/example.go",
            "autocarto/core/tests/test_example.py",
            "autocarto/core/README.md",
            "autocarto/flows/example/FLOW.md",
        )
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for filename in paths:
                path = root / filename
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("行军 河南 军标\n", encoding="utf-8")
            self.assertEqual(collect_violations(root), set())

    def test_extension_and_public_resources_are_scanned(self):
        paths = (
            "autocarto/tools/system/marching-route-knowledge/tool.yaml",
            "carto_agent/internal/knowledge/executor.go",
            "carto_agent/internal/deepagent/knowledge_extract.go",
            "carto_agent/internal/config/agent_config_migrate_v1.go",
            "autocarto/knowledge/example.py",
            "autocarto/resources/system/styles/example.qml",
            "autocarto/resources/system/README.md",
        )
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for filename in paths:
                path = root / filename
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("行军 河南 军标\n", encoding="utf-8")
            self.assertEqual(len(collect_violations(root)), 3 * len(paths))

    def test_english_identifiers_ignore_case_only_in_code(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            code = root / "autocarto/tools/system/example/tool.yaml"
            resource = root / "autocarto/resources/system/example.csv"
            for path in (code, resource):
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("Military MARCHING Henan route_paths\n", encoding="utf-8")
            actual = collect_violations(root)
            self.assertEqual(len(actual), 3)
            self.assertTrue(all(item.startswith(code.relative_to(root).as_posix()+":") for item in actual))

    def test_new_line_in_existing_debt_file_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "autocarto/knowledge/example.py"
            source.parent.mkdir(parents=True)
            source.write_text('old = "行军"\n', encoding="utf-8")
            debt = root / "debt.txt"
            debt.write_text("\n".join(collect_violations(root)), encoding="utf-8")
            source.write_text(source.read_text()+'new = "行军"\n', encoding="utf-8")
            with self.assertRaisesRegex(AssertionError, "新增场景词违规"):
                assert_debt_matches_source(root, debt)

    def test_escaped_windows_path_is_detected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "carto_agent/internal/example/path.go"
            source.parent.mkdir(parents=True)
            source.write_text('const path = "F:\\\\qgis_project"\n', encoding="utf-8")
            self.assertEqual(len(collect_violations(root)), 1)


if __name__ == "__main__":
    unittest.main()
