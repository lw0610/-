package flow

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseExamples(t *testing.T) {
	tests := []struct {
		name                     string
		steps, inputs, resources int
		role                     string
	}{
		{"marching-route", 11, 6, 6, "知识抽取专家"},
		{"statistical-choropleth", 4, 11, 1, "数据工程专家"},
		{"traffic-route", 4, 4, 2, "数据工程专家"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/" + test.name + ".md")
			if err != nil {
				t.Fatal(err)
			}
			doc := Parse(string(raw))
			if len(doc.Steps) != test.steps || len(doc.Inputs) != test.inputs || len(doc.Resources) != test.resources {
				t.Fatalf("结构数量错误：步骤 %d，输入 %d，资源 %d；诊断 %+v", len(doc.Steps), len(doc.Inputs), len(doc.Resources), doc.Diagnostics)
			}
			if doc.Meta.ID != test.name || doc.Meta.Version != "2.0.0" || doc.Steps[0].Role != test.role {
				t.Fatalf("头部或负责错误：%+v / %+v", doc.Meta, doc.Steps[0])
			}
			if len(doc.Diagnostics) != 0 {
				t.Fatalf("规范示例不应有解析诊断：%+v", doc.Diagnostics)
			}
			for i, step := range doc.Steps {
				if step.Index != i+1 || step.ID == "" || step.Title == "" || step.Instructions == "" || len(step.ExpectedOutputs) == 0 {
					t.Fatalf("步骤不完整：%+v", step)
				}
			}
			first, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 20; i++ {
				repeated, err := json.Marshal(Parse(string(raw)))
				if err != nil {
					t.Fatal(err)
				}
				if string(repeated) != string(first) {
					t.Fatal("同一文档重复解析结果不同")
				}
			}
			switch test.name {
			case "marching-route":
				if got := doc.Steps[0].Condition.Rule; got == nil || got.Op != "provided" || got.Input != "史料原文" {
					t.Fatalf("原文条件错误：%+v", got)
				}
				if got := doc.Steps[6].Condition.Rule; got == nil || got.Op != "truthy" {
					t.Fatalf("布尔条件错误：%+v", got)
				}
				if got := doc.Steps[10].Condition.Rule; got == nil || got.Op != "falsy" {
					t.Fatalf("否定条件错误：%+v", got)
				}
				assertOutput(t, doc.Steps[0], "路线数据集", "dataset", "route_dataset", true)
				assertOutput(t, doc.Steps[7], "初审结果", "review_initial", "", true)
				assertOutput(t, doc.Steps[9], "终审结果", "review_final", "", true)
				assertOutput(t, doc.Steps[8], "终稿 PDF", "file", "pdf", true)
				if !contains(doc.Steps[3].Refs.Inputs, "路线数据集") || !containsOutputRef(doc.Steps[3].Refs.Outputs, "s1", "路线数据集") {
					t.Fatalf("同名输入与产出应同时匹配：%+v", doc.Steps[3].Refs)
				}
				if doc.Steps[0].OnFailure != "stop" || doc.Steps[3].OnFailure != "ask" || doc.Steps[1].OnFailure != "retry_then_ask" {
					t.Fatal("失败处理映射错误")
				}
				if !reflect.DeepEqual(doc.Inputs[2].Options, []string{"A0", "A1", "A2", "A3", "A4"}) || !doc.Inputs[3].Boolean || doc.Inputs[3].Default != "否" {
					t.Fatalf("输入属性错误：%+v", doc.Inputs)
				}
			case "statistical-choropleth":
				assertOutput(t, doc.Steps[0], "灰底图层", "layers", "", false)
				assertOutput(t, doc.Steps[0], "数据质量报告", "file", "", false)
			case "traffic-route":
				if doc.Steps[1].Role != "空间分析专家" {
					t.Fatal("空间分析角色丢失")
				}
				assertOutput(t, doc.Steps[3], "版面", "layout", "", true)
				assertOutput(t, doc.Steps[3], "成果 PDF", "file", "pdf", true)
			}
		})
	}
}

func TestParseAliasesAndMergedSections(t *testing.T) {
	raw := `---
 Name: 同义词测试
 ID: aliases
 DESCRIPTION: 头部英文键
 AUTHOR: 作者
---
## 什么时候使用
第一段
## 适用范围
第二段
## 需要用户提供
- 纸张(必填;默认 A1;可选值:A0、A1;自定义属性): 纸张说明
- 审查（是/否，默认否）
- 史料（与「数据」二选一）：原文
## 引用资源
- 样式「单样式」: resources/single.QML
- 参考「说明」：resources/docs/说明.md
## 操作步骤
### 没有序号的标题
- **执行者**: 测试角色
- __仅当__：「纸张」为 A1 时执行
- **具体做法**：使用「纸张」和「单样式」。
  - 子列表内容

紧随其后的段落继续属于做法。
- **验收**: 得到「成果」(文件,PDF)
- 出错时: 跳过本步
## 流程步骤
### 步骤8：确认
- 角色：用户
- 内容：确认「成果」。
- 完成条件：「用户确认」（确认结果）
## 约束
- 第一条
## 禁止事项
- 第二条
## 交付成果
最终成果
## 其他业务说明
保留 **原始 Markdown**。
`
	doc := Parse(raw)
	if doc.Meta.Name != "同义词测试" || doc.Meta.ID != "aliases" || doc.Meta.Version != "1.0.0" || doc.Meta.Description != "头部英文键" || doc.Meta.Author != "作者" {
		t.Fatalf("头部映射错误：%+v", doc.Meta)
	}
	if len(doc.Steps) != 2 || doc.Steps[0].Title != "没有序号的标题" || doc.Steps[1].Title != "确认" || doc.Steps[1].ID != "s2" {
		t.Fatalf("标题或合并错误：%+v", doc.Steps)
	}
	if doc.Steps[0].Role != "测试角色" || doc.Steps[0].OnFailure != "skip" || !strings.Contains(doc.Steps[0].Instructions, "紧随其后的段落") || !strings.Contains(doc.Steps[0].Instructions, "- 子列表内容") {
		t.Fatalf("字段提取错误：%+v", doc.Steps[0])
	}
	if doc.Inputs[0].Default != "A1" || !doc.Inputs[0].Required || !reflect.DeepEqual(doc.Inputs[0].UnknownAttributes, []string{"自定义属性"}) || !reflect.DeepEqual(doc.Inputs[2].Alternatives, []string{"数据"}) {
		t.Fatalf("输入属性错误：%+v", doc.Inputs)
	}
	if doc.Resources[0].Type != "style" || doc.Resources[1].Type != "doc" {
		t.Fatalf("资源映射错误：%+v", doc.Resources)
	}
	assertOutput(t, doc.Steps[0], "成果", "file", "pdf", true)
	assertOutput(t, doc.Steps[1], "用户确认", "confirmation", "", true)
	if !strings.Contains(doc.Applicability, "第一段\n\n第二段") || len(doc.Rules) != 2 || doc.Deliverables != "最终成果" || len(doc.Notes) != 1 || doc.Notes[0].Content != "保留 **原始 Markdown**。" {
		t.Fatalf("分节内容丢失：%+v", doc)
	}
	if countDiagnostic(doc, "FLOW_SECTION_DUPLICATE") != 3 || countDiagnostic(doc, "FLOW_SECTION_UNKNOWN") != 1 {
		t.Fatalf("分节提示错误：%+v", doc.Diagnostics)
	}
	for _, d := range doc.Diagnostics {
		if d.Level == "error" {
			t.Fatalf("不应出现错误：%+v", d)
		}
	}
}

func TestParseASTKeepsCodeTablesAndNestedFields(t *testing.T) {
	raw := "---\n名称: AST 测试\n---\n## 步骤\n### 第9步 保留正文\n做法之前的自由段落。\n\n- 负责：真实角色\n- 做法：第一行\n  - 子项\n    - 负责：这不是字段\n\n  ```markdown\n  ## 步骤\n  ### 第2步 伪步骤\n  - 负责：伪角色\n  - 完成标准：伪字段\n  ```\n\n  | 名称 | 值 |\n  | --- | --- |\n  | A | B |\n\n紧跟段落。\n- 完成标准：「正文」（文本）\n\n## 交付\n正文\n"
	doc := Parse(raw)
	if len(doc.Steps) != 1 || doc.Steps[0].Role != "真实角色" {
		t.Fatalf("代码或嵌套列表错误地改变了结构：%+v", doc.Steps)
	}
	step := doc.Steps[0]
	for _, fragment := range []string{"做法之前的自由段落。", "- 负责：这不是字段", "```markdown\n## 步骤\n### 第2步 伪步骤\n- 负责：伪角色\n- 完成标准：伪字段\n```", "| 名称 | 值 |\n| --- | --- |\n| A | B |", "紧跟段落。"} {
		if !strings.Contains(step.Instructions, fragment) {
			t.Fatalf("正文没有保留 %q：\n%s", fragment, step.Instructions)
		}
	}
	if step.Line != 5 {
		t.Fatalf("步骤源行错误：%d", step.Line)
	}
	if len(doc.Diagnostics) != 0 {
		t.Fatalf("出现多余诊断：%+v", doc.Diagnostics)
	}
}

func TestParseMissingFieldsAndMalformedMetadata(t *testing.T) {
	tests := []struct {
		name, raw string
		codes     []string
	}{
		{"空文档", "", []string{"FLOW_NO_NAME", "FLOW_NO_STEPS"}},
		{"头部未结束", "---\n名称: 测试\n## 步骤\n### 标题\n- 做法：执行\n", []string{"FLOW_PARSE_FAILED", "FLOW_NO_NAME", "FLOW_STEP_NO_ROLE", "FLOW_STEP_NO_COMPLETION"}},
		{"头部语法错误", "---\n名称: [\n---\n## 步骤\n### 标题\n- 做法：执行\n", []string{"FLOW_PARSE_FAILED", "FLOW_NO_NAME"}},
		{"头部不是字典", "---\n- 名称\n---\n", []string{"FLOW_PARSE_FAILED", "FLOW_NO_NAME", "FLOW_NO_STEPS"}},
		{"头部重复别名", "---\n名称: 测试\nname: 重复\n---\n", []string{"FLOW_PARSE_FAILED", "FLOW_NO_STEPS"}},
		{"头部嵌套值", "---\n名称: [测试]\n---\n", []string{"FLOW_PARSE_FAILED", "FLOW_NO_NAME"}},
		{"缺步骤字段", "---\n名称: 测试\n---\n## 步骤\n### 第1步 缺字段\n- 负责：\n- 做法：\n- 完成标准：\n", []string{"FLOW_STEP_NO_INSTRUCTIONS", "FLOW_STEP_NO_ROLE", "FLOW_STEP_NO_COMPLETION"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc := Parse(test.raw)
			for _, code := range test.codes {
				if countDiagnostic(doc, code) == 0 {
					t.Fatalf("缺少 %s：%+v", code, doc.Diagnostics)
				}
			}
		})
	}
	meta, diagnostics := ParseMetadata("---\n名称: 只有头部\n---\n")
	if meta.Name != "只有头部" || meta.Version != "1.0.0" || len(diagnostics) != 0 {
		t.Fatalf("只读元数据不应校验正文：%+v %+v", meta, diagnostics)
	}
}

func TestParseReferencesAndDuplicateOutputs(t *testing.T) {
	raw := `---
名称: 引用测试
---
## 输入
- 同名（可选）
## 资源
- 文档「资料」：公共资源库
## 步骤
### 1. 首步
- 负责：角色
- 做法：使用「后序」和「不存在」及「不存在」。
- 完成标准：「同名」（文本）
### 2、次步
- 负责：角色
- 做法：使用「同名」「资料」「同名」。
- 完成标准：「后序」（文本）
### 第3步 重复
- 负责：角色
- 做法：读取「同名」。
- 完成标准：「同名」（文本）
`
	doc := Parse(raw)
	if countDiagnostic(doc, "FLOW_REF_FORWARD") != 1 || countDiagnostic(doc, "FLOW_REF_UNRESOLVED") != 1 || countDiagnostic(doc, "FLOW_OUTPUT_DUPLICATE") != 1 {
		t.Fatalf("引用诊断错误：%+v", doc.Diagnostics)
	}
	if !reflect.DeepEqual(doc.Steps[0].Refs.Unresolved, []string{"后序", "不存在"}) {
		t.Fatalf("未解析引用应稳定去重：%+v", doc.Steps[0].Refs)
	}
	if !reflect.DeepEqual(doc.Steps[1].Refs.Inputs, []string{"同名"}) || !reflect.DeepEqual(doc.Steps[1].Refs.Resources, []string{"资料"}) || !reflect.DeepEqual(doc.Steps[1].Refs.Outputs, []OutputRef{{Step: "s1", Label: "同名"}}) {
		t.Fatalf("引用匹配错误：%+v", doc.Steps[1].Refs)
	}
	for _, step := range doc.Steps {
		for _, unresolved := range step.Refs.Unresolved {
			if unresolved == "同名" {
				t.Fatal("产出声明自身不能成为未解析引用")
			}
		}
	}
}

func TestParseAllOutputAndResourceTypes(t *testing.T) {
	raw := `---
名称: 类型测试
---
## 资源
- 数据清单「清单」：resources/data.csv
- 数据文件夹「文件夹」：/数据
- 样式「样式」：resources/a.qml
- 样式集「样式集」：resources/styles
- 路线样式「路线」：公共资源库
- 版式「版式」：公共资源库
- 符号「符号」：公共资源库
- 参考「参考」：公共资源库
## 步骤
### 类型
- 负责：角色
- 做法：执行。
- 完成标准：
  - 「图层」（图层）
  - 「路线数据」（数据集，路线格式）
  - 「统计数据」（数据集，统计格式）
  - 「文件」（文件，.PNG）
  - 「版面」（版面）
  - 「文本」（文本）
  - 「初审」（审查结果（初审））
  - 「终审」（审查结果，终审）
  - 「确认」（确认结果）
  - 「其他」（其他，可选）
`
	doc := Parse(raw)
	if len(doc.Diagnostics) != 0 {
		t.Fatalf("类型应正常解析：%+v", doc.Diagnostics)
	}
	for index, typeName := range []string{"data_list", "data_folder", "style", "style_set", "route_style", "layout", "symbols", "doc"} {
		if doc.Resources[index].Type != typeName {
			t.Fatalf("资源 %d 类型错误：%+v", index, doc.Resources[index])
		}
	}
	for index, typeName := range []string{"layers", "dataset", "dataset", "file", "layout", "text", "review_initial", "review_final", "confirmation", "any"} {
		if doc.Steps[0].ExpectedOutputs[index].Type != typeName {
			t.Fatalf("产出 %d 类型错误：%+v", index, doc.Steps[0].ExpectedOutputs[index])
		}
	}
	assertOutput(t, doc.Steps[0], "统计数据", "dataset", "statistical_dataset", true)
	assertOutput(t, doc.Steps[0], "文件", "file", "png", true)
	assertOutput(t, doc.Steps[0], "其他", "any", "", false)
}

func TestParseConditions(t *testing.T) {
	tests := []struct {
		raw  string
		want *ConditionRule
	}{
		{"提供了「史料原文」时执行", &ConditionRule{Op: "provided", Input: "史料原文"}},
		{"提供「统计表」时执行。", &ConditionRule{Op: "provided", Input: "统计表"}},
		{"未提供「史料原文」时执行", &ConditionRule{Op: "not_provided", Input: "史料原文"}},
		{"「质量审查」为是时执行", &ConditionRule{Op: "truthy", Input: "质量审查"}},
		{"「质量审查」为否时执行", &ConditionRule{Op: "falsy", Input: "质量审查"}},
		{"「纸张」为 A1 时执行", &ConditionRule{Op: "equals", Input: "纸张", Value: "A1"}},
		{"第3步完成时执行", &ConditionRule{Op: "step_status", Step: "s3", Status: "completed"}},
		{"第03步完成时执行", &ConditionRule{Op: "step_status", Step: "s3", Status: "completed"}},
		{"第0步完成时执行", nil},
		{"判断质量合格时再执行", nil},
		{"提供了「史料原文」时执行，否则跳过", nil},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			if got := ParseCondition(test.raw); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("得到 %+v，需要 %+v", got, test.want)
			}
		})
	}
	doc := Parse("---\n名称: 条件\n---\n## 步骤\n### 标题\n- 负责：角色\n- 条件：人工判断合适时执行\n- 做法：执行\n- 完成标准：「结果」（其他）\n")
	if doc.Steps[0].Condition.Text != "人工判断合适时执行" || doc.Steps[0].Condition.Rule != nil || countDiagnostic(doc, "FLOW_CONDITION_UNPARSED") != 1 {
		t.Fatalf("非标准条件应保留原文：%+v", doc)
	}
}

func TestParseLineNumbersAndNewlines(t *testing.T) {
	raw := "\ufeff---\r\n名称: 行号\r\n---\r\n\r\n## 输入\r\n\r\n- 名称（必填）\r\n\r\n## 资源\r\n- 文档「说明」：公共资源库\r\n\r\n## 步骤\r\n\r\n### 第7步 标题\r\n- 做法：使用「名称」和「说明」。\r\n"
	doc := Parse(raw)
	if doc.Inputs[0].Line != 7 || doc.Resources[0].Line != 10 || doc.Steps[0].Line != 14 {
		t.Fatalf("源行号错误：输入 %d，资源 %d，步骤 %d", doc.Inputs[0].Line, doc.Resources[0].Line, doc.Steps[0].Line)
	}
	for _, d := range doc.Diagnostics {
		if d.Step == "s1" && d.Line != 14 {
			t.Fatalf("诊断行号错误：%+v", d)
		}
	}
}

func TestParseMalformedFieldMarkup(t *testing.T) {
	for _, marker := range []string{"**", "__", "****", "____"} {
		raw := "---\n名称: 非完整标记\n---\n## 步骤\n### 标题\n- " + marker + "：正文\n"
		doc := Parse(raw)
		if len(doc.Steps) != 1 || doc.Steps[0].Instructions == "" {
			t.Fatalf("不完整加粗标记应按普通正文保留：%+v", doc)
		}
	}
}

func TestParseEmptyAndSetextHeadings(t *testing.T) {
	for _, raw := range []string{"##\n##", "## \n## ###\n", "###\n###"} {
		doc := Parse(raw)
		if doc == nil || countDiagnostic(doc, "FLOW_NO_NAME") != 1 {
			t.Fatalf("空标题仍应返回诊断：%+v", doc)
		}
	}
	raw := "---\n名称: 标题边界\n---\n步骤\n----\n### 第1步 标题\n- 负责：角色\n- 做法：正文\n- 完成标准：「结果」（文本）\n##\n### 不属于步骤\n- 做法：不能误认为第二步\n"
	doc := Parse(raw)
	if len(doc.Steps) != 1 || len(doc.Notes) != 1 || doc.Notes[0].Title != "" || countDiagnostic(doc, "FLOW_SECTION_UNKNOWN") != 1 {
		t.Fatalf("空二级标题应终止步骤分节，Setext 标题应识别：%+v", doc)
	}
	if strings.Contains(doc.Steps[0].Completion, "不属于步骤") {
		t.Fatal("二级标题之后的内容不能并入上一步")
	}
}

func FuzzParseDoesNotPanic(f *testing.F) {
	for _, raw := range []string{
		"", "---\n名称: 测试\n---\n## 步骤\n### 标题\n- 做法：正文\n",
		"---\n名称: [\n---\n## 步骤\n### 标题\n- **：正文\n",
		"## 步骤\n### 标题\n- 做法：\n  ```\n  ## 步骤\n  - 负责：角色\n  ```\n",
	} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		first, err := json.Marshal(Parse(raw))
		if err != nil {
			t.Fatal(err)
		}
		second, err := json.Marshal(Parse(raw))
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Fatal("相同输入解析结果不同")
		}
	})
}

func assertOutput(t *testing.T, step StepDoc, label, typeName, format string, required bool) {
	t.Helper()
	for _, output := range step.ExpectedOutputs {
		if output.Label == label {
			if output.Type != typeName || output.Format != format || output.Required != required {
				t.Fatalf("产出 %s 错误：%+v", label, output)
			}
			return
		}
	}
	t.Fatalf("步骤 %s 缺少产出 %s：%+v", step.ID, label, step.ExpectedOutputs)
}

func countDiagnostic(doc *FlowDoc, code string) int {
	count := 0
	for _, diagnostic := range doc.Diagnostics {
		if diagnostic.Code == code {
			count++
		}
	}
	return count
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
func containsOutputRef(values []OutputRef, step, label string) bool {
	for _, item := range values {
		if item.Step == step && item.Label == label {
			return true
		}
	}
	return false
}
