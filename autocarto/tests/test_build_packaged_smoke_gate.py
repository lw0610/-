import json
import os
import shutil
import subprocess
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path
from typing import Optional, TypedDict


REPO_ROOT = Path(__file__).resolve().parents[2]
SOURCE_BUILD_SH = REPO_ROOT / "build.sh"


class CheckRunResult(TypedDict):
    completed: subprocess.CompletedProcess[str]
    repo_root: Path
    smoke_runs: list[dict[str, str]]


class BuildPackagedSmokeGateTestCase(unittest.TestCase):
    maxDiff: Optional[int] = None

    def test_help_mentions_packaged_smoke_command_and_env(self):
        completed = self._run_simple_command("help")

        self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)
        self.assertIn("packaged-smoke", completed.stdout)
        self.assertIn("AUTOCARTO_PACKAGED_SMOKE=1 ./build.sh check", completed.stdout)
        self.assertIn(
            ".sisyphus/evidence/task-10-build-check-packaged.txt", completed.stdout
        )

    def test_packaged_smoke_subcommand_resolves_dist_appimage(self):
        completed, fake_log, smoke_runs = self._run_packaged_smoke_command(
            command=["bash", "./build.sh", "packaged-smoke"]
        )

        self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)
        self.assertIn("Packaged Runtime Smoke", completed.stdout)
        self.assertEqual(len(smoke_runs), 1, smoke_runs)
        self.assertTrue(
            smoke_runs[0]["artifact_path"].endswith(
                "dist/AutoCartoAI-0.1.0-linux-amd64.AppImage"
            ),
            smoke_runs,
        )
        self.assertEqual(smoke_runs[0]["expected_contract_version"], "v1")
        self.assertIn(
            "python autocarto-web/scripts/packaged_runtime_smoke.py",
            fake_log.read_text(encoding="utf-8"),
        )

    def test_dist_runs_packaged_smoke_and_fails_when_smoke_fails(self):
        completed, repo_root, smoke_runs = self._run_dist(
            packaged_smoke_exit=1,
            packaged_smoke_failure_reason="descriptor readiness 非 ready: degraded",
        )

        self.assertNotEqual(
            completed.returncode, 0, completed.stdout + completed.stderr
        )
        self.assertIn("Packaged Runtime Smoke", completed.stdout)
        self.assertEqual(len(smoke_runs), 1, smoke_runs)

        summary_path = (
            repo_root / ".sisyphus" / "evidence" / "task-7-release-gate" / "summary.txt"
        )
        self.assertTrue(summary_path.exists(), summary_path.as_posix())
        summary_text = summary_path.read_text(encoding="utf-8")
        self.assertIn("status=FAIL", summary_text)
        self.assertIn("task-6-packaged-runtime-smoke", summary_text)
        self.assertIn("descriptor.json", summary_text)
        self.assertIn("legacy_rejection.json", summary_text)
        self.assertIn("smoke.log", summary_text)
        self.assertIn("production bundle Playwright", summary_text)
        self.assertIn("descriptor readiness 非 ready: degraded", summary_text)

    def test_check_reports_skip_by_default_and_runs_packaged_smoke_when_enabled(self):
        skipped = self._run_check(packaged_smoke_enabled=False)

        self.assertEqual(
            skipped["completed"].returncode,
            0,
            skipped["completed"].stdout + skipped["completed"].stderr,
        )
        self.assertIn(
            "packaged smoke：SKIPPED（原因：未设置 AUTOCARTO_PACKAGED_SMOKE=1）",
            skipped["completed"].stdout,
        )
        self.assertIn(
            "本次运行证据：.sisyphus/evidence/task-10-build-check.txt",
            skipped["completed"].stdout,
        )
        self.assertIn(
            "packaged check 证据：.sisyphus/evidence/task-10-build-check-packaged.txt",
            skipped["completed"].stdout,
        )
        self.assertEqual(skipped["smoke_runs"], [], skipped["smoke_runs"])

        enabled = self._run_check(
            packaged_smoke_enabled=True,
            packaged_smoke_exit=1,
            packaged_smoke_failure_reason="descriptor readiness 非 ready: degraded",
        )

        self.assertNotEqual(
            enabled["completed"].returncode,
            0,
            enabled["completed"].stdout + enabled["completed"].stderr,
        )
        self.assertIn(
            "packaged smoke：ENABLED（AUTOCARTO_PACKAGED_SMOKE=1）",
            enabled["completed"].stdout,
        )
        self.assertIn(
            "本次运行证据：.sisyphus/evidence/task-10-build-check-packaged.txt",
            enabled["completed"].stdout,
        )
        self.assertIn(
            "packaged smoke 证据目录：.sisyphus/evidence/task-6-packaged-runtime-smoke",
            enabled["completed"].stdout,
        )
        self.assertIn(
            "release gate 摘要目录：.sisyphus/evidence/task-7-release-gate",
            enabled["completed"].stdout,
        )
        self.assertEqual(len(enabled["smoke_runs"]), 1, enabled["smoke_runs"])
        self.assertIn(
            "descriptor readiness 非 ready: degraded",
            (
                enabled["repo_root"]
                / ".sisyphus"
                / "evidence"
                / "task-7-release-gate"
                / "summary.txt"
            ).read_text(encoding="utf-8"),
        )

    def _run_simple_command(
        self, command_name: str
    ) -> subprocess.CompletedProcess[str]:
        temp_dir = tempfile.TemporaryDirectory()
        self.addCleanup(temp_dir.cleanup)

        repo_root = Path(temp_dir.name) / "repo"
        repo_root.mkdir(parents=True, exist_ok=True)
        _ = shutil.copy2(SOURCE_BUILD_SH, repo_root / "build.sh")
        self._seed_repo_layout(repo_root)

        completed = subprocess.run(
            ["bash", "./build.sh", command_name],
            cwd=repo_root,
            capture_output=True,
            text=True,
            check=False,
        )
        return completed

    def _run_packaged_smoke_command(
        self, command: list[str]
    ) -> tuple[subprocess.CompletedProcess[str], Path, list[dict[str, str]]]:
        temp_dir = tempfile.TemporaryDirectory()
        self.addCleanup(temp_dir.cleanup)

        repo_root = Path(temp_dir.name) / "repo"
        repo_root.mkdir(parents=True, exist_ok=True)
        _ = shutil.copy2(SOURCE_BUILD_SH, repo_root / "build.sh")

        fake_log = repo_root / "fake-commands.log"
        fake_bin = repo_root / "fake-bin"
        fake_bin.mkdir(parents=True, exist_ok=True)

        self._seed_repo_layout(repo_root)
        self._seed_dist_artifact(repo_root)
        self._write_fake_commands(fake_bin, fake_log, repo_root)

        env = os.environ.copy()
        env["PATH"] = f"{fake_bin}{os.pathsep}{env.get('PATH', '')}"
        env["AUTOCARTO_FAKE_LOG"] = str(fake_log)
        env["AUTOCARTO_FAKE_REPO"] = str(repo_root)
        env["AUTOCARTO_FAKE_PACKAGED_SMOKE_RUNS"] = str(
            repo_root / "packaged-smoke-runs.jsonl"
        )

        completed = subprocess.run(
            command,
            cwd=repo_root,
            env=env,
            capture_output=True,
            text=True,
            check=False,
        )
        return completed, fake_log, self._read_smoke_runs(repo_root)

    def _run_dist(
        self,
        packaged_smoke_exit: int = 0,
        packaged_smoke_failure_reason: str = "",
    ) -> tuple[subprocess.CompletedProcess[str], Path, list[dict[str, str]]]:
        temp_dir = tempfile.TemporaryDirectory()
        self.addCleanup(temp_dir.cleanup)

        repo_root = Path(temp_dir.name) / "repo"
        repo_root.mkdir(parents=True, exist_ok=True)
        _ = shutil.copy2(SOURCE_BUILD_SH, repo_root / "build.sh")

        fake_log = repo_root / "fake-commands.log"
        fake_bin = repo_root / "fake-bin"
        fake_bin.mkdir(parents=True, exist_ok=True)

        self._seed_repo_layout(repo_root)
        self._write_fake_commands(fake_bin, fake_log, repo_root)

        env = os.environ.copy()
        env["PATH"] = f"{fake_bin}{os.pathsep}{env.get('PATH', '')}"
        env["AUTOCARTO_FAKE_LOG"] = str(fake_log)
        env["AUTOCARTO_FAKE_REPO"] = str(repo_root)
        env["AUTOCARTO_FAKE_PACKAGED_SMOKE_RUNS"] = str(
            repo_root / "packaged-smoke-runs.jsonl"
        )
        env["AUTOCARTO_FAKE_PACKAGED_SMOKE_EXIT"] = str(packaged_smoke_exit)
        env["AUTOCARTO_FAKE_PACKAGED_SMOKE_FAILURE_REASON"] = (
            packaged_smoke_failure_reason
        )

        completed = subprocess.run(
            ["bash", "./build.sh", "dist"],
            cwd=repo_root,
            env=env,
            capture_output=True,
            text=True,
            check=False,
        )
        return completed, repo_root, self._read_smoke_runs(repo_root)

    def _run_check(
        self,
        packaged_smoke_enabled: bool,
        packaged_smoke_exit: int = 0,
        packaged_smoke_failure_reason: str = "",
        live_e2e_enabled: bool = False,
    ) -> CheckRunResult:
        temp_dir = tempfile.TemporaryDirectory()
        self.addCleanup(temp_dir.cleanup)

        repo_root = Path(temp_dir.name) / "repo"
        repo_root.mkdir(parents=True, exist_ok=True)
        _ = shutil.copy2(SOURCE_BUILD_SH, repo_root / "build.sh")

        fake_log = repo_root / "fake-commands.log"
        fake_bin = repo_root / "fake-bin"
        fake_bin.mkdir(parents=True, exist_ok=True)

        self._seed_repo_layout(repo_root)
        self._seed_dist_artifact(repo_root)
        self._write_fake_commands(fake_bin, fake_log, repo_root)

        env = os.environ.copy()
        env["PATH"] = f"{fake_bin}{os.pathsep}{env.get('PATH', '')}"
        env["AUTOCARTO_FAKE_LOG"] = str(fake_log)
        env["AUTOCARTO_FAKE_REPO"] = str(repo_root)
        env["AUTOCARTO_FAKE_PACKAGED_SMOKE_RUNS"] = str(
            repo_root / "packaged-smoke-runs.jsonl"
        )
        env["AUTOCARTO_FAKE_PACKAGED_SMOKE_EXIT"] = str(packaged_smoke_exit)
        env["AUTOCARTO_FAKE_PACKAGED_SMOKE_FAILURE_REASON"] = (
            packaged_smoke_failure_reason
        )
        if live_e2e_enabled:
            env["AUTOCARTO_LIVE_E2E"] = "1"
        else:
            _ = env.pop("AUTOCARTO_LIVE_E2E", None)
        if packaged_smoke_enabled:
            env["AUTOCARTO_PACKAGED_SMOKE"] = "1"
        else:
            _ = env.pop("AUTOCARTO_PACKAGED_SMOKE", None)

        completed = subprocess.run(
            ["bash", "./build.sh", "check"],
            cwd=repo_root,
            env=env,
            capture_output=True,
            text=True,
            check=False,
        )
        return {
            "completed": completed,
            "repo_root": repo_root,
            "smoke_runs": self._read_smoke_runs(repo_root),
        }

    def _read_smoke_runs(self, repo_root: Path) -> list[dict[str, str]]:
        runs_path = repo_root / "packaged-smoke-runs.jsonl"
        if not runs_path.exists():
            return []
        return [
            json.loads(line)
            for line in runs_path.read_text(encoding="utf-8").splitlines()
            if line
        ]

    def _seed_repo_layout(self, repo_root: Path) -> None:
        _ = (repo_root / ".sisyphus" / "evidence").mkdir(parents=True, exist_ok=True)

        agent_dir = repo_root / "carto_agent"
        _ = agent_dir.mkdir(parents=True, exist_ok=True)
        _ = (agent_dir / "go.mod").write_text(
            "module example.com/test\n\ngo 1.24.0\n",
            encoding="utf-8",
        )

        web_dir = repo_root / "autocarto-web"
        _ = (web_dir / "src-tauri").mkdir(parents=True, exist_ok=True)
        _ = (web_dir / "node_modules" / ".bin").mkdir(parents=True, exist_ok=True)
        _ = (web_dir / "tests" / "e2e").mkdir(parents=True, exist_ok=True)
        _ = (web_dir / "scripts").mkdir(parents=True, exist_ok=True)
        _ = (web_dir / "package-lock.json").write_text("{}\n", encoding="utf-8")
        _ = (web_dir / "src-tauri" / "Cargo.toml").write_text(
            '[package]\nname = "fake"\nversion = "0.1.0"\n',
            encoding="utf-8",
        )
        _ = (web_dir / "src-tauri" / "tauri.conf.json").write_text(
            '{"version":"0.1.0","productName":"AutoCartoAI"}\n',
            encoding="utf-8",
        )
        _ = (web_dir / "scripts" / "packaged_runtime_smoke.py").write_text(
            textwrap.dedent(
                """
                #!/usr/bin/env python3
                import argparse
                import json
                import os
                import sys
                from pathlib import Path

                parser = argparse.ArgumentParser()
                parser.add_argument("--artifact-path", required=True)
                parser.add_argument("--runtime-home", required=True)
                parser.add_argument("--evidence-dir", required=True)
                parser.add_argument("--expected-contract-version", required=True)
                parser.add_argument("--timeout-sec", required=True)
                args = parser.parse_args()

                evidence_dir = Path(args.evidence_dir)
                evidence_dir.mkdir(parents=True, exist_ok=True)
                summary = {
                    "artifact_path": args.artifact_path,
                    "runtime_home": args.runtime_home,
                    "evidence_dir": args.evidence_dir,
                    "expected_contract_version": args.expected_contract_version,
                    "timeout_sec": args.timeout_sec,
                }
                runs_path = Path(os.environ["AUTOCARTO_FAKE_PACKAGED_SMOKE_RUNS"])
                with runs_path.open("a", encoding="utf-8") as handle:
                    handle.write(json.dumps(summary, ensure_ascii=False) + "\\n")

                failure_reason = os.environ.get(
                    "AUTOCARTO_FAKE_PACKAGED_SMOKE_FAILURE_REASON",
                    "descriptor readiness 非 ready: degraded",
                )
                descriptor_payload = {
                    "status_code": 200,
                    "payload": {
                        "success": False,
                        "data": {
                            "version": "v1",
                            "readiness": "degraded",
                            "degraded": True,
                        },
                    },
                    "failure_reason": failure_reason,
                }
                legacy_payload = {
                    "status_code": 400,
                    "payload": {
                        "code": "HTTP_CONTRACT_VERSION_UNSUPPORTED",
                        "data": {
                            "requested_contract_version": "1",
                            "contract_version": "v1",
                            "supported_contract_versions": ["v1"],
                        },
                    },
                }
                (evidence_dir / "descriptor.json").write_text(
                    json.dumps(descriptor_payload, ensure_ascii=False, indent=2) + "\\n",
                    encoding="utf-8",
                )
                (evidence_dir / "legacy_rejection.json").write_text(
                    json.dumps(legacy_payload, ensure_ascii=False, indent=2) + "\\n",
                    encoding="utf-8",
                )
                (evidence_dir / "stdout.log").write_text("fake stdout\\n", encoding="utf-8")
                (evidence_dir / "stderr.log").write_text(failure_reason + "\\n", encoding="utf-8")
                (evidence_dir / "smoke.log").write_text(failure_reason + "\\n", encoding="utf-8")
                log_path = os.environ.get("AUTOCARTO_FAKE_LOG")
                if log_path:
                    with Path(log_path).open("a", encoding="utf-8") as handle:
                        handle.write(
                            "python autocarto-web/scripts/packaged_runtime_smoke.py --artifact-path "
                            + args.artifact_path
                            + "\\n"
                        )
                raise SystemExit(int(os.environ.get("AUTOCARTO_FAKE_PACKAGED_SMOKE_EXIT", "0")))
                """
            ).strip()
            + "\n",
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

        tauri_bin = web_dir / "node_modules" / ".bin" / "tauri"
        _ = tauri_bin.write_text(
            textwrap.dedent(
                """#!/bin/bash
                printf '%s\n' "tauri $*" >> "$AUTOCARTO_FAKE_LOG"
                repo_root="$AUTOCARTO_FAKE_REPO"
                bundle_root="$repo_root/autocarto-web/src-tauri/target/release/bundle"
                mkdir -p "$bundle_root/appimage" "$bundle_root/deb"
                printf 'fake appimage\n' > "$bundle_root/appimage/AutoCartoAI.AppImage"
                chmod +x "$bundle_root/appimage/AutoCartoAI.AppImage"
                printf 'fake deb\n' > "$bundle_root/deb/autocarto-ai.deb"
                exit 0
                """
            ),
            encoding="utf-8",
        )
        _ = tauri_bin.chmod(0o755)

        _ = (repo_root / "autocarto" / "tests").mkdir(parents=True, exist_ok=True)
        _ = (repo_root / "autocarto" / "metadata.txt").write_text(
            "name=AutoCartoAI\nversion=0.1.0\n",
            encoding="utf-8",
        )
        _ = (repo_root / "autocarto" / "tool_catalog_client.py").write_text(
            "VERSION = 'new-source'\n",
            encoding="utf-8",
        )

    def _seed_dist_artifact(self, repo_root: Path) -> None:
        artifact_path = repo_root / "dist" / "AutoCartoAI-0.1.0-linux-amd64.AppImage"
        artifact_path.parent.mkdir(parents=True, exist_ok=True)
        _ = artifact_path.write_text("fake packaged appimage\n", encoding="utf-8")
        _ = artifact_path.chmod(0o755)

    def _write_fake_commands(
        self, fake_bin: Path, fake_log: Path, repo_root: Path
    ) -> None:
        _ = fake_log.write_text("", encoding="utf-8")

        self._write_executable(
            fake_bin / "go",
            textwrap.dedent(
                f"""#!/bin/bash
                if [ "${{1:-}}" = "version" ]; then
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
                printf '%s\\n' "go $*" >> "$AUTOCARTO_FAKE_LOG"
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
        self._write_executable(fake_bin / "lsof", "#!/bin/bash\nexit 0\n")
        self._write_executable(
            fake_bin / "python",
            textwrap.dedent(
                f"""#!/bin/bash
                REAL_PYTHON={sys.executable}
                if [ "${{1:-}}" = "-m" ] && [ "${{2:-}}" = "pytest" ]; then
                  printf '%s\\n' "python $*" >> "$AUTOCARTO_FAKE_LOG"
                  exit 0
                fi
                export AUTOCARTO_FAKE_REPO={repo_root}
                exec "$REAL_PYTHON" "$@"
                """
            ),
        )

    def _write_executable(self, path: Path, content: str) -> None:
        _ = path.write_text(content, encoding="utf-8")
        _ = path.chmod(0o755)


if __name__ == "__main__":
    _ = unittest.main()
