package flowrun

import (
	"encoding/json"
	"sort"

	"github.com/autocarto/carto_agent/internal/flow"
)

// A compiler-version upgrade alone must not rewrite persisted input keys. Only
// plans whose executable structure can still be proven equal use this bridge.
func legacyResumeCompatible(snapshot, current *flow.FlowPlan, values map[string]any, projectRoot string) bool {
	if snapshot == nil || current == nil || current.Compiler.Version != flow.CompilerVersion || snapshot.Compiler.Version == flow.CompilerVersion {
		return false
	}
	if snapshot.Compiler.Version != "" && snapshot.Compiler.Version != "1" && snapshot.Compiler.Version != "2" {
		return false
	}
	if snapshot.FlowID != current.FlowID || snapshot.Scheme != current.Scheme || len(snapshot.Inputs) != len(current.Inputs) || len(snapshot.Steps) != len(current.Steps) {
		return false
	}
	oldLabels, newLabels := map[string]string{}, map[string]string{}
	prior := map[string]flow.InputPlan{}
	for _, input := range snapshot.Inputs {
		label := input.Label
		if label == "" {
			label = input.Key
		}
		if _, exists := prior[label]; exists {
			return false
		}
		prior[label] = input
		oldLabels[input.Key] = label
	}
	currentValues := resumePlanInputs(snapshot, current, values)
	for _, input := range current.Inputs {
		label := input.Label
		if label == "" {
			label = input.Key
		}
		old, ok := prior[label]
		if !ok {
			return false
		}
		newLabels[input.Key] = label
		if old.Required != input.Required || !sameJSON(old.Default, input.Default) || !sameJSON(nonNil(old.Options), nonNil(input.Options)) {
			return false
		}
		if old.Type != input.Type {
			if old.Type != "" && old.Type != "text" {
				return false
			}
			if input.Type != "file" && input.Type != "folder" && input.Type != "layer" {
				return false
			}
			if _, err := convertInput(input, currentValues[input.Key], projectRoot); err != nil {
				return false
			}
		}
	}
	oldResources, newResources := map[string]string{}, map[string]string{}
	for _, r := range snapshot.Resources {
		oldResources[r.Key] = r.Type + ":" + r.Label
	}
	for _, r := range current.Resources {
		newResources[r.Key] = r.Type + ":" + r.Label
	}
	normalizeCondition := func(condition *flow.ConditionDoc, labels map[string]string) *flow.ConditionDoc {
		return remapCondition(condition, labels)
	}
	type stepStructure struct {
		ID, Title, Kind, Role, Executor, Instructions, Completion, OnFailure string
		Condition                                                            *flow.ConditionDoc
		Refs                                                                 flow.PlanRefs
		Outputs                                                              []flow.OutputDeclaration
	}
	shape := func(s flow.StepPlan, labels, resources map[string]string) stepStructure {
		refs := flow.PlanRefs{Inputs: remapKeys(s.Refs.Inputs, labels), Resources: remapKeys(s.Refs.Resources, resources), Outputs: append([]flow.OutputRef{}, s.Refs.Outputs...)}
		return stepStructure{s.ID, s.Title, s.Kind, s.Role, s.Executor, s.Instructions, s.Completion, s.OnFailure, normalizeCondition(s.Condition, labels), refs, append([]flow.OutputDeclaration{}, s.ExpectedOutputs...)}
	}
	relevant := map[string]bool{}
	auto := false
	for i, old := range snapshot.Steps {
		next := current.Steps[i]
		if !sameJSON(shape(old, oldLabels, oldResources), shape(next, newLabels, newResources)) {
			return false
		}
		relevant[old.Role] = true
		if old.Kind == "auto" {
			auto = true
		}
	}
	roleShape := func(plan *flow.FlowPlan) map[string]any {
		result := map[string]any{}
		for _, role := range plan.Roles {
			if !auto && !relevant[role.ID] {
				continue
			}
			tools := append([]string{}, role.Tools...)
			packages := append([]string{}, role.ToolPackages...)
			sort.Strings(tools)
			sort.Strings(packages)
			result[role.ID] = []any{role.Kind, role.Executor, role.Enabled, role.FlowOnly, role.IsMain, tools, packages}
		}
		return result
	}
	constraints := func(plan *flow.FlowPlan, labels map[string]string) []flow.InputConstraint {
		out := []flow.InputConstraint{}
		for _, c := range plan.InputConstraints {
			out = append(out, flow.InputConstraint{Op: c.Op, Inputs: remapKeys(c.Inputs, labels)})
		}
		return out
	}
	return sameJSON(roleShape(snapshot), roleShape(current)) && sameJSON(constraints(snapshot, oldLabels), constraints(current, newLabels)) && sameJSON(nonNil(snapshot.Rules), nonNil(current.Rules)) && snapshot.Deliverables == current.Deliverables
}
func nonNil(values []string) []string { return append([]string{}, values...) }
func sameJSON(a, b any) bool {
	x, e1 := json.Marshal(a)
	y, e2 := json.Marshal(b)
	return e1 == nil && e2 == nil && string(x) == string(y)
}
func remapKeys(keys []string, labels map[string]string) []string {
	out := []string{}
	for _, key := range keys {
		if label, ok := labels[key]; ok {
			out = append(out, label)
		} else {
			out = append(out, key)
		}
	}
	return out
}
func remapCondition(condition *flow.ConditionDoc, labels map[string]string) *flow.ConditionDoc {
	if condition == nil {
		return nil
	}
	raw, _ := json.Marshal(condition)
	out := &flow.ConditionDoc{}
	_ = json.Unmarshal(raw, out)
	var visit func(*flow.ConditionRule)
	visit = func(rule *flow.ConditionRule) {
		if rule == nil {
			return
		}
		if label, ok := labels[rule.Input]; ok {
			rule.Input = label
		}
		for i := range rule.Args {
			visit(&rule.Args[i])
		}
		visit(rule.Arg)
	}
	visit(out.Rule)
	return out
}
func resumePlanInputs(snapshot, current *flow.FlowPlan, values map[string]any) map[string]any {
	result := map[string]any{}
	labels := map[string]string{}
	if snapshot != nil {
		for _, input := range snapshot.Inputs {
			label := input.Label
			if label == "" {
				label = input.Key
			}
			labels[label] = input.Key
		}
	}
	if current != nil {
		for _, input := range current.Inputs {
			label := input.Label
			if label == "" {
				label = input.Key
			}
			key, ok := labels[label]
			if !ok {
				key = input.Key
			}
			if value, exists := values[key]; exists {
				result[input.Key] = value
			}
		}
	}
	return result
}

// Retain the old fingerprint seed when the resolved resource metadata is equal.
// Re-stat the old paths so a real file change still requires acknowledgement.
func legacyResourceFingerprint(snapshot, current *flow.FlowPlan) string {
	shape := func(plan *flow.FlowPlan) []any {
		out := []any{}
		for _, r := range plan.Resources {
			out = append(out, []any{r.ID, r.Label, r.Type, r.Version, r.Owner, r.Root, r.Entry, r.Digest, r.BaseDir, r.BaseDirFromInput, r.Entries})
		}
		return out
	}
	if !sameJSON(shape(snapshot), shape(current)) {
		return current.LinkFingerprint
	}
	if fingerprint, err := flow.ResourceSnapshotFingerprint(snapshot); err == nil {
		return fingerprint
	}
	return current.LinkFingerprint
}
