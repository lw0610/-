package flowrun

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/autocarto/carto_agent/internal/flow"
)

type StartupIssue struct {
	Code    string `json:"code"`
	Input   string `json:"input,omitempty"`
	Message string `json:"message"`
}
type InputError struct {
	Reasons []StartupIssue `json:"reasons"`
}

func (e *InputError) Error() string {
	parts := []string{}
	for _, r := range e.Reasons {
		parts = append(parts, r.Message)
	}
	return strings.Join(parts, "；")
}

func ResolveInputs(plan *flow.FlowPlan, raw map[string]any, source, projectRoot string) (map[string]any, error) {
	return resolveInputs(plan, raw, source, projectRoot, false)
}
func resolveInputs(plan *flow.FlowPlan, raw map[string]any, source, projectRoot string, allowReference bool) (map[string]any, error) {
	resolved := map[string]any{}
	reasons := []StartupIssue{}
	known := map[string]bool{}
	for _, input := range plan.Inputs {
		known[input.Key] = true
		value, exists := raw[input.Key]
		if !exists || value == nil {
			value = input.Default
		}
		if input.Type == "conversation_text" {
			if strings.TrimSpace(source) != "" {
				sum := sha256.Sum256([]byte(source))
				value = map[string]any{"$ref": "current_user_message", "sha256": hex.EncodeToString(sum[:]), "length": len([]byte(source))}
			} else if allowReference {
				if text, ok := value.(string); ok {
					if strings.TrimSpace(text) == "" {
						value = nil
					}
				} else if ref, ok := value.(map[string]any); ok && ref["$ref"] == "current_user_message" {
					if _, exists := ref["length"]; !exists {
						value = map[string]any{"$ref": "current_user_message", "length": 1}
					}
				} else {
					value = nil
				}
			} else {
				value = nil
			}
		} else {
			if ref, ok := value.(map[string]any); ok {
				if ref["$ref"] == "current_user_message" {
					value = source
				} else {
					reasons = append(reasons, StartupIssue{Code: "FLOW_INPUT_INVALID", Input: input.Key, Message: input.Label + " 引用无效"})
					continue
				}
			}
			converted, err := convertInput(input, value, projectRoot)
			if err != nil {
				reasons = append(reasons, StartupIssue{Code: "FLOW_INPUT_INVALID", Input: input.Key, Message: input.Label + "：" + err.Error()})
				continue
			}
			value = converted
		}
		if input.Required && !provided(value) {
			reasons = append(reasons, StartupIssue{Code: "FLOW_INPUT_REQUIRED", Input: input.Key, Message: "请提供" + input.Label})
		}
		resolved[input.Key] = value
	}
	for key := range raw {
		if !known[key] {
			reasons = append(reasons, StartupIssue{Code: "FLOW_INPUT_UNKNOWN", Input: key, Message: "流程没有声明输入：" + key})
		}
	}
	for _, constraint := range plan.InputConstraints {
		count := 0
		for _, key := range constraint.Inputs {
			if provided(resolved[key]) {
				count++
			}
		}
		valid := true
		switch constraint.Op {
		case "at_least_one":
			valid = count > 0
		case "exactly_one":
			valid = count == 1
		case "at_most_one":
			valid = count <= 1
		default:
			valid = false
		}
		if !valid {
			reasons = append(reasons, StartupIssue{Code: "FLOW_INPUT_CONSTRAINT", Message: "输入组合不满足 " + constraint.Op + "：" + strings.Join(constraint.Inputs, "、")})
		}
	}
	if len(reasons) > 0 {
		return nil, &InputError{Reasons: reasons}
	}
	return resolved, nil
}

func convertInput(input flow.InputPlan, value any, root string) (any, error) {
	if value == nil || value == "" {
		return nil, nil
	}
	switch input.Type {
	case "boolean":
		if v, ok := value.(bool); ok {
			return v, nil
		}
		switch fmt.Sprint(value) {
		case "是", "true", "1":
			return true, nil
		case "否", "false", "0":
			return false, nil
		}
		return nil, fmt.Errorf("需要是/否")
	case "number":
		number, err := strconv.ParseFloat(fmt.Sprint(value), 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("需要有效数字")
		}
		return number, nil
	case "text", "choice", "file", "folder", "layer", "":
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("需要文字或路径")
		}
		if input.Type != "text" && input.Type != "" {
			text = strings.TrimSpace(text)
		}
		if input.Type == "choice" && text != "" {
			found := input.Default != nil && text == fmt.Sprint(input.Default)
			for _, option := range input.Options {
				if text == option {
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("请选择列出的选项")
			}
		}
		if (input.Type == "file" || input.Type == "folder") && text != "" {
			path := text
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}
			info, err := os.Stat(path)
			if err != nil {
				return nil, fmt.Errorf("路径不存在：%s", text)
			}
			if input.Type == "file" && !info.Mode().IsRegular() {
				return nil, fmt.Errorf("需要普通文件")
			}
			if input.Type == "folder" && !info.IsDir() {
				return nil, fmt.Errorf("需要文件夹")
			}
			return filepath.Clean(path), nil
		}
		return text, nil
	default:
		return nil, fmt.Errorf("不支持的输入类型 %s", input.Type)
	}
}
func provided(value any) bool {
	if value == nil {
		return false
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case map[string]any:
		if v["$ref"] == "current_user_message" {
			n, _ := json.Marshal(v["length"])
			return string(n) != "0" && string(n) != "null"
		}
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map:
		return v.Len() > 0
	}
	return true
}
func truthy(value any) bool {
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
	return provided(value)
}

// InputProblem is shared by the editing form and the run entrypoint.
type InputProblem struct {
	Label   string `json:"label"`
	Level   string `json:"level"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ValidateInputs(plan *flow.FlowPlan, values map[string]any, projectRoot string) []InputProblem {
	_, problems := validateInputPlan(plan, values, projectRoot)
	return problems
}

// Return the exact checked resource snapshot to Start as well as the form's
// diagnostics, so starting a run does not resolve a different revision again.
func validateInputPlan(plan *flow.FlowPlan, values map[string]any, projectRoot string) (*flow.FlowPlan, []InputProblem) {
	problems := []InputProblem{}
	for _, diagnostic := range plan.CompileDiagnostics {
		if diagnostic.Code == "FLOW_DATA_INPUT_INVALID" {
			problems = append(problems, InputProblem{Level: "error", Code: diagnostic.Code, Message: diagnostic.Message})
		}
	}
	normalized, err := resolveInputs(plan, values, "", projectRoot, true)
	if err != nil {
		if invalid, ok := err.(*InputError); ok {
			for _, reason := range invalid.Reasons {
				problems = append(problems, InputProblem{Label: reason.Input, Level: "error", Code: reason.Code, Message: reason.Message})
			}
		}
		return plan, problems
	}
	linked := flow.ResolveInputResources(plan, normalized)
	for _, diagnostic := range linked.LinkDiagnostics {
		if diagnostic.Code == "FLOW_RESOURCE_CHECK_FAILED" {
			problems = append(problems, InputProblem{Level: diagnostic.Level, Code: diagnostic.Code, Message: diagnostic.Message})
		}
	}
	for _, r := range linked.Resources {
		if r.BaseDirFromInput == "" {
			continue
		}
		for _, problem := range r.Problems {
			if problem.Level == "error" || problem.Level == "warning" {
				problems = append(problems, InputProblem{Label: r.BaseDirFromInput, Level: problem.Level, Code: problem.Code, Message: problem.Message})
			}
		}
	}
	return linked, problems
}
