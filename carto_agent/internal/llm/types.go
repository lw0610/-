package llm

// Tool 表示工具定义
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// Config LLM配置
type Config struct {
	Provider        string  `yaml:"provider"`         // provider id（openai/anthropic/google/ollama）
	Model           string  `yaml:"model"`            // 模型名称
	Vision          bool    `yaml:"vision"`           // 已配置的图片输入能力
	APIKey          string  `yaml:"api_key"`          // API密钥
	BaseURL         string  `yaml:"base_url"`         // 自定义API地址（可选）
	MaxTokens       int     `yaml:"max_tokens"`       // 最大token数
	Temperature     float64 `yaml:"temperature"`      // 温度参数
	ThinkingEnabled bool    `yaml:"thinking_enabled"` // 是否启用深度思考
	ThinkingBudget  int     `yaml:"thinking_budget"`  // 深度思考预算（兼容字段）
}
