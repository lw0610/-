package config

import "testing"

func TestProviderToLLMConfigPreservesDeclaredVision(t *testing.T) {
	for _, vision := range []bool{false, true} {
		provider := &LLMProviderConfig{Provider: "openai", Model: "test-model", Vision: vision}
		if got := ProviderToLLMConfig(provider); got.Vision != vision {
			t.Fatalf("configured vision=%v lost at model construction", vision)
		}
	}
}
