package flowrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/planexec"
)

func TestCartoResourceChangeRequiresFreshAcknowledgement(t *testing.T) {
	_, ex, req := cartoFixture(t, 1)
	path := filepath.Join(req.SessionDir, "style.qml")
	if err := os.WriteFile(path, []byte("initial"), 0600); err != nil {
		t.Fatal(err)
	}
	ex.Resources = []flow.ResourcePlan{{Key: "style", Entry: path, Root: req.SessionDir, Type: "style"}}
	c, _ := NewController(Options{Store: FileRunStore{}, Runner: cartoRunner(func(context.Context, StepRequest) (StepResult, error) { return StepResult{Interrupted: true}, nil })})
	run, err := c.StartFromPlan(context.Background(), req, ex)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("changed contents"), 0600); err != nil {
		t.Fatal(err)
	}
	next, _ := NewController(c.options)
	resume := ResumeRequest{SessionID: req.SessionID, SessionDir: req.SessionDir, FlowRunID: run.ID}
	paused, err := next.CheckResume(context.Background(), resume)
	var change *ResourceChangeError
	if !errors.As(err, &change) || paused.ResourceChange == nil {
		t.Fatal(paused, err)
	}
	resume.ResourceChoice = "continue_snapshot"
	resume.ResourceFingerprint = change.Change.Fingerprint
	if err = os.WriteFile(path, []byte("changed again, after acknowledgement"), 0600); err != nil {
		t.Fatal(err)
	}
	paused, err = next.CheckResume(context.Background(), resume)
	if !errors.As(err, &change) || paused.ResourceChange.Fingerprint == resume.ResourceFingerprint {
		t.Fatal(paused, err)
	}
	resume.ResourceFingerprint = change.Change.Fingerprint
	restored, err := next.RestoreWaiting(context.Background(), resume)
	if err != nil || restored.Resources[0].Entry != path {
		t.Fatal(restored, err)
	}
}

func TestCartoPausedDiskRecoveryCanSkipOnlyOptionalStep(t *testing.T) {
	for _, optional := range []bool{true, false} {
		t.Run(map[bool]string{true: "optional", false: "required"}[optional], func(t *testing.T) {
			_, ex, req := cartoFixture(t, 2)
			ex.Steps[0].Optional = optional
			calls := map[string]int{}
			options := Options{Store: FileRunStore{}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
				calls[r.Step.ID]++
				if r.Step.ID == "a" {
					return StepResult{}, errors.New("unavailable")
				}
				return cartoReceipt(r), nil
			})}
			c, _ := NewController(options)
			run, err := c.StartFromPlan(context.Background(), req, ex)
			if err != nil || run.Status != "needs_replan" || calls["a"] != 2 {
				t.Fatal(run, err, calls)
			}
			saved, err := (FileRunStore{}).Load(context.Background(), req.SessionDir, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			next, _ := NewController(options)
			if err = next.AttachPaused(context.Background(), saved, ""); err != nil {
				t.Fatal(err)
			}
			run, err = next.SkipOptional(context.Background(), req.SessionID)
			if !optional {
				if err == nil || calls["b"] != 0 {
					t.Fatal(run, err, calls)
				}
				return
			}
			if err != nil || run.Status != "completed_with_issues" || calls["a"] != 2 || calls["b"] != 1 {
				t.Fatal(run, err, calls)
			}
		})
	}
}

func TestCartoFourthFailedPlanStopsAfterThreeRevisionsAcrossRestarts(t *testing.T) {
	p, ex, req := cartoFixture(t, 1)
	calls := 0
	options := Options{Store: FileRunStore{}, Runner: cartoRunner(func(context.Context, StepRequest) (StepResult, error) {
		calls++
		return StepResult{}, errors.New("source unavailable")
	})}
	c, _ := NewController(options)
	run, err := c.StartFromPlan(context.Background(), req, ex)
	if err != nil || run.Status != "needs_replan" {
		t.Fatal(run, err)
	}
	for revision := 1; revision <= 3; revision++ {
		saved, err := (FileRunStore{}).Load(context.Background(), req.SessionDir, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		nextController, _ := NewController(options)
		if err = nextController.AttachPaused(context.Background(), saved, ""); err != nil {
			t.Fatal(err)
		}
		next, _ := plan.Clone(p)
		next.Version = revision + 1
		run, err = nextController.Revise(context.Background(), req.SessionID, next, ex, req.Inputs, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := "needs_replan"
		if revision == 3 {
			want = "failed"
		}
		if run.Status != want || run.Revisions != revision {
			t.Fatal(run)
		}
	}
	if calls != 8 || len(run.PlanHistory) != 4 || !strings.Contains(run.Result.Summary, "三次计划修订上限") {
		t.Fatal(calls, run)
	}
}

func TestCartoAcquireCancellationStopsBeforeConsumer(t *testing.T) {
	_, ex, req := cartoFixture(t, 1)
	req.CartoPlan.Data = []plan.Data{{ID: "data", Name: "河流", From: "acquire", FindID: "df-fixture"}}
	ex.Steps[0].Kind = "acquire"
	ex.Steps[0].DataIDs = []string{"data"}
	started := make(chan struct{})
	done := make(chan struct{})
	c, _ := NewController(Options{Store: FileRunStore{}, Runner: cartoRunner(func(context.Context, StepRequest) (StepResult, error) {
		t.Fatal("consumer executed")
		return StepResult{}, nil
	}), DataGet: func(ctx context.Context, _ *RunState, _ plan.Data) (DataResult, error) {
		close(started)
		<-ctx.Done()
		return DataResult{}, ctx.Err()
	}})
	go func() {
		defer close(done)
		if _, err := c.StartFromPlan(context.Background(), req, ex); err != nil {
			t.Error(err)
		}
	}()
	<-started
	run, err := c.Cancel(context.Background(), req.SessionID)
	if err != nil || run.Status != "cancelled" {
		t.Fatal(run, err)
	}
	<-done
}

func TestCartoAcquireRetryPreservesAlreadyResolvedRows(t *testing.T) {
	_, ex, req := cartoFixture(t, 1)
	req.CartoPlan.Data = []plan.Data{{ID: "first", Name: "河流", From: "acquire", FindID: "df-one"}, {ID: "second", Name: "铁路", From: "acquire", FindID: "df-two"}}
	ex.Steps[0].Kind = "acquire"
	ex.Steps[0].DataIDs = []string{"first", "second"}
	calls := map[string]int{}
	c, _ := NewController(Options{Store: FileRunStore{}, Runner: cartoRunner(func(context.Context, StepRequest) (StepResult, error) {
		t.Fatal("acquisition called model runner")
		return StepResult{}, nil
	}), DataGet: func(_ context.Context, _ *RunState, d plan.Data) (DataResult, error) {
		calls[d.ID]++
		if d.ID == "second" && calls[d.ID] == 1 {
			return DataResult{}, errors.New("temporary fetch failure")
		}
		return DataResult{Value: d.ID + ".geojson", Notes: []string{d.Name + "：source.fixture"}}, nil
	}})
	run, err := c.StartFromPlan(context.Background(), req, ex)
	if err != nil || run.Status != "completed" || calls["first"] != 1 || calls["second"] != 2 || len(run.SourceNotes) != 2 {
		t.Fatal(run, err, calls)
	}
	saved, err := (FileRunStore{}).Load(context.Background(), req.SessionDir, run.ID)
	if err != nil || EffectiveInputs(saved)["first"] != "first.geojson" || EffectiveInputs(saved)["second"] != "second.geojson" {
		t.Fatal(saved, err)
	}
}

func TestRevisionCannotRewriteDataOfCompletedImplicitAcquire(t *testing.T) {
	p, ex, req := cartoFixture(t, 1)
	p.Data = []plan.Data{{ID: "data", Name: "河流", From: "acquire", FindID: "df-one", Choice: "source-a"}}
	p.Steps[0].Uses = []string{"data"}
	env := flow.Environment{Scheme: "test", Roles: ex.Roles, Packages: []flow.PackageSpec{{ID: "read", Kind: "category", Enabled: true, Tools: []string{"read"}}}}
	executable, inputs, err := planexec.Build(p, planexec.BuildOptions{Environment: env})
	if err != nil {
		t.Fatal(err)
	}
	req.Inputs = inputs
	c, _ := NewController(Options{Store: FileRunStore{}, Runner: cartoRunner(func(context.Context, StepRequest) (StepResult, error) {
		return StepResult{}, errors.New("consumer failed")
	}), DataGet: func(context.Context, *RunState, plan.Data) (DataResult, error) {
		return DataResult{Value: "original-source.geojson"}, nil
	}})
	run, err := c.StartFromPlan(context.Background(), req, executable)
	if err != nil || run.Status != "needs_replan" || run.Steps[0].Status != "completed" {
		t.Fatal(run, err)
	}
	next, _ := plan.Clone(p)
	next.Version = 2
	next.Data[0].Choice = "source-b"
	changed, values, err := planexec.Build(next, planexec.BuildOptions{Environment: env})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Revise(context.Background(), req.SessionID, next, changed, values, nil); err == nil {
		t.Fatal("completed hidden acquisition lost its protected source")
	}
	saved, err := c.Snapshot(req.SessionID)
	if err != nil || saved.Revisions != 0 || EffectiveInputs(saved)["data"] != "original-source.geojson" || saved.CartoPlan.Version != 1 {
		t.Fatal(saved, err)
	}
}
