package websocket

import (
	"reflect"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
)

func TestRuntimeProviderIdentityUsesSameSourceAsModel(t *testing.T) {
	stale := &config.Config{LLM: config.LLMConfig{Active: "custom", Providers: []config.LLMProviderConfig{
		{Name: "custom", Provider: "openai", Model: "old-model", MaxTokens: 4096},
	}}}
	current := &config.Config{LLM: config.LLMConfig{Active: "custom", Providers: []config.LLMProviderConfig{
		{Name: "custom", Provider: "ollama", Model: "current-model", BaseURL: "http://localhost:11434", MaxTokens: 16000},
	}}}
	for _, hasService := range []bool{false, true} {
		t.Run(map[bool]string{false: "snapshot-fallback", true: "live-settings"}[hasService], func(t *testing.T) {
			server := NewDeepServer(0, nil)
			server.SetAgentRebuildConfig(&AgentRebuildConfig{BaseConfig: stale})
			if hasService {
				server.SetManagers(newAutoTitleTestConfigService(t, current), nil, nil)
			}
			// resolveRuntimeChatModel gets its configuration through this lookup.
			modelProvider, err := server.resolveProviderByName("custom")
			if err != nil {
				t.Fatal(err)
			}
			identityProvider := server.resolveRuntimeProviderConfig(" custom ")
			if !reflect.DeepEqual(modelProvider, identityProvider) {
				t.Fatalf("model and cache identity used different settings: model=%#v identity=%#v", modelProvider, identityProvider)
			}
			wantModel := "old-model"
			if hasService {
				wantModel = "current-model"
			}
			if identityProvider == nil || identityProvider.Model != wantModel {
				t.Fatalf("model identity=%#v, want %s", identityProvider, wantModel)
			}
			identityProvider.Model = "mutated-copy"
			if next := server.resolveRuntimeProviderConfig("custom"); next == nil || next.Model != wantModel {
				t.Fatal("returned provider config was not isolated")
			}
			if missing := server.resolveRuntimeProviderConfig("missing"); missing != nil {
				t.Fatalf("unknown provider acquired an identity: %#v", missing)
			}
		})
	}
}
