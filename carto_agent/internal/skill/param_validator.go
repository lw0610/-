package skill

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	numericComparisonRulePattern = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*(>=|<=|==|!=|>|<)\s*(-?\d+(?:\.\d+)?)$`)
	layerExistsRulePattern       = regexp.MustCompile(`^layer_exists\(\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)$`)
)

type ExecutionValidationError struct {
	SkillName string
	Message   string
}

func (e *ExecutionValidationError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.SkillName) == "" {
		return strings.TrimSpace(e.Message)
	}
	return fmt.Sprintf("skill %s validation failed: %s", strings.TrimSpace(e.SkillName), strings.TrimSpace(e.Message))
}

func applyParameterDefaults(def SkillDefinition, args map[string]any) map[string]any {
	out := cloneMapAny(args)
	if out == nil {
		out = make(map[string]any)
	}
	for _, param := range def.Spec.Parameters {
		if _, exists := out[param.Name]; exists {
			continue
		}
		if param.Default != nil {
			out[param.Name] = cloneAny(param.Default)
		}
	}
	return out
}

func applyExecutionOutputDefaults(def SkillDefinition, args map[string]any, session SessionContext) map[string]any {
	out := cloneMapAny(args)
	if out == nil {
		out = make(map[string]any)
	}
	if strings.EqualFold(effectiveSkillRuntime(def), RuntimeIDCLI) {
		if def.Execution.Output == nil || !def.Execution.Output.AutoLoad {
			return out
		}
		for _, param := range def.Spec.Parameters {
			if !strings.EqualFold(strings.TrimSpace(param.Type), "output_layer") {
				continue
			}
			if !shouldAutoMaterializeOutput(out[param.Name]) {
				continue
			}
			baseName := strings.TrimSpace(def.Metadata.Name)
			if baseName == "" {
				baseName = strings.TrimSpace(param.Name)
			}
			out[param.Name] = session.AutoOutputPath(baseName)
		}
		return out
	}

	normalizeBridgeFileOutputs(def, out, session)
	return out
}

func normalizeBridgeFileOutputs(def SkillDefinition, args map[string]any, session SessionContext) {
	if !strings.EqualFold(effectiveSkillRuntime(def), RuntimeIDQGIS) {
		return
	}
	subdirs := session.SessionSubdirs()
	exportsDir := strings.TrimSpace(subdirs["exports"])
	if exportsDir == "" {
		return
	}

	for _, param := range def.Spec.Parameters {
		if !strings.EqualFold(strings.TrimSpace(param.Type), "file") {
			continue
		}
		current := strings.TrimSpace(stringifyResolvedValue(args[param.Name]))
		if current == "" || strings.EqualFold(current, "TEMPORARY_OUTPUT") {
			continue
		}
		cleanCurrent := filepath.Clean(current)
		if strings.HasPrefix(cleanCurrent, exportsDir+string(filepath.Separator)) || cleanCurrent == exportsDir {
			continue
		}
		fileName := strings.TrimSpace(filepath.Base(cleanCurrent))
		if fileName == "" || fileName == "." {
			continue
		}
		args[param.Name] = filepath.Join(exportsDir, fileName)
	}
}

func shouldAutoMaterializeOutput(value any) bool {
	text := strings.TrimSpace(stringifyResolvedValue(value))
	if text == "" {
		return true
	}
	if strings.EqualFold(text, "TEMPORARY_OUTPUT") {
		return true
	}
	return strings.HasPrefix(text, "memory:")
}

func validateExecutionArgs(def SkillDefinition, args map[string]any, session SessionContext) error {
	for _, param := range def.Spec.Parameters {
		if !param.Required {
			continue
		}
		if !hasArgumentValue(args[param.Name]) {
			return &ExecutionValidationError{SkillName: def.Metadata.Name, Message: fmt.Sprintf("parameter %s is required", param.Name)}
		}
	}

	env := resolveEnv{vars: buildResolveVars(args, session, def.SourcePath), session: session}
	for _, rule := range def.Execution.Validation {
		ok, err := evaluateValidationRule(rule.Rule, env)
		if err != nil {
			return fmt.Errorf("evaluate validation rule %q: %w", rule.Rule, err)
		}
		if ok {
			continue
		}
		message := strings.TrimSpace(rule.Message)
		if rendered, err := resolveTemplateString(message, env); err == nil {
			message = stringifyResolvedValue(rendered)
		}
		if message == "" {
			message = fmt.Sprintf("validation rule failed: %s", rule.Rule)
		}
		return &ExecutionValidationError{SkillName: def.Metadata.Name, Message: message}
	}

	return nil
}

func evaluateValidationRule(rule string, env resolveEnv) (bool, error) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return true, nil
	}
	if matches := numericComparisonRulePattern.FindStringSubmatch(rule); len(matches) == 4 {
		leftValue, ok := env.vars[matches[1]]
		if !ok {
			return false, fmt.Errorf("unknown validation variable %q", matches[1])
		}
		left, ok := toFloat64(leftValue)
		if !ok {
			return false, fmt.Errorf("validation variable %q is not numeric", matches[1])
		}
		right, err := strconv.ParseFloat(matches[3], 64)
		if err != nil {
			return false, err
		}
		switch matches[2] {
		case ">":
			return left > right, nil
		case ">=":
			return left >= right, nil
		case "<":
			return left < right, nil
		case "<=":
			return left <= right, nil
		case "==":
			return left == right, nil
		case "!=":
			return left != right, nil
		default:
			return false, fmt.Errorf("unsupported comparison operator %q", matches[2])
		}
	}
	if matches := layerExistsRulePattern.FindStringSubmatch(rule); len(matches) == 2 {
		value, ok := env.vars[matches[1]]
		if !ok {
			return false, fmt.Errorf("unknown validation variable %q", matches[1])
		}
		return env.session.LayerExists(stringifyResolvedValue(value)), nil
	}
	return false, fmt.Errorf("unsupported validation rule %q", rule)
}

func hasArgumentValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case []string:
		return len(typed) > 0
	default:
		return true
	}
}

func toFloat64(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
