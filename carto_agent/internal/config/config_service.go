package config

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/autocarto/carto_agent/defaults"
	"github.com/autocarto/carto_agent/internal/llm"
	"github.com/autocarto/carto_agent/internal/logger"
	"gopkg.in/yaml.v3"
)

// ConfigService 配置服务
type ConfigService struct {
	db             *sql.DB
	config         *Config
	configPath     string // 默认配置文件路径（只读）
	userConfigPath string // 用户配置文件路径（写入目标）
	configRevision string
	secretsManager *SecretsManager
	runtimeLogDir  string
	mu             sync.RWMutex
}

type ConfigMutationResult struct {
	Revision string
	Changed  bool
}

type ConfigMigrationError struct {
	Operation string
	Cause     error
}

type Image2ConfigView struct {
	Enabled   bool   `json:"enabled"`
	Model     string `json:"model"`
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	HasAPIKey bool   `json:"has_api_key"`
}

var ErrInvalidImage2BaseURL = errors.New("invalid Image 2 base_url")

// NormalizeImage2BaseURL validates and normalizes the OpenAI-compatible Image 2 endpoint.
// An empty value keeps the official OpenAI endpoint as the default.
func NormalizeImage2BaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = Image2DefaultBaseURL
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" {
		return "", fmt.Errorf("%w: must be an absolute HTTP(S) URL", ErrInvalidImage2BaseURL)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: scheme must be http or https", ErrInvalidImage2BaseURL)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: credentials, query, and fragment are not allowed", ErrInvalidImage2BaseURL)
	}

	return strings.TrimRight(parsed.String(), "/"), nil
}

func (e *ConfigMigrationError) Error() string {
	if e == nil {
		return "config migration failed"
	}
	if e.Cause == nil {
		return fmt.Sprintf("config migration failed: %s", strings.TrimSpace(e.Operation))
	}
	return e.Cause.Error()
}

func (e *ConfigMigrationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func AsConfigMigrationError(err error) (*ConfigMigrationError, bool) {
	var target *ConfigMigrationError
	if !errors.As(err, &target) || target == nil {
		return nil, false
	}
	return target, true
}

// NewConfigService 创建配置服务
func NewConfigService(db *sql.DB, config *Config, configPath string, userConfigPath string) *ConfigService {
	configDir := filepath.Dir(userConfigPath)
	if strings.TrimSpace(userConfigPath) == "" {
		configDir = filepath.Dir(configPath)
	}

	// 创建 SecretsManager
	secretsMgr, err := NewSecretsManager(configDir)
	if err != nil {
		// 如果创建失败，记录警告但继续（API Key 功能将不可用）
		logger.Warn("Failed to create SecretsManager: %v", err)
	}

	// 确保用户配置目录存在
	if userConfigPath != "" {
		userDir := filepath.Dir(userConfigPath)
		os.MkdirAll(userDir, 0755)
	}

	service := &ConfigService{
		db:             db,
		config:         config,
		configPath:     configPath,
		userConfigPath: userConfigPath,
		secretsManager: secretsMgr,
		runtimeLogDir:  config.Log.Dir,
	}
	service.configRevision = computeConfigRevision(config)
	for _, warning := range service.GetRuntimeDirectoryDiagnostics().Warnings {
		logger.Warn("运行目录检测 code=%s message=%s", warning.Code, warning.Message)
	}
	return service
}

// GetConfig 获取配置值
func (cs *ConfigService) GetConfig(path string, userID string) (any, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if path == "llm" {
		return cs.buildLLMConfigView(true), nil
	}

	// 解析路径
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid path")
	}

	// 将配置转换为 map
	data, err := yaml.Marshal(cs.config)
	if err != nil {
		return nil, err
	}

	var configMap map[string]any
	if err := yaml.Unmarshal(data, &configMap); err != nil {
		return nil, err
	}

	// 遍历路径获取值
	var current any = configMap
	for _, part := range parts {
		if m, ok := current.(map[string]any); ok {
			current = m[part]
		} else {
			return nil, fmt.Errorf("path not found: %s", path)
		}
	}

	return current, nil
}

// buildLLMConfigView 组装返回给前端的 LLM 配置视图。
func (cs *ConfigService) buildLLMConfigView(includeAPIKey bool) map[string]any {
	secretKeys := map[string]bool{}

	if cs.secretsManager != nil {
		if secrets, err := cs.secretsManager.LoadSecrets(); err == nil {
			for name, key := range secrets.APIKeys {
				secretKeys[name] = strings.TrimSpace(key) != ""
			}
		}
	}

	providers := make([]any, 0, len(cs.config.LLM.Providers))
	for _, provider := range cs.config.LLM.Providers {
		apiKey := strings.TrimSpace(provider.APIKey)
		if cs.secretsManager != nil {
			if decryptedKey, err := cs.secretsManager.GetAPIKey(provider.Name); err == nil {
				apiKey = strings.TrimSpace(decryptedKey)
			}
		}
		hasKey := secretKeys[provider.Name] || apiKey != ""
		diagnostics := buildLLMProviderViewDiagnostics(provider, hasKey)
		providerView := map[string]any{
			"name":                        provider.Name,
			"provider":                    provider.Provider,
			"model":                       provider.Model,
			"base_url":                    provider.BaseURL,
			"max_tokens":                  provider.MaxTokens,
			"temperature":                 provider.Temperature,
			"thinking_enabled":            provider.ThinkingEnabled,
			"vision":                      provider.Vision,
			"thinking_budget":             provider.ThinkingBudget,
			"max_context_tokens":          provider.MaxContextTokens,
			"max_context_usage":           provider.MaxContextUsage,
			"auto_compress_threshold":     provider.AutoCompressThreshold,
			"compress_strategy":           provider.CompressStrategy,
			"has_api_key":                 hasKey,
			"supports_thinking":           diagnostics.SupportsThinking,
			"supports_plan_execute":       diagnostics.SupportsPlanExecute,
			"supports_forced_tool_choice": diagnostics.SupportsForcedToolChoice,
			"ready_for_chat":              diagnostics.ReadyForChat,
			"ready_for_plan":              diagnostics.ReadyForPlan,
			"ready_for_auto_title":        diagnostics.ReadyForAutoTitle,
			"readiness_reason":            diagnostics.ReadinessReason,
		}
		if includeAPIKey {
			providerView["api_key"] = apiKey
		}
		providers = append(providers, providerView)
	}

	return map[string]any{
		"active":      cs.config.LLM.Active,
		"quick_model": cs.config.LLM.QuickModel,
		"providers":   providers,
	}
}

type llmProviderViewDiagnostics struct {
	SupportsThinking         bool
	SupportsPlanExecute      bool
	SupportsForcedToolChoice bool
	ReadyForChat             bool
	ReadyForPlan             bool
	ReadyForAutoTitle        bool
	ReadinessReason          string
}

func buildLLMProviderViewDiagnostics(provider LLMProviderConfig, hasKey bool) llmProviderViewDiagnostics {
	result := llmProviderViewDiagnostics{}

	info, ok := llm.GetProviderInfo(provider.Provider)
	if !ok {
		result.ReadinessReason = fmt.Sprintf("unsupported provider: %s", strings.TrimSpace(provider.Provider))
		return result
	}

	result.SupportsThinking = info.SupportsThinking
	result.SupportsPlanExecute = info.SupportsPlanExecute
	result.SupportsForcedToolChoice = info.SupportsForcedToolChoice

	baseURL := strings.TrimSpace(provider.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(info.DefaultBaseURL)
	}

	switch {
	case info.RequiresAPIKey && !hasKey:
		result.ReadinessReason = fmt.Sprintf("API key not configured for provider: %s", info.DisplayName)
	case info.RequiresBaseURL && baseURL == "":
		result.ReadinessReason = fmt.Sprintf("base_url is required for provider: %s", info.DisplayName)
	default:
		result.ReadyForChat = true
		result.ReadyForAutoTitle = true
		result.ReadyForPlan = info.SupportsPlanExecute
	}

	return result
}

// GetProviderByName 根据配置名称获取 provider 配置（自动注入 API Key）。
func (cs *ConfigService) GetProviderByName(name string) (*LLMProviderConfig, error) {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return nil, fmt.Errorf("provider name is empty")
	}

	cs.mu.RLock()
	if cs.config == nil {
		cs.mu.RUnlock()
		return nil, fmt.Errorf("config not loaded")
	}

	var providerCopy *LLMProviderConfig
	for i := range cs.config.LLM.Providers {
		if cs.config.LLM.Providers[i].Name == trimmedName {
			copied := cs.config.LLM.Providers[i]
			providerCopy = &copied
			break
		}
	}
	secretsManager := cs.secretsManager
	cs.mu.RUnlock()

	if providerCopy == nil {
		return nil, fmt.Errorf("provider not found: %s", trimmedName)
	}

	if secretsManager != nil {
		if apiKey, err := secretsManager.GetAPIKey(providerCopy.Name); err == nil && strings.TrimSpace(apiKey) != "" {
			providerCopy.APIKey = apiKey
		}
	}

	return providerCopy, nil
}

// GetQuickProvider 获取快速模型配置（若未配置 quick_model 则回退 active）。
func (cs *ConfigService) GetQuickProvider() (*LLMProviderConfig, error) {
	cs.mu.RLock()
	if cs.config == nil {
		cs.mu.RUnlock()
		return nil, fmt.Errorf("config not loaded")
	}
	quickModel := strings.TrimSpace(cs.config.LLM.QuickModel)
	activeModel := strings.TrimSpace(cs.config.LLM.Active)
	cs.mu.RUnlock()

	providerName := quickModel
	if providerName == "" {
		providerName = activeModel
	}
	if providerName == "" {
		return nil, fmt.Errorf("active/quick model not configured")
	}

	return cs.GetProviderByName(providerName)
}

// GetAutoTitleMaxInputTokens 获取自动标题输入 token 上限。
func (cs *ConfigService) GetAutoTitleMaxInputTokens() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if cs.config == nil || cs.config.Session.AutoTitleMaxInputTokens <= 0 {
		return 512
	}
	return cs.config.Session.AutoTitleMaxInputTokens
}

func (cs *ConfigService) GetConfigRevision() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return strings.TrimSpace(cs.configRevision)
}

// GetImage2Config 返回独立于聊天 Provider 的 Image 2 工具配置。
func (cs *ConfigService) GetImage2Config() (Image2ConfigView, error) {
	cs.mu.RLock()
	if cs.config == nil {
		cs.mu.RUnlock()
		return Image2ConfigView{}, fmt.Errorf("config not loaded")
	}
	enabled := cs.config.Image2.IsEnabled()
	baseURL := strings.TrimSpace(cs.config.Image2.BaseURL)
	secretsManager := cs.secretsManager
	cs.mu.RUnlock()

	baseURL, err := NormalizeImage2BaseURL(baseURL)
	if err != nil {
		return Image2ConfigView{}, err
	}
	apiKey := ""
	if secretsManager != nil {
		if key, err := secretsManager.GetAPIKey(Image2SecretName); err == nil {
			apiKey = strings.TrimSpace(key)
		}
	}
	return Image2ConfigView{
		Enabled:   enabled,
		Model:     Image2ModelName,
		BaseURL:   baseURL,
		APIKey:    apiKey,
		HasAPIKey: apiKey != "",
	}, nil
}

// SaveImage2Config 保存启用状态和服务地址，并将 API Key 写入现有加密密钥机制。
func (cs *ConfigService) SaveImage2Config(enabled bool, apiKey, baseURL string) (ConfigMutationResult, error) {
	normalizedBaseURL, err := NormalizeImage2BaseURL(baseURL)
	if err != nil {
		return ConfigMutationResult{}, err
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.config == nil {
		return ConfigMutationResult{}, fmt.Errorf("config not loaded")
	}
	if cs.secretsManager == nil {
		return ConfigMutationResult{}, fmt.Errorf("secrets manager not initialized")
	}

	trimmedKey := strings.TrimSpace(apiKey)
	oldKey, _ := cs.secretsManager.GetAPIKey(Image2SecretName)
	oldKey = strings.TrimSpace(oldKey)
	secretChanged := oldKey != trimmedKey
	if secretChanged {
		var err error
		if trimmedKey == "" {
			err = cs.secretsManager.DeleteAPIKey(Image2SecretName)
		} else {
			err = cs.secretsManager.SetAPIKey(Image2SecretName, trimmedKey)
		}
		if err != nil {
			return ConfigMutationResult{}, fmt.Errorf("save Image 2 API key: %w", err)
		}
	}

	previous := *cs.config
	currentRevision := strings.TrimSpace(cs.configRevision)
	next := *cs.config
	next.Image2.Enabled = boolPtr(enabled)
	next.Image2.BaseURL = normalizedBaseURL
	nextRevision := computeConfigRevision(&next)
	configChanged := nextRevision != currentRevision
	if configChanged {
		*cs.config = next
		if err := cs.saveConfig(); err != nil {
			*cs.config = previous
			if secretChanged {
				if oldKey == "" {
					_ = cs.secretsManager.DeleteAPIKey(Image2SecretName)
				} else {
					_ = cs.secretsManager.SetAPIKey(Image2SecretName, oldKey)
				}
			}
			return ConfigMutationResult{}, err
		}
		cs.configRevision = nextRevision
	}

	return ConfigMutationResult{
		Revision: cs.configRevision,
		Changed:  secretChanged || configChanged,
	}, nil
}

// SetConfig 设置配置值
func (cs *ConfigService) SetConfig(path string, value any, userID string) (ConfigMutationResult, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	currentRevision := strings.TrimSpace(cs.configRevision)
	next := *cs.config
	if err := applyConfigMutation(&next, path, value); err != nil {
		return ConfigMutationResult{}, err
	}
	nextRevision := computeConfigRevision(&next)
	if nextRevision == currentRevision {
		return ConfigMutationResult{Revision: currentRevision, Changed: false}, nil
	}
	*cs.config = next

	// 保存到文件
	if err := cs.saveConfig(); err != nil {
		return ConfigMutationResult{}, err
	}
	cs.configRevision = nextRevision
	return ConfigMutationResult{Revision: nextRevision, Changed: true}, nil
}

// saveConfig 保存配置到用户配置文件（autocarto/config/config.yaml）
// 不再写入默认配置文件，不再调用 collapsePaths。
func (cs *ConfigService) saveConfig() error {
	if cs.userConfigPath == "" {
		return fmt.Errorf("user config path not set")
	}

	data, err := yaml.Marshal(cs.config)
	if err != nil {
		return err
	}

	return os.WriteFile(cs.userConfigPath, data, 0644)
}

// ReloadConfig 重新加载配置（双配置合并）
func (cs *ConfigService) ReloadConfig() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	var (
		defaultData []byte
		err         error
	)
	if strings.TrimSpace(cs.configPath) != "" {
		defaultData, err = os.ReadFile(cs.configPath)
		if err != nil {
			return wrapConfigMigrationError("read_default_config", fmt.Errorf("read default config: %w", err))
		}
	} else {
		defaultData, err = defaults.FS.ReadFile("config.yaml")
		if err != nil {
			return wrapConfigMigrationError("read_embedded_default_config", fmt.Errorf("read embedded default config: %w", err))
		}
	}

	var baseMap map[string]any
	if err := yaml.Unmarshal(defaultData, &baseMap); err != nil {
		return wrapConfigMigrationError("parse_default_config", fmt.Errorf("parse default config: %w", err))
	}

	// 加载用户配置并合并
	if cs.userConfigPath != "" {
		if userData, err := os.ReadFile(cs.userConfigPath); err == nil {
			var userMap map[string]any
			if err := yaml.Unmarshal(userData, &userMap); err == nil && userMap != nil {
				baseMap = mergeYAML(baseMap, userMap)
			}
		}
	}

	// 反序列化为 Config
	mergedData, err := yaml.Marshal(baseMap)
	if err != nil {
		return wrapConfigMigrationError("marshal_merged_config", fmt.Errorf("marshal merged config: %w", err))
	}

	if err := yaml.Unmarshal(mergedData, cs.config); err != nil {
		return wrapConfigMigrationError("parse_merged_config", fmt.Errorf("parse merged config: %w", err))
	}

	// 展开路径
	cs.config.expandPaths()
	cs.configRevision = computeConfigRevision(cs.config)
	return nil
}

// GetFullConfig 获取完整配置
func (cs *ConfigService) GetFullConfig() (*Config, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	// 深拷贝配置
	data, err := yaml.Marshal(cs.config)
	if err != nil {
		return nil, err
	}

	var configCopy Config
	if err := yaml.Unmarshal(data, &configCopy); err != nil {
		return nil, err
	}
	configCopy.ConfigBaseDir = cs.config.ConfigBaseDir
	if cs.secretsManager != nil {
		for i := range configCopy.LLM.Providers {
			if apiKey, err := cs.secretsManager.GetAPIKey(configCopy.LLM.Providers[i].Name); err == nil && strings.TrimSpace(apiKey) != "" {
				configCopy.LLM.Providers[i].APIKey = apiKey
			}
		}
	}

	return &configCopy, nil
}

// GetAllConfig 获取完整配置视图（LLM 部分不包含明文 API Key）
func (cs *ConfigService) GetAllConfig() (map[string]any, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	// 将配置转换为 map
	data, err := yaml.Marshal(cs.config)
	if err != nil {
		return nil, err
	}

	var configMap map[string]any
	if err := yaml.Unmarshal(data, &configMap); err != nil {
		return nil, err
	}

	// 替换 LLM 部分为安全视图
	configMap["llm"] = cs.buildLLMConfigView(false)

	return configMap, nil
}

// BatchSetConfig 批量设置配置项
func (cs *ConfigService) BatchSetConfig(items []ConfigItem) (ConfigMutationResult, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	currentRevision := strings.TrimSpace(cs.configRevision)
	next := *cs.config

	// 逐项设置
	for _, item := range items {
		if err := applyConfigMutation(&next, item.Path, item.Value); err != nil {
			return ConfigMutationResult{}, fmt.Errorf("设置 %s 失败: %w", item.Path, err)
		}
	}
	nextRevision := computeConfigRevision(&next)
	if nextRevision == currentRevision {
		return ConfigMutationResult{Revision: currentRevision, Changed: false}, nil
	}
	*cs.config = next

	if err := cs.saveConfig(); err != nil {
		return ConfigMutationResult{}, err
	}
	cs.configRevision = nextRevision
	return ConfigMutationResult{Revision: nextRevision, Changed: true}, nil
}

// ConfigItem 配置项（用于批量设置）
type ConfigItem struct {
	Path  string `json:"path"`
	Value any    `json:"value"`
}

func normalizeConfigWriteValue(path string, value any) any {
	return value
}

func applyConfigMutation(cfg *Config, path string, value any) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	trimmedPath := strings.TrimSpace(path)
	switch trimmedPath {
	case "plan.confirm":
		text, err := coerceString(value)
		if err != nil {
			return err
		}
		next := CartographicPlanConfig{Confirm: text}
		if err := next.Validate(); err != nil {
			return err
		}
		cfg.Plan = next
	case "acquisition":
		if value == nil {
			return fmt.Errorf("acquisition 必须是配置对象")
		}
		raw, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		next := cfg.Acquisition.WithDefaults()
		fallback := *next.AutoFallback
		next.AutoFallback = &fallback
		decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
		decoder.KnownFields(true)
		if err := decoder.Decode(&next); err != nil {
			return err
		}
		if err := next.Validate(); err != nil {
			return err
		}
		cfg.Acquisition = next
	case "integrations":
		integrations, err := decodeIntegrationsConfig(value)
		if err != nil {
			return err
		}
		cfg.Integrations = integrations
	case "log.level":
		text, err := coerceString(value)
		if err != nil {
			return err
		}
		cfg.Log.Level = text
	case "log.dir":
		text, err := coerceString(value)
		if err != nil {
			return err
		}
		cfg.Log.Dir = text
	case "log.max_size":
		number, err := coerceInt64(value)
		if err != nil {
			return err
		}
		cfg.Log.MaxSize = number
	case "log.max_backups":
		number, err := coerceInt(value)
		if err != nil {
			return err
		}
		cfg.Log.MaxBackups = number
	case "log.max_age":
		number, err := coerceInt(value)
		if err != nil {
			return err
		}
		cfg.Log.MaxAge = number
	case "log.max_total_size":
		number, err := coerceInt64(value)
		if err != nil {
			return err
		}
		cfg.Log.MaxTotalSize = number
	case "log.compress":
		flag, err := coerceBool(value)
		if err != nil {
			return err
		}
		cfg.Log.Compress = flag
	case "performance.tool_call_timeout":
		number, err := coerceInt(value)
		if err != nil {
			return err
		}
		cfg.Performance.ToolCallTimeout = number
	case "performance.agent_response_timeout":
		number, err := coerceInt(value)
		if err != nil {
			return err
		}
		cfg.Performance.AgentResponseTimeout = number
	case "performance.max_concurrent_tasks":
		number, err := coerceInt(value)
		if err != nil {
			return err
		}
		cfg.Performance.MaxConcurrentTasks = number
	case "agent.loop_max_iterations":
		number, err := coerceInt(value)
		if err != nil {
			return err
		}
		cfg.Agent.LoopMaxIterations = number
	case "data_dir":
		text, err := coerceString(value)
		if err != nil {
			return err
		}
		cfg.DataDir = text
	case "qgis.executable_path":
		text, err := coerceString(value)
		if err != nil {
			return err
		}
		cfg.QGIS.ExecutablePath = text
	case "qgis.recent_projects":
		projects, err := coerceRecentProjects(value)
		if err != nil {
			return err
		}
		cfg.QGIS.RecentProjects = projects
	case "image2.enabled":
		flag, err := coerceBool(value)
		if err != nil {
			return err
		}
		cfg.Image2.Enabled = boolPtr(flag)
	case "session.default_name_pattern":
		text, err := coerceString(value)
		if err != nil {
			return err
		}
		cfg.Session.DefaultNamePattern = text
	case "session.default_execution_mode":
		text, err := coerceString(value)
		if err != nil {
			return err
		}
		normalized := NormalizeSessionExecutionMode(text)
		if normalized == "" {
			return fmt.Errorf("unsupported session execution mode: %s", text)
		}
		cfg.Session.DefaultExecutionMode = normalized
	case "session.auto_title_enabled":
		flag, err := coerceBool(value)
		if err != nil {
			return err
		}
		cfg.Session.AutoTitleEnabled = boolPtr(flag)
	case "session.auto_snapshot_before_compress":
		flag, err := coerceBool(value)
		if err != nil {
			return err
		}
		cfg.Session.AutoSnapshotBeforeCompress = boolPtr(flag)
	case "session.max_snapshots_per_session":
		number, err := coerceInt(value)
		if err != nil {
			return err
		}
		cfg.Session.MaxSnapshotsPerSession = number
	case "session.auto_archive.enabled":
		flag, err := coerceBool(value)
		if err != nil {
			return err
		}
		cfg.Session.AutoArchive.Enabled = boolPtr(flag)
	case "session.auto_archive.days":
		number, err := coerceInt(value)
		if err != nil {
			return err
		}
		cfg.Session.AutoArchive.Days = number
	case "session.auto_cleanup.enabled":
		flag, err := coerceBool(value)
		if err != nil {
			return err
		}
		cfg.Session.AutoCleanup.Enabled = boolPtr(flag)
	case "session.auto_cleanup.archive_retention_days":
		number, err := coerceInt(value)
		if err != nil {
			return err
		}
		cfg.Session.AutoCleanup.ArchiveRetentionDays = number
	case "services.lifecycle.auto_start_on_app_launch":
		flag, err := coerceBool(value)
		if err != nil {
			return err
		}
		cfg.Services.Lifecycle.AutoStartOnAppLaunch = boolPtr(flag)
	case "services.lifecycle.auto_stop_on_app_exit":
		flag, err := coerceBool(value)
		if err != nil {
			return err
		}
		cfg.Services.Lifecycle.AutoStopOnAppExit = boolPtr(flag)
	case "services.lifecycle.auto_restart_on_failure":
		flag, err := coerceBool(value)
		if err != nil {
			return err
		}
		cfg.Services.Lifecycle.AutoRestartOnFailure = boolPtr(flag)
	case "ui.main_panel.visible":
		flag, err := coerceBool(value)
		if err != nil {
			return err
		}
		cfg.UI.MainPanel.Visible = boolPtr(flag)
	case "ports.agent_system.default":
		number, err := coerceInt(value)
		if err != nil {
			return err
		}
		cfg.Ports.AgentSystem.Default = number
	case "compute.port":
		number, err := coerceInt(value)
		if err != nil {
			return err
		}
		cfg.Compute.Port = number
	default:
		return fmt.Errorf("unsupported config path: %s", trimmedPath)
	}
	return nil
}

func coerceString(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("expected string value, got %T", value)
	}
	return text, nil
}

func coerceBool(value any) (bool, error) {
	flag, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("expected bool value, got %T", value)
	}
	return flag, nil
}

func coerceInt(value any) (int, error) {
	number, ok := toInt(value)
	if !ok {
		return 0, fmt.Errorf("expected integer-compatible value, got %T", value)
	}
	return number, nil
}

func coerceInt64(value any) (int64, error) {
	number, err := coerceInt(value)
	if err != nil {
		return 0, err
	}
	return int64(number), nil
}

func coerceRecentProjects(value any) ([]QGISRecentProject, error) {
	items, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]QGISRecentProject); ok {
			return typed, nil
		}
		return nil, fmt.Errorf("expected recent projects array, got %T", value)
	}

	projects := make([]QGISRecentProject, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected recent project object, got %T", item)
		}
		path, err := coerceString(entry["path"])
		if err != nil {
			return nil, fmt.Errorf("recent project path: %w", err)
		}
		name, err := coerceString(entry["name"])
		if err != nil {
			return nil, fmt.Errorf("recent project name: %w", err)
		}
		lastOpened, err := coerceString(entry["last_opened"])
		if err != nil {
			return nil, fmt.Errorf("recent project last_opened: %w", err)
		}
		projects = append(projects, QGISRecentProject{
			Path:       path,
			Name:       name,
			LastOpened: lastOpened,
		})
	}
	return projects, nil
}

// SetAPIKey 设置 API Key 到加密存储
func (cs *ConfigService) SetAPIKey(providerName, apiKey string) error {
	if cs.secretsManager == nil {
		return fmt.Errorf("secrets manager not initialized")
	}
	return cs.secretsManager.SetAPIKey(providerName, apiKey)
}

// GetAPIKey 从加密存储获取 API Key
func (cs *ConfigService) GetAPIKey(providerName string) (string, error) {
	if cs.secretsManager == nil {
		return "", fmt.Errorf("secrets manager not initialized")
	}
	return cs.secretsManager.GetAPIKey(providerName)
}

// DeleteAPIKey 从加密存储删除 API Key
func (cs *ConfigService) DeleteAPIKey(providerName string) error {
	if cs.secretsManager == nil {
		return fmt.Errorf("secrets manager not initialized")
	}
	return cs.secretsManager.DeleteAPIKey(providerName)
}

// SaveLLMConfig 保存完整的 LLM 配置（包括 API Key）
func (cs *ConfigService) SaveLLMConfig(llmConfig map[string]any) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// 提取 providers 列表
	providers, ok := llmConfig["providers"].([]any)
	if !ok {
		return fmt.Errorf("invalid providers format")
	}

	// 保存每个 provider 的 API Key 到加密存储
	for _, p := range providers {
		provider, ok := p.(map[string]any)
		if !ok {
			continue
		}

		name := strings.TrimSpace(getString(provider, "name"))
		apiKey := strings.TrimSpace(getString(provider, "api_key"))

		if name != "" && cs.secretsManager != nil {
			if apiKey == "" {
				if err := cs.secretsManager.DeleteAPIKey(name); err != nil {
					return fmt.Errorf("delete API key for %s: %w", name, err)
				}
			} else if err := cs.secretsManager.SetAPIKey(name, apiKey); err != nil {
				return fmt.Errorf("save API key for %s: %w", name, err)
			}
		}
	}

	active := strings.TrimSpace(getString(llmConfig, "active"))
	quickModel := strings.TrimSpace(getString(llmConfig, "quick_model"))

	// 先构建 providers 列表，校验通过后再回写到运行时配置
	nextProviders := make([]LLMProviderConfig, 0, len(providers))
	providerNames := make(map[string]struct{}, len(providers))
	for _, p := range providers {
		provider, ok := p.(map[string]any)
		if !ok {
			continue
		}

		cfg := LLMProviderConfig{
			Name:                  strings.TrimSpace(getString(provider, "name")),
			Provider:              llm.NormalizeProviderID(getString(provider, "provider")),
			Model:                 getString(provider, "model"),
			BaseURL:               getString(provider, "base_url"),
			MaxTokens:             4096,
			Temperature:           0.7,
			ThinkingEnabled:       false,
			ThinkingBudget:        10240,
			MaxContextTokens:      128000,
			MaxContextUsage:       90,
			AutoCompressThreshold: 85,
			CompressStrategy:      "summary",
		}
		if cfg.Name == "" {
			return fmt.Errorf("provider name is required")
		}
		if cfg.Provider == "" {
			cfg.Provider = "openai"
		}
		if _, ok := llm.GetProviderInfo(cfg.Provider); !ok {
			return fmt.Errorf("unsupported provider: %s", cfg.Provider)
		}
		if _, exists := providerNames[cfg.Name]; exists {
			return fmt.Errorf("duplicate provider name: %s", cfg.Name)
		}

		if mt, ok := toInt(provider["max_tokens"]); ok {
			cfg.MaxTokens = mt
		}
		if temp, ok := toFloat(provider["temperature"]); ok {
			cfg.Temperature = temp
		}
		if vision, ok := provider["vision"].(bool); ok {
			cfg.Vision = vision
		}
		if thinkingEnabled, ok := provider["thinking_enabled"].(bool); ok {
			cfg.ThinkingEnabled = thinkingEnabled
		}
		if thinkingBudget, ok := toInt(provider["thinking_budget"]); ok {
			cfg.ThinkingBudget = thinkingBudget
		}
		if contextTokens, ok := toInt(provider["max_context_tokens"]); ok {
			cfg.MaxContextTokens = contextTokens
		}
		if contextUsage, ok := toInt(provider["max_context_usage"]); ok {
			cfg.MaxContextUsage = contextUsage
		}
		if compressThreshold, ok := toInt(provider["auto_compress_threshold"]); ok {
			cfg.AutoCompressThreshold = compressThreshold
		}
		if strategy := strings.TrimSpace(getString(provider, "compress_strategy")); strategy != "" {
			cfg.CompressStrategy = strategy
		}

		providerNames[cfg.Name] = struct{}{}

		nextProviders = append(nextProviders, cfg)
	}

	if active != "" {
		if _, ok := providerNames[active]; !ok {
			return fmt.Errorf("active model not found in providers: %s", active)
		}
	}
	if quickModel != "" {
		if _, ok := providerNames[quickModel]; !ok {
			return fmt.Errorf("quick model not found in providers: %s", quickModel)
		}
	}

	cs.config.LLM.Providers = nextProviders
	cs.config.LLM.Active = active
	cs.config.LLM.QuickModel = quickModel

	// 保存配置到文件
	if err := cs.saveConfig(); err != nil {
		return err
	}
	cs.configRevision = computeConfigRevision(cs.config)
	return nil
}

// SetActiveLLMProvider 仅切换当前激活的 provider，不改动其它配置。
func (cs *ConfigService) SetActiveLLMProvider(providerName string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return fmt.Errorf("provider name is empty")
	}

	found := false
	for _, provider := range cs.config.LLM.Providers {
		if provider.Name == providerName {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("provider not found: %s", providerName)
	}

	cs.config.LLM.Active = providerName
	if err := cs.saveConfig(); err != nil {
		return err
	}
	cs.configRevision = computeConfigRevision(cs.config)
	return nil
}

func wrapConfigMigrationError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &ConfigMigrationError{
		Operation: strings.TrimSpace(operation),
		Cause:     err,
	}
}

func computeConfigRevision(cfg *Config) string {
	if cfg == nil {
		return "cfg-0"
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "cfg-0"
	}
	sum := sha256.Sum256(data)
	return "cfg-" + hex.EncodeToString(sum[:8])
}

// getString 从 map 中安全获取字符串
func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// toInt 从 JSON 反序列化值中提取整数
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

// toFloat 从 JSON 反序列化值中提取浮点数
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
