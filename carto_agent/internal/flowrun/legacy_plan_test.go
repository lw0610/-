package flowrun

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestResumeReadsLegacyPlanFieldsAndSavesCanonicalSnapshot(t *testing.T) {
	old, start := interruptedResourceRun(t)
	raw, err := os.ReadFile(old.RunJSONPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	plan := saved["plan"].(map[string]any)
	plan["cache_key"] = plan["compile_key"]
	delete(plan, "compile_key")
	item := plan["resources"].([]any)[0].(map[string]any)
	item["resolved_path"] = item["entry"]
	delete(item, "entry")
	raw, _ = json.Marshal(saved)
	if err := os.WriteFile(old.RunJSONPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	current := resourcePlan("original", "/original/base.csv")
	c, _, _ := newTestController(t, current, func(_ context.Context, req StepRequest) (StepResult, error) {
		if len(req.Resources) != 1 || req.Resources[0].Entry != "/original/base.csv" {
			t.Fatalf("saved legacy resource lost: %+v", req.Resources)
		}
		return completed(req.Step), nil
	}, nil)
	resumed, err := c.Resume(context.Background(), ResumeRequest{SessionID: start.SessionID, SessionDir: start.SessionDir, FlowRunID: old.ID})
	if err != nil || resumed.Status != "completed" || resumed.Plan.CompileKey != "structure" {
		t.Fatalf("legacy run did not resume: %+v %v", resumed, err)
	}
	raw, err = os.ReadFile(old.RunJSONPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	plan = saved["plan"].(map[string]any)
	item = plan["resources"].([]any)[0].(map[string]any)
	if _, exists := plan["cache_key"]; exists {
		t.Fatal("legacy cache_key written back")
	}
	if _, exists := item["resolved_path"]; exists || item["entry"] != "/original/base.csv" {
		t.Fatal("legacy resource path written back", item)
	}
}
