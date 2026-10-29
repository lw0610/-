package plan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/flow"
)

var outputTypes = []string{"file", "layers", "layout", "text", "dataset", "review_initial", "review_final", "confirmation"}

// ValidationError uses the existing tool-error contract. The model receives
// structured diagnostics and current constraints, not JSON embedded in a string.
type ValidationError struct {
	Diagnostics         []flow.Diagnostic `json:"diagnostics"`
	AllowedCapabilities []string          `json:"allowed_capabilities,omitempty"`
}

func NewValidationError(diagnostics []flow.Diagnostic, env Environment) *ValidationError {
	allowed := []string{}
	for id, available := range env.Capabilities {
		if available {
			allowed = append(allowed, id)
		}
	}
	sort.Strings(allowed)
	return &ValidationError{Diagnostics: diagnostics, AllowedCapabilities: allowed}
}

func (e *ValidationError) Error() string {
	count, first := 0, ""
	for _, diagnostic := range e.Diagnostics {
		if diagnostic.Level == "error" {
			count++
			if first == "" {
				first = diagnostic.Message
			}
		}
	}
	return fmt.Sprintf("计划校验未通过（%d 项）；%s", count, first)
}

func (e *ValidationError) ToolErrorCode() string { return "PLAN_VALIDATION_FAILED" }
func (e *ValidationError) ToolRetryable() bool   { return true }
func (e *ValidationError) ToolProviderDiagnostics() map[string]any {
	return map[string]any{
		"diagnostics":          e.Diagnostics,
		"next_action":          "correct_plan",
		"allowed_capabilities": append([]string{}, e.AllowedCapabilities...),
		"constraints": map[string]any{
			"inputs":  "引用 data.id 或前序步骤 produces.id；输入写步骤编号时会展开其产出。",
			"sources": "user(ref为附件、文件、消息原文或会话文件ID)、project、builtin、retrieve(need与extent)。",
			"extent":  "extent 使用 region、bbox 或 from:前序数据ID + expand:0.2。",
			"repair":  "只修正所列语义问题；系统自动保存已完成的步骤和产出。无需data_find/find_id或重复抽取。",
		},
	}
}

func outputSummary(outputs []flow.OutputDeclaration) string {
	values := make([]string, 0, len(outputs))
	for _, output := range outputs {
		values = append(values, fmt.Sprintf("%s(type=%s,format=%s)", output.Label, output.Type, output.Format))
	}
	if len(values) == 0 {
		return "无"
	}
	return strings.Join(values, "；")
}

func declaredOutputs(p *Plan) string {
	var outputs []flow.OutputDeclaration
	for _, step := range p.Steps {
		outputs = append(outputs, step.Outputs...)
	}
	return outputSummary(outputs)
}
