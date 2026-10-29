package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
	if name == "get_layers_info" {
		return map[string]any{"success": true, "data": map[string]any{"layers": []any{}}}, nil
	}
	return nil, fmt.Errorf("unexpected verification")
}

func cartoRuntimeFixture(t *testing.T) (*DeepServer, *runtimeFlowFactory, flowSessionContext, *plan.Plan) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", home)
	root, _ := filepath.Abs("../../..")
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(root, "autocarto"))
	s := NewDeepServer(0, nil)
	s.runtime = &agentruntime.Runtime{ActiveConfigName: "test", ActiveConfig: &config.AgentConfigFile{}}
	factory := &runtimeFlowFactory{agent: flowReceiptAgent{}}
	s.SetRuntimeFactory(factory)
	library, err := resource.NewDefaultLibrary()
	if err != nil {
		t.Fatal(err)
	}
	guides, err := flow.NewDefaultGuideStore(library)
	if err != nil {
		t.Fatal(err)
	}
	env := flow.Environment{Scheme: "test", PlanningInstructions: "测试规划契约", Roles: []flow.RoleSpec{{ID: "worker", Kind: "agent", Enabled: true, ToolPackages: []string{"read"}}}, Packages: []flow.PackageSpec{{ID: "read", DisplayName: "读取数据", Enabled: true, Kind: "category", Tools: []string{"read"}}}}
	if err = s.SetPlanRuntime(PlanRuntimeOptions{Guides: guides, Resources: library, LoadEnvironment: func(context.Context, string) (flow.Environment, error) { return env, nil }}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(home, "work", "project.qgz")
	id := "carto-runtime"
	if err = s.SaveSession(map[string]any{"session_id": id, "messages": []any{}, "project_path": project}, project); err != nil {
		t.Fatal(err)
	}
	sess := s.sessionManager.GetOrCreate(id)
	sess.AddMessage(schema.UserMessage("保留完整原文"))
	run := flowSessionContext{Session: sess, SessionID: id, ProjectPath: project, Scheme: "test", Mode: executionModeBuild, OriginalSource: "保留完整原文"}
	s.setFlowContext(run)
	output := filepath.Join(home, "result.txt")
	if err = os.WriteFile(output, []byte("保留的结果"), 0600); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(map[string]any{"status": "completed", "summary": "完成", "outputs": map[string]any{"result": output}, "issues": []string{}})
	factory.agent = flowReceiptAgent{receipt: string(encoded)}
	p := &plan.Plan{APIVersion: plan.APIVersion, ID: "cp-test", Version: 1, Goal: "测试制图", Steps: []plan.Step{{ID: "work", Title: "处理", Capabilities: []string{"read"}, Instructions: "读取并返回真实结果", Produces: []plan.Product{{ID: "result", Kind: "file", Format: "txt"}}}}}
	return s, factory, run, p
}
func TestCartographicProposalExecutesAndRestoresPlanCard(t *testing.T) {
	s, f, run, p := cartoRuntimeFixture(t)
	source := ""
	f.agent = flowReceiptAgent{source: &source, receipt: f.agent.(flowReceiptAgent).receipt}
	raw, _ := json.Marshal(p)
	answer, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), raw)
	if err != nil || answer["status"] != "ready" {
		t.Fatal(answer, err)
	}
	state, err := s.startPendingCartographicPlan(context.Background(), run.SessionID)
	if err != nil || state.Status != "completed" || source != run.OriginalSource || len(f.roles) != 1 {
		t.Fatal(state, err, source, f.roles)
	}
	cards, err := s.ListPlans(context.Background(), run.SessionID, run.ProjectPath)
	if err != nil || len(cards) != 1 || cards[0].Status != "completed" || cards[0].RunID != state.ID {
		t.Fatal(cards, err)
	}
	session, err := s.LoadSession(run.SessionID, run.ProjectPath)
	if err != nil || len(session["carto_plans"].([]*plan.Snapshot)) != 1 {
		t.Fatal(session, err)
	}
}
func TestCartographicPlanModeAndHighAssumptionNeverAutoExecute(t *testing.T) {
	for _, mode := range []executionMode{executionModeBuild, executionModePlan} {
		t.Run(string(mode), func(t *testing.T) {
			s, _, run, p := cartoRuntimeFixture(t)
			run.Mode = mode
			s.setFlowContext(run)
			p.Assumptions = []plan.Assumption{{Text: "用户尚未确定范围", Impact: "high"}}
			raw, _ := json.Marshal(p)
			out, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), raw)
			if err != nil || s.hasPendingCartographicPlan(run.SessionID) {
				t.Fatal(out, err)
			}
			want := "awaiting_confirmation"
			if mode == executionModePlan {
				want = "preview"
			}
			if out["status"] != want {
				t.Fatal(out)
			}
		})
	}
}
func TestCartographicEditIsDeterministicAndVersioned(t *testing.T) {
	s, _, run, p := cartoRuntimeFixture(t)
	run.Mode = executionModePlan
	s.setFlowContext(run)
	path := filepath.Join(t.TempDir(), "route.geojson")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.bindTaskReferences(context.Background(), run.SessionID, run.ProjectPath, Message{}, schema.UserMessage("使用 `"+path+"`")); err != nil {
		t.Fatal(err)
	}
	p.Data = []plan.Data{{ID: "route", Name: "路线", Source: "user", Ref: path}}
	p.Steps[0].Inputs = []string{"route"}
	raw, _ := json.Marshal(p)
	if _, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	updated, err := s.EditPlan(context.Background(), run.SessionID, run.ProjectPath, p.ID, plan.EditRequest{Version: 1, DataID: "route", Source: "user", Ref: path})
	if err != nil || updated.Plan.Version != 2 || s.hasPendingCartographicPlan(run.SessionID) {
		t.Fatal(updated, err)
	}
	if _, err = s.EditPlan(context.Background(), run.SessionID, run.ProjectPath, p.ID, plan.EditRequest{Version: 1, DataID: "route", Source: "user", Ref: path}); err == nil {
		t.Fatal("stale edit accepted")
	}
}
func TestCartographicAttachmentRegistersExactFileOnly(t *testing.T) {
	s, _, run, _ := cartoRuntimeFixture(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "provided.geojson")
	sibling := filepath.Join(dir, "private.geojson")
	for _, p := range []string{file, sibling} {
		if err := os.WriteFile(p, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := schema.UserMessage("使用此附件")
	if err := s.bindTaskReferences(context.Background(), run.SessionID, run.ProjectPath, Message{Data: map[string]any{"attachments": []any{map[string]any{"path": file}}}}, m); err != nil {
		t.Fatal(err)
	}
	store, err := sessionfiles.New(run.ProjectPath, run.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.ValidatePath(file); err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.ValidatePath(sibling); err != nil {
		t.Fatalf("existing local file must not require attachment authorization: %v", err)
	}
	items, err := store.List(false)
	canonicalFile, _, pathErr := store.ValidatePath(file)
	if err != nil || pathErr != nil || len(items) != 1 || items[0].Path != canonicalFile {
		t.Fatalf("attaching one file should index only that material: %v, %v", items, err)
	}
}

func TestPlanModeCanStartSameVersionAfterSwitchToBuild(t *testing.T) {
	s, _, run, p := cartoRuntimeFixture(t)
	run.Mode = executionModePlan
	s.setFlowContext(run)
	raw, _ := json.Marshal(p)
	if _, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	request := plan.ActionRequest{Action: "start", Version: 1}
	if _, err := s.ActOnPlan(context.Background(), run.SessionID, run.ProjectPath, p.ID, request); err == nil {
		t.Fatal("Plan mode executed")
	}
	run.Mode = executionModeBuild
	s.setFlowContext(run)
	if _, err := s.ActOnPlan(context.Background(), run.SessionID, run.ProjectPath, p.ID, request); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		cards, err := s.ListPlans(context.Background(), run.SessionID, run.ProjectPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(cards) == 1 && cards[0].Status == "completed" {
			if cards[0].PlanVersion != 1 || cards[0].PlanID != p.ID {
				t.Fatal(cards)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("same plan did not complete after Build switch")
}
func TestPendingRevisionIsNotReplacedWithPriorRunStatus(t *testing.T) {
	s, f, run, p := cartoRuntimeFixture(t)
	f.agent = flowReceiptAgent{receipt: `{"status":"failed","summary":"失败","outputs":{},"issues":["缺少数据"]}`}
	raw, _ := json.Marshal(p)
	if _, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	state, err := s.startPendingCartographicPlan(context.Background(), run.SessionID)
	// This fixture has no replanning model; the failed run is still persisted.
	if state == nil || state.Status != "needs_replan" {
		t.Fatal(state, err)
	}
	store, err := planStore(run)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := store.Load(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	prior.Plan.Version = 2
	prior.Status = "awaiting_confirmation"
	prior.NeedsConfirmation = true
	if err = store.Save(prior, 1); err != nil {
		t.Fatal(err)
	}
	cards, err := s.ListPlans(context.Background(), run.SessionID, run.ProjectPath)
	if err != nil || len(cards) != 1 || cards[0].Status != "awaiting_confirmation" || cards[0].Run != nil {
		t.Fatal(cards, err)
	}
}
