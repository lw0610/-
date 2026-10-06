// Package geeruntime runs Earth Engine Python independently of the QGIS runtime.
// It is a process boundary, not a security sandbox: scripts have the user's OS permissions.
package geeruntime

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed worker.py
var workerSource []byte

const maxOutputBytes = 64 * 1024
const maxResultBytes = 1024 * 1024
const maxCodeBytes = 1024 * 1024

type Options struct {
	Enabled        bool
	Python         string
	Project        string
	ProxyURL       string
	TimeoutSeconds int
	OutputDir      string
	OnProgress     func(Progress)
}

// Result distinguishes process completion, environment readiness and cloud task completion.
// A failed Invoke can return both a Result and an error; callers should retain the Result.
type Result struct {
	Success         bool             `json:"success"`
	Operation       string           `json:"operation"`
	Enabled         bool             `json:"enabled"`
	Status          string           `json:"status"`
	ErrorCode       string           `json:"error_code,omitempty"`
	Message         string           `json:"message,omitempty"`
	Environment     map[string]any   `json:"environment,omitempty"`
	Stdout          string           `json:"stdout,omitempty"`
	Stderr          string           `json:"stderr,omitempty"`
	StdoutTruncated bool             `json:"stdout_truncated,omitempty"`
	StderrTruncated bool             `json:"stderr_truncated,omitempty"`
	ScriptPath      string           `json:"script_path,omitempty"`
	OutputDir       string           `json:"output_dir,omitempty"`
	ArtifactPaths   []string         `json:"artifact_paths,omitempty"`
	Tasks           []map[string]any `json:"tasks,omitempty"`
	Downloads       []map[string]any `json:"downloads,omitempty"`
	Task            map[string]any   `json:"task,omitempty"`
	ElapsedMS       int64            `json:"elapsed_ms"`
	ExitCode        int              `json:"exit_code"`
	TimedOut        bool             `json:"timed_out,omitempty"`
	Cancelled       bool             `json:"cancelled,omitempty"`
}

func Handles(name string) bool {
	switch name {
	case "gee_check_environment", "gee_run_script", "gee_task_status", "gee_cancel_task":
		return true
	}
	return false
}

var taskPattern = regexp.MustCompile(`^(?:[A-Za-z0-9_-]+|projects/[A-Za-z0-9._:-]+/operations/[A-Za-z0-9_-]+)$`)

func Invoke(ctx context.Context, name string, args map[string]any, opts Options) (any, error) {
	r := &Result{Operation: name, Enabled: opts.Enabled, ExitCode: -1}
	fail := func(code, message string) (any, error) {
		r.Success, r.Status, r.ErrorCode, r.Message = false, "failed", code, message
		return r, errors.New(message)
	}
	if !Handles(name) {
		return fail("unknown_tool", "unsupported Earth Engine tool: "+name)
	}
	if !opts.Enabled && name != "gee_check_environment" {
		return fail("disabled", "Earth Engine is disabled. Enable the GEE runtime in settings before executing scripts or managing cloud tasks.")
	}
	code, _ := args["code"].(string)
	taskID, _ := args["task_id"].(string)
	if name == "gee_run_script" && (strings.TrimSpace(code) == "" || len(code) > maxCodeBytes) {
		return fail("invalid_arguments", "code must be a non-empty Python string no larger than 1 MiB")
	}
	if (name == "gee_task_status" || name == "gee_cancel_task") && !taskPattern.MatchString(taskID) {
		return fail("invalid_arguments", "task_id must be an Earth Engine task ID or projects/<project>/operations/<id>")
	}
	python := strings.TrimSpace(opts.Python)
	if python == "" {
		if name == "gee_check_environment" {
			r.Success, r.Status, r.Message = true, "python_not_configured", "Configure gee.python_path with a Python interpreter containing earthengine-api. No packages or credentials were changed."
			r.Environment = map[string]any{"ready": false, "python_configured": false}
			return r, nil
		}
		return fail("python_not_configured", "Configure the GEE Python interpreter first.")
	}
	resolved, err := exec.LookPath(python)
	if err != nil {
		return fail("python_not_found", "The configured GEE Python interpreter cannot be found: "+python)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return fail("python_not_found", err.Error())
	}
	if opts.ProxyURL != "" {
		u, parseErr := url.Parse(opts.ProxyURL)
		if parseErr != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fail("invalid_proxy", "GEE proxy must be a valid http:// or https:// URL")
		}
	}
	root := strings.TrimSpace(opts.OutputDir)
	if root == "" {
		root = filepath.Join("outputs", "gee")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return fail("output_directory", err.Error())
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return fail("output_directory", err.Error())
	}
	runDir, err := os.MkdirTemp(root, "run-")
	if err != nil {
		return fail("output_directory", err.Error())
	}
	r.OutputDir = runDir
	workerPath := filepath.Join(runDir, "_gee_worker.py")
	resultPath := filepath.Join(runDir, "_worker_result.json")
	if err := os.WriteFile(workerPath, workerSource, 0600); err != nil {
		return fail("write_worker", err.Error())
	}
	if name == "gee_run_script" {
		r.ScriptPath = filepath.Join(runDir, "script.py")
		if err := os.WriteFile(r.ScriptPath, []byte(code), 0600); err != nil {
			return fail("write_script", err.Error())
		}
	}
	payload, err := json.Marshal(map[string]any{
		"operation": name, "project": strings.TrimSpace(opts.Project), "enabled": opts.Enabled,
		"script_path": r.ScriptPath, "task_id": taskID, "output_dir": runDir, "result_path": resultPath,
		"download_root": root,
	})
	if err != nil {
		return fail("invalid_arguments", err.Error())
	}
	timeout := opts.TimeoutSeconds
	if timeout <= 0 {
		timeout = 300
	}
	if timeout > 3600 {
		timeout = 3600
	}
	// GeoCode keeps a script alive while it reports activity. A fixed total
	// deadline would kill a healthy large download and encourage wasteful retries.
	// Short diagnostic/task operations still have an absolute deadline.
	var runCtx context.Context
	var cancel context.CancelFunc
	var activity func()
	var startIdleWatch func()
	if name == "gee_run_script" {
		runCtx, cancel, activity, startIdleWatch = withOutputIdleTimeout(ctx, time.Duration(timeout)*time.Second)
	} else {
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	}
	defer cancel()
	cmd := exec.CommandContext(runCtx, resolved, "-u", workerPath)
	cmd.Dir, cmd.Env, cmd.Stdin = runDir, runtimeEnv(opts.ProxyURL), bytes.NewReader(payload)
	cmd.WaitDelay = 2 * time.Second
	configureProcess(cmd)
	stdout, stderr := &boundedBuffer{onWrite: activity}, &boundedBuffer{onWrite: activity}
	// Keep bounded logs visible while Python is running, including before timeout.
	for file, buffer := range map[string]*boundedBuffer{"stdout.log": stdout, "stderr.log": stderr} {
		f, err := os.OpenFile(filepath.Join(runDir, file), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fail("write_artifact", err.Error())
		}
		defer f.Close()
		buffer.sink = f
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	started := time.Now()
	callback := opts.OnProgress
	if callback == nil {
		callback, _ = ctx.Value(progressCallbackKey{}).(func(Progress))
	}
	stopProgress := watchProgress(runCtx, filepath.Join(runDir, "progress.jsonl"), callback)
	runErr := cmd.Start()
	if runErr == nil {
		// Start the idle window only after the process has actually spawned,
		// matching upstream. Log setup or OS process creation is not script idle.
		if startIdleWatch != nil {
			startIdleWatch()
		}
		runErr = cmd.Wait()
	}
	processCtxErr := runCtx.Err()
	processCause := context.Cause(runCtx)
	// Stop the idle watchdog before log/progress delivery and result processing.
	// Their latency must never turn a completed subprocess into a timeout.
	cancel()
	stopProgress()
	cleanupIncompleteDownloads(runDir)
	r.ElapsedMS = time.Since(started).Milliseconds()
	r.Stdout, r.Stderr = strings.ToValidUTF8(stdout.String(), "?"), strings.ToValidUTF8(stderr.String(), "?")
	r.StdoutTruncated, r.StderrTruncated = stdout.truncated, stderr.truncated
	if cmd.ProcessState != nil {
		r.ExitCode = cmd.ProcessState.ExitCode()
	}
	// A separate response file prevents user print() output from masquerading as protocol JSON.
	if f, openErr := os.Open(resultPath); openErr == nil {
		data, readErr := io.ReadAll(io.LimitReader(f, maxResultBytes+1))
		f.Close()
		if readErr == nil && len(data) <= maxResultBytes {
			var response Result
			if json.Unmarshal(data, &response) == nil {
				r.Success, r.Status, r.ErrorCode, r.Message = response.Success, response.Status, response.ErrorCode, response.Message
				r.Environment, r.Tasks, r.Task = response.Environment, response.Tasks, response.Task
				r.Downloads = response.Downloads
			}
		}
	}
	if processCtxErr != nil {
		r.Success = false
		idleTimedOut := errors.Is(processCause, errOutputIdleTimeout)
		r.TimedOut = idleTimedOut || errors.Is(processCtxErr, context.DeadlineExceeded)
		r.Cancelled = !r.TimedOut
		r.Status, r.ErrorCode, r.Message = "cancelled", "cancelled", "Local GEE execution was cancelled. Already submitted cloud tasks continue; use gee_task_status and, only when intended, gee_cancel_task."
		if r.TimedOut {
			r.Status, r.ErrorCode, r.Message = "timed_out", "timeout", "Local GEE execution exceeded its timeout. Already submitted cloud tasks continue; query their recorded IDs before resubmitting."
			if idleTimedOut {
				r.ErrorCode, r.Message = "idle_timeout", "Local GEE script produced no stdout/stderr output for its configured idle timeout. Use heartbeat() around long-running operations. Completed local files are preserved; already submitted cloud tasks continue, so query their recorded IDs before resubmitting."
			}
		}
	} else if runErr != nil {
		r.Success = false
		if r.ErrorCode == "" {
			r.Status, r.ErrorCode, r.Message = "failed", "process_failed", "GEE Python process failed: "+runErr.Error()
		}
	} else if r.Status == "" {
		r.Success, r.Status, r.ErrorCode, r.Message = false, "failed", "invalid_response", "GEE worker exited without a valid response. Script/process termination is not evidence of successful analysis."
	}
	// Persist bounded logs even on failure. Preserve script and generated files for review.
	for file, content := range map[string]string{"stdout.log": r.Stdout, "stderr.log": r.Stderr} {
		if writeErr := os.WriteFile(filepath.Join(runDir, file), []byte(content), 0600); writeErr != nil && r.Success {
			r.Success, r.Status, r.ErrorCode, r.Message = false, "failed", "write_artifact", writeErr.Error()
		}
	}
	r.ArtifactPaths = collectArtifacts(runDir)
	// The export helper journals IDs before waiting for server status, including on timeout.
	if len(r.Tasks) == 0 {
		r.Tasks = readTaskJournal(filepath.Join(runDir, "tasks.jsonl"))
	}
	if len(r.Downloads) == 0 {
		r.Downloads = readDownloadJournal(filepath.Join(runDir, "downloads.jsonl"))
	}
	r.ArtifactPaths = appendCompletedDownloadArtifacts(r.ArtifactPaths, r.Downloads, root)
	if !r.Success {
		return r, errors.New(r.Message)
	}
	return r, nil
}

var errOutputIdleTimeout = errors.New("GEE script output idle timeout")

// withOutputIdleTimeout records activity before any log truncation or delivery.
// The watchdog is independent of progress/UI callbacks and respects cancellation
// and deadlines inherited from the parent. The caller arms the watchdog after
// cmd.Start succeeds. Elapsed times use the monotonic clock.
func withOutputIdleTimeout(parent context.Context, idle time.Duration) (context.Context, context.CancelFunc, func(), func()) {
	ctx, cancelCause := context.WithCancelCause(parent)
	started := time.Now()
	var lastActivity atomic.Int64
	activity := func() {
		now := time.Since(started).Nanoseconds()
		// Concurrent stdout/stderr writers must not move the clock backwards if
		// one writer is descheduled between taking a timestamp and publishing it.
		for previous := lastActivity.Load(); now > previous; previous = lastActivity.Load() {
			if lastActivity.CompareAndSwap(previous, now) {
				break
			}
		}
	}
	var armOnce sync.Once
	arm := func() {
		armOnce.Do(func() {
			activity()
			go func() {
				timer := time.NewTimer(idle)
				defer timer.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-timer.C:
						remaining := idle - (time.Since(started) - time.Duration(lastActivity.Load()))
						if remaining <= 0 {
							cancelCause(errOutputIdleTimeout)
							return
						}
						timer.Reset(remaining)
					}
				}
			}()
		})
	}
	return ctx, func() { cancelCause(context.Canceled) }, activity, arm
}

type boundedBuffer struct {
	buffer    bytes.Buffer
	truncated bool
	sink      io.Writer
	onWrite   func()
}

func (b *boundedBuffer) String() string { return b.buffer.String() }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n > 0 && b.onWrite != nil {
		b.onWrite()
	}
	remaining := maxOutputBytes - b.buffer.Len()
	if len(p) > remaining {
		p, b.truncated = p[:remaining], true
	}
	_, _ = b.buffer.Write(p)
	if b.sink != nil {
		_, _ = b.sink.Write(p)
	}
	return n, nil
}

func runtimeEnv(proxy string) []string {
	env := map[string]string{}
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	// QGIS PYTHONHOME/PYTHONPATH contaminates independent conda/venv interpreters.
	delete(env, "PYTHONHOME")
	delete(env, "PYTHONPATH")
	env["PYTHONUTF8"], env["PYTHONIOENCODING"], env["PYTHONUNBUFFERED"] = "1", "utf-8", "1"
	env["PYTHONDONTWRITEBYTECODE"] = "1"
	if proxy != "" {
		for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
			env[key] = proxy
		}
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func collectArtifacts(root string) []string {
	var paths []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if len(paths) >= 200 {
			return filepath.SkipAll
		}
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "_gee_") {
			return filepath.SkipDir
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), "_gee_") || entry.Name() == "_worker_result.json" || strings.HasSuffix(entry.Name(), ".part") {
			return nil
		}
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			paths = append(paths, path)
		}
		return nil
	})
	return paths
}

// Forced process cancellation cannot run Python's finally blocks. Remove only
// the helper's private staging directories, preserving all published outputs.
func cleanupIncompleteDownloads(root string) {
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "_gee_download_") {
			_ = os.RemoveAll(path)
			return filepath.SkipDir
		}
		return nil
	})
}

func readTaskJournal(path string) []map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, maxResultBytes))
	var tasks []map[string]any
	for len(tasks) < 100 {
		var item map[string]any
		if decoder.Decode(&item) != nil {
			break
		}
		tasks = append(tasks, item)
	}
	return tasks
}

// Each journal entry is a complete snapshot. Keep only the latest complete
// record for each download so cancellation cannot lose a completed/reused file.
func readDownloadJournal(path string) []map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, maxResultBytes))
	decoder.UseNumber()
	var downloads []map[string]any
	indices := map[int64]int{}
	for {
		var item map[string]any
		if decoder.Decode(&item) != nil {
			break
		}
		idNumber, ok := item["download_id"].(json.Number)
		if !ok {
			continue
		}
		id, err := idNumber.Int64()
		if err != nil || id <= 0 {
			continue
		}
		if index, exists := indices[id]; exists {
			downloads[index] = item
		} else if len(downloads) < 100 {
			indices[id] = len(downloads)
			downloads = append(downloads, item)
		}
	}
	return downloads
}

func appendCompletedDownloadArtifacts(paths []string, downloads []map[string]any, root string) []string {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return paths
	}
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		seen[filepath.Clean(path)] = true
	}
	for _, download := range downloads {
		if download["status"] != "completed" {
			continue
		}
		path, ok := download["path"].(string)
		if !ok || !filepath.IsAbs(path) {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(root, resolved)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		path = filepath.Clean(path)
		if !seen[path] && len(paths) < 200 {
			paths = append(paths, path)
			seen[path] = true
		}
	}
	return paths
}
