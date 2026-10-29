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
			"capabilities": "steps[].capabilities 使用当前工具包 ID；具体工具调用由执行智能体决定。",
			"session_data": "已有会话成果使用 from=session，并提供 file_id 或工具返回的精确绝对 path；name/layer_name 仅作显示，不参与查找。整体数据集目录用 kind=dataset、不填 geometry；单文件用 kind=file，geometry 可为 table/point/line/polygon/raster。from=step 引用同一计划内的前序步骤产出；新计划引用已有成果时，不要增加重复生产步骤。",
			"output_types": append([]string{}, outputTypes...),
			"uses":         "steps[].uses 使用 data[].id，不使用显示名；前序 acquire 步骤 ID 可展开为其 data ID，前序单产出步骤 ID 可绑定唯一产出。多产出必须声明 from=step、step 和 output 后使用该 data.id，不能猜测。depends_on 使用前序步骤 ID。",
			"outputs":      "每个步骤的 outputs[].label 非空且在本步骤内唯一。from=step 数据用 step 指定生产步骤、output 选择其产出 label，name 仅作显示。省略 output 时兼容旧 name 匹配或唯一产出；显式 output 必须正确。deliverables 的 label/type/format 对应同一已声明步骤产出。",
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
