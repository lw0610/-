package skill

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
)

var (
	fullTemplateExprPattern = regexp.MustCompile(`^\s*\{\{\s*(.*?)\s*\}\}\s*$`)
	templateExprPattern     = regexp.MustCompile(`\{\{\s*(.*?)\s*\}\}`)
	identifierPattern       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	autoOutputExprPattern   = regexp.MustCompile(`^auto_output_path\(\s*(.*?)\s*\)$`)
	resolveLayerExprPattern = regexp.MustCompile(`^resolve_layer\(\s*(.*?)\s*\)$`)
)

type resolveEnv struct {
	vars    map[string]any
	session SessionContext
}

func resolveParamMapping(mapping map[string]any, args map[string]any, parameters []SkillParameter, session SessionContext, sourcePath string) (map[string]any, error) {
	if len(mapping) == 0 {
		return nil, nil
	}
	env := resolveEnv{vars: buildResolveVars(args, session, sourcePath), session: session}
	optional := buildOptionalParamSet(parameters)
	out := make(map[string]any, len(mapping))
	for key, value := range mapping {
		resolved, err := resolveTemplateValue(value, env)
		if err != nil {
			if optional[key] && isUnknownTemplateVariableErr(err) {
				continue
			}
			return nil, fmt.Errorf("resolve param %s: %w", key, err)
		}
		out[key] = resolved
	}
	return out, nil
}

func buildOptionalParamSet(parameters []SkillParameter) map[string]bool {
	out := make(map[string]bool, len(parameters))
	for _, param := range parameters {
		name := strings.TrimSpace(param.Name)
		if name == "" {
			continue
		}
		out[name] = !param.Required
	}
	return out
}

func isUnknownTemplateVariableErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "unknown template variable")
}

func resolveTemplateValue(value any, env resolveEnv) (any, error) {
	switch typed := value.(type) {
	case string:
		return resolveTemplateString(typed, env)
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			resolved, err := resolveTemplateValue(item, env)
			if err != nil {
				return nil, err
			}
			out[key] = resolved
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for idx, item := range typed {
			resolved, err := resolveTemplateValue(item, env)
			if err != nil {
				return nil, err
			}
			out[idx] = resolved
		}
		return out, nil
	default:
		return cloneAny(value), nil
	}
}

func resolveTemplateString(raw string, env resolveEnv) (any, error) {
	if matches := fullTemplateExprPattern.FindStringSubmatch(raw); len(matches) == 2 {
		return evalTemplateExpr(matches[1], env)
	}
	if !strings.Contains(raw, "{{") {
		return raw, nil
	}

	var resolveErr error
	resolved := templateExprPattern.ReplaceAllStringFunc(raw, func(segment string) string {
		if resolveErr != nil {
			return segment
		}
		matches := templateExprPattern.FindStringSubmatch(segment)
		if len(matches) != 2 {
			return segment
		}
		value, err := evalTemplateExpr(matches[1], env)
		if err != nil {
			resolveErr = err
			return segment
		}
		return stringifyResolvedValue(value)
	})
	if resolveErr != nil {
		return nil, resolveErr
	}
	return resolved, nil
}

func evalTemplateExpr(expr string, env resolveEnv) (any, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("empty template expression")
	}
	if matches := autoOutputExprPattern.FindStringSubmatch(expr); len(matches) == 2 {
		name, err := evalExpressionArgument(matches[1], env)
		if err != nil {
			return nil, err
		}
		return env.session.AutoOutputPath(stringifyResolvedValue(name)), nil
	}
	if matches := resolveLayerExprPattern.FindStringSubmatch(expr); len(matches) == 2 {
		reference, err := evalExpressionArgument(matches[1], env)
		if err != nil {
			return nil, err
		}
		return env.session.ResolveLayer(stringifyResolvedValue(reference)), nil
	}
	if identifierPattern.MatchString(expr) {
		value, ok := env.vars[expr]
		if !ok {
			return nil, fmt.Errorf("unknown template variable %q", expr)
		}
		return cloneAny(value), nil
	}
	if unquoted, ok := unquoteTemplateLiteral(expr); ok {
		return unquoted, nil
	}
	return nil, fmt.Errorf("unsupported template expression %q", expr)
}

func evalExpressionArgument(raw string, env resolveEnv) (any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty expression argument")
	}
	if unquoted, ok := unquoteTemplateLiteral(raw); ok {
		return unquoted, nil
	}
	if identifierPattern.MatchString(raw) {
		value, ok := env.vars[raw]
		if !ok {
			return nil, fmt.Errorf("unknown template variable %q", raw)
		}
		return cloneAny(value), nil
	}
	return nil, fmt.Errorf("unsupported expression argument %q", raw)
}

func buildResolveVars(args map[string]any, session SessionContext, sourcePath string) map[string]any {
	session = session.Clone()
	session.normalize()
	vars := cloneMapAny(args)
	if vars == nil {
		vars = make(map[string]any)
	}
	mergeResolveVar(vars, "session_id", session.SessionID)
	mergeResolveVar(vars, "project_id", session.ProjectID)
	mergeResolveVar(vars, "project_path", session.ProjectPath)
	mergeResolveVar(vars, "instance_id", session.InstanceID)
	mergeResolveVar(vars, "python_path", session.PythonPath)
	mergeResolveVar(vars, "autocarto_home", resolveAutocartoHome(session))
	if skillDir := resolveSkillDir(sourcePath); skillDir != "" {
		mergeResolveVar(vars, "skill_dir", skillDir)
	}
	return vars
}

func resolveAutocartoHome(session SessionContext) string {
	if trimmed := strings.TrimSpace(session.AutocartoHome); trimmed != "" {
		return trimmed
	}
	resolved, err := config.ResolveAutoCartoHome()
	if err != nil {
		return ""
	}
	return resolved
}

func resolveSkillDir(sourcePath string) string {
	trimmedSourcePath := strings.TrimSpace(sourcePath)
	if trimmedSourcePath == "" {
		return ""
	}
	dir := filepath.Dir(trimmedSourcePath)
	if strings.EqualFold(filepath.Base(dir), "tools") {
		parent := filepath.Dir(dir)
		if parent != "" && parent != "." {
			return parent
		}
	}
	return dir
}

func mergeResolveVar(vars map[string]any, key string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	if _, exists := vars[key]; exists {
		return
	}
	vars[key] = value
}

func stringifyResolvedValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(typed)
	}
}

func unquoteTemplateLiteral(raw string) (string, bool) {
	if len(raw) < 2 {
		return "", false
	}
	if (strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"")) || (strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'")) {
		unquoted := raw[1 : len(raw)-1]
		if strings.HasPrefix(raw, "\"") {
			parsed, err := strconv.Unquote(raw)
			if err == nil {
				return parsed, true
			}
		}
		return unquoted, true
	}
	return "", false
}
