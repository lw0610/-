package websocket

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type runtimeFlowCompiler struct{ plan *flow.FlowPlan }

func (c runtimeFlowCompiler) EnsureRunnable(context.Context, string, string) (*flow.FlowPlan, error) {
	return c.plan, nil
}
func (c runtimeFlowCompiler) AvailablePlans(context.Context, string) ([]*flow.FlowPlan, error) {
	return []*flow.FlowPlan{c.plan}, nil
}

type flowReceiptAgent struct {
	source  *string
	receipt string
}

func (a flowReceiptAgent) Name(context.Context) string        { return "worker" }
func (a flowReceiptAgent) Description(context.Context) string { return "worker" }
func (a flowReceiptAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		if a.source != nil {
			*a.source, _ = knowledge.OriginalSource(ctx)
		}
		receipt := a.receipt
		if receipt == "" {
			receipt = `{"status":"completed","summary":"完成","outputs":{"结果":"保留的结果"},"issues":[]}`
		}
		gen.Send(adk.EventFromMessage(schema.AssistantMessage(receipt, nil), nil, schema.Assistant, ""))
	}()
	return iter
}

type runtimeFlowFactory struct {
	agent  adk.Agent
	roles  []string
	invoke func(context.Context, string, map[string]any) (any, error)
}

func (f *runtimeFlowFactory) Build(context.Context, string, *config.AgentConfigFile) (*agentruntime.Runtime, error) {
	return nil, fmt.Errorf("unused")
}
func (f *runtimeFlowFactory) BuildActive(context.Context, *config.AgentConfigManager) (*agentruntime.Runtime, error) {
	return nil, fmt.Errorf("unused")
}
func (f *runtimeFlowFactory) BuildRole(_ context.Context, _ string, _ *config.AgentConfigFile, name string) (adk.Agent, error) {
	f.roles = append(f.roles, name)
	return f.agent, nil
}
func (f *runtimeFlowFactory) CallFlowJSON(context.Context, *config.AgentConfigFile, string) (string, error) {
	return "", fmt.Errorf("unexpected repair")
}
func (f *runtimeFlowFactory) InvokeFlowTool(ctx context.Context, name string, args map[string]any) (any, error) {
	if f.invoke != nil {
		return f.invoke(ctx, name, args)
	}
	return nil, fmt.Errorf("unexpected verification")
}
func flowRuntimeFixture(t *testing.T, steps []flow.StepPlan) (*DeepServer, *runtimeFlowFactory, flowSessionContext) {
	t.Helper()
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	root, _ := filepath.Abs("../../..")
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(root, "autocarto"))
	s := NewDeepServer(0, nil)
	s.runtime = &agentruntime.Runtime{ActiveConfigName: "test", ActiveConfig: &config.AgentConfigFile{}}
	factory := &runtimeFlowFactory{agent: flowReceiptAgent{}}
	s.SetRuntimeFactory(factory)
	plan := &flow.FlowPlan{FlowID: "test-flow", Name: "测试流程", Scheme: "test", CompileKey: "frozen", FlowVersion: "1", Roles: []flow.RoleSpec{{ID: "worker", DisplayName: "处理角色", Kind: "agent", Enabled: true}}, Steps: steps}
	if err := s.SetFlowCompiler(runtimeFlowCompiler{plan}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "project.qgz")
	id := "flow-runtime"
	if err := s.SaveSession(map[string]any{"session_id": id, "messages": []any{}, "project_path": project}, project); err != nil {
		t.Fatal(err)
	}
	sess := s.sessionManager.GetOrCreate(id)
	sess.AddMessage(schema.UserMessage("源文本：第一行\n第二行"))
	run := flowSessionContext{Session: sess, SessionID: id, ProjectPath: project, Scheme: "test", Mode: executionModeBuild, OriginalSource: "源文本：第一行\n第二行"}
	s.setFlowContext(run)
	return s, factory, run
}
func flowWorkerStep() flow.StepPlan {
	return flow.StepPlan{ID: "work", Index: 1, Title: "处理", Kind: "agent", Role: "worker", Instructions: "完成本步", ExpectedOutputs: []flow.OutputDeclaration{{Label: "结果", Type: "text", Required: true}}}
}
func TestFlowRuntimeRunsRoleReceiptAndAuthoritativeSnapshot(t *testing.T) {
	s, factory, run := flowRuntimeFixture(t, []flow.StepPlan{flowWorkerStep()})
	source := ""
	factory.agent = flowReceiptAgent{source: &source}
	state, err := s.runDirectFlow(context.Background(), run, flowTaskRequest{ID: "test-flow"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "completed" || len(factory.roles) != 1 || factory.roles[0] != "worker" || source != run.OriginalSource {
		t.Fatalf("bad role/source/result: %+v %v %q", state, factory.roles, source)
	}
	data, err := s.LoadSession(run.SessionID, run.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := data["flow_run"].(*flowrun.RunState)
	if snapshot.EventSeq <= 0 || snapshot.Status != "completed" {
		t.Fatalf("missing final snapshot: %+v", snapshot)
	}
	replay := s.sessionRunEventBuffers[run.SessionID]
	receipts := 0
	finals := 0
	for _, event := range replay {
		payload, _ := event.Data["payload"].(map[string]any)
		if event.Data["source_type"] == MsgTypeResponse {
			finals++
			if strings.Contains(stringValue(payload["content"]), `"status":"completed"`) {
				receipts++
			}
		}
	}
	if finals != 1 || receipts != 0 {
		t.Fatalf("step receipt leaked as final: finals=%d receipts=%d", finals, receipts)
	}
	messages := run.Session.GetMessages()
	if len(messages) < 3 || messages[1].Extra["step_id"] != "work" || messages[1].Extra["flow_run_id"] != state.ID {
		t.Fatalf("step history metadata missing: %+v", messages)
	}
}
func TestFlowRuntimeConfirmationResumeDoesNotRunNextStepTwice(t *testing.T) {
	confirm := flow.StepPlan{ID: "approve", Index: 1, Title: "确认", Kind: "confirm", Instructions: "确认继续", ExpectedOutputs: []flow.OutputDeclaration{{Label: "确认", Type: "confirmation"}}}
	worker := flowWorkerStep()
	worker.Index = 2
	s, factory, run := flowRuntimeFixture(t, []flow.StepPlan{confirm, worker})
	state, err := s.runDirectFlow(context.Background(), run, flowTaskRequest{ID: "test-flow"})
	if err != nil || state.Status != "waiting_user" {
		t.Fatalf("not paused: %+v %v", state, err)
	}
	pending, ok := s.popPendingInterrupt(nil, run.SessionID)
	if !ok || pending.FlowOptions.flowRunID != state.ID || len(pending.Contexts) == 0 {
		t.Fatal("missing flow HITL metadata")
	}
	targets := map[string]any{pending.Contexts[0].ID: map[string]any{"option": "confirm", "text": "确认"}}
	s.resumeTask(context.Background(), nil, pending, targets)
	_, controller := s.flowDependencies()
	final, err := controller.Snapshot(run.SessionID)
	if err != nil || final.Status != "completed" || len(factory.roles) != 1 {
		t.Fatalf("resume not completed once: %+v %v roles=%v", final, err, factory.roles)
	}
	s.finishFlowResume(context.Background(), pending, consumeEventsResult{LastContent: `{"choice":"confirm"}`})
	if len(factory.roles) != 1 {
		t.Fatal("duplicate callback reran next step")
	}
}
func TestFlowRuntimeRestoresWaitingCheckpointAcrossServerRestart(t *testing.T) {
	confirm := flow.StepPlan{ID: "approve", Index: 1, Title: "确认", Kind: "confirm", Instructions: "确认继续"}
	worker := flowWorkerStep()
	worker.Index = 2
	first, _, run := flowRuntimeFixture(t, []flow.StepPlan{confirm, worker})
	db := filepath.Join(t.TempDir(), "checkpoints.db")
	store, err := runtimepersist.NewSQLiteCheckpointStore(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.checkpointStore = store
	state, err := first.runDirectFlow(context.Background(), run, flowTaskRequest{ID: "test-flow"})
	if err != nil {
		t.Fatal(err)
	}
	second := NewDeepServer(0, nil)
	second.runtime = first.runtime
	factory := &runtimeFlowFactory{agent: flowReceiptAgent{}}
	second.SetRuntimeFactory(factory)
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := runtimepersist.NewSQLiteCheckpointStore(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	second.checkpointStore = reopened
	compiler, _ := first.flowDependencies()
	if err = second.SetFlowCompiler(compiler); err != nil {
		t.Fatal(err)
	}
	pending, err := second.restoreFlowPending(context.Background(), run.SessionID, run.ProjectPath, state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.InterruptData["interrupt_contexts"] == nil {
		t.Fatal("restored card lost options")
	}
	second.popPendingInterrupt(nil, run.SessionID)
	second.resumeTask(context.Background(), nil, pending, map[string]any{pending.Contexts[0].ID: map[string]any{"option": "confirm"}})
	_, controller := second.flowDependencies()
	finished, err := controller.Snapshot(run.SessionID)
	if err != nil || finished.Status != "completed" || len(factory.roles) != 1 {
		t.Fatalf("restart failed: %+v %v", finished, err)
	}
}
func TestFlowRuntimePreviewPlanAndQueuedStartTrustBoundary(t *testing.T) {
	s, _, run := flowRuntimeFixture(t, []flow.StepPlan{flowWorkerStep()})
	service := sessionFlowService{server: s, session: run}
	if _, err := service.PreviewFlow(context.Background(), "test-flow", nil); err != nil {
		t.Fatal(err)
	}
	service.session.Mode = executionModePlan
	if _, err := service.StartFlow(context.Background(), "test-flow", nil); err == nil {
		t.Fatal("Plan started flow")
	}
	service.session.Mode = executionModeBuild
	accepted, err := service.StartFlow(context.Background(), "test-flow", nil)
	if err != nil || accepted["accepted"] != true {
		t.Fatalf("queue failed: %v %v", accepted, err)
	}
	_, controller := s.flowDependencies()
	snapshot, _ := controller.Snapshot(run.SessionID)
	if snapshot != nil {
		t.Fatal("tool executed flow synchronously")
	}
	state, err := s.startPendingFlow(context.Background(), run.SessionID)
	if err != nil || state.Status != "completed" || state.ID != accepted["flow_run_id"] {
		t.Fatalf("queued run failed: %+v %v", state, err)
	}
	service.session.FlowRunID = state.ID
	if _, err := service.StartFlow(context.Background(), "test-flow", nil); err == nil {
		t.Fatal("nested flow accepted")
	}
}

type registeringFlowAgent struct{}

func (registeringFlowAgent) Name(context.Context) string        { return "orchestrator" }
func (registeringFlowAgent) Description(context.Context) string { return "orchestrator" }
func (registeringFlowAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		_, err := runtimecontrol.Invoke(ctx, "flow_start", map[string]any{"flow_id": "test-flow"})
		if err != nil {
			gen.Send(&adk.AgentEvent{Err: err})
			return
		}
		gen.Send(adk.EventFromMessage(schema.AssistantMessage("已登记流程", nil), nil, schema.Assistant, ""))
	}()
	return iter
}
func TestFlowRuntimeProcessTaskDirectAndOrchestrator(t *testing.T) {
	for _, direct := range []bool{true, false} {
		t.Run(fmt.Sprint(direct), func(t *testing.T) {
			s, factory, run := flowRuntimeFixture(t, []flow.StepPlan{flowWorkerStep()})
			seedTestRunningInstance(s)
			s.buildAgent = registeringFlowAgent{}
			s.runtime.BuildAgent = s.buildAgent
			data := map[string]any{"project_path": run.ProjectPath, "mode": "build"}
			if direct {
				data["flow"] = map[string]any{"id": "test-flow"}
			}
			s.processTask(context.Background(), nil, Message{Type: "task", SessionID: run.SessionID, Content: run.OriginalSource, Data: data})
			_, controller := s.flowDependencies()
			state, _ := controller.Snapshot(run.SessionID)
			if state == nil || state.Status != "completed" || len(factory.roles) != 1 {
				t.Fatalf("task failed: %+v roles=%v events=%+v", state, factory.roles, s.sessionRunEventBuffers[run.SessionID])
			}
			if !direct && state.Origin != "orchestrator" {
				t.Fatal("origin lost")
			}
		})
	}
}
func TestFlowRuntimeCancelPausedAndStartupMarksOnlyRunning(t *testing.T) {
	confirm := flow.StepPlan{ID: "approve", Index: 1, Kind: "confirm", Instructions: "确认继续"}
	s, _, run := flowRuntimeFixture(t, []flow.StepPlan{confirm})
	state, err := s.runDirectFlow(context.Background(), run, flowTaskRequest{ID: "test-flow"})
	if err != nil {
		t.Fatal(err)
	}
	s.cancelSessionTask(nil, run.SessionID)
	_, controller := s.flowDependencies()
	cancelled, _ := controller.Snapshot(run.SessionID)
	if cancelled.Status != "cancelled" {
		t.Fatalf("paused cancellation failed %+v", cancelled)
	}
	state.Status = "running"
	if err = (flowrun.FileRunStore{}).Save(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	next := NewDeepServer(0, nil)
	compiler, _ := s.flowDependencies()
	if err = next.SetFlowCompiler(compiler); err != nil {
		t.Fatal(err)
	}
	restored, err := next.flowSnapshot(run.SessionID, run.ProjectPath)
	if err != nil || restored.Status != "interrupted" {
		t.Fatalf("startup not marked: %+v %v", restored, err)
	}
	if next.nextSessionRunEventSequence(run.SessionID) <= uint64(restored.EventSeq) {
		t.Fatal("restart sequence regressed")
	}
}
func TestFlowRuntimeHistoryRestoresStepMetadataAndRejectCancelsController(t *testing.T) {
	s, _, run := flowRuntimeFixture(t, []flow.StepPlan{flowWorkerStep()})
	_, err := s.runDirectFlow(context.Background(), run, flowTaskRequest{ID: "test-flow"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.loadSessionFromFile(run.SessionID, run.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	messages, _ := raw["messages"].([]any)
	found := false
	for _, value := range messages {
		item, _ := value.(map[string]any)
		data, _ := item["data"].(map[string]any)
		if data["step_id"] == "work" && data["flow_run_id"] != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("REST history lost step markers")
	}
	// A negative approval is cancellation and must release the flow slot too.
	confirm := flow.StepPlan{ID: "confirm", Index: 1, Kind: "confirm", Instructions: "确认继续"}
	next, _, nextRun := flowRuntimeFixture(t, []flow.StepPlan{confirm})
	_, err = next.runDirectFlow(context.Background(), nextRun, flowTaskRequest{ID: "test-flow"})
	if err != nil {
		t.Fatal(err)
	}
	next.handleInterruptResponse(nil, Message{SessionID: nextRun.SessionID, Data: map[string]any{"approved": false}})
	_, controller := next.flowDependencies()
	state, _ := controller.Snapshot(nextRun.SessionID)
	if state.Status != "cancelled" {
		t.Fatal(state.Status)
	}
}
func TestFlowRuntimeVerificationCarriesTrustedSessionBinding(t *testing.T) {
	step := flowWorkerStep()
	step.ExpectedOutputs = []flow.OutputDeclaration{{Label: "图层", Type: "layers", Required: true}}
	s, factory, run := flowRuntimeFixture(t, []flow.StepPlan{step})
	run.InstanceID = "bound-instance"
	s.setFlowContext(run)
	factory.agent = flowReceiptAgent{receipt: `{"status":"completed","summary":"加载完成","outputs":{"图层":["roads"]},"issues":[]}`}
	calls := 0
	factory.invoke = func(ctx context.Context, name string, args map[string]any) (any, error) {
		calls++
		if name != "get_layers_info" || sessionIDFromContext(ctx) != run.SessionID || projectPathFromContext(ctx) != run.ProjectPath || instanceIDFromContext(ctx) != run.InstanceID {
			return nil, fmt.Errorf("lost trusted binding")
		}
		return map[string]any{"success": true, "info": map[string]any{"layers": []any{map[string]any{"id": "roads-id", "name": "roads"}}}}, nil
	}
	state, err := s.runDirectFlow(context.Background(), run, flowTaskRequest{ID: "test-flow"})
	if err != nil || state.Status != "completed" || calls != 1 {
		t.Fatalf("verification failed: %+v %v calls=%d", state, err, calls)
	}
}
