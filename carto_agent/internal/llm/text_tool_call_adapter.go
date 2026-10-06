package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// TextToolCallModelWrapper lets local models use tools even when they return
// tool requests as plain JSON text instead of native tool_calls.
type TextToolCallModelWrapper struct {
	underlying            model.ToolCallingChatModel
	toolInfos             []*schema.ToolInfo
	toolNames             map[string]string
	toolSummaries         []textToolSummary
	toolContracts         map[string]textToolContract
	toolRelevance         map[string]int
	topRelevantToolScore  int
	relevantToolSummaries []textToolSummary
}

var _ model.ToolCallingChatModel = (*TextToolCallModelWrapper)(nil)

func NewTextToolCallModelWrapper(underlying model.ToolCallingChatModel) *TextToolCallModelWrapper {
	return &TextToolCallModelWrapper{underlying: underlying}
}

func (w *TextToolCallModelWrapper) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	effective := w.withToolsFromOptions(opts).withToolRelevance(input)
	resp, err := w.underlying.Generate(ctx, effective.withTextToolCallInstruction(input), opts...)
	if err != nil || resp == nil || len(effective.toolNames) == 0 {
		return resp, err
	}

	if toolCalls, ok, problems := effective.normalizeToolCalls(resp.ToolCalls); len(resp.ToolCalls) > 0 {
		if ok {
			converted := *resp
			converted.ToolCalls = toolCalls
			return &converted, nil
		}
		if repaired, repairErr := effective.retryAfterInvalidToolOutput(ctx, input, opts, problems); repairErr == nil && repaired != nil {
			return repaired, nil
		}
		return resp, nil
	}

	toolCalls, ok := parseTextToolCalls(resp.Content, effective.toolNames)
	if !ok {
		if shouldRetryTextToolCall(resp.Content) {
			if repaired, repairErr := effective.retryAfterInvalidToolOutput(ctx, input, opts, []string{"上一次回复没有输出可执行的工具调用 JSON"}); repairErr == nil && repaired != nil {
				return repaired, nil
			}
		}
		return resp, nil
	}
	if normalized, valid, problems := effective.normalizeToolCalls(toolCalls); valid {
		toolCalls = normalized
	} else if repaired, repairErr := effective.retryAfterInvalidToolOutput(ctx, input, opts, problems); repairErr == nil && repaired != nil {
		return repaired, nil
	} else {
		return resp, nil
	}

	converted := *resp
	converted.Content = ""
	converted.ToolCalls = toolCalls
	return &converted, nil
}

func (w *TextToolCallModelWrapper) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	resp, err := w.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{resp}), nil
}

func (w *TextToolCallModelWrapper) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	newUnderlying, err := w.underlying.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &TextToolCallModelWrapper{
		underlying:    newUnderlying,
		toolInfos:     tools,
		toolNames:     buildToolNameLookup(tools),
		toolSummaries: buildToolSummaries(tools),
		toolContracts: buildToolContracts(tools),
	}, nil
}

func (w *TextToolCallModelWrapper) withToolsFromOptions(opts []model.Option) *TextToolCallModelWrapper {
	commonOpts := model.GetCommonOptions(&model.Options{}, opts...)
	if len(commonOpts.Tools) == 0 {
		return w
	}
	return &TextToolCallModelWrapper{
		underlying:    w.underlying,
		toolInfos:     commonOpts.Tools,
		toolNames:     buildToolNameLookup(commonOpts.Tools),
		toolSummaries: buildToolSummaries(commonOpts.Tools),
		toolContracts: buildToolContracts(commonOpts.Tools),
	}
}

func (w *TextToolCallModelWrapper) withToolRelevance(input []*schema.Message) *TextToolCallModelWrapper {
	if len(w.toolInfos) == 0 {
		return w
	}
	query := latestUserText(input)
	if strings.TrimSpace(query) == "" {
		return w
	}
	relevance := make(map[string]int, len(w.toolInfos))
	type scoredTool struct {
		info  *schema.ToolInfo
		score int
	}
	scored := make([]scoredTool, 0, len(w.toolInfos))
	for _, item := range w.toolInfos {
		if item == nil {
			continue
		}
		score := scoreToolRelevance(query, item)
		relevance[item.Name] = score
		scored = append(scored, scoredTool{info: item, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].info.Name < scored[j].info.Name
	})

	topScore := 0
	if len(scored) > 0 {
		topScore = scored[0].score
	}
	relevantSummaries := make([]textToolSummary, 0, 5)
	for _, item := range scored {
		if item.score <= 0 {
			continue
		}
		relevantSummaries = append(relevantSummaries, textToolSummary{
			Name: item.info.Name,
			Desc: compactToolDescription(item.info.Desc),
		})
		if len(relevantSummaries) >= 5 {
			break
		}
	}

	next := *w
	next.toolRelevance = relevance
	next.topRelevantToolScore = topScore
	next.relevantToolSummaries = relevantSummaries
	return &next
}

func (w *TextToolCallModelWrapper) withTextToolCallInstruction(input []*schema.Message) []*schema.Message {
	if len(w.toolNames) == 0 {
		return input
	}

	instruction := schema.SystemMessage(
		"[本地模型工具调用兼容]\n" +
			"下面列出的是当前真实可调用工具，不只是 skill 名称。只要工具列表中存在能完成用户请求的工具，就必须输出工具调用 JSON，不要回答“没有可用技能”。\n" +
			"简单的工程状态、图层状态、布局状态读取任务，优先直接调用对应状态工具，不要先转交 task 子智能体。\n" +
			"调用 task 时 arguments 只使用 subagent_type 和 description；完整子任务内容必须全部写入 description，不要使用 prompt 字段。\n" +
			"当需要执行工具时，只输出一个 JSON 对象，不要输出解释、Markdown、AUTOCARTO 伪代码或自然语言。\n" +
			"单次工具调用格式：{\"tool\":\"工具名\",\"arguments\":{...}}\n" +
			"多步工具调用格式：{\"steps\":[{\"tool\":\"工具名\",\"arguments\":{...}}]}\n" +
			"工具名必须从以下列表选择：\n" + renderToolSummaries(w.toolNames, w.toolSummaries) +
			renderRelevantToolHint(w.relevantToolSummaries),
	)

	out := make([]*schema.Message, 0, len(input)+1)
	out = append(out, instruction)
	out = append(out, input...)
	return out
}

type textToolSummary struct {
	Name string
	Desc string
}

type textToolContract struct {
	Name     string
	Required []string
}

func buildToolNameLookup(tools []*schema.ToolInfo) map[string]string {
	if len(tools) == 0 {
		return nil
	}
	names := make(map[string]string, len(tools))
	for _, item := range tools {
		if item == nil {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		names[name] = name
		names[strings.ToLower(name)] = name
	}
	return names
}

func sortedToolNames(toolNames map[string]string) []string {
	seen := make(map[string]struct{}, len(toolNames))
	names := make([]string, 0, len(toolNames))
	for _, canonical := range toolNames {
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		names = append(names, canonical)
	}
	sort.Strings(names)
	return names
}

func buildToolSummaries(tools []*schema.ToolInfo) []textToolSummary {
	if len(tools) == 0 {
		return nil
	}
	summaries := make([]textToolSummary, 0, len(tools))
	seen := make(map[string]struct{}, len(tools))
	for _, item := range tools {
		if item == nil {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		summaries = append(summaries, textToolSummary{
			Name: name,
			Desc: compactToolDescription(item.Desc),
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	return summaries
}

func buildToolContracts(tools []*schema.ToolInfo) map[string]textToolContract {
	if len(tools) == 0 {
		return nil
	}
	contracts := make(map[string]textToolContract, len(tools))
	for _, item := range tools {
		if item == nil {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		contract := textToolContract{Name: name}
		if js, err := item.ToJSONSchema(); err == nil && js != nil && len(js.Required) > 0 {
			contract.Required = append(contract.Required, js.Required...)
		}
		// The upstream deep-agent task tool currently declares these fields in
		// its schema without marking them required, although its executor cannot
		// run without either value. Enforce the real contract here so malformed
		// model output is repaired before it reaches the graph ToolNode.
		if name == "task" {
			contract.Required = appendUniqueStrings(contract.Required, "description", "subagent_type")
		}
		sort.Strings(contract.Required)
		contracts[name] = contract
	}
	return contracts
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func renderToolSummaries(toolNames map[string]string, summaries []textToolSummary) string {
	if len(summaries) == 0 {
		return strings.Join(sortedToolNames(toolNames), ", ")
	}
	lines := make([]string, 0, len(summaries))
	for _, item := range summaries {
		if item.Desc == "" {
			lines = append(lines, "- "+item.Name)
			continue
		}
		lines = append(lines, "- "+item.Name+": "+item.Desc)
	}
	return strings.Join(lines, "\n")
}

func renderRelevantToolHint(summaries []textToolSummary) string {
	if len(summaries) == 0 {
		return ""
	}
	return "\n\n根据当前用户请求，优先考虑这些更相关的工具：\n" + renderToolSummaries(nil, summaries)
}

func compactToolDescription(desc string) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(desc)), " ")
	const limit = 120
	if len([]rune(trimmed)) <= limit {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:limit]) + "..."
}

func (w *TextToolCallModelWrapper) retryAfterInvalidToolOutput(ctx context.Context, input []*schema.Message, opts []model.Option, problems []string) (*schema.Message, error) {
	if len(w.toolNames) == 0 {
		return nil, nil
	}
	retryInput := w.withToolCallRepairInstruction(input, problems)
	resp, err := w.underlying.Generate(ctx, retryInput, opts...)
	if err != nil || resp == nil {
		return resp, err
	}
	if toolCalls, ok, _ := w.normalizeToolCalls(resp.ToolCalls); len(resp.ToolCalls) > 0 && ok {
		converted := *resp
		converted.ToolCalls = toolCalls
		return &converted, nil
	}
	toolCalls, ok := parseTextToolCalls(resp.Content, w.toolNames)
	if !ok {
		return resp, nil
	}
	if normalized, valid, _ := w.normalizeToolCalls(toolCalls); valid {
		toolCalls = normalized
	} else {
		return resp, nil
	}
	converted := *resp
	converted.Content = ""
	converted.ToolCalls = toolCalls
	return &converted, nil
}

func (w *TextToolCallModelWrapper) withToolCallRepairInstruction(input []*schema.Message, problems []string) []*schema.Message {
	repairText := "[工具调用修复]\n" +
		"上一次输出无法被工具执行器接收，请重新输出一个可执行工具调用 JSON。\n" +
		"不要解释，不要输出思考过程，不要输出 Markdown。\n" +
		"工具名必须来自工具列表；arguments 必须是 JSON object，并且必须包含工具 schema 要求的必填字段。"
	if len(problems) > 0 {
		repairText += "\n上一次的问题：\n- " + strings.Join(problems, "\n- ")
	}
	out := make([]*schema.Message, 0, len(input)+2)
	out = append(out, schema.SystemMessage(
		"[本地模型工具调用兼容]\n"+
			"下面列出的是当前真实可调用工具，不只是 skill 名称。只要工具列表中存在能完成用户请求的工具，就必须输出工具调用 JSON，不要回答“没有可用技能”。\n"+
			"简单的工程状态、图层状态、布局状态读取任务，优先直接调用对应状态工具，不要先转交 task 子智能体。\n"+
			"调用 task 时 arguments 只使用 subagent_type 和 description；完整子任务内容必须全部写入 description，不要使用 prompt 字段。\n"+
			"当需要执行工具时，只输出一个 JSON 对象，不要输出解释、Markdown、AUTOCARTO 伪代码或自然语言。\n"+
			"单次工具调用格式：{\"tool\":\"工具名\",\"arguments\":{...}}\n"+
			"多步工具调用格式：{\"steps\":[{\"tool\":\"工具名\",\"arguments\":{...}}]}\n"+
			"工具名必须从以下列表选择：\n"+renderToolSummaries(w.toolNames, w.toolSummaries)+
			renderRelevantToolHint(w.relevantToolSummaries),
	))
	out = append(out, schema.SystemMessage(repairText))
	out = append(out, input...)
	return out
}

func (w *TextToolCallModelWrapper) normalizeToolCalls(calls []schema.ToolCall) ([]schema.ToolCall, bool, []string) {
	if len(calls) == 0 {
		return nil, false, nil
	}
	normalized := make([]schema.ToolCall, 0, len(calls))
	var problems []string
	for idx, call := range calls {
		name, ok := resolveToolName(call.Function.Name, w.toolNames)
		if !ok {
			problems = append(problems, fmt.Sprintf("第 %d 个工具调用的工具名 %q 不在可用工具列表中", idx+1, call.Function.Name))
			continue
		}
		arguments, ok := normalizeToolArguments(json.RawMessage(call.Function.Arguments))
		if !ok {
			problems = append(problems, fmt.Sprintf("工具 %s 的 arguments 不是 JSON object", name))
			continue
		}
		arguments, ok = normalizeKnownToolArguments(name, arguments)
		if !ok {
			problems = append(problems, fmt.Sprintf("工具 %s 的 arguments 无法标准化", name))
			continue
		}
		if problem := w.validateToolArguments(name, arguments); problem != "" {
			problems = append(problems, problem)
			continue
		}
		next := call
		if strings.TrimSpace(next.ID) == "" {
			next.ID = fmt.Sprintf("tool_call_%d", idx+1)
		}
		if strings.TrimSpace(next.Type) == "" {
			next.Type = "function"
		}
		next.Function.Name = name
		next.Function.Arguments = arguments
		normalized = append(normalized, next)
	}
	return normalized, len(problems) == 0 && len(normalized) == len(calls), problems
}

func normalizeKnownToolArguments(name string, arguments string) (string, bool) {
	if name != "task" {
		return arguments, true
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return "", false
	}
	promptValue, hasPrompt := payload["prompt"]
	if !hasPrompt {
		return arguments, true
	}
	prompt, ok := promptValue.(string)
	if !ok {
		return "", false
	}
	prompt = strings.TrimSpace(prompt)
	description, _ := payload["description"].(string)
	description = strings.TrimSpace(description)
	if prompt != "" {
		switch {
		case description == "":
			description = prompt
		case strings.Contains(description, prompt):
		case strings.Contains(prompt, description):
			description = prompt
		default:
			description += "\n\n" + prompt
		}
		payload["description"] = description
	}
	delete(payload, "prompt")
	normalized, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	return string(normalized), true
}

func (w *TextToolCallModelWrapper) validateToolArguments(name string, arguments string) string {
	if problem := w.validateToolRelevance(name); problem != "" {
		return problem
	}
	contract, ok := w.toolContracts[name]
	if !ok || len(contract.Required) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return fmt.Sprintf("工具 %s 的 arguments 不是有效 JSON object", name)
	}
	var missing []string
	for _, key := range contract.Required {
		value, exists := payload[key]
		if !exists || value == nil {
			missing = append(missing, key)
			continue
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("工具 %s 缺少必填参数: %s", name, strings.Join(missing, ", "))
}

func (w *TextToolCallModelWrapper) validateToolRelevance(name string) string {
	if len(w.toolRelevance) == 0 || w.topRelevantToolScore < 4 {
		return ""
	}
	if name == "task" || name == "skill" {
		return ""
	}
	score := w.toolRelevance[name]
	if score > 0 && score*3 >= w.topRelevantToolScore*2 {
		return ""
	}
	candidates := topRelevantToolNames(w.toolRelevance, name, 4)
	if len(candidates) == 0 {
		return ""
	}
	return fmt.Sprintf("工具 %s 与当前用户请求相关性较低；更相关的候选工具: %s", name, strings.Join(candidates, ", "))
}

func latestUserText(input []*schema.Message) string {
	for i := len(input) - 1; i >= 0; i-- {
		if input[i] != nil && input[i].Role == schema.User {
			return input[i].Content
		}
	}
	return ""
}

func scoreToolRelevance(query string, tool *schema.ToolInfo) int {
	if tool == nil {
		return 0
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 0
	}
	haystack := strings.ToLower(strings.Join([]string{tool.Name, tool.Desc}, " "))
	score := 0
	for _, token := range asciiTokens(query) {
		if strings.Contains(haystack, token) {
			score += 2
		}
	}
	for _, token := range asciiTokens(haystack) {
		if strings.Contains(query, token) {
			score += 2
		}
	}
	for _, gram := range cjkBigrams(query) {
		if strings.Contains(haystack, gram) {
			score += 3
		}
	}
	for _, gram := range cjkBigrams(haystack) {
		if strings.Contains(query, gram) {
			score += 3
		}
	}
	return score
}

func asciiTokens(text string) []string {
	var tokens []string
	var buf strings.Builder
	flush := func() {
		if buf.Len() < 2 {
			buf.Reset()
			return
		}
		tokens = append(tokens, buf.String())
		buf.Reset()
	}
	for _, r := range text {
		switch {
		case r >= 'a' && r <= 'z':
			buf.WriteRune(r)
		case r >= '0' && r <= '9':
			buf.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return uniqueStrings(tokens)
}

func cjkBigrams(text string) []string {
	var cjk []rune
	for _, r := range text {
		if isCJK(r) {
			cjk = append(cjk, r)
		}
	}
	if len(cjk) < 2 {
		return nil
	}
	grams := make([]string, 0, len(cjk)-1)
	for i := 0; i < len(cjk)-1; i++ {
		grams = append(grams, string(cjk[i:i+2]))
	}
	return uniqueStrings(grams)
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0xF900 && r <= 0xFAFF)
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func topRelevantToolNames(relevance map[string]int, excluded string, limit int) []string {
	type item struct {
		name  string
		score int
	}
	items := make([]item, 0, len(relevance))
	for name, score := range relevance {
		if name == excluded || score <= 0 {
			continue
		}
		items = append(items, item{name: name, score: score})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].name < items[j].name
	})
	if len(items) > limit {
		items = items[:limit]
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.name)
	}
	return names
}

func shouldRetryTextToolCall(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return false
	}
	needles := []string{
		"没有可用的技能",
		"没有可用技能",
		"没有相关的功能",
		"没有与",
		"no available tool",
		"no available skill",
		"cannot find a tool",
	}
	for _, needle := range needles {
		if strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

type textToolPlan struct {
	Tool      string             `json:"tool"`
	Name      string             `json:"name"`
	Arguments json.RawMessage    `json:"arguments"`
	Args      json.RawMessage    `json:"args"`
	Steps     []textToolCallSpec `json:"steps"`
	ToolCalls []textToolCallSpec `json:"tool_calls"`
}

type textToolCallSpec struct {
	ID        string                `json:"id"`
	Type      string                `json:"type"`
	Tool      string                `json:"tool"`
	Name      string                `json:"name"`
	Arguments json.RawMessage       `json:"arguments"`
	Args      json.RawMessage       `json:"args"`
	Function  *textFunctionCallSpec `json:"function"`
}

type textFunctionCallSpec struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func parseTextToolCalls(content string, toolNames map[string]string) ([]schema.ToolCall, bool) {
	if strings.TrimSpace(content) == "" || len(toolNames) == 0 {
		return nil, false
	}

	for _, candidate := range extractJSONValueCandidates(content) {
		calls, ok := parseTextToolCallsCandidate(candidate, toolNames)
		if ok {
			return calls, true
		}
	}
	return nil, false
}

func parseTextToolCallsCandidate(candidate string, toolNames map[string]string) ([]schema.ToolCall, bool) {
	var plan textToolPlan
	if err := json.Unmarshal([]byte(candidate), &plan); err == nil {
		if calls, ok := toolCallsFromPlan(plan, toolNames); ok {
			return calls, true
		}
	}

	var specs []textToolCallSpec
	if err := json.Unmarshal([]byte(candidate), &specs); err != nil {
		return nil, false
	}
	return toolCallsFromSpecs(specs, toolNames)
}

func toolCallsFromPlan(plan textToolPlan, toolNames map[string]string) ([]schema.ToolCall, bool) {
	switch {
	case len(plan.ToolCalls) > 0:
		return toolCallsFromSpecs(plan.ToolCalls, toolNames)
	case len(plan.Steps) > 0:
		return toolCallsFromSpecs(plan.Steps, toolNames)
	case strings.TrimSpace(plan.Tool) != "" || strings.TrimSpace(plan.Name) != "":
		return toolCallsFromSpecs([]textToolCallSpec{{
			Tool:      plan.Tool,
			Name:      plan.Name,
			Arguments: plan.Arguments,
			Args:      plan.Args,
		}}, toolNames)
	default:
		return nil, false
	}
}

func toolCallsFromSpecs(specs []textToolCallSpec, toolNames map[string]string) ([]schema.ToolCall, bool) {
	if len(specs) == 0 {
		return nil, false
	}

	calls := make([]schema.ToolCall, 0, len(specs))
	for idx, spec := range specs {
		name := firstNonEmpty(spec.Tool, spec.Name)
		args := firstRawMessage(spec.Arguments, spec.Args)
		if spec.Function != nil {
			name = firstNonEmpty(spec.Function.Name, name)
			args = firstRawMessage(spec.Function.Arguments, args)
		}

		canonical, ok := resolveToolName(name, toolNames)
		if !ok {
			return nil, false
		}
		arguments, ok := normalizeToolArguments(args)
		if !ok {
			return nil, false
		}

		id := strings.TrimSpace(spec.ID)
		if id == "" {
			id = fmt.Sprintf("text_tool_call_%d", idx+1)
		}
		callType := strings.TrimSpace(spec.Type)
		if callType == "" {
			callType = "function"
		}
		calls = append(calls, schema.ToolCall{
			ID:   id,
			Type: callType,
			Function: schema.FunctionCall{
				Name:      canonical,
				Arguments: arguments,
			},
		})
	}
	return calls, true
}

func resolveToolName(name string, toolNames map[string]string) (string, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", false
	}
	if canonical, ok := toolNames[trimmed]; ok {
		return canonical, true
	}
	canonical, ok := toolNames[strings.ToLower(trimmed)]
	return canonical, ok
}

func normalizeToolArguments(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "{}", true
	}

	if trimmed[0] == '"' {
		var encoded string
		if err := json.Unmarshal(trimmed, &encoded); err != nil {
			return "", false
		}
		encoded = strings.TrimSpace(encoded)
		if encoded == "" {
			return "{}", true
		}
		return compactJSONObject([]byte(encoded))
	}

	return compactJSONObject(trimmed)
}

func compactJSONObject(data []byte) (string, bool) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "{}", true
	}
	if trimmed[0] != '{' {
		return "", false
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return "", false
	}
	return buf.String(), true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstRawMessage(values ...json.RawMessage) json.RawMessage {
	for _, value := range values {
		if len(bytes.TrimSpace(value)) > 0 {
			return value
		}
	}
	return nil
}

func extractJSONValueCandidates(content string) []string {
	data := []byte(content)
	candidates := make([]string, 0, 2)
	for idx, ch := range data {
		if ch != '{' && ch != '[' {
			continue
		}
		if end := findJSONValueEnd(data[idx:]); end > 0 {
			candidates = append(candidates, string(data[idx:idx+end]))
		}
	}
	return candidates
}

func findJSONValueEnd(data []byte) int {
	if len(data) == 0 {
		return -1
	}
	open := data[0]
	close := byte('}')
	if open == '[' {
		close = ']'
	} else if open != '{' {
		return -1
	}

	depth := 0
	inString := false
	escaped := false
	for idx, ch := range data {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return idx + 1
			}
		}
	}
	return -1
}
