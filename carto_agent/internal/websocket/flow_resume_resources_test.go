package websocket

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/cloudwego/eino/adk"
)

type changingFlowCompiler struct{ plan *flow.FlowPlan }

func (c *changingFlowCompiler) EnsureRunnable(context.Context, string, string) (*flow.FlowPlan, error) {
	raw, _ := json.Marshal(c.plan)
	var copy flow.FlowPlan
	_ = json.Unmarshal(raw, &copy)
	return &copy, nil
}

type resourceReceiptAgent struct{ received *string }

func (a resourceReceiptAgent) Name(context.Context) string        { return "worker" }
func (a resourceReceiptAgent) Description(context.Context) string { return "test resource snapshot" }
func (a resourceReceiptAgent) Run(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	scope, ok := resource.ScopeFromContext(ctx)
	if ok && len(scope.Linked) == 1 {
		*a.received = scope.Linked[0].Entry
	}
	return (flowReceiptAgent{}).Run(ctx, input, opts...)
}

func TestFlowRuntimeResourceChoiceGuardsADKAndRestarts(t *testing.T) {
	for _, choice := range []string{"continue_snapshot", "restart"} {
		t.Run(choice, func(t *testing.T) {
			confirm := flow.StepPlan{ID: "approve", Index: 1, Title: "确认", Kind: "confirm", Instructions: "确认继续"}
			worker := flowWorkerStep()
			worker.Index = 2
			server, factory, session := flowRuntimeFixture(t, []flow.StepPlan{confirm, worker})
			original, _ := server.flowDependencies()
			plan, err := original.EnsureRunnable(context.Background(), "test-flow", "test")
			if err != nil {
				t.Fatal(err)
			}
			plan.CompileKey = "structure"
			plan.LinkFingerprint = "old"
			plan.Resources = []flow.ResourcePlan{{Key: "data_list.base", ID: "data_list.base", Type: "data_list", Entry: "/original/base.csv"}}
			compiler := &changingFlowCompiler{plan: plan}
			if err := server.SetFlowCompiler(compiler); err != nil {
				t.Fatal(err)
			}
			received := ""
			factory.agent = resourceReceiptAgent{received: &received}
			old, err := server.runDirectFlow(context.Background(), session, flowTaskRequest{ID: "test-flow"})
			if err != nil {
				t.Fatal(err)
			}
			pending, ok := server.popPendingInterrupt(nil, session.SessionID)
			if !ok {
				t.Fatal("missing checkpoint")
			}
			compiler.plan.LinkFingerprint = "new"
			compiler.plan.Resources[0].Entry = "/current/base.csv"
			// The actual resume path must stop before ADK consumes the original decision.
			server.resumeTask(context.Background(), nil, pending, map[string]any{pending.Contexts[0].ID: map[string]any{"option": "confirm"}})
			_, controller := server.flowDependencies()
			paused, err := controller.Snapshot(session.SessionID)
			if err != nil || paused.ResourceChange == nil || paused.Status != "waiting_user" || len(factory.roles) != 0 || received != "" {
				t.Fatalf("%+v %v roles=%v", paused, err, factory.roles)
			}
			if retained, ok := server.getPendingInterrupt(session.SessionID); !ok || retained.CheckpointID != pending.CheckpointID {
				t.Fatal("resource guard discarded checkpoint")
			}
			if !server.isPausedFlowResume(session.SessionID, session.ProjectPath, map[string]any{"flow": map[string]any{"resume_run_id": old.ID}}) {
				t.Fatal("task admission blocked the current paused flow")
			}
			if server.isPausedFlowResume(session.SessionID, session.ProjectPath, map[string]any{"flow": map[string]any{"resume_run_id": "another-run"}}) || server.isPausedFlowResume(session.SessionID, session.ProjectPath, map[string]any{"flow": map[string]any{"id": old.FlowID}}) {
				t.Fatal("task admission allowed an unrelated flow")
			}
			// This is the same structured request produced by the two frontend buttons.
			wire, err := parseFlowRequest(map[string]any{"flow": map[string]any{"id": old.FlowID, "resume_run_id": old.ID, "resume_resource_choice": choice, "resource_fingerprint": "stale"}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := server.runDirectFlow(context.Background(), session, *wire); err != nil {
				t.Fatal(err)
			}
			if len(factory.roles) != 0 {
				t.Fatal("stale resource acknowledgement executed a step")
			}
			wire.ResourceFingerprint = "new"
			resumed, err := server.runDirectFlow(context.Background(), session, *wire)
			if err != nil {
				t.Fatal(err)
			}
			active, ok := server.popPendingInterrupt(nil, session.SessionID)
			if !ok {
				t.Fatal("confirmation checkpoint missing after resource choice")
			}
			if choice == "continue_snapshot" {
				if resumed.ID != old.ID || active.CheckpointID != pending.CheckpointID {
					t.Fatal("snapshot continuation replaced original checkpoint")
				}
			} else {
				if resumed.ID == old.ID || active.CheckpointID == pending.CheckpointID {
					t.Fatal("restart reused old run or checkpoint")
				}
				if _, exists, err := server.checkpointStore.Get(context.Background(), pending.CheckpointID); err != nil || exists {
					t.Fatalf("old checkpoint survived restart: %v %v", exists, err)
				}
				server.finishFlowResume(context.Background(), pending, consumeEventsResult{LastContent: `{"choice":"confirm"}`})
				if len(factory.roles) != 0 {
					t.Fatal("old checkpoint callback replayed after restart")
				}
			}
			server.resumeTask(context.Background(), nil, active, map[string]any{active.Contexts[0].ID: map[string]any{"option": "confirm"}})
			final, err := controller.Snapshot(session.SessionID)
			want := "/original/base.csv"
			if choice == "restart" {
				want = "/current/base.csv"
			}
			if err != nil || final.Status != "completed" || received != want || len(factory.roles) != 1 {
				t.Fatalf("%+v %v resource=%s roles=%v", final, err, received, factory.roles)
			}
		})
	}
}

func TestFlowRuntimeChangedResourcesAfterServiceRestartPreserveThenInvalidateCheckpoint(t *testing.T) {
	confirm := flow.StepPlan{ID: "approve", Index: 1, Title: "确认", Kind: "confirm", Instructions: "确认继续"}
	first, factory, session := flowRuntimeFixture(t, []flow.StepPlan{confirm})
	initial, _ := first.flowDependencies()
	plan, _ := initial.EnsureRunnable(context.Background(), "test-flow", "test")
	plan.CompileKey, plan.LinkFingerprint = "structure", "old"
	compiler := &changingFlowCompiler{plan: plan}
	if err := first.SetFlowCompiler(compiler); err != nil {
		t.Fatal(err)
	}
	old, err := first.runDirectFlow(context.Background(), session, flowTaskRequest{ID: "test-flow"})
	if err != nil {
		t.Fatal(err)
	}
	original, ok := first.getPendingInterrupt(session.SessionID)
	if !ok {
		t.Fatal("missing checkpoint")
	}
	compiler.plan.LinkFingerprint = "new"
	second := NewDeepServer(0, nil)
	second.runtime, second.checkpointStore = first.runtime, first.checkpointStore
	second.SetRuntimeFactory(factory)
	if err := second.SetFlowCompiler(compiler); err != nil {
		t.Fatal(err)
	}
	if _, err := second.restoreFlowPending(context.Background(), session.SessionID, session.ProjectPath, old.ID); !resourceChangePending(err) {
		t.Fatalf("old waiting checkpoint was silently restored: %v", err)
	}
	if _, exists, err := second.checkpointStore.Get(context.Background(), original.CheckpointID); err != nil || !exists {
		t.Fatalf("guard lost original checkpoint: %v %v", exists, err)
	}
	restarted, err := second.runDirectFlow(context.Background(), session, flowTaskRequest{ResumeRunID: old.ID, ResourceChoice: "restart", ResourceFingerprint: "new"})
	if err != nil || restarted.ID == old.ID {
		t.Fatalf("%+v %v", restarted, err)
	}
	if _, exists, err := second.checkpointStore.Get(context.Background(), original.CheckpointID); err != nil || exists {
		t.Fatalf("restart failed to delete detached old checkpoint: %v %v", exists, err)
	}
}
