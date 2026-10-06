package websocket

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
)

func newAutoTitleTestConfigService(t *testing.T, cfg *config.Config) *config.ConfigService {
	t.Helper()
	baseDir := t.TempDir()
	defaultPath := filepath.Join(baseDir, "defaults", "config.yaml")
	userPath := filepath.Join(baseDir, "user", "config.yaml")
	return config.NewConfigService(nil, cfg, defaultPath, userPath)
}

func TestResolveAutoTitleProviderCandidates_PrefersReadyQuickThenActive(t *testing.T) {
	cs := newAutoTitleTestConfigService(t, &config.Config{
		LLM: config.LLMConfig{
			Active:     "chat-main",
			QuickModel: "local-quick",
			Providers: []config.LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
				{Name: "local-quick", Provider: "ollama", Model: "qwen3:latest"},
			},
		},
	})
	if err := cs.SetAPIKey("chat-main", "sk-test-123"); err != nil {
		t.Fatalf("SetAPIKey failed: %v", err)
	}

	server := NewDeepServer(0, nil)
	server.SetManagers(cs, nil, nil)

	got, err := server.resolveAutoTitleProviderCandidates()
	if err != nil {
		t.Fatalf("resolveAutoTitleProviderCandidates failed: %v", err)
	}
	want := []string{"local-quick", "chat-main"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected provider candidates: got=%v want=%v", got, want)
	}
}

func TestResolveAutoTitleProviderCandidates_FallsBackToActiveAndOtherReadyProviders(t *testing.T) {
	cs := newAutoTitleTestConfigService(t, &config.Config{
		LLM: config.LLMConfig{
			Active:     "chat-main",
			QuickModel: "quick-missing-key",
			Providers: []config.LLMProviderConfig{
				{Name: "chat-main", Provider: "anthropic", Model: "claude-sonnet-4-0"},
				{Name: "quick-missing-key", Provider: "openai", Model: "gpt-4.1"},
				{Name: "local-ready", Provider: "ollama", Model: "qwen3:latest"},
			},
		},
	})
	if err := cs.SetAPIKey("chat-main", "sk-ant-123"); err != nil {
		t.Fatalf("SetAPIKey failed: %v", err)
	}

	server := NewDeepServer(0, nil)
	server.SetManagers(cs, nil, nil)

	got, err := server.resolveAutoTitleProviderCandidates()
	if err != nil {
		t.Fatalf("resolveAutoTitleProviderCandidates failed: %v", err)
	}
	want := []string{"chat-main", "local-ready"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected provider candidates: got=%v want=%v", got, want)
	}
}

func TestResolveAutoTitleProviderCandidates_ReturnsErrorWhenNoneReady(t *testing.T) {
	cs := newAutoTitleTestConfigService(t, &config.Config{
		LLM: config.LLMConfig{
			Active: "chat-main",
			Providers: []config.LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
			},
		},
	})

	server := NewDeepServer(0, nil)
	server.SetManagers(cs, nil, nil)

	if _, err := server.resolveAutoTitleProviderCandidates(); err == nil {
		t.Fatal("expected error when no auto title provider is ready")
	}
}
