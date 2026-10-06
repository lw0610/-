package geeruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDownloadJournalKeepsLastCompleteRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "downloads.jsonl")
	content := `{"download_id":1,"status":"preparing"}
{"download_id":2,"status":"preparing"}
{"download_id":1,"status":"completed","path":"/tmp/completed.tif"}
{"download_id":2,"status":"failed"}
{"download_id":0,"status":"completed"}
{"download_id":1,"status":`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	got := readDownloadJournal(path)
	if len(got) != 2 || got[0]["status"] != "completed" || got[1]["status"] != "failed" {
		t.Fatalf("download recovery lost latest complete states: %+v", got)
	}
}

func TestReusedDownloadArtifactsMustBeCompletedFilesInsideRoot(t *testing.T) {
	root := t.TempDir()
	complete := filepath.Join(root, "existing.tif")
	outside := filepath.Join(t.TempDir(), "outside.tif")
	for _, path := range []string{complete, outside} {
		if err := os.WriteFile(path, []byte("complete"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	downloads := []map[string]any{
		{"status": "completed", "path": complete},
		{"status": "completed", "path": complete},
		{"status": "completed", "path": outside},
		{"status": "completed", "path": filepath.Join(root, "missing.tif")},
		{"status": "completed", "path": root},
		{"status": "failed", "path": complete},
	}
	link := filepath.Join(root, "link.tif")
	if err := os.Symlink(outside, link); err == nil {
		downloads = append(downloads, map[string]any{"status": "completed", "path": link})
	}
	parentLink := filepath.Join(root, "linked-directory")
	if err := os.Symlink(filepath.Dir(outside), parentLink); err == nil {
		downloads = append(downloads, map[string]any{"status": "completed", "path": filepath.Join(parentLink, filepath.Base(outside))})
	}
	got := appendCompletedDownloadArtifacts(nil, downloads, root)
	if len(got) != 1 || got[0] != complete {
		t.Fatalf("unsafe, missing or duplicate artifact was published: %v", got)
	}
	got = appendCompletedDownloadArtifacts(got, downloads, root)
	if len(got) != 1 {
		t.Fatalf("existing artifact was duplicated: %v", got)
	}
}

func TestIdleTimeoutRecoversCompletedReusedDownload(t *testing.T) {
	opts := mockOptions(t)
	opts.TimeoutSeconds = 1
	oldRun := filepath.Join(opts.OutputDir, "run-previous")
	if err := os.MkdirAll(oldRun, 0700); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(oldRun, "completed.tif")
	if err := os.WriteFile(previous, []byte("completed raster"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err := json.Marshal(map[string]any{"download_id": 1, "status": "completed", "path": previous, "reused": true})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := json.Marshal(string(entry) + "\n")
	code := "(output_dir / 'downloads.jsonl').write_text(" + string(content) + ")\nimport time\ntime.sleep(30)"
	r, err := invokeResult(t, context.Background(), "gee_run_script", map[string]any{"code": code}, opts)
	if err == nil || !r.TimedOut || r.ErrorCode != "idle_timeout" || len(r.Downloads) != 1 || r.Downloads[0]["path"] != previous || !slices.Contains(r.ArtifactPaths, previous) {
		t.Fatalf("completed reuse disappeared when a later operation stalled: %+v error=%v", r, err)
	}
}
