package websocket

import (
	"context"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/cloudwego/eino/schema"
	"strings"
	"testing"
)

type acquisitionPolicyCompiler struct {
	runtimeFlowCompiler
	env datasource.PlanningEnvironment
}

func (c acquisitionPolicyCompiler) AcquisitionEnvironment(context.Context, string) (datasource.PlanningEnvironment, error) {
	return c.env, nil
}

func acquisitionPolicyFixture(t *testing.T) (sessionFlowService, *flow.FlowPlan) {
	t.Helper()
	server, _, run := flowRuntimeFixture(t, []flow.StepPlan{flowWorkerStep()})
	compiler, _ := server.flowDependencies()
	plan := compiler.(runtimeFlowCompiler).plan
	catalog, err := datasource.Load("../../../autocarto/tools/system/data-acquisition/assets", "")
	if err != nil {
		t.Fatal(err)
	}
	plan.AutoAcquire = "ask"
	plan.Inputs = []flow.InputPlan{{Key: "data", Label: "铁路数据", Type: "file", Required: true, Acquirable: true, AcquireAvailable: true, Modes: []string{"user", "auto"}}, {Key: "region", Label: "范围", Type: "text"}}
	plan.Acquisition = &flow.AcquisitionPlan{Items: []flow.AcquisitionItem{{Key: "rail", Requirement: "铁路", Input: "data", InputType: "file", Category: "transport.railway", Acquirable: true, Required: true, ExtentExpression: "「region」", ScaleExpression: "自动", Params: map[string]string{}}}}
	server.flowCompiler = acquisitionPolicyCompiler{runtimeFlowCompiler{plan}, datasource.PlanningEnvironment{Catalog: catalog, Integrations: map[string]bool{"osm": true, "web": true}, MaxDownloadMB: 2048}}
	return sessionFlowService{server: server, session: run}, plan
}
func TestAcquisitionPolicyInjectionAlwaysIncludesAvailabilityFields(t *testing.T) {
	service, plan := acquisitionPolicyFixture(t)
	for _, policy := range []string{"ask", "allow", "deny"} {
		plan.AutoAcquire = policy
		messages := service.server.injectAvailableFlows(context.Background(), []*schema.Message{schema.UserMessage("test")}, "test")
		if len(messages) != 2 {
			t.Fatal(messages)
		}
		content := messages[1].Content
		for _, part := range []string{"[flow_auto_acquire:" + policy + "]", "[flow_acquire:common]", `"acquirable":true`, `"acquire_available":true`, `"acquirable":false`, `"modes":["user","auto"]`} {
			if !strings.Contains(content, part) {
				t.Fatalf("missing %s: %s", part, content)
			}
		}
	}
}
func TestAcquisitionPreviewAndQueueExposeTheSameChosenSources(t *testing.T) {
	for _, mode := range []string{"auto", "fill"} {
		t.Run(mode, func(t *testing.T) {
			service, plan := acquisitionPolicyFixture(t)
			choice := map[string]any{"$acquire": true}
			if mode == "fill" {
				choice = map[string]any{"$fill": true, "path": t.TempDir()}
				plan.Inputs[0].Type = "folder"
				plan.Inputs[0].Modes = []string{"user", "auto", "fill"}
				plan.Acquisition.Items[0].InputType = "folder"
				plan.Acquisition.Items[0].Entry = "铁路"
				plan.Acquisition.Items[0].Filename = "rail.geojson"
			}
			inputs := map[string]any{"data": choice, "region": "郑州市"}
			preview, err := service.PreviewFlow(context.Background(), "test-flow", inputs)
			if err != nil {
				t.Fatal(err)
			}
			check := preview["input_check"].(map[string]any)
			if check["valid"] != true {
				t.Fatal(check)
			}
			acquired := preview["acquisition_plan"].(*flow.AcquisitionPlan)
			if len(acquired.Items) != 1 || acquired.Items[0].Chosen == nil {
				t.Fatal(acquired)
			}
			result, err := service.StartFlow(context.Background(), "test-flow", inputs)
			if err != nil {
				t.Fatal(err)
			}
			summary := result["acquire"].([]flow.AcquisitionItem)
			if result["accepted"] != true || len(summary) != 1 || summary[0].Chosen.SourceID != acquired.Items[0].Chosen.SourceID {
				t.Fatal(result)
			}
			if plan.Acquisition.Items[0].Chosen != nil {
				t.Fatal("mutated shared plan")
			}
		})
	}
}
func TestAcquisitionPreviewRejectsDeniedUnavailableAndExplicitSources(t *testing.T) {
	for _, test := range []struct {
		name, policy, field, code string
		available                 bool
	}{{"deny", "deny", "", "FLOW_ACQUIRE_DENIED", true}, {"unavailable", "ask", "", "FLOW_INPUT_ACQUIRE_UNAVAILABLE", false}, {"source", "ask", "source", "FLOW_ORCHESTRATOR_SOURCE_FORBIDDEN", true}, {"sources", "ask", "sources", "FLOW_ORCHESTRATOR_SOURCE_FORBIDDEN", true}} {
		t.Run(test.name, func(t *testing.T) {
			service, plan := acquisitionPolicyFixture(t)
			plan.AutoAcquire = test.policy
			plan.Inputs[0].AcquireAvailable = test.available
			choice := map[string]any{"$acquire": true}
			if test.field != "" {
				choice[test.field] = "source.invented"
			}
			_, err := service.StartFlow(context.Background(), "test-flow", map[string]any{"data": choice, "region": "郑州市"})
			if err == nil || !strings.Contains(err.Error(), test.code) {
				t.Fatal(err)
			}
			if len(service.server.pendingFlows) != 0 {
				t.Fatal("invalid acquisition queued")
			}
		})
	}
}
func TestAcquisitionPreviewMissingInputRetainsAcquirableFlag(t *testing.T) {
	service, _ := acquisitionPolicyFixture(t)
	result, err := service.PreviewFlow(context.Background(), "test-flow", nil)
	if err != nil {
		t.Fatal(err)
	}
	problems := result["input_check"].(map[string]any)["problems"].([]flowrun.InputProblem)
	if len(problems) == 0 || !problems[0].Acquirable {
		t.Fatal(problems)
	}
}
