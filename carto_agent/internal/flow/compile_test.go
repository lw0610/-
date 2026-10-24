package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/resource"
)

type modelFunc func(context.Context, ModelRequest) (string, error)

func (f modelFunc) Complete(ctx context.Context, r ModelRequest) (string, error) { return f(ctx, r) }

func compilerProject(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(root, "autocarto"))
	return root
}

func compilerFixture(t *testing.T, raw string, environment *Environment) (*Compiler, *Store, *resource.Library) {
	t.Helper()
	project := compilerProject(t)
	home := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", home)
	store := NewStore(filepath.Join(home, "builtin"), filepath.Join(home, "flows"), bindings.New(filepath.Join(home, "config", "resource_bindings.yaml")))
	if raw != "" {
		if _, err := store.Create(raw, "test-flow"); err != nil {
			t.Fatal(err)
		}
	}
	library := resource.NewLibrary(filepath.Join(project, "autocarto", "resources", "system"), filepath.Join(home, "resources"), store.Bindings)
	compiler := NewCompiler(CompilerOptions{Store: store, Resources: library, LoadEnvironment: func(_ context.Context, scheme string) (Environment, error) {
		env := *environment
		if scheme != "" {
			env.Scheme = scheme
		}
		return env, nil
	}, CacheDir: filepath.Join(home, "data", "flow_cache")})
	return compiler, store, library
}

func fixtureEnvironment() Environment {
	catalog := []CapabilitySpec{
		{Name: "data_load_list", DisplayName: "按清单加载数据", Description: "清单检查底图交通图层加载", PackageIDs: []string{"data"}},
		{Name: "style_set_apply", DisplayName: "应用样式集", Description: "底图符号化样式集对照表标注", PackageIDs: []string{"style"}},
		{Name: "route_draw", DisplayName: "按表绘制路线", Description: "路线专题点拓扑方向", PackageIDs: []string{"route"}},
		{Name: "layout_compose", DisplayName: "按版式一次排版", Description: "版式版面排版主图标题综述图例", PackageIDs: []string{"layout"}},
		{Name: "layout_add_callouts", DisplayName: "添加图文标注", Description: "图文标注图片正文牵引线", PackageIDs: []string{"layout"}},
		{Name: "layout_move_callout", DisplayName: "整组移动图文标注", Description: "审查意见整组移动修改", PackageIDs: []string{"layout"}},
		{Name: "layout_export_image", DisplayName: "导出版面图片", Description: "初版终稿审查PNG导出", PackageIDs: []string{"layout"}},
		{Name: "export_print_layout", DisplayName: "导出版面成果", Description: "导出PNG PDF SVG成果文件", PackageIDs: []string{"layout"}},
		{Name: "prepare_quality_review", DisplayName: "准备质量审查", Description: "初审终审质量审查版面快照", PackageIDs: []string{"review"}},
		{Name: "load_review_images", DisplayName: "读取审查图片", Description: "质量终审图片读取", PackageIDs: []string{"review"}},
		{Name: "data_prepare_statistical", DisplayName: "准备统计数据", Description: "行政区统计表关联缺测指标", PackageIDs: []string{"data"}},
		{Name: "cartography_graduated_style", DisplayName: "分级设色", Description: "统计数值分级设色色阶标注", PackageIDs: []string{"style"}},
		{Name: "clip", DisplayName: "裁剪图层", Description: "边界交通图层裁剪筛选", PackageIDs: []string{"analysis"}},
	}
	roles := []RoleSpec{{ID: "reader-custom", DisplayName: "知识抽取专家", Kind: "executor", Executor: "route_knowledge", Enabled: true, Tools: []string{}, ToolPackages: []string{}}, {ID: "data-custom", DisplayName: "数据工程专家", Kind: "agent", Enabled: true, Tools: []string{"data_load_list", "data_prepare_statistical"}, ToolPackages: []string{"data"}}, {ID: "map-custom", DisplayName: "制图专家", Kind: "agent", Enabled: true, Tools: []string{"style_set_apply", "route_draw", "layout_compose", "layout_add_callouts", "layout_move_callout", "layout_export_image", "export_print_layout", "cartography_graduated_style"}, ToolPackages: []string{"style", "route", "layout"}}, {ID: "review-custom", DisplayName: "质量反馈智能体", Kind: "agent", Enabled: true, Tools: []string{"prepare_quality_review", "load_review_images"}, ToolPackages: []string{"review"}}, {ID: "analysis-custom", DisplayName: "空间分析专家", Kind: "agent", Enabled: true, Tools: []string{"clip"}, ToolPackages: []string{"analysis"}}}
	return Environment{Scheme: "测试方案", SchemeRevision: "scheme1", CatalogRevision: "catalog1", Roles: roles, Capabilities: catalog, Packages: []PackageSpec{}, ModelName: "固定夹具模型"}
}

func fixedModel(t *testing.T, id string, calls *int) JSONModel {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "compile-model-"+id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Flow  json.RawMessage     `json:"flow"`
		Tools map[string][]string `json:"tools"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	return modelFunc(func(_ context.Context, request ModelRequest) (string, error) {
		*calls++
		if request.Temperature != 0 || !request.JSON {
			t.Fatal("模型没有采用温度 0 和 JSON 要求")
		}
		if request.Kind == "flow" {
			return string(fixture.Flow), nil
		}
		value := inferredStep{SuggestedTools: fixture.Tools[request.Step], MissingCapabilities: []string{}, ExpectedOutputs: []OutputDeclaration{}, Notes: "固定输出夹具"}
		encoded, _ := json.Marshal(value)
		return string(encoded), nil
	})
}

func TestCompilerThreeExamplesMatchFixtures(t *testing.T) {
	for _, id := range []string{"marching-route", "statistical-choropleth", "traffic-route"} {
		t.Run(id, func(t *testing.T) {
			env := fixtureEnvironment()
			calls := 0
			env.Model = fixedModel(t, id, &calls)
			compiler, store, _ := compilerFixture(t, "", &env)
			project := compilerProject(t)
			item, err := store.Import(filepath.Join(project, "docs", "refactor", "examples", id), ImportOptions{})
			if err != nil {
				t.Fatal(err)
			}
			base := ""
			if id == "marching-route" {
				base = filepath.Join(filepath.Dir(store.UserDir), "basemap")
				if err = os.MkdirAll(base, 0700); err != nil {
					t.Fatal(err)
				}
				// 真实读取 CSV 与运行输入；使用隔离小文件，不依赖用户底图。
				csv := "名称,路径\n省域,province.geojson\n河流,river.geojson\n"
				if err = os.WriteFile(filepath.Join(item.Dir, "resources", "data", "中国底图.csv"), []byte(csv), 0600); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"province.geojson", "river.geojson"} {
					if err = os.WriteFile(filepath.Join(base, name), []byte(`{"type":"FeatureCollection","features":[]}`), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			plan, err := compiler.Compile(context.Background(), id, "", false)
			if err != nil {
				t.Fatal(err)
			}
			if base != "" {
				plan = ResolveInputResources(plan, map[string]any{"底图数据": base})
			}
			if plan.HasErrors() {
				t.Fatalf("示例有错误：%+v", plan.Diagnostics)
			}
			if calls != len(plan.Steps)+1 {
				t.Fatalf("调用次数 %d，步骤数 %d", calls, len(plan.Steps))
			}
			for index, step := range plan.Steps {
				if step.Instructions != Parse(item.FlowMD).Steps[index].Instructions {
					t.Fatal("做法原文被改写")
				}
				for _, d := range plan.Diagnostics {
					if d.Code == "FLOW_COMPILE_LLM_FAILED" || d.Code == "FLOW_CAPABILITY_MISSING_IN_ROLE" {
						t.Fatalf("夹具辅助失败：%+v", d)
					}
				}
			}
			if id == "marching-route" {
				if plan.Steps[0].Kind != "executor" || plan.Inputs[0].Type != "conversation_text" || len(plan.InputConstraints) != 1 || plan.Inputs[4].Default != false {
					t.Fatalf("路线输入、约束或执行器错误")
				}
				if plan.Steps[0].Condition.Rule.Input != "史料原文" {
					t.Fatal("条件未绑定 key")
				}
			}
			if id == "statistical-choropleth" && plan.Inputs[2].Type != "layer" {
				t.Fatal("模型 layer 类型未保留")
			}
			// 比较完整语义计划；仅移除运行时间/指纹和替换隔离绝对根。
			snapshot := *plan
			snapshot.CompiledAt = time.Time{}
			snapshot.CompileKey = "<cache-key>"
			snapshot.LinkFingerprint = "<link-fingerprint>"
			snapshot.LinkedAt = time.Time{}
			snapshot.Enrichment.UpdatedAt = nil
			encoded, _ := json.MarshalIndent(snapshot, "", "  ")
			normalized := strings.ReplaceAll(string(encoded), filepath.ToSlash(filepath.Dir(store.UserDir)), "<runtime>")
			normalized = strings.ReplaceAll(normalized, filepath.ToSlash(project), "<project>") + "\n"
			path := filepath.Join("testdata", "compile-plan-"+id+".json")
			if os.Getenv("AUTOCARTO_UPDATE_FLOW_FIXTURES") == "1" {
				if err = os.WriteFile(path, []byte(normalized), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if normalized != string(want) {
				t.Fatalf("编译结果与夹具不一致：%s", path)
			}
		})
	}
}

const testFlow = "---\n名称: 通用测试流程\n编号: test-flow\n版本: 2.0.0\n---\n## 需要准备\n- 原文（与「文件夹」二选一）：用户在对话中提供的原文\n- 文件夹（与「原文」二选一）：数据文件夹\n- 开关（是/否，默认否）\n- 纸张（默认 A4；可选值：A3、A4）\n## 步骤\n### 第1步 检查\n- 负责：数据工程专家\n- 做法：读取「原文」，检查「文件夹」\n- 完成标准：「结果」（文本）\n"

func hasCode(plan *FlowPlan, code string) bool {
	for _, d := range plan.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestCompilerFallbackAndRunGuard(t *testing.T) {
	env := fixtureEnvironment()
	compiler, store, _ := compilerFixture(t, testFlow, &env)
	plan, err := compiler.EnsureCompiled(context.Background(), "test-flow", "")
	if err != nil {
		t.Fatalf("%v；诊断：%+v", err, plan)
	}
	if plan.PlanStatus != PlanStatusBasic || hasCode(plan, "FLOW_PLAN_BASIC") || hasCode(plan, "FLOW_COMPILE_LLM_FAILED") || plan.Inputs[0].Key != "原文" || plan.Inputs[0].Type != "conversation_text" || plan.Inputs[1].Type != "folder" || plan.Inputs[2].Default != false || plan.Inputs[3].Type != "choice" || len(plan.InputConstraints) != 1 {
		t.Fatalf("回退结果错误：%+v", plan)
	}
	if !reflect.DeepEqual(plan.InputConstraints[0].Inputs, []string{"原文", "文件夹"}) {
		t.Fatal(plan.InputConstraints)
	}
	invalid := strings.Replace(testFlow, "数据工程专家", "不存在的专家", 1)
	if _, err = store.Save("test-flow", invalid); err != nil {
		t.Fatal(err)
	}
	plan, err = compiler.EnsureCompiled(context.Background(), "test-flow", "")
	var validation *ValidationError
	if !errors.As(err, &validation) || !hasCode(plan, "FLOW_ROLE_NOT_FOUND") {
		t.Fatalf("错误计划未拦截：%v", err)
	}
	empty := []string{}
	env.EnabledFlows = &empty
	if _, err = compiler.EnsureCompiled(context.Background(), "test-flow", ""); !errors.Is(err, ErrFlowDisabled) {
		t.Fatal(err)
	}
	if _, err = compiler.Compile(context.Background(), "test-flow", "", true); err != nil {
		t.Fatalf("禁用不应阻止只读计划解析：%v", err)
	}
}

func TestCompilerRoleBindingPriority(t *testing.T) {
	roles := []RoleSpec{{ID: "exact", DisplayName: "数据", Enabled: true}, {ID: "数据", DisplayName: "数据工程专家", Enabled: true}, {ID: "second", DisplayName: "数据分析专家", Enabled: false}}
	for _, test := range []struct{ label, id, code string }{{"数据", "exact", ""}, {"second", "second", "FLOW_ROLE_DISABLED"}, {"专家", "", "FLOW_ROLE_AMBIGUOUS"}, {"没有", "", "FLOW_ROLE_NOT_FOUND"}, {"系统自动", "", ""}, {"人工", "", ""}} {
		t.Run(test.label, func(t *testing.T) {
			role, diagnostics := bindRole(StepDoc{ID: "s1", Role: test.label}, roles)
			if test.id != "" && (role == nil || role.ID != test.id) {
				t.Fatal(role)
			}
			if test.code != "" && (len(diagnostics) != 1 || diagnostics[0].Code != test.code) {
				t.Fatal(diagnostics)
			}
		})
	}
}

func TestCompilerRetryValidationAndDeclaredAuthority(t *testing.T) {
	env := fixtureEnvironment()
	requests := []ModelRequest{}
	env.Model = modelFunc(func(_ context.Context, r ModelRequest) (string, error) {
		requests = append(requests, r)
		if r.Kind == "flow" {
			if len(requests) == 1 {
				return `{"inputs":[],"input_constraints":[],"triggers":[]}`, nil
			}
			return `{"inputs":[{"key":"original","label":"原文","type":"conversation_text","options":[]},{"key":"directory","label":"文件夹","type":"folder","options":[]},{"key":"flag","label":"开关","type":"text","options":["错误选项"]},{"key":"paper","label":"纸张","type":"text","options":["错误选项"]}],"input_constraints":[],"triggers":["测试"]}`, nil
		}
		if len(requests) == 3 {
			return `{"suggested_tools":["invented_tool"],"missing_capabilities":[],"expected_outputs":[],"condition":null,"notes":""}`, nil
		}
		return `{"suggested_tools":["data_load_list"],"missing_capabilities":[],"expected_outputs":[{"label":"错误覆盖","type":"file","required":false}],"condition":null,"notes":"完成"}`, nil
	})
	compiler, _, _ := compilerFixture(t, testFlow, &env)
	plan, err := compiler.Compile(context.Background(), "test-flow", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 4 || !strings.Contains(requests[1].Prompt, "未通过校验") || !strings.Contains(requests[3].Prompt, "不在候选") {
		t.Fatalf("重试没有反馈校验错误：%+v", requests)
	}
	if hasCode(plan, "FLOW_COMPILE_LLM_FAILED") || plan.Steps[0].ExpectedOutputs[0].Label != "结果" || plan.Inputs[2].Type != "boolean" || plan.Inputs[3].Type != "choice" || !reflect.DeepEqual(plan.Inputs[3].Options, []string{"A3", "A4"}) {
		t.Fatal("显式声明被模型覆盖")
	}
	if len(plan.Inputs[2].Options) != 0 {
		t.Fatal("布尔属性不应被推断出选项")
	}
}

func TestCompilerStepDiagnosticsAndInference(t *testing.T) {
	tests := []struct {
		name, raw, code string
		prepare         func(*Environment)
	}{
		{"disabled", testFlow, "FLOW_ROLE_DISABLED", func(env *Environment) { env.Roles[1].Enabled = false }},
		{"executor missing", strings.Replace(testFlow, "读取「原文」，检查「文件夹」", "检查「文件夹」", 1), "FLOW_EXECUTOR_INPUT_MISSING", func(env *Environment) { env.Roles[1].Kind = "executor"; env.Roles[1].Executor = "route_knowledge" }},
		{"resource missing", strings.Replace(testFlow, "## 步骤", "## 资源\n- 参考「文档」：resources/docs/no-file.txt\n## 步骤", 1), "FLOW_RESOURCE_NOT_FOUND", nil},
		{"condition unparsed", strings.Replace(testFlow, "- 做法：", "- 条件：如果当前情况合适就执行\n- 做法：", 1), "FLOW_CONDITION_UNPARSED", nil},
		{"forward condition", strings.Replace(testFlow, "- 做法：", "- 条件：第2步完成时执行\n- 做法：", 1), "FLOW_REF_FORWARD", nil},
		{"forward output", strings.Replace(testFlow, "读取「原文」", "读取「未来结果」", 1) + "\n### 第2步 之后\n- 做法：产生值\n- 完成标准：「未来结果」（文本）\n", "FLOW_REF_FORWARD", nil},
		{"duplicate output", testFlow + "\n### 第2步 再检查\n- 做法：检查值\n- 完成标准：「结果」（文本）\n", "FLOW_OUTPUT_DUPLICATE", nil},
		{"unknown quoted", strings.Replace(testFlow, "读取「原文」", "读取「不存在的输入」", 1), "FLOW_REF_UNRESOLVED", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := fixtureEnvironment()
			if test.prepare != nil {
				test.prepare(&env)
			}
			compiler, _, _ := compilerFixture(t, test.raw, &env)
			plan, err := compiler.Compile(context.Background(), "test-flow", "", true)
			if err != nil {
				t.Fatal(err)
			}
			if !hasCode(plan, test.code) {
				t.Fatalf("缺诊断 %s：%+v", test.code, plan.Diagnostics)
			}
		})
	}
	env := fixtureEnvironment()
	env.Roles[1].Tools = nil
	env.Model = modelFunc(func(_ context.Context, r ModelRequest) (string, error) {
		if r.Kind == "flow" {
			return "invalid", nil
		}
		return `{"suggested_tools":["data_load_list"],"missing_capabilities":["未知的数据验证能力"],"expected_outputs":[{"label":"推断结果","type":"text","required":true}],"condition":null,"notes":""}`, nil
	})
	raw := strings.Replace(strings.Replace(testFlow, "读取「原文」", "加载清单，读取「原文」", 1), "「结果」（文本）", "给出检查结论", 1)
	compiler, _, _ := compilerFixture(t, raw, &env)
	plan, err := compiler.Compile(context.Background(), "test-flow", "", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"FLOW_CAPABILITY_MISSING_IN_ROLE", "FLOW_CAPABILITY_NOT_FOUND", "FLOW_OUTPUT_INFERRED"} {
		if !hasCode(plan, code) {
			t.Fatalf("缺 %s：%+v", code, plan.Diagnostics)
		}
	}
	for _, d := range plan.Diagnostics {
		if d.Code == "FLOW_CAPABILITY_MISSING_IN_ROLE" && (d.Fix["role"] != "data-custom" || d.Fix["package"] != "data" || d.Fix["action"] != "add_tool_package") {
			t.Fatal(d)
		}
	}
}

func TestCompilerCacheInvalidationAndNoPreviewModelCalls(t *testing.T) {
	env := fixtureEnvironment()
	calls := 0
	env.Model = modelFunc(func(_ context.Context, _ ModelRequest) (string, error) { calls++; return "invalid", nil })
	compiler, store, _ := compilerFixture(t, testFlow, &env)
	ctx := context.Background()
	if plan, err := compiler.Plan(ctx, "test-flow", ""); err != nil || plan.PlanStatus != PlanStatusBasic || plan.Steps[0].Enrichment != StepEnrichmentNone || calls != 0 {
		t.Fatalf("执行计划触发了模型或状态错误：%v", err)
	}
	first, err := compiler.Compile(ctx, "test-flow", "", false)
	if err != nil {
		t.Fatal(err)
	}
	firstCalls := calls
	if firstCalls != 4 || first.Enrichment.Flow != StepEnrichmentFailed || first.Steps[0].Enrichment != StepEnrichmentFailed || len(first.Enrichment.Failed) != 2 || first.Enrichment.Failed[1].StepID != "s1" || first.Enrichment.Failed[1].Reason != reasonInvalidJSON {
		t.Fatalf("失败没有逐项记录：calls=%d %+v", calls, first.Enrichment)
	}
	second, err := compiler.Compile(ctx, "test-flow", "", false)
	if err != nil || calls != firstCalls || second.CompileKey != first.CompileKey {
		t.Fatal("失败记录在重试间隔内被重试")
	}
	plan, err := compiler.Plan(ctx, "test-flow", "")
	if err != nil || calls != firstCalls || plan.PlanStatus != PlanStatusBasic || plan.Steps[0].Enrichment != StepEnrichmentNone || hasCode(plan, "FLOW_COMPILE_LLM_FAILED") {
		t.Fatalf("执行计划不应展示旧增强失败记录：%v", err)
	}
	unchanged := func(stage, scheme string) {
		t.Helper()
		before := calls
		if _, err := compiler.Compile(ctx, "test-flow", scheme, false); err != nil || calls != before {
			t.Fatalf("%s触发了模型：%v，调用 %d 次", stage, err, calls-before)
		}
	}
	env.SchemeRevision = "scheme2"
	unchanged("方案修订号变化", "")
	env.CatalogRevision = "catalog2"
	unchanged("能力目录修订号变化", "")
	enabled := []string{"test-flow", "other-flow"}
	env.EnabledFlows = &enabled
	unchanged("启停其他流程", "")
	env.Roles[4].Tools = nil
	unchanged("修改与本流程无关的角色", "")
	unchanged("切换到角色相同的另一方案", "另一方案")
	item, _ := store.Get("test-flow")
	attachments := filepath.Join(item.Dir, "resources")
	if err = os.MkdirAll(attachments, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(attachments, "context.txt"), []byte("context"), 0600); err != nil {
		t.Fatal(err)
	}
	unchanged("未引用的附件变化", "")
	if _, err = store.Save("test-flow", testFlow+"\n## 交付\n交付结论"); err != nil {
		t.Fatal(err)
	}
	unchanged("只改交付说明", "")
	beforeForce := calls
	if _, err = compiler.Compile(ctx, "test-flow", "", true); err != nil || calls != beforeForce+4 {
		t.Fatalf("force 未重做全部增强：调用 %d 次", calls-beforeForce)
	}
	compiler.options.RetryAfter = time.Nanosecond
	beforeRetry := calls
	if _, err = compiler.Compile(ctx, "test-flow", "", false); err != nil || calls != beforeRetry+4 {
		t.Fatalf("超过重试间隔的失败记录没有重试：调用 %d 次", calls-beforeRetry)
	}
}

func TestCompilerDataBindingAndFileContentInvalidate(t *testing.T) {
	env := fixtureEnvironment()
	raw := strings.Replace(testFlow, "## 步骤", "## 资源\n- 底图数据「基础数据」：resources/data/list.csv\n## 步骤", 1)
	compiler, store, library := compilerFixture(t, raw, &env)
	item, _ := store.Get("test-flow")
	path := filepath.Join(item.Dir, "resources", "data")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "list.csv"), []byte("名称,路径\n底图,base.geojson\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	plan, err := compiler.Compile(ctx, "test-flow", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(plan, "FLOW_RESOURCE_UNBOUND") {
		t.Fatal(plan.Diagnostics)
	}
	base := filepath.Join(filepath.Dir(store.UserDir), "base")
	if err = os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	if err = library.Bindings.Set("flow:test-flow/"+"path:data/list.csv", base); err != nil {
		t.Fatal(err)
	}
	bound, err := compiler.Compile(ctx, "test-flow", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(bound, "FLOW_DATA_FILE_MISSING") || bound.CompileKey != plan.CompileKey || bound.LinkFingerprint == plan.LinkFingerprint {
		t.Fatal("基准目录绑定未进入指纹")
	}
	file := filepath.Join(base, "base.geojson")
	if err = os.WriteFile(file, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	present, err := compiler.Compile(ctx, "test-flow", "", false)
	if err != nil || hasCode(present, "FLOW_DATA_FILE_MISSING") {
		t.Fatal(err, present.Diagnostics)
	}
	if err = os.WriteFile(file, []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := compiler.Compile(ctx, "test-flow", "", false)
	if err != nil || changed.CompileKey != present.CompileKey || changed.LinkFingerprint == present.LinkFingerprint {
		t.Fatal("同大小外部文件内容变化未使缓存失效")
	}
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	removed, err := compiler.Compile(ctx, "test-flow", "", false)
	if err != nil || !hasCode(removed, "FLOW_DATA_FILE_MISSING") {
		t.Fatal("删除外部文件未被校验发现")
	}
}

func TestCompilerCacheConcurrentAndCorrupt(t *testing.T) {
	env := fixtureEnvironment()
	var count int
	var mu sync.Mutex
	env.Model = modelFunc(func(_ context.Context, _ ModelRequest) (string, error) {
		mu.Lock()
		count++
		mu.Unlock()
		return "invalid", nil
	})
	compiler, _, _ := compilerFixture(t, testFlow, &env)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := compiler.Compile(context.Background(), "test-flow", "", false); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if count != 4 {
		t.Fatalf("并发缓存未合并，模型调用 %d", count)
	}
	paths, _ := filepath.Glob(filepath.Join(compiler.enrichmentDir("test-flow"), "*.json"))
	if len(paths) != 2 {
		t.Fatalf("应有流程级与步骤级各一条记录：%v", paths)
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := compiler.Compile(context.Background(), "test-flow", "", false); err != nil || count != 8 {
		t.Fatal("损坏缓存未重建")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := compiler.Compile(ctx, "test-flow", "", true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestChineseBM25AndCandidateBoundary(t *testing.T) {
	catalog := []CapabilitySpec{{Name: "owned", DisplayName: "完全不相关"}}
	for index := 0; index < 25; index++ {
		catalog = append(catalog, CapabilitySpec{Name: fmt.Sprintf("layout_%02d", index), DisplayName: "版面排版", Description: "主图标题图例"})
	}
	catalog = append(catalog, CapabilitySpec{Name: "english_layout", DisplayName: "无关能力", Description: "网络下载"})
	candidates := CandidateCapabilities("版面排版", []string{"owned"}, catalog, 20)
	if len(candidates) != 21 || candidates[0].Name != "owned" || candidates[1].Name != "layout_00" || candidates[20].Name != "layout_19" {
		t.Fatal(candidates)
	}
	if got := CandidateCapabilities("不存在", []string{"owned"}, catalog, 20); len(got) != 1 {
		t.Fatal("无关能力被填充进候选", got)
	}
}

func TestCompilerRejectsInvalidStepJSONAfterOneRetry(t *testing.T) {
	for _, response := range []string{
		"```json\n{}\n```",
		`{"suggested_tools":null,"missing_capabilities":[],"expected_outputs":[],"condition":null,"notes":""}`,
		`{"suggested_tools":[],"missing_capabilities":[],"expected_outputs":[{"label":"结果","type":"invented","required":true}],"condition":null,"notes":""}`,
		`{"suggested_tools":[],"missing_capabilities":[],"expected_outputs":[],"condition":{"op":"run_code","input":"input_1"},"notes":""}`,
		`{"suggested_tools":[],"missing_capabilities":[],"expected_outputs":[],"condition":{"op":"provided","input":"unknown"},"notes":""}`,
		`{"suggested_tools":[],"missing_capabilities":[],"expected_outputs":[],"condition":null,"notes":"","execute":"anything"}`,
	} {
		t.Run(response, func(t *testing.T) {
			env := fixtureEnvironment()
			calls := 0
			env.Model = modelFunc(func(_ context.Context, r ModelRequest) (string, error) {
				calls++
				if r.Kind == "flow" {
					return "invalid", nil
				}
				return response, nil
			})
			raw := strings.Replace(testFlow, "- 做法：", "- 条件：当前情况适合时执行\n- 做法：", 1)
			compiler, _, _ := compilerFixture(t, raw, &env)
			plan, err := compiler.Compile(context.Background(), "test-flow", "", true)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 4 || len(plan.Steps[0].SuggestedTools) != 0 || !hasCode(plan, "FLOW_COMPILE_LLM_FAILED") || !hasCode(plan, "FLOW_CONDITION_UNPARSED") {
				t.Fatalf("非法输出未按两次上限回退：%d %+v", calls, plan.Diagnostics)
			}
		})
	}
}

func TestCompilerInferredOutputResolvesReferencesAndCompoundCondition(t *testing.T) {
	env := fixtureEnvironment()
	env.Model = modelFunc(func(_ context.Context, r ModelRequest) (string, error) {
		if r.Kind == "flow" {
			return "invalid", nil
		}
		if r.Step == "s1" {
			return `{"suggested_tools":[],"missing_capabilities":[],"expected_outputs":[{"label":"中间结果","type":"text","format":"","required":true}],"condition":null,"notes":""}`, nil
		}
		return `{"suggested_tools":[],"missing_capabilities":[],"expected_outputs":[],"condition":{"op":"and","args":[{"op":"provided","input":"原文"},{"op":"not","arg":{"op":"falsy","input":"开关"}},{"op":"output_exists","step":"s1","label":"中间结果"}]},"notes":""}`, nil
	})
	raw := strings.Replace(testFlow, "「结果」（文本）", "生成检查结论", 1) + "\n### 第2步 整理\n- 负责：系统自动\n- 条件：原文和开关满足要求且中间结果存在\n- 做法：读取「中间结果」整理\n- 完成标准：「最终结论」（文本）\n"
	compiler, _, _ := compilerFixture(t, raw, &env)
	plan, err := compiler.Compile(context.Background(), "test-flow", "", false)
	if err != nil {
		t.Fatal(err)
	}
	step := plan.Steps[1]
	if hasCode(plan, "FLOW_REF_UNRESOLVED") || hasCode(plan, "FLOW_CONDITION_UNPARSED") || step.Kind != "auto" || step.Condition.Rule.Op != "and" || !reflect.DeepEqual(step.Refs.Outputs, []OutputRef{{Step: "s1", Label: "中间结果"}}) {
		t.Fatalf("辅助推断后的引用或条件错误：%+v", plan)
	}
	if !reflect.DeepEqual(step.Refs.Inputs, []string{"原文", "开关"}) {
		t.Fatal(step.Refs.Inputs)
	}
}

func TestCompilerResourceBindingsSurviveDeclarationReorder(t *testing.T) {
	env := fixtureEnvironment()
	resources := "## 资源\n- 底图数据「基础数据」：resources/data/list.csv\n- 参考「说明」：resources/docs/readme.txt\n"
	raw := strings.Replace(testFlow, "## 步骤", resources+"## 步骤", 1)
	compiler, store, library := compilerFixture(t, raw, &env)
	item, _ := store.Get("test-flow")
	for _, entry := range []struct{ path, content string }{{"resources/data/list.csv", "名称,路径\n底图,base.geojson\n"}, {"resources/docs/readme.txt", "说明内容"}} {
		path := filepath.Join(item.Dir, entry.path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(entry.content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := compiler.Compile(context.Background(), "test-flow", "", false)
	if err != nil {
		t.Fatal(err)
	}
	key := first.Resources[0].Key
	base := filepath.Join(filepath.Dir(store.UserDir), "base-data")
	if err = os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(base, "base.geojson"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = library.Bindings.Set("flow:test-flow/"+key, base); err != nil {
		t.Fatal(err)
	}
	reordered := strings.Replace(raw, resources, "## 资源\n- 参考「说明」：resources/docs/readme.txt\n- 底图数据「基础数据」：resources/data/list.csv\n", 1)
	if _, err = store.Save("test-flow", reordered); err != nil {
		t.Fatal(err)
	}
	second, err := compiler.Compile(context.Background(), "test-flow", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Resources[1].Key != key || second.Resources[1].BaseDir != base || !second.Resources[1].Entries[0].Exists || hasCode(second, "FLOW_RESOURCE_UNBOUND") {
		t.Fatalf("重排声明导致绑定迁移：%+v", second.Resources)
	}
}

func TestCompilerExecutorResourceContractsAndRelinkRepair(t *testing.T) {
	env := fixtureEnvironment()
	env.Roles[1].Kind, env.Roles[1].Executor = "executor", "route_knowledge"
	compiler, store, _ := compilerFixture(t, testFlow, &env)
	plan, err := compiler.Compile(context.Background(), "test-flow", "", false)
	if err != nil || !hasCode(plan, "FLOW_EXECUTOR_RESOURCE_MISSING") || len(plan.Resources) != 0 {
		t.Fatalf("missing declaration silently defaulted: %v %+v", err, plan)
	}
	item, _ := store.Get("test-flow")
	write := func(relative, content string) {
		t.Helper()
		path := filepath.Join(item.Dir, "resources", relative)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("route.csv", "topology,side,route_type,draw_type,style_id\nstandard,friendly,main,bezier_arrow,own\n")
	symbols := "symbol_type,svg_path\nstart_flag,point.svg\nwaypoint,point.svg\n"
	for _, name := range []string{"symbols", "second"} {
		write(name+"/对照表.csv", symbols)
		write(name+"/point.svg", `<svg xmlns="http://www.w3.org/2000/svg"><circle r="1"/></svg>`)
	}
	declared := strings.Replace(testFlow, "## 步骤", "## 资源\n- 符号「自己的符号」：resources/symbols\n- 路线样式「自己的样式」：resources/route.csv\n## 步骤", 1)
	if _, err := store.Save("test-flow", declared); err != nil {
		t.Fatal(err)
	}
	plan, err = compiler.Compile(context.Background(), "test-flow", "", false)
	if err != nil || !hasCode(plan, "FLOW_EXECUTOR_RESOURCE_MISSING") {
		t.Fatal("unreferenced declarations accepted", err)
	}
	referenced := strings.Replace(declared, "读取「原文」", "使用「自己的符号」「自己的样式」，读取「原文」", 1)
	if _, err := store.Save("test-flow", referenced); err != nil {
		t.Fatal(err)
	}
	ready, err := compiler.EnsureRunnable(context.Background(), "test-flow", "")
	if err != nil || len(ready.Resources) != 2 {
		t.Fatalf("valid contract: %v %+v", err, ready)
	}
	write("symbols/对照表.csv", "symbol_type,svg_path\nstart_flag,point.svg\n")
	broken, stale, err := compiler.Relink(context.Background(), "test-flow", "")
	if err != nil || stale || !hasCode(broken, "FLOW_RESOURCE_CONTRACT_UNSATISFIED") || broken.CompileKey != ready.CompileKey || !broken.CompiledAt.Equal(ready.CompiledAt) {
		t.Fatalf("resource edit did not invalidate only link: %v stale=%v %+v", err, stale, broken)
	}
	write("symbols/对照表.csv", symbols)
	repaired, stale, err := compiler.Relink(context.Background(), "test-flow", "")
	if err != nil || stale || repaired.HasErrors() || repaired.CompileKey != ready.CompileKey || !repaired.CompiledAt.Equal(ready.CompiledAt) {
		t.Fatalf("resource repair required recompile: %v stale=%v %+v", err, stale, repaired)
	}
	duplicate := strings.Replace(referenced, "## 步骤", "- 符号「第二符号」：resources/second\n## 步骤", 1)
	duplicate = strings.Replace(duplicate, "使用「自己的符号」", "使用「第二符号」「自己的符号」", 1)
	if _, err := store.Save("test-flow", duplicate); err != nil {
		t.Fatal(err)
	}
	ambiguous, err := compiler.Compile(context.Background(), "test-flow", "", false)
	if err != nil || !hasCode(ambiguous, "FLOW_EXECUTOR_RESOURCE_AMBIGUOUS") {
		t.Fatal("ambiguous symbols accepted", err)
	}
}

func TestCompilerAvailablePlansAndChangesDuringGeneration(t *testing.T) {
	env := fixtureEnvironment()
	compiler, store, _ := compilerFixture(t, testFlow, &env)
	if _, err := store.Create(strings.Replace(testFlow, "数据工程专家", "不存在的角色", 1), "bad-flow"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(testFlow, "disabled-flow"); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(store.UserDir, "broken-flow")
	if err := os.MkdirAll(broken, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(store.UserDir, "test-flow", "FLOW.md"), filepath.Join(broken, "FLOW.md")); err != nil {
		t.Fatal(err)
	}
	enabled := []string{"test-flow", "bad-flow", "broken-flow"}
	env.EnabledFlows = &enabled
	plans, err := compiler.AvailablePlans(context.Background(), "")
	if err != nil || len(plans) != 1 || plans[0].FlowID != "test-flow" {
		t.Fatalf("坏或禁用流程进入可用摘要：%v %+v", err, plans)
	}
	calls := 0
	env.Model = modelFunc(func(context.Context, ModelRequest) (string, error) { calls++; return "invalid", nil })
	env.ModelName = "另一个模型"
	if _, err = compiler.EnsureCompiled(context.Background(), "test-flow", ""); err != nil || calls != 0 {
		t.Fatalf("运行入口调用模型或拒绝基础计划: %v calls=%d", err, calls)
	}
	if _, err = compiler.AvailablePlans(context.Background(), ""); err != nil || calls != 0 {
		t.Fatalf("可用流程调用模型: %v calls=%d", err, calls)
	}
	compiler.options.LoadEnvironment = func(context.Context, string) (Environment, error) {
		return Environment{}, fmt.Errorf("方案服务不可用")
	}
	if _, err = compiler.AvailablePlans(context.Background(), ""); err == nil {
		t.Fatal("方案服务失败被伪装成空列表")
	}
}
