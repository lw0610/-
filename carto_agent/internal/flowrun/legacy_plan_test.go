package flowrun

import (
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/flow"
	"os"
	"strings"
	"testing"
)

func TestLegacyPlanFieldsRemainReadableWithoutExecuting(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p := testPlan(1)
	p.CompileKey = "structure"
	p.Resources = []flow.ResourcePlan{{Key: "base", Entry: "/original/base.csv"}}
	old := &RunState{APIVersion: APIVersion, ID: "legacy-run", SessionID: "session", SessionDir: dir, Status: "interrupted", Plan: p, Steps: []StepState{{ID: "s1", Status: "pending"}}}
	store := FileRunStore{}
	if err := store.Save(ctx, old); err != nil {
		t.Fatal(err)
	}
	path, _ := RunPath(dir, old.ID)
	raw, _ := os.ReadFile(path)
	var saved map[string]any
	_ = json.Unmarshal(raw, &saved)
	snapshot := saved["plan"].(map[string]any)
	snapshot["cache_key"] = snapshot["compile_key"]
	delete(snapshot, "compile_key")
	item := snapshot["resources"].([]any)[0].(map[string]any)
	item["resolved_path"] = item["entry"]
	delete(item, "entry")
	raw, _ = json.Marshal(saved)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(ctx, dir, old.ID)
	if err != nil || loaded.Plan.CompileKey != "structure" || loaded.Plan.Resources[0].Entry != "/original/base.csv" {
		t.Fatal(loaded, err)
	}
	c, _ := NewController(Options{Store: store, Runner: runnerFunc(func(context.Context, StepRequest) (StepResult, error) {
		t.Fatal("historical run executed")
		return StepResult{}, nil
	})})
	if _, err = c.Resume(ctx, ResumeRequest{SessionID: old.SessionID, SessionDir: dir, FlowRunID: old.ID}); err == nil || !strings.Contains(err.Error(), "只读") {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("read rewrote historical record")
	}
}
