package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/autocarto/carto_agent/internal/session"
)

const (
	contextCompressionKeepRecentMessages = 4
	contextCompressionFallbackSummary    = "对话历史已压缩，请基于最新上下文继续当前任务。"
	contextCompressionInstruction        = "请将以下对话压缩为后续继续执行任务所需的简洁中文摘要。只保留当前目标、已完成工作、待办事项、关键约束、重要参数、路径和错误信息。不要输出分析过程，不要使用 XML 或标签，只输出摘要正文，控制在 300 字以内。"
)

type contextCompressionResult struct {
	Summary           string
	CompressedContext []map[string]any
	OriginalCount     int
	CompressedCount   int
}

type contextStatsSnapshot struct {
	MessageCount    int
	TotalChars      int
	EstimatedTokens int
	CompressedCount int
}

func (s *DeepServer) resolveContextCompressionModel() (model.ToolCallingChatModel, error) {
	rebuildConfig := s.agentRebuildConfigSnapshot()
	if s == nil || rebuildConfig == nil || rebuildConfig.ChatModel == nil {
		return nil, fmt.Errorf("智能体未初始化，无法生成摘要")
	}
	return rebuildConfig.ChatModel, nil
}

func (s *DeepServer) resolveContextCompressionProviderConfig() *config.LLMProviderConfig {
	if s == nil {
		return nil
	}
	return s.defaultRuntimeProviderConfig()
}

func compressContextMessages(
	ctx context.Context,
	chatModel model.ToolCallingChatModel,
	rawMessages []any,
	providerCfg *config.LLMProviderConfig,
) (*contextCompressionResult, error) {
	strategy := config.NormalizeCompressStrategy("")
	if providerCfg != nil {
		strategy = config.NormalizeCompressStrategy(providerCfg.CompressStrategy)
	}

	switch strategy {
	case config.CompressStrategyTruncate, config.CompressStrategySlidingWindow:
		return compressContextMessagesWithWindow(rawMessages, providerCfg, strategy)
	default:
		return compressContextMessagesWithSummary(ctx, chatModel, rawMessages)
	}
}

func compressContextMessagesWithSummary(
	ctx context.Context,
	chatModel model.ToolCallingChatModel,
	rawMessages []any,
) (*contextCompressionResult, error) {
	if chatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	decoded := decodeSessionMessages(rawMessages, -1)
	if len(decoded) == 0 {
		return nil, fmt.Errorf("消息列表为空或格式无效")
	}

	state := &adk.ChatModelAgentState{
		Messages: make([]adk.Message, len(decoded)),
	}
	copy(state.Messages, decoded)

	patchToolCallsHandler, err := patchtoolcalls.New(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 patchtoolcalls handler 失败: %w", err)
	}
	_, state, err = patchToolCallsHandler.BeforeModelRewriteState(ctx, state, nil)
	if err != nil {
		return nil, fmt.Errorf("执行 patchtoolcalls 失败: %w", err)
	}

	var rawSummary string
	capturingModel := &capturingBaseChatModel{
		base: chatModel,
		onGenerate: func(msg *schema.Message) {
			if msg == nil {
				return
			}
			rawSummary = sanitizeContextCompressionSummary(msg.Content)
		},
	}

	finalSummary := ""
	summarizationHandler, err := summarization.New(ctx, &summarization.Config{
		Model: capturingModel,
		TokenCounter: func(_ context.Context, input *summarization.TokenCounterInput) (int, error) {
			if input == nil || len(input.Messages) == 0 {
				return 0, nil
			}
			return 2, nil
		},
		Trigger:         &summarization.TriggerCondition{ContextTokens: 1},
		UserInstruction: contextCompressionInstruction,
		PreserveUserMessages: &summarization.PreserveUserMessages{
			Enabled: false,
		},
		Finalize: func(_ context.Context, originalMessages []adk.Message, _ adk.Message) ([]adk.Message, error) {
			finalSummary = sanitizeContextCompressionSummary(rawSummary)
			if finalSummary == "" {
				finalSummary = contextCompressionFallbackSummary
			}
			return buildCompressedContextMessages(finalSummary, originalMessages), nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("创建 summarization handler 失败: %w", err)
	}

	_, state, err = summarizationHandler.BeforeModelRewriteState(ctx, state, nil)
	if err != nil {
		return nil, fmt.Errorf("执行 summarization 失败: %w", err)
	}

	if finalSummary == "" {
		finalSummary = contextCompressionFallbackSummary
	}

	compressed := encodeCompressedContextMessages(state.Messages)
	return &contextCompressionResult{
		Summary:           finalSummary,
		CompressedContext: compressed,
		OriginalCount:     len(decoded),
		CompressedCount:   len(compressed),
	}, nil
}

func compressContextMessagesWithWindow(
	rawMessages []any,
	providerCfg *config.LLMProviderConfig,
	strategy string,
) (*contextCompressionResult, error) {
	decoded := decodeSessionMessages(rawMessages, -1)
	if len(decoded) == 0 {
		return nil, fmt.Errorf("消息列表为空或格式无效")
	}

	targetTokens := config.ContextCompressionTargetTokens(providerCfg)
	selected, afterTokens := selectWindowMessages(decoded, targetTokens, windowMinMessages(strategy))
	if len(selected) == 0 {
		selected = selectWindowMessagesFallback(decoded)
		afterTokens = estimateContextTokens(selected)
	}

	beforeTokens := estimateContextTokens(decoded)
	summary := buildWindowCompressionSummary(strategy, len(decoded), len(selected), beforeTokens, afterTokens)

	compressedMessages := selected
	if len(selected) < len(decoded) {
		compressedMessages = append([]adk.Message{
			schema.SystemMessage(fmt.Sprintf("[上下文压缩]\n%s", summary)),
		}, selected...)
	}

	compressed := encodeCompressedContextMessages(compressedMessages)
	return &contextCompressionResult{
		Summary:           summary,
		CompressedContext: compressed,
		OriginalCount:     len(decoded),
		CompressedCount:   len(compressed),
	}, nil
}

func buildCompressedContextMessages(summary string, originalMessages []adk.Message) []adk.Message {
	summary = sanitizeContextCompressionSummary(summary)
	if summary == "" {
		summary = contextCompressionFallbackSummary
	}

	result := make([]adk.Message, 0, contextCompressionKeepRecentMessages+1)
	result = append(result, schema.SystemMessage(fmt.Sprintf("[对话历史摘要]\n%s", summary)))
	result = append(result, selectRecentContextMessages(originalMessages, contextCompressionKeepRecentMessages)...)
	return result
}

func selectRecentContextMessages(messages []adk.Message, keep int) []adk.Message {
	if keep <= 0 || len(messages) == 0 {
		return nil
	}

	filtered := make([]adk.Message, 0, len(messages))
	for _, msg := range messages {
		if !shouldKeepCompressedContextMessage(msg) {
			continue
		}
		filtered = append(filtered, cloneContextMessageForCompression(msg))
	}

	if len(filtered) <= keep {
		return filtered
	}
	return filtered[len(filtered)-keep:]
}

func shouldKeepCompressedContextMessage(msg adk.Message) bool {
	if msg == nil {
		return false
	}
	if msg.Role == schema.System {
		return false
	}
	return hasMeaningfulSessionMessageContent(msg)
}

func encodeCompressedContextMessages(messages []adk.Message) []map[string]any {
	if len(messages) == 0 {
		return []map[string]any{}
	}

	encoded := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		if !shouldKeepWindowCompressedMessage(msg) && msg.Role != schema.System {
			continue
		}
		encoded = append(encoded, encodeStructuredContextMessage(msg))
	}
	return encoded
}

func sanitizeContextCompressionSummary(raw string) string {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "<summary>")
	text = strings.TrimSuffix(text, "</summary>")
	return strings.TrimSpace(text)
}

func selectWindowMessages(messages []adk.Message, targetTokens int, minKeep int) ([]adk.Message, int) {
	if len(messages) == 0 {
		return nil, 0
	}
	if minKeep <= 0 {
		minKeep = 1
	}

	selected := make([]adk.Message, 0, len(messages))
	totalTokens := 0

	for idx := len(messages) - 1; idx >= 0; idx-- {
		msg := messages[idx]
		if !shouldKeepWindowCompressedMessage(msg) {
			continue
		}
		msgTokens := estimateContextMessageTokens(msg)
		if msgTokens <= 0 {
			msgTokens = 1
		}
		if len(selected) >= minKeep && targetTokens > 0 && totalTokens+msgTokens > targetTokens {
			break
		}
		totalTokens += msgTokens
		selected = append(selected, cloneContextMessageForCompression(msg))
	}

	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}
	return selected, totalTokens
}

func selectWindowMessagesFallback(messages []adk.Message) []adk.Message {
	selected, _ := selectWindowMessages(messages, 0, 1)
	return selected
}

func shouldKeepWindowCompressedMessage(msg adk.Message) bool {
	if msg == nil {
		return false
	}
	return hasMeaningfulSessionMessageContent(msg)
}

func cloneContextMessageForCompression(msg adk.Message) adk.Message {
	if msg == nil {
		return nil
	}
	raw, err := json.Marshal(msg)
	if err == nil {
		var cloned schema.Message
		if err := json.Unmarshal(raw, &cloned); err == nil {
			return &cloned
		}
	}

	return &schema.Message{
		Role:    msg.Role,
		Content: summarizeSchemaMessageContent(msg),
	}
}

func encodeStructuredContextMessage(msg adk.Message) map[string]any {
	if msg == nil {
		return map[string]any{}
	}

	raw, err := json.Marshal(msg)
	if err == nil {
		payload := map[string]any{}
		if err := json.Unmarshal(raw, &payload); err == nil {
			if role, _ := payload["role"].(string); strings.TrimSpace(role) == "" {
				payload["role"] = string(schema.System)
			}
			if !hasMeaningfulSessionMessageContent(msg) {
				payload["content"] = summarizeSchemaMessageContent(msg)
			}
			return payload
		}
	}

	return map[string]any{
		"role":    string(msg.Role),
		"content": summarizeSchemaMessageContent(msg),
	}
}

func estimateContextTokens(messages []adk.Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateContextMessageTokens(msg)
	}
	return total
}

func estimateContextMessageTokens(msg adk.Message) int {
	if msg == nil {
		return 0
	}
	var builder strings.Builder
	builder.WriteString(string(msg.Role))
	builder.WriteString("\n")
	builder.WriteString(msg.ReasoningContent)
	builder.WriteString("\n")
	builder.WriteString(msg.Content)
	builder.WriteString("\n")
	for _, tc := range msg.ToolCalls {
		builder.WriteString(tc.Function.Name)
		builder.WriteString("\n")
		builder.WriteString(tc.Function.Arguments)
		builder.WriteString("\n")
	}
	text := builder.String()
	if text == "" {
		return 0
	}
	return estimateTextTokens(text)
}

func estimateTextTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	asciiCount := 0
	nonASCII := 0
	for _, r := range trimmed {
		if r <= unicode.MaxASCII {
			asciiCount++
		} else {
			nonASCII++
		}
	}
	tokens := asciiCount/4 + nonASCII/2
	return maxInt(1, tokens)
}

func buildContextStatsSnapshot(messages []adk.Message, compressedCount int) contextStatsSnapshot {
	stats := contextStatsSnapshot{CompressedCount: compressedCount}
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		stats.MessageCount++
		text := summarizeSchemaMessageContent(msg)
		stats.TotalChars += len(text)
		stats.EstimatedTokens += estimateContextMessageTokens(msg)
	}
	return stats
}

func compressedContextToAny(items []map[string]any) []any {
	if len(items) == 0 {
		return nil
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

func (s *DeepServer) maybeAutoCompressSessionContext(
	ctx context.Context,
	conn *websocket.Conn,
	sessionID string,
	projectPath string,
	sess *session.Session,
	mode executionMode,
) {
	if s == nil || sess == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(projectPath) == "" {
		return
	}
	providerCfg := s.resolveContextCompressionProviderConfig()
	triggerTokens := config.ContextCompressionTargetTokens(providerCfg)
	if triggerTokens <= 0 {
		return
	}
	currentMessages := sess.GetMessages()
	beforeStats := buildContextStatsSnapshot(currentMessages, 0)
	if beforeStats.EstimatedTokens < triggerTokens {
		return
	}
	chatModel, err := s.resolveContextCompressionModel()
	if err != nil {
		logger.Warn("自动压缩获取 chat model 失败: %v", err)
		return
	}
	result, err := compressContextMessages(ctx, chatModel, encodeRuntimeSnapshotMessages(currentMessages), providerCfg)
	if err != nil {
		logger.Warn("自动压缩执行失败: session_id=%s err=%v", strings.TrimSpace(sessionID), err)
		return
	}
	compressedMessages := decodeSessionMessages(compressedContextToAny(result.CompressedContext), -1)
	if len(compressedMessages) == 0 {
		return
	}
	afterStats := buildContextStatsSnapshot(compressedMessages, result.CompressedCount)
	if afterStats.EstimatedTokens >= beforeStats.EstimatedTokens {
		return
	}
	planContext := sess.GetPlanContext()
	createdAt := sess.CreatedAt
	sess.Restore(compressedMessages, planContext, createdAt, time.Now())
	s.persistManagedSessionState(sessionID, projectPath, mode)
	if conn != nil {
		s.sendContextResponse(conn, sessionID, "", true, map[string]any{
			"summary":            result.Summary,
			"compressed_context": result.CompressedContext,
			"original_count":     result.OriginalCount,
			"compressed_count":   result.CompressedCount,
			"message_count":      afterStats.MessageCount,
			"total_chars":        afterStats.TotalChars,
			"estimated_tokens":   afterStats.EstimatedTokens,
			"auto_compressed":    true,
		}, "")
	}
}

func windowMinMessages(strategy string) int {
	switch config.NormalizeCompressStrategy(strategy) {
	case config.CompressStrategySlidingWindow:
		return 4
	default:
		return 1
	}
}

func buildWindowCompressionSummary(strategy string, beforeCount, afterCount, beforeTokens, afterTokens int) string {
	label := "摘要"
	switch config.NormalizeCompressStrategy(strategy) {
	case config.CompressStrategyTruncate:
		label = "截断"
	case config.CompressStrategySlidingWindow:
		label = "滑动窗口"
	}
	return fmt.Sprintf("已按%s策略裁剪上下文，消息数 %d -> %d，估算 tokens %d -> %d。", label, beforeCount, afterCount, beforeTokens, afterTokens)
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

type capturingBaseChatModel struct {
	base       model.BaseChatModel
	onGenerate func(*schema.Message)
}

func (m *capturingBaseChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	resp, err := m.base.Generate(ctx, input, opts...)
	if err == nil && m.onGenerate != nil {
		m.onGenerate(resp)
	}
	return resp, err
}

func (m *capturingBaseChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.base.Stream(ctx, input, opts...)
}
