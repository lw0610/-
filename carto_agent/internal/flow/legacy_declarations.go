package flow

import (
	"github.com/autocarto/carto_agent/internal/flow/legacy"
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

func legacySections(raw string, contentLine int) []legacy.Section {
	out := []legacy.Section{}
	for _, s := range headings(raw, 3) {
		section := legacy.Section{Title: s.title, Line: contentLine + s.line - 1}
		for _, item := range listItems(s.content) {
			section.Items = append(section.Items, legacy.Item{Content: item.content})
		}
		out = append(out, section)
	}
	return out
}
func parseRoleSlots(doc *FlowDoc, raw string, contentLine int) {
	d := legacy.Document{RoleSlots: doc.RoleSlots}
	executors := []legacy.Executor{}
	for _, x := range ExecutorSpecs() {
		executors = append(executors, legacy.Executor{Type: x.Type, DisplayName: x.DisplayName})
	}
	legacy.ParseRoles(&d, legacySections(raw, contentLine), executors)
	doc.RoleSlots = d.RoleSlots
	doc.Diagnostics = append(doc.Diagnostics, d.Diagnostics...)
}
func parseRequirements(doc *FlowDoc, raw string, contentLine int) {
	d := legacy.Document{Requirements: doc.Requirements}
	legacy.ParseRequirements(&d, legacySections(raw, contentLine))
	doc.Requirements = d.Requirements
	doc.Diagnostics = append(doc.Diagnostics, d.Diagnostics...)
}
