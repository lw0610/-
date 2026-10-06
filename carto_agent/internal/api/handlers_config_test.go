package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/settings"
)

type stubConfigMutationSettingsService struct {
	batchErr error
}

func (s stubConfigMutationSettingsService) GetAllConfig() (map[string]any, error) {
	return nil, nil
}

func (s stubConfigMutationSettingsService) GetConfig(string, string) (any, error) {
	return nil, nil
}

func (s stubConfigMutationSettingsService) GetLLMConfig() (any, error) {
	return nil, nil
}

func (s stubConfigMutationSettingsService) SaveConfig(string, any, string) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, nil
}

func (s stubConfigMutationSettingsService) BatchSetConfig([]config.ConfigItem) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, s.batchErr
}

func (s stubConfigMutationSettingsService) SaveLLMProviders(map[string]any) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, nil
}

func (s stubConfigMutationSettingsService) SwitchActiveLLMProvider(string) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, nil
}

func (s stubConfigMutationSettingsService) CreateAgentConfig(string, string, string) (map[string]any, error) {
	return nil, nil
}

func (s stubConfigMutationSettingsService) CloneAgentConfig(string, string, string) (map[string]any, error) {
	return nil, nil
}

func (s stubConfigMutationSettingsService) SaveAgentConfig(string, string, settings.AgentProfileView) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, nil
}

func (s stubConfigMutationSettingsService) SaveAgentConfigData(string, string, *config.AgentConfigData) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, nil
}

func (s stubConfigMutationSettingsService) DeleteAgentConfig(string, string) (map[string]any, error) {
	return nil, nil
}

func (s stubConfigMutationSettingsService) ActivateAgentConfig(string, string) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, nil
}

func (s stubConfigMutationSettingsService) AddAgentToConfig(string, string, config.AgentDefData) (map[string]any, error) {
	return nil, nil
}

func (s stubConfigMutationSettingsService) UpdateAgentInConfig(string, string, config.AgentDefData) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, nil
}

func (s stubConfigMutationSettingsService) RemoveAgentFromConfig(string, string) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, nil
}

func (s stubConfigMutationSettingsService) ToggleSkillPackage(string, bool) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, nil
}

func (s stubConfigMutationSettingsService) SaveSkillWithScope(string, string, string, string, string) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, nil
}

func (s stubConfigMutationSettingsService) SaveSkill(string, string, string) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, nil
}

func (s stubConfigMutationSettingsService) DeleteSkillWithScope(string, string, string) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, nil
}

func (s stubConfigMutationSettingsService) DeleteSkill(string) (settings.SettingsMutationResult, error) {
	return settings.SettingsMutationResult{}, nil
}

func (s stubConfigMutationSettingsService) ImportSkillWithScope(string, bool, string, string) (map[string]any, error) {
	return nil, nil
}

func (s stubConfigMutationSettingsService) ImportSkill(string, bool) (map[string]any, error) {
	return nil, nil
}

func TestConfigMigrationFailureEnvelope(t *testing.T) {
	tmpDir := t.TempDir()
	defaultPath := filepath.Join(tmpDir, "broken-default.yaml")
	userPath := filepath.Join(tmpDir, "user.yaml")
	if err := os.WriteFile(defaultPath, []byte("llm: [\n"), 0o644); err != nil {
		t.Fatalf("write invalid default config failed: %v", err)
	}

	configService := config.NewConfigService(nil, &config.Config{}, defaultPath, userPath)
	migrationErr := configService.ReloadConfig()
	if migrationErr == nil {
		t.Fatal("expected ReloadConfig to fail for broken default config")
	}
	if got := fmt.Sprint(migrationErr); got == "" {
		t.Fatal("expected migration error message")
	}

	handler := NewAPIWithServices(nil, nil, nil, nil, nil, stubConfigMutationSettingsService{batchErr: migrationErr}, nil, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodPut, "/api/config/batch", bytes.NewBufferString(`{"items":[{"path":"log.level","value":"DEBUG"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-ID", "corr-test-1")
	req.Header.Set("X-Evidence-ID", "evid-test-1")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status=%d body=%s", rr.Code, rr.Body.String())
	}
	payload, envelope := decodeEnvelopeResponse(t, rr)
	assertEnvelopeResponse(t, payload, envelope, "CONFIG_MIGRATION_FAILED", "config", "config_migration")
	debugContext, ok := envelope["debug_context"].(map[string]any)
	if !ok {
		t.Fatalf("expected debug_context object, got %#v", envelope["debug_context"])
	}
	if got, _ := debugContext["operation"].(string); got != "batch_set_config" {
		t.Fatalf("unexpected debug_context.operation: %#v", debugContext["operation"])
	}
	if got, _ := debugContext["failure_kind"].(string); got != "schema_migration" {
		t.Fatalf("unexpected debug_context.failure_kind: %#v", debugContext["failure_kind"])
	}
	if got, _ := debugContext["cause"].(string); got == "" {
		t.Fatalf("expected debug_context.cause, got %#v", debugContext)
	}
	if got, _ := envelope["user_action"].(string); got == "" {
		t.Fatalf("expected user_action, got %#v", envelope)
	}
}
