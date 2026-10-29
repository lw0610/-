package flowrun

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/planexec"
	"path/filepath"
	"strings"
	"testing"
)

type cartoRunner func(context.Context, StepRequest) (StepResult, error)

func (f cartoRunner) Run(c context.Context, r StepRequest) (StepResult, error) { return f(c, r) }
func cartoFixture(t *testing.T, n int) (*plan.Plan, *flow.FlowPlan, StartRequest) {
	t.Helper()
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	p := &plan.Plan{APIVersion: plan.APIVersion, ID: "cp-fixture", Version: 1, Goal: "制图"}
	for i := 0; i < n; i++ {
		id := string(rune('a' + i))
		p.Steps = append(p.Steps, plan.Step{ID: id, Title: id, Capabilities: []string{"read"}, Instructions: "执行可核验操作", Outputs: []flow.OutputDeclaration{{Label: id, Type: "text"}}})
	}
	env := flow.Environment{Scheme: "test", Roles: []flow.RoleSpec{{ID: "worker", Enabled: true, Kind: "agent", ToolPackages: []string{"read"}}}, Packages: []flow.PackageSpec{{ID: "read", Kind: "category", Enabled: true, Tools: []string{"read"}}}}
	ex, in, err := planexec.Build(p, planexec.BuildOptions{Environment: env})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	return p, ex, StartRequest{SessionID: "carto", SessionDir: dir, ProjectRoot: dir, FlowID: p.ID, Scheme: "test", CartoPlan: p, Inputs: in}
}
func cartoReceipt(r StepRequest) StepResult {
	out := map[string]any{}
	for _, o := range r.Step.ExpectedOutputs {
		out[o.Label] = "verified"
	}
	raw, _ := json.Marshal(Receipt{Status: "completed", Outputs: out})
	return StepResult{LastContent: string(raw)}
}
func TestCartoPlanExecutesWithoutCompilerAndPersists(t *testing.T) {
	p, ex, req := cartoFixture(t, 2)
	calls := 0
	c, err := NewController(Options{Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		calls++
		if !strings.Contains(r.Messages[0].Content, "【可用数据】") {
			t.Fatal("missing context")
		}
		return cartoReceipt(r), nil
	}), Store: FileRunStore{}})
	if err != nil {
		t.Fatal(err)
	}
	state, err := c.StartFromPlan(context.Background(), req, ex)
	if err != nil || state.Status != "completed" || calls != 2 {
		t.Fatalf("%+v %v %d", state, err, calls)
	}
	saved, err := (FileRunStore{}).Load(context.Background(), req.SessionDir, state.ID)
	if err != nil || saved.CartoPlan.ID != p.ID || len(saved.PlanHistory) != 1 {
		t.Fatal(saved, err)
	}
}
func TestCartoFailureRetriesOnceThenRevisionPreservesCompleted(t *testing.T) {
	p, ex, req := cartoFixture(t, 2)
	calls := map[string]int{}
	fail := true
	c, _ := NewController(Options{Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		calls[r.Step.ID]++
		if r.Step.ID == "b" && fail {
			return StepResult{}, errors.New("真实工具失败")
		}
		return cartoReceipt(r), nil
	}), Store: FileRunStore{}})
	run, err := c.StartFromPlan(context.Background(), req, ex)
	if err != nil || run.Status != "needs_replan" || calls["a"] != 1 || calls["b"] != 2 {
		t.Fatalf("%+v %v %v", run, err, calls)
	}
	next, _ := plan.Clone(p)
	next.Version = 2
	next.Steps[0].Instructions = "重写完成步骤"
	if err = CheckRevision(run, next, ex); err == nil {
		t.Fatal("rewrote completed step")
	}
	next.Steps[0] = p.Steps[0]
	next.Steps[1].Instructions = "修订未完成部分"
	if err = CheckRevision(run, next, ex); err != nil {
		t.Fatal(err)
	}
	fail = false
	run, err = c.Revise(context.Background(), req.SessionID, next, ex, req.Inputs, nil)
	if err != nil || run.Status != "completed" || calls["a"] != 1 || calls["b"] != 3 || run.Revisions != 1 {
		t.Fatalf("%+v %v %v", run, err, calls)
	}
	if len(run.Steps[1].Attempts) != 3 {
		t.Fatal("revision erased previous attempts")
	}
}
func TestCartoFinalChecksCannotClaimCompletion(t *testing.T) {
	_, ex, req := cartoFixture(t, 1)
	c, _ := NewController(Options{Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) { return cartoReceipt(r), nil }), Store: FileRunStore{}, FinalChecks: func(context.Context, *RunState) Verification {
		return Verification{Errors: []string{"工程缺少图层"}}
	}})
	state, err := c.StartFromPlan(context.Background(), req, ex)
	if err != nil || state.Status != "needs_replan" || state.FinalVerification == nil || state.FinalVerification.OK {
		t.Fatal(state, err)
	}
}
func TestCartoRestartResumesOnlyUnfinishedStepAndLegacyReadOnly(t *testing.T) {
	_, ex, req := cartoFixture(t, 2)
	c, _ := NewController(Options{Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Step.ID == "b" {
			return StepResult{Interrupted: true}, nil
		}
		return cartoReceipt(r), nil
	}), Store: FileRunStore{}})
	state, err := c.StartFromPlan(context.Background(), req, ex)
	if err != nil {
		t.Fatal(err)
	}
	state.Status = "interrupted"
	if err = (FileRunStore{}).Save(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	restored, _ := NewController(Options{Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		calls = append(calls, r.Step.ID)
		return cartoReceipt(r), nil
	}), Store: FileRunStore{}})
	state, err = restored.Resume(context.Background(), ResumeRequest{SessionID: req.SessionID, SessionDir: req.SessionDir, FlowRunID: state.ID})
	if err != nil || state.Status != "completed" || strings.Join(calls, ",") != "b" {
		t.Fatal(state, err, calls)
	}
	state.CartoPlan = nil
	state.Status = "interrupted"
	state.Cursor = 1
	_ = (FileRunStore{}).Save(context.Background(), state)
	if _, err = restored.CheckResume(context.Background(), ResumeRequest{SessionID: req.SessionID, SessionDir: req.SessionDir, FlowRunID: state.ID}); err == nil || !strings.Contains(err.Error(), "只读") {
		t.Fatal(err)
	}
}
func TestCartoDataStepUsesConfirmedBindingBeforeConsumer(t *testing.T) {
	p, ex, req := cartoFixture(t, 1)
	p.Data = []plan.Data{{ID: "data", Name: "铁路", From: "acquire", FindID: "f"}}
	p.Steps[0].Uses = []string{"data"}
	ex.Steps[0].DataUses = []string{"data"}
	ex.Steps[0].Refs.Inputs = []string{"data"}
	ex.Inputs = []flow.InputPlan{{Key: "data", Label: "铁路", Type: "text"}}
	ex.Steps = append([]flow.StepPlan{{ID: "acquire-data", Kind: "acquire", DataIDs: []string{"data"}}}, ex.Steps...)
	path := filepath.Join(req.SessionDir, "rail.geojson")
	downloaded := false
	c, _ := NewController(Options{Store: FileRunStore{}, DataGet: func(context.Context, *RunState, plan.Data) (DataResult, error) {
		downloaded = true
		return DataResult{Value: path, Notes: []string{"fixture attribution"}}, nil
	}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		if !downloaded || !strings.Contains(r.Messages[0].Content, path) {
			t.Fatal("consumer ran before binding")
		}
		return cartoReceipt(r), nil
	})})
	state, err := c.StartFromPlan(context.Background(), req, ex)
	if err != nil || state.Status != "completed" || state.AcquiredInputs["data"] != path {
		t.Fatal(state, err)
	}
}
