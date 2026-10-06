package config

import "github.com/autocarto/carto_agent/internal/llm"

// ProviderToLLMConfig 将 LLMProviderConfig 转换为 llm.Config
func ProviderToLLMConfig(provider *LLMProviderConfig) *llm.Config {
	if provider == nil {
		return nil
	}
	return &llm.Config{
		Provider:        provider.Provider,
		Model:           provider.Model,
		APIKey:          provider.APIKey,
		BaseURL:         provider.BaseURL,
		MaxTokens:       provider.MaxTokens,
		Temperature:     provider.Temperature,
		ThinkingEnabled: provider.ThinkingEnabled,
		ThinkingBudget:  provider.ThinkingBudget,
	}
}
