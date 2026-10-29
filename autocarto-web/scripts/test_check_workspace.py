import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import signal
import unittest


HELPER = Path(__file__).with_name("check_workspace.py")
SPEC = importlib.util.spec_from_file_location("check_workspace", HELPER)
workspace = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(workspace)


class CheckWorkspaceTest(unittest.TestCase):
    def test_build_and_tauri_outputs_stay_in_snapshot(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "source"
            web = root / "autocarto-web"
            for path in (".next/dev", "out", "src-tauri/binaries", "src-tauri/target", "node_modules/.bin", "src"):
                (web / path).mkdir(parents=True)
            for path in (".next/dev/sentinel", "out/sentinel", "src-tauri/binaries/sentinel", "src-tauri/target/sentinel", ".env.local", ".env.production", "tsconfig.tsbuildinfo"):
                (web / path).write_text("must remain untouched")
            (web / "src-tauri/tauri.conf.json").write_text('{"build":{"frontendDist":"../out"}}')
            (web / "src/page.tsx").write_text("export default function Page() { return null }")
            for name in ("carto_agent", "autocarto"):
                (root / name).mkdir()
            executable = web / "node_modules/.bin/next"
            executable.write_text("#!" + sys.executable + "\n" + "import json, os\nfrom pathlib import Path\nassert 'AUTOCARTO_NEXT_DIST_DIR' not in os.environ\nPath('.next').mkdir()\nPath('.next/build').write_text('built')\nPath('out').mkdir()\nPath('out/chat.html').write_text('built')\n")
            executable.chmod(0o755)
            snapshot = workspace.prepare_workspace(root, Path(temporary) / "check")
            self.assertTrue((snapshot / "src-tauri/tauri.conf.json").is_file())
            self.assertTrue((snapshot / "src/page.tsx").is_file())
            for omitted in (".next", "out", "src-tauri/binaries", "src-tauri/target", ".env.local", ".env.production", "tsconfig.tsbuildinfo"):
                self.assertFalse((snapshot / omitted).exists(), omitted)
            workspace.build_frontend(snapshot, {**os.environ, "AUTOCARTO_NEXT_DIST_DIR": str(web / ".next")})
            self.assertEqual((snapshot / "out/chat.html").read_text(), "built")
            (snapshot / "src-tauri/binaries").mkdir()
            (snapshot / "src-tauri/binaries/agent").write_text("new sidecar")
            for path in (".next/dev/sentinel", "out/sentinel", "src-tauri/binaries/sentinel", "src-tauri/target/sentinel", ".env.local"):
                self.assertEqual((web / path).read_text(), "must remain untouched")
            self.assertFalse((web / "src-tauri/binaries/agent").exists())
            self.assertTrue((snapshot / "node_modules").is_symlink())
            self.assertEqual((snapshot.parent / "carto_agent").resolve(), (root / "carto_agent").resolve())

    def test_failed_build_is_reported_without_reusing_original_export(self):
        with tempfile.TemporaryDirectory() as temporary:
            web = Path(temporary)
            (web / "node_modules/.bin").mkdir(parents=True)
            executable = web / "node_modules/.bin/next"
            executable.write_text("#!" + sys.executable + "\nraise SystemExit(7)\n")
            executable.chmod(0o755)
            with self.assertRaises(subprocess.CalledProcessError) as error:
                workspace.build_frontend(web)
            self.assertEqual(error.exception.returncode, 7)
            self.assertFalse((web / "out").exists())

    def test_package_settings_change_only_copied_assets(self):
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary) / "assets"
            for name in ("skills", "tools", "flows", "resources"):
                (source / name).mkdir(parents=True)
            package = "skills/system/canvas-navigation"
            (source / package).mkdir(parents=True)
            (source / package / "SKILL.md").write_text("a built-in package")
            copied = workspace.prepare_assets(source, Path(temporary) / "copied")
            (copied / package / "config.yaml").write_text("enabled: false\n")
            self.assertFalse((source / package / "config.yaml").exists())
            self.assertEqual((source / package / "SKILL.md").read_text(), "a built-in package")

    @unittest.skipIf(os.name == "nt", "POSIX process-group shutdown")
    def test_supervisor_sigterm_reaps_its_server(self):
        with tempfile.TemporaryDirectory() as temporary:
            pidfile = Path(temporary) / "child.pid"
            child_code = f"import os,time; from pathlib import Path; Path({str(pidfile)!r}).write_text(str(os.getpid())); time.sleep(60)"
            supervisor_code = f"import sys,os;sys.path.insert(0,{str(HELPER.parent)!r});from check_workspace import run_owned;run_owned([sys.executable,'-c',{child_code!r}],cwd={temporary!r},env=os.environ.copy())"
            supervisor = subprocess.Popen([sys.executable, "-c", supervisor_code])
            try:
                deadline = time.monotonic() + 5
                while not pidfile.exists() and time.monotonic() < deadline and supervisor.poll() is None:
                    time.sleep(0.01)
                self.assertTrue(pidfile.exists(), "server never started")
                pid = int(pidfile.read_text())
                supervisor.terminate()
                self.assertEqual(supervisor.wait(timeout=5), 128 + signal.SIGTERM)
                with self.assertRaises(ProcessLookupError):
                    os.kill(pid, 0)
            finally:
                if supervisor.poll() is None:
                    supervisor.terminate()
                    supervisor.wait(timeout=15)


if __name__ == "__main__":
    unittest.main()
