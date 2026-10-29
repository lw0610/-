package flowrun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/plan"
)

func TestV7ResumeHistoricalBudgetPauseKeepsCompletedWork(t *testing.T) {
	ex, req := preparationV7Fixture(t, []plan.Data{{ID: "table", Name: "本地表", Kind: "table", Source: "retrieve", Need: &plan.Need{Category: "stats.annual_indicator"}}}, []plan.Step{
		{ID: "prepared", Title: "已完成步骤", Inputs: []string{"table"}},
		{ID: "remaining", Title: "待继续步骤", Inputs: []string{"table"}},
	})
	path := filepath.Join(req.SessionDir, "table.csv")
	if err := os.WriteFile(path, []byte("name,value\nfixture,1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gets := 0
	options := Options{Store: FileRunStore{}, DataGet: func(ctx context.Context, _ *RunState, _ plan.Data) (DataResult, error) {
		gets++
		if _, set := ctx.Deadline(); set {
			t.Fatal("data preparation added an execution deadline")
		}
		return DataResult{Value: path}, nil
	}, Runner: cartoRunner(func(ctx context.Context, r StepRequest) (StepResult, error) {
		if _, set := ctx.Deadline(); set {
			t.Fatal("step added an execution deadline")
		}
		if r.Step.ID == "remaining" {
			return StepResult{Interrupted: true}, nil
		}
		return cartoReceipt(r), nil
	})}
	c, err := NewController(options)
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.StartFromPlan(context.Background(), req, ex)
	if err != nil || run.Status != "waiting_user" || gets != 1 {
		t.Fatal(run, err, gets)
	}
	// Persist the old budget fields exactly at the input boundary, without
	// retaining their deleted Go types or changing any real session.
	raw, err := os.ReadFile(run.RunJSONPath)
	if err != nil {
		t.Fatal(err)
	}
	var historical map[string]any
	if err = json.Unmarshal(raw, &historical); err != nil {
		t.Fatal(err)
	}
	historical["status"] = "interrupted"
	historical["budget_started_at"] = "2020-01-01T00:00:00Z"
	historical["budget"] = map[string]any{"limit_seconds": 3600, "elapsed_seconds": 3600, "tool_calls": 60}
	step := historical["steps"].([]any)[1].(map[string]any)
	step["status"] = "interrupted"
	step["last_failure"] = "EXECUTION_BUDGET_REACHED: 已达到 60 次工具调用预算"
	step["attempts"].([]any)[0].(map[string]any)["budget"] = map[string]any{"limit_seconds": 600, "max_tool_calls": 60, "elapsed_seconds": 600, "tool_calls": 60}
	raw, err = json.Marshal(historical)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(run.RunJSONPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var steps []string
	options.Runner = cartoRunner(func(ctx context.Context, r StepRequest) (StepResult, error) {
		steps = append(steps, r.Step.ID)
		if _, set := ctx.Deadline(); set {
			t.Fatal("continuation added an execution deadline")
		}
		return cartoReceipt(r), nil
	})
	restored, err := NewController(options)
	if err != nil {
		t.Fatal(err)
	}
	run, err = restored.Resume(context.Background(), ResumeRequest{SessionID: req.SessionID, SessionDir: req.SessionDir, FlowRunID: run.ID})
	if err != nil || run.Status != "completed" || strings.Join(steps, ",") != "remaining" || gets != 1 || run.AcquiredInputs["table"] != path {
		t.Fatal(run, err, steps, gets)
	}
	if run.Steps[0].Outputs["result_prepared"] != "verified" || len(run.Steps[0].Attempts) != 1 || len(run.Steps[1].Attempts) != 2 {
		t.Fatal("continuation discarded completed work or historical attempts", run.Steps)
	}
}
