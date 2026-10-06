package control

import (
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/extension"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
)

const skillRuntimeApplyWarning = "技能变更将在下次会话创建时生效"

// SettingsMutationServiceDeps 表示 SettingsMutationService 的依赖。
type SettingsMutationServiceDeps struct {
	ConfigService      *config.ConfigService
	AgentConfigManager *config.AgentConfigManager
	RuntimeController  settings.RuntimeController
	SkillCatalog       SkillCatalogStore
	ActiveRuns         ActiveRunInspector
	SnapshotRefresher  snapshotRefreshRequester
}

type settingsMutationService struct {
	configService      *config.ConfigService
	agentConfigManager *config.AgentConfigManager
	runtimeController  settings.RuntimeController
	skillCatalog       SkillCatalogStore
	activeRuns         ActiveRunInspector
	snapshotRefresher  snapshotRefreshRequester
}

type scopedSkillMutationCatalog interface {
	GetSkillPackageByScope(name, scope, configName string) (settings.SkillPackageDetail, error)
	SetSkillPackageEnabledByScope(name, scope, configName string, enabled bool) error
	SaveSkillWithScope(name, scope, configName, description, content string) error
	DeleteSkillWithScope(name, scope, configName string) error
	ImportSkillWithScope(sourcePath string, overwrite bool, scope, configName string) (map[string]any, error)
}

type extensionImportCatalog interface {
	scopedSkillMutationCatalog
	ManagedSkillsDir() string
	ManagedToolsDir() string
}

type snapshotRefreshRequester interface {
	RequestRefresh(reason string)
}

// NewSettingsMutationService 创建统一设置写入服务。
func NewSettingsMutationService(deps SettingsMutationServiceDeps) SettingsMutationService {
	return &settingsMutationService{
		configService:      deps.ConfigService,
		agentConfigManager: deps.AgentConfigManager,
		runtimeController:  deps.RuntimeController,
		skillCatalog:       deps.SkillCatalog,
		activeRuns:         deps.ActiveRuns,
		snapshotRefresher:  deps.SnapshotRefresher,
	}
}

func (s *settingsMutationService) GetAllConfig() (map[string]any, error) {
	if s.configService == nil {
		return nil, newConfigServiceUnavailableError()
	}
	return s.configService.GetAllConfig()
}

func (s *settingsMutationService) GetConfig(path, userID string) (any, error) {
	if s.configService == nil {
		return nil, newConfigServiceUnavailableError()
	}
	return s.configService.GetConfig(path, userID)
}

func (s *settingsMutationService) GetLLMConfig() (any, error) {
	if s.configService == nil {
		return nil, newConfigServiceUnavailableError()
	}
	return s.configService.GetConfig("llm", "")
}

func (s *settingsMutationService) SaveConfig(path string, value any, userID string) (settings.SettingsMutationResult, error) {
	if s.configService == nil {
		return settings.SettingsMutationResult{}, newConfigServiceUnavailableError()
	}
	normalizedPath := strings.TrimSpace(path)
	if settings.IsPlanModeConfigPath(normalizedPath) {
		if err := s.blockIfActiveRuns("config", []string{normalizedPath}, "当前存在运行中的会话，无法修改会影响 Plan 运行态的配置。"); err != nil {
			return settings.SettingsMutationResult{}, err
		}
	}
	configMutation, err := s.configService.SetConfig(path, value, userID)
	if err != nil {
		return settings.SettingsMutationResult{}, err
	}
	result := s.newMutationResult("config")
	result.ConfigRevision = configMutation.Revision
	result.Idempotent = !configMutation.Changed
	result.Persisted = configMutation.Changed
	if result.Idempotent {
		result.RequiredAction = settings.MutationRequiredActionNone
		result.NextStep = ""
	}
	result.AffectedPaths = []string{normalizedPath}
	if settings.IsPlanModeConfigPath(path) {
		settings.ApplyPlanModeMutation(settings.NewMutationBuilder(s.runtimeDiagnostics()), s.runtimeApplyProvider(), &result, "配置已保存，但 Plan 运行态刷新失败: ")
	}
	return settings.FinalizeMutationResult(result), nil
}

func (s *settingsMutationService) BatchSetConfig(items []config.ConfigItem) (settings.SettingsMutationResult, error) {
	if s.configService == nil {
		return settings.SettingsMutationResult{}, newConfigServiceUnavailableError()
	}
	affectedPaths := make([]string, 0, len(items))
	if settings.HasPlanModeConfigItem(items) {
		for _, item := range items {
			affectedPaths = append(affectedPaths, strings.TrimSpace(item.Path))
		}
		if err := s.blockIfActiveRuns("config", affectedPaths, "当前存在运行中的会话，无法批量修改会影响 Plan 运行态的配置。"); err != nil {
			return settings.SettingsMutationResult{}, err
		}
	}
	configMutation, err := s.configService.BatchSetConfig(items)
	if err != nil {
		return settings.SettingsMutationResult{}, err
	}
	result := s.newMutationResult("config")
	result.ConfigRevision = configMutation.Revision
	result.Idempotent = !configMutation.Changed
	result.Persisted = configMutation.Changed
	if result.Idempotent {
		result.RequiredAction = settings.MutationRequiredActionNone
		result.NextStep = ""
	}
	for _, item := range items {
		trimmed := strings.TrimSpace(item.Path)
		if trimmed == "" {
			continue
		}
		result.AffectedPaths = append(result.AffectedPaths, trimmed)
	}
	if settings.HasPlanModeConfigItem(items) {
		settings.ApplyPlanModeMutation(settings.NewMutationBuilder(s.runtimeDiagnostics()), s.runtimeApplyProvider(), &result, "配置已保存，但 Plan 运行态刷新失败: ")
	}
	return settings.FinalizeMutationResult(result), nil
}

func (s *settingsMutationService) SaveLLMProviders(llmConfig map[string]any) (settings.SettingsMutationResult, error) {
	if s.configService == nil {
		return settings.SettingsMutationResult{}, newConfigServiceUnavailableError()
	}
	if err := s.blockIfActiveRuns("llm", []string{"llm"}, "当前存在运行中的会话，无法修改当前激活模型配置。"); err != nil {
		return settings.SettingsMutationResult{}, err
	}
	if err := s.configService.SaveLLMConfig(llmConfig); err != nil {
		return settings.SettingsMutationResult{}, err
	}
	result := s.newMutationResult("llm")
	result.ConfigRevision = s.configService.GetConfigRevision()
	settings.ApplyLLMMutation(settings.NewMutationBuilder(s.runtimeDiagnostics()), s.runtimeApplyProvider(), &result, "LLM 配置已保存，但运行时重建失败: ")
	return settings.FinalizeMutationResult(result), nil
}

func (s *settingsMutationService) SwitchActiveLLMProvider(providerName string) (settings.SettingsMutationResult, error) {
	if s.configService == nil {
		return settings.SettingsMutationResult{}, newConfigServiceUnavailableError()
	}
	if err := s.blockIfActiveRuns("llm", []string{"llm.active"}, "当前存在运行中的会话，无法修改当前激活模型配置。"); err != nil {
		return settings.SettingsMutationResult{}, err
	}
	if err := s.configService.SetActiveLLMProvider(providerName); err != nil {
		return settings.SettingsMutationResult{}, err
	}
	result := s.newMutationResult("llm")
	result.ConfigRevision = s.configService.GetConfigRevision()
	result.AffectedPaths = []string{"llm.active"}
	settings.ApplyLLMMutation(settings.NewMutationBuilder(s.runtimeDiagnostics()), s.runtimeApplyProvider(), &result, "LLM 配置已保存，但运行时重建失败: ")
	return settings.FinalizeMutationResult(result), nil
}

func (s *settingsMutationService) CreateAgentConfig(userID, name, cloneFrom string) (map[string]any, error) {
	_ = userID
	payload, err := settings.CreateAgentConfig(s.agentConfigManager, s.runtimeDiagnostics(), name, cloneFrom)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *settingsMutationService) CloneAgentConfig(userID, sourceName, targetName string) (map[string]any, error) {
	_ = userID
	payload, err := settings.CloneAgentConfig(s.agentConfigManager, s.runtimeDiagnostics(), sourceName, targetName)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *settingsMutationService) SaveAgentConfig(userID, name string, profile settings.AgentProfileView) (settings.SettingsMutationResult, error) {
	configData := settings.ExpandAgentProfileView(profile)
	return s.SaveAgentConfigData(userID, name, configData)
}

func (s *settingsMutationService) SaveAgentConfigData(userID, name string, data *config.AgentConfigData) (settings.SettingsMutationResult, error) {
	if err := s.blockIfAffectsActiveConfig("agent_config", name, []string{"agent_configs." + strings.TrimSpace(name)}, "当前存在运行中的会话，无法修改当前激活方案。"); err != nil {
		return settings.SettingsMutationResult{}, err
	}
	return settings.SaveAgentConfig(s.agentConfigManager, s.runtimeController, userID, name, data)
}

func (s *settingsMutationService) DeleteAgentConfig(userID, name string) (map[string]any, error) {
	if err := s.blockIfAffectsActiveConfig("agent_config_delete", name, []string{"agent_configs." + strings.TrimSpace(name)}, "当前存在运行中的会话，无法删除当前激活方案。"); err != nil {
		return nil, err
	}
	return settings.DeleteAgentConfig(s.agentConfigManager, s.runtimeController, userID, name)
}

func (s *settingsMutationService) ActivateAgentConfig(userID, name string) (settings.SettingsMutationResult, error) {
	if err := s.blockIfActiveRuns("agent_config_switch", []string{"agent_configs." + strings.TrimSpace(name)}, "当前存在运行中的会话，无法切换当前激活方案。"); err != nil {
		return settings.SettingsMutationResult{}, err
	}
	return settings.ActivateAgentConfig(s.agentConfigManager, s.runtimeController, userID, name)
}

func (s *settingsMutationService) AddAgentToConfig(configName, agentName string, agent config.AgentDefData) (map[string]any, error) {
	if err := s.blockIfAffectsActiveConfig("agent_config", configName, []string{"agent_configs." + strings.TrimSpace(configName)}, "当前存在运行中的会话，无法修改当前激活方案。"); err != nil {
		return nil, err
	}
	return settings.AddAgentToConfig(s.agentConfigManager, s.runtimeController, configName, agentName, agent)
}

func (s *settingsMutationService) UpdateAgentInConfig(configName, agentName string, agent config.AgentDefData) (settings.SettingsMutationResult, error) {
	if err := s.blockIfAffectsActiveConfig("agent_config", configName, []string{"agent_configs." + strings.TrimSpace(configName)}, "当前存在运行中的会话，无法修改当前激活方案。"); err != nil {
		return settings.SettingsMutationResult{}, err
	}
	return settings.UpdateAgentInConfig(s.agentConfigManager, s.runtimeController, configName, agentName, agent)
}

func (s *settingsMutationService) RemoveAgentFromConfig(configName, agentName string) (settings.SettingsMutationResult, error) {
	if err := s.blockIfAffectsActiveConfig("agent_config", configName, []string{"agent_configs." + strings.TrimSpace(configName)}, "当前存在运行中的会话，无法修改当前激活方案。"); err != nil {
		return settings.SettingsMutationResult{}, err
	}
	return settings.RemoveAgentFromConfig(s.agentConfigManager, s.runtimeController, configName, agentName)
}

func (s *settingsMutationService) ToggleSkillPackage(name string, enabled bool) (settings.SettingsMutationResult, error) {
	return s.ToggleSkillPackageWithScope(name, "", "", enabled)
}

func (s *settingsMutationService) ToggleSkillPackageWithScope(name, scope, configName string, enabled bool) (settings.SettingsMutationResult, error) {
	if s.skillCatalog == nil {
		return settings.SettingsMutationResult{}, newSkillCatalogUnavailableError("settings_mutation")
	}
	detail, err := s.skillPackageDetail(name, scope, configName)
	if err != nil {
		return settings.SettingsMutationResult{}, err
	}
	runtimeImpact := packageExecutableToolMutationRequiresRuntime(detail, detail.Enabled, enabled)
	if runtimeImpact {
		if err := s.blockIfActiveRuns("skill_package_toggle", []string{buildSkillAffectedPath(name)}, "当前存在运行中的会话，无法切换会影响工具执行的技能包。"); err != nil {
			return settings.SettingsMutationResult{}, err
		}
	}
	if err := s.setSkillPackageEnabled(name, scope, configName, enabled); err != nil {
		return settings.SettingsMutationResult{}, err
	}
	result := s.newMutationResult("skill_package_toggle")
	result.AffectedPaths = []string{buildSkillAffectedPath(name)}
	if runtimeImpact {
		result.RequiredAction = settings.MutationRequiredActionRuntimeRebuild
		settings.ApplyActiveRuntimeMutation(settings.NewMutationBuilder(s.runtimeDiagnostics()), s.runtimeApplyProvider(), &result, "技能包工具状态已保存，但运行时重建失败: ")
		return settings.FinalizeMutationResult(result), nil
	}
	settings.AppendMutationWarning(&result, skillRuntimeApplyWarning)
	return settings.FinalizeMutationResult(result), nil
}

func (s *settingsMutationService) MigrateExclusiveSkillPackages() (map[string]any, error) {
	if s.agentConfigManager == nil {
		return nil, newAgentConfigUnavailableError("settings_mutation")
	}
	if err := s.blockIfActiveRuns("skill_package_migrate", []string{"skills"}, "当前存在运行中的会话，无法迁移技能包归属。"); err != nil {
		return nil, err
	}
	result, err := s.agentConfigManager.MigrateExclusiveSkills()
	if err != nil {
		return nil, err
	}
	affectedPaths := make([]string, 0, len(result.Migrated))
	for _, item := range result.Migrated {
		affectedPaths = append(affectedPaths, buildSkillAffectedPath(item.PackageID))
	}
	mutation := s.newMutationResult("skill_package_migrate")
	if len(affectedPaths) > 0 {
		mutation.AffectedPaths = affectedPaths
		mutation.RequiredAction = settings.MutationRequiredActionRuntimeRebuild
		settings.ApplyActiveRuntimeMutation(settings.NewMutationBuilder(s.runtimeDiagnostics()), s.runtimeApplyProvider(), &mutation, "技能包迁移已保存，但运行时重建失败: ")
	}
	return settings.MutationEnvelope(map[string]any{
		"migrated": result.Migrated,
		"skipped":  result.Skipped,
	}, mutation), nil
}

func (s *settingsMutationService) skillPackageDetail(name, scope, configName string) (settings.SkillPackageDetail, error) {
	if scopedCatalog, ok := s.skillCatalog.(scopedSkillMutationCatalog); ok {
		return scopedCatalog.GetSkillPackageByScope(name, scope, configName)
	}
	return s.skillCatalog.GetSkillPackage(name)
}

func (s *settingsMutationService) setSkillPackageEnabled(name, scope, configName string, enabled bool) error {
	if scopedCatalog, ok := s.skillCatalog.(scopedSkillMutationCatalog); ok {
		return scopedCatalog.SetSkillPackageEnabledByScope(name, scope, configName, enabled)
	}
	return s.skillCatalog.SetSkillPackageEnabled(name, enabled)
}

func (s *settingsMutationService) SaveSkill(name, description, content string) (settings.SettingsMutationResult, error) {
	return s.SaveSkillWithScope(name, "", "", description, content)
}

func (s *settingsMutationService) SaveSkillWithScope(name, scope, configName, description, content string) (settings.SettingsMutationResult, error) {
	if s.skillCatalog == nil {
		return settings.SettingsMutationResult{}, newSkillCatalogUnavailableError("settings_mutation")
	}
	if scopedCatalog, ok := s.skillCatalog.(scopedSkillMutationCatalog); ok {
		if err := scopedCatalog.SaveSkillWithScope(name, scope, configName, description, content); err != nil {
			return settings.SettingsMutationResult{}, err
		}
	} else if err := s.skillCatalog.SaveSkill(name, description, content); err != nil {
		return settings.SettingsMutationResult{}, err
	}
	return s.newSkillMutationResult("skill", buildSkillAffectedPath(name)), nil
}

func (s *settingsMutationService) DeleteSkill(name string) (settings.SettingsMutationResult, error) {
	return s.DeleteSkillWithScope(name, "", "")
}

func (s *settingsMutationService) DeleteSkillWithScope(name, scope, configName string) (settings.SettingsMutationResult, error) {
	if s.skillCatalog == nil {
		return settings.SettingsMutationResult{}, newSkillCatalogUnavailableError("settings_mutation")
	}
	detail, err := s.skillPackageDetail(name, scope, configName)
	if err != nil {
		return settings.SettingsMutationResult{}, err
	}
	if packageExecutableToolsEnabled(detail) {
		if err := s.blockIfActiveRuns("skill_delete", []string{buildSkillAffectedPath(name)}, "当前存在运行中的会话，无法删除会影响工具执行的技能包。"); err != nil {
			return settings.SettingsMutationResult{}, err
		}
	}
	if scopedCatalog, ok := s.skillCatalog.(scopedSkillMutationCatalog); ok {
		if err := scopedCatalog.DeleteSkillWithScope(name, scope, configName); err != nil {
			return settings.SettingsMutationResult{}, err
		}
	} else if err := s.skillCatalog.DeleteSkill(name); err != nil {
		return settings.SettingsMutationResult{}, err
	}
	result := s.newSkillMutationResult("skill_delete", buildSkillAffectedPath(name))
	if packageExecutableToolsEnabled(detail) {
		result.RequiredAction = settings.MutationRequiredActionRuntimeRebuild
		settings.ApplyActiveRuntimeMutation(settings.NewMutationBuilder(s.runtimeDiagnostics()), s.runtimeApplyProvider(), &result, "技能包工具删除已保存，但运行时重建失败: ")
	}
	return settings.FinalizeMutationResult(result), nil
}

func (s *settingsMutationService) ImportSkill(sourcePath string, overwrite bool) (map[string]any, error) {
	return s.ImportSkillWithScope(sourcePath, overwrite, "", "")
}

func (s *settingsMutationService) ImportSkillWithScope(sourcePath string, overwrite bool, scope, configName string) (map[string]any, error) {
	if s.skillCatalog == nil {
		return nil, newSkillCatalogUnavailableError("settings_mutation")
	}
	if result, handled, err := s.importExtensionManifest(sourcePath); handled {
		if err != nil {
			return nil, err
		}
		mutation := s.newSkillMutationResult("skill_import", extractImportedSkillPaths(result)...)
		if s.importedExtensionRequiresRuntimeRebuild(result) {
			settings.ApplyActiveRuntimeMutation(settings.NewMutationBuilder(s.runtimeDiagnostics()), s.runtimeApplyProvider(), &mutation, "扩展技能包已导入，但运行时重建失败: ")
		}
		return settings.MutationEnvelope(result, mutation), nil
	}
	var (
		result map[string]any
		err    error
	)
	if scopedCatalog, ok := s.skillCatalog.(scopedSkillMutationCatalog); ok {
		result, err = scopedCatalog.ImportSkillWithScope(sourcePath, overwrite, scope, configName)
	} else {
		result, err = s.skillCatalog.ImportSkill(sourcePath, overwrite)
	}
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = map[string]any{}
	}
	return settings.MutationEnvelope(result, s.newSkillMutationResult("skill_import", extractImportedSkillPaths(result)...)), nil
}

func (s *settingsMutationService) importExtensionManifest(sourcePath string) (map[string]any, bool, error) {
	looksLikeManifest, detectErr := extension.LooksLikeExtensionManifest(strings.TrimSpace(sourcePath))
	if detectErr == nil && !looksLikeManifest {
		return nil, false, nil
	}
	manifest, err := extension.LoadFile(strings.TrimSpace(sourcePath))
	if err != nil {
		if detectErr == nil && looksLikeManifest {
			return nil, true, err
		}
		return nil, false, nil
	}
	managedCatalog, ok := s.skillCatalog.(extensionImportCatalog)
	if !ok {
		return nil, true, newSkillCatalogUnavailableError("settings_mutation")
	}
	configBaseDir := ""
	if s.agentConfigManager != nil {
		configBaseDir = s.agentConfigManager.ConfigBaseDir()
	}
	importResult, err := extension.ImportManifest(manifest, managedCatalog.ManagedSkillsDir(), managedCatalog.ManagedToolsDir(), configBaseDir)
	if err != nil {
		return nil, true, err
	}
	metadataPath, err := extension.RegisterManifest(manifest, importResult, configBaseDir)
	if err != nil {
		return nil, true, err
	}
	if !manifest.Spec.Activation.EnabledByDefault {
		for _, resource := range importResult.Resources {
			if !strings.EqualFold(strings.TrimSpace(resource.Type), extension.ResourceSkillPackage) {
				continue
			}
			if err := managedCatalog.SetSkillPackageEnabledByScope(resource.PackageID, resource.Scope, resource.OwnerConfig, false); err != nil {
				return nil, true, err
			}
		}
	}
	if s.snapshotRefresher != nil {
		s.snapshotRefresher.RequestRefresh(fmt.Sprintf("extension_import:%s", strings.TrimSpace(manifest.Metadata.Name)))
	}
	importedItems := make([]map[string]any, 0, len(importResult.Resources))
	for _, resource := range importResult.Resources {
		importedItems = append(importedItems, map[string]any{
			"name":          resource.PackageID,
			"type":          resource.Type,
			"source":        strings.TrimSpace(sourcePath),
			"source_dir":    resource.SourceDir,
			"target":        resource.TargetDir,
			"scope":         resource.Scope,
			"owner_config":  resource.OwnerConfig,
			"metadata_path": metadataPath,
			"manifest_name": strings.TrimSpace(manifest.Metadata.Name),
		})
	}
	result := map[string]any{
		"action":   "import",
		"mode":     "extension_manifest",
		"imported": importedItems,
		"skipped":  []map[string]any{},
	}
	return result, true, nil
}

func (s *settingsMutationService) importedSkillPackageDetail(name, scope, ownerConfig string) (settings.SkillPackageDetail, error) {
	if strings.TrimSpace(name) == "" {
		return settings.SkillPackageDetail{}, newSkillCatalogUnavailableError("settings_mutation")
	}
	if scopedCatalog, ok := s.skillCatalog.(scopedSkillMutationCatalog); ok {
		return scopedCatalog.GetSkillPackageByScope(name, scope, ownerConfig)
	}
	return s.skillCatalog.GetSkillPackage(name)
}

func (s *settingsMutationService) importedExtensionRequiresRuntimeRebuild(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	importedRaw, ok := payload["imported"].([]map[string]any)
	if !ok || len(importedRaw) == 0 {
		return false
	}
	for _, item := range importedRaw {
		resourceType, _ := item["type"].(string)
		if strings.EqualFold(strings.TrimSpace(resourceType), extension.ResourceToolPackage) {
			return true
		}
		if !strings.EqualFold(strings.TrimSpace(resourceType), extension.ResourceSkillPackage) && strings.TrimSpace(resourceType) != "" {
			continue
		}
		name, _ := item["name"].(string)
		scope, _ := item["scope"].(string)
		ownerConfig, _ := item["owner_config"].(string)
		detail, err := s.importedSkillPackageDetail(name, scope, ownerConfig)
		if err != nil {
			continue
		}
		if packageExecutableToolsEnabled(detail) {
			return true
		}
	}
	return false
}

func (s *settingsMutationService) listActiveRuns() []ActiveRunInfo {
	if s.activeRuns == nil {
		return nil
	}
	runs := s.activeRuns.ListActiveRuns()
	result := make([]ActiveRunInfo, 0, len(runs))
	for _, run := range runs {
		status := strings.ToLower(strings.TrimSpace(run.Status))
		if status != "running" && status != "interrupted" {
			continue
		}
		result = append(result, ActiveRunInfo{
			SessionID:        strings.TrimSpace(run.SessionID),
			Status:           status,
			BoundInstanceID:  strings.TrimSpace(run.BoundInstanceID),
			BoundProjectPath: strings.TrimSpace(run.BoundProjectPath),
		})
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func (s *settingsMutationService) blockIfActiveRuns(scope string, affectedPaths []string, message string) error {
	runs := s.listActiveRuns()
	if len(runs) == 0 {
		return nil
	}
	return newActiveRunBlockedError(s.runtimeDiagnostics(), scope, affectedPaths, runs, message)
}

func (s *settingsMutationService) blockIfAffectsActiveConfig(scope, configName string, affectedPaths []string, message string) error {
	target := strings.TrimSpace(configName)
	activeConfig := strings.TrimSpace(s.currentRuntimeActiveConfig())
	if target == "" || activeConfig == "" || !strings.EqualFold(target, activeConfig) {
		return nil
	}
	return s.blockIfActiveRuns(scope, affectedPaths, message)
}

func (s *settingsMutationService) currentRuntimeActiveConfig() string {
	if diagnostics := s.runtimeDiagnostics(); diagnostics != nil {
		return diagnostics.GetRuntimeActiveConfigName()
	}
	if s.agentConfigManager == nil {
		return ""
	}
	return s.agentConfigManager.GetActiveConfigName()
}

func (s *settingsMutationService) newMutationResult(scope string) settings.SettingsMutationResult {
	return settings.NewMutationBuilder(s.runtimeDiagnostics()).New(scope)
}

func (s *settingsMutationService) newSkillMutationResult(scope string, affectedPaths ...string) settings.SettingsMutationResult {
	result := s.newMutationResult(scope)
	for _, path := range affectedPaths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		result.AffectedPaths = append(result.AffectedPaths, trimmed)
	}
	settings.AppendMutationWarning(&result, skillRuntimeApplyWarning)
	return settings.FinalizeMutationResult(result)
}

func packageExecutableToolsEnabled(detail settings.SkillPackageDetail) bool {
	return strings.EqualFold(strings.TrimSpace(detail.Kind), skill.SkillPackageKindCategory) && detail.HasTools && detail.Enabled
}

func packageExecutableToolMutationRequiresRuntime(detail settings.SkillPackageDetail, beforeEnabled bool, afterEnabled bool) bool {
	if !strings.EqualFold(strings.TrimSpace(detail.Kind), skill.SkillPackageKindCategory) || !detail.HasTools {
		return false
	}
	return beforeEnabled != afterEnabled
}

func (s *settingsMutationService) runtimeApplyProvider() settings.RuntimeApplyProvider {
	if s.runtimeController == nil {
		return nil
	}
	return s.runtimeController
}

func (s *settingsMutationService) runtimeDiagnostics() settings.RuntimeDiagnosticsProvider {
	if s.runtimeController == nil {
		return nil
	}
	return s.runtimeController
}

func buildSkillAffectedPath(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "skills"
	}
	return "skills." + trimmed
}

func extractImportedSkillPaths(payload map[string]any) []string {
	if payload == nil {
		return []string{"skills"}
	}
	importedRaw, ok := payload["imported"].([]map[string]any)
	if !ok || len(importedRaw) == 0 {
		return []string{"skills"}
	}
	paths := make([]string, 0, len(importedRaw))
	for _, item := range importedRaw {
		name, _ := item["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		resourceType, _ := item["type"].(string)
		if strings.EqualFold(strings.TrimSpace(resourceType), extension.ResourceToolPackage) {
			paths = append(paths, "tools."+name)
			continue
		}
		paths = append(paths, buildSkillAffectedPath(name))
	}
	if len(paths) == 0 {
		return []string{"skills"}
	}
	return paths
}
