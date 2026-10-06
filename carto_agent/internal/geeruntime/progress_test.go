package geeruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProgressIgnoresIncompleteAndInvalidRecords(t *testing.T) {
	p := filepath.Join(t.TempDir(), "progress.jsonl")
	valid := `{"type":"download_progress","download_id":1,"filename":"/tmp/image.tif","phase":"downloading","percent":25}` + "\n"
	invalid := `{"type":"download_progress","download_id":1,"filename":"image.tif","phase":"downloading","percent":101}` + "\n"
	partial := `{"type":"download_progress","filename":"unfinished`
	if err := os.WriteFile(p, []byte(strings.Repeat("x", 70000)+"\n"+valid+invalid+partial), 0600); err != nil {
		t.Fatal(err)
	}
	event, ok := latestProgress(p)
	if !ok || event.Filename != "image.tif" || event.Percent == nil || *event.Percent != 25 {
		t.Fatalf("invalid or partial progress superseded complete record: %+v", event)
	}
}

func TestWorkflowStagesReportElapsedTimeAndHaveSeparateIdentity(t *testing.T) {
	p := filepath.Join(t.TempDir(), "progress.jsonl")
	valid := `{"type":"workflow_progress","stage_id":2,"stage":"筛选候选日期","phase":"running","elapsed_seconds":30}` + "\n"
	invalid := `{"type":"workflow_progress","stage_id":0,"stage":"bad","phase":"running","elapsed_seconds":30}` + "\n"
	if err := os.WriteFile(p, []byte(valid+invalid), 0600); err != nil {
		t.Fatal(err)
	}
	event, ok := latestProgress(p)
	if !ok || event.Type != "workflow_progress" || event.StageID != 2 || !strings.Contains(event.Text(), "30 秒") || !strings.HasSuffix(event.RequestKey, ":stage:2") {
		t.Fatalf("workflow progress was lost or has an ambiguous identity: %+v", event)
	}
	if err := os.WriteFile(p, []byte(`{"type":"workflow_progress","stage_id":2,"stage":"筛选候选日期","phase":"failed","elapsed_seconds":31}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	failed, ok := latestProgress(p)
	if !ok || failed.RequestKey != event.RequestKey || !strings.Contains(failed.Text(), "失败") {
		t.Fatalf("failure did not update the same stage: %+v", failed)
	}
}

func TestProgressCallbackCannotBlockCancellation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "progress.jsonl")
	_ = os.WriteFile(p, []byte(`{"type":"download_progress","download_id":1,"filename":"image.tif","phase":"downloading","percent":25}`+"\n"), 0600)
	entered, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer close(release)
	stop := watchProgress(ctx, p, func(Progress) { close(entered); <-release })
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("callback never ran")
	}
	cancel()
	finished := make(chan struct{})
	go func() { stop(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("cancelled download blocked on display callback")
	}
}

func TestDisplayDelayDoesNotChangeSuccessfulProcessToTimeout(t *testing.T) {
	opts := mockOptions(t)
	opts.TimeoutSeconds = 1
	release := make(chan struct{})
	defer close(release)
	opts.OnProgress = func(Progress) { <-release }
	code := "(output_dir / 'progress.jsonl').write_text('{\"type\":\"download_progress\",\"download_id\":1,\"filename\":\"image.tif\",\"phase\":\"completed\",\"percent\":100}\\n')"
	r, err := invokeResult(t, context.Background(), "gee_run_script", map[string]any{"code": code}, opts)
	if err != nil || !r.Success || r.TimedOut {
		t.Fatalf("UI delivery delay changed completed Python result: %+v %v", r, err)
	}
}

func TestRunReportsProgressAndLogsBeforeCompletion(t *testing.T) {
	opts := mockOptions(t)
	opts.TimeoutSeconds = 8
	release := filepath.Join(t.TempDir(), "release")
	releaseJSON, _ := json.Marshal(release)
	progress := make(chan Progress, 4)
	ctx := WithProgressCallback(context.Background(), func(p Progress) { progress <- p })
	code := "import json, time\nfrom pathlib import Path\n" +
		"print('download started', flush=True)\n" +
		"(output_dir / 'progress.jsonl').write_text(json.dumps({'type': 'download_progress', 'download_id': 1, 'filename': 'image.tif', 'phase': 'downloading', 'percent': 25}) + '\\n')\n" +
		"while not Path(" + string(releaseJSON) + ").exists(): time.sleep(0.05)\n" +
		"(output_dir / '_gee_download_incomplete').mkdir()\n(output_dir / '_gee_download_incomplete' / 'tile.tif').write_bytes(b'partial')\n" +
		"(output_dir / 'unfinished.tif.part').write_bytes(b'partial')\n(output_dir / 'image.tif').write_bytes(b'complete')"
	done := make(chan *Result, 1)
	go func() {
		raw, _ := Invoke(ctx, "gee_run_script", map[string]any{"code": code}, opts)
		done <- raw.(*Result)
	}()
	defer os.WriteFile(release, nil, 0600)
	select {
	case p := <-progress:
		if p.Percent == nil || *p.Percent != 25 {
			t.Fatalf("wrong live progress: %+v", p)
		}
	case r := <-done:
		t.Fatalf("process ended before live progress: %+v", r)
	case <-time.After(5 * time.Second):
		t.Fatal("no progress while process was running")
	}
	logs, err := filepath.Glob(filepath.Join(opts.OutputDir, "run-*", "stdout.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("missing live log: %v %v", logs, err)
	}
	data, err := os.ReadFile(logs[0])
	if err != nil || !strings.Contains(string(data), "download started") {
		t.Fatalf("stdout was withheld until completion: %q %v", data, err)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if !r.Success {
		t.Fatalf("script failed: %+v", r)
	}
	for _, path := range r.ArtifactPaths {
		if strings.Contains(path, "_gee_download_") || strings.HasSuffix(path, ".part") {
			t.Fatalf("partial download published as artifact: %s", path)
		}
	}
}
