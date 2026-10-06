package hitl

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
)

const requestToolName = "hitl_request"

const (
	selectModeSingle   = "single"
	selectModeMultiple = "multiple"
	selectModeConfirm  = "confirm"
	selectModeText     = "text"
)

// RequestOption 供前端渲染按钮的选项。
type RequestOption struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Reason string `json:"reason,omitempty"`
}

// DecisionItem 批量决策中的单项内容。
type DecisionItem struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	SelectMode  string          `json:"select_mode,omitempty"`
	Options     []RequestOption `json:"options,omitempty"`
	Default     string          `json:"default,omitempty"`
	Defaults    []string        `json:"defaults,omitempty"`
	Required    bool            `json:"required,omitempty"`
	Category    string          `json:"category,omitempty"`
}

// RequestPlanStep 计划确认中的单步信息。
type RequestPlanStep struct {
	Agent  string `json:"agent,omitempty"`
	Action string `json:"action,omitempty"`
	Impact string `json:"impact,omitempty"`
}

// RequestPlan 计划确认内容。
type RequestPlan struct {
	Objective string            `json:"objective,omitempty"`
	Steps     []RequestPlanStep `json:"steps,omitempty"`
	Risks     []string          `json:"risks,omitempty"`
	Fallback  string            `json:"fallback,omitempty"`
}

// InterruptPayload 中断卡片结构。
type InterruptPayload struct {
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Options     []RequestOption `json:"options,omitempty"`
	Default     string          `json:"default,omitempty"`
	Defaults    []string        `json:"defaults,omitempty"`
	Plan        *RequestPlan    `json:"plan,omitempty"`
	SelectMode  string          `json:"select_mode,omitempty"`
	Decisions   []DecisionItem  `json:"decisions,omitempty"`
	Required    bool            `json:"required,omitempty"`
	Category    string          `json:"category,omitempty"`
}

// RequestInput hitl_request 输入参数。
type RequestInput struct {
	Type        string          `json:"type,omitempty"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Options     []RequestOption `json:"options,omitempty"`
	Default     string          `json:"default,omitempty"`
	Defaults    []string        `json:"defaults,omitempty"`
	Plan        *RequestPlan    `json:"plan,omitempty"`
	SelectMode  string          `json:"select_mode,omitempty"`
	Decisions   []DecisionItem  `json:"decisions,omitempty"`
	Required    bool            `json:"required,omitempty"`
	Category    string          `json:"category,omitempty"`
}

// DecisionResult 结构化决策结果。
type DecisionResult struct {
	Selected  []string `json:"selected,omitempty"`
	Text      string   `json:"text,omitempty"`
	Note      string   `json:"note,omitempty"`
	Confirmed *bool    `json:"confirmed,omitempty"`
}

// RequestOutput hitl_request 恢复后的输出。
type RequestOutput struct {
	Approved  bool                      `json:"approved"`
	Type      string                    `json:"type,omitempty"`
	UserData  any                       `json:"user_data,omitempty"`
	Decisions map[string]DecisionResult `json:"decisions,omitempty"`
}

func init() {
	gob.Register(InterruptPayload{})
	gob.Register(DecisionItem{})
	gob.Register(DecisionResult{})
	gob.Register(RequestPlan{})
	gob.Register(RequestPlanStep{})
	gob.Register([]RequestOption{})
	gob.Register([]DecisionItem{})
	gob.Register(map[string]DecisionResult{})
	gob.Register([]RequestPlanStep{})
	gob.Register([]string{})
}

// NewRequestTool 创建主动 HITL 请求工具。
func NewRequestTool() (einotool.InvokableTool, error) {
	return toolutils.InferTool[RequestInput, RequestOutput](
		requestToolName,
		"主动发起人工确认中断；恢复后返回用户选择结果",
		runRequestTool,
	)
}

func Invoke(ctx context.Context, args map[string]any) (RequestOutput, error) {
	// Keep this internal entry point equivalent to the typed native tool,
	// including nested plan steps, options and batch decision fields.
	data, err := json.Marshal(args)
	if err != nil {
		return RequestOutput{}, fmt.Errorf("encode HITL request: %w", err)
	}
	var input RequestInput
	if err := json.Unmarshal(data, &input); err != nil {
		return RequestOutput{}, fmt.Errorf("decode HITL request: %w", err)
	}
	return runRequestTool(ctx, input)
}

func runRequestTool(ctx context.Context, input RequestInput) (RequestOutput, error) {
	info := normalizeRequestInfo(input)

	wasInterrupted, _, _ := einotool.GetInterruptState[any](ctx)
	if !wasInterrupted {
		return RequestOutput{}, einotool.Interrupt(ctx, info)
	}

	isResumeTarget, hasData, data := einotool.GetResumeContext[any](ctx)
	if !isResumeTarget {
		// 当前不是恢复目标，继续保持中断状态。
		return RequestOutput{}, einotool.Interrupt(ctx, info)
	}

	out := RequestOutput{
		Approved: true,
		Type:     normalizeInterruptType(input.Type),
	}
	if hasData {
		out.UserData = data
		out.Decisions = extractDecisionResults(data)
	}
	return out, nil
}

func normalizeRequestInfo(input RequestInput) InterruptPayload {
	interruptType := normalizeInterruptType(input.Type)
	title := strings.TrimSpace(input.Title)
	if title == "" {
		switch interruptType {
		case "clarification":
			title = "需要澄清您的意图"
		case "decision":
			title = "需要您参与决策"
		default:
			title = "需要您的确认"
		}
	}

	description := strings.TrimSpace(input.Description)
	if description == "" {
		switch interruptType {
		case "clarification":
			description = "检测到多个可行方案，请先确认后继续。"
		case "decision":
			description = "规划过程中检测到关键分歧点，请先完成决策后继续。"
		default:
			description = "该步骤涉及关键决策，请确认后继续。"
		}
	}

	info := InterruptPayload{
		Type:        interruptType,
		Title:       title,
		Description: description,
		SelectMode:  normalizeSelectMode(input.SelectMode, interruptType),
	}
	if len(input.Options) > 0 {
		info.Options = normalizeRequestOptions(input.Options)
	}
	if normalizedDefault := strings.TrimSpace(input.Default); normalizedDefault != "" {
		info.Default = normalizedDefault
	}
	if defaults := normalizeStringSlice(input.Defaults); len(defaults) > 0 {
		info.Defaults = defaults
	}
	if input.Plan != nil {
		info.Plan = input.Plan
	}
	if decisions := normalizeDecisionItems(input.Decisions); len(decisions) > 0 {
		info.Decisions = decisions
	}
	if input.Required {
		info.Required = true
	}
	if category := strings.TrimSpace(input.Category); category != "" {
		info.Category = category
	}
	return info
}

func normalizeRequestOptions(options []RequestOption) []RequestOption {
	result := make([]RequestOption, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option.Value)
		label := strings.TrimSpace(option.Label)
		reason := strings.TrimSpace(option.Reason)
		if value == "" {
			value = label
		}
		if value == "" {
			continue
		}
		if label == "" {
			label = value
		}
		result = append(result, RequestOption{
			Label:  label,
			Value:  value,
			Reason: reason,
		})
	}
	return result
}

func normalizeSelectMode(raw string, interruptType string) string {
	mode := normalizeSelectModeValue(raw)
	if mode != "" {
		return mode
	}

	switch interruptType {
	case "clarification":
		return selectModeSingle
	case "plan_confirmation":
		return selectModeConfirm
	case "decision":
		return selectModeSingle
	default:
		return selectModeSingle
	}
}

func normalizeSelectModeValue(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case selectModeSingle:
		return selectModeSingle
	case selectModeMultiple:
		return selectModeMultiple
	case selectModeConfirm:
		return selectModeConfirm
	case selectModeText:
		return selectModeText
	default:
		return ""
	}
}

func normalizeDecisionItems(items []DecisionItem) []DecisionItem {
	result := make([]DecisionItem, 0, len(items))
	for idx, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = fmt.Sprintf("decision_%d", idx+1)
		}

		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = fmt.Sprintf("决策项 %d", idx+1)
		}

		mode := normalizeSelectModeValue(item.SelectMode)
		if mode == "" {
			mode = selectModeSingle
		}

		normalized := DecisionItem{
			ID:          id,
			Title:       title,
			Description: strings.TrimSpace(item.Description),
			SelectMode:  mode,
			Required:    item.Required,
			Category:    strings.TrimSpace(item.Category),
		}

		if mode != selectModeText && mode != selectModeConfirm {
			normalized.Options = normalizeRequestOptions(item.Options)
		}

		if def := strings.TrimSpace(item.Default); def != "" {
			normalized.Default = def
		}
		if defs := normalizeStringSlice(item.Defaults); len(defs) > 0 {
			normalized.Defaults = defs
		}

		if mode == selectModeMultiple && len(normalized.Defaults) == 0 && normalized.Default != "" {
			normalized.Defaults = []string{normalized.Default}
		}

		result = append(result, normalized)
	}
	return result
}

func normalizeStringSlice(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	result := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func extractDecisionResults(raw any) map[string]DecisionResult {
	if raw == nil {
		return nil
	}

	if typed, ok := raw.(map[string]DecisionResult); ok {
		if result := normalizeDecisionResultsMap(typed, true); len(result) > 0 {
			return result
		}
	}

	record, ok := raw.(map[string]any)
	if !ok {
		return nil
	}

	if nested, ok := record["decisions"]; ok {
		if result := normalizeDecisionResultsMap(nested, true); len(result) > 0 {
			return result
		}
	}

	return normalizeDecisionResultsMap(record, false)
}

func normalizeDecisionResultsMap(raw any, allowPrimitive bool) map[string]DecisionResult {
	if raw == nil {
		return nil
	}

	if typed, ok := raw.(map[string]DecisionResult); ok {
		if len(typed) == 0 {
			return nil
		}
		result := make(map[string]DecisionResult, len(typed))
		for id, value := range typed {
			trimmedID := strings.TrimSpace(id)
			if trimmedID == "" {
				continue
			}
			result[trimmedID] = value
		}
		if len(result) == 0 {
			return nil
		}
		return result
	}

	record, ok := raw.(map[string]any)
	if !ok {
		return nil
	}

	result := make(map[string]DecisionResult)
	for rawID, value := range record {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		parsed, ok := parseDecisionResult(value, allowPrimitive)
		if !ok {
			continue
		}
		result[id] = parsed
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func parseDecisionResult(raw any, allowPrimitive bool) (DecisionResult, bool) {
	if raw == nil {
		return DecisionResult{}, false
	}

	record, ok := raw.(map[string]any)
	if ok {
		result := DecisionResult{}
		if selected := normalizeAnyStringSlice(record["selected"]); len(selected) > 0 {
			result.Selected = selected
		} else if selected := normalizeAnyStringSlice(record["options"]); len(selected) > 0 {
			result.Selected = selected
		} else if option := strings.TrimSpace(toString(record["option"])); option != "" {
			result.Selected = []string{option}
		}

		if text := strings.TrimSpace(toString(firstNonNil(record["text"], record["input"]))); text != "" {
			result.Text = text
		}
		if note := strings.TrimSpace(toString(record["note"])); note != "" {
			result.Note = note
		}

		if confirmed, ok := record["confirmed"].(bool); ok {
			result.Confirmed = &confirmed
		}

		if len(result.Selected) == 0 && result.Text == "" && result.Note == "" && result.Confirmed == nil {
			return DecisionResult{}, false
		}
		return result, true
	}

	if !allowPrimitive {
		return DecisionResult{}, false
	}

	switch value := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return DecisionResult{}, false
		}
		return DecisionResult{Selected: []string{trimmed}}, true
	case []string:
		selected := normalizeAnyStringSlice(value)
		if len(selected) == 0 {
			return DecisionResult{}, false
		}
		return DecisionResult{Selected: selected}, true
	case []any:
		selected := normalizeAnyStringSlice(value)
		if len(selected) == 0 {
			return DecisionResult{}, false
		}
		return DecisionResult{Selected: selected}, true
	case bool:
		confirmed := value
		return DecisionResult{Confirmed: &confirmed}, true
	default:
		return DecisionResult{}, false
	}
}

func normalizeAnyStringSlice(raw any) []string {
	if raw == nil {
		return nil
	}

	values := make([]string, 0)
	switch input := raw.(type) {
	case string:
		if trimmed := strings.TrimSpace(input); trimmed != "" {
			values = append(values, trimmed)
		}
	case []string:
		values = append(values, input...)
	case []any:
		for _, item := range input {
			if text := strings.TrimSpace(toString(item)); text != "" {
				values = append(values, text)
			}
		}
	}

	if len(values) == 0 {
		return nil
	}
	return normalizeStringSlice(values)
}

func toString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprintf("%v", value)
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func normalizeInterruptType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "clarification":
		return "clarification"
	case "decision":
		return "decision"
	default:
		return "plan_confirmation"
	}
}
