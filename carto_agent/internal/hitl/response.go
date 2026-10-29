package hitl

import "strings"

// Decision returns only the explicitly addressed answer. An empty ID addresses
// the single-question card; it never searches unrelated batch answers.
func (o RequestOutput) Decision(id string) (DecisionResult, bool) {
	if id = strings.TrimSpace(id); id != "" {
		answer, ok := o.Decisions[id]
		return answer, ok
	}
	if o.Answer == nil {
		return DecisionResult{}, false
	}
	return *o.Answer, true
}

// IsSelected requires one explicit choice. A submitted form, an affirmative
// answer to another question, or a selection containing multiple values does
// not authorize a single-choice operation.
func (o RequestOutput) IsSelected(value string) bool {
	answer, ok := o.Decision("")
	return o.Approved && ok && answer.IsSelected(value)
}

func (o RequestOutput) IsConfirmed() bool {
	answer, ok := o.Decision("")
	return o.Approved && ok && answer.IsConfirmed()
}

func (d DecisionResult) IsSelected(value string) bool {
	return value != "" && len(d.Selected) == 1 && d.Selected[0] == value &&
		(d.Confirmed == nil || *d.Confirmed)
}

func (d DecisionResult) IsConfirmed() bool {
	return d.Confirmed != nil && *d.Confirmed && len(d.Selected) == 0
}

func normalizeResponse(info InterruptPayload, hasData bool, data any) RequestOutput {
	out := RequestOutput{Type: info.Type}
	if !hasData || data == nil {
		return out
	}
	// Keep arbitrary file and text payloads intact; parsing a decision must not
	// discard fields owned by another kind of interaction.
	out.UserData = data
	if responseCancelled(data) {
		return out
	}
	out.Approved = true
	if len(info.Decisions) > 0 {
		parsed := extractDecisionResults(data)
		out.Decisions = map[string]DecisionResult{}
		for _, item := range info.Decisions {
			if answer, ok := parsed[item.ID]; ok {
				out.Decisions[item.ID] = answer
			}
		}
		if len(out.Decisions) == 0 {
			out.Decisions = nil
		}
		return out
	}
	if answer, ok := parseDecisionResult(data, true); ok {
		out.Answer = &answer
		if answer.Confirmed != nil && !*answer.Confirmed {
			out.Approved = false
		}
	}
	return out
}

func responseCancelled(raw any) bool {
	value, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"cancelled", "canceled"} {
		if flag, ok := value[key].(bool); ok && flag {
			return true
		}
	}
	approved, ok := value["approved"].(bool)
	return ok && !approved
}

func extractDecisionResults(raw any) map[string]DecisionResult {
	if typed, ok := raw.(map[string]DecisionResult); ok {
		return normalizeDecisionResultsMap(typed, true)
	}
	record, ok := raw.(map[string]any)
	if !ok || responseCancelled(raw) {
		return nil
	}
	if nested, exists := record["decisions"]; exists {
		return normalizeDecisionResultsMap(nested, true)
	}
	// Historical batch payloads used a direct ID -> structured answer mapping.
	// Top-level option/text fields belong to a single answer, not question IDs.
	return normalizeDecisionResultsMap(record, false)
}

func normalizeDecisionResultsMap(raw any, allowPrimitive bool) map[string]DecisionResult {
	result := map[string]DecisionResult{}
	if typed, ok := raw.(map[string]DecisionResult); ok {
		for id, value := range typed {
			if parsed, ok := parseDecisionResult(value, true); ok && strings.TrimSpace(id) != "" {
				result[strings.TrimSpace(id)] = parsed
			}
		}
	} else if record, ok := raw.(map[string]any); ok {
		for id, value := range record {
			if parsed, ok := parseDecisionResult(value, allowPrimitive); ok && strings.TrimSpace(id) != "" {
				result[strings.TrimSpace(id)] = parsed
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// parseDecisionResult is the only translation from UI/legacy answer fields to
// the normalized result. Values are not coerced: numbers or arbitrary objects
// must never become apparently valid choices.
func parseDecisionResult(raw any, allowPrimitive bool) (DecisionResult, bool) {
	if typed, ok := raw.(DecisionResult); ok {
		typed.Selected = normalizeStringSlice(typed.Selected)
		typed.Text = strings.TrimSpace(typed.Text)
		typed.Note = strings.TrimSpace(typed.Note)
		return typed, decisionHasValue(typed)
	}
	if record, ok := raw.(map[string]any); ok {
		if responseCancelled(record) {
			return DecisionResult{}, false
		}
		result := DecisionResult{}
		for _, key := range []string{"selected", "options", "option", "value"} {
			if selected := normalizeAnyStringSlice(record[key]); len(selected) > 0 {
				if len(result.Selected) > 0 && !sameSelection(result.Selected, selected) {
					// Conflicting encodings are not a choice; do not pick whichever
					// alias happens to have higher precedence.
					return DecisionResult{}, false
				}
				result.Selected = selected
			}
		}
		result.Text = firstString(record["text"], record["input"])
		result.Note = firstString(record["note"])
		if confirmed, ok := record["confirmed"].(bool); ok {
			result.Confirmed = &confirmed
		}
		return result, decisionHasValue(result)
	}
	if !allowPrimitive {
		return DecisionResult{}, false
	}
	if confirmed, ok := raw.(bool); ok {
		return DecisionResult{Confirmed: &confirmed}, true
	}
	selected := normalizeAnyStringSlice(raw)
	return DecisionResult{Selected: selected}, len(selected) > 0
}

func normalizeAnyStringSlice(raw any) []string {
	var values []string
	switch value := raw.(type) {
	case string:
		values = []string{value}
	case []string:
		values = value
	case []any:
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				return nil
			}
			values = append(values, text)
		}
	}
	return normalizeStringSlice(values)
}

func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func decisionHasValue(d DecisionResult) bool {
	return len(d.Selected) > 0 || d.Text != "" || d.Note != "" || d.Confirmed != nil
}

func sameSelection(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := map[string]bool{}
	for _, value := range left {
		seen[value] = true
	}
	for _, value := range right {
		if !seen[value] {
			return false
		}
	}
	return true
}
