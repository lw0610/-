package flow

import "testing"

func TestV4ParseRoleAndRequirementDeclarations(t *testing.T) {
	raw := `---
名称: 测试
---
## 角色分工
### 工程师
- 职责：处理数据
- 工具包：遥感影像（GEE）、data-loading
- 模型：读图
- 只用方案角色：是
### 抽取
- 执行器：路线知识抽取
## 数据来源
### 边界
- 类别：行政区划面
- 范围：「区域」外扩 20%
- 参数：年份=「年份」；层级=省
- 字段：NAME=name；CODE
- 推荐来源：来源甲、来源乙
## 步骤
### 处理
- 负责：工程师
- 能力：格式转换；可选：空间分析
- 做法：处理
- 完成标准：完成
`
	doc := Parse(raw)
	if HasErrors(doc.Diagnostics) {
		t.Fatalf("%+v", doc.Diagnostics)
	}
	if len(doc.RoleSlots) != 2 || doc.RoleSlots[0].Packages[0] != "遥感影像（GEE）" || !doc.RoleSlots[0].NeedsVision || !doc.RoleSlots[0].SchemeOnly {
		t.Fatalf("%+v", doc.RoleSlots)
	}
	r := doc.Requirements[0]
	if r.Scale != "自动" || r.Acquisition != "系统" || r.Fields["NAME"] != "name" || r.Fields["CODE"] != "CODE" || r.Params["年份"] != "「年份」" {
		t.Fatalf("%+v", r)
	}
	if len(doc.Steps[0].Capabilities) != 1 || doc.Steps[0].OptionalCapabilities[0] != "空间分析" {
		t.Fatalf("%+v", doc.Steps[0])
	}
}
func TestV4ParseDeclarationDiagnostics(t *testing.T) {
	cases := []struct{ name, section, code string }{
		{"duplicate role", "## 参与角色\n### A\n### A", "FLOW_ROLE_SLOT_DUPLICATE"},
		{"unknown executor", "## 参与角色\n### A\n- 执行器：不存在", "FLOW_ROLE_SLOT_INVALID"},
		{"conflicting executor", "## 参与角色\n### A\n- 执行器：route_knowledge\n- 工具包：数据加载", "FLOW_ROLE_SLOT_INVALID"},
		{"unknown field", "## 参与角色\n### A\n- 不认识：内容", "FLOW_ROLE_SLOT_FIELD_UNKNOWN"},
		{"invalid requirement", "## 数据获取\n### A\n- 类别：面\n- 清单：清单", "FLOW_REQUIREMENT_INVALID"},
		{"duplicate requirement", "## 数据需求\n### A\n- 类别：面\n- 范围：全国\n### A\n- 类别：面\n- 范围：全国", "FLOW_REQUIREMENT_DUPLICATE"},
		{"unknown requirement field", "## 数据需求\n### A\n- 类别：面\n- 范围：全国\n- 未知：值", "FLOW_REQUIREMENT_FIELD_UNKNOWN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Parse("---\n名称: 测试\n---\n" + c.section + "\n## 步骤\n### 执行\n- 负责：A\n- 做法：完成\n- 完成标准：完成\n")
			for _, v := range d.Diagnostics {
				if v.Code == c.code {
					return
				}
			}
			t.Fatalf("missing %s: %+v", c.code, d.Diagnostics)
		})
	}
}
