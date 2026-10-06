package config

import "testing"

func TestValidateLLMConfig_AllowsOllamaWithoutAPIKey(t *testing.T) {
	cfg := &Config{
		LLM: LLMConfig{
			Active: "local-ollama",
			Providers: []LLMProviderConfig{
				{
					Name:     "local-ollama",
					Provider: "ollama",
					Model:    "qwen3:latest",
					BaseURL:  "http://127.0.0.1:11434",
				},
			},
		},
	}

	if err := cfg.ValidateLLMConfig(); err != nil {
		t.Fatalf("ValidateLLMConfig should allow ollama without api key: %v", err)
	}
}

func TestValidateLLMConfig_RequiresAPIKeyForOpenAI(t *testing.T) {
	cfg := &Config{
		LLM: LLMConfig{
			Active: "openai-main",
			Providers: []LLMProviderConfig{
				{
					Name:     "openai-main",
					Provider: "openai",
					Model:    "gpt-4o-mini",
				},
			},
		},
	}

	if err := cfg.ValidateLLMConfig(); err == nil {
		t.Fatalf("ValidateLLMConfig should reject openai without api key")
	}
}
