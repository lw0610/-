from build_test_support import install_fake_build_platform_and_go
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


class BuildCheckGateTestCase(unittest.TestCase):
    maxDiff: Optional[int] = None

    def test_check_reports_headless_gate_and_live_skip_by_default(self):
        completed, log_lines = self._run_build_check()

        self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)
        self.assertIn("Wave 6 架构门禁（Task 10）", completed.stdout)
        self.assertIn(
            "本次运行证据：.sisyphus/evidence/task-10-build-check.txt",
            completed.stdout,
        )
        self.assertIn(
            "默认 check 证据：.sisyphus/evidence/task-10-build-check.txt",
            completed.stdout,
        )
        self.assertIn(
            "packaged check 证据：.sisyphus/evidence/task-10-build-check-packaged.txt",
            completed.stdout,
        )
        self.assertIn(
            "live check 证据：.sisyphus/evidence/task-10-build-check-live.txt",
            completed.stdout,
        )
        self.assertIn("headless Playwright：ENABLED（默认）", completed.stdout)
        self.assertIn(
            "packaged smoke：SKIPPED（原因：未设置 AUTOCARTO_PACKAGED_SMOKE=1）",
            completed.stdout,
        )
        self.assertIn(
            "live-QGIS：SKIPPED（原因：未设置 AUTOCARTO_LIVE_E2E=1）",
            completed.stdout,
        )
        self.assertIn("最终状态：PASS", completed.stdout)
        self.assertTrue(
            any(
                line.startswith("playwright test ")
                and "diagnostics-page.spec.ts" in line
                and "services-page.spec.ts" in line
                and "session-workspace.spec.ts" in line
                and "backend-reconnect.spec.ts" in line
                and "live-qgis-integration.spec.ts" not in line
                for line in log_lines
            ),
            log_lines,
        )

    def test_check_runs_live_qgis_gate_when_flag_enabled(self):
        completed, log_lines = self._run_build_check(live=True)

        self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)
        self.assertIn(
            "本次运行证据：.sisyphus/evidence/task-10-build-check-live.txt",
            completed.stdout,
        )
        self.assertIn("headless Playwright：ENABLED（默认）", completed.stdout)
        self.assertIn("live-QGIS：ENABLED（AUTOCARTO_LIVE_E2E=1）", completed.stdout)
        self.assertIn("最终状态：PASS", completed.stdout)
        self.assertFalse(
            "live-QGIS：SKIPPED（原因：未设置 AUTOCARTO_LIVE_E2E=1）"
            in completed.stdout,
            completed.stdout,
        )
        self.assertTrue(
            any(
                line.startswith("playwright test ")
                and "backend-reconnect.spec.ts" in line
                and "diagnostics-page.spec.ts" in line
                and "services-page.spec.ts" in line
                and "session-workspace.spec.ts" in line
                for line in log_lines
            ),
            log_lines,
        )
        self.assertTrue(
            any("live-qgis-integration.spec.ts" in line for line in log_lines),
            log_lines,
        )

    def test_check_runs_next_and_rust_in_disposable_tree(self):
        completed, log_lines = self._run_build_check(require_isolated_cargo=True)

        self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)
        cargo_line = next(line for line in log_lines if line.startswith("cargo check "))
        self.assertIn("/project/autocarto-web/src-tauri/Cargo.toml", cargo_line)
        self.assertFalse(any(line.startswith("go build ") and "-o" in line for line in log_lines), log_lines)
        self.assertLess(log_lines.index("next build"), next(i for i, line in enumerate(log_lines) if line.startswith("playwright test ")))
        # Actual cargo executes this synchronous sidecar builder in the copied
        # manifest. A shell-side build in the original directory is unnecessary.
        rust_builder = (REPO_ROOT / "autocarto-web/src-tauri/build.rs").read_text()
        self.assertIn('manifest.join("../scripts/build-agent.mjs")', rust_builder)
        self.assertIn('tauri_build::build()', rust_builder)

    def _run_build_check(
        self, live: bool = False, require_isolated_cargo: bool = False
    ):
        with tempfile.TemporaryDirectory() as tmp_dir:
            repo_root = Path(tmp_dir)
            _ = shutil.copy2(SOURCE_BUILD_SH, repo_root / "build.sh")
            self._seed_repo_layout(repo_root)

            fake_log = repo_root / "fake-commands.log"
            fake_bin = repo_root / "fake-bin"
            fake_bin.mkdir(parents=True, exist_ok=True)
            self._write_fake_commands(fake_bin, fake_log)

            env = os.environ.copy()
            env["PYTHON_BIN"] = str(fake_bin / "python")
            env["PATH"] = f"{fake_bin}{os.pathsep}{env.get('PATH', '')}"
            env["AUTOCARTO_FAKE_LOG"] = str(fake_log)
            env["AUTOCARTO_FAKE_REPO"] = str(repo_root)
            _ = env.pop("AUTOCARTO_PACKAGED_SMOKE", None)
            _ = env.pop("AUTOCARTO_PACKAGED_SMOKE_TIMEOUT_SEC", None)
            if require_isolated_cargo:
                env["AUTOCARTO_REQUIRE_ISOLATED_CARGO"] = "1"
            else:
                _ = env.pop("AUTOCARTO_REQUIRE_ISOLATED_CARGO", None)
            if live:
                env["AUTOCARTO_LIVE_E2E"] = "1"
            else:
                _ = env.pop("AUTOCARTO_LIVE_E2E", None)

            completed = subprocess.run(
                ["bash", "./build.sh", "check"],
                cwd=repo_root,
                env=env,
                capture_output=True,
                text=True,
                check=False,
            )
            log_lines = fake_log.read_text(encoding="utf-8").splitlines()
            return completed, log_lines

    def _seed_repo_layout(self, repo_root: Path) -> None:
        for asset in ("flows/system", "resources/system"):
            (repo_root / "autocarto" / asset).mkdir(parents=True, exist_ok=True)
        _ = (repo_root / ".sisyphus" / "evidence").mkdir(parents=True, exist_ok=True)

        agent_dir = repo_root / "carto_agent"
        _ = agent_dir.mkdir(parents=True, exist_ok=True)
        _ = (agent_dir / "go.mod").write_text(
            "module example.com/test\n\ngo 1.24.0\n", encoding="utf-8"
        )

        web_dir = repo_root / "autocarto-web"
        _ = (web_dir / "src-tauri").mkdir(parents=True, exist_ok=True)
        _ = (web_dir / "node_modules" / ".bin").mkdir(parents=True, exist_ok=True)
        _ = (web_dir / "tests" / "e2e").mkdir(parents=True, exist_ok=True)
        _ = (web_dir / "package-lock.json").write_text("{}\n", encoding="utf-8")
        _ = (web_dir / "src-tauri" / "Cargo.toml").write_text(
            '[package]\nname = "fake"\nversion = "0.1.0"\n',
            encoding="utf-8",
        )

        next_bin = web_dir / "node_modules" / ".bin" / "next"
        _ = next_bin.write_text(
            '#!/bin/bash\nprintf \'%s\\n\' "next $*" >> "$AUTOCARTO_FAKE_LOG"\n',
            encoding="utf-8",
        )
        _ = next_bin.chmod(0o755)

        playwright_bin = web_dir / "node_modules" / ".bin" / "playwright"
        _ = playwright_bin.write_text(
            '#!/bin/bash\nprintf \'%s\\n\' "playwright $*" >> "$AUTOCARTO_FAKE_LOG"\n',
            encoding="utf-8",
        )
        _ = playwright_bin.chmod(0o755)

        _ = (repo_root / "autocarto" / "tests").mkdir(parents=True, exist_ok=True)

    def _write_fake_commands(self, fake_bin: Path, fake_log: Path) -> None:
        _ = fake_log.write_text("", encoding="utf-8")

        self._write_executable(
            fake_bin / "go",
            """#!/bin/bash
if [ \"${1:-}\" = \"version\" ]; then
  printf 'go version go1.24.11 linux/amd64\\n'
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
  printf 'fake binary\\n' > "$output"
fi
printf '%s\\n' \"go $*\" >> \"$AUTOCARTO_FAKE_LOG\"
""",
        )
        self._write_executable(fake_bin / "node", "#!/bin/bash\nexit 0\n")
        self._write_executable(
            fake_bin / "npm",
            '#!/bin/bash\nprintf \'%s\\n\' "npm $*" >> "$AUTOCARTO_FAKE_LOG"\n',
        )
        self._write_executable(fake_bin / "rustc", "#!/bin/bash\nexit 0\n")
        self._write_executable(
            fake_bin / "cargo",
            textwrap.dedent(
                """#!/bin/bash
                printf '%s\n' "cargo $*" >> "$AUTOCARTO_FAKE_LOG"
                if [ "${AUTOCARTO_REQUIRE_ISOLATED_CARGO:-0}" = "1" ] && [ "${1:-}" = "check" ]; then
                  mkdir -p "$CARGO_TARGET_DIR"
                  if [ "$PWD" -ef "$AUTOCARTO_FAKE_REPO/autocarto-web/src-tauri" ] || [ ! "$CARGO_TARGET_DIR" -ef "$PWD/target" ]; then
                    printf 'Rust build must use the isolated manifest and target\n' >&2
                    exit 11
                  fi
                  mkdir -p binaries
                  printf 'test sidecar\n' > binaries/carto-agent
                  if [ -e "$AUTOCARTO_FAKE_REPO/autocarto-web/src-tauri/binaries/carto-agent" ]; then exit 12; fi
                fi
                """
            ),
        )
        self._write_executable(fake_bin / "cc", "#!/bin/bash\nexit 0\n")
        self._write_executable(fake_bin / "pkg-config", "#!/bin/bash\nexit 0\n")
        self._write_executable(
            fake_bin / "python",
            textwrap.dedent(
                f"""#!/bin/bash
REAL_PYTHON={sys.executable}
if [[ "${{1:-}}" == */scripts/refactor/run_python_tests.py ]]; then
  printf '%s\\n' "python $*" >> "$AUTOCARTO_FAKE_LOG"
  exit 0
fi
if [ \"${{1:-}}\" = \"-m\" ] && [ \"${{2:-}}\" = \"pytest\" ]; then
  printf '%s\\n' \"python $*\" >> \"$AUTOCARTO_FAKE_LOG\"
  exit 0
fi
exec \"$REAL_PYTHON\" \"$@\"
"""
            ),
        )

        install_fake_build_platform_and_go(fake_bin)

    def _write_executable(self, path: Path, content: str) -> None:
        _ = path.write_text(content, encoding="utf-8")
        _ = path.chmod(0o755)


if __name__ == "__main__":
    _ = unittest.main()
