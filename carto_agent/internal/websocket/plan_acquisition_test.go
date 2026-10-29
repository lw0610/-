package websocket

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func configureAcquisitionTestRole(t *testing.T, s *DeepServer) {
	t.Helper()
	env, err := s.cartoRuntime.options.LoadEnvironment(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	env.Roles = append(env.Roles, flow.RoleSpec{ID: "source_researcher", DisplayName: "数据研究员", Provider: "configured-provider", Kind: "agent", Enabled: true, ToolPackages: []string{"data-service"}, Tools: []string{"data_find", "data_get", "data_register"}})
	s.cartoRuntime.options.LoadEnvironment = func(context.Context, string) (flow.Environment, error) { return env, nil }
}

func acquisitionSnapshotFixture(t *testing.T) (*flowrun.RunState, plan.Data, dataservice.Selection) {
	t.Helper()
	data := plan.Data{ID: "population", Name: "各市人口", From: "acquire", FindID: "find-pinned", Choice: "fallback.web_search", Geometry: "table"}
	fallback := dataservice.Candidate{ID: "fallback.web_search", Kind: "web_search", Available: true, Name: "公开统计数据"}
	selection := dataservice.Selection{
		FindID:   data.FindID,
		Request:  dataservice.FindRequest{Need: dataservice.Need{Name: data.Name, What: dataservice.What{Category: "stats.annual_indicator"}, Extent: "河南省", Params: map[string]string{"year": "2025", "indicator": "常住人口"}}, IncludeFallback: true},
		Rows:     []dataservice.SelectedRow{{Route: dataservice.RouteExplore, Row: data.Name, Candidate: fallback, Item: datasource.AcquisitionItem{Category: "stats.annual_indicator", Params: map[string]string{"year": "2025"}}}},
		Fallback: []dataservice.Candidate{fallback},
	}
	raw, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	state := &flowrun.RunState{CartoPlan: &plan.Plan{Goal: "河南各市人口统计图", Data: []plan.Data{data}, Facts: []plan.Fact{{Name: "指标", Value: "常住人口", From: "用户"}}}, Selections: map[string]json.RawMessage{data.ID: raw}, Steps: []flowrun.StepState{{Attempts: []flowrun.Attempt{{Mode: "acquire", DataID: data.ID}}}}}
	return state, data, selection
}

func TestDataDiscoveryHandoffPreservesConfirmedQuery(t *testing.T) {
	state, data, selection := acquisitionSnapshotFixture(t)
	s, _, _, _ := cartoRuntimeFixture(t)
	configureAcquisitionTestRole(t, s)
	resolved := map[string]string{"行政区划": "/session/verified-boundaries.gpkg"}
	result, err := s.prepareDataDiscovery(context.Background(), state, data, selection, resolved)
	if err != nil || result.Preparation == nil || result.Value != nil {
		t.Fatalf("expected preparation without fabricated data: %+v, %v", result, err)
	}
	prepared := result.Preparation
	if prepared.DataID != data.ID || prepared.Role.ID != "source_researcher" || len(prepared.Messages) != 1 {
		t.Fatalf("incorrect expert handoff: %+v", prepared)
	}
	var pinned dataservice.Selection
	if err = json.Unmarshal(prepared.Selection, &pinned); err != nil || !reflect.DeepEqual(selection, pinned) {
		t.Fatalf("selection changed: %+v, %v", pinned, err)
	}
	_, raw, ok := strings.Cut(prepared.Messages[0].Content, "\n")
	var payload struct {
		Goal      string                `json:"goal"`
		Data      plan.Data             `json:"data"`
		Selection dataservice.Selection `json:"selection"`
		Facts     []plan.Fact           `json:"facts"`
		Resolved  map[string]string     `json:"resolved_data"`
	}
	if !ok || json.Unmarshal([]byte(raw), &payload) != nil || !reflect.DeepEqual(payload.Selection, selection) || !reflect.DeepEqual(payload.Resolved, resolved) || !reflect.DeepEqual(payload.Facts, state.CartoPlan.Facts) || !reflect.DeepEqual(payload.Data, data) || payload.Goal != state.CartoPlan.Goal {
		t.Fatalf("handoff lost query inputs or known facts: %s", raw)
	}
}

func TestDataDiscoveryResumeRestoresOnlyCurrentPinnedSelection(t *testing.T) {
	for _, name := range []string{"valid", "missing query", "different query", "different data", "direct", "ordinary step"} {
		t.Run(name, func(t *testing.T) {
			state, data, selection := acquisitionSnapshotFixture(t)
			switch name {
			case "missing query":
				delete(state.Selections, data.ID)
			case "different query":
				state.CartoPlan.Data[0].FindID = "find-other"
			case "different data":
				state.Steps[0].Attempts[0].DataID = "other"
			case "direct":
				selection.Rows[0].Route = dataservice.RouteDirect
				state.Selections[data.ID], _ = json.Marshal(selection)
			case "ordinary step":
				state.Steps[0].Attempts[0].Mode = "run"
			}
			ctx, err := withRunAcquisitionSnapshot(context.Background(), state)
			if name != "valid" && name != "ordinary step" {
				if err == nil {
					t.Fatal("unmatched acquisition snapshot must fail closed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, ok := dataservice.AcquisitionSelection(ctx)
			if name == "ordinary step" {
				if ok {
					t.Fatal("ordinary step received acquisition authority")
				}
			} else if !ok || !reflect.DeepEqual(got, selection) {
				t.Fatalf("resume lost its exact frozen query: %+v", got)
			}
		})
	}
}

type acquisitionContextAgent struct {
	selection dataservice.Selection
	bound     bool
	need      dataservice.AcquisitionNeed
	needBound bool
	messages  []*schema.Message
}

func (*acquisitionContextAgent) Name(context.Context) string {
	return "source_researcher"
}
func (*acquisitionContextAgent) Description(context.Context) string {
	return "test acquisition context"
}
func (a *acquisitionContextAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	a.selection, a.bound = dataservice.AcquisitionSelection(ctx)
	a.need, a.needBound = dataservice.AcquisitionNeedFromContext(ctx)
	a.messages = input.Messages
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		gen.Send(adk.EventFromMessage(schema.AssistantMessage("已尝试获取，数据是否登记由执行器独立核验", nil), nil, schema.Assistant, ""))
	}()
	return iter
}

func TestPlanAcquisitionSelectsOnlyConfiguredDataRoles(t *testing.T) {
	role := flow.RoleSpec{ID: "renamed_collector", DisplayName: "自定义取数角色", Provider: "own-model", Kind: "agent", Enabled: true, Tools: []string{"data_register"}, ToolPackages: []string{"custom-data-tools"}}
	other := role
	other.ID = "another_collector"
	for _, tc := range []struct {
		name  string
		roles []flow.RoleSpec
		steps []flow.StepPlan
		want  string
	}{
		{name: "renamed role", roles: []flow.RoleSpec{role}, want: role.ID},
		{name: "missing capability", roles: []flow.RoleSpec{{ID: "data_engineer", Kind: "agent", Enabled: true}}},
		{name: "disabled", roles: []flow.RoleSpec{{ID: role.ID, Kind: "agent", Tools: role.Tools}}},
		{name: "main cannot be delegated", roles: []flow.RoleSpec{{ID: "main", Kind: "agent", Enabled: true, IsMain: true, Tools: role.Tools}}},
		{name: "executor cannot discover", roles: []flow.RoleSpec{{ID: "fixed_executor", Kind: "executor", Enabled: true, Tools: role.Tools}}},
		{name: "ambiguous", roles: []flow.RoleSpec{role, other}},
		{name: "current binding", roles: []flow.RoleSpec{role, other}, steps: []flow.StepPlan{{Role: other.ID, DataUses: []string{"population"}}}, want: other.ID},
		{name: "consumer binding", roles: []flow.RoleSpec{role, other}, steps: []flow.StepPlan{{Kind: "acquire", DataIDs: []string{"population"}}, {Role: role.ID, DataUses: []string{"population"}}}, want: role.ID},
		{name: "unbound auto is ambiguous", roles: []flow.RoleSpec{role, other}, steps: []flow.StepPlan{{Kind: "auto", DataUses: []string{"population"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, _ := cartoRuntimeFixture(t)
			env := flow.Environment{Roles: tc.roles}
			s.cartoRuntime.options.LoadEnvironment = func(_ context.Context, scheme string) (flow.Environment, error) {
				if scheme != "selected-scheme" {
					t.Fatalf("loaded wrong scheme %q", scheme)
				}
				return env, nil
			}
			state := &flowrun.RunState{Scheme: "selected-scheme", Plan: &flow.FlowPlan{Steps: tc.steps}}
			got, err := s.planAcquisitionRole(context.Background(), state, "population")
			if tc.want == "" {
				if err == nil || got.ID != "" {
					t.Fatalf("must not create or guess a role: %+v %v", got, err)
				}
				return
			}
			if err != nil || got.ID != tc.want || got.Provider != role.Provider || !reflect.DeepEqual(got.ToolPackages, role.ToolPackages) {
				t.Fatalf("configured role was changed: %+v %v", got, err)
			}
		})
	}
}

func TestV2AcquisitionUsesConfiguredRoleAndPreservesNeed(t *testing.T) {
	s, factory, session, _ := cartoRuntimeFixture(t)
	configureAcquisitionTestRole(t, s)
	need := dataservice.AcquisitionNeed{ID: "plan/population", Goal: "使用核验后人口数据", Need: dataservice.Need{Name: "人口表"}, Acceptance: []string{"来源可核验"}, Ledger: []map[string]any{{"method": "existing", "result": "failed"}}}
	state := &flowrun.RunState{Scheme: "test"}
	ledger := []flowrun.PreparationAttempt{{Method: "existing", Result: "failed"}}
	result, err := s.v2Discovery(context.Background(), state, plan.Data{ID: "population"}, need, map[string]any{"name": "人口表"}, ledger)
	if err != nil || result.Preparation == nil || result.Preparation.Role.ID != "source_researcher" || !reflect.DeepEqual(result.Ledger, ledger) {
		t.Fatal(result, err)
	}
	var pinned dataservice.AcquisitionNeed
	if err = json.Unmarshal(result.Preparation.Need, &pinned); err != nil || !reflect.DeepEqual(pinned, need) {
		t.Fatal("need or ledger changed", pinned, err)
	}
	agent := &acquisitionContextAgent{}
	factory.agent = agent
	prepared := result.Preparation
	_, err = (flowRuntimeBinding{server: s}).Run(context.Background(), flowrun.StepRequest{SessionID: session.SessionID, FlowRunID: "v2-discovery", RunID: "v2-acquire", Mode: "prepare", Step: flow.StepPlan{ID: "consume"}, Role: prepared.Role, Messages: prepared.Messages, Acquisition: prepared})
	if err != nil || !agent.needBound || !reflect.DeepEqual(agent.need, need) || !reflect.DeepEqual(factory.roles, []string{"source_researcher"}) {
		t.Fatalf("configured role lost its preparation: %+v %v %v", agent.need, factory.roles, err)
	}
}

func TestAcquisitionResumeRejectsRemovedRoleWithoutSubstitution(t *testing.T) {
	for _, mode := range []string{"acquire", "prepare"} {
		for _, savedRole := range []string{"source_researcher", "removed_legacy_role", ""} {
			t.Run(mode+"/"+savedRole, func(t *testing.T) {
				s, factory, _, _ := cartoRuntimeFixture(t)
				configureAcquisitionTestRole(t, s)
				state := &flowrun.RunState{Scheme: "test", CartoPlan: &plan.Plan{}, Status: "waiting_user", Steps: []flowrun.StepState{{Role: savedRole, Attempts: []flowrun.Attempt{{Mode: mode, Role: savedRole}}}}}
				err := s.validateCartographicResume(context.Background(), state)
				if (savedRole == "source_researcher") != (err == nil) || len(factory.roles) != 0 {
					t.Fatalf("resume guessed or created a replacement: %v %v", factory.roles, err)
				}
			})
		}
	}
}

func TestFlowRuntimePassesFrozenAcquisitionToConfiguredRole(t *testing.T) {
	s, factory, session, _ := cartoRuntimeFixture(t)
	configureAcquisitionTestRole(t, s)
	state, data, selection := acquisitionSnapshotFixture(t)
	result, err := s.prepareDataDiscovery(context.Background(), state, data, selection, nil)
	if err != nil {
		t.Fatal(err)
	}
	agent := &acquisitionContextAgent{}
	factory.agent = agent
	prepared := result.Preparation
	_, err = (flowRuntimeBinding{server: s}).Run(context.Background(), flowrun.StepRequest{SessionID: session.SessionID, FlowRunID: "discovery-run", RunID: "discovery-step", Mode: "acquire", Step: flow.StepPlan{ID: "acquire-population", Kind: "acquire"}, Role: prepared.Role, Messages: prepared.Messages, Acquisition: prepared})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(factory.roles, []string{"source_researcher"}) || !agent.bound || !reflect.DeepEqual(agent.selection, selection) || len(agent.messages) != 1 || agent.messages[0].Content != prepared.Messages[0].Content {
		t.Fatalf("configured role did not receive its pinned context: roles=%v bound=%v selection=%+v", factory.roles, agent.bound, agent.selection)
	}
}
