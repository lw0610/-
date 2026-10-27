package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/testutil"
	"github.com/gorilla/mux"
	"gopkg.in/yaml.v3"
)

// Python 内容验收通过此辅助测试调用真正的 Go 解析器、编译器和 HTTP 接口。
func TestBuiltinFlowsParseAndCompile(t *testing.T) {
	a, manager, project, home := realFlowAPI(t)
	profiles := []string{"v1", "v2", "current", "zttzt"}
	for _, version := range profiles {
		path := filepath.Join("testdata", "builtin_flows-"+version+"-profile.yaml")
		if version == "current" {
			path = filepath.Join(project, "carto_agent", "defaults", "agent_configs", "default", "agent.yaml")
		}
		raw, err := os.ReadFile(path)
		if version == "zttzt" {
			raw, err = []byte(testutil.V4ZttztProfileYAML), nil
		}
		if err != nil {
			t.Fatal(err)
		}
		var data config.AgentConfigData
		if err = yaml.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		if err = manager.SaveConfig("", "builtin-"+version, &data); err != nil {
			t.Fatal(err)
		}
		got, err := manager.GetConfigData("builtin-" + version)
		if err != nil {
			t.Fatal(err)
		}
		if got.Version != "2.0" || version != "zttzt" && got.Runtime.Agents["knowledge_engineer"].Executor != "route_knowledge" {
			t.Fatal("迁移/目标方案未保留专用执行器")
		}
	}
	store, err := a.flowStore()
	if err != nil {
		t.Fatal(err)
	}
	library, err := a.resourceStore()
	if err != nil {
		t.Fatal(err)
	}
	basemap := os.Getenv("AUTOCARTO_BUILTIN_BASEMAP_DIR")
	actualBasemap := basemap != ""
	if basemap == "" {
		basemap = filepath.Join(home, "basemap-fixture")
		if err = os.MkdirAll(basemap, 0700); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(project, "autocarto", "flows", "system", "marching-route", "resources", "data", "中国底图.csv"))
		if err != nil {
			t.Fatal(err)
		}
		rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\ufeff"))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 9 {
			t.Fatal("底图应为表头加8条数据")
		}
		for _, row := range rows[1:] {
			if filepath.IsAbs(row[1]) || strings.Contains(row[1], "..") {
				t.Fatal("清单包含越界路径")
			}
			for _, filename := range []string{row[1], strings.TrimSuffix(row[1], filepath.Ext(row[1])) + ".shx", strings.TrimSuffix(row[1], filepath.Ext(row[1])) + ".dbf", strings.TrimSuffix(row[1], filepath.Ext(row[1])) + ".prj"} {
				if err = os.WriteFile(filepath.Join(basemap, filename), []byte("existence-only fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	var selectedFlow string
	model := apiFlowModelFunc(func(_ context.Context, request flow.ModelRequest) (string, error) {
		var fixture struct {
			Flow  json.RawMessage     `json:"flow"`
			Tools map[string][]string `json:"tools"`
		}
		raw, err := os.ReadFile(filepath.Join(project, "carto_agent", "internal", "flow", "testdata", "compile-model-"+selectedFlow+".json"))
		if err != nil {
			return "", err
		}
		if err = json.Unmarshal(raw, &fixture); err != nil {
			return "", err
		}
		if request.Kind == "flow" {
			return string(fixture.Flow), nil
		}
		var condition *flow.ConditionRule
		if selectedFlow == "marching-route" && request.Step == "s1" {
			condition = &flow.ConditionRule{Op: "or", Args: []flow.ConditionRule{{Op: "not_provided", Input: "路线数据集"}, {Op: "truthy", Input: "重新抽取"}}}
		}
		encoded, _ := json.Marshal(map[string]any{"suggested_tools": fixture.Tools[request.Step], "missing_capabilities": []string{}, "expected_outputs": []any{}, "condition": condition, "notes": "固定内容验收模型"})
		return string(encoded), nil
	})
	compiler := flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: filepath.Join(home, "data", "flow_cache"), LoadEnvironment: func(ctx context.Context, scheme string) (flow.Environment, error) {
		env, err := a.loadFlowEnvironment(ctx, scheme)
		env.Model = model
		env.ModelName = "固定内容验收模型"
		return env, err
	}})
	a.SetFlowCompiler(compiler)
	router := mux.NewRouter()
	a.SetupRoutes(router)
	results := []map[string]any{}
	for _, version := range profiles {
		for _, id := range []string{"marching-route", "statistical-choropleth", "traffic-route", "data-to-map"} {
			selectedFlow = id
			stored, err := store.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			doc := flow.Parse(stored.FlowMD)
			if flow.HasErrors(doc.Diagnostics) {
				t.Fatalf("%s 解析错误：%+v", id, doc.Diagnostics)
			}
			if doc.Meta.ID != id {
				t.Fatalf("编号不一致：%s / %s", id, doc.Meta.ID)
			}
			// 无模型计划校验同样不能有错误；其条件提示与模型回退警告如实保留。
			fallback := flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: filepath.Join(home, "fallback-cache"), LoadEnvironment: a.loadFlowEnvironment})
			withoutModel, err := fallback.BasePlan(context.Background(), id, "builtin-"+version)
			if err != nil {
				t.Fatal(err)
			}
			if withoutModel.HasErrors() {
				t.Fatalf("%s/%s 无模型计划校验失败：%+v", version, id, withoutModel.Diagnostics)
			}
			for _, diagnostic := range withoutModel.Diagnostics {
				if diagnostic.Code == "FLOW_INPUT_TYPE_IMPLICIT" {
					t.Fatalf("%s 仍有依赖模型猜类型的输入：%+v", id, diagnostic)
				}
			}
			for _, input := range withoutModel.Inputs {
				if input.Key != input.Label {
					t.Fatalf("输入名称与稳定键不一致：%+v", input)
				}
				if (input.Type == "file" || input.Type == "folder") && !input.Remember {
					t.Fatalf("内置数据输入没有默认记忆：%+v", input)
				}
			}
			// Seed historical successful results separately; the public preview is local.
			if _, err := compiler.Compile(context.Background(), id, "builtin-"+version, true); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/api/flows/"+id+"?scheme=builtin-"+version, nil)
			req.Header.Set("X-AutoCarto-Contract-Version", "v1")
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			if rr.Code != 200 {
				t.Fatalf("preview HTTP %d: %s", rr.Code, rr.Body.String())
			}
			var response struct {
				Data struct {
					Plan flow.FlowPlan `json:"plan"`
				} `json:"data"`
			}
			if err = json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if len(response.Data.Plan.Inputs) == 0 {
				t.Fatal("预览响应未提供输入表单")
			}
			plan, err := compiler.Plan(context.Background(), id, "builtin-"+version)
			if err != nil {
				t.Fatal(err)
			}
			if id == "marching-route" {
				if plan.Resources[0].BaseDirFromInput != "底图数据" || !plan.Resources[0].Entries[0].ExistsUnknown {
					t.Fatal("未选择数据时应显示输入关联而非缺失文件")
				}
				plan = flow.ResolveInputResources(plan, map[string]any{"底图数据": basemap})
			}
			if plan.HasErrors() {
				t.Fatalf("%s/%s 计划校验存在错误：%+v", version, id, plan.Diagnostics)
			}
			for _, d := range plan.Diagnostics {
				if d.Code == "FLOW_COMPILE_LLM_FAILED" || d.Code == "FLOW_CAPABILITY_MISSING_IN_ROLE" || d.Code == "FLOW_REF_UNRESOLVED" || d.Code == "FLOW_CONDITION_UNPARSED" {
					t.Fatalf("%s/%s 不完整绑定：%+v", version, id, d)
				}
			}
			if id == "marching-route" {
				business := builtinBusinessSteps(plan)
				if len(business) != 11 || len(plan.Resources[0].Entries) != 8 || len(plan.InputConstraints) != 1 || len(plan.Acquisition.Items) != 8 {
					t.Fatal("路线完整步骤、资源或互斥约束不符")
				}
				rule := business[0].Condition.Rule
				for _, test := range []struct {
					dataset     any
					force, want bool
				}{{nil, false, true}, {"/ready/dataset", false, false}, {"/ready/dataset", true, true}, {nil, true, true}} {
					run := &flowrun.RunState{Inputs: map[string]any{"史料原文": map[string]any{"$ref": "current_user_message"}, "路线数据集": test.dataset, "重新抽取": test.force}}
					value, err := flowrun.EvaluateRule(rule, run)
					if err != nil || value != test.want {
						t.Fatalf("已有数据集/重抽优先级错误：%+v %v", test, err)
					}
				}
				if business[9].OnFailure != "stop" || business[9].ExpectedOutputs[0].Type != "review_final" || business[10].Condition.Rule.Op != "falsy" {
					t.Fatal("质量终态与非审查导出条件错误")
				}
				for _, input := range plan.Inputs {
					if input.Type == "choice" && input.Default != nil {
						found := false
						for _, option := range input.Options {
							if option == input.Default {
								found = true
							}
						}
						if !found {
							t.Fatalf("默认值不在选项中：%+v", input)
						}
					}
				}
			}
			if version == "current" {
				expectedRoles := map[string]string{"数据工程专家": "data_engineer", "制图布局专家": "cartographer", "空间分析专家": "spatial_analyst", "质量反馈智能体": "quality_feedback", "制图知识抽取专家": "knowledge_engineer"}
				for _, slot := range plan.RoleSlots {
					if slot.Resolution.RoleID != expectedRoles[slot.Name] {
						t.Fatalf("默认方案绑定改变：%s %+v", id, slot)
					}
				}
			}
			if id == "traffic-route" && version == "current" {
				env, e := a.loadFlowEnvironment(context.Background(), "builtin-current")
				if e != nil {
					t.Fatal(e)
				}
				planning := env.AcquisitionEnvironment()
				planning.Integrations = map[string]bool{"osm": true}
				for _, scope := range []struct{ region, level string }{{"郑州市", "市"}, {"410105", "县"}} {
					result, issues := acquire.Plan(context.Background(), plan, map[string]any{"交通数据": map[string]any{"$acquire": true}, "区域": scope.region, "行政区级别": scope.level}, planning, nil)
					if len(issues) > 0 {
						t.Fatal(issues)
					}
					found := false
					for _, item := range result.Items {
						if item.Category == "admin.division" {
							found = true
							if item.Chosen == nil || item.Chosen.SourceID != "source.builtin.cn-admin" || item.Params["level"] != scope.level || item.Params["within"] != scope.region {
								t.Fatalf("交通范围/层级没有选中正确行政区来源：%+v", item)
							}
						}
					}
					if !found {
						t.Fatal("交通清单缺少行政区项")
					}
				}
			}
			for _, resource := range plan.Resources {
				for _, entry := range resource.Entries {
					if !entry.Exists && !entry.ExistsUnknown {
						t.Fatalf("资源条目不存在：%+v", entry)
					}
				}
			}
			results = append(results, map[string]any{"flow_id": id, "scheme": "builtin-" + version, "steps": len(builtinBusinessSteps(plan)), "system_steps": len(plan.Steps) - len(builtinBusinessSteps(plan)), "diagnostics": plan.Diagnostics, "actual_basemap": actualBasemap})
		}
	}
	encoded, _ := json.Marshal(results)
	t.Log("BUILTIN_FLOW_RESULT=" + string(encoded))
}

func builtinBusinessSteps(plan *flow.FlowPlan) []flow.StepPlan {
	steps := []flow.StepPlan{}
	for _, step := range plan.Steps {
		if step.Kind != "acquire" {
			steps = append(steps, step)
		}
	}
	return steps
}
