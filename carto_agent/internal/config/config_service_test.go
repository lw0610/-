package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSetDefaults_ServiceLifecycleDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.setDefaults()

	if cfg.Services.Lifecycle.AutoStartOnAppLaunch == nil || !*cfg.Services.Lifecycle.AutoStartOnAppLaunch {
		t.Fatalf("AutoStartOnAppLaunch default should be true")
	}
	if cfg.Services.Lifecycle.AutoStopOnAppExit == nil || !*cfg.Services.Lifecycle.AutoStopOnAppExit {
		t.Fatalf("AutoStopOnAppExit default should be true")
	}
	if cfg.Services.Lifecycle.AutoRestartOnFailure == nil || !*cfg.Services.Lifecycle.AutoRestartOnFailure {
		t.Fatalf("AutoRestartOnFailure default should be true")
	}
	if cfg.Session.AutoTitleMaxInputTokens != 512 {
		t.Fatalf("AutoTitleMaxInputTokens default should be 512, got %d", cfg.Session.AutoTitleMaxInputTokens)
	}
	if cfg.QGIS.RecentProjects == nil {
		t.Fatalf("QGIS.RecentProjects default should be an empty slice")
	}
	if cfg.PlanMode.Enabled == nil || !*cfg.PlanMode.Enabled {
		t.Fatalf("PlanMode.Enabled default should be true")
	}
	if cfg.Image2.Enabled == nil || !*cfg.Image2.Enabled {
		t.Fatalf("Image2.Enabled default should be true")
	}
	if cfg.Image2.BaseURL != Image2DefaultBaseURL {
		t.Fatalf("Image2.BaseURL default mismatch, got %q", cfg.Image2.BaseURL)
	}
	if cfg.Agent.UseSkillEngine == nil || !*cfg.Agent.UseSkillEngine {
		t.Fatalf("Agent.UseSkillEngine default should be true")
	}
	if cfg.Ports.AgentSystem.BindRetryCount != 3 {
		t.Fatalf("AgentSystem.BindRetryCount default should be 3, got %d", cfg.Ports.AgentSystem.BindRetryCount)
	}
	if cfg.Ports.AgentSystem.AutoFallback == nil || !*cfg.Ports.AgentSystem.AutoFallback {
		t.Fatalf("AgentSystem.AutoFallback default should be true")
	}
	if cfg.Performance.Memory.Enabled == nil || !*cfg.Performance.Memory.Enabled {
		t.Fatalf("Performance.Memory.Enabled default should be true")
	}
	if cfg.Performance.Memory.MaxTaskMemoryMB != 2048 {
		t.Fatalf("Performance.Memory.MaxTaskMemoryMB default should be 2048, got %d", cfg.Performance.Memory.MaxTaskMemoryMB)
	}
	if cfg.Performance.Cache.Enabled == nil || !*cfg.Performance.Cache.Enabled {
		t.Fatalf("Performance.Cache.Enabled default should be true")
	}
	if cfg.Performance.Cache.TTL != 300 {
		t.Fatalf("Performance.Cache.TTL default should be 300, got %d", cfg.Performance.Cache.TTL)
	}
	if cfg.Performance.Cache.MaxSizeMB != 100 {
		t.Fatalf("Performance.Cache.MaxSizeMB default should be 100, got %d", cfg.Performance.Cache.MaxSizeMB)
	}
	if cfg.Performance.Cache.Strategy != "lru" {
		t.Fatalf("Performance.Cache.Strategy default should be lru, got %q", cfg.Performance.Cache.Strategy)
	}
	if cfg.Session.Recovery.AutoSaveIntervalSeconds != 60 {
		t.Fatalf("Session.Recovery.AutoSaveIntervalSeconds default should be 60, got %d", cfg.Session.Recovery.AutoSaveIntervalSeconds)
	}
	if cfg.Session.Recovery.EnableCrashRecovery == nil || !*cfg.Session.Recovery.EnableCrashRecovery {
		t.Fatalf("Session.Recovery.EnableCrashRecovery default should be true")
	}
}

func TestSetDefaults_ValidatePasses(t *testing.T) {
	cfg := &Config{}
	cfg.setDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate should pass after setDefaults, got error: %v", err)
	}
}

func TestSetDefaults_PathLayoutUsesPluginRelativeDirs(t *testing.T) {
	cfg := &Config{}
	cfg.setDefaults()

	if cfg.Log.Dir != "data/logs" {
		t.Fatalf("Log.Dir default mismatch, got %q want %q", cfg.Log.Dir, "data/logs")
	}
	if cfg.DataDir != "data" {
		t.Fatalf("DataDir default mismatch, got %q want %q", cfg.DataDir, "data")
	}
}

func TestExpandPathWithHomePrefix(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir failed: %v", err)
	}

	got := expandPath("~/.autocarto/config/example")
	want := filepath.Join(home, ".autocarto", "config", "example")
	if got != want {
		t.Fatalf("expandPath home prefix mismatch, got %q want %q", got, want)
	}
}

func TestExpandPathWithMacOSBundleResources(t *testing.T) {
	root := t.TempDir()
	resourcesDir := filepath.Join(root, "AutoCarto AI.app", "Contents", "Resources")
	if err := os.MkdirAll(filepath.Join(resourcesDir, "autocarto", "core", "tool_engine"), 0o755); err != nil {
		t.Fatalf("mkdir resources failed: %v", err)
	}

	execPath := filepath.Join(root, "AutoCarto AI.app", "Contents", "MacOS", "carto-agent")
	got := expandPathWithExecutable(filepath.Join("autocarto", "core", "tool_engine"), execPath)
	want := filepath.Join(resourcesDir, "autocarto", "core", "tool_engine")
	if got != want {
		t.Fatalf("expandPathWithExecutable macOS bundle mismatch, got %q want %q", got, want)
	}
}

func TestExpandPathWithLegacyMacOSBundlePathKeepsExplicitPath(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, "AutoCarto AI.app", "Contents", "autocarto", "core", "tool_engine")
	if err := os.MkdirAll(legacyPath, 0o755); err != nil {
		t.Fatalf("mkdir explicit path failed: %v", err)
	}

	got := expandPathWithExecutable(legacyPath, "")
	if got != legacyPath {
		t.Fatalf("expandPathWithExecutable explicit path mismatch, got %q want %q", got, legacyPath)
	}
}
func TestExpandPathWithExecutableKeepsLegacyParentFallback(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "autocarto")
	if err := os.MkdirAll(filepath.Join(pluginDir, "core", "tool_engine"), 0o755); err != nil {
		t.Fatalf("mkdir plugin dir failed: %v", err)
	}

	execPath := filepath.Join(pluginDir, "bin", "carto-agent")
	got := expandPathWithExecutable(filepath.Join("core", "tool_engine"), execPath)
	want := filepath.Join(pluginDir, "core", "tool_engine")
	if got != want {
		t.Fatalf("expandPathWithExecutable legacy fallback mismatch, got %q want %q", got, want)
	}
}

func TestExpandPathWithExecutableSupportsTauriBinariesLayout(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "autocarto", "core", "tool_engine"), 0o755); err != nil {
		t.Fatalf("mkdir tool_engine failed: %v", err)
	}

	execPath := filepath.Join(root, "autocarto-web", "src-tauri", "binaries", "carto-agent-x86_64-unknown-linux-gnu")
	got := expandPathWithExecutable(filepath.Join("autocarto", "core", "tool_engine"), execPath)
	want := filepath.Join(root, "autocarto", "core", "tool_engine")
	if got != want {
		t.Fatalf("expandPathWithExecutable tauri binaries mismatch, got %q want %q", got, want)
	}
}

func TestExpandPathWithExecutableFallsBackToWorkingDirectoryParents(t *testing.T) {
	root := physicalPath(t, t.TempDir())
	workspaceDir := filepath.Join(root, "workspace")
	workDir := filepath.Join(workspaceDir, "carto_agent")
	tempExecModuleDir := filepath.Join(root, "go-build", "tmp", "autocarto", "core", "tool_engine")
	moduleDir := filepath.Join(workspaceDir, "autocarto", "core", "tool_engine")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work dir failed: %v", err)
	}
	if err := os.MkdirAll(tempExecModuleDir, 0o755); err != nil {
		t.Fatalf("mkdir temp exec module dir failed: %v", err)
	}
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir failed: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalWD)
	}()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("os.Chdir failed: %v", err)
	}

	execPath := filepath.Join(root, "go-build", "tmp", "autocarto-agent")
	got := expandPathWithExecutable(filepath.Join("autocarto", "core", "tool_engine"), execPath)
	if got != moduleDir {
		t.Fatalf("expandPathWithExecutable working directory fallback mismatch, got %q want %q", got, moduleDir)
	}
}

func TestSaveLLMConfig_PersistsQuickModel(t *testing.T) {
	cs := newTestConfigService(t, &Config{
		LLM: LLMConfig{
			Active: "chat-main",
			Providers: []LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
				{Name: "quick-lite", Provider: "google", Model: "gemini-2.5-flash"},
			},
		},
	})

	payload := map[string]any{
		"active":      "chat-main",
		"quick_model": "quick-lite",
		"providers": []any{
			map[string]any{"name": "chat-main", "provider": "openai", "model": "gpt-4.1"},
			map[string]any{"name": "quick-lite", "provider": "google", "model": "gemini-2.5-flash"},
		},
	}

	if err := cs.SaveLLMConfig(payload); err != nil {
		t.Fatalf("SaveLLMConfig failed: %v", err)
	}

	llmViewRaw, err := cs.GetConfig("llm", "")
	if err != nil {
		t.Fatalf("GetConfig(llm) failed: %v", err)
	}
	llmView, ok := llmViewRaw.(map[string]any)
	if !ok {
		t.Fatalf("llm view type mismatch: %#v", llmViewRaw)
	}

	if got, _ := llmView["quick_model"].(string); got != "quick-lite" {
		t.Fatalf("quick_model should be quick-lite, got %q", got)
	}
	providers, ok := llmView["providers"].([]any)
	if !ok || len(providers) != 2 {
		t.Fatalf("providers view mismatch: %#v", llmView["providers"])
	}
	quickProvider, ok := providers[1].(map[string]any)
	if !ok {
		t.Fatalf("provider view type mismatch: %#v", providers[1])
	}
	if got, _ := quickProvider["supports_plan_execute"].(bool); !got {
		t.Fatalf("google quick provider should support plan execute")
	}
	if got, _ := quickProvider["ready_for_plan"].(bool); got {
		t.Fatalf("provider without api key should not be ready for plan")
	}
	if got, _ := quickProvider["ready_for_auto_title"].(bool); got {
		t.Fatalf("provider without api key should not be ready for auto title")
	}
	if got, _ := quickProvider["readiness_reason"].(string); !strings.Contains(got, "API key not configured") {
		t.Fatalf("expected API key readiness reason, got %q", got)
	}

	provider, err := cs.GetQuickProvider()
	if err != nil {
		t.Fatalf("GetQuickProvider failed: %v", err)
	}
	if provider.Name != "quick-lite" {
		t.Fatalf("quick provider should be quick-lite, got %q", provider.Name)
	}
}

func TestGetQuickProvider_FallbackToActive(t *testing.T) {
	cs := newTestConfigService(t, &Config{
		LLM: LLMConfig{
			Active: "chat-main",
			Providers: []LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
			},
		},
	})

	provider, err := cs.GetQuickProvider()
	if err != nil {
		t.Fatalf("GetQuickProvider failed: %v", err)
	}
	if provider.Name != "chat-main" {
		t.Fatalf("fallback provider should be chat-main, got %q", provider.Name)
	}
}

func TestGetConfig_LLMIncludesDecryptedAPIKey(t *testing.T) {
	cs := newTestConfigService(t, &Config{
		LLM: LLMConfig{
			Active: "chat-main",
			Providers: []LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
			},
		},
	})

	if err := cs.SetAPIKey("chat-main", "sk-test-123"); err != nil {
		t.Fatalf("SetAPIKey failed: %v", err)
	}

	llmViewRaw, err := cs.GetConfig("llm", "")
	if err != nil {
		t.Fatalf("GetConfig(llm) failed: %v", err)
	}
	llmView, ok := llmViewRaw.(map[string]any)
	if !ok {
		t.Fatalf("llm view type mismatch: %#v", llmViewRaw)
	}
	providers, ok := llmView["providers"].([]any)
	if !ok || len(providers) != 1 {
		t.Fatalf("providers view mismatch: %#v", llmView["providers"])
	}
	provider, ok := providers[0].(map[string]any)
	if !ok {
		t.Fatalf("provider view type mismatch: %#v", providers[0])
	}
	if got, _ := provider["api_key"].(string); got != "sk-test-123" {
		t.Fatalf("api_key should be returned to desktop settings UI, got %q", got)
	}
	if got, _ := provider["has_api_key"].(bool); !got {
		t.Fatalf("has_api_key should be true when decrypted key exists")
	}
	if got, _ := provider["supports_plan_execute"].(bool); !got {
		t.Fatalf("openai provider should support plan execute")
	}
	if got, _ := provider["supports_forced_tool_choice"].(bool); !got {
		t.Fatalf("openai provider should support forced tool choice")
	}
	if got, _ := provider["ready_for_chat"].(bool); !got {
		t.Fatalf("provider with api key should be ready for chat")
	}
	if got, _ := provider["ready_for_plan"].(bool); !got {
		t.Fatalf("provider with api key should be ready for plan")
	}
	if got, _ := provider["ready_for_auto_title"].(bool); !got {
		t.Fatalf("provider with api key should be ready for auto title")
	}
}

func TestImage2ConfigRoundTripUsesDedicatedSecret(t *testing.T) {
	disabled := false
	cs := newTestConfigService(t, &Config{
		Image2: Image2Config{Enabled: &disabled},
	})
	backend := &fakeKeyringBackend{}
	manager, err := newSecretsManagerWithKeyring(t.TempDir(), backend)
	if err != nil {
		t.Fatalf("newSecretsManagerWithKeyring failed: %v", err)
	}
	cs.secretsManager = manager

	result, err := cs.SaveImage2Config(
		true,
		"sk-image2-test",
		" https://image2.example.com/openai/v1/ ",
	)
	if err != nil {
		t.Fatalf("SaveImage2Config failed: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected Image 2 config mutation to be marked changed")
	}
	if got := backend.values[Image2SecretName]; got != "sk-image2-test" {
		t.Fatalf("Image 2 key should use dedicated secret name, got %q", got)
	}

	view, err := cs.GetImage2Config()
	if err != nil {
		t.Fatalf("GetImage2Config failed: %v", err)
	}
	if !view.Enabled || !view.HasAPIKey || view.APIKey != "sk-image2-test" {
		t.Fatalf("unexpected Image 2 view: %#v", view)
	}
	if view.Model != Image2ModelName || view.BaseURL != "https://image2.example.com/openai/v1" {
		t.Fatalf("unexpected fixed Image 2 endpoint: %#v", view)
	}

	configData, err := os.ReadFile(cs.userConfigPath)
	if err != nil {
		t.Fatalf("read user config failed: %v", err)
	}
	if strings.Contains(string(configData), "sk-image2-test") {
		t.Fatal("Image 2 API key must not be written to config YAML")
	}
	var persisted Config
	if err := yaml.Unmarshal(configData, &persisted); err != nil {
		t.Fatalf("parse user config failed: %v", err)
	}
	if persisted.Image2.BaseURL != "https://image2.example.com/openai/v1" {
		t.Fatalf("Image 2 base_url was not persisted, got %q", persisted.Image2.BaseURL)
	}
}

func TestSaveImage2ConfigRejectsInvalidBaseURLBeforeChangingSecret(t *testing.T) {
	enabled := true
	cs := newTestConfigService(t, &Config{
		Image2: Image2Config{Enabled: &enabled, BaseURL: Image2DefaultBaseURL},
	})
	backend := &fakeKeyringBackend{values: map[string]string{Image2SecretName: "sk-original"}}
	manager, err := newSecretsManagerWithKeyring(t.TempDir(), backend)
	if err != nil {
		t.Fatalf("newSecretsManagerWithKeyring failed: %v", err)
	}
	cs.secretsManager = manager

	_, err = cs.SaveImage2Config(false, "sk-replacement", "file:///tmp/image2")
	if !errors.Is(err, ErrInvalidImage2BaseURL) {
		t.Fatalf("expected invalid Image 2 base_url error, got %v", err)
	}
	if got := backend.values[Image2SecretName]; got != "sk-original" {
		t.Fatalf("invalid base_url must not change encrypted API key, got %q", got)
	}
	view, err := cs.GetImage2Config()
	if err != nil {
		t.Fatalf("GetImage2Config failed: %v", err)
	}
	if !view.Enabled || view.BaseURL != Image2DefaultBaseURL {
		t.Fatalf("invalid base_url must not change Image 2 config: %#v", view)
	}
}

func TestGetAllConfig_LLMMasksDecryptedAPIKey(t *testing.T) {
	cs := newTestConfigService(t, &Config{
		LLM: LLMConfig{
			Active: "chat-main",
			Providers: []LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
			},
		},
	})

	if err := cs.SetAPIKey("chat-main", "sk-test-123"); err != nil {
		t.Fatalf("SetAPIKey failed: %v", err)
	}

	allConfig, err := cs.GetAllConfig()
	if err != nil {
		t.Fatalf("GetAllConfig failed: %v", err)
	}
	llmView := mustGetNestedMap(t, allConfig, "llm")
	providers, ok := llmView["providers"].([]any)
	if !ok || len(providers) != 1 {
		t.Fatalf("providers view mismatch: %#v", llmView["providers"])
	}
	provider, ok := providers[0].(map[string]any)
	if !ok {
		t.Fatalf("provider view type mismatch: %#v", providers[0])
	}
	if _, ok := provider["api_key"]; ok {
		t.Fatalf("GetAllConfig should not expose plaintext api_key")
	}
	if got, _ := provider["has_api_key"].(bool); !got {
		t.Fatalf("has_api_key should stay true when secret exists")
	}
	if got, _ := provider["ready_for_chat"].(bool); !got {
		t.Fatalf("provider with stored secret should be ready for chat")
	}
}

func TestGetFullConfig_LLMIncludesDecryptedAPIKey(t *testing.T) {
	cs := newTestConfigService(t, &Config{
		LLM: LLMConfig{
			Active: "chat-main",
			Providers: []LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
			},
		},
	})

	if err := cs.SetAPIKey("chat-main", "sk-test-123"); err != nil {
		t.Fatalf("SetAPIKey failed: %v", err)
	}

	fullCfg, err := cs.GetFullConfig()
	if err != nil {
		t.Fatalf("GetFullConfig failed: %v", err)
	}
	provider := fullCfg.GetActiveProvider()
	if provider == nil {
		t.Fatal("expected active provider in full config")
	}
	if provider.APIKey != "sk-test-123" {
		t.Fatalf("expected decrypted API key in full config, got %q", provider.APIKey)
	}
}

func TestSaveLLMConfig_EmptyAPIKeyDeletesSecret(t *testing.T) {
	cs := newTestConfigService(t, &Config{
		LLM: LLMConfig{
			Active: "chat-main",
			Providers: []LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
			},
		},
	})

	if err := cs.SetAPIKey("chat-main", "sk-test-123"); err != nil {
		t.Fatalf("SetAPIKey failed: %v", err)
	}

	err := cs.SaveLLMConfig(map[string]any{
		"active":      "chat-main",
		"quick_model": "",
		"providers": []any{
			map[string]any{"name": "chat-main", "provider": "openai", "model": "gpt-4.1", "api_key": ""},
		},
	})
	if err != nil {
		t.Fatalf("SaveLLMConfig failed: %v", err)
	}

	if _, err := cs.GetAPIKey("chat-main"); err == nil {
		t.Fatalf("API key should be deleted after saving empty value")
	}
}

func TestSaveLLMConfig_RejectsUnknownQuickModel(t *testing.T) {
	initial := &Config{
		LLM: LLMConfig{
			Active: "chat-main",
			Providers: []LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
			},
		},
	}
	cs := newTestConfigService(t, initial)

	err := cs.SaveLLMConfig(map[string]any{
		"active":      "chat-main",
		"quick_model": "missing-model",
		"providers": []any{
			map[string]any{"name": "chat-main", "provider": "openai", "model": "gpt-4.1"},
		},
	})
	if err == nil {
		t.Fatalf("SaveLLMConfig should fail for unknown quick_model")
	}
	if !strings.Contains(err.Error(), "quick model not found") {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cs.config.LLM.QuickModel; got != "" {
		t.Fatalf("quick model should remain empty after failure, got %q", got)
	}
	if got := len(cs.config.LLM.Providers); got != 1 {
		t.Fatalf("providers should remain unchanged after failure, got %d", got)
	}
}

func TestSaveLLMConfig_RejectsUnsupportedProvider(t *testing.T) {
	cs := newTestConfigService(t, &Config{
		LLM: LLMConfig{
			Active: "chat-main",
			Providers: []LLMProviderConfig{
				{Name: "chat-main", Provider: "openai", Model: "gpt-4.1"},
			},
		},
	})

	err := cs.SaveLLMConfig(map[string]any{
		"active":      "chat-main",
		"quick_model": "",
		"providers": []any{
			map[string]any{"name": "chat-main", "provider": "custom", "model": "custom-chat"},
		},
	})
	if err == nil {
		t.Fatalf("SaveLLMConfig should fail for unsupported provider")
	}
	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cs.config.LLM.Providers[0].Provider; got != "openai" {
		t.Fatalf("provider should remain unchanged after failure, got %q", got)
	}
}

func TestNewConfigService_PrefersUserConfigDirForSecrets(t *testing.T) {
	tmpDir := t.TempDir()
	defaultDir := filepath.Join(tmpDir, "repo-defaults")
	userDir := filepath.Join(tmpDir, "user-home")
	defaultPath := filepath.Join(defaultDir, "config.yaml")
	userPath := filepath.Join(userDir, "config.yaml")

	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		t.Fatalf("mkdir default dir failed: %v", err)
	}
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user dir failed: %v", err)
	}
	if err := os.WriteFile(defaultPath, []byte("llm:\n  active: \"\"\n  providers: []\n"), 0o644); err != nil {
		t.Fatalf("write default config failed: %v", err)
	}

	cfg := &Config{}
	cfg.setDefaults()
	cs := NewConfigService(nil, cfg, defaultPath, userPath)

	if err := cs.SetAPIKey("chat-main", "sk-user-only"); err != nil {
		t.Fatalf("SetAPIKey failed: %v", err)
	}

	defaultSecretsPath := filepath.Join(defaultDir, "secrets.enc")
	userSecretsPath := filepath.Join(userDir, "secrets.enc")
	if _, err := os.Stat(userSecretsPath); err != nil {
		t.Fatalf("expected secrets under user dir, stat err: %v", err)
	}
	if _, err := os.Stat(defaultSecretsPath); err == nil {
		t.Fatalf("expected default config dir to stay free of secrets file")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat default secrets path failed: %v", err)
	}
}

func TestSetConfig_RejectsUnknownPath(t *testing.T) {
	cs := newTestConfigService(t, &Config{})

	_, err := cs.SetConfig("session.unknown_field", true, "")
	if err == nil {
		t.Fatalf("SetConfig should reject unknown path")
	}
	if !strings.Contains(err.Error(), "unsupported config path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigService_GetAutoTitleMaxInputTokens(t *testing.T) {
	cs := newTestConfigService(t, &Config{Session: SessionConfig{AutoTitleMaxInputTokens: 1536}})
	if got := cs.GetAutoTitleMaxInputTokens(); got != 1536 {
		t.Fatalf("expected 1536, got %d", got)
	}

	csDefault := newTestConfigService(t, &Config{})
	if got := csDefault.GetAutoTitleMaxInputTokens(); got != 512 {
		t.Fatalf("default tokens should be 512, got %d", got)
	}
}

func TestBatchSetConfig_PreservesExtendedSections(t *testing.T) {
	tmpDir := t.TempDir()
	defaultPath := filepath.Join(tmpDir, "config.yaml")
	userPath := filepath.Join(tmpDir, "user.yaml")

	// defaultPath 仅用于初始化 SecretsManager 所在目录
	if err := os.WriteFile(defaultPath, []byte("llm:\n  active: \"\"\n  providers: []\n"), 0o644); err != nil {
		t.Fatalf("write default config failed: %v", err)
	}

	cfg := &Config{
		LLM: LLMConfig{
			Active:    "",
			Providers: []LLMProviderConfig{},
		},
		Ports: PortsConfig{
			AgentSystem: PortConfig{Default: 3000},
		},
		Log: LogConfig{
			Level:        "INFO",
			Dir:          tmpDir,
			MaxSize:      1024,
			MaxBackups:   2,
			MaxAge:       7,
			MaxTotalSize: 2048,
			Compress:     false,
		},
		Agent: AgentConfig{
			LoopMaxIterations: 100,
		},
		Performance: PerformanceConfig{
			ToolCallTimeout:      30000,
			AgentResponseTimeout: 120000,
			MaxConcurrentTasks:   5,
		},
		Services: ServicesConfig{
			Lifecycle: ServiceLifecycleConfig{
				AutoStartOnAppLaunch: boolPtr(true),
				AutoStopOnAppExit:    boolPtr(true),
				AutoRestartOnFailure: boolPtr(true),
			},
		},
		UI: UIConfig{
			MainPanel: MainPanelConfig{
				Visible:  boolPtr(true),
				Floating: boolPtr(false),
				Area:     2,
				Tabified: boolPtr(true),
			},
		},
		DataDir: tmpDir,
	}

	cs := NewConfigService(nil, cfg, defaultPath, userPath)
	_, err := cs.BatchSetConfig([]ConfigItem{
		{Path: "services.lifecycle.auto_start_on_app_launch", Value: false},
		{Path: "performance.tool_call_timeout", Value: 45000},
		{Path: "ui.main_panel.visible", Value: false},
	})
	if err != nil {
		t.Fatalf("BatchSetConfig failed: %v", err)
	}

	view, err := cs.GetAllConfig()
	if err != nil {
		t.Fatalf("GetAllConfig failed: %v", err)
	}

	if got := mustGetNestedBool(t, view, "services", "lifecycle", "auto_start_on_app_launch"); got {
		t.Fatalf("services.lifecycle.auto_start_on_app_launch should be false, got true")
	}
	if got := mustGetNestedInt(t, view, "performance", "tool_call_timeout"); got != 45000 {
		t.Fatalf("performance.tool_call_timeout should be 45000, got %d", got)
	}
	if got := mustGetNestedBool(t, view, "ui", "main_panel", "visible"); got {
		t.Fatalf("ui.main_panel.visible should be false, got true")
	}

	raw, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatalf("read saved user config failed: %v", err)
	}
	var persisted map[string]any
	if err := yaml.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("unmarshal saved user config failed: %v", err)
	}

	if got := mustGetNestedBool(t, persisted, "services", "lifecycle", "auto_start_on_app_launch"); got {
		t.Fatalf("persisted services.lifecycle.auto_start_on_app_launch should be false")
	}
	if got := mustGetNestedInt(t, persisted, "performance", "tool_call_timeout"); got != 45000 {
		t.Fatalf("persisted performance.tool_call_timeout should be 45000, got %d", got)
	}
	if got := mustGetNestedBool(t, persisted, "ui", "main_panel", "visible"); got {
		t.Fatalf("persisted ui.main_panel.visible should be false")
	}
}

func mustGetNestedMap(t *testing.T, root map[string]any, key string) map[string]any {
	t.Helper()
	raw, ok := root[key]
	if !ok {
		t.Fatalf("missing key: %s", key)
	}
	nested, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("key %s is not map[string]any", key)
	}
	return nested
}

func mustGetNestedBool(t *testing.T, root map[string]any, keys ...string) bool {
	t.Helper()
	current := root
	for i, key := range keys {
		raw, ok := current[key]
		if !ok {
			t.Fatalf("missing key: %s", key)
		}
		if i == len(keys)-1 {
			val, ok := raw.(bool)
			if !ok {
				t.Fatalf("key %s is not bool", key)
			}
			return val
		}
		next, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("key %s is not map[string]any", key)
		}
		current = next
	}
	return false
}

func mustGetNestedInt(t *testing.T, root map[string]any, keys ...string) int {
	t.Helper()
	current := root
	for i, key := range keys {
		raw, ok := current[key]
		if !ok {
			t.Fatalf("missing key: %s", key)
		}
		if i == len(keys)-1 {
			switch v := raw.(type) {
			case int:
				return v
			case int64:
				return int(v)
			case float64:
				return int(v)
			default:
				t.Fatalf("key %s is not int-compatible", key)
			}
		}
		next, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("key %s is not map[string]any", key)
		}
		current = next
	}
	return 0
}

func newTestConfigService(t *testing.T, cfg *Config) *ConfigService {
	t.Helper()

	if cfg == nil {
		cfg = &Config{}
	}
	cfg.setDefaults()

	tmpDir := t.TempDir()
	defaultPath := filepath.Join(tmpDir, "config.yaml")
	userPath := filepath.Join(tmpDir, "user.yaml")
	if err := os.WriteFile(defaultPath, []byte("llm:\n  active: \"\"\n  providers: []\n"), 0o644); err != nil {
		t.Fatalf("write default config failed: %v", err)
	}

	return NewConfigService(nil, cfg, defaultPath, userPath)
}
