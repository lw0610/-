package llm

import "strings"

type ProviderCategory string

const (
	CategoryNative ProviderCategory = "native"
)

// ProviderInfo 描述内置/可扩展的大模型供应商元信息。
type ProviderInfo struct {
	ID                       string           `json:"id"`
	DisplayName              string           `json:"display_name"`
	Category                 ProviderCategory `json:"category"`
	DefaultBaseURL           string           `json:"default_base_url"`
	RequiresAPIKey           bool             `json:"requires_api_key"`
	RequiresBaseURL          bool             `json:"requires_base_url"`
	SuggestedModels          []string         `json:"suggested_models"`
	SupportsThinking         bool             `json:"supports_thinking"`
	SupportsForcedToolChoice bool             `json:"supports_forced_tool_choice"`
	SupportsPlanExecute      bool             `json:"supports_plan_execute"`
	BackendType              string           `json:"backend_type"`
}

var providerRegistry = map[string]ProviderInfo{
	"openai": {
		ID:                       "openai",
		DisplayName:              "OpenAI",
		Category:                 CategoryNative,
		RequiresAPIKey:           true,
		SuggestedModels:          []string{"gpt-4.1", "gpt-4o", "o4-mini"},
		SupportsThinking:         true,
		SupportsForcedToolChoice: true,
		SupportsPlanExecute:      true,
		BackendType:              "openai",
	},
	"anthropic": {
		ID:                       "anthropic",
		DisplayName:              "Anthropic Claude",
		Category:                 CategoryNative,
		RequiresAPIKey:           true,
		SuggestedModels:          []string{"claude-sonnet-4-0", "claude-opus-4-0"},
		SupportsThinking:         true,
		SupportsForcedToolChoice: true,
		SupportsPlanExecute:      true,
		BackendType:              "claude",
	},
	"google": {
		ID:                       "google",
		DisplayName:              "Google Gemini",
		Category:                 CategoryNative,
		RequiresAPIKey:           true,
		SuggestedModels:          []string{"gemini-2.5-pro", "gemini-2.5-flash"},
		SupportsThinking:         true,
		SupportsForcedToolChoice: true,
		SupportsPlanExecute:      true,
		BackendType:              "gemini",
	},
	"ollama": {
		ID:                       "ollama",
		DisplayName:              "Ollama",
		Category:                 CategoryNative,
		DefaultBaseURL:           "http://127.0.0.1:11434",
		RequiresAPIKey:           false,
		SuggestedModels:          []string{"qwen3:latest", "llama3.1:8b", "deepseek-r1:8b"},
		SupportsThinking:         false,
		SupportsForcedToolChoice: true,
		SupportsPlanExecute:      true,
		BackendType:              "ollama",
	},
}

func NormalizeProviderID(provider string) string {
	return strings.TrimSpace(strings.ToLower(provider))
}

func GetProviderInfo(provider string) (ProviderInfo, bool) {
	info, ok := providerRegistry[NormalizeProviderID(provider)]
	if !ok {
		return ProviderInfo{}, false
	}
	return info, true
}

func ListProviderRegistry() []ProviderInfo {
	ordered := []string{"openai", "anthropic", "google", "ollama"}
	result := make([]ProviderInfo, 0, len(ordered))
	for _, id := range ordered {
		if info, ok := providerRegistry[id]; ok {
			result = append(result, info)
		}
	}
	return result
}
