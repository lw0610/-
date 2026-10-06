import os
import shutil
import subprocess
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
SOURCE_BUILD_SH = REPO_ROOT / "build.sh"


class BuildPrebuildCleanupTestCase(unittest.TestCase):
    def test_check_cleans_local_artifacts_before_gate_execution(self):
        completed, repo_root = self._run_command("check")

        self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)
        self._assert_local_artifacts_removed(repo_root, expect_sidecar=True)
        self.assertFalse((repo_root / "dist").exists())

    def test_check_preserves_release_artifacts_needed_by_dist_layout_gate(self):
        completed, repo_root = self._run_command("check", seed_release_artifacts=True)

        self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)
        self.assertTrue(
            (repo_root / "dist" / "AutoCartoAI-0.1.0-linux-amd64.AppImage").exists()
        )
        self.assertTrue(
            (repo_root / "dist" / "AutoCartoAI-0.1.0-linux-amd64.deb").exists()
        )
        self.assertTrue(
            list((repo_root / "dist").glob("autocarto-plugin-*.zip")),
            "missing preserved plugin archive under dist/",
        )
        self.assertTrue(
            (
                repo_root
                / "autocarto-web"
                / "src-tauri"
                / "target"
                / "release"
                / "bundle"
                / "appimage"
                / "AutoCartoAI.AppDir"
                / "usr"
                / "bin"
                / "carto-agent"
            ).exists()
        )
        self.assertTrue(
            (
                repo_root
                / "autocarto-web"
                / "src-tauri"
                / "target"
                / "release"
                / "bundle"
                / "deb"
                / "autocarto-ai"
                / "data"
                / "usr"
                / "lib"
                / "autocarto"
                / "autocarto"
                / "tools"
                / "system"
                / "sample"
                / "tool.yaml"
            ).exists()
        )
        self.assertFalse((repo_root / "dist" / "stale-dist.txt").exists())

    def test_go_preserves_other_artifacts_during_sidecar_build(self):
        completed, repo_root = self._run_command("go")

        self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)
        for relative_path in (
            "autocarto-web/.next/cache/stale.txt",
            "autocarto-web/out/stale.txt",
            "autocarto-web/src-tauri/target/stale.txt",
            "autocarto-web/src-tauri/binaries/stale-sidecar",
            "dist/stale-dist.txt",
            "autocarto/bin/stale.bin",
            "autocarto/logs/stale.log",
            "autocarto/.session/stale.session",
        ):
            self.assertEqual((repo_root / relative_path).read_text(), "stale\n")
        binaries_dir = repo_root / "autocarto-web" / "src-tauri" / "binaries"
        self.assertTrue(list(binaries_dir.glob("carto-agent-*")))

    def test_plugin_cleans_local_artifacts_before_packaging(self):
        completed, repo_root = self._run_command("plugin")

        self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)
        self._assert_local_artifacts_removed(repo_root)
        self.assertFalse((repo_root / "dist" / "stale-dist.txt").exists())
        self.assertTrue(
            list((repo_root / "dist").glob("autocarto-plugin-*.zip")),
            "missing plugin archive under dist/",
        )

    def _run_command(
        self, command_name: str, seed_release_artifacts: bool = False
    ) -> tuple[subprocess.CompletedProcess[str], Path]:
        with tempfile.TemporaryDirectory() as tmp_dir:
            repo_root = Path(tmp_dir)
            _ = shutil.copy2(SOURCE_BUILD_SH, repo_root / "build.sh")
            self._seed_repo_layout(repo_root)
            self._seed_local_artifacts(repo_root)
            if seed_release_artifacts:
                self._seed_release_artifacts(repo_root)

            fake_log = repo_root / "fake-commands.log"
            fake_bin = repo_root / "fake-bin"
            fake_bin.mkdir(parents=True, exist_ok=True)
            self._write_fake_commands(fake_bin, fake_log)

            env = os.environ.copy()
            env["PATH"] = f"{fake_bin}{os.pathsep}{env.get('PATH', '')}"
            env["AUTOCARTO_FAKE_LOG"] = str(fake_log)
            _ = env.pop("AUTOCARTO_LIVE_E2E", None)
            _ = env.pop("AUTOCARTO_PACKAGED_SMOKE", None)
            _ = env.pop("AUTOCARTO_PACKAGED_SMOKE_TIMEOUT_SEC", None)

            completed = subprocess.run(
                ["bash", "./build.sh", command_name],
                cwd=repo_root,
                env=env,
                capture_output=True,
                text=True,
                check=False,
            )
            repo_snapshot = Path(tempfile.mkdtemp(prefix="autocarto-build-cleanup-"))
            self.addCleanup(shutil.rmtree, repo_snapshot, ignore_errors=True)
            _ = shutil.copytree(repo_root, repo_snapshot, dirs_exist_ok=True)
            return completed, repo_snapshot

    def _seed_repo_layout(self, repo_root: Path) -> None:
        _ = (repo_root / ".sisyphus" / "evidence").mkdir(parents=True, exist_ok=True)

        agent_dir = repo_root / "carto_agent"
        _ = agent_dir.mkdir(parents=True, exist_ok=True)
        _ = (agent_dir / "go.mod").write_text(
            "module example.com/test\n\ngo 1.24.0\n", encoding="utf-8"
        )

        web_dir = repo_root / "autocarto-web"
        _ = (web_dir / "src-tauri" / "target").mkdir(parents=True, exist_ok=True)
        _ = (web_dir / "src-tauri" / "binaries").mkdir(parents=True, exist_ok=True)
        _ = (web_dir / "node_modules" / ".bin").mkdir(parents=True, exist_ok=True)
        _ = (web_dir / "tests" / "e2e").mkdir(parents=True, exist_ok=True)
        _ = (web_dir / "package-lock.json").write_text("{}\n", encoding="utf-8")
        _ = (web_dir / "src-tauri" / "Cargo.toml").write_text(
            '[package]\nname = "fake"\nversion = "0.1.0"\n',
            encoding="utf-8",
        )
        _ = (web_dir / "src-tauri" / "tauri.conf.json").write_text(
            '{"version":"0.1.0","productName":"AutoCartoAI"}\n',
            encoding="utf-8",
        )

        next_bin = web_dir / "node_modules" / ".bin" / "next"
        _ = next_bin.write_text(
            '#!/bin/bash\nprintf \'%s\\n\' "next $*" >> "$AUTOCARTO_FAKE_LOG"\n',
            encoding="utf-8",
        )
        _ = next_bin.chmod(0o755)

        tauri_bin = web_dir / "node_modules" / ".bin" / "tauri"
        _ = tauri_bin.write_text(
            '#!/bin/bash\nprintf \'%s\\n\' "tauri $*" >> "$AUTOCARTO_FAKE_LOG"\nexit 0\n',
            encoding="utf-8",
        )
        _ = tauri_bin.chmod(0o755)

        playwright_bin = web_dir / "node_modules" / ".bin" / "playwright"
        _ = playwright_bin.write_text(
            '#!/bin/bash\nprintf \'%s\\n\' "playwright $*" >> "$AUTOCARTO_FAKE_LOG"\n',
            encoding="utf-8",
        )
        _ = playwright_bin.chmod(0o755)

        _ = (repo_root / "autocarto").mkdir(parents=True, exist_ok=True)
        _ = (repo_root / "autocarto" / "metadata.txt").write_text(
            "name=AutoCartoAI\nversion=0.1.0\n",
            encoding="utf-8",
        )
        _ = (repo_root / "autocarto" / "tool_catalog_client.py").write_text(
            "VERSION = 'new-source'\n",
            encoding="utf-8",
        )
        _ = (repo_root / "autocarto" / "tests").mkdir(parents=True, exist_ok=True)

    def _seed_local_artifacts(self, repo_root: Path) -> None:
        _ = (repo_root / "autocarto-web" / ".next" / "cache").mkdir(
            parents=True, exist_ok=True
        )
        _ = (repo_root / "autocarto-web" / ".next" / "cache" / "stale.txt").write_text(
            "stale\n",
            encoding="utf-8",
        )
        _ = (repo_root / "autocarto-web" / "out").mkdir(parents=True, exist_ok=True)
        _ = (repo_root / "autocarto-web" / "out" / "stale.txt").write_text(
            "stale\n",
            encoding="utf-8",
        )
        _ = (
            repo_root / "autocarto-web" / "src-tauri" / "target" / "stale.txt"
        ).write_text(
            "stale\n",
            encoding="utf-8",
        )
        _ = (
            repo_root / "autocarto-web" / "src-tauri" / "binaries" / "stale-sidecar"
        ).write_text(
            "stale\n",
            encoding="utf-8",
        )
        _ = (repo_root / "dist").mkdir(parents=True, exist_ok=True)
        _ = (repo_root / "dist" / "stale-dist.txt").write_text(
            "stale\n",
            encoding="utf-8",
        )
        _ = (repo_root / "autocarto" / "bin").mkdir(parents=True, exist_ok=True)
        _ = (repo_root / "autocarto" / "bin" / "stale.bin").write_text(
            "stale\n",
            encoding="utf-8",
        )
        _ = (repo_root / "autocarto" / "logs").mkdir(parents=True, exist_ok=True)
        _ = (repo_root / "autocarto" / "logs" / "stale.log").write_text(
            "stale\n",
            encoding="utf-8",
        )
        _ = (repo_root / "autocarto" / ".session").mkdir(parents=True, exist_ok=True)
        _ = (repo_root / "autocarto" / ".session" / "stale.session").write_text(
            "stale\n",
            encoding="utf-8",
        )

    def _seed_release_artifacts(self, repo_root: Path) -> None:
        dist_dir = repo_root / "dist"
        dist_dir.mkdir(parents=True, exist_ok=True)
        _ = (dist_dir / "AutoCartoAI-0.1.0-linux-amd64.AppImage").write_text(
            "packaged appimage\n",
            encoding="utf-8",
        )
        _ = (dist_dir / "AutoCartoAI-0.1.0-linux-amd64.deb").write_text(
            "packaged deb\n",
            encoding="utf-8",
        )
        _ = (dist_dir / "autocarto-plugin-0.1.0-linux-amd64.zip").write_text(
            "plugin zip\n",
            encoding="utf-8",
        )

        bundle_dir = (
            repo_root / "autocarto-web" / "src-tauri" / "target" / "release" / "bundle"
        )
        appimage_root = bundle_dir / "appimage" / "AutoCartoAI.AppDir" / "usr" / "bin"
        appimage_root.mkdir(parents=True, exist_ok=True)
        _ = (appimage_root / "carto-agent").write_text("sidecar\n", encoding="utf-8")

        skills_root = (
            bundle_dir
            / "deb"
            / "autocarto-ai"
            / "data"
            / "usr"
            / "lib"
            / "autocarto"
            / "autocarto"
            / "skills"
            / "system"
            / "sample"
        )
        skills_root.mkdir(parents=True, exist_ok=True)
        _ = (skills_root / "SKILL.md").write_text("skill\n", encoding="utf-8")

        tools_root = (
            bundle_dir
            / "deb"
            / "autocarto-ai"
            / "data"
            / "usr"
            / "lib"
            / "autocarto"
            / "autocarto"
            / "tools"
            / "system"
            / "sample"
        )
        tools_root.mkdir(parents=True, exist_ok=True)
        _ = (tools_root / "tool.yaml").write_text("tool: sample\n", encoding="utf-8")

    def _assert_local_artifacts_removed(
        self, repo_root: Path, expect_sidecar: bool = False
    ) -> None:
        self.assertFalse((repo_root / "autocarto-web" / ".next").exists())
        self.assertFalse((repo_root / "autocarto-web" / "out").exists())
        self.assertFalse(
            (repo_root / "autocarto-web" / "src-tauri" / "target").exists()
        )
        self.assertFalse((repo_root / "autocarto" / "bin").exists())
        self.assertFalse((repo_root / "autocarto" / "logs").exists())
        self.assertFalse((repo_root / "autocarto" / ".session").exists())

        binaries_dir = repo_root / "autocarto-web" / "src-tauri" / "binaries"
        if expect_sidecar:
            self.assertTrue(
                list(binaries_dir.glob("carto-agent-*")),
                f"missing rebuilt sidecar under {binaries_dir}",
            )
            self.assertFalse((binaries_dir / "stale-sidecar").exists())
        else:
            self.assertFalse(binaries_dir.exists())

    def _write_fake_commands(self, fake_bin: Path, fake_log: Path) -> None:
        _ = fake_log.write_text("", encoding="utf-8")

        self._write_executable(
            fake_bin / "go",
            textwrap.dedent(
                f"""#!/bin/bash
if [ "${{1:-}}" = "version" ]; then
  printf 'go version go1.24.11 linux/amd64\n'
  exit 0
fi
output=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    output="$arg"
    break
  fi
  prev="$arg"
done
if [ -n "$output" ]; then
  mkdir -p "$(dirname "$output")"
  printf 'fake binary\n' > "$output"
fi
printf '%s\n' "go $*" >> "$AUTOCARTO_FAKE_LOG"
"""
            ),
        )
        self._write_executable(fake_bin / "node", "#!/bin/bash\nexit 0\n")
        self._write_executable(
            fake_bin / "npm",
            '#!/bin/bash\nprintf \'%s\\n\' "npm $*" >> "$AUTOCARTO_FAKE_LOG"\n',
        )
        self._write_executable(fake_bin / "rustc", "#!/bin/bash\nexit 0\n")
        self._write_executable(
            fake_bin / "cargo",
            '#!/bin/bash\nprintf \'%s\\n\' "cargo $*" >> "$AUTOCARTO_FAKE_LOG"\n',
        )
        self._write_executable(fake_bin / "cc", "#!/bin/bash\nexit 0\n")
        self._write_executable(fake_bin / "pkg-config", "#!/bin/bash\nexit 0\n")
        self._write_executable(
            fake_bin / "python",
            textwrap.dedent(
                f"""#!/bin/bash
REAL_PYTHON={sys.executable}
if [ "${{1:-}}" = "-m" ] && [ "${{2:-}}" = "pytest" ]; then
  printf '%s\n' "python $*" >> "$AUTOCARTO_FAKE_LOG"
  exit 0
fi
exec "$REAL_PYTHON" "$@"
"""
            ),
        )

    def _write_executable(self, path: Path, content: str) -> None:
        _ = path.write_text(content, encoding="utf-8")
        _ = path.chmod(0o755)


if __name__ == "__main__":
    _ = unittest.main()
