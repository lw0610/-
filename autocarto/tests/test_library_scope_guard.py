"""The shipped public library contains only declared reusable templates/styles."""
from pathlib import Path
import tempfile
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[2]
FORMATS = ROOT / "carto_agent/internal/resource/formats/resource_formats.yaml"
PUBLIC = {"layout", "style"}


def violations(library):
    errors, owned = [], set()
    manifests = sorted(set(library.rglob("resource.yaml")) | set(library.rglob("*.resource.yaml")))
    for manifest in manifests:
        data = yaml.safe_load(manifest.read_text(encoding="utf-8")) or {}
        owned.add(manifest)
        if data.get("type") not in PUBLIC:
            errors.append(f"{manifest.relative_to(library)}: non-public type {data.get('type')}")
            continue
        entry = (manifest.parent / str(data.get("entry", ""))).resolve()
        if not entry.is_relative_to(manifest.parent.resolve()) or not entry.is_file():
            errors.append(f"{manifest.relative_to(library)}: invalid entry")
            continue
        if manifest.name == "resource.yaml":
            # Managed directories own their main file, preview, and supporting assets.
            owned.update(path for path in manifest.parent.rglob("*") if path.is_file())
        else:
            owned.add(entry)
    for path in sorted(library.rglob("*")):
        if path.is_file() and path.name != "README.md" and path.resolve() not in {p.resolve() for p in owned}:
            errors.append(f"{path.relative_to(library)}: resource has no public manifest")
    return errors


class LibraryScopeGuardTest(unittest.TestCase):
    def test_public_contract(self):
        formats = yaml.safe_load(FORMATS.read_text(encoding="utf-8"))
        self.assertEqual({key for key, value in formats["types"].items() if value.get("public")}, PUBLIC)

    def test_shipped_library(self):
        self.assertEqual(violations(ROOT / "autocarto/resources/system"), [])

    def test_detects_retired_manifest_and_unowned_files(self):
        with tempfile.TemporaryDirectory() as directory:
            library = Path(directory)
            (library / "resource.yaml").write_text("type: route_style\nentry: routes.csv\n")
            (library / "routes.csv").write_text("private route parameters")
            self.assertTrue(any("non-public type" in problem for problem in violations(library)))
            (library / "resource.yaml").unlink()
            self.assertTrue(any("no public manifest" in problem for problem in violations(library)))

    def test_rejects_entry_escape(self):
        with tempfile.TemporaryDirectory() as directory:
            library = Path(directory) / "library"
            library.mkdir()
            (library.parent / "outside.qml").write_text("<qgis/>")
            (library / "resource.yaml").write_text("type: style\nentry: ../outside.qml\n")
            self.assertTrue(any("invalid entry" in problem for problem in violations(library)))


if __name__ == "__main__":
    unittest.main()
