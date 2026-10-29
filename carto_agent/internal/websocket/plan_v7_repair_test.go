package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type v7RepairAgent struct {
	calls       int
	output      string
	repairInput []*schema.Message
}

func (*v7RepairAgent) Name(context.Context) string { return "worker" }
func (*v7RepairAgent) Description(context.Context) string {
	return "content repair regression model boundary"
}
func (a *v7RepairAgent) Run(_ context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	a.calls++
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		output := a.output
		if a.calls == 1 {
			call := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "read-source", Type: "function", Function: schema.FunctionCall{Name: "file_read", Arguments: `{"path":"source.csv"}`}}}}
			gen.Send(adk.EventFromMessage(call, nil, schema.Assistant, ""))
			gen.Send(adk.EventFromMessage(schema.ToolMessage(`{"success":true,"content":"name,value\n郑州市,1"}`, "read-source", schema.WithToolName("file_read")), nil, schema.Tool, "file_read"))
			if err := os.WriteFile(a.output, []byte("name,count\n郑州市,1\n"), 0600); err != nil {
				gen.Send(&adk.AgentEvent{Err: err})
				return
			}
		} else {
			a.repairInput = append([]*schema.Message{}, input.Messages...)
			if err := os.WriteFile(a.output, []byte("name,value\n郑州市,1\n"), 0600); err != nil {
				gen.Send(&adk.AgentEvent{Err: err})
				return
			}
		}
		raw, _ := json.Marshal(flowrun.Receipt{Status: "completed", Outputs: map[string]any{"result": output}})
		gen.Send(adk.EventFromMessage(schema.AssistantMessage(string(raw), nil), nil, schema.Assistant, ""))
	}()
	return iter
}

func TestV7ContentRepairKeepsSameAgentAndToolHistory(t *testing.T) {
	s, f, run, p := cartoRuntimeFixture(t)
	p.Steps[0].Produces = []plan.Product{{ID: "result", Kind: "table", Format: "csv", Fields: []string{"name", "value"}}}
	agent := &v7RepairAgent{output: filepath.Join(t.TempDir(), "repaired.csv")}
	f.agent = agent
	raw, _ := json.Marshal(p)
	if _, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	state, err := s.startPendingCartographicPlan(context.Background(), run.SessionID)
	if err != nil || state.Status != "completed" || agent.calls != 2 || len(f.roles) != 1 {
		t.Fatalf("repair rebuilt or failed agent: state=%+v err=%v runs=%d builds=%v", state, err, agent.calls, f.roles)
	}
	if len(state.Steps[0].Attempts) != 2 || state.Steps[0].Attempts[0].Verification.OK || !state.Steps[0].Attempts[1].Verification.OK {
		t.Fatal(state.Steps)
	}
	var callAt, resultAt, receiptAt int = -1, -1, -1
	for i, m := range agent.repairInput {
		if len(m.ToolCalls) == 1 && m.ToolCalls[0].ID == "read-source" {
			callAt = i
		}
		if m.Role == schema.Tool && m.ToolCallID == "read-source" && strings.Contains(m.Content, "郑州市") {
			resultAt = i
		}
		if m.Role == schema.Assistant && strings.Contains(m.Content, agent.output) {
			receiptAt = i
		}
	}
	last := agent.repairInput[len(agent.repairInput)-1]
	if callAt < 0 || resultAt <= callAt || receiptAt <= resultAt || last.Role != schema.User || !strings.Contains(last.Content, "仅修正") || !strings.Contains(last.Content, "缺少字段 value") {
		t.Fatalf("repair lost actual prior tool conversation or failure reason: %+v", agent.repairInput)
	}
}

func TestV7DataPreparationEventCannotOverwriteWholePlanStatus(t *testing.T) {
	s, _, run, p := cartoRuntimeFixture(t)
	store, err := planStore(run)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &plan.Snapshot{Plan: p, SessionID: run.SessionID, Status: "running"}
	if err = store.Save(snapshot, 0); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Dir, p.ID+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	binding := flowRuntimeBinding{server: s}
	var lastSeq int64
	for _, status := range []string{"running", "completed", "pending_agent", "failed"} {
		lastSeq = binding.Emit(context.Background(), flowrun.Event{PlanID: p.ID, PlanVersion: 1, SessionID: run.SessionID, FlowRunID: "flow-test", Event: "flow.data_preparation", Payload: map[string]any{"data_id": "river", "status": status}})
		after, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("data item %s rewrote plan snapshot", status)
		}
	}
	s.mu.Lock()
	events := append([]Message(nil), s.sessionRunEventBuffers[run.SessionID]...)
	s.mu.Unlock()
	if len(events) != 4 || lastSeq == 0 {
		t.Fatalf("preparation events missing or duplicated by plan events: %+v", events)
	}
	for _, event := range events {
		if event.Data["source_type"] != MsgTypeFlowEvent {
			t.Fatalf("unexpected second plan card event: %+v", event)
		}
	}
	for _, tc := range []struct {
		event   string
		payload map[string]any
		want    string
	}{
		{"flow.step_waiting_user", map[string]any{}, "waiting_user"},
		{"flow.step_started", map[string]any{}, "running"},
		{"flow.step_waiting_user", map[string]any{"status": "interrupted"}, "interrupted"},
		{"flow.step_failed", map[string]any{"next": "replan", "status": "needs_replan"}, "needs_replan"},
		{"flow.finished", map[string]any{"status": "completed"}, "completed"},
	} {
		binding.Emit(context.Background(), flowrun.Event{PlanID: p.ID, PlanVersion: 1, SessionID: run.SessionID, FlowRunID: "flow-test", Event: tc.event, Payload: tc.payload})
		got, e := store.Load(p.ID)
		if e != nil || got.Status != tc.want {
			t.Fatal(fmt.Sprintf("%s => %s", tc.event, tc.want), got, e)
		}
	}
}
