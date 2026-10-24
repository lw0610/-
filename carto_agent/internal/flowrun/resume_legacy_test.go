package flowrun

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
)

func legacyResumePlans(t *testing.T) (*flow.FlowPlan, *flow.FlowPlan) {
	t.Helper()
	old := testPlan(1)
	old.Compiler.Version = "2"
	old.CompileKey = "v2-key"
	old.Inputs = []flow.InputPlan{{Key: "old_source", Label: "材料", Type: "text", Required: true}}
	old.Steps[0].Refs.Inputs = []string{"old_source"}
	old.Steps[0].Condition = &flow.ConditionDoc{Rule: &flow.ConditionRule{Op: "provided", Input: "old_source"}}
	old.InputConstraints = []flow.InputConstraint{{Op: "at_least_one", Inputs: []string{"old_source"}}}
	var err error
	old.LinkFingerprint, err = flow.ResourceSnapshotFingerprint(old)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(old)
	var current flow.FlowPlan
	_ = json.Unmarshal(raw, &current)
	current.Compiler.Version = "3"
	current.CompileKey = "v3-key"
	current.Inputs[0].Key = "材料"
	current.Steps[0].Refs.Inputs = []string{"材料"}
	current.Steps[0].Condition.Rule.Input = "材料"
	current.InputConstraints[0].Inputs = []string{"材料"}
	current.LinkFingerprint = "new-algorithm-link"
	return old, &current
}
func TestLegacyV2RunResumesAcrossCompilerKeyUpgradeWithoutRewritingSnapshot(t *testing.T) {
	old, current := legacyResumePlans(t)
	calls := 0
	c, request, _ := newTestController(t, old, func(_ context.Context, r StepRequest) (StepResult, error) {
		calls++
		if calls == 1 {
			return StepResult{Interrupted: true}, nil
		}
		if len(r.Step.Refs.Inputs) != 1 || r.Step.Refs.Inputs[0] != "old_source" {
			t.Error("snapshot keys rewritten", r.Step)
		}
		return completed(r.Step), nil
	}, nil)
	request.Inputs = map[string]any{"old_source": "原输入"}
	run, err := c.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	run.Status = "interrupted"
	if err = (FileRunStore{}).Save(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	restored, err := NewController(Options{Compiler: compilerStub{plan: current}, Runner: c.options.Runner, Store: FileRunStore{}})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := restored.Resume(context.Background(), ResumeRequest{SessionID: request.SessionID, SessionDir: request.SessionDir, FlowRunID: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != "completed" || resumed.Inputs["old_source"] != "原输入" || resumed.Plan.CompileKey != "v2-key" || resumed.Plan.Inputs[0].Key != "old_source" {
		t.Fatal(resumed)
	}
	if _, exists := resumed.Inputs["材料"]; exists {
		t.Fatal("new key entered old snapshot")
	}
}
func TestLegacyV2RunRejectsChangedExecutableStructure(t *testing.T) {
	for _, change := range []string{"instructions", "role_tools", "condition", "output", "required_input"} {
		t.Run(change, func(t *testing.T) {
			old, current := legacyResumePlans(t)
			c, request, _ := newTestController(t, old, func(context.Context, StepRequest) (StepResult, error) { return StepResult{Interrupted: true}, nil }, nil)
			request.Inputs = map[string]any{"old_source": "原输入"}
			run, err := c.Start(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "instructions":
				current.Steps[0].Instructions = "新的处理方式"
			case "role_tools":
				current.Roles[0].Tools = append(current.Roles[0].Tools, "new-tool")
			case "condition":
				current.Steps[0].Condition.Rule.Op = "not_provided"
			case "output":
				current.Steps[0].ExpectedOutputs[0].Type = "file"
			case "required_input":
				current.Inputs = append(current.Inputs, flow.InputPlan{Key: "底图", Label: "底图", Type: "folder", Required: true})
			}
			c.options.Compiler = compilerStub{plan: current}
			_, err = c.CheckResume(context.Background(), ResumeRequest{SessionID: request.SessionID, SessionDir: request.SessionDir, FlowRunID: run.ID})
			if err == nil || !strings.Contains(err.Error(), "结构已改变") {
				t.Fatal(change, err)
			}
		})
	}
}
