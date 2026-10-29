package websocket

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Only the model and QGIS normalization boundary are faked. Discovery, plan
// validation/storage, executor handoff, registration, hashing and lookup use
// their production services, all inside the test's private session directory.
type contractAcquisitionFactory struct {
	*runtimeFlowFactory
	build          func(string) adk.Agent
	normalizations int
}

func (f *contractAcquisitionFactory) BuildRole(_ context.Context, _ string, _ *config.AgentConfigFile, name string) (adk.Agent, error) {
	f.roles = append(f.roles, name)
	return f.build(name), nil
}

func (f *contractAcquisitionFactory) InvokeAcquisitionTool(_ context.Context, name string, args map[string]any) (any, error) {
	if name != "data_normalize" {
		return nil, fmt.Errorf("unexpected tool or network access: %s", name)
	}
	f.normalizations++
	input, _ := args["input"].(map[string]any)
	path, _ := input["path"].(string)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil || len(rows) != 2 || !reflect.DeepEqual(rows[0], []string{"region", "year", "value"}) {
		return nil, fmt.Errorf("test normalizer requires a real, readable statistics table")
	}
	output, _ := args["output"].(string)
	if err = os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return nil, err
	}
	if err = os.WriteFile(output, raw, 0600); err != nil {
		return nil, err
	}
	return map[string]any{"path": output, "count": 1, "fields": map[string]float64{"region": 1, "year": 1, "value": 1}}, nil
}

type contractAcquisitionAgent struct {
	name string
	run  func(context.Context, *adk.AgentInput) (string, error)
}

func (a contractAcquisitionAgent) Name(context.Context) string { return a.name }
func (a contractAcquisitionAgent) Description(context.Context) string {
	return "contract-test model boundary"
}
func (a contractAcquisitionAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	content, err := a.run(ctx, input)
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		if err != nil {
			gen.Send(&adk.AgentEvent{Err: err})
			return
		}
		gen.Send(adk.EventFromMessage(schema.AssistantMessage(content, nil), nil, schema.Assistant, ""))
	}()
	return iter
}

func TestLegacyPlanningAcquisitionProductionContract(t *testing.T) {
	for _, outcome := range []string{"registered", "unregistered", "changed", "deleted"} {
		t.Run(outcome, func(t *testing.T) {
			ctx := context.Background()
			s, base, session, p := cartoRuntimeFixture(t)
			configureAcquisitionTestRole(t, s)
			session.Mode = executionModePlan
			s.setFlowContext(session)
			cfg := &config.Config{Acquisition: config.AcquisitionConfig{NetworkPolicy: "direct", FallbackPolicy: "allow", WebSearchEnabled: true}, Integrations: config.IntegrationsConfig{Web: config.WebDataConfig{Enabled: true}}}
			s.configService = config.NewConfigService(nil, cfg, "", filepath.Join(t.TempDir(), "config.yaml"))
			factory := &contractAcquisitionFactory{runtimeFlowFactory: base}
			s.SetRuntimeFactory(factory)
			service, err := s.dataService(ctx, session.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			need := dataservice.FindRequest{Need: dataservice.Need{Name: "各市人口", What: dataservice.What{Category: "stats.annual_indicator"}, Extent: "bbox:31,110,36,117", Scale: "auto", Fields: map[string]string{"region": "region", "year": "year", "value": "value"}}, IncludeFallback: true}
			found, err := service.Find(ctx, need)
			if err != nil || found.State != "exploration_required" || len(found.Rows) != 1 || len(found.Rows[0].Candidates) != 0 {
				t.Fatalf("expected real discovery with no direct source: %+v %v", found, err)
			}
			choice := ""
			for _, candidate := range found.Fallback {
				if candidate.Kind == "web_search" && candidate.Available {
					choice = candidate.ID
				}
			}
			if choice == "" {
				t.Fatal("test catalog must expose a real available fallback")
			}
			// Exercise persisted v1 acquisition snapshots, never submit a new v1 plan.
			p.APIVersion = plan.LegacyAPIVersion
			p.Steps[0].Inputs, p.Steps[0].Produces = nil, nil
			p.Steps[0].Outputs = []flow.OutputDeclaration{{Label: "结果", Type: "text"}}
			p.Data = []plan.Data{{ID: "population", Name: need.Need.Name, From: "acquire", Geometry: "table", FindID: found.FindID, Choice: choice}}
			p.Facts = []plan.Fact{{Name: "统计年份", Value: "2025", From: "用户要求"}}
			p.Steps[0].Uses = []string{"population"}
			legacy, err := s.prepareLegacyPlan(ctx, session, p, nil)
			if err != nil {
				t.Fatal(err)
			}
			store, err := planStore(session)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Save(legacy.Snapshot, 0); err != nil {
				t.Fatal(err)
			}
			if factory.normalizations != 0 || len(factory.roles) != 0 {
				t.Fatal("restoring legacy plan metadata must not execute an acquisition")
			}
			cards, err := s.ListPlans(ctx, session.SessionID, session.ProjectPath)
			if err != nil || len(cards) != 1 {
				t.Fatal(cards, err)
			}
			snapshot := cards[0]
			var frozen dataservice.Selection
			if err = json.Unmarshal(snapshot.Selections["population"], &frozen); err != nil || !frozen.RequiresDiscovery() || frozen.FindID != found.FindID || !reflect.DeepEqual(frozen.Request, need) || len(frozen.Rows) != 1 || frozen.Rows[0].Route != dataservice.RouteExplore || frozen.Rows[0].Candidate.ID != choice {
				t.Fatalf("plan did not pin the validated discovery result: %+v %v", frozen, err)
			}
			dir, err := config.ResolveSessionDir(session.ProjectPath, session.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			stateForGet := &flowrun.RunState{SessionID: session.SessionID, SessionDir: dir, CartoPlan: snapshot.Plan, Selections: snapshot.Selections}
			initial, err := s.getPlanData(ctx, stateForGet, snapshot.Plan.Data[0])
			if err != nil || initial.Preparation == nil || initial.Value != nil || initial.Preparation.Role.ID != "source_researcher" {
				t.Fatalf("validated fallback did not become an acquisition handoff: %+v %v", initial, err)
			}
			var handedOff dataservice.Selection
			if err = json.Unmarshal(initial.Preparation.Selection, &handedOff); err != nil || !reflect.DeepEqual(handedOff, frozen) || !strings.Contains(initial.Preparation.Messages[0].Content, "2025") {
				t.Fatal("handoff changed frozen query or lost plan facts", err)
			}
			experts, consumers := 0, 0
			registeredPath := ""
			factory.build = func(role string) adk.Agent {
				return contractAcquisitionAgent{name: role, run: func(callCtx context.Context, input *adk.AgentInput) (string, error) {
					if role != "source_researcher" {
						consumers++
						if outcome != "registered" || registeredPath == "" || len(input.Messages) == 0 || !strings.Contains(input.Messages[0].Content, registeredPath) {
							return "", fmt.Errorf("consumer ran without the verified registered path")
						}
						return `{"status":"completed","summary":"完成","outputs":{"结果":"真实数据已交给消费者"},"issues":[]}`, nil
					}
					experts++
					selection, ok := dataservice.AcquisitionSelection(callCtx)
					if !ok || !reflect.DeepEqual(selection, frozen) {
						return "", fmt.Errorf("expert did not receive the exact query frozen by plan service")
					}
					if outcome == "unregistered" {
						return `{"status":"completed","outputs":{"population":"/invented/population.csv"}}`, nil
					}
					path := filepath.Join(dir, "population.csv")
					if err := os.WriteFile(path, []byte("region,year,value\n郑州,2025,1308\n"), 0600); err != nil {
						return "", err
					}
					activeService := dataservice.FromContext(callCtx)
					if activeService == nil {
						return "", fmt.Errorf("production agent context lost its data service")
					}
					registered, err := activeService.Register(callCtx, dataservice.RegisterRequest{FindID: selection.FindID, Row: selection.Rows[0].Row, Path: path, Kind: "web_search", URL: "https://example.test/statistics", License: "CC0", Attribution: "isolated contract fixture"})
					if err != nil || len(registered.Rows) != 1 || len(registered.Rows[0].Paths) != 1 {
						return "", fmt.Errorf("registration failed: %+v %v", registered, err)
					}
					registeredPath = registered.Rows[0].Paths[0]
					verified, err := s.getPlanData(callCtx, stateForGet, snapshot.Plan.Data[0])
					if err != nil || verified.Preparation != nil || verified.Value != registeredPath {
						return "", fmt.Errorf("plan could not collect real registered data: %+v %v", verified, err)
					}
					if outcome == "changed" {
						err = os.WriteFile(registeredPath, []byte("region,year,value\n郑州,2025,1\n"), 0600)
					} else if outcome == "deleted" {
						err = os.Remove(registeredPath)
					}
					return "文件已登记，具体路径由系统核验。", err
				}}
			}
			session.Mode = executionModeBuild
			s.setFlowContext(session)
			prepared, err := s.prepareCartographicPlan(ctx, session, mustClonePlan(snapshot.Plan), snapshot)
			if err != nil {
				t.Fatal(err)
			}
			// Starting a preview revalidates its choice and renews SelectedAt. The
			// running expert must receive this newly frozen execution snapshot.
			if err = json.Unmarshal(prepared.Selections["population"], &frozen); err != nil {
				t.Fatal(err)
			}
			_, controller := s.flowDependencies()
			state, err := controller.StartFromPlan(ctx, flowrun.StartRequest{SessionID: session.SessionID, SessionDir: dir, ProjectPath: session.ProjectPath, Inputs: prepared.Inputs, OriginalSource: session.OriginalSource, Origin: "orchestrator", CartoPlan: prepared.Snapshot.Plan, Selections: prepared.Selections}, prepared.Executable)
			if err != nil || state == nil || experts != 1 {
				t.Fatalf("acquisition execution failed: state=%+v experts=%d err=%v", state, experts, err)
			}
			if outcome == "registered" {
				if state.Status != "completed" || consumers != 1 || state.AcquiredInputs["population"] != registeredPath || factory.normalizations != 0 {
					t.Fatalf("registered data did not reach consumer: status=%s failures=%+v consumers=%d", state.Status, state.Steps, consumers)
				}
				// Statistical tables are verified by the real Go table path;
				// a fake QGIS normalization must not make this contract pass.
				contents, err := os.ReadFile(registeredPath)
				if err != nil || string(contents) != "region,year,value\n郑州,2025,1308\n" {
					t.Fatalf("registered statistical values changed: %q %v", contents, err)
				}
			} else if state.Status != "needs_replan" || consumers != 0 || state.AcquiredInputs["population"] != nil {
				t.Fatalf("missing or altered registration released consumer: %+v consumers=%d", state, consumers)
			} else {
				code := "DATA_REGISTER_MISMATCH"
				if outcome == "unregistered" {
					code = "DATA_DISCOVERY_INCOMPLETE"
				}
				if !strings.Contains(state.Steps[0].LastFailure, code) {
					t.Fatalf("unexpected failure prevented testing the registration guard: %s", state.Steps[0].LastFailure)
				}
			}
		})
	}
}
