package geeruntime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIdleWindowStartsAfterProcessSetupAndCountsTruncatedOutput(t *testing.T) {
	const idle = 200 * time.Millisecond
	ctx, cancel, activity, arm := withOutputIdleTimeout(context.Background(), idle)
	defer cancel()
	// Slow log/process setup has no script-idle deadline before cmd.Start.
	time.Sleep(2 * idle)
	if ctx.Err() != nil {
		t.Fatal("idle watchdog ran before the child process was started")
	}
	arm()
	output := &boundedBuffer{onWrite: activity}
	_, _ = output.Write(bytes.Repeat([]byte{'x'}, maxOutputBytes+1))
	for range 6 {
		time.Sleep(idle / 3)
		_, _ = output.Write([]byte("still working after log limit"))
		if ctx.Err() != nil {
			t.Fatal("truncated output stopped resetting the idle window")
		}
	}
	if !output.truncated || len(output.String()) != maxOutputBytes {
		t.Fatal("fixture did not exercise the truncated log path")
	}
	select {
	case <-ctx.Done():
		if context.Cause(ctx) != errOutputIdleTimeout {
			t.Fatalf("unexpected stop cause: %v", context.Cause(ctx))
		}
	case <-time.After(5 * idle):
		t.Fatal("idle watchdog did not fire once output stopped")
	}
}

func TestScriptOutputKeepsExecutionAliveBeyondTimeout(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		for _, truncate := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/truncate=%v", stream, truncate), func(t *testing.T) {
				opts := mockOptions(t)
				opts.TimeoutSeconds = 1
				code := "import sys,time\n"
				if truncate {
					code += fmt.Sprintf("print('x' * 100000, file=sys.%s, flush=True)\n", stream)
				}
				code += fmt.Sprintf("for step in range(8):\n    print('working', file=sys.%s, flush=True)\n    time.sleep(0.25)\n", stream)
				r, err := invokeResult(t, context.Background(), "gee_run_script", map[string]any{"code": code}, opts)
				if err != nil || !r.Success || r.TimedOut || r.ElapsedMS < 1800 {
					t.Fatalf("active script failed before completing beyond old total limit: status=%s elapsed=%d timeout=%v stdout_bytes=%d stderr_bytes=%d error=%v", r.Status, r.ElapsedMS, r.TimedOut, len(r.Stdout), len(r.Stderr), err)
				}
				if truncate && ((stream == "stdout" && !r.StdoutTruncated) || (stream == "stderr" && !r.StderrTruncated)) {
					t.Fatal("fixture did not exceed the log limit")
				}
			})
		}
	}
}

func TestScriptTimesOutAfterOutputStops(t *testing.T) {
	opts := mockOptions(t)
	opts.TimeoutSeconds = 1
	code := "import time\nfor step in range(3):\n    print('working', flush=True)\n    time.sleep(0.25)\ntime.sleep(30)"
	r, err := invokeResult(t, context.Background(), "gee_run_script", map[string]any{"code": code}, opts)
	if err == nil || r.Success || !r.TimedOut || r.Cancelled || r.ErrorCode != "idle_timeout" || !strings.Contains(r.Message, "no stdout/stderr output") {
		t.Fatalf("stalled script returned wrong outcome: %+v error=%v", r, err)
	}
	if r.ElapsedMS < 1400 || r.ElapsedMS > 5000 {
		t.Fatalf("idle window was not reset after output: elapsed=%dms", r.ElapsedMS)
	}
}

func TestParentDeadlineStillStopsActiveScript(t *testing.T) {
	opts := mockOptions(t)
	opts.TimeoutSeconds = 5
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	code := "import time\nwhile True:\n    print('working', flush=True)\n    time.sleep(0.1)"
	r, err := invokeResult(t, ctx, "gee_run_script", map[string]any{"code": code}, opts)
	if err == nil || r.Success || !r.TimedOut || r.Cancelled || r.ErrorCode != "timeout" || r.ElapsedMS > 2500 {
		t.Fatalf("active output ignored parent deadline: %+v error=%v", r, err)
	}
}

func TestDiagnosticRetainsAbsoluteTimeoutWhileProducingOutput(t *testing.T) {
	opts := mockOptions(t)
	opts.TimeoutSeconds = 1
	// Environment checks initialize EE too; emulate a stuck initialization that
	// keeps printing. Unlike user scripts, short diagnostic calls stay bounded.
	module := filepath.Join(filepath.Dir(opts.Python), "ee.py")
	f, err := os.OpenFile(module, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\nimport time\ndef Initialize(project=None):\n    while True:\n        print('initializing', flush=True)\n        time.sleep(0.1)\n")
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("write mock: %v %v", err, closeErr)
	}
	r, err := invokeResult(t, context.Background(), "gee_check_environment", nil, opts)
	if err == nil || r.Success || !r.TimedOut || r.ErrorCode != "timeout" || r.ElapsedMS > 3000 || !strings.Contains(r.Stdout, "initializing") {
		t.Fatalf("diagnostic lost absolute deadline: %+v error=%v", r, err)
	}
}
