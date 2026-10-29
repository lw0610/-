package flowrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/schema"
)

func discoveryFixture(t *testing.T, ids ...string) (*flow.FlowPlan, StartRequest) {
	t.Helper()
	p, executable, request := cartoFixture(t, 1)
	for _, id := range ids {
		p.Data = append(p.Data, plan.Data{ID: id, Name: id, From: "acquire", FindID: "find-" + id})
		executable.Inputs = append(executable.Inputs, flow.InputPlan{Key: id, Label: id})
	}
	p.Steps[0].Uses = ids
	executable.Steps[0].DataUses = ids
	executable.Steps[0].Refs.Inputs = ids
	executable.Steps[0].Index = 2
	executable.Steps = append([]flow.StepPlan{{ID: "obtain", Title: "准备数据", Index: 1, Kind: "acquire", DataIDs: ids}}, executable.Steps...)
	return executable, request
}

func discoveryPreparation(d plan.Data) DataResult {
	return DataResult{Preparation: &DataPreparation{
		DataID: d.ID, Selection: json.RawMessage(fmt.Sprintf(`{"find_id":%q}`, d.FindID)),
		Role:     flow.RoleSpec{ID: "system_data_acquisition", DisplayName: "数据获取智能体", Kind: "agent", Enabled: true},
		Messages: []*schema.Message{schema.UserMessage("冻结查询 " + d.FindID)},
	}}
}

func TestDiscoveryUsesSharedInterruptLifecycleAndRegisteredResults(t *testing.T) {
	executable, request := discoveryFixture(t, "direct", "first", "second")
	registered := map[string]bool{}
	gets, expertCalls, consumerCalls := map[string]int{}, 0, 0
	checkpoint := ""
	controller, _ := NewController(Options{Store: FileRunStore{}, DataGet: func(_ context.Context, _ *RunState, d plan.Data) (DataResult, error) {
		gets[d.ID]++
		if d.ID == "direct" || registered[d.ID] {
			return DataResult{Value: "/verified/" + d.ID, Notes: []string{"registered " + d.ID}}, nil
		}
		return discoveryPreparation(d), nil
	}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Acquisition != nil {
			expertCalls++
			if r.Mode != "acquire" || r.Role.ID != "system_data_acquisition" || !strings.Contains(r.Messages[0].Content, "find-"+r.Acquisition.DataID) {
				t.Fatalf("lost structured handoff: %+v", r)
			}
			if r.Acquisition.DataID == "first" {
				checkpoint = r.RunID
				return StepResult{Interrupted: true}, nil
			}
			registered["second"] = true
			// No receipt repair/model call is needed for data registration.
			return StepResult{LastContent: "文件已经登记；这不是 JSON 回执"}, nil
		}
		consumerCalls++
		for _, id := range []string{"direct", "first", "second"} {
			if !strings.Contains(r.Messages[0].Content, "/verified/"+id) {
				t.Fatalf("consumer missing verified data %s", id)
			}
		}
		return cartoReceipt(r), nil
	})})
	run, err := controller.StartFromPlan(context.Background(), request, executable)
	if err != nil || run.Status != "waiting_user" || consumerCalls != 0 || expertCalls != 1 || run.AcquiredInputs["direct"] != "/verified/direct" {
		t.Fatalf("%+v err=%v experts=%d consumers=%d", run, err, expertCalls, consumerCalls)
	}
	if run.Steps[0].Attempts[0].DataID != "first" {
		t.Fatal("pending query identity was not persisted")
	}
	// A second interruption keeps the exact running expert rather than starting
	// it again or consuming an automatic failure/revision.
	run, err = controller.OnStepRunFinished(context.Background(), checkpoint, StepResult{Interrupted: true})
	if err != nil || run.Status != "waiting_user" || run.Steps[0].AutomaticFailures != 0 || expertCalls != 1 {
		t.Fatal(run, err)
	}
	registered["first"] = true
	run, err = controller.OnStepRunFinished(context.Background(), checkpoint, StepResult{LastContent: "完成"})
	if err != nil || run.Status != "completed" || expertCalls != 2 || consumerCalls != 1 {
		t.Fatalf("%+v err=%v experts=%d consumers=%d", run, err, expertCalls, consumerCalls)
	}
	if gets["direct"] != 1 || gets["first"] != 2 || gets["second"] != 2 || len(run.SourceNotes) != 3 {
		t.Fatalf("repeated completed acquisitions: gets=%v notes=%v", gets, run.SourceNotes)
	}
	if duplicate, err := controller.OnStepRunFinished(context.Background(), checkpoint, StepResult{}); err != nil || duplicate != nil || consumerCalls != 1 {
		t.Fatal("duplicate checkpoint completion executed work", duplicate, err)
	}
}

func TestDiscoveryDoesNotTrustModelPathsOrRestartWithoutRegistration(t *testing.T) {
	for _, expertErr := range []error{nil, errors.New("用户拒绝联网")} {
		t.Run(fmt.Sprint(expertErr), func(t *testing.T) {
			executable, request := discoveryFixture(t, "population")
			expertCalls, consumerCalls := 0, 0
			controller, _ := NewController(Options{Store: FileRunStore{}, DataGet: func(_ context.Context, _ *RunState, d plan.Data) (DataResult, error) {
				return discoveryPreparation(d), nil
			}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
				if r.Acquisition != nil {
					expertCalls++
					return StepResult{LastContent: `{"status":"completed","outputs":{"population":"/fabricated/population.csv"}}`}, expertErr
				}
				consumerCalls++
				return cartoReceipt(r), nil
			})})
			run, err := controller.StartFromPlan(context.Background(), request, executable)
			if err != nil || run.Status != "needs_replan" || expertCalls != 1 || consumerCalls != 0 || provided(run.AcquiredInputs["population"]) {
				t.Fatalf("%+v err=%v experts=%d consumers=%d", run, err, expertCalls, consumerCalls)
			}
			if expertErr == nil && !strings.Contains(run.Steps[0].LastFailure, "DATA_DISCOVERY_INCOMPLETE") {
				t.Fatal("unregistered output did not produce a concrete blocker")
			}
		})
	}
}

func TestDiscoveryServiceFailureStopsWithoutRetryOrReplan(t *testing.T) {
	for _, tc := range []struct {
		message string
		direct  bool
	}{
		{"[NodeRunError] status code: 402, message: Insufficient Balance (request_id: fixture-request)", false},
		{"status code: 401, invalid API key", false},
		{"HTTP 403 Forbidden", false},
		{"status code: 402, message: Insufficient Balance", true},
	} {
		t.Run(tc.message, func(t *testing.T) {
			executable, request := discoveryFixture(t, "population")
			gets, experts, consumers := 0, 0, 0
			failure := errors.New(tc.message)
			controller, _ := NewController(Options{Store: FileRunStore{}, DataGet: func(_ context.Context, _ *RunState, d plan.Data) (DataResult, error) {
				gets++
				if tc.direct {
					return DataResult{}, failure
				}
				return discoveryPreparation(d), nil
			}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
				if r.Acquisition != nil {
					experts++
					return StepResult{Err: failure}, nil
				}
				consumers++
				return cartoReceipt(r), nil
			})})
			run, err := controller.StartFromPlan(context.Background(), request, executable)
			if err != nil || run.Status != "failed" || gets != 1 || consumers != 0 || len(run.Steps[0].Attempts) != 1 {
				t.Fatalf("service failure retried/replanned: state=%+v err=%v gets=%d experts=%d consumers=%d", run, err, gets, experts, consumers)
			}
			if (!tc.direct && experts != 1) || (tc.direct && experts != 0) {
				t.Fatalf("unexpected expert calls: %d", experts)
			}
			if run.Steps[0].LastFailure != tc.message || !strings.Contains(run.Result.Summary, tc.message) || run.Steps[0].Attempts[0].Verification.Errors[0] != tc.message {
				t.Fatal("original provider reason was lost")
			}
		})
	}
}

func TestCancelledDiscoveryCannotResumeConsumer(t *testing.T) {
	executable, request := discoveryFixture(t, "population")
	checkpoint := ""
	calls := 0
	controller, _ := NewController(Options{Store: FileRunStore{}, DataGet: func(_ context.Context, _ *RunState, d plan.Data) (DataResult, error) {
		return discoveryPreparation(d), nil
	}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		calls++
		checkpoint = r.RunID
		return StepResult{Interrupted: true}, nil
	})})
	run, err := controller.StartFromPlan(context.Background(), request, executable)
	if err != nil || run.Status != "waiting_user" {
		t.Fatal(run, err)
	}
	run, err = controller.Cancel(context.Background(), request.SessionID)
	if err != nil || run.Status != "cancelled" {
		t.Fatal(run, err)
	}
	if run, err = controller.OnStepRunFinished(context.Background(), checkpoint, StepResult{}); err != nil || run != nil || calls != 1 {
		t.Fatal("cancelled acquisition resumed", run, err, calls)
	}
}

func TestDiscoveryCancelledDuringRegistrationCheckCannotBindData(t *testing.T) {
	executable, request := discoveryFixture(t, "population")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checkpoint := ""
	gets, consumers := 0, 0
	controller, _ := NewController(Options{Store: FileRunStore{}, DataGet: func(_ context.Context, _ *RunState, d plan.Data) (DataResult, error) {
		gets++
		if gets == 1 {
			return discoveryPreparation(d), nil
		}
		cancel()
		return DataResult{Value: "/verified/population.csv"}, nil
	}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Acquisition != nil {
			checkpoint = r.RunID
			return StepResult{Interrupted: true}, nil
		}
		consumers++
		return cartoReceipt(r), nil
	})})
	run, err := controller.StartFromPlan(context.Background(), request, executable)
	if err != nil || run.Status != "waiting_user" {
		t.Fatal(run, err)
	}
	run, err = controller.OnStepRunFinished(ctx, checkpoint, StepResult{LastContent: "完成"})
	if err != nil || run.Status != "cancelled" || consumers != 0 || provided(run.AcquiredInputs["population"]) {
		t.Fatal("cancelled verification released its consumer", run, err, consumers)
	}
}
