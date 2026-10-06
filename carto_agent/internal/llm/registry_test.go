package llm

import "testing"

func TestListProviderRegistry_IncludesFourBuiltinProviders(t *testing.T) {
	registry := ListProviderRegistry()
	if len(registry) != 4 {
		t.Fatalf("expected 4 providers, got %d", len(registry))
	}

	expectedOrder := []string{"openai", "anthropic", "google", "ollama"}
	for idx, providerID := range expectedOrder {
		if registry[idx].ID != providerID {
			t.Fatalf("expected provider %q at index %d, got %q", providerID, idx, registry[idx].ID)
		}
	}
}

func TestGetProviderInfo_OllamaDoesNotRequireAPIKey(t *testing.T) {
	info, ok := GetProviderInfo("ollama")
	if !ok {
		t.Fatalf("expected ollama provider info")
	}
	if info.RequiresAPIKey {
		t.Fatalf("ollama should not require api key")
	}
	if info.DefaultBaseURL == "" {
		t.Fatalf("ollama should expose default base url")
	}
}

func TestPrepareConfigForCapability_PreservesConfiguredBaseURL(t *testing.T) {
	prepared, err := PrepareConfigForCapability(&Config{
		Provider:        "openai",
		Model:           "deepseek-chat",
		BaseURL:         "https://api.deepseek.com/beta",
		Temperature:     0.7,
		ThinkingEnabled: true,
		ThinkingBudget:  1024,
	}, CapabilityForcedToolChoice)
	if err != nil {
		t.Fatalf("PrepareConfigForCapability returned error: %v", err)
	}
	if prepared.BaseURL != "https://api.deepseek.com/beta" {
		t.Fatalf("unexpected base url: %q", prepared.BaseURL)
	}
	if prepared.Temperature != 0 {
		t.Fatalf("temperature should be reset for forced tool choice, got %v", prepared.Temperature)
	}
	if prepared.ThinkingEnabled {
		t.Fatal("thinking should be disabled for forced tool choice")
	}
	if prepared.ThinkingBudget != 0 {
		t.Fatalf("thinking budget should be cleared, got %d", prepared.ThinkingBudget)
	}
}

func TestValidateCapability_AllowsOllamaThroughTextToolAdapter(t *testing.T) {
	err := ValidateCapability(&Config{
		Provider: "ollama",
		Model:    "qwen3:latest",
	}, CapabilityForcedToolChoice)
	if err != nil {
		t.Fatalf("expected ollama to satisfy forced tool choice through adapter, got %v", err)
	}
}
