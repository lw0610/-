package main

import (
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
)

func TestResolvePlanChatModelFromRuntimeConfig_AllowsOpenAICompatibleDeepSeek(t *testing.T) {
	baseCfg := &config.Config{
		LLM: config.LLMConfig{
			Active: "deepseek-chat",
			Providers: []config.LLMProviderConfig{
				{
					Name:     "deepseek-chat",
					Provider: "openai",
					Model:    "deepseek-chat",
					BaseURL:  "https://api.deepseek.com/beta",
					APIKey:   "test-key",
				},
			},
		},
	}

	model, err := resolvePlanChatModelFromRuntimeConfig(baseCfg, "")
	if err != nil {
		t.Fatalf("expected openai-compatible deepseek planner model, got %v", err)
	}
	if model == nil {
		t.Fatal("expected planner model instance")
	}
}

func TestResolvePlanChatModelFromRuntimeConfig_PrefersQuickModelForInternalPlanTasks(t *testing.T) {
	baseCfg := &config.Config{
		LLM: config.LLMConfig{
			Active:     "chat-main",
			QuickModel: "deepseek-chat",
			Providers: []config.LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
				{Name: "deepseek-chat", Provider: "openai", Model: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1", APIKey: "test-key"},
			},
		},
	}

	model, err := resolvePlanChatModelFromRuntimeConfig(baseCfg, "")
	if err != nil {
		t.Fatalf("expected quick model to satisfy plan model resolution, got %v", err)
	}
	if model == nil {
		t.Fatal("expected planner model instance")
	}
}

func TestResolvePlanChatModelFromRuntimeConfig_FallsBackToActiveWhenQuickModelNotReady(t *testing.T) {
	baseCfg := &config.Config{
		LLM: config.LLMConfig{
			Active:     "chat-main",
			QuickModel: "quick-missing-key",
			Providers: []config.LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1", APIKey: "test-key"},
				{Name: "quick-missing-key", Provider: "openai", Model: "gpt-4o-mini"},
			},
		},
	}

	model, err := resolvePlanChatModelFromRuntimeConfig(baseCfg, "")
	if err != nil {
		t.Fatalf("expected active model fallback to satisfy plan model resolution, got %v", err)
	}
	if model == nil {
		t.Fatal("expected planner model instance")
	}
}

func TestResolvePlanChatModelFromRuntimeConfig_UsesExplicitPlannerOverrideBeforeQuickFallback(t *testing.T) {
	baseCfg := &config.Config{
		LLM: config.LLMConfig{
			Active:     "chat-main",
			QuickModel: "quick-missing-key",
			Providers: []config.LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
				{Name: "quick-missing-key", Provider: "openai", Model: "gpt-4o-mini"},
				{Name: "planner-override", Provider: "openai", Model: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1", APIKey: "test-key"},
			},
		},
	}

	model, err := resolvePlanChatModelFromRuntimeConfig(baseCfg, "planner-override")
	if err != nil {
		t.Fatalf("expected explicit planner override to bypass quick fallback, got %v", err)
	}
	if model == nil {
		t.Fatal("expected planner model instance")
	}
}
