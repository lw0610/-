package flow

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func countingModel(calls *int) JSONModel {
	return modelFunc(func(context.Context, ModelRequest) (string, error) {
		*calls++
		return "{}", nil
	})
}

func TestBasePlanBuiltinFlowsNeverCallModel(t *testing.T) {
	project := compilerProject(t)
	for _, id := range []string{"data-to-map", "marching-route", "statistical-choropleth", "traffic-route"} {
		t.Run(id, func(t *testing.T) {
			env := fixtureEnvironment()
			calls := 0
			env.Model = countingModel(&calls)
			compiler, store, _ := compilerFixture(t, "", &env)
			if _, err := store.Import(filepath.Join(project, "autocarto", "flows", "system", id), ImportOptions{}); err != nil {
				t.Fatal(err)
			}
			plan, err := compiler.BasePlan(context.Background(), id, "")
			if err != nil {
				t.Fatal(err)
			}
			if calls != 0 {
				t.Fatalf("基础计划调用了模型 %d 次", calls)
			}
			if plan.PlanStatus != PlanStatusPartial {
				t.Fatalf("有模型但尚未增强时应为 partial，实际 %s", plan.PlanStatus)
			}
			for _, input := range plan.Inputs {
				if input.Key != input.Label {
					t.Fatalf("输入键应等于名称：%+v", input)
				}
			}
			for _, step := range plan.Steps {
				if step.Enrichment != StepEnrichmentPending || len(step.SuggestedTools) != 0 {
					t.Fatalf("基础计划不应包含模型结果：%+v", step)
				}
			}
			if hasCode(plan, "FLOW_COMPILE_LLM_FAILED") {
				t.Fatal("基础计划不应把未增强记为失败")
			}
		})
	}
}

const explicitInputFlow = "---\n名称: 输入类型\n编号: test-flow\n版本: 1.0.0\n---\n## 需要准备\n" +
	"- 底图数据（必填，文件夹）：存放图层文件\n" +
	"- 区域图层（可选，图层）：工程中已加载的面图层\n" +
	"- 参考资料（可选）：一个文件\n" +
	"- 统计年份（可选，数字）\n" +
	"- 输出位置（可选，文件夹，不记住）\n" +
	"- 备注（可选）\n" +
	"## 步骤\n### 第1步 检查\n- 负责：数据工程专家\n- 做法：读取「底图数据」「区域图层」「参考资料」「统计年份」「输出位置」「备注」\n- 完成标准：「结果」（文本）\n"

func TestBasePlanExplicitInputTypesAndMemoryFlags(t *testing.T) {
	env := fixtureEnvironment()
	// 模型试图改写所有类型：只有未写类型、说明也没有关键词的"备注"允许被细化。
	env.Model = modelFunc(func(_ context.Context, r ModelRequest) (string, error) {
		if r.Kind == "flow" {
			return `{"inputs":[{"label":"底图数据","type":"text","options":[]},{"label":"区域图层","type":"text","options":[]},{"label":"参考资料","type":"text","options":[]},{"label":"统计年份","type":"text","options":[]},{"label":"输出位置","type":"text","options":[]},{"label":"备注","type":"layer","options":[]}],"input_constraints":[],"triggers":[]}`, nil
		}
		return `{"suggested_tools":[],"missing_capabilities":[],"expected_outputs":[],"condition":null,"notes":""}`, nil
	})
	compiler, _, _ := compilerFixture(t, explicitInputFlow, &env)
	want := map[string]string{"底图数据": "folder", "区域图层": "layer", "参考资料": "file", "统计年份": "number", "输出位置": "folder", "备注": "text"}
	base, err := compiler.BasePlan(context.Background(), "test-flow", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range base.Inputs {
		if input.Type != want[input.Label] {
			t.Fatalf("基础类型错误：%s=%s", input.Label, input.Type)
		}
	}
	if !hasCode(base, "FLOW_INPUT_TYPE_IMPLICIT") {
		t.Fatal("隐式文本输入应得到提示")
	}
	remember := map[string]bool{}
	for _, input := range base.Inputs {
		remember[input.Label] = input.Remember
	}
	if !remember["底图数据"] || !remember["参考资料"] || remember["输出位置"] || remember["区域图层"] {
		t.Fatalf("记住标志错误：%v", remember)
	}
	enriched, err := compiler.Compile(context.Background(), "test-flow", "", false)
	if err != nil {
		t.Fatal(err)
	}
	want["备注"] = "layer"
	for _, input := range enriched.Inputs {
		if input.Type != want[input.Label] || input.Key != input.Label {
			t.Fatalf("模型改写了不允许改写的类型或键：%+v", input)
		}
	}
}

func TestParseRejectsDuplicateInputNames(t *testing.T) {
	doc := Parse("---\n名称: 重名\n版本: 1.0.0\n---\n## 需要准备\n- 数据（必填，文件）\n- 数据（可选，文件夹）\n## 步骤\n### 第1步 检查\n- 负责：系统自动\n- 做法：读取「数据」\n- 完成标准：「结果」（文本）\n")
	found := false
	for _, d := range doc.Diagnostics {
		if d.Code == "FLOW_INPUT_DUPLICATE" && d.Level == "error" {
			found = true
		}
	}
	if !found {
		t.Fatalf("重名输入应报错：%+v", doc.Diagnostics)
	}
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		result[rel] = info.ModTime().Format(time.RFC3339Nano) + "|" + info.Mode().String()
		if !info.IsDir() {
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			result[rel] += "|" + string(raw)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPlanReadOnlyUsesRuntimeInputEnrichmentWithoutWrites(t *testing.T) {
	raw := "---\n名称: 输入校验\n编号: test-flow\n版本: 1.0.0\n---\n## 需要准备\n- 数量（可选）\n- 说明（可选）\n## 步骤\n### 第1步 检查\n- 负责：数据工程专家\n- 做法：读取「数量」「说明」\n- 完成标准：「结果」（文本）\n"
	env := fixtureEnvironment()
	calls := 0
	env.Model = modelFunc(func(_ context.Context, request ModelRequest) (string, error) {
		calls++
		if request.Kind == "flow" {
			return `{"inputs":[{"label":"数量","type":"number","options":[]},{"label":"说明","type":"text","options":[]}],"input_constraints":[{"op":"at_least_one","inputs":["数量","说明"]}],"triggers":[]}`, nil
		}
		return `{"suggested_tools":[],"missing_capabilities":[],"expected_outputs":[],"condition":null,"notes":""}`, nil
	})
	compiler, store, _ := compilerFixture(t, raw, &env)
	if _, err := compiler.Compile(context.Background(), "test-flow", "", false); err != nil {
		t.Fatal(err)
	}
	runtime, err := compiler.EnsureRunnable(context.Background(), "test-flow", "")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Inputs[0].Type != "number" || len(runtime.InputConstraints) != 1 {
		t.Fatalf("runtime lacks expected enrichment: %+v %+v", runtime.Inputs, runtime.InputConstraints)
	}
	item, err := store.Get("test-flow")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(item.Dir, "flow.lock")); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, filepath.Dir(store.UserDir))
	calls = 0
	checked, err := compiler.PlanReadOnly(context.Background(), "test-flow", "")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || !reflect.DeepEqual(runtime.Inputs, checked.Inputs) || !reflect.DeepEqual(runtime.InputConstraints, checked.InputConstraints) {
		t.Fatalf("read-only inputs must match execution without model calls: calls=%d inputs=%+v constraints=%+v", calls, checked.Inputs, checked.InputConstraints)
	}
	if after := snapshotTree(t, filepath.Dir(store.UserDir)); !reflect.DeepEqual(before, after) {
		t.Fatal("read-only plan changed lock, cache, or saved files")
	}

}
