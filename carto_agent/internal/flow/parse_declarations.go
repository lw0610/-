package flow

import (
	"regexp"
	"strings"
)

func declarationNames(raw string) []string {
	values := []string{}
	for _, value := range strings.Split(raw, "、") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return uniqueStrings(values)
}

func parseStepCapabilities(raw string) ([]string, []string) {
	required, optional := []string{}, []string{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == '；' || r == '\n' }) {
		label, value, ok := splitField(part)
		if ok && label == "可选" {
			optional = append(optional, declarationNames(value)...)
		} else {
			required = append(required, declarationNames(part)...)
		}
	}
	return uniqueStrings(required), uniqueStrings(optional)
}

func parseRoleSlots(doc *FlowDoc, raw string, contentLine int) {
	for _, section := range headings(raw, 3) {
		slot := RoleSlotDoc{Name: strings.TrimSpace(section.title), Packages: []string{}, Line: contentLine + section.line - 1}
		for _, item := range listItems(section.content) {
			label, value, ok := splitField(item.content)
			if !ok {
				label = item.content
			}
			switch label {
			case "职责", "说明", "描述":
				slot.Description = value
			case "工具包", "能力":
				slot.Packages = declarationNames(value)
			case "执行器":
				slot.Executor = value
			case "模型":
				if value == "读图" {
					slot.NeedsVision = true
				} else {
					doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_ROLE_SLOT_INVALID", "", slot.Line, "角色模型要求只支持“读图”。"))
				}
			case "只用方案角色":
				if value == "是" {
					slot.SchemeOnly = true
				} else if value != "否" {
					doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_ROLE_SLOT_INVALID", "", slot.Line, "只用方案角色应填写“是”或“否”。"))
				}
			default:
				doc.Diagnostics = append(doc.Diagnostics, diagnostic("warning", "FLOW_ROLE_SLOT_FIELD_UNKNOWN", "", slot.Line, "参与角色“"+slot.Name+"”包含未知字段："+label))
			}
		}
		if slot.Executor != "" {
			found := false
			for _, executor := range ExecutorSpecs() {
				if executor.Type == slot.Executor || executor.DisplayName == slot.Executor {
					found = true
					break
				}
			}
			if !found || len(slot.Packages) > 0 {
				doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_ROLE_SLOT_INVALID", "", slot.Line, "执行器必须已注册，且不能同时声明工具包："+slot.Name))
			}
		}
		for _, existing := range doc.RoleSlots {
			if existing.Name == slot.Name {
				doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_ROLE_SLOT_DUPLICATE", "", slot.Line, "参与角色名称重复："+slot.Name))
				break
			}
		}
		doc.RoleSlots = append(doc.RoleSlots, slot)
	}
}

func validateRoleSlotUses(doc *FlowDoc, declared bool) {
	if !declared {
		return
	}
	used := map[string]bool{}
	slots := map[string]bool{}
	for _, slot := range doc.RoleSlots {
		slots[slot.Name] = true
	}
	for _, step := range doc.Steps {
		if slots[step.Role] {
			used[step.Role] = true
			continue
		}
		switch step.Role {
		case "", "系统", "系统自动", "auto", "用户", "人工":
			continue
		}
		doc.Diagnostics = append(doc.Diagnostics, diagnostic("warning", "FLOW_ROLE_SLOT_UNDECLARED", step.ID, step.Line, "负责角色未在参与角色中声明，将按方案名称匹配："+step.Role))
	}
	for _, slot := range doc.RoleSlots {
		if !used[slot.Name] {
			doc.Diagnostics = append(doc.Diagnostics, diagnostic("info", "FLOW_ROLE_SLOT_UNUSED", "", slot.Line, "参与角色没有被步骤使用："+slot.Name))
		}
	}
}

var requirementScale = regexp.MustCompile(`^1\s*[:：]\s*[1-9][0-9]*$`)
var requirementAcquisition = regexp.MustCompile(`^「第\s*[1-9][0-9]*\s*步」$`)

func parseRequirements(doc *FlowDoc, raw string, contentLine int) {
	for _, section := range headings(raw, 3) {
		r := RequirementDoc{Name: strings.TrimSpace(section.title), Scale: "自动", Acquisition: "系统", Params: map[string]string{}, Fields: map[string]string{}, RecommendedSources: []string{}, ExcludedSources: []string{}, Line: contentLine + section.line - 1}
		invalid := func(message string) {
			doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_REQUIREMENT_INVALID", "", r.Line, message))
		}
		seen := map[string]bool{}
		for _, item := range listItems(section.content) {
			label, value, ok := splitField(item.content)
			if !ok {
				label = item.content
			}
			if seen[label] {
				invalid("数据需求字段重复：" + label)
			}
			seen[label] = true
			switch label {
			case "清单":
				r.List = value
			case "类别":
				r.Category = value
			case "层级":
				r.Level = value
			case "范围":
				r.Extent = value
			case "比例尺":
				r.Scale = value
			case "参数", "字段":
				values := r.Params
				if label == "字段" {
					values = r.Fields
				}
				for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == '；' }) {
					key, val, has := strings.Cut(strings.TrimSpace(part), "=")
					key = strings.TrimSpace(key)
					val = strings.TrimSpace(val)
					if !has && label == "字段" {
						val = key
						has = true
					}
					if !has || key == "" || val == "" {
						invalid(label + "须填写键=值，多个值用分号分隔。")
						continue
					}
					if _, exists := values[key]; exists {
						invalid(label + "名称重复：" + key)
						continue
					}
					values[key] = val
				}
			case "推荐来源":
				r.RecommendedSources = declarationNames(value)
			case "排除来源":
				r.ExcludedSources = declarationNames(value)
			case "获取方式":
				r.Acquisition = value
			default:
				doc.Diagnostics = append(doc.Diagnostics, diagnostic("warning", "FLOW_REQUIREMENT_FIELD_UNKNOWN", "", r.Line, "数据需求“"+r.Name+"”包含未知字段："+label))
			}
		}
		if (r.List == "") == (r.Category == "") {
			invalid("数据需求须在清单和类别中选择一项：" + r.Name)
		}
		if r.Extent == "" {
			invalid("数据需求必须声明范围：" + r.Name)
		}
		if r.Scale != "自动" && !requirementScale.MatchString(r.Scale) {
			invalid("比例尺应填写“自动”或“1:N”。")
		}
		if r.Acquisition != "系统" && !requirementAcquisition.MatchString(r.Acquisition) {
			invalid("获取方式应填写“系统”或“「第N步」”。")
		}
		for _, existing := range doc.Requirements {
			if existing.Name == r.Name {
				doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_REQUIREMENT_DUPLICATE", "", r.Line, "数据需求名称重复："+r.Name))
				break
			}
		}
		doc.Requirements = append(doc.Requirements, r)
	}
}
