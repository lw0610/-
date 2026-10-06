package websocket

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"

	"github.com/autocarto/carto_agent/internal/llm"
	"github.com/autocarto/carto_agent/internal/sessionmeta"
)

const (
	autoTitleGenerateTimeout   = 30 * time.Second
	autoTitleResponseMaxTokens = 128
	autoTitleResponseMaxRunes  = 20
	autoTitleDefaultInputLimit = 512
	autoTitleMinInputRuneLimit = 128
)

const autoTitleSystemPrompt = "你是会话标题助手。请根据用户首条消息生成一个简洁、明确的标题，只输出标题文本。"

const autoTitleUserPromptTemplate = "请为下面的用户消息生成标题，要求：\n1. 标题简洁，尽量控制在 8-16 个字；\n2. 准确反映任务主题；\n3. 不要使用引号、句号、冒号等结尾符号；\n4. 只输出标题，不要解释。\n\n用户消息：\n%s"

type autoTitleGenerationResult struct {
	Title        string
	Source       string
	ProviderName string
}

func (s *DeepServer) generateAutoSessionTitle(titleSeed string) (*autoTitleGenerationResult, error) {
	input := normalizeAutoTitleInput(titleSeed)
	if input == "" {
		return nil, fmt.Errorf("title seed is empty")
	}
	if s.configService == nil {
		return nil, fmt.Errorf("config service is nil")
	}

	maxInputTokens := s.configService.GetAutoTitleMaxInputTokens()
	input = trimAutoTitleInputByTokenBudget(input, maxInputTokens)
	if input == "" {
		return nil, fmt.Errorf("title input empty after normalization")
	}

	providerNames, err := s.resolveAutoTitleProviderCandidates()
	if err != nil {
		return nil, err
	}

	errorsText := make([]string, 0, len(providerNames))
	for _, providerName := range providerNames {
		result, runErr := s.generateAutoSessionTitleWithProvider(providerName, input)
		if runErr == nil {
			return result, nil
		}
		errorsText = append(errorsText, fmt.Sprintf("%s: %v", providerName, runErr))
	}

	if len(errorsText) == 0 {
		return nil, fmt.Errorf("no provider is ready for auto title")
	}
	return nil, fmt.Errorf("generate auto title failed for all candidates: %s", strings.Join(errorsText, "; "))
}

func (s *DeepServer) resolveAutoTitleProviderCandidates() ([]string, error) {
	if s == nil || s.configService == nil {
		return nil, fmt.Errorf("config service is nil")
	}

	raw, err := s.configService.GetConfig("llm", "")
	if err != nil {
		return nil, fmt.Errorf("load llm config view: %w", err)
	}
	llmView, ok := raw.(map[string]any)
	if !ok || llmView == nil {
		return nil, fmt.Errorf("llm config view type mismatch")
	}

	quickModel := strings.TrimSpace(anyString(llmView["quick_model"]))
	activeModel := strings.TrimSpace(anyString(llmView["active"]))
	providers, _ := llmView["providers"].([]any)
	readyProviders := make([]string, 0, len(providers))
	for _, item := range providers {
		provider, ok := item.(map[string]any)
		if !ok || provider == nil {
			continue
		}
		name := strings.TrimSpace(anyString(provider["name"]))
		if name == "" {
			continue
		}
		if ready, _ := provider["ready_for_auto_title"].(bool); !ready {
			continue
		}
		readyProviders = append(readyProviders, name)
	}

	result := make([]string, 0, len(readyProviders)+2)
	for _, name := range []string{quickModel, activeModel} {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		for _, ready := range readyProviders {
			if ready == trimmed {
				result = append(result, trimmed)
				break
			}
		}
	}
	result = uniqueAutoTitleProviderNames(append(result, readyProviders...)...)
	if len(result) == 0 {
		return nil, fmt.Errorf("no provider is ready for auto title")
	}
	return result, nil
}

func uniqueAutoTitleProviderNames(items ...string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

func anyString(raw any) string {
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}

func (s *DeepServer) generateAutoSessionTitleWithProvider(providerName string, input string) (*autoTitleGenerationResult, error) {
	provider, err := s.configService.GetProviderByName(providerName)
	if err != nil {
		return nil, fmt.Errorf("resolve provider %s: %w", providerName, err)
	}

	modelConfig := &llm.Config{
		Provider:        provider.Provider,
		Model:           provider.Model,
		APIKey:          provider.APIKey,
		BaseURL:         provider.BaseURL,
		MaxTokens:       normalizeAutoTitleMaxTokens(provider.MaxTokens),
		Temperature:     normalizeAutoTitleTemperature(provider.Temperature),
		ThinkingEnabled: false,
		ThinkingBudget:  provider.ThinkingBudget,
	}

	chatModel, err := llm.NewChatModel(modelConfig)
	if err != nil {
		return nil, fmt.Errorf("create chat model for auto title: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), autoTitleGenerateTimeout)
	defer cancel()

	messages := []*schema.Message{
		{Role: schema.System, Content: autoTitleSystemPrompt},
		{Role: schema.User, Content: fmt.Sprintf(autoTitleUserPromptTemplate, input)},
	}

	resp, err := chatModel.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("generate auto title: %w", err)
	}

	title := sanitizeAutoSessionTitle(resp.Content)
	if title == "" {
		return nil, fmt.Errorf("generated auto title is empty")
	}
	resolvedName := strings.TrimSpace(provider.Name)
	if resolvedName == "" {
		resolvedName = strings.TrimSpace(providerName)
	}
	return &autoTitleGenerationResult{
		Title:        title,
		Source:       sessionmeta.TitleSourceLLM,
		ProviderName: resolvedName,
	}, nil
}

func normalizeAutoTitleInput(input string) string {
	text := strings.TrimSpace(input)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.Join(strings.Fields(text), " ")
	return strings.TrimSpace(text)
}

func trimAutoTitleInputByTokenBudget(input string, maxInputTokens int) string {
	if maxInputTokens <= 0 {
		maxInputTokens = autoTitleDefaultInputLimit
	}
	maxRunes := maxInputTokens * 4
	if maxRunes < autoTitleMinInputRuneLimit {
		maxRunes = autoTitleMinInputRuneLimit
	}

	if utf8.RuneCountInString(input) <= maxRunes {
		return input
	}
	runes := []rune(input)
	return strings.TrimSpace(string(runes[:maxRunes]))
}

func sanitizeAutoSessionTitle(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}

	text = strings.ReplaceAll(text, "\r\n", "\n")
	for _, line := range strings.Split(text, "\n") {
		candidate := strings.TrimSpace(line)
		if candidate == "" {
			continue
		}
		text = candidate
		break
	}

	text = strings.TrimSpace(text)
	for _, prefix := range []string{"标题:", "标题：", "会话标题:", "会话标题："} {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
	}

	text = strings.Join(strings.Fields(text), " ")
	text = strings.Trim(text, "*\"'`“”‘’()[]{}<>《》")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	if utf8.RuneCountInString(text) > autoTitleResponseMaxRunes {
		runes := []rune(text)
		text = string(runes[:autoTitleResponseMaxRunes]) + "..."
	}

	return text
}

func generateFallbackAutoSessionTitle(titleSeed string) string {
	text := normalizeAutoTitleInput(titleSeed)
	if text == "" {
		return ""
	}

	text = trimAutoTitlePolitePrefix(text)
	if text == "" {
		return ""
	}

	candidate := pickAutoTitleClause(text)
	if candidate == "" {
		candidate = text
	}

	return sanitizeAutoSessionTitle(candidate)
}

func trimAutoTitlePolitePrefix(input string) string {
	text := strings.TrimSpace(input)
	if text == "" {
		return ""
	}

	prefixes := []string{
		"请帮我", "请帮忙", "请你", "请", "帮我", "帮忙",
		"我想", "我需要", "麻烦", "帮忙看下", "帮忙看一下",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
			break
		}
	}

	return strings.TrimSpace(text)
}

func pickAutoTitleClause(input string) string {
	text := strings.TrimSpace(input)
	if text == "" {
		return ""
	}

	candidate := text
	if idx := strings.IndexAny(text, "\n。！？!?；;"); idx > 0 {
		candidate = strings.TrimSpace(text[:idx])
	}

	if utf8.RuneCountInString(candidate) < 4 {
		if idx := strings.IndexAny(text, "，,:："); idx > 0 {
			commaCandidate := strings.TrimSpace(text[:idx])
			if utf8.RuneCountInString(commaCandidate) >= 4 {
				candidate = commaCandidate
			}
		}
	}

	if utf8.RuneCountInString(candidate) > autoTitleResponseMaxRunes {
		runes := []rune(candidate)
		candidate = strings.TrimSpace(string(runes[:autoTitleResponseMaxRunes]))
	}

	return strings.TrimSpace(candidate)
}

func normalizeAutoTitleMaxTokens(providerMaxTokens int) int {
	if providerMaxTokens <= 0 {
		return autoTitleResponseMaxTokens
	}
	if providerMaxTokens < autoTitleResponseMaxTokens {
		return providerMaxTokens
	}
	return autoTitleResponseMaxTokens
}

func normalizeAutoTitleTemperature(temperature float64) float64 {
	if temperature < 0 {
		return 0
	}
	if temperature > 2 {
		return 2
	}
	return temperature
}
