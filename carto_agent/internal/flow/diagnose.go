package flow

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

func bindRole(step StepDoc, roles []RoleSpec) (*RoleSpec, []Diagnostic) {
	label := strings.TrimSpace(step.Role)
	if label == "" || label == "系统自动" || label == "用户" || label == "人工" {
		return nil, nil
	}
	var matches []int
	for _, match := range []func(RoleSpec) bool{func(role RoleSpec) bool { return role.DisplayName == label }, func(role RoleSpec) bool { return role.ID == label }, func(role RoleSpec) bool { return strings.Contains(role.DisplayName, label) }} {
		for index, role := range roles {
			if match(role) {
				matches = append(matches, index)
			}
		}
		if len(matches) > 0 {
			break
		}
	}
	fix := map[string]any{"action": "set_role", "step": step.ID}
	if len(matches) == 0 {
		return nil, []Diagnostic{{Level: "error", Code: "FLOW_ROLE_NOT_FOUND", Step: step.ID, Message: "当前方案没有负责角色“" + label + "”。", Fix: fix}}
	}
	if len(matches) > 1 {
		return nil, []Diagnostic{{Level: "error", Code: "FLOW_ROLE_AMBIGUOUS", Step: step.ID, Message: "负责角色“" + label + "”匹配到多个角色，请填写完整显示名或编号。", Fix: fix}}
	}
	role := roles[matches[0]]
	if !role.Enabled {
		return &role, []Diagnostic{{Level: "error", Code: "FLOW_ROLE_DISABLED", Step: step.ID, Message: "负责角色“" + role.DisplayName + "”已被禁用。", Fix: map[string]any{"action": "enable_role", "role": role.ID}}}
	}
	return &role, nil
}

func normalizeCondition(rule *ConditionRule, inputs []InputPlan) {
	if rule == nil {
		return
	}
	for _, input := range inputs {
		if rule.Input == input.Label {
			rule.Input = input.Key
			break
		}
	}
	for index := range rule.Args {
		normalizeCondition(&rule.Args[index], inputs)
	}
	normalizeCondition(rule.Arg, inputs)
}

func validateCondition(rule *ConditionRule, inputs []InputPlan, steps []StepDoc, index int) error {
	keys := map[string]bool{}
	for _, input := range inputs {
		keys[input.Key] = true
	}
	previous := map[string]bool{}
	for _, step := range steps {
		if step.Index < index {
			previous[step.ID] = true
		}
	}
	var visit func(*ConditionRule, int) error
	visit = func(value *ConditionRule, depth int) error {
		if value == nil || depth > 32 {
			return fmt.Errorf("条件规则为空或嵌套过深")
		}
		fields := ""
		switch value.Op {
		case "provided", "not_provided", "truthy", "falsy":
			fields = "input"
			if !keys[value.Input] {
				return fmt.Errorf("条件引用未知输入：%s", value.Input)
			}
		case "equals":
			fields = "input value"
			if !keys[value.Input] || value.Value == nil {
				return fmt.Errorf("equals 需要有效输入和值")
			}
		case "step_status":
			fields = "step status"
			if !previous[value.Step] {
				return fmt.Errorf("条件只能引用前序步骤：%s", value.Step)
			}
			if value.Status != "completed" && value.Status != "failed" && value.Status != "skipped" && value.Status != "cancelled" {
				return fmt.Errorf("步骤状态无效")
			}
		case "output_exists":
			fields = "step label"
			if !previous[value.Step] || strings.TrimSpace(value.Label) == "" {
				return fmt.Errorf("产出条件需要前序步骤和产出名")
			}
		case "and", "or":
			fields = "args"
			if len(value.Args) == 0 {
				return fmt.Errorf("组合条件不能为空")
			}
			for i := range value.Args {
				if err := visit(&value.Args[i], depth+1); err != nil {
					return err
				}
			}
		case "not":
			fields = "arg"
			if err := visit(value.Arg, depth+1); err != nil {
				return err
			}
		default:
			return fmt.Errorf("条件操作符无效：%s", value.Op)
		}
		for name, present := range map[string]bool{"input": value.Input != "", "value": value.Value != nil, "step": value.Step != "", "status": value.Status != "", "label": value.Label != "", "args": len(value.Args) > 0, "arg": value.Arg != nil} {
			if present && !strings.Contains(" "+fields+" ", " "+name+" ") {
				return fmt.Errorf("操作符 %s 不接受字段 %s", value.Op, name)
			}
		}
		return nil
	}
	return visit(rule, 0)
}

func completeDiagnostics(doc *FlowDoc, plan *FlowPlan, env Environment) {
	resolved := *doc
	resolved.Diagnostics = nil
	resolved.Steps = append([]StepDoc{}, doc.Steps...)
	for index := range resolved.Steps {
		resolved.Steps[index].ExpectedOutputs = plan.Steps[index].ExpectedOutputs
		resolved.Steps[index].Refs = DocRefs{}
	}
	resolveReferences(&resolved)
	plan.Diagnostics = append(plan.Diagnostics, resolved.Diagnostics...)
	inputsByLabel, resourcesByLabel := map[string]string{}, map[string]string{}
	inputType := map[string]string{}
	used := map[string]bool{}
	for _, input := range plan.Inputs {
		inputsByLabel[input.Label] = input.Key
		inputType[input.Key] = input.Type
	}
	for _, item := range plan.Resources {
		resourcesByLabel[item.Label] = item.Key
	}
	roleByID := map[string]RoleSpec{}
	for _, role := range env.Roles {
		roleByID[role.ID] = role
	}
	capabilities := map[string]CapabilitySpec{}
	for _, capability := range env.Capabilities {
		capabilities[capability.Name] = capability
	}
	outputs := map[string]map[string]bool{}
	for _, step := range plan.Steps {
		outputs[step.ID] = map[string]bool{}
		for _, output := range step.ExpectedOutputs {
			outputs[step.ID][output.Label] = true
		}
	}
	for index := range plan.Steps {
		step := &plan.Steps[index]
		if stepHasToolDetail(step.Instructions, env.Capabilities) {
			plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "info", Code: "FLOW_STEP_TOOL_DETAIL", Step: step.ID, Message: "这一步写得像工具说明，建议精简", Fix: map[string]any{"action": "open_handbook"}})
		}
		refs := resolved.Steps[index].Refs
		for _, label := range refs.Inputs {
			if key := inputsByLabel[label]; key != "" {
				step.Refs.Inputs = append(step.Refs.Inputs, key)
				used[key] = true
			}
		}
		for _, label := range refs.Resources {
			if key := resourcesByLabel[label]; key != "" {
				step.Refs.Resources = append(step.Refs.Resources, key)
			}
		}
		step.Refs.Outputs = append([]OutputRef{}, refs.Outputs...)
		if step.Condition != nil {
			normalizeCondition(step.Condition.Rule, plan.Inputs)
			if step.Condition.Rule != nil {
				if err := validateCondition(step.Condition.Rule, plan.Inputs, resolved.Steps, step.Index); err != nil {
					// 解析器可直接识别的“第N步”同样必须遵守前序限制。
					if step.Condition.Rule.Step != "" {
						plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "error", Code: "FLOW_REF_FORWARD", Step: step.ID, Message: "条件引用的步骤不存在或不是前序步骤。"})
					}
					step.Condition.Rule = nil
				}
			}
			if step.Condition.Rule == nil {
				plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "info", Code: "FLOW_CONDITION_UNPARSED", Step: step.ID, Message: "条件未能转换为规则，运行时需要按原文判断。"})
			} else {
				walkCondition(step.Condition.Rule, func(rule *ConditionRule) {
					if rule.Input != "" {
						used[rule.Input] = true
						step.Refs.Inputs = uniqueStrings(append(step.Refs.Inputs, rule.Input))
					}
					if rule.Op == "output_exists" {
						if !outputs[rule.Step][rule.Label] {
							plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "warning", Code: "FLOW_REF_UNRESOLVED", Step: step.ID, Message: "条件引用的产出“" + rule.Label + "”未在前序步骤声明。"})
						} else {
							found := false
							for _, ref := range step.Refs.Outputs {
								if ref.Step == rule.Step && ref.Label == rule.Label {
									found = true
								}
							}
							if !found {
								step.Refs.Outputs = append(step.Refs.Outputs, OutputRef{Step: rule.Step, Label: rule.Label})
							}
						}
					}
				})
			}
		}
		role := roleByID[step.Role]
		if step.Kind == "executor" {
			if executor, ok := FindExecutorSpec(step.Executor); ok {
				for _, requiredType := range executor.RequiredInputTypes {
					found := false
					for _, key := range step.Refs.Inputs {
						if inputType[key] == requiredType {
							found = true
						}
					}
					if !found {
						plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "error", Code: "FLOW_EXECUTOR_INPUT_MISSING", Step: step.ID, Message: "专用执行器“" + executor.DisplayName + "”需要本步引用 " + requiredType + " 类型的输入。"})
					}
				}
			} else {
				plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "error", Code: "FLOW_EXECUTOR_NOT_FOUND", Step: step.ID, Message: "方案中的专用执行器类型“" + step.Executor + "”未注册。"})
			}
		}
		owned := map[string]bool{}
		for _, name := range role.Tools {
			owned[name] = true
		}
		if step.Role != "" {
			for _, name := range step.SuggestedTools {
				if owned[name] {
					continue
				}
				capability := capabilities[name]
				packageIDs := append([]string{}, capability.PackageIDs...)
				sort.Strings(packageIDs)
				fix := map[string]any{"action": "open_handbook"}
				if len(packageIDs) > 0 {
					fix = map[string]any{"action": "add_tool_package", "role": step.Role, "package": packageIDs[0]}
				}
				plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "warning", Code: "FLOW_CAPABILITY_MISSING_IN_ROLE", Step: step.ID, Message: "建议能力“" + capability.DisplayName + "”存在，但负责角色“" + role.DisplayName + "”尚未获得对应工具包。", Fix: fix})
			}
		}
		for _, missing := range step.MissingCapabilities {
			plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "warning", Code: "FLOW_CAPABILITY_NOT_FOUND", Step: step.ID, Message: "系统可能缺少所需能力：" + missing, Fix: map[string]any{"action": "open_handbook"}})
		}
	}
	for _, r := range plan.Resources {
		if r.BaseDirFromInput != "" {
			used[r.BaseDirFromInput] = true
		}
	}
	for _, input := range plan.Inputs {
		if !used[input.Key] {
			plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "info", Code: "FLOW_INPUT_UNUSED", Message: "输入“" + input.Label + "”没有被任何步骤或条件引用。"})
		}
	}
}

func walkCondition(rule *ConditionRule, visit func(*ConditionRule)) {
	if rule == nil {
		return
	}
	visit(rule)
	for index := range rule.Args {
		walkCondition(&rule.Args[index], visit)
	}
	walkCondition(rule.Arg, visit)
}

// Match complete parameter identifiers: line_width is relevant, while
// outline_width and line_width_mm are distinct names. Chinese prose may touch
// an ASCII parameter directly (for example “设置line_width为2”).
func stepHasToolDetail(instructions string, capabilities []CapabilitySpec) bool {
	if utf8.RuneCountInString(instructions) > 600 {
		return true
	}
	identifier := func(b byte) bool {
		return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
	}
	for _, capability := range capabilities {
		for _, raw := range capability.Parameters {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			for from := 0; from < len(instructions); {
				offset := strings.Index(instructions[from:], name)
				if offset < 0 {
					break
				}
				start := from + offset
				end := start + len(name)
				if (start == 0 || !identifier(instructions[start-1])) && (end == len(instructions) || !identifier(instructions[end])) {
					return true
				}
				from = end
			}
		}
	}
	return false
}
