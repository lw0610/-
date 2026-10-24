package flow

import (
	"context"
	"strings"
	"testing"
)

const threeStepFlow = testFlow + "\n### 第2步 汇总\n- 负责：数据工程专家\n- 做法：汇总「结果」\n- 完成标准：「汇总」（文本）\n" +
	"\n### 第3步 出图\n- 负责：制图专家\n- 做法：按「汇总」排版出图\n- 完成标准：「成图」（文本）\n"

// stepCountingModel 对流程级与步骤级都返回合法 JSON，并按步骤编号计数；failStep 的调用返回超时。
func stepCountingModel(calls map[string]int, failStep string) JSONModel {
	return modelFunc(func(_ context.Context, r ModelRequest) (string, error) {
		if r.Kind == "flow" {
			calls["flow"]++
			return `{"inputs":[{"label":"原文","type":"conversation_text","options":[]},{"label":"文件夹","type":"folder","options":[]},{"label":"开关","type":"boolean","options":[]},{"label":"纸张","type":"choice","options":["A3","A4"]}],"input_constraints":[],"triggers":["测试"]}`, nil
		}
		calls[r.Step]++
		if r.Step == failStep {
			return "", context.DeadlineExceeded
		}
		return `{"suggested_tools":[],"missing_capabilities":[],"expected_outputs":[],"condition":null,"notes":"已完善"}`, nil
	})
}

func total(calls map[string]int) int {
	sum := 0
	for _, count := range calls {
		sum += count
	}
	return sum
}

func TestEnrichmentReusesUnchangedStepsAndRedoesOnModelChange(t *testing.T) {
	env := fixtureEnvironment()
	calls := map[string]int{}
	env.Model = stepCountingModel(calls, "")
	compiler, store, _ := compilerFixture(t, threeStepFlow, &env)
	ctx := context.Background()
	plan, err := compiler.Compile(ctx, "test-flow", "", false)
	if err != nil || plan.PlanStatus != PlanStatusReady || plan.Enrichment.Done != 3 || plan.Enrichment.Total != 3 || total(calls) != 4 {
		t.Fatalf("首次增强：%v calls=%v %+v", err, calls, plan.Enrichment)
	}
	expect := func(stage string, want map[string]int) {
		t.Helper()
		for key := range calls {
			delete(calls, key)
		}
		plan, err := compiler.Compile(ctx, "test-flow", "", false)
		if err != nil || plan.PlanStatus != PlanStatusReady {
			t.Fatalf("%s：%v %+v", stage, err, plan)
		}
		if len(calls) != len(want) {
			t.Fatalf("%s：调用 %v，期望 %v", stage, calls, want)
		}
		for key, count := range want {
			if calls[key] != count {
				t.Fatalf("%s：调用 %v，期望 %v", stage, calls, want)
			}
		}
	}
	if _, err = store.Save("test-flow", strings.Replace(threeStepFlow, "按「汇总」排版出图", "按「汇总」排版并导出", 1)); err != nil {
		t.Fatal(err)
	}
	expect("只改第 3 步做法", map[string]int{"s3": 1})
	env.Roles[2].Tools = env.Roles[2].Tools[1:]
	expect("修改第 3 步负责角色的工具", map[string]int{"s3": 1})
	env.ModelName = "另一个模型"
	expect("换编译模型", map[string]int{"flow": 1, "s1": 1, "s2": 1, "s3": 1})
	expect("没有变化", map[string]int{})
	for key := range calls {
		delete(calls, key)
	}
	if plan, err = compiler.Plan(ctx, "test-flow", ""); err != nil || plan.PlanStatus != PlanStatusReady || len(calls) != 0 {
		t.Fatalf("执行计划：%v calls=%v %+v", err, calls, plan)
	}
}

func TestEnrichmentTimeoutOnlyAffectsThatStep(t *testing.T) {
	env := fixtureEnvironment()
	calls := map[string]int{}
	env.Model = stepCountingModel(calls, "s2")
	compiler, _, _ := compilerFixture(t, threeStepFlow, &env)
	plan, err := compiler.Compile(context.Background(), "test-flow", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PlanStatus != PlanStatusPartial || plan.Enrichment.Done != 2 || calls["s2"] != 2 {
		t.Fatalf("超时步骤状态错误：calls=%v %+v", calls, plan.Enrichment)
	}
	for _, step := range plan.Steps {
		want := StepEnrichmentReady
		if step.ID == "s2" {
			want = StepEnrichmentFailed
		}
		if step.Enrichment != want {
			t.Fatalf("%s 增强状态 %s，期望 %s", step.ID, step.Enrichment, want)
		}
	}
	if len(plan.Enrichment.Failed) != 1 || plan.Enrichment.Failed[0].StepID != "s2" || plan.Enrichment.Failed[0].Reason != reasonTimeout {
		t.Fatalf("失败原因：%+v", plan.Enrichment.Failed)
	}
	failures := 0
	for _, d := range plan.Diagnostics {
		if d.Code == "FLOW_COMPILE_LLM_FAILED" {
			failures++
			if d.Step != "s2" || d.Level != "warning" {
				t.Fatalf("失败诊断：%+v", d)
			}
		}
	}
	if failures != 1 || plan.HasErrors() {
		t.Fatalf("超时步骤不应阻止运行：%+v", plan.Diagnostics)
	}
	if _, err = compiler.EnsureRunnable(context.Background(), "test-flow", ""); err != nil {
		t.Fatalf("部分未完善的计划应可运行：%v", err)
	}
}
