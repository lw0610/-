package flow

import (
	"reflect"
	"testing"
)

func TestParseAllMissingCondition(t *testing.T) {
	for _, tc := range []struct {
		text   string
		inputs []string
	}{
		{"未提供「统计表」且未提供「已有专题图层」时执行", []string{"统计表", "已有专题图层"}},
		{" 未提供 「统计表」 且 未提供 「已有专题图层」 时执行。 ", []string{"统计表", "已有专题图层"}},
		{"未提供「统计表」且未提供「已有专题图层」且未提供「缓存数据」时执行", []string{"统计表", "已有专题图层", "缓存数据"}},
		{"未提供「数量且质量」且未提供「备用表」时执行", []string{"数量且质量", "备用表"}},
	} {
		t.Run(tc.text, func(t *testing.T) {
			want := &ConditionRule{Op: "and"}
			for _, input := range tc.inputs {
				want.Args = append(want.Args, ConditionRule{Op: "not_provided", Input: input})
			}
			if got := ParseCondition(tc.text); !reflect.DeepEqual(got, want) {
				t.Fatalf("ParseCondition(%q) = %+v, want %+v", tc.text, got, want)
			}
		})
	}

	for _, raw := range []string{
		"未提供「统计表」或未提供「已有专题图层」时执行",
		"未提供「统计表」且提供「已有专题图层」时执行",
		"未提供「统计表」且未提供「已有专题图层」时执行，否则跳过",
		"未提供「统计表」且未提供「 」时执行",
	} {
		if got := ParseCondition(raw); got != nil {
			t.Errorf("ambiguous or unsupported condition %q was silently parsed as %+v", raw, got)
		}
	}
}

func TestAllMissingConditionAndStepCapabilitiesInDocument(t *testing.T) {
	doc := Parse(`---
名称: 条件取数
---
## 需要准备
- 统计表（可选，文件）
- 已有专题图层（可选，图层）
## 步骤
### 第1步 在线获取
- 负责：数据工程专家
- 条件：未提供「统计表」且未提供「已有专题图层」时执行
- 需要能力：网页与浏览器取数
- 做法：获取缺失的统计表。
- 完成标准：「联网统计表」（文件，CSV）
`)
	if HasErrors(doc.Diagnostics) || countDiagnostic(doc, "FLOW_CONDITION_UNPARSED") != 0 {
		t.Fatalf("deterministic condition should not need model interpretation: %+v", doc.Diagnostics)
	}
	step := doc.Steps[0]
	if step.Condition == nil || step.Condition.Rule == nil || step.Condition.Rule.Op != "and" || len(step.Condition.Rule.Args) != 2 {
		t.Fatalf("missing deterministic condition: %+v", step.Condition)
	}
	if !reflect.DeepEqual(step.Capabilities, []string{"网页与浏览器取数"}) || len(step.OptionalCapabilities) != 0 {
		t.Fatalf("step-only capability declaration lost: %+v", step)
	}
	assertOutput(t, step, "联网统计表", "file", "csv", true)
}
