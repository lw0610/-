package websocket

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/deepagent"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

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
	resolved := map[string]string{"行政区划": "/session/verified-boundaries.gpkg"}
	result, err := prepareDataDiscovery(state, data, selection, resolved)
	if err != nil || result.Preparation == nil || result.Value != nil {
		t.Fatalf("expected preparation without fabricated data: %+v, %v", result, err)
	}
	prepared := result.Preparation
	if prepared.DataID != data.ID || prepared.Role.ID != deepagent.DataAcquisitionExpertName || len(prepared.Messages) != 1 {
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
	messages  []*schema.Message
}

func (*acquisitionContextAgent) Name(context.Context) string {
	return deepagent.DataAcquisitionExpertName
}
func (*acquisitionContextAgent) Description(context.Context) string {
	return "test acquisition context"
}
func (a *acquisitionContextAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	a.selection, a.bound = dataservice.AcquisitionSelection(ctx)
	a.messages = input.Messages
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		gen.Send(adk.EventFromMessage(schema.AssistantMessage("已尝试获取，数据是否登记由执行器独立核验", nil), nil, schema.Assistant, ""))
	}()
	return iter
}

func TestFlowRuntimePassesFrozenAcquisitionToSystemExpert(t *testing.T) {
	s, factory, session, _ := cartoRuntimeFixture(t)
	state, data, selection := acquisitionSnapshotFixture(t)
	result, err := prepareDataDiscovery(state, data, selection, nil)
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
	if !reflect.DeepEqual(factory.roles, []string{deepagent.DataAcquisitionExpertName}) || !agent.bound || !reflect.DeepEqual(agent.selection, selection) || len(agent.messages) != 1 || agent.messages[0].Content != prepared.Messages[0].Content {
		t.Fatalf("system expert did not receive its pinned context: roles=%v bound=%v selection=%+v", factory.roles, agent.bound, agent.selection)
	}
}
