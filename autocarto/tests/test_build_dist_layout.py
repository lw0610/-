import unittest
import json
import subprocess
import sys
import tempfile
import zipfile
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
    def test_private_tool_assets_are_shipped_without_extension_filtering(self):
        from autocarto.tools.system._shared.tool_assets import tool_asset_path
        required = [tool_asset_path("admin-boundaries", "中国行政区边界", "LICENSE"),
                    tool_asset_path("admin-boundaries", "中国行政区边界", "data", "china_province.geojson"),
                    tool_asset_path("route-cartography", "renderer-defaults.json"),
                    tool_asset_path("cartography", "basemap-legacy-presets.json")]
        self.assertTrue(all(path.is_file() for path in required))
        config = json.loads((REPO_ROOT / "autocarto-web/src-tauri/tauri.conf.json").read_text())
        self.assertEqual(config["bundle"]["resources"]["../../autocarto/tools/"], "autocarto/tools/")

    def test_plugin_archive_excludes_runtime_and_preserves_tool_assets(self):
        shell = (REPO_ROOT / "build.sh").read_text()
        function = shell.split("build_plugin_archive() {", 1)[1]
        script = function.split("<<'PY'", 1)[1].split("\n", 1)[1].split("\nPY\n", 1)[0]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            plugin = root / "autocarto"
            retained = ["__init__.py", "tools/admin/assets/data/boundary.geojson", "tools/admin/assets/LICENSE"]
            excluded = ["data/instances.db", "data/runtime/state.json", "config/.master_key", "config/secrets.enc", "config/plugin_config.yaml", "symbology-style.db", "instances.db-wal", ".port"]
            for name in retained + excluded:
                target = plugin / name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text("test fixture", encoding="utf-8")
            archive = root / "plugin.zip"
            result = subprocess.run([sys.executable, "-c", script, str(root), str(archive)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            with zipfile.ZipFile(archive) as bundle:
                self.assertEqual(set(bundle.namelist()), {"autocarto/" + name for name in retained})

    def test_check_does_not_clean_and_explicit_cleanup_can_preserve_runtime(self):
        import shlex
        shell = (REPO_ROOT / "build.sh").read_text()
        cleanup = "clean_local_artifacts() {" + shell.split("clean_local_artifacts() {", 1)[1].split("\n}", 1)[0] + "\n}"
        check_branch = shell.rsplit("    check)", 1)[1].split(";;", 1)[0]
        self.assertNotIn("clean_local_artifacts", check_branch)
        self.assertIn("run_check", check_branch)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            plugin = root / "autocarto"
            markers = [plugin / "logs" / "keep.log", plugin / ".session" / "keep.json"]
            for marker in markers:
                marker.parent.mkdir(parents=True, exist_ok=True)
                marker.write_text("synthetic runtime fixture", encoding="utf-8")
            variables = {"PLUGIN_DIR": plugin, "TAURI_DIR": root / "tauri", "WEB_DIR": root / "web", "DIST_DIR": root / "dist", "BINARIES_DIR": root / "binaries"}
            script = "\n".join(f"{key}={shlex.quote(str(value))}" for key, value in variables.items())
            script += "\nstep() { :; }\ninfo() { :; }\nok() { :; }\npreserve_release_artifacts_snapshot() { :; }\nrestore_release_artifacts_snapshot() { :; }\n"
            script += cleanup + "\nclean_local_artifacts 1 1\n"
            result = subprocess.run(["bash", "-c", script], capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            for marker in markers:
                self.assertEqual(marker.read_text(), "synthetic runtime fixture")

    def test_desktop_manifest_includes_flow_packages(self):
        config = json.loads((REPO_ROOT / "autocarto-web/src-tauri/tauri.conf.json").read_text())
        resources = config["bundle"]["resources"]
        self.assertEqual(resources["../../autocarto/flows/"], "autocarto/flows/")
        self.assertTrue((REPO_ROOT / "autocarto/flows/system").is_dir())

    def test_desktop_manifest_includes_resource_library(self):
        config = json.loads((REPO_ROOT / "autocarto-web/src-tauri/tauri.conf.json").read_text())
        self.assertEqual(
            config["bundle"]["resources"]["../../autocarto/resources/system/"],
            "autocarto/resources/system/",
        )
        # The public library contains only layouts/styles since v3. Retired
        # data/symbol/docs groups can be empty locally and are not tracked by Git.
        for group in ("styles", "layouts"):
            self.assertTrue((REPO_ROOT / "autocarto/resources/system" / group).is_dir())

    def test_build_scripts_require_flow_and_resource_assets(self):
        shell = (REPO_ROOT / "build.sh").read_text()
        windows = (REPO_ROOT / "build.bat").read_text()
        for suffix in ("flows/system", "resources/system"):
            self.assertIn(f'require_dir "$PLUGIN_DIR/{suffix}"', shell)
            self.assertIn(f'%PLUGIN_DIR%\\{suffix.replace("/", chr(92))}', windows)

    @unittest.skipUnless(DIST_DIR.exists(), "需先构建发行产物，再执行发行包验收")
    def test_dist_layout_contains_desktop_artifacts_and_plugin_archive(self):
        desktop_artifacts = sorted(DIST_DIR.glob("*.deb")) + sorted(
            DIST_DIR.glob("*.AppImage")
        )
        plugin_archives = sorted(DIST_DIR.glob("autocarto-plugin-*.zip"))

        self.assertTrue(desktop_artifacts, "missing desktop artifacts under dist/")
        self.assertTrue(plugin_archives, "missing plugin archive under dist/")

    @unittest.skipUnless(_iter_bundle_roots(), "需先构建 Linux 发行包，才有 bundle staging")
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
        self.assertTrue(
            any((root / "autocarto/flows/system").is_dir() or _has_marker(root, "autocarto/flows/system/**/FLOW.md") for root in bundle_roots),
            "desktop bundle is missing flow package assets",
        )
        self.assertTrue(
            any(_has_marker(root, "autocarto/resources/system/README.md")
                or (root / "autocarto/resources/system").is_dir()
                for root in bundle_roots),
            "桌面发行包缺少内置资源库",
        )

    @unittest.skipUnless(_iter_bundle_roots(), "需先构建 Linux 发行包，才有 bundle staging")
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
