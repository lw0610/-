// Package legacy contains declaration parsing used only while converting FLOW.md.
package legacy

import (
	diagnostics "github.com/autocarto/carto_agent/internal/shared/diagnostic"
	"regexp"
	"strings"
)

type Document struct {
	RoleSlots    []RoleSlotDoc
	Requirements []RequirementDoc
	Diagnostics  []diagnostics.Diagnostic
}
type Item struct{ Content string }
type Section struct {
	Title string
	Line  int
	Items []Item
}
type Executor struct{ Type, DisplayName string }

func diagnostic(level, code, step string, line int, message string) diagnostics.Diagnostic {
	return diagnostics.Diagnostic{Level: level, Code: code, Step: step, Line: line, Message: message}
}
func declarationNames(raw string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range strings.Split(raw, "、") {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
func splitField(raw string) (string, string, bool) {
	for i, r := range raw {
		if r == ':' || r == '：' {
			return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+len(string(r)):]), true
		}
	}
	return "", "", false
}

// RoleSlotDoc describes a requested role without binding it to a scheme.
type RoleSlotDoc struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Packages    []string `json:"packages"`
	Executor    string   `json:"executor,omitempty"`
	NeedsVision bool     `json:"needs_vision,omitempty"`
	SchemeOnly  bool     `json:"scheme_only,omitempty"`
	Line        int      `json:"line,omitempty"`
}

// RequirementDoc preserves author-supplied names and references for later binding.
type RequirementDoc struct {
	Name               string            `json:"name"`
	List               string            `json:"list,omitempty"`
	Category           string            `json:"category,omitempty"`
	Level              string            `json:"level,omitempty"`
	Extent             string            `json:"extent"`
	Scale              string            `json:"scale"`
	Params             map[string]string `json:"params"`
	Fields             map[string]string `json:"fields"`
	RecommendedSources []string          `json:"recommended_sources"`
	ExcludedSources    []string          `json:"excluded_sources"`
	Acquisition        string            `json:"acquisition"`
	Line               int               `json:"line,omitempty"`
}

func ParseRoles(doc *Document, sections []Section, executors []Executor) {
	for _, section := range sections {
		slot := RoleSlotDoc{Name: strings.TrimSpace(section.Title), Packages: []string{}, Line: section.Line}
		for _, item := range section.Items {
			label, value, ok := splitField(item.Content)
			if !ok {
				label = item.Content
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
			for _, executor := range executors {
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

var requirementScale = regexp.MustCompile(`^1\s*[:：]\s*[1-9][0-9]*$`)
var requirementAcquisition = regexp.MustCompile(`^「第\s*[1-9][0-9]*\s*步」$`)

func ParseRequirements(doc *Document, sections []Section) {
	for _, section := range sections {
		r := RequirementDoc{Name: strings.TrimSpace(section.Title), Scale: "自动", Acquisition: "系统", Params: map[string]string{}, Fields: map[string]string{}, RecommendedSources: []string{}, ExcludedSources: []string{}, Line: section.Line}
		invalid := func(message string) {
			doc.Diagnostics = append(doc.Diagnostics, diagnostic("error", "FLOW_REQUIREMENT_INVALID", "", r.Line, message))
		}
		seen := map[string]bool{}
		for _, item := range section.Items {
			label, value, ok := splitField(item.Content)
			if !ok {
				label = item.Content
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
