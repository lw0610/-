package config

import "testing"

func TestV4VisionProviderRoundTrip(t *testing.T) {
	cs := newTestConfigService(t, &Config{})
	if err := cs.SaveLLMConfig(map[string]any{"active": "fixture", "providers": []any{map[string]any{"name": "fixture", "provider": "openai", "model": "fixture-model", "vision": true}}}); err != nil {
		t.Fatal(err)
	}
	if !cs.config.LLM.Providers[0].Vision {
		t.Fatal("vision lost on save")
	}
	view, err := cs.GetConfig("llm", "")
	if err != nil {
		t.Fatal(err)
	}
	providers := view.(map[string]any)["providers"].([]any)
	if providers[0].(map[string]any)["vision"] != true {
		t.Fatal("vision lost in view")
	}
}
