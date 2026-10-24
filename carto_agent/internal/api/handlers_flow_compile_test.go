package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/gorilla/mux"
)

type apiFlowModelFunc func(context.Context, flow.ModelRequest) (string, error)

func (f apiFlowModelFunc) Complete(ctx context.Context, r flow.ModelRequest) (string, error) {
	return f(ctx, r)
}

func realFlowAPI(t *testing.T) (*API, *config.AgentConfigManager, string, string) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	project := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	home := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", project)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(project, "autocarto"))
	t.Setenv("AUTOCARTO_HOME", home)
	configDir := filepath.Join(home, "config", "agent_configs")
	path := filepath.Join(configDir, "compile-scheme", "agent.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	raw := `version: '2.0'
runtime:
  shared_tool_packages: []
  agents:
    main-renamed:
      kind: agent
      display_name: 编排角色
      enabled: true
      is_main: true
      tool_packages: []
    reader-renamed:
      kind: executor
      executor: route_knowledge
      display_name: 知识抽取专家
      enabled: true
      tool_packages: []
    data-renamed:
      kind: agent
      display_name: 数据工程专家
      enabled: true
      tool_packages: [data-loading, statistical-data-tools]
    map-renamed:
      kind: agent
      display_name: 制图专家
      enabled: true
      tool_packages: [cartography-styling, print-layout, route-cartography]
    analysis-renamed:
      kind: agent
      display_name: 空间分析专家
      enabled: true
      tool_packages: [vector-geometry]
    review-renamed:
      kind: agent
      display_name: 质量反馈智能体
      enabled: true
      tool_packages: [quality-feedback]
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	manager := config.NewAgentConfigManager(configDir)
	assets := filepath.Join(project, "autocarto")
	service := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{SkillsDir: filepath.Join(assets, "skills", "system"), BuiltinSkillsDir: filepath.Join(assets, "skills", "system"), UserSkillsDir: filepath.Join(home, "skills"), GlobalSkillsDir: filepath.Join(home, "skills"), BuiltinToolsDir: filepath.Join(assets, "tools", "system"), UserToolsDir: filepath.Join(home, "tools"), RuntimeToolDirs: []string{filepath.Join(assets, "tools", "system")}, RuntimeSkillDirs: []string{filepath.Join(assets, "skills", "system")}}, manager)
	a := NewAPI(nil, manager, nil, service, filepath.Join(assets, "skills", "system"), nil)
	return a, manager, project, home
}

func TestFlowPreviewHTTPRealCatalogAndLegacySuccessfulResults(t *testing.T) {
	a, manager, project, home := realFlowAPI(t)
	a.SetFlowStore(flow.NewStore("", filepath.Join(home, "flows"), bindings.New(filepath.Join(home, "config", "resource_bindings.yaml"))))
	env, err := a.loadFlowEnvironment(context.Background(), "compile-scheme")
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Packages) < 20 || len(env.Capabilities) < 100 {
		t.Fatalf("未读取正式工具归属：%d 包/%d 工具", len(env.Packages), len(env.Capabilities))
	}
	roles := map[string]flow.RoleSpec{}
	for _, role := range env.Roles {
		roles[role.ID] = role
	}
	contains := func(values []string, want string) bool {
		for _, value := range values {
			if value == want {
				return true
			}
		}
		return false
	}
	if !contains(roles["map-renamed"].Tools, "layout_compose") || contains(roles["main-renamed"].Tools, "layout_compose") || !contains(roles["reader-renamed"].Tools, "marching_route_compile_dataset") || !contains(roles["data-renamed"].Tools, "hitl_request") {
		t.Fatalf("共享、系统、显式或执行器自动挂载不正确：%+v", roles)
	}
	// 使用真实 AssetCatalog/配置/资源/HTTP，仅模型回复由固定 JSON 夹具隔离外部调用。
	store, err := a.flowStore()
	if err != nil {
		t.Fatal(err)
	}
	resources, err := a.resourceStore()
	if err != nil {
		t.Fatal(err)
	}
	var currentFlow string
	calls := 0
	compiler := flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: resources, CacheDir: filepath.Join(home, "data", "flow_cache"), LoadEnvironment: func(ctx context.Context, scheme string) (flow.Environment, error) {
		environment, err := a.loadFlowEnvironment(ctx, scheme)
		if err != nil {
			return environment, err
		}
		environment.ModelName = "固定 HTTP 验收模型"
		environment.Model = apiFlowModelFunc(func(_ context.Context, request flow.ModelRequest) (string, error) {
			calls++
			var fixture struct {
				Flow  json.RawMessage     `json:"flow"`
				Tools map[string][]string `json:"tools"`
			}
			raw, err := os.ReadFile(filepath.Join(project, "carto_agent", "internal", "flow", "testdata", "compile-model-"+currentFlow+".json"))
			if err != nil {
				return "", err
			}
			if err = json.Unmarshal(raw, &fixture); err != nil {
				return "", err
			}
			if request.Kind == "flow" {
				return string(fixture.Flow), nil
			}
			response := map[string]any{"suggested_tools": fixture.Tools[request.Step], "missing_capabilities": []string{}, "expected_outputs": []any{}, "condition": nil, "notes": "固定 JSON 验收"}
			encoded, _ := json.Marshal(response)
			return string(encoded), nil
		})
		return environment, nil
	}})
	a.SetFlowCompiler(compiler)
	router := mux.NewRouter()
	a.SetupRoutes(router)
	request := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		encoded, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		req.Header.Set("X-AutoCarto-Contract-Version", "v1")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, rr.Code, rr.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if data, ok := result["data"].(map[string]any); ok {
			return data
		}
		return result
	}
	for _, id := range []string{"marching-route", "statistical-choropleth", "traffic-route"} {
		currentFlow = id
		if _, err = store.Import(filepath.Join(project, "docs", "refactor", "examples", id), flow.ImportOptions{}); err != nil {
			t.Fatal(err)
		}
		// Seed an old successful result with an isolated fixture model. HTTP only
		// reads that compatible cache and never schedules additional analysis.
		if _, err := compiler.Compile(context.Background(), id, "compile-scheme", true); err != nil {
			t.Fatal(err)
		}
		response := request("GET", "/api/flows/"+id+"?scheme=compile-scheme", nil, 200)
		encoded, _ := json.Marshal(response["plan"])
		var plan flow.FlowPlan
		if err = json.Unmarshal(encoded, &plan); err != nil {
			t.Fatal(err)
		}
		if plan.APIVersion != "flow-plan/v1" || plan.Scheme != "compile-scheme" || len(plan.Steps) == 0 {
			t.Fatal("HTTP 未返回真实执行计划")
		}
		for _, d := range plan.Diagnostics {
			if d.Code == "FLOW_COMPILE_LLM_FAILED" {
				t.Fatalf("真实候选校验失败：%+v", d)
			}
		}
		if id == "marching-route" {
			if plan.HasErrors() {
				t.Fatalf("输入关联清单应留待输入校验：%+v", plan.Diagnostics)
			}
			linked := false
			for _, r := range plan.Resources {
				linked = linked || r.BaseDirFromInput != ""
			}
			if !linked {
				t.Fatal("行军流程没有关联文件夹输入")
			}
		} else if plan.HasErrors() {
			t.Fatalf("无外部数据资源的示例不应报错：%+v", plan.Diagnostics)
		}
		before := calls
		preview := request("GET", "/api/flows/"+id+"?scheme=compile-scheme", nil, 200)
		if preview["stale"] != nil || preview["plan"] == nil || calls != before {
			t.Fatal("GET 预览触发了模型或未命中计划")
		}
		if id == "traffic-route" {
			// 与步骤无关的正文修改不进入任何增强内容键：保存后计划仍完整，且不再调用模型。
			loaded, _ := store.Get(id)
			saved := request("PUT", "/api/flows/"+id+"?scheme=compile-scheme", map[string]any{"flow_md": loaded.FlowMD + "\n## 补充说明\n验收补充"}, 200)
			if saved["plan"] == nil || saved["stale"] != nil || calls != before || saved["plan"].(map[string]any)["plan_status"] != flow.PlanStatusReady {
				t.Fatal("保存后计划不完整或重复调用了模型")
			}
		}
		t.Logf("开发示例 HTTP compile：%s，%d 步，%d 条诊断", id, len(plan.Steps), len(plan.Diagnostics))
	}
	data, err := manager.GetConfigData("compile-scheme")
	if err != nil {
		t.Fatal(err)
	}
	data.Flows = &config.FlowConfig{Enabled: []string{"traffic-route"}}
	if err = manager.SaveConfig("", "compile-scheme", data); err != nil {
		t.Fatal(err)
	}
	list := request("GET", "/api/flows?scheme=compile-scheme", nil, 200)
	items := list["data"].([]any)
	for _, value := range items {
		item := value.(map[string]any)
		if item["enabled"] != (item["id"] == "traffic-route") || item["diagnostics_summary"] == nil {
			t.Fatal(item)
		}
	}
	// 启停流程等与编译无关的方案修改不使增强结果失效。
	before := calls
	current := request("GET", "/api/flows/traffic-route?scheme=compile-scheme", nil, 200)
	if current["stale"] != nil || current["plan"].(map[string]any)["plan_status"] != flow.PlanStatusReady || calls != before {
		t.Fatal("与编译无关的方案修改使执行计划失效")
	}
}

func TestFlowPreviewDoesNotRequireModelAndRejectsUnknownScheme(t *testing.T) {
	a, _, _, _ := realFlowAPI(t)
	store, err := a.flowStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Create("---\n名称: 无模型测试\n编号: test-flow\n---\n## 步骤\n### 检查\n- 负责：用户\n- 做法：确认结果\n- 完成标准：「确认」（确认结果）\n", ""); err != nil {
		t.Fatal(err)
	}
	compiler, err := a.FlowCompiler()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compiler.Plan(context.Background(), "test-flow", "compile-scheme")
	if err != nil || plan.Steps[0].Kind != "confirm" {
		t.Fatal(err)
	}
	found := false
	for _, d := range plan.Diagnostics {
		if d.Code == "FLOW_COMPILE_LLM_FAILED" {
			t.Fatalf("没有模型时不应报告模型调用失败：%+v", d)
		}
		if d.Code == "FLOW_PLAN_BASIC" {
			found = true
		}
	}
	if found || plan.PlanStatus != flow.PlanStatusBasic {
		t.Fatal("本地预览不应包含已移除的编译模型提示")
	}
	if _, err = compiler.Plan(context.Background(), "test-flow", "unknown-scheme"); err == nil || !strings.Contains(err.Error(), "配置") {
		t.Fatal("未知方案被默认为当前方案", err)
	}
}
