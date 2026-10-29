package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// This fixture replaces only model/QGIS execution. Proposal validation, session
// artifact resolution, executable construction, receipt verification and run
// persistence all use their production paths.
type datasetContinuationAgent struct {
	dataset string
	export  string
	calls   []string
}

func (*datasetContinuationAgent) Name(context.Context) string { return "cartographer" }
func (*datasetContinuationAgent) Description(context.Context) string {
	return "模拟已有路线数据集的制图和导出"
}
func (a *datasetContinuationAgent) Run(_ context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		var text strings.Builder
		for _, message := range input.Messages {
			text.WriteString(message.Content)
			text.WriteByte('\n')
		}
		a.calls = append(a.calls, text.String())
		outputs := map[string]any{}
		var err error
		switch len(a.calls) {
		case 1:
			if !strings.Contains(text.String(), a.dataset) || !strings.Contains(text.String(), "（session，dataset）") {
				err = fmt.Errorf("route-cartography 未收到已登记数据集的精确路径和类型")
			} else if content, readErr := os.ReadFile(filepath.Join(a.dataset, "routes.csv")); readErr != nil {
				err = readErr
			} else if !strings.Contains(string(content), "existing-route") {
				err = fmt.Errorf("route-cartography 未读取已有路线数据")
			}
			outputs["路线图层"] = []string{"existing-route-layer"}
		case 2:
			if !strings.Contains(text.String(), "existing-route-layer") {
				err = fmt.Errorf("导出步骤未收到前一步图层产出")
			} else {
				err = os.WriteFile(a.export, []byte("%PDF-1.4\nmock export fixture\n%%EOF\n"), 0600)
			}
			outputs["路线图PDF"] = a.export
		default:
			err = fmt.Errorf("已有成果续作只应执行制图和导出，实际执行第 %d 次", len(a.calls))
		}
		if err != nil {
			gen.Send(&adk.AgentEvent{Err: err})
			return
		}
		receipt, _ := json.Marshal(map[string]any{"status": "completed", "summary": "使用已有数据集完成当前步骤", "outputs": outputs, "issues": []string{}})
		gen.Send(adk.EventFromMessage(schema.AssistantMessage(string(receipt), nil), nil, schema.Assistant, ""))
	}()
	return iter
}

func TestIndexedDatasetContinuesThroughProposalBuildAndFlowRun(t *testing.T) {
	for _, reference := range []string{"path_with_kind", "path_infers_dataset", "file_id"} {
		t.Run(reference, func(t *testing.T) {
			ctx := context.Background()
			s, factory, run, p := cartoRuntimeFixture(t)
			store, err := sessionfiles.New(run.ProjectPath, run.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			dataset := filepath.Join(store.Dir, "output", "confirmed-route-dataset")
			if err := os.MkdirAll(dataset, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"places.csv", "routes.csv", "route_paths.csv", "points.csv", "symbol_mapping.csv", "callouts.csv"} {
				if err := os.WriteFile(filepath.Join(dataset, name), []byte("id,name\nexisting-route,已确认路线\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			artifact, err := store.Register(dataset, sessionfiles.Origin{Type: "tool", Tool: "marching_route_compile_dataset", Label: "已确认路线数据集"})
			if err != nil {
				t.Fatal(err)
			}
			data := plan.Data{ID: "confirmed-routes", Name: "已确认路线数据集", From: "session"}
			switch reference {
			case "path_with_kind":
				data.Path, data.Kind = artifact.Path, "dataset"
			case "path_infers_dataset":
				data.Path = artifact.Path
			case "file_id":
				data.FileID = artifact.ID
			}
			env := flow.Environment{
				Scheme: "test", PlanningInstructions: "已有成果直接进入剩余制图步骤",
				Roles: []flow.RoleSpec{{ID: "cartographer", Kind: "agent", Enabled: true, ToolPackages: []string{"route-cartography", "print-layout"}}},
				Packages: []flow.PackageSpec{
					{ID: "route-cartography", DisplayName: "路线制图", Kind: "category", Enabled: true, Tools: []string{"draw_routes"}},
					{ID: "print-layout", DisplayName: "导出地图", Kind: "category", Enabled: true, Tools: []string{"export_layout"}},
				},
			}
			s.cartoRuntime.options.LoadEnvironment = func(context.Context, string) (flow.Environment, error) { return env, nil }
			export := filepath.Join(filepath.Dir(artifact.Path), "route-map.pdf")
			agent := &datasetContinuationAgent{dataset: artifact.Path, export: export}
			factory.agent = agent
			factory.invoke = func(_ context.Context, name string, _ map[string]any) (any, error) {
				if name != "get_layers_info" {
					return nil, fmt.Errorf("续作中出现意外工具：%s", name)
				}
				return map[string]any{"layers": []map[string]any{{"id": "existing-route-layer", "name": "路线图层"}}}, nil
			}
			p.Goal = "从已确认并编译的数据集继续制图和导出"
			p.Data = []plan.Data{data}
			p.Steps = []plan.Step{
				{ID: "draw", Title: "制作路线地图", Capabilities: []string{"route-cartography"}, Uses: []string{data.ID}, Instructions: "使用已确认路线数据集制作路线图层", Outputs: []flow.OutputDeclaration{{Label: "路线图层", Type: "layers"}}},
				{ID: "export", Title: "导出路线地图", Capabilities: []string{"print-layout"}, DependsOn: []string{"draw"}, Instructions: "将前一步路线图层导出为PDF", Outputs: []flow.OutputDeclaration{{Label: "路线图PDF", Type: "file", Format: "pdf"}}},
			}
			p.Deliverables = []flow.OutputDeclaration{{Label: "路线图PDF", Type: "file", Format: "pdf"}}
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			answer, err := (sessionPlanService{server: s, run: run}).ProposePlan(ctx, raw)
			if err != nil || answer["status"] != "ready" || answer["needs_confirmation"] != false {
				t.Fatalf("已有会话成果应接受且不重复确认：answer=%v err=%v", answer, err)
			}
			state, err := s.startPendingCartographicPlan(ctx, run.SessionID)
			if err != nil || state == nil || state.Status != "completed" {
				t.Fatalf("续作未完成：state=%+v err=%v", state, err)
			}
			if len(agent.calls) != 2 || len(factory.roles) != 2 || len(state.Steps) != 2 {
				t.Fatalf("已有成果不应重复抽取：calls=%d roles=%v steps=%+v", len(agent.calls), factory.roles, state.Steps)
			}
			for _, step := range state.Plan.Steps {
				if step.Executor == "route_knowledge" || step.Kind == "confirm" || step.Role != "cartographer" {
					t.Fatalf("续作中出现抽取、人工确认或非制图角色：%+v", step)
				}
			}
			for _, step := range state.Steps {
				if step.Status != "completed" || len(step.Attempts) != 1 || step.AwaitingChoice {
					t.Fatalf("后续步骤应一次完成：%+v", step)
				}
			}
			if _, pending := s.getPendingInterrupt(run.SessionID); pending {
				t.Fatal("已有确认成果续作不应留下新的人工确认")
			}
			if state.Inputs[data.ID] != artifact.Path || state.Result.Deliverables["路线图PDF"] != export {
				t.Fatalf("数据集或最终交付物交接错误：inputs=%v result=%+v", state.Inputs, state.Result)
			}
			if len(state.Plan.Inputs) != 1 || state.Plan.Inputs[0].Type != "dataset" {
				t.Fatalf("目录应以dataset输入进入执行器，不能当文字或单张表：%+v", state.Plan.Inputs)
			}
			persisted, err := (flowrun.FileRunStore{}).Load(ctx, store.Dir, state.ID)
			if err != nil || persisted.Status != "completed" || persisted.Inputs[data.ID] != artifact.Path || persisted.Result.Deliverables["路线图PDF"] != export {
				t.Fatalf("续作持久化不完整：state=%+v err=%v", persisted, err)
			}
			cards, err := s.ListPlans(ctx, run.SessionID, run.ProjectPath)
			if err != nil || len(cards) != 1 || cards[0].Status != "completed" || cards[0].NeedsConfirmation {
				t.Fatalf("恢复后的计划状态错误：cards=%+v err=%v", cards, err)
			}
			resolved := cards[0].Plan.Data[0]
			if resolved.Kind != "dataset" || resolved.Path != artifact.Path || resolved.FileID != artifact.ID || resolved.From != "session" {
				t.Fatalf("恢复后必须保留同一会话成果身份：%+v", resolved)
			}
		})
	}
}
