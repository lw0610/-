package flowrun

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/schema"
)

func TestDiscoveryRestartRestoresCheckpointWithoutStartingAnotherExpert(t *testing.T) {
	executable, request := discoveryFixture(t, "population")
	request.Selections = map[string]json.RawMessage{"population": json.RawMessage(`{"find_id":"find-population","rows":[{"route":"explore"}]}`)}
	registered := false
	gets, experts, consumers := 0, 0, 0
	options := Options{Store: FileRunStore{}, DataGet: func(_ context.Context, _ *RunState, d plan.Data) (DataResult, error) {
		gets++
		if registered {
			return DataResult{Value: "/verified/population.csv"}, nil
		}
		return discoveryPreparation(d), nil
	}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Acquisition != nil {
			experts++
			return StepResult{Interrupted: true}, nil
		}
		consumers++
		return cartoReceipt(r), nil
	})}
	first, _ := NewController(options)
	state, err := first.StartFromPlan(context.Background(), request, executable)
	if err != nil || state.Status != "waiting_user" || experts != 1 {
		t.Fatal(state, err, experts)
	}
	checkpoint := state.Steps[0].Attempts[0].RunID
	// Registration may have succeeded before a subsequent question interrupted
	// the expert. Restoring must not call DataGet or replace its ADK checkpoint.
	registered = true
	restarted, _ := NewController(options)
	resume := ResumeRequest{SessionID: request.SessionID, SessionDir: request.SessionDir, FlowRunID: state.ID}
	for i := 0; i < 2; i++ {
		restored, err := restarted.RestoreWaiting(context.Background(), resume)
		if err != nil || restored.RunID != checkpoint || restored.Role.ID != "source_researcher" || restored.Acquisition == nil || restored.Acquisition.DataID != "population" || len(restored.Messages) != 0 {
			t.Fatalf("restart lost the existing expert checkpoint: %+v %v", restored, err)
		}
		var got, want any
		if json.Unmarshal(restored.Acquisition.Selection, &got) != nil || json.Unmarshal(request.Selections["population"], &want) != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("restart changed frozen selection", got, want)
		}
		if gets != 1 || experts != 1 || consumers != 0 {
			t.Fatalf("restore executed acquisition: gets=%d experts=%d consumers=%d", gets, experts, consumers)
		}
	}
	state, err = restarted.OnStepRunFinished(context.Background(), checkpoint, StepResult{LastContent: "登记完成"})
	if err != nil || state.Status != "completed" || experts != 1 || consumers != 1 || gets != 2 || len(state.Steps[0].Attempts) != 1 {
		t.Fatalf("restored checkpoint repeated acquisition or failed to collect registration: %+v %v", state, err)
	}
}

func TestPreparationRestartRestoresConfiguredRoleAndNeed(t *testing.T) {
	executable, request := preparationV7Fixture(t,
		[]plan.Data{{ID: "population", Name: "人口", Source: "retrieve", Kind: "table"}},
		[]plan.Step{{ID: "draw", Title: "制图", Inputs: []string{"population"}}})
	role := flow.RoleSpec{ID: "configured_collector", DisplayName: "当前方案数据角色", Enabled: true, Kind: "agent", Provider: "data-model", Tools: []string{"data_register"}}
	executable.Roles = append(executable.Roles, role)
	need := json.RawMessage(`{"need_id":"cp-prepare-v7/population","need":{"name":"人口"}}`)
	registered := false
	gets, collectors, consumers := 0, 0, 0
	options := Options{Store: FileRunStore{}, DataGet: func(_ context.Context, _ *RunState, d plan.Data) (DataResult, error) {
		gets++
		if registered {
			return DataResult{Value: "/verified/population.csv"}, nil
		}
		return DataResult{Preparation: &DataPreparation{DataID: d.ID, Need: need, Role: role, Messages: []*schema.Message{schema.UserMessage("按已确认需求取数并登记")}}}, nil
	}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Acquisition != nil {
			collectors++
			if r.Role.ID != role.ID || r.Mode != "prepare" {
				t.Fatal("wrong acquisition role", r)
			}
			return StepResult{Interrupted: true}, nil
		}
		consumers++
		if r.Role.ID != "worker" {
			t.Fatal("acquisition role replaced the consumer", r.Role)
		}
		return cartoReceipt(r), nil
	})}
	first, _ := NewController(options)
	state, err := first.StartFromPlan(context.Background(), request, executable)
	if err != nil || state.Status != "waiting_user" || state.Steps[0].Role != role.ID || state.Steps[0].Attempts[0].Role != role.ID {
		t.Fatal("preparation lost the actual checkpoint role", state, err)
	}
	checkpoint := state.Steps[0].Attempts[0].RunID
	registered = true
	restarted, _ := NewController(options)
	restored, err := restarted.RestoreWaiting(context.Background(), ResumeRequest{SessionID: request.SessionID, SessionDir: request.SessionDir, FlowRunID: state.ID})
	if err != nil || restored.RunID != checkpoint || !reflect.DeepEqual(restored.Role, role) || restored.Acquisition == nil || len(restored.Messages) != 0 || gets != 1 || collectors != 1 || consumers != 0 {
		t.Fatalf("restore changed role, need or re-executed discovery: %+v %v; gets=%d collectors=%d consumers=%d", restored, err, gets, collectors, consumers)
	}
	var gotNeed, wantNeed any
	if json.Unmarshal(restored.Acquisition.Need, &gotNeed) != nil || json.Unmarshal(need, &wantNeed) != nil || !reflect.DeepEqual(gotNeed, wantNeed) {
		t.Fatal("restored preparation changed its frozen need", gotNeed, wantNeed)
	}
	state, err = restarted.OnStepRunFinished(context.Background(), checkpoint, StepResult{LastContent: "数据已登记"})
	if err != nil || state.Status != "completed" || collectors != 1 || consumers != 1 || gets != 2 {
		t.Fatal("resumed registration did not reach the original consumer", state, err, gets, collectors, consumers)
	}
}

func TestPreparationRestoreRefusesLegacyCheckpointWithoutActualRole(t *testing.T) {
	controller, _ := NewController(Options{Store: FileRunStore{}, Runner: cartoRunner(func(context.Context, StepRequest) (StepResult, error) {
		t.Fatal("restore must not run any agent")
		return StepResult{}, nil
	})})
	state := &RunState{
		Plan: &flow.FlowPlan{
			Roles: []flow.RoleSpec{{ID: "worker", Enabled: true, Kind: "agent"}},
			Steps: []flow.StepPlan{{ID: "draw", Role: "worker", DataUses: []string{"population"}}},
		},
		CartoPlan: &plan.Plan{Data: []plan.Data{{ID: "population"}}},
		DataNeeds: map[string]json.RawMessage{"population": json.RawMessage(`{"need_id":"population"}`)},
		Steps:     []StepState{{Role: "worker"}},
	}
	for _, attempt := range []Attempt{{Mode: "prepare", DataID: "population"}, {Mode: "prepare", Role: "removed_legacy_role", DataID: "population"}} {
		if _, err := controller.restoreStepRequest(state, "", attempt); err == nil || !strings.Contains(err.Error(), "修订当前计划") {
			t.Fatalf("legacy checkpoint was silently assigned to consumer: %+v %v", attempt, err)
		}
	}
}

func TestRevisionInvalidatesOldDiscoveryCompletions(t *testing.T) {
	executable, request := discoveryFixture(t, "population")
	registered := false
	experts, consumers := 0, 0
	controller, _ := NewController(Options{Store: FileRunStore{}, DataGet: func(_ context.Context, _ *RunState, d plan.Data) (DataResult, error) {
		if registered {
			return DataResult{Value: "/verified/population.csv"}, nil
		}
		return discoveryPreparation(d), nil
	}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Acquisition != nil {
			experts++
			return StepResult{Interrupted: true}, nil
		}
		consumers++
		return cartoReceipt(r), nil
	})})
	state, err := controller.StartFromPlan(context.Background(), request, executable)
	if err != nil || state.Status != "waiting_user" {
		t.Fatal(state, err)
	}
	oldRunID := state.Steps[0].Attempts[0].RunID
	next, _ := plan.Clone(request.CartoPlan)
	next.Version++
	state, err = controller.Revise(context.Background(), request.SessionID, next, executable, request.Inputs, request.Selections)
	if err != nil || state.Status != "waiting_user" || experts != 2 {
		t.Fatal(state, err, experts)
	}
	attempts := state.Steps[state.Cursor].Attempts
	currentRunID := attempts[len(attempts)-1].RunID
	if currentRunID == oldRunID {
		t.Fatal("revision reused the old checkpoint identity")
	}
	controller.mu.Lock()
	_, stale := controller.waiting[oldRunID]
	controller.mu.Unlock()
	if stale {
		t.Fatal("revision retained the old checkpoint registration")
	}
	before, _ := controller.Snapshot(request.SessionID)
	for _, late := range []StepResult{{LastContent: "旧专家完成"}, {Interrupted: true}} {
		if got, err := controller.OnStepRunFinished(context.Background(), oldRunID, late); err != nil || got != nil {
			t.Fatal("old checkpoint callback was accepted", got, err)
		}
	}
	// Also model a callback which obtained the old lookup before revision. A
	// stale map entry alone must not bypass the current-attempt check under e.mu.
	controller.mu.Lock()
	controller.waiting[oldRunID] = state.ID
	controller.mu.Unlock()
	if got, err := controller.OnStepRunFinished(context.Background(), oldRunID, StepResult{LastContent: "迟到的旧结果"}); err != nil || got != nil {
		t.Fatal("stale lookup was applied to the new cursor", got, err)
	}
	after, _ := controller.Snapshot(request.SessionID)
	if !reflect.DeepEqual(before, after) || experts != 2 || consumers != 0 {
		t.Fatal("late callback changed the revised plan", before, after, experts, consumers)
	}
	registered = true
	state, err = controller.OnStepRunFinished(context.Background(), currentRunID, StepResult{LastContent: "新专家完成"})
	if err != nil || state.Status != "completed" || experts != 2 || consumers != 1 || len(state.Steps[0].Attempts) != 2 {
		t.Fatal(state, err, experts, consumers)
	}
}
