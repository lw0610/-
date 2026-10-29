package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/deepagent"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type derivedExtentCheckpointAgent struct {
	check    func(context.Context) error
	register func(context.Context) error
}

func (*derivedExtentCheckpointAgent) Name(context.Context) string {
	return deepagent.DataAcquisitionExpertName
}
func (*derivedExtentCheckpointAgent) Description(context.Context) string {
	return "derived extent checkpoint contract"
}
func (a *derivedExtentCheckpointAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		if err := a.check(ctx); err != nil {
			gen.Send(&adk.AgentEvent{Err: err})
			return
		}
		gen.Send(adk.StatefulInterrupt(ctx, "允许使用已冻结范围获取数据", "await-registration"))
	}()
	return iter
}
func (a *derivedExtentCheckpointAgent) Resume(ctx context.Context, _ *adk.ResumeInfo, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		if err := a.register(ctx); err != nil {
			gen.Send(&adk.AgentEvent{Err: err})
			return
		}
		gen.Send(adk.EventFromMessage(schema.AssistantMessage("已登记真实数据", nil), nil, schema.Assistant, ""))
	}()
	return iter
}

func TestDerivedAcquisitionExtentPersistsAcrossHITLRestartAndRegistration(t *testing.T) {
	ctx := context.Background()
	s, base, session, p := cartoRuntimeFixture(t)
	session.Mode = executionModePlan
	s.setFlowContext(session)
	cfg := &config.Config{Acquisition: config.AcquisitionConfig{FallbackPolicy: "allow", WebSearchEnabled: true}, Integrations: config.IntegrationsConfig{Web: config.WebDataConfig{Enabled: true}}}
	s.configService = config.NewConfigService(nil, cfg, "", filepath.Join(t.TempDir(), "config.yaml"))
	factory := &contractAcquisitionFactory{runtimeFlowFactory: base}
	s.SetRuntimeFactory(factory)
	service, err := s.dataService(ctx, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	request := dataservice.FindRequest{Need: dataservice.Need{Name: "各市人口", What: dataservice.What{Category: "stats.annual_indicator"}, Extent: "「派生范围」 外扩 10%", Scale: "auto"}, IncludeFallback: true}
	found, err := service.Find(ctx, request)
	if err != nil || !found.Resolved.Extent.Pending || found.State != "exploration_required" {
		t.Fatal(found, err)
	}
	choice := ""
	for _, c := range found.Fallback {
		if c.Available && c.Kind == "web_search" {
			choice = c.ID
		}
	}
	if choice == "" {
		t.Fatal("missing available fallback")
	}
	p.Data = []plan.Data{{ID: "population", Name: request.Need.Name, From: "acquire", FindID: found.FindID, Choice: choice, Geometry: "table"}, {ID: "boundary", Name: "派生范围", From: "step", Step: "derive", Output: "boundary_result", Geometry: "polygon"}}
	p.Steps[0].Uses = []string{"population", "boundary"}
	p.Steps = append([]plan.Step{{ID: "derive", Title: "生成派生范围", Capabilities: []string{"read"}, Instructions: "生成派生范围", Outputs: []flow.OutputDeclaration{{Label: "boundary_result", Type: "file", Format: "geojson"}}}}, p.Steps...)
	raw, _ := json.Marshal(p)
	if _, err = (sessionPlanService{server: s, run: session}).ProposePlan(ctx, raw); err != nil {
		t.Fatal(err)
	}
	cards, err := s.ListPlans(ctx, session.SessionID, session.ProjectPath)
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	var preview dataservice.Selection
	if err = json.Unmarshal(cards[0].Selections["population"], &preview); err != nil || !preview.Rows[0].Item.Extent.Pending {
		t.Fatal("Plan must preserve unresolved upstream scope until execution", preview, err)
	}
	dir, err := config.ResolveSessionDir(session.ProjectPath, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	stateForGet := &flowrun.RunState{SessionID: session.SessionID, SessionDir: dir, CartoPlan: cards[0].Plan, Selections: cards[0].Selections}
	if _, err = s.getPlanData(ctx, stateForGet, p.Data[0]); err == nil || !strings.Contains(err.Error(), "DATA_EXTENT_UNRESOLVED") {
		t.Fatal("missing upstream data must block before creating the expert", err)
	}
	boundary := filepath.Join(dir, "boundary.geojson")
	expertRuns, resumes, producers, consumers := 0, 0, 0, 0
	registeredPath := ""
	checkScope := func(callCtx context.Context) error {
		selection, ok := dataservice.AcquisitionSelection(callCtx)
		if !ok || selection.FindID != found.FindID || len(selection.Rows) != 1 || selection.Rows[0].Item.Extent == nil || selection.Rows[0].Item.Extent.Pending || len(selection.Rows[0].Item.Extent.BBox) != 4 {
			return fmt.Errorf("expert scope still pending or wrong query: %+v", selection)
		}
		for i, expected := range []float64{109.9, 30.9, 111.1, 32.1} {
			if math.Abs(selection.Rows[0].Item.Extent.BBox[i]-expected) > 1e-9 {
				return fmt.Errorf("frozen extent changed or expanded repeatedly: %+v", selection.Rows[0].Item.Extent)
			}
		}
		return nil
	}
	factory.build = func(role string) adk.Agent {
		if role == deepagent.DataAcquisitionExpertName {
			return &derivedExtentCheckpointAgent{check: func(callCtx context.Context) error { expertRuns++; return checkScope(callCtx) }, register: func(callCtx context.Context) error {
				resumes++
				if err := checkScope(callCtx); err != nil {
					return err
				}
				selection, _ := dataservice.AcquisitionSelection(callCtx)
				path := filepath.Join(dir, "population.csv")
				if err := os.WriteFile(path, []byte("region,year,value\n郑州,2025,1308\n"), 0600); err != nil {
					return err
				}
				service := dataservice.FromContext(callCtx)
				if service == nil {
					return fmt.Errorf("resume lost data service")
				}
				result, err := service.Register(callCtx, dataservice.RegisterRequest{FindID: selection.FindID, Row: selection.Rows[0].Row, Path: path, Kind: "web_search", URL: "https://example.test/population", License: "CC0"})
				if err != nil || len(result.Rows) != 1 || len(result.Rows[0].Paths) != 1 {
					return fmt.Errorf("derived extent registration failed: %+v %v", result, err)
				}
				registeredPath = result.Rows[0].Paths[0]
				return nil
			}}
		}
		return contractAcquisitionAgent{name: role, run: func(_ context.Context, input *adk.AgentInput) (string, error) {
			if strings.Contains(input.Messages[0].Content, "生成派生范围") && registeredPath == "" {
				producers++
				err := os.WriteFile(boundary, []byte(`{"type":"Polygon","coordinates":[[[110,31],[111,31],[111,32],[110,32],[110,31]]]}`), 0600)
				raw, _ := json.Marshal(flowrun.Receipt{Status: "completed", Outputs: map[string]any{"boundary_result": boundary}})
				return string(raw), err
			}
			consumers++
			if registeredPath == "" || !strings.Contains(input.Messages[0].Content, registeredPath) || !strings.Contains(input.Messages[0].Content, "- 派生范围：\""+boundary+"\"（step）") {
				return "", fmt.Errorf("consumer missing registered data")
			}
			return `{"status":"completed","outputs":{"结果":"已使用登记的数据"},"issues":[]}`, nil
		}}
	}
	session.Mode = executionModeBuild
	s.setFlowContext(session)
	prepared, err := s.prepareCartographicPlan(ctx, session, mustClonePlan(p), cards[0])
	if err != nil {
		t.Fatal(err)
	}
	_, controller := s.flowDependencies()
	state, err := controller.StartFromPlan(ctx, flowrun.StartRequest{SessionID: session.SessionID, SessionDir: dir, ProjectPath: session.ProjectPath, OriginalSource: session.OriginalSource, CartoPlan: prepared.Snapshot.Plan, Inputs: prepared.Inputs, Selections: prepared.Selections}, prepared.Executable)
	if err != nil || state.Status != "waiting_user" || state.Cursor != 1 || expertRuns != 1 || producers != 1 || consumers != 0 {
		t.Fatalf("did not reach derived acquisition checkpoint: state=%+v err=%v", state, err)
	}
	var saved dataservice.Selection
	if json.Unmarshal(state.Selections["population"], &saved) != nil || saved.Rows[0].Item.Extent.Pending || len(saved.Rows[0].Item.Extent.BBox) != 4 {
		t.Fatal("resolved extent was not persisted in controller snapshot")
	}
	// Changing the upstream file while waiting must not silently change the
	// acquisition scope already handed off and confirmed by this checkpoint.
	if err = os.WriteFile(boundary, []byte(`{"type":"Polygon","coordinates":[[[80,10],[81,10],[81,11],[80,11],[80,10]]]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.popPendingInterrupt(nil, session.SessionID); !ok {
		t.Fatal("missing persisted ADK interrupt")
	}
	if err = s.SetPlanRuntime(s.cartoRuntime.options); err != nil {
		t.Fatal(err)
	}
	pending, err := s.restoreFlowPending(ctx, session.SessionID, session.ProjectPath, state.ID)
	if err != nil {
		t.Fatal("disk restoration failed", err)
	}
	targets := map[string]any{}
	for _, ic := range pending.Contexts {
		if ic.IsRootCause {
			targets[ic.ID] = map[string]any{"confirmed": true}
		}
	}
	s.resumeTask(ctx, nil, pending, targets)
	_, controller = s.flowDependencies()
	state, err = controller.Snapshot(session.SessionID)
	if err != nil || state == nil || state.Status != "completed" || state.AcquiredInputs["population"] != registeredPath || registeredPath == "" || expertRuns != 1 || resumes != 1 || producers != 1 || consumers != 1 {
		t.Fatalf("derived extent restart/register sequence failed: state=%+v err=%v runs=%d resumes=%d producers=%d consumers=%d", state, err, expertRuns, resumes, producers, consumers)
	}
}
