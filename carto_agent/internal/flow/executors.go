package flow

import "strings"

// ExecutorSpec declares validation requirements without importing execution code.
type ExecutorSpec struct {
	Type               string                        `json:"type"`
	DisplayName        string                        `json:"display_name"`
	RequiredInputTypes []string                      `json:"required_input_types"`
	RequiredResources  []ExecutorResourceRequirement `json:"required_resources"`
}

type ExecutorResourceRequirement struct {
	Type          string   `json:"type"`
	Label         string   `json:"label"`
	RequiredNames []string `json:"required_names"`
}

func ExecutorSpecs() []ExecutorSpec {
	return []ExecutorSpec{{Type: "route_knowledge", DisplayName: "路线知识抽取", RequiredInputTypes: []string{"conversation_text"}, RequiredResources: []ExecutorResourceRequirement{
		{Type: "route_style", Label: "路线样式"},
		{Type: "symbols", Label: "专题符号", RequiredNames: []string{"start_flag", "waypoint"}},
	}}}
}

func FindExecutorSpec(executor string) (ExecutorSpec, bool) {
	for _, spec := range ExecutorSpecs() {
		if spec.Type == executor {
			return spec, true
		}
	}
	return ExecutorSpec{}, false
}

// Resource contracts are checked after linking, so repairing a resource only
// re-links the plan and never calls the model or silently supplies a default.
func executorResourceDiagnostics(plan *FlowPlan) []Diagnostic {
	problems := []Diagnostic{}
	byKey := map[string]ResourcePlan{}
	for _, item := range plan.Resources {
		byKey[item.Key] = item
	}
	for _, step := range plan.Steps {
		if step.Kind != "executor" {
			continue
		}
		spec, ok := FindExecutorSpec(step.Executor)
		if !ok {
			continue
		}
		for _, required := range spec.RequiredResources {
			selected := []ResourcePlan{}
			for _, key := range uniqueStrings(step.Refs.Resources) {
				if item, ok := byKey[key]; ok && item.Type == required.Type {
					selected = append(selected, item)
				}
			}
			fix := map[string]any{"action": "open_handbook", "resource_type": required.Type}
			if len(selected) == 0 {
				problems = append(problems, Diagnostic{Level: "error", Code: "FLOW_EXECUTOR_RESOURCE_MISSING", Step: step.ID, Message: "执行器步骤必须明确引用一项" + required.Label + "资源。", Fix: map[string]any{"action": "import_resource", "resource_type": required.Type}})
				continue
			}
			if len(selected) > 1 {
				problems = append(problems, Diagnostic{Level: "error", Code: "FLOW_EXECUTOR_RESOURCE_AMBIGUOUS", Step: step.ID, Message: "执行器步骤引用了多项" + required.Label + "资源，请只选择一项。", Fix: fix})
				continue
			}
			names := map[string]bool{}
			for _, entry := range selected[0].Entries {
				names[entry.Name] = true
			}
			missing := []string{}
			for _, name := range required.RequiredNames {
				if !names[name] {
					missing = append(missing, name)
				}
			}
			if len(missing) > 0 {
				problems = append(problems, Diagnostic{Level: "error", Code: "FLOW_RESOURCE_CONTRACT_UNSATISFIED", Step: step.ID, Message: required.Label + "「" + selected[0].Label + "」缺少执行器需要的条目：" + strings.Join(missing, "、") + "。", Fix: map[string]any{"action": "import_resource", "resource_id": selected[0].ID, "resource_type": required.Type}})
			}
		}
	}
	return problems
}
