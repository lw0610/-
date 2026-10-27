package flow

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type Reach string

const (
	ReachMust  Reach = "must"
	ReachMaybe Reach = "maybe"
	ReachNever Reach = "never"
)

func InputProvided(value any) bool {
	if value == nil {
		return false
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case map[string]any:
		if v["$ref"] == "current_user_message" {
			b, _ := json.Marshal(v["length"])
			return string(b) != "0" && string(b) != "null"
		}
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map:
		return v.Len() > 0
	}
	return true
}
func InputTruthy(value any) bool {
	if v, ok := value.(bool); ok {
		return v
	}
	if value == nil {
		return false
	}
	switch fmt.Sprint(value) {
	case "false", "否", "0", "":
		return false
	}
	return InputProvided(value)
}
func StepReach(plan *FlowPlan, inputs map[string]any, modes map[string]string) map[string]Reach {
	result := map[string]Reach{}
	if plan == nil {
		return result
	}
	for _, step := range plan.Steps {
		r := ReachMust
		if step.Condition != nil {
			r = ReachMaybe
			if inputs != nil {
				r = conditionReach(step.Condition.Rule, inputs, modes)
			}
		}
		result[step.ID] = r
	}
	return result
}
func conditionReach(rule *ConditionRule, inputs map[string]any, modes map[string]string) Reach {
	if rule == nil {
		return ReachMaybe
	}
	v := inputs[rule.Input]
	future := modes[rule.Input] == "auto" || modes[rule.Input] == "fill"
	b := false
	switch rule.Op {
	case "provided":
		b = future || InputProvided(v)
	case "not_provided":
		b = !(future || InputProvided(v))
	case "truthy":
		b = future || InputTruthy(v)
	case "falsy":
		b = !(future || InputTruthy(v))
	case "equals":
		if future {
			return ReachMaybe
		}
		b = reflect.DeepEqual(v, rule.Value) || fmt.Sprint(v) == fmt.Sprint(rule.Value)
	case "not":
		switch conditionReach(rule.Arg, inputs, modes) {
		case ReachMust:
			return ReachNever
		case ReachNever:
			return ReachMust
		default:
			return ReachMaybe
		}
	case "and", "or":
		if len(rule.Args) == 0 {
			return ReachMaybe
		}
		unknown := false
		for i := range rule.Args {
			r := conditionReach(&rule.Args[i], inputs, modes)
			if rule.Op == "and" && r == ReachNever {
				return ReachNever
			}
			if rule.Op == "or" && r == ReachMust {
				return ReachMust
			}
			unknown = unknown || r == ReachMaybe
		}
		if unknown {
			return ReachMaybe
		}
		if rule.Op == "and" {
			return ReachMust
		}
		return ReachNever
	default:
		return ReachMaybe
	}
	if b {
		return ReachMust
	}
	return ReachNever
}

// RoleIssues rechecks conditional role requirements using the current run inputs.
func RoleIssues(plan *FlowPlan, inputs map[string]any, modes map[string]string) []Diagnostic {
	reach := StepReach(plan, inputs, modes)
	result := []Diagnostic{}
	for _, step := range plan.Steps {
		if step.Unresolved == "" || reach[step.ID] == ReachNever {
			continue
		}
		level := "error"
		message := "步骤“" + step.Title + "”缺少可用执行者或所需工具包。"
		if reach[step.ID] == ReachMaybe {
			level = "warning"
			if step.Condition != nil {
				message += "仅在条件满足时需要：" + step.Condition.Text
			}
		}
		result = append(result, Diagnostic{Level: level, Code: step.Unresolved, Step: step.ID, Message: message})
	}
	return result
}
func gradeRoleDiagnostics(plan *FlowPlan, inputs map[string]any, modes map[string]string) {
	issues := RoleIssues(plan, inputs, modes)
	byStep := map[string]Diagnostic{}
	for _, d := range issues {
		byStep[d.Step] = d
	}
	for i := range plan.Diagnostics {
		d := &plan.Diagnostics[i]
		if issue, ok := byStep[d.Step]; ok && d.Code == issue.Code && d.Code != "FLOW_ROLE_AMBIGUOUS" && d.Code != "FLOW_ROLE_PACKAGE_AMBIGUOUS" {
			d.Level = issue.Level
			if issue.Level == "warning" {
				d.Message += " " + issue.Message
			}
		}
	}
}
