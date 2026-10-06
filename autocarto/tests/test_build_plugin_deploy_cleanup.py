import os
import shutil
import subprocess
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path
from typing import Optional


REPO_ROOT = Path(__file__).resolve().parents[2]
SOURCE_BUILD_SH = REPO_ROOT / "build.sh"


class BuildPluginDeployCleanupTestCase(unittest.TestCase):
    maxDiff: Optional[int] = None

    def test_build_deploy_plugin_replaces_installed_plugin_directory(self):
        completed, target_plugin_dir = self._run_build_command("build")

        self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)

        self._assert_plugin_directory_replaced(target_plugin_dir)

    def test_dev_deploy_plugin_replaces_installed_plugin_directory(self):
        completed, target_plugin_dir = self._run_build_command("dev")

        self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)

        self._assert_plugin_directory_replaced(target_plugin_dir)

    def _assert_plugin_directory_replaced(self, target_plugin_dir: Path) -> None:
        deployed_source = target_plugin_dir / "tool_catalog_client.py"
        stale_pyc = (
            target_plugin_dir / "__pycache__" / "tool_catalog_client.cpython-310.pyc"
        )
        stale_legacy_only = target_plugin_dir / "legacy_only.py"

        self.assertTrue(deployed_source.exists(), deployed_source.as_posix())
        self.assertEqual(
            deployed_source.read_text(encoding="utf-8"), "VERSION = 'new-source'\n"
        )
        self.assertFalse(stale_pyc.exists(), stale_pyc.as_posix())
        self.assertFalse((target_plugin_dir / "__pycache__").exists())
        self.assertFalse(stale_legacy_only.exists(), stale_legacy_only.as_posix())
        self.assertFalse((target_plugin_dir / "logs").exists())
        self.assertFalse((target_plugin_dir / ".session").exists())

    def _run_build_command(self, command_name: str):
        temp_dir = tempfile.TemporaryDirectory()
        self.addCleanup(temp_dir.cleanup)

        sandbox_root = Path(temp_dir.name)
        repo_root = sandbox_root / "repo"
        repo_root.mkdir(parents=True, exist_ok=True)
        _ = shutil.copy2(SOURCE_BUILD_SH, repo_root / "build.sh")

        fake_log = repo_root / "fake-commands.log"
        fake_bin = repo_root / "fake-bin"
        fake_bin.mkdir(parents=True, exist_ok=True)
        self._write_fake_commands(fake_bin, fake_log)

        self._seed_repo_layout(repo_root)

        home_dir = sandbox_root / "home"
        qgis_plugins_dir = self._seed_qgis_profile(home_dir)
        target_plugin_dir = qgis_plugins_dir / "autocarto"
        self._seed_stale_target(target_plugin_dir)

        env = os.environ.copy()
        env["HOME"] = str(home_dir)
        env["PATH"] = f"{fake_bin}{os.pathsep}{env.get('PATH', '')}"
        env["AUTOCARTO_FAKE_LOG"] = str(fake_log)

        completed = subprocess.run(
            ["bash", "./build.sh", command_name],
            cwd=repo_root,
            env=env,
            capture_output=True,
            text=True,
            check=False,
        )
        return completed, target_plugin_dir

    def _seed_repo_layout(self, repo_root: Path) -> None:
        _ = (repo_root / ".sisyphus" / "evidence").mkdir(parents=True, exist_ok=True)

        agent_dir = repo_root / "carto_agent"
        _ = agent_dir.mkdir(parents=True, exist_ok=True)
        _ = (agent_dir / "go.mod").write_text(
            "module example.com/test\n\ngo 1.24.0\n", encoding="utf-8"
        )

        web_dir = repo_root / "autocarto-web"
        _ = (web_dir / "src-tauri").mkdir(parents=True, exist_ok=True)
        _ = (web_dir / "node_modules" / ".bin").mkdir(parents=True, exist_ok=True)
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
            "#!/bin/bash\nexit 0\n",
            encoding="utf-8",
        )
        _ = next_bin.chmod(0o755)

        tauri_bin = web_dir / "node_modules" / ".bin" / "tauri"
        _ = tauri_bin.write_text(
            "#!/bin/bash\nexit 0\n",
            encoding="utf-8",
        )
        _ = tauri_bin.chmod(0o755)

        _ = (repo_root / "autocarto").mkdir(parents=True, exist_ok=True)
        _ = (repo_root / "autocarto" / "metadata.txt").write_text(
            "name=AutoCartoAI\nversion=0.1.0\n",
            encoding="utf-8",
        )
        _ = (repo_root / "autocarto" / "tool_catalog_client.py").write_text(
            "VERSION = 'new-source'\n",
            encoding="utf-8",
        )

    def _seed_qgis_profile(self, home_dir: Path) -> Path:
        qgis_plugins_dir = (
            home_dir
            / ".local"
            / "share"
            / "QGIS"
            / "QGIS3"
            / "profiles"
            / "default"
            / "python"
            / "plugins"
        )
        _ = qgis_plugins_dir.mkdir(parents=True, exist_ok=True)
        return qgis_plugins_dir

    def _seed_stale_target(self, target_plugin_dir: Path) -> None:
        _ = (target_plugin_dir / "__pycache__").mkdir(parents=True, exist_ok=True)
        _ = (
            target_plugin_dir / "__pycache__" / "tool_catalog_client.cpython-310.pyc"
        ).write_text(
            "old bytecode\n",
            encoding="utf-8",
        )
        _ = (target_plugin_dir / "tool_catalog_client.py").write_text(
            "VERSION = 'old'\n",
            encoding="utf-8",
        )
        _ = (target_plugin_dir / "legacy_only.py").write_text(
            "LEGACY = True\n",
            encoding="utf-8",
        )
        _ = (target_plugin_dir / "logs").mkdir(parents=True, exist_ok=True)
        _ = (target_plugin_dir / "logs" / "keep.log").write_text(
            "keep\n",
            encoding="utf-8",
        )
        _ = (target_plugin_dir / ".session").mkdir(parents=True, exist_ok=True)
        _ = (target_plugin_dir / ".session" / "keep.session").write_text(
            "keep\n",
            encoding="utf-8",
        )

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
        self._write_executable(fake_bin / "npm", "#!/bin/bash\nexit 0\n")
        self._write_executable(fake_bin / "rustc", "#!/bin/bash\nexit 0\n")
        self._write_executable(fake_bin / "cargo", "#!/bin/bash\nexit 0\n")
        self._write_executable(fake_bin / "cc", "#!/bin/bash\nexit 0\n")
        self._write_executable(
            fake_bin / "pkg-config",
            "#!/bin/bash\nexit 0\n",
        )
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
        self._write_executable(
            fake_bin / "tauri",
            '#!/bin/bash\nprintf \'%s\\n\' "tauri $*" >> "$AUTOCARTO_FAKE_LOG"\nexit 0\n',
        )

    def _write_executable(self, path: Path, content: str) -> None:
        _ = path.write_text(content, encoding="utf-8")
        _ = path.chmod(0o755)


if __name__ == "__main__":
    _ = unittest.main()
