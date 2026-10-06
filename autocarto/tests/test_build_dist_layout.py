import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
DIST_DIR = REPO_ROOT / "dist"
TAURI_BUNDLE_DIR = (
    REPO_ROOT / "autocarto-web" / "src-tauri" / "target" / "release" / "bundle"
)


def _iter_bundle_roots() -> list[Path]:
    roots: list[Path] = []
    roots.extend(sorted((TAURI_BUNDLE_DIR / "deb").glob("*/data")))
    roots.extend(sorted((TAURI_BUNDLE_DIR / "appimage").glob("*.AppDir")))
    return [root for root in roots if root.exists()]


def _has_marker(root: Path, marker: str) -> bool:
    return any(path.is_file() for path in root.rglob(marker))


class BuildDistLayoutTestCase(unittest.TestCase):
    def test_dist_layout_contains_desktop_artifacts_and_plugin_archive(self):
        desktop_artifacts = sorted(DIST_DIR.glob("*.deb")) + sorted(
            DIST_DIR.glob("*.AppImage")
        )
        plugin_archives = sorted(DIST_DIR.glob("autocarto-plugin-*.zip"))

        self.assertTrue(desktop_artifacts, "missing desktop artifacts under dist/")
        self.assertTrue(plugin_archives, "missing plugin archive under dist/")

    def test_dist_layout_bundles_sidecar_and_runtime_assets(self):
        bundle_roots = _iter_bundle_roots()
        self.assertTrue(
            bundle_roots, f"missing bundle staging directories under {TAURI_BUNDLE_DIR}"
        )

        self.assertTrue(
            any(_has_marker(root, "carto-agent") for root in bundle_roots),
            "desktop bundle is missing carto-agent sidecar",
        )
        self.assertTrue(
            any(
                _has_marker(root, "autocarto/skills/system/**/SKILL.md")
                for root in bundle_roots
            ),
            "desktop bundle is missing autocarto/skills/system runtime assets",
        )
        self.assertTrue(
            any(
                _has_marker(root, "autocarto/tools/system/**/*.yaml")
                for root in bundle_roots
            ),
            "desktop bundle is missing autocarto/tools/system runtime assets",
        )

    def test_dist_layout_keeps_runtime_data_out_of_bundle(self):
        bundle_roots = _iter_bundle_roots()
        self.assertTrue(
            bundle_roots, f"missing bundle staging directories under {TAURI_BUNDLE_DIR}"
        )

        forbidden_markers = [
            ".port",
            "instances.db",
            "projects.yaml",
            "secrets.enc",
            ".master_key",
        ]
        for marker in forbidden_markers:
            self.assertFalse(
                any(_has_marker(root, marker) for root in bundle_roots),
                f"runtime writable data leaked into desktop bundle: {marker}",
            )


if __name__ == "__main__":
    _ = unittest.main()
