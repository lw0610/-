package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/adk"
)

type accountFailureAgent struct{}

func (accountFailureAgent) Name(context.Context) string        { return "worker" }
func (accountFailureAgent) Description(context.Context) string { return "fixture account failure" }
func (accountFailureAgent) Run(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		gen.Send(&adk.AgentEvent{Err: errors.New("status code: 402, Insufficient Balance")})
	}()
	return iter
}
func TestV7RestartUsesRunStateForResumeAndCancel(t *testing.T) {
	for _, action := range []string{"start", "stop"} {
		t.Run(action, func(t *testing.T) {
			s, f, run, p := cartoRuntimeFixture(t)
			success := f.agent
			f.agent = accountFailureAgent{}
			raw, _ := json.Marshal(p)
			if _, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), raw); err != nil {
				t.Fatal(err)
			}
			paused, err := s.startPendingCartographicPlan(context.Background(), run.SessionID)
			if err != nil || paused.Status != "interrupted" {
				t.Fatal(paused, err)
			}
			store, err := planStore(run)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.UpdateStatus(p.ID, p.Version, func(v *plan.Snapshot) error { v.Status = "running"; return nil }); err != nil {
				t.Fatal(err)
			}
			options := s.cartoRuntime.options
			if err = s.SetPlanRuntime(options); err != nil {
				t.Fatal(err)
			}
			f.agent = success
			if _, err = s.ActOnPlan(context.Background(), run.SessionID, run.ProjectPath, p.ID, plan.ActionRequest{Version: p.Version, Action: action}); err != nil {
				t.Fatal(err)
			}
			want := "completed"
			if action == "stop" {
				want = "cancelled"
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				state, err := s.planRunSnapshot(run, paused.ID)
				if err != nil {
					t.Fatal(err)
				}
				if state.Status == want {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatalf("persisted interrupted run did not become %s", want)
		})
	}
}
