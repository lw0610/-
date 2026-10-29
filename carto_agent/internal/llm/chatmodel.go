package llm

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	claudeModel "github.com/cloudwego/eino-ext/components/model/claude"
	geminiModel "github.com/cloudwego/eino-ext/components/model/gemini"
	ollamaModel "github.com/cloudwego/eino-ext/components/model/ollama"
	openaiModel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"google.golang.org/genai"
)

// NewChatModel 创建 Eino ChatModel，使用官方 eino-ext 实现
func NewChatModel(config *Config) (model.ToolCallingChatModel, error) {
	if config == nil {
		return nil, fmt.Errorf("nil llm config")
	}

	providerInfo, ok := GetProviderInfo(config.Provider)
	if !ok {
		return nil, fmt.Errorf("unsupported provider: %s", config.Provider)
	}

	if providerInfo.RequiresAPIKey && strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("API key not configured for provider: %s", providerInfo.DisplayName)
	}

	if strings.TrimSpace(config.BaseURL) == "" && strings.TrimSpace(providerInfo.DefaultBaseURL) != "" {
		config.BaseURL = providerInfo.DefaultBaseURL
	}

	if providerInfo.RequiresBaseURL && strings.TrimSpace(config.BaseURL) == "" {
		return nil, fmt.Errorf("base_url is required for provider: %s", providerInfo.DisplayName)
	}

	var chat model.ToolCallingChatModel
	var err error
	switch providerInfo.BackendType {
	case "claude":
		chat, err = createClaudeChatModel(config)
	case "openai":
		chat, err = createOpenAIChatModel(config)
	case "gemini":
		chat, err = createGeminiChatModel(config)
	case "ollama":
		chat, err = createOllamaChatModel(config)
	default:
		return nil, fmt.Errorf("unsupported backend type: %s", providerInfo.BackendType)
	}
	if err != nil {
		return nil, err
	}
	// Apply the selected model's actual media contract to every role, including
	// Plan file reads and ordinary tools, before provider-specific conversion.
	return NewMediaCapabilityModel(chat, MediaCapabilitiesForProvider(config.Provider, config.Model, config.Vision)), nil
}

// createClaudeChatModel 创建 Claude ChatModel
func createClaudeChatModel(config *Config) (model.ToolCallingChatModel, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("API key not configured for Claude")
	}

	claudeConfig := &claudeModel.Config{
		APIKey:    config.APIKey,
		Model:     config.Model,
		MaxTokens: config.MaxTokens,
	}

	// 设置可选参数
	if config.Temperature > 0 {
		temp := float32(config.Temperature)
		claudeConfig.Temperature = &temp
	}

	if config.BaseURL != "" {
		claudeConfig.BaseURL = &config.BaseURL
	}

	return claudeModel.NewChatModel(context.Background(), claudeConfig)
}

// createOpenAIChatModel 创建 OpenAI ChatModel。
// 支持自定义 base_url（第三方代理）、ReasoningEffort，并在发送前清洗 tool 消息。
func createOpenAIChatModel(config *Config) (model.ToolCallingChatModel, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("API key not configured for OpenAI")
	}

	openaiConfig := &openaiModel.ChatModelConfig{
		APIKey: config.APIKey,
		Model:  config.Model,
	}
	if httpClient := buildOpenAIHTTPClient(config.BaseURL); httpClient != nil {
		openaiConfig.HTTPClient = httpClient
	}

	// OpenAI and DeepSeek share the chat protocol, but not these controls.
	// Omitting DeepSeek's thinking switch enables its server-side default;
	// max_completion_tokens is not its documented output limit parameter.
	deepSeek := normalizedBaseURLHost(config.BaseURL) == "api.deepseek.com"
	if config.MaxTokens > 0 {
		if deepSeek {
			openaiConfig.MaxTokens = &config.MaxTokens
		} else {
			openaiConfig.MaxCompletionTokens = &config.MaxTokens
		}
	}

	if config.Temperature > 0 {
		temp := float32(config.Temperature)
		openaiConfig.Temperature = &temp
	}

	// 支持自定义 BaseURL（第三方代理）
	if config.BaseURL != "" {
		openaiConfig.BaseURL = config.BaseURL
	}

	if config.ThinkingEnabled {
		openaiConfig.ReasoningEffort = mapThinkingBudgetToReasoningEffort(config.ThinkingBudget)
	}
	if deepSeek {
		thinking := "disabled"
		if config.ThinkingEnabled {
			thinking = "enabled"
		}
		openaiConfig.ExtraFields = map[string]any{"thinking": map[string]any{"type": thinking}}
	}

	// 创建底层模型
	chatModel, err := openaiModel.NewChatModel(context.Background(), openaiConfig)
	if err != nil {
		return nil, err
	}

	wrapped := wrapOpenAICompatibleModel(config.BaseURL, chatModel)
	if shouldDisableHTTP2ForOpenAIBaseURL(config.BaseURL) {
		wrapped = NewRetryingModelWrapper(wrapped)
	}
	return wrapped, nil
}

func wrapOpenAICompatibleModel(baseURL string, chatModel model.ToolCallingChatModel) model.ToolCallingChatModel {
	wrapped := model.ToolCallingChatModel(NewSanitizingModelWrapper(chatModel))
	if isLocalOpenAICompatibleBaseURL(baseURL) {
		wrapped = NewTextToolCallModelWrapper(wrapped)
	}
	return wrapped
}

func buildOpenAIHTTPClient(baseURL string) *http.Client {
	if !shouldDisableHTTP2ForOpenAIBaseURL(baseURL) {
		return nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}

	return &http.Client{
		Timeout:   5 * time.Minute,
		Transport: transport,
	}
}

func shouldDisableHTTP2ForOpenAIBaseURL(baseURL string) bool {
	host := normalizedBaseURLHost(baseURL)
	return host == "api.deepseek.com"
}

func isLocalOpenAICompatibleBaseURL(baseURL string) bool {
	switch normalizedBaseURLHost(baseURL) {
	case "127.0.0.1", "localhost", "::1", "0.0.0.0":
		return true
	default:
		return false
	}
}

func normalizedBaseURLHost(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return ""
	}

	host := trimmed
	if parsed, err := url.Parse(trimmed); err == nil {
		if hostname := strings.TrimSpace(parsed.Hostname()); hostname != "" {
			host = hostname
		}
	}

	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.Split(host, "/")[0]
	if strings.Count(host, ":") == 1 {
		host = strings.Split(host, ":")[0]
	}
	return host
}

// mapThinkingBudgetToReasoningEffort 将 ThinkingBudget 映射到 OpenAI 推理强度。
func mapThinkingBudgetToReasoningEffort(budget int) openaiModel.ReasoningEffortLevel {
	switch {
	case budget >= 8192:
		return openaiModel.ReasoningEffortLevelHigh
	case budget >= 4096:
		return openaiModel.ReasoningEffortLevelMedium
	default:
		return openaiModel.ReasoningEffortLevelLow
	}
}

// createGeminiChatModel 创建 Gemini ChatModel
func createGeminiChatModel(config *Config) (model.ToolCallingChatModel, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("API key not configured for Gemini")
	}

	// 创建 Gemini 客户端
	ctx := context.Background()
	clientConfig := &genai.ClientConfig{
		APIKey:  config.APIKey,
		Backend: genai.BackendGeminiAPI,
	}

	// 支持自定义 BaseURL（第三方代理）
	if config.BaseURL != "" {
		clientConfig.HTTPOptions = genai.HTTPOptions{
			BaseURL: config.BaseURL,
		}
	}

	client, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}

	geminiConfig := &geminiModel.Config{
		Client: client,
		Model:  config.Model,
	}

	// 设置可选参数
	if config.MaxTokens > 0 {
		geminiConfig.MaxTokens = &config.MaxTokens
	}

	if config.Temperature > 0 {
		temp := float32(config.Temperature)
		geminiConfig.Temperature = &temp
	}

	return geminiModel.NewChatModel(context.Background(), geminiConfig)
}

// createOllamaChatModel 创建 Ollama ChatModel
func createOllamaChatModel(config *Config) (model.ToolCallingChatModel, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, fmt.Errorf("base_url is required for Ollama")
	}

	ollamaConfig := &ollamaModel.ChatModelConfig{
		BaseURL: config.BaseURL,
		Model:   config.Model,
		Timeout: 5 * time.Minute,
	}

	options := &ollamaModel.Options{}
	hasOptions := false

	if config.MaxTokens > 0 {
		options.NumPredict = config.MaxTokens
		hasOptions = true
	}

	if config.Temperature > 0 {
		options.Temperature = float32(config.Temperature)
		hasOptions = true
	}

	if hasOptions {
		ollamaConfig.Options = options
	}

	chatModel, err := ollamaModel.NewChatModel(context.Background(), ollamaConfig)
	if err != nil {
		return nil, err
	}

	return NewTextToolCallModelWrapper(NewSanitizingModelWrapper(chatModel)), nil
}
