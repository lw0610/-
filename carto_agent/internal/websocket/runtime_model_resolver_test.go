package websocket

import (
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
)

func TestResolvePlanChatModel_PrefersQuickProviderWhenPlannerOverrideEmpty(t *testing.T) {
	baseCfg := &config.Config{
		LLM: config.LLMConfig{
			Active:     "chat-main",
			QuickModel: "deepseek-chat",
			Providers: []config.LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
				{Name: "deepseek-chat", Provider: "openai", Model: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1"},
			},
		},
	}
	cs := newAutoTitleTestConfigService(t, baseCfg)
	if err := cs.SetAPIKey("deepseek-chat", "sk-test-123"); err != nil {
		t.Fatalf("SetAPIKey failed: %v", err)
	}

	server := NewDeepServer(0, nil)
	server.SetManagers(cs, nil, nil)
	server.SetAgentRebuildConfig(&AgentRebuildConfig{BaseConfig: baseCfg})

	model, err := server.resolvePlanChatModel("")
	if err != nil {
		t.Fatalf("expected quick provider to satisfy plan model resolution, got %v", err)
	}
	if model == nil {
		t.Fatal("expected resolved plan model")
	}
}

func TestResolvePlanChatModel_FallsBackToActiveWhenQuickProviderIsNotReady(t *testing.T) {
	baseCfg := &config.Config{
		LLM: config.LLMConfig{
			Active:     "chat-main",
			QuickModel: "quick-missing-key",
			Providers: []config.LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
				{Name: "quick-missing-key", Provider: "openai", Model: "gpt-4o-mini"},
			},
		},
	}
	cs := newAutoTitleTestConfigService(t, baseCfg)
	if err := cs.SetAPIKey("chat-main", "sk-test-123"); err != nil {
		t.Fatalf("SetAPIKey failed: %v", err)
	}

	server := NewDeepServer(0, nil)
	server.SetManagers(cs, nil, nil)
	server.SetAgentRebuildConfig(&AgentRebuildConfig{BaseConfig: baseCfg})

	model, err := server.resolvePlanChatModel("")
	if err != nil {
		t.Fatalf("expected active provider fallback to satisfy plan model resolution, got %v", err)
	}
	if model == nil {
		t.Fatal("expected resolved plan model")
	}
}

func TestResolvePlanChatModel_UsesBaseConfigQuickProviderBeforeManagersReady(t *testing.T) {
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

	server := NewDeepServer(0, nil)
	server.SetAgentRebuildConfig(&AgentRebuildConfig{BaseConfig: baseCfg})

	model, err := server.resolvePlanChatModel("")
	if err != nil {
		t.Fatalf("expected base config quick provider to satisfy plan model resolution, got %v", err)
	}
	if model == nil {
		t.Fatal("expected resolved plan model")
	}
}

func TestResolvePlanChatModel_UsesExplicitPlannerOverrideBeforeQuickFallback(t *testing.T) {
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

	server := NewDeepServer(0, nil)
	server.SetAgentRebuildConfig(&AgentRebuildConfig{BaseConfig: baseCfg})

	model, err := server.resolvePlanChatModel("planner-override")
	if err != nil {
		t.Fatalf("expected explicit planner override to bypass quick fallback, got %v", err)
	}
	if model == nil {
		t.Fatal("expected resolved plan model")
	}
}
