package flowrun

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/planexec"
)

func TestStepDataUsesOutputIdentifierWhilePreservingDisplayName(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "显示名称独立"
		output := "verified_boundary"
		if legacy {
			name, output = output, ""
		}
		t.Run(name, func(t *testing.T) {
			p, executable, request := cartoFixture(t, 2)
			p.Data = []plan.Data{{ID: "boundary", Name: name, From: "step", Step: "a", Output: output}}
			p.Steps[0].Outputs = []flow.OutputDeclaration{{Label: "verified_boundary", Type: "text"}}
			p.Steps[1].Uses = []string{"boundary"}
			env := flow.Environment{Roles: executable.Roles, Packages: []flow.PackageSpec{{ID: "read", Enabled: true, Kind: "category", Tools: []string{"read"}}}}
			var err error
			executable, request.Inputs, err = planexec.Build(p, planexec.BuildOptions{Environment: env})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			controller, _ := NewController(Options{Store: FileRunStore{}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
				calls++
				if r.Step.ID == "b" && !strings.Contains(r.Messages[0].Content, "- "+name+"：\"verified\"（step）") {
					t.Fatalf("consumer lost its named upstream output: %s", r.Messages[0].Content)
				}
				return cartoReceipt(r), nil
			})})
			state, err := controller.StartFromPlan(context.Background(), request, executable)
			if err != nil || state.Status != "completed" || calls != 2 {
				t.Fatal(state, err, calls)
			}
		})
	}
}

func TestRevisionAcceptsExplicitEquivalentOfCompletedLegacyOutput(t *testing.T) {
	p, executable, request := cartoFixture(t, 3)
	p.Data = []plan.Data{{ID: "boundary", Name: "verified_boundary", From: "step", Step: "a"}}
	p.Steps[0].Outputs = []flow.OutputDeclaration{{Label: "verified_boundary", Type: "text"}}
	p.Steps[1].Uses = []string{"boundary"}
	env := flow.Environment{Roles: executable.Roles, Packages: []flow.PackageSpec{{ID: "read", Enabled: true, Kind: "category", Tools: []string{"read"}}}}
	var err error
	executable, request.Inputs, err = planexec.Build(p, planexec.BuildOptions{Environment: env})
	if err != nil {
		t.Fatal(err)
	}
	controller, _ := NewController(Options{Store: FileRunStore{}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Step.ID == "c" {
			return StepResult{}, errors.New("needs revision")
		}
		return cartoReceipt(r), nil
	})})
	state, err := controller.StartFromPlan(context.Background(), request, executable)
	if err != nil || state.Status != "needs_replan" || state.Steps[1].Status != "completed" {
		t.Fatal(state, err)
	}
	next, _ := plan.Clone(p)
	next.Version++
	next.Data[0].Output = "verified_boundary"
	if err = CheckRevision(state, next, executable); err != nil {
		t.Fatal("equivalent normalized reference rejected for a completed input", err)
	}
	next.Data[0].Output = "another_output"
	if err = CheckRevision(state, next, executable); err == nil {
		t.Fatal("real output-reference change was allowed for a completed input")
	}
}
