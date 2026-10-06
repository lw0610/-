package geeruntime

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func findPython(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		if found, err := exec.LookPath(name); err == nil {
			return found
		}
	}
	t.Skip("Python is unavailable; offline Python integration tests require Python 3")
	return ""
}

func mockOptions(t *testing.T) Options {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX interpreter fixture; Python helper tests also cover Windows-compatible worker logic")
	}
	python := findPython(t)
	dir := t.TempDir()
	ee := `__version__ = 'offline-mock'
def Initialize(project=None):
    assert project == 'test-project'
class _Task:
    id = 'mock-cloud-task'
    name = 'projects/test-project/operations/mock-cloud-task'
    def start(self): pass
    def status(self): return {'state': 'RUNNING'}
class _ImageExport:
    @staticmethod
    def toDrive(**kwargs): return _Task()
class _Export: image = _ImageExport()
class _Batch: Export = _Export()
batch = _Batch()
`
	if err := os.WriteFile(filepath.Join(dir, "ee.py"), []byte(ee), 0600); err != nil {
		t.Fatal(err)
	}
	dirJSON, _ := json.Marshal(dir)
	bootstrap := "import sys,runpy;sys.path.insert(0," + string(dirJSON) + ");runpy.run_path(sys.argv[1],run_name='__main__')"
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	launcher := "#!/bin/sh\nexec " + quote(python) + " -c " + quote(bootstrap) + " \"$2\"\n"
	wrapper := filepath.Join(dir, "gee-python")
	if err := os.WriteFile(wrapper, []byte(launcher), 0700); err != nil {
		t.Fatal(err)
	}
	return Options{Enabled: true, Python: wrapper, Project: "test-project", TimeoutSeconds: 5, OutputDir: filepath.Join(dir, "outputs with spaces")}
}

func invokeResult(t *testing.T, ctx context.Context, name string, args map[string]any, opts Options) (*Result, error) {
	t.Helper()
	raw, err := Invoke(ctx, name, args, opts)
	result, ok := raw.(*Result)
	if !ok {
		t.Fatalf("expected structured result, got %T", raw)
	}
	return result, err
}

func TestDisabledAndInvalidInputsDoNotExecute(t *testing.T) {
	for _, name := range []string{"gee_run_script", "gee_task_status", "gee_cancel_task"} {
		r, err := invokeResult(t, context.Background(), name, map[string]any{"code": "pass", "task_id": "id"}, Options{})
		if err == nil || r.Success || r.ErrorCode != "disabled" || r.OutputDir != "" {
			t.Fatalf("disabled operation ran: %+v %v", r, err)
		}
	}
	r, err := invokeResult(t, context.Background(), "gee_check_environment", nil, Options{})
	if err != nil || !r.Success || r.Status != "python_not_configured" {
		t.Fatalf("diagnostic failed: %+v %v", r, err)
	}
	for _, name := range []string{"unknown", "GEE_RUN_SCRIPT"} {
		if Handles(name) {
			t.Fatalf("unexpected tool %s", name)
		}
	}
	r, err = invokeResult(t, context.Background(), "gee_task_status", map[string]any{"task_id": "../../oops"}, Options{Enabled: true})
	if err == nil || r.ErrorCode != "invalid_arguments" {
		t.Fatalf("invalid task accepted: %+v", r)
	}
}

func TestRunScriptArtifactsBoundedOutputAndIndependentEnv(t *testing.T) {
	opts := mockOptions(t)
	t.Setenv("PYTHONHOME", "/broken/qgis/home")
	t.Setenv("PYTHONPATH", "/broken/qgis/modules")
	r, err := invokeResult(t, context.Background(), "gee_run_script", map[string]any{"code": "(output_dir / 'result.csv').write_text('x\\n1\\n')\nprint('x' * 100000)\nimport sys\nprint('y' * 100000, file=sys.stderr)"}, opts)
	if err != nil || !r.Success || r.Status != "script_completed" {
		t.Fatalf("run failed: %+v %v", r, err)
	}
	if len(r.Stdout) > maxOutputBytes || !r.StdoutTruncated || !r.StderrTruncated {
		t.Fatalf("unbounded outputs: stdout=%d/%v stderr=%d/%v", len(r.Stdout), r.StdoutTruncated, len(r.Stderr), r.StderrTruncated)
	}
	if !filepath.IsAbs(r.ScriptPath) || !filepath.IsAbs(r.OutputDir) {
		t.Fatal("artifact paths are not absolute")
	}
	for _, name := range []string{"script.py", "result.csv", "stdout.log", "stderr.log"} {
		path := filepath.Join(r.OutputDir, name)
		found := false
		for _, artifact := range r.ArtifactPaths {
			if artifact == path {
				found = true
			}
		}
		if !found {
			t.Fatalf("artifact %s missing: %v", name, r.ArtifactPaths)
		}
	}
}

func TestWorkerFailuresNeverBecomeSuccess(t *testing.T) {
	opts := mockOptions(t)
	for _, code := range []string{"raise RuntimeError('bad geometry')", "import os; os._exit(0)", "raise SystemExit(0)"} {
		t.Run(strconv.Quote(code), func(t *testing.T) {
			setup := "(output_dir / '_gee_download_interrupted').mkdir()\n(output_dir / '_gee_download_interrupted' / 'unfinished.part').write_bytes(b'partial')\n(output_dir / 'completed.csv').write_text('preserve me')\n"
			r, err := invokeResult(t, context.Background(), "gee_run_script", map[string]any{"code": setup + code}, opts)
			if err == nil || r.Success || r.ErrorCode == "" {
				t.Fatalf("failure misreported: %+v %v", r, err)
			}
			if _, statErr := os.Stat(r.ScriptPath); statErr != nil {
				t.Fatal("failed script not retained", statErr)
			}
			if _, statErr := os.Stat(filepath.Join(r.OutputDir, "_gee_download_interrupted")); !os.IsNotExist(statErr) {
				t.Fatal("interrupted raster staging directory not removed", statErr)
			}
			if _, statErr := os.Stat(filepath.Join(r.OutputDir, "completed.csv")); statErr != nil {
				t.Fatal("completed output was removed during failure cleanup", statErr)
			}
		})
	}
}

func TestTimeoutKeepsSubmittedTaskID(t *testing.T) {
	opts := mockOptions(t)
	opts.TimeoutSeconds = 1
	started := time.Now()
	r, err := invokeResult(t, context.Background(), "gee_run_script", map[string]any{"code": "export_image_to_drive(object(), 'sample')\nimport time\ntime.sleep(30)"}, opts)
	if err == nil || r.Success || !r.TimedOut || r.ErrorCode != "idle_timeout" {
		t.Fatalf("timeout incorrect: %+v %v", r, err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("timeout did not promptly stop process")
	}
	if len(r.Tasks) != 1 || r.Tasks[0]["id"] != "mock-cloud-task" {
		t.Fatalf("lost cloud task ID: %+v", r.Tasks)
	}
}

func TestContextCancellation(t *testing.T) {
	opts := mockOptions(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(150*time.Millisecond, cancel)
	r, err := invokeResult(t, ctx, "gee_run_script", map[string]any{"code": "import time; time.sleep(30)"}, opts)
	if err == nil || r.Success || !r.Cancelled || r.TimedOut {
		t.Fatalf("cancellation incorrect: %+v %v", r, err)
	}
}

func TestCancellationStopsChildProcesses(t *testing.T) {
	opts := mockOptions(t)
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	late := filepath.Join(dir, "must-not-be-created")
	readyJSON, _ := json.Marshal(ready)
	lateJSON, _ := json.Marshal(late)
	childCode := "import time; from pathlib import Path; time.sleep(1); Path(" + string(lateJSON) + ").write_text('orphan')"
	childJSON, _ := json.Marshal(childCode)
	code := "import subprocess,sys,time\nfrom pathlib import Path\nsubprocess.Popen([sys.executable, '-c', " + string(childJSON) + "])\nPath(" + string(readyJSON) + ").write_text('ready')\ntime.sleep(30)"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *Result, 1)
	go func() {
		raw, _ := Invoke(ctx, "gee_run_script", map[string]any{"code": code}, opts)
		done <- raw.(*Result)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child process fixture did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case r := <-done:
		if !r.Cancelled || r.Success {
			t.Fatalf("process cancellation failed: status=%s", r.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("process tree did not stop promptly")
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(late); err == nil {
		t.Fatal("orphan child survived cancellation")
	}
}

func TestDisabledRuntimeStillAllowsEnvironmentDiagnostic(t *testing.T) {
	opts := mockOptions(t)
	opts.Enabled = false
	r, err := invokeResult(t, context.Background(), "gee_check_environment", nil, opts)
	if err != nil || !r.Success || r.Enabled || (r.Status != "ready" && r.Status != "ready_without_local_download") || r.Environment["ready"] != true {
		t.Fatalf("disabled diagnostic incorrect: %+v %v", r, err)
	}
}

func TestOfflinePythonWorkerSuite(t *testing.T) {
	python := findPython(t)
	cmd := exec.Command(python, "-m", "unittest", "worker_test.py")
	cmd.Env = runtimeEnv("")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("offline Python helper tests: %v\n%s", err, output)
	}
}
