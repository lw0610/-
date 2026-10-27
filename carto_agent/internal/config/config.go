package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/defaults"
	"github.com/autocarto/carto_agent/internal/llm"
	"gopkg.in/yaml.v3"
)

// Config 系统配置
type Config struct {
	// LLM配置
	LLM LLMConfig `yaml:"llm"`

	FlowCompile FlowCompileConfig `yaml:"flow_compile"`

	Acquisition AcquisitionConfig `yaml:"acquisition"`

	// Image 2 工具服务配置（不参与聊天模型选择）
	Image2 Image2Config `yaml:"image2"`

	Integrations IntegrationsConfig `yaml:"integrations"`

	// 端口配置
	Ports PortsConfig `yaml:"ports"`

	// 日志配置
	Log LogConfig `yaml:"log"`

	// 智能体配置
	Agent AgentConfig `yaml:"agent"`

	// 性能配置
	Performance PerformanceConfig `yaml:"performance"`

	// 无头计算服务配置
	Compute ComputeConfig `yaml:"compute"`

	// Plan 模式独立配置
	PlanMode PlanModeConfig `yaml:"plan_mode"`

	// 服务管理配置
	Services ServicesConfig `yaml:"services"`

	// 会话策略配置
	Session SessionConfig `yaml:"session"`

	// UI 配置（用于透传/持久化，不在 Go 端使用）
	UI UIConfig `yaml:"ui"`

	// QGIS 桌面端相关配置
	QGIS QGISConfig `yaml:"qgis"`

	// 数据目录
	DataDir string `yaml:"data_dir"`

	// 配置基础目录（默认 autocarto/config）
	ConfigBaseDir string `yaml:"-"`
}

type FlowCompileConfig struct {
	Provider           string `yaml:"provider"`
	Concurrency        int    `yaml:"concurrency"`
	CallTimeoutSeconds int    `yaml:"call_timeout_seconds"`
	JobTimeoutSeconds  int    `yaml:"job_timeout_seconds"`
	RetryAfterSeconds  int    `yaml:"retry_after_seconds"`
	WarmEnabledFlows   bool   `yaml:"warm_enabled_flows"`
}

func (c FlowCompileConfig) WithDefaults() FlowCompileConfig {
	if c.Concurrency <= 0 {
		c.Concurrency = 3
	}
	if c.CallTimeoutSeconds <= 0 {
		c.CallTimeoutSeconds = 90
	}
	if c.JobTimeoutSeconds <= 0 {
		c.JobTimeoutSeconds = 900
	}
	if c.RetryAfterSeconds <= 0 {
		c.RetryAfterSeconds = 600
	}
	return c
}

const (
	Image2ModelName      = "gpt-image-2"
	Image2DefaultBaseURL = "https://api.openai.com/v1"
	Image2SecretName     = "image2"
)

// Image2Config 只保存非敏感的 Image 2 工具配置。
// API Key 复用 SecretsManager，不会写入 YAML。
type Image2Config struct {
	Enabled *bool  `yaml:"enabled"`
	BaseURL string `yaml:"base_url"`
}

func (c Image2Config) IsEnabled() bool {
	return c.Enabled != nil && *c.Enabled
}

// LLMConfig LLM配置
type LLMConfig struct {
	Active     string              `yaml:"active"`      // 当前激活的对话配置名称
	QuickModel string              `yaml:"quick_model"` // 快速模型配置名称（为空时回退 active）
	Providers  []LLMProviderConfig `yaml:"providers"`   // 提供商配置列表
}

// LLMProviderConfig 单个 LLM 提供商配置
type LLMProviderConfig struct {
	Vision                bool    `yaml:"vision"`                  // 该模型支持图片输入，由用户明确声明。
	Name                  string  `yaml:"name"`                    // 配置名称
	Provider              string  `yaml:"provider"`                // 提供商类型 (openai/anthropic/google/ollama)
	Model                 string  `yaml:"model"`                   // 模型名称
	BaseURL               string  `yaml:"base_url"`                // 自定义API地址
	MaxTokens             int     `yaml:"max_tokens"`              // 最大token数
	Temperature           float64 `yaml:"temperature"`             // 温度参数
	ThinkingEnabled       bool    `yaml:"thinking_enabled"`        // 是否启用深度思考
	ThinkingBudget        int     `yaml:"thinking_budget"`         // 深度思考预算
	MaxContextTokens      int     `yaml:"max_context_tokens"`      // 上下文窗口
	MaxContextUsage       int     `yaml:"max_context_usage"`       // 最大上下文使用比例
	AutoCompressThreshold int     `yaml:"auto_compress_threshold"` // 自动压缩阈值
	CompressStrategy      string  `yaml:"compress_strategy"`       // 压缩策略
	APIKey                string  `yaml:"-"`                       // API密钥（从加密存储加载，不序列化到YAML）
}

// PortsConfig 端口配置
type PortsConfig struct {
	AgentSystem PortConfig `yaml:"agent_system"` // 智能体系统端口
}

// PortConfig 单个端口配置
type PortConfig struct {
	Default             int    `yaml:"default"`               // 默认端口
	Range               []int  `yaml:"range"`                 // 端口范围 [min, max]
	Protocol            string `yaml:"protocol"`              // 协议类型
	Description         string `yaml:"description"`           // 描述
	HealthCheckInterval int    `yaml:"health_check_interval"` // 端口健康检查间隔（秒）
	BindRetryCount      int    `yaml:"bind_retry_count"`      // 端口绑定失败重试次数
	BindRetryDelay      int    `yaml:"bind_retry_delay"`      // 端口绑定重试间隔（毫秒）
	AutoFallback        *bool  `yaml:"auto_fallback"`         // 端口冲突时是否自动回退到 range
}

// GetPort 获取端口值（兼容旧格式）
func (p *PortConfig) GetPort() int {
	if p.Default > 0 {
		return p.Default
	}
	return 0
}

// IsAutoFallbackEnabled 返回端口冲突时是否启用自动回退（默认 true）。
func (p PortConfig) IsAutoFallbackEnabled() bool {
	if p.AutoFallback == nil {
		return true
	}
	return *p.AutoFallback
}

// RangeBounds 返回端口范围边界。
func (p PortConfig) RangeBounds() (int, int, bool) {
	if len(p.Range) != 2 {
		return 0, 0, false
	}
	minPort := p.Range[0]
	maxPort := p.Range[1]
	if minPort > maxPort {
		minPort, maxPort = maxPort, minPort
	}
	if minPort <= 0 || maxPort > 65535 {
		return 0, 0, false
	}
	return minPort, maxPort, true
}

// LogConfig 日志配置
type LogConfig struct {
	Level        string `yaml:"level"`          // DEBUG, INFO, WARN, ERROR
	Dir          string `yaml:"dir"`            // 日志目录
	MaxSize      int64  `yaml:"max_size"`       // 单个日志文件最大大小（字节）
	MaxBackups   int    `yaml:"max_backups"`    // 最大备份文件数
	MaxAge       int    `yaml:"max_age"`        // 日志文件最大保留天数
	MaxTotalSize int64  `yaml:"max_total_size"` // 所有日志文件最大总大小（字节）
	Compress     bool   `yaml:"compress"`       // 是否压缩旧日志文件
}

// AgentConfig 智能体配置
type AgentConfig struct {
	// Agentic Loop 配置
	LoopMaxIterations int   `yaml:"loop_max_iterations"` // 最大迭代次数
	UseSkillEngine    *bool `yaml:"use_skill_engine"`
}

func (a AgentConfig) IsSkillEngineEnabled() bool {
	return a.UseSkillEngine != nil && *a.UseSkillEngine
}

// PerformanceConfig 性能配置
type PerformanceConfig struct {
	ToolCallTimeout      int                     `yaml:"tool_call_timeout"`      // 工具调用超时(ms)
	AgentResponseTimeout int                     `yaml:"agent_response_timeout"` // 智能体响应超时(ms)
	MaxConcurrentTasks   int                     `yaml:"max_concurrent_tasks"`   // 最大并发任务数
	Memory               PerformanceMemoryConfig `yaml:"memory"`                 // 内存策略
	Cache                PerformanceCacheConfig  `yaml:"cache"`                  // 缓存策略
}

// PerformanceMemoryConfig 运行时内存策略
type PerformanceMemoryConfig struct {
	Enabled         *bool `yaml:"enabled"`            // 是否启用内存阈值策略
	MaxTaskMemoryMB int   `yaml:"max_task_memory_mb"` // 单任务内存上限（MB）
}

// IsEnabled 返回内存策略是否启用（默认 true）
func (m PerformanceMemoryConfig) IsEnabled() bool {
	if m.Enabled == nil {
		return true
	}
	return *m.Enabled
}

// PerformanceCacheConfig 运行时缓存策略
type PerformanceCacheConfig struct {
	Enabled   *bool  `yaml:"enabled"`     // 是否启用缓存
	TTL       int    `yaml:"ttl"`         // 缓存过期时间（秒）
	MaxSizeMB int    `yaml:"max_size_mb"` // 缓存最大容量（MB）
	Strategy  string `yaml:"strategy"`    // 缓存策略（lru/lfu/fifo）
}

// IsEnabled 返回缓存策略是否启用（默认 true）
func (c PerformanceCacheConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

type ComputeConfig struct {
	Port       int        `yaml:"port"`
	PortConfig PortConfig `yaml:"port_config"`
	MaxWorkers int        `yaml:"max_workers"` // 最大 worker 数
	PythonPath string     `yaml:"python_path"` // 可选：指定 Python 解释器路径
}

// GetEffectivePort 获取有效端口（PortConfig 优先，回退到 Port 字段）
func (c ComputeConfig) GetEffectivePort() int {
	if p := c.PortConfig.GetPort(); p > 0 {
		return p
	}
	return c.Port
}

// PlanModeConfig Plan 模式全局配置
type PlanModeConfig struct {
	Enabled *bool `yaml:"enabled"`
}

// IsEnabled 返回 Plan 模式是否启用（默认 true）。
func (p PlanModeConfig) IsEnabled() bool {
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}

// ServicesConfig 服务管理配置
type ServicesConfig struct {
	Lifecycle ServiceLifecycleConfig `yaml:"lifecycle"`
}

// ServiceLifecycleConfig 服务生命周期策略
type ServiceLifecycleConfig struct {
	AutoStartOnAppLaunch *bool `yaml:"auto_start_on_app_launch"`
	AutoStopOnAppExit    *bool `yaml:"auto_stop_on_app_exit"`
	AutoRestartOnFailure *bool `yaml:"auto_restart_on_failure"`
}

// SessionConfig 会话策略配置
type SessionConfig struct {
	DefaultNamePattern         string                   `yaml:"default_name_pattern"`
	DefaultExecutionMode       string                   `yaml:"default_execution_mode"`
	AutoTitleEnabled           *bool                    `yaml:"auto_title_enabled"`
	AutoTitleMaxInputTokens    int                      `yaml:"auto_title_max_input_tokens"`
	AutoSnapshotBeforeCompress *bool                    `yaml:"auto_snapshot_before_compress"`
	MaxSnapshotsPerSession     int                      `yaml:"max_snapshots_per_session"`
	AutoArchive                SessionAutoArchiveConfig `yaml:"auto_archive"`
	AutoCleanup                SessionAutoCleanupConfig `yaml:"auto_cleanup"`
	Timeout                    SessionTimeoutConfig     `yaml:"timeout"`
	Concurrency                SessionConcurrencyConfig `yaml:"concurrency"`
	Recovery                   SessionRecoveryConfig    `yaml:"recovery"`
}

// SessionAutoArchiveConfig 会话自动归档配置
type SessionAutoArchiveConfig struct {
	Enabled *bool `yaml:"enabled"`
	Days    int   `yaml:"days"`
}

// SessionAutoCleanupConfig 会话自动清理配置
type SessionAutoCleanupConfig struct {
	Enabled              *bool `yaml:"enabled"`
	ArchiveRetentionDays int   `yaml:"archive_retention_days"`
}

// SessionTimeoutConfig 会话超时配置
type SessionTimeoutConfig struct {
	IdleTimeoutSeconds int `yaml:"idle_timeout_seconds"` // 空闲超时（秒），0 表示禁用
	MaxDurationSeconds int `yaml:"max_duration_seconds"` // 会话最大持续时长（秒），0 表示禁用
}

// SessionConcurrencyConfig 会话并发限制配置
type SessionConcurrencyConfig struct {
	MaxActiveSessions     int `yaml:"max_active_sessions"`      // 最大活跃会话数，0 表示不限制
	MaxMessagesPerSession int `yaml:"max_messages_per_session"` // 单会话最大消息数，0 表示不限制
}

// SessionRecoveryConfig 会话恢复配置
type SessionRecoveryConfig struct {
	AutoSaveIntervalSeconds int   `yaml:"auto_save_interval_seconds"` // 自动保存间隔（秒）
	EnableCrashRecovery     *bool `yaml:"enable_crash_recovery"`      // 启用崩溃恢复
}

// IsCrashRecoveryEnabled 返回是否启用崩溃恢复（默认 true）。
func (c SessionRecoveryConfig) IsCrashRecoveryEnabled() bool {
	if c.EnableCrashRecovery == nil {
		return true
	}
	return *c.EnableCrashRecovery
}

// UIConfig UI 配置
type UIConfig struct {
	MainPanel MainPanelConfig `yaml:"main_panel"`
}

// MainPanelConfig 主面板配置（透传）
type MainPanelConfig struct {
	Visible      *bool          `yaml:"visible"`
	Floating     *bool          `yaml:"floating"`
	Area         int            `yaml:"area"`
	Geometry     map[string]any `yaml:"geometry"`
	Tabified     *bool          `yaml:"tabified"`
	TabifiedWith string         `yaml:"tabified_with"`
}

// QGISConfig QGIS 启动与最近工程配置
type QGISConfig struct {
	ExecutablePath string              `yaml:"executable_path"`
	RecentProjects []QGISRecentProject `yaml:"recent_projects"`
}

// QGISRecentProject 最近打开工程记录
type QGISRecentProject struct {
	Path       string `yaml:"path"`
	Name       string `yaml:"name"`
	LastOpened string `yaml:"last_opened"`
}

// Load 加载配置（双配置合并：默认配置 + 用户配置）
// 配置加载优先级：
//  1. 文件系统 default 配置（开发模式）
//  2. go:embed 内嵌默认配置（sidecar fallback）
//  3. autocarto/config/config.yaml（用户配置覆盖）
func Load() (*Config, error) {
	var baseMap map[string]any

	// 1. 尝试从文件系统加载默认配置
	defaultPath := findConfigFile()
	if defaultPath != "" {
		// 从文件加载（开发模式）
		data, err := os.ReadFile(defaultPath)
		if err != nil {
			return nil, fmt.Errorf("read default config: %w", err)
		}
		if err := yaml.Unmarshal(data, &baseMap); err != nil {
			return nil, fmt.Errorf("parse default config: %w", err)
		}
	} else {
		// 2. 使用内嵌默认配置（sidecar 模式 fallback）
		writeConfigNotice("[INFO] 未找到文件系统配置，使用内嵌默认配置")
		embeddedData, readErr := defaults.FS.ReadFile("config.yaml")
		if readErr != nil {
			return nil, fmt.Errorf("read embedded config: %w", readErr)
		}
		if err := yaml.Unmarshal(embeddedData, &baseMap); err != nil {
			return nil, fmt.Errorf("parse embedded config: %w", err)
		}
	}

	// 3. 加载用户配置（如存在），深度合并覆盖默认值
	userPath := FindUserConfigFile()
	if userPath != "" {
		if userData, err := os.ReadFile(userPath); err == nil {
			var userMap map[string]any
			if err := yaml.Unmarshal(userData, &userMap); err == nil && userMap != nil {
				baseMap = mergeYAML(baseMap, userMap)
			}
		}
	}

	// 4. 将合并后的 map 反序列化为 Config 结构体
	mergedData, err := yaml.Marshal(baseMap)
	if err != nil {
		return nil, fmt.Errorf("marshal merged config: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(mergedData, cfg); err != nil {
		return nil, fmt.Errorf("parse merged config: %w", err)
	}

	// 5. 设置默认值、展开路径
	cfg.setDefaults()
	cfg.expandPaths()

	// 6. 加载 API 密钥
	configDir, err := ResolveSecretsDir()
	if err != nil {
		return nil, err
	}
	if err := cfg.loadSecrets(configDir); err != nil {
		return nil, fmt.Errorf("load secrets: %w", err)
	}

	// 7. 验证配置
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	// 8. 设置 ConfigBaseDir
	cfg.ConfigBaseDir, err = ResolveAutoCartoConfigDir()
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate 验证配置
// 返回错误列表，第一个是致命错误（如果有），其余是警告
func (c *Config) Validate() error {
	// 验证 LLM 配置（非致命，允许服务启动后通过设置页面配置）
	if c.LLM.Active == "" {
		// 仅警告，不阻止启动
		writeConfigNotice("[WARN] LLM active config not set, please configure via settings")
	} else {
		// 查找激活的提供商配置
		activeProvider := c.GetActiveProvider()
		if activeProvider == nil {
			writeConfigNotice("[WARN] Active LLM provider not found: %s", c.LLM.Active)
		} else {
			providerInfo, providerKnown := llm.GetProviderInfo(activeProvider.Provider)
			if activeProvider.Provider == "" {
				writeConfigNotice("[WARN] LLM provider type not set")
			}
			if activeProvider.Model == "" {
				writeConfigNotice("[WARN] LLM model not set")
			}
			if providerKnown && providerInfo.RequiresAPIKey && activeProvider.APIKey == "" {
				writeConfigNotice("[WARN] LLM API key not set for: %s, please configure via settings", c.LLM.Active)
			}
			if providerKnown && providerInfo.RequiresBaseURL &&
				strings.TrimSpace(activeProvider.BaseURL) == "" &&
				strings.TrimSpace(providerInfo.DefaultBaseURL) == "" {
				writeConfigNotice("[WARN] LLM base_url not set for: %s, please configure via settings", c.LLM.Active)
			}
		}
	}

	if quick := strings.TrimSpace(c.LLM.QuickModel); quick != "" {
		if c.GetProviderByName(quick) == nil {
			writeConfigNotice("[WARN] LLM quick model provider not found: %s", quick)
		}
	}
	for _, provider := range c.LLM.Providers {
		if strings.TrimSpace(provider.Name) == "" {
			return fmt.Errorf("llm.providers.name 不能为空")
		}
		if strings.TrimSpace(provider.Provider) == "" {
			return fmt.Errorf("llm.providers[%s].provider 不能为空", provider.Name)
		}
		if _, ok := llm.GetProviderInfo(provider.Provider); !ok {
			return fmt.Errorf("llm.providers[%s].provider 不受支持: %s", provider.Name, provider.Provider)
		}
	}

	// 验证端口配置（致命错误，必须正确）
	if err := validatePortConfig("agent system", c.Ports.AgentSystem); err != nil {
		return err
	}
	if c.Compute.Port < 0 || c.Compute.Port > 65535 {
		return fmt.Errorf("invalid compute service port: %d", c.Compute.Port)
	}

	if c.Performance.ToolCallTimeout <= 0 {
		return fmt.Errorf("performance.tool_call_timeout must be > 0")
	}
	if c.Performance.AgentResponseTimeout <= 0 {
		return fmt.Errorf("performance.agent_response_timeout must be > 0")
	}
	if c.Performance.MaxConcurrentTasks <= 0 {
		return fmt.Errorf("performance.max_concurrent_tasks must be > 0")
	}

	if c.Performance.Memory.IsEnabled() && c.Performance.Memory.MaxTaskMemoryMB <= 0 {
		return fmt.Errorf("performance.memory.max_task_memory_mb must be > 0 when memory policy is enabled")
	}
	if c.Performance.Cache.IsEnabled() {
		if c.Performance.Cache.TTL <= 0 {
			return fmt.Errorf("performance.cache.ttl must be > 0 when cache is enabled")
		}
		if c.Performance.Cache.MaxSizeMB <= 0 {
			return fmt.Errorf("performance.cache.max_size_mb must be > 0 when cache is enabled")
		}
		strategy := strings.ToLower(strings.TrimSpace(c.Performance.Cache.Strategy))
		switch strategy {
		case "lru", "lfu", "fifo":
		default:
			return fmt.Errorf("performance.cache.strategy must be one of: lru, lfu, fifo")
		}
	}

	if c.Session.Timeout.IdleTimeoutSeconds < 0 {
		return fmt.Errorf("session.timeout.idle_timeout_seconds cannot be negative")
	}
	if c.Session.Timeout.MaxDurationSeconds < 0 {
		return fmt.Errorf("session.timeout.max_duration_seconds cannot be negative")
	}
	if c.Session.Concurrency.MaxActiveSessions < 0 {
		return fmt.Errorf("session.concurrency.max_active_sessions cannot be negative")
	}
	if c.Session.Concurrency.MaxMessagesPerSession < 0 {
		return fmt.Errorf("session.concurrency.max_messages_per_session cannot be negative")
	}
	if c.Session.Recovery.AutoSaveIntervalSeconds <= 0 {
		return fmt.Errorf("session.recovery.auto_save_interval_seconds must be > 0")
	}

	return nil
}

// ValidateLLMConfig 验证 LLM 配置是否完整（用于实际调用 LLM 前检查）
func (c *Config) ValidateLLMConfig() error {
	if c.LLM.Active == "" {
		return fmt.Errorf("LLM active config not set")
	}

	activeProvider := c.GetActiveProvider()
	if activeProvider == nil {
		return fmt.Errorf("active LLM provider not found: %s", c.LLM.Active)
	}

	if activeProvider.Provider == "" {
		return fmt.Errorf("LLM provider type not set")
	}
	providerInfo, ok := llm.GetProviderInfo(activeProvider.Provider)
	if !ok {
		return fmt.Errorf("unsupported LLM provider: %s", activeProvider.Provider)
	}
	if activeProvider.Model == "" {
		return fmt.Errorf("LLM model not set")
	}
	if providerInfo.RequiresAPIKey && activeProvider.APIKey == "" {
		return fmt.Errorf("LLM API key not set for: %s", c.LLM.Active)
	}
	if providerInfo.RequiresBaseURL &&
		strings.TrimSpace(activeProvider.BaseURL) == "" &&
		strings.TrimSpace(providerInfo.DefaultBaseURL) == "" {
		return fmt.Errorf("LLM base_url not set for: %s", c.LLM.Active)
	}

	return nil
}

// GetActiveProvider 获取当前激活的 LLM 提供商配置
func (c *Config) GetActiveProvider() *LLMProviderConfig {
	for i := range c.LLM.Providers {
		if c.LLM.Providers[i].Name == c.LLM.Active {
			return &c.LLM.Providers[i]
		}
	}
	return nil
}

// GetProviderByName 根据配置名称获取 LLM 提供商配置
func (c *Config) GetProviderByName(name string) *LLMProviderConfig {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil
	}
	for i := range c.LLM.Providers {
		if c.LLM.Providers[i].Name == trimmed {
			return &c.LLM.Providers[i]
		}
	}
	return nil
}

// FindConfigFile 查找默认配置文件（导出版本）
func FindConfigFile() string {
	return findConfigFile()
}

// FindUserConfigFile 返回用户配置文件路径 (autocarto/config/config.yaml)
func FindUserConfigFile() string {
	path, err := ResolveUserConfigFile()
	if err != nil {
		return ""
	}
	return path
}

// mergeYAML 深度合并两个 map，override 中的值覆盖 base 中的值
func mergeYAML(base, override map[string]any) map[string]any {
	result := make(map[string]any, len(base))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		if baseVal, exists := result[k]; exists {
			baseMap, baseIsMap := baseVal.(map[string]any)
			overMap, overIsMap := v.(map[string]any)
			if baseIsMap && overIsMap {
				result[k] = mergeYAML(baseMap, overMap)
				continue
			}
		}
		result[k] = v
	}
	return result
}

func validatePortConfig(name string, cfg PortConfig) error {
	port := cfg.GetPort()
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid %s port: %d", name, port)
	}

	if len(cfg.Range) != 2 {
		return fmt.Errorf("invalid %s port range: expected [min,max], got %v", name, cfg.Range)
	}
	if _, _, ok := cfg.RangeBounds(); !ok {
		return fmt.Errorf("invalid %s port range bounds: %v", name, cfg.Range)
	}

	if cfg.HealthCheckInterval <= 0 {
		return fmt.Errorf("%s health_check_interval must be > 0", name)
	}
	if cfg.BindRetryCount < 0 {
		return fmt.Errorf("%s bind_retry_count cannot be negative", name)
	}
	if cfg.BindRetryDelay <= 0 {
		return fmt.Errorf("%s bind_retry_delay must be > 0", name)
	}

	return nil
}

// findConfigFile 查找配置文件
func findConfigFile() string {
	paths := []string{
		"defaults/config.yaml",
		"../defaults/config.yaml",
		"../../defaults/config.yaml",
		"carto_agent/defaults/config.yaml",
		"../carto_agent/defaults/config.yaml",
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// setDefaults 设置默认值
func (c *Config) setDefaults() {
	c.Acquisition = c.Acquisition.WithDefaults()
	c.FlowCompile = c.FlowCompile.WithDefaults()
	c.Integrations.SetDefaults()
	if c.Image2.Enabled == nil {
		c.Image2.Enabled = boolPtr(true)
	}
	if strings.TrimSpace(c.Image2.BaseURL) == "" {
		c.Image2.BaseURL = Image2DefaultBaseURL
	}

	// 端口默认值
	if c.Ports.AgentSystem.Default == 0 {
		c.Ports.AgentSystem.Default = 3000
	}
	if len(c.Ports.AgentSystem.Range) != 2 {
		c.Ports.AgentSystem.Range = []int{50000, 50099}
	}
	if strings.TrimSpace(c.Ports.AgentSystem.Protocol) == "" {
		c.Ports.AgentSystem.Protocol = "websocket"
	}
	if strings.TrimSpace(c.Ports.AgentSystem.Description) == "" {
		c.Ports.AgentSystem.Description = "智能体系统 WebSocket 服务"
	}
	if c.Ports.AgentSystem.HealthCheckInterval <= 0 {
		c.Ports.AgentSystem.HealthCheckInterval = 30
	}
	if c.Ports.AgentSystem.BindRetryCount <= 0 {
		c.Ports.AgentSystem.BindRetryCount = 3
	}
	if c.Ports.AgentSystem.BindRetryDelay <= 0 {
		c.Ports.AgentSystem.BindRetryDelay = 1000
	}
	if c.Ports.AgentSystem.AutoFallback == nil {
		c.Ports.AgentSystem.AutoFallback = boolPtr(true)
	}

	// 日志默认值
	if c.Log.Level == "" {
		c.Log.Level = "INFO"
	}
	if c.Log.Dir == "" {
		c.Log.Dir = "data/logs"
	}
	if c.Log.MaxSize == 0 {
		c.Log.MaxSize = 10 * 1024 * 1024
	}
	if c.Log.MaxBackups == 0 {
		c.Log.MaxBackups = 5
	}
	if c.Log.MaxAge == 0 {
		c.Log.MaxAge = 30
	}
	if c.Log.MaxTotalSize == 0 {
		c.Log.MaxTotalSize = 100 * 1024 * 1024
	}

	// 智能体默认值
	if c.Agent.LoopMaxIterations == 0 {
		c.Agent.LoopMaxIterations = 100
	}
	if c.Agent.UseSkillEngine == nil {
		c.Agent.UseSkillEngine = boolPtr(true)
	}

	// 性能默认值
	if c.Performance.ToolCallTimeout == 0 {
		c.Performance.ToolCallTimeout = 30000
	}
	if c.Performance.AgentResponseTimeout == 0 {
		c.Performance.AgentResponseTimeout = 120000
	}
	if c.Performance.MaxConcurrentTasks == 0 {
		c.Performance.MaxConcurrentTasks = 5
	}
	if c.Performance.Memory.Enabled == nil {
		c.Performance.Memory.Enabled = boolPtr(true)
	}
	if c.Performance.Memory.MaxTaskMemoryMB <= 0 {
		c.Performance.Memory.MaxTaskMemoryMB = 2048
	}
	if c.Performance.Cache.Enabled == nil {
		c.Performance.Cache.Enabled = boolPtr(true)
	}
	if c.Performance.Cache.TTL <= 0 {
		c.Performance.Cache.TTL = 300
	}
	if c.Performance.Cache.MaxSizeMB <= 0 {
		c.Performance.Cache.MaxSizeMB = 100
	}
	if strategy := strings.ToLower(strings.TrimSpace(c.Performance.Cache.Strategy)); strategy == "" {
		c.Performance.Cache.Strategy = "lru"
	} else {
		c.Performance.Cache.Strategy = strategy
	}

	if c.Compute.MaxWorkers == 0 {
		c.Compute.MaxWorkers = 2
	}
	c.Compute.PythonPath = strings.TrimSpace(c.Compute.PythonPath)

	if c.PlanMode.Enabled == nil {
		c.PlanMode.Enabled = boolPtr(true)
	}

	// 路径默认值
	if c.DataDir == "" {
		c.DataDir = "data"
	}
	if c.QGIS.RecentProjects == nil {
		c.QGIS.RecentProjects = []QGISRecentProject{}
	}
	// 服务生命周期默认值（指针 nil 才设置，避免覆盖用户显式 false）
	if c.Services.Lifecycle.AutoStartOnAppLaunch == nil {
		c.Services.Lifecycle.AutoStartOnAppLaunch = boolPtr(true)
	}
	if c.Services.Lifecycle.AutoStopOnAppExit == nil {
		c.Services.Lifecycle.AutoStopOnAppExit = boolPtr(true)
	}
	if c.Services.Lifecycle.AutoRestartOnFailure == nil {
		c.Services.Lifecycle.AutoRestartOnFailure = boolPtr(true)
	}

	// 会话策略默认值
	if strings.TrimSpace(c.Session.DefaultNamePattern) == "" {
		c.Session.DefaultNamePattern = "会话 MM-DD HH:mm"
	}
	if mode := NormalizeSessionExecutionMode(c.Session.DefaultExecutionMode); mode == "" {
		c.Session.DefaultExecutionMode = "build"
	} else {
		c.Session.DefaultExecutionMode = mode
	}
	if c.Session.AutoTitleEnabled == nil {
		c.Session.AutoTitleEnabled = boolPtr(true)
	}
	if c.Session.AutoTitleMaxInputTokens <= 0 {
		c.Session.AutoTitleMaxInputTokens = 512
	}
	if c.Session.AutoSnapshotBeforeCompress == nil {
		c.Session.AutoSnapshotBeforeCompress = boolPtr(true)
	}
	if c.Session.MaxSnapshotsPerSession <= 0 {
		c.Session.MaxSnapshotsPerSession = 10
	}
	if c.Session.AutoArchive.Enabled == nil {
		c.Session.AutoArchive.Enabled = boolPtr(false)
	}
	if c.Session.AutoArchive.Days <= 0 {
		c.Session.AutoArchive.Days = 30
	}
	if c.Session.AutoCleanup.Enabled == nil {
		c.Session.AutoCleanup.Enabled = boolPtr(false)
	}
	if c.Session.AutoCleanup.ArchiveRetentionDays <= 0 {
		c.Session.AutoCleanup.ArchiveRetentionDays = 90
	}
	if c.Session.Timeout.IdleTimeoutSeconds < 0 {
		c.Session.Timeout.IdleTimeoutSeconds = 0
	}
	if c.Session.Timeout.MaxDurationSeconds < 0 {
		c.Session.Timeout.MaxDurationSeconds = 0
	}
	if c.Session.Concurrency.MaxActiveSessions < 0 {
		c.Session.Concurrency.MaxActiveSessions = 0
	}
	if c.Session.Concurrency.MaxMessagesPerSession < 0 {
		c.Session.Concurrency.MaxMessagesPerSession = 0
	}
	if c.Session.Recovery.AutoSaveIntervalSeconds <= 0 {
		c.Session.Recovery.AutoSaveIntervalSeconds = 60
	}
	if c.Session.Recovery.EnableCrashRecovery == nil {
		c.Session.Recovery.EnableCrashRecovery = boolPtr(true)
	}

	// LLM 提供商默认值
	for i := range c.LLM.Providers {
		c.LLM.Providers[i].Provider = llm.NormalizeProviderID(c.LLM.Providers[i].Provider)
		if c.LLM.Providers[i].Provider == "" {
			c.LLM.Providers[i].Provider = "openai"
		}
		if c.LLM.Providers[i].MaxTokens == 0 {
			c.LLM.Providers[i].MaxTokens = 4096
		}
		if c.LLM.Providers[i].Temperature == 0 {
			c.LLM.Providers[i].Temperature = 0.7
		}
		if c.LLM.Providers[i].MaxContextTokens == 0 {
			c.LLM.Providers[i].MaxContextTokens = 128000
		}
		if c.LLM.Providers[i].MaxContextUsage == 0 {
			c.LLM.Providers[i].MaxContextUsage = 90
		}
		if c.LLM.Providers[i].AutoCompressThreshold == 0 {
			c.LLM.Providers[i].AutoCompressThreshold = 85
		}
		if c.LLM.Providers[i].CompressStrategy == "" {
			c.LLM.Providers[i].CompressStrategy = "summary"
		}
		if c.LLM.Providers[i].ThinkingBudget <= 0 {
			c.LLM.Providers[i].ThinkingBudget = 10240
		}
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func NormalizeSessionExecutionMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "plan", "build":
		return strings.ToLower(strings.TrimSpace(mode))
	case "plan_only":
		return "plan"
	case "execute":
		return "build"
	default:
		return ""
	}
}

func NormalizeSessionRunIntent(intent string) string {
	switch strings.ToLower(strings.TrimSpace(intent)) {
	case "plan_only", "plan":
		return "plan_only"
	case "execute", "build":
		return "execute"
	default:
		return ""
	}
}

func ExecutionModeToRunIntent(mode string) string {
	switch NormalizeSessionExecutionMode(mode) {
	case "plan":
		return "plan_only"
	case "build":
		return "execute"
	default:
		return ""
	}
}

func RunIntentToExecutionMode(intent string) string {
	switch NormalizeSessionRunIntent(intent) {
	case "plan_only":
		return "plan"
	case "execute":
		return "build"
	default:
		return ""
	}
}

func writeConfigNotice(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// expandPath 展开路径中的~ 或相对路径
func expandPath(path string) string {
	execPath, err := os.Executable()
	if err != nil {
		execPath = ""
	}
	return expandPathWithExecutable(path, execPath)
}

func expandPathFromBase(path string, baseDir string, execPath string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return normalizeLegacyMacOSBundleResourcePath(path)
	}
	if strings.HasPrefix(trimmed, "~") || filepath.IsAbs(trimmed) {
		return expandPathWithExecutable(trimmed, execPath)
	}
	base := strings.TrimSpace(baseDir)
	if base != "" {
		return normalizeLegacyMacOSBundleResourcePath(filepath.Join(base, filepath.Clean(trimmed)))
	}
	return expandPathWithExecutable(trimmed, execPath)
}

func expandPathWithExecutable(path string, execPath string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			if strings.HasPrefix(path, "~/") {
				return filepath.Join(home, strings.TrimPrefix(path, "~/"))
			}
			if strings.HasPrefix(path, "~\\") {
				return filepath.Join(home, strings.TrimPrefix(path, "~\\"))
			}
		}
	}

	if filepath.IsAbs(path) || strings.TrimSpace(path) == "" {
		return normalizeLegacyMacOSBundleResourcePath(path)
	}

	var candidates []string
	if isEphemeralGoBuildExecutable(execPath) {
		candidates = append(expandRelativePathCandidatesFromWorkingDir(path), expandRelativePathCandidates(execPath, path)...)
	} else {
		candidates = append(expandRelativePathCandidates(execPath, path), expandRelativePathCandidatesFromWorkingDir(path)...)
	}
	candidates = dedupePaths(candidates)
	for _, candidate := range candidates {
		normalized := normalizeLegacyMacOSBundleResourcePath(candidate)
		if pathExists(normalized) {
			return normalized
		}
	}

	if len(candidates) > 0 {
		return normalizeLegacyMacOSBundleResourcePath(candidates[0])
	}
	return normalizeLegacyMacOSBundleResourcePath(path)
}

func isEphemeralGoBuildExecutable(execPath string) bool {
	trimmed := strings.TrimSpace(execPath)
	if trimmed == "" {
		return false
	}
	cleaned := filepath.Clean(trimmed)
	return strings.Contains(cleaned, string(filepath.Separator)+"go-build")
}

func expandRelativePathCandidatesFromWorkingDir(path string) []string {
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		return nil
	}
	cleanedRel := filepath.Clean(path)
	return dedupePaths([]string{
		filepath.Join(wd, cleanedRel),
		filepath.Join(wd, "..", cleanedRel),
		filepath.Join(wd, "..", "..", cleanedRel),
	})
}

func expandRelativePathCandidates(execPath string, path string) []string {
	trimmedExec := strings.TrimSpace(execPath)
	if trimmedExec == "" {
		return nil
	}

	cleanedRel := filepath.Clean(path)
	execDir := filepath.Dir(filepath.Clean(trimmedExec))
	candidates := make([]string, 0, 5)
	if resourcesDir := detectMacOSBundleResourcesDir(execDir); resourcesDir != "" {
		candidates = append(candidates, filepath.Join(resourcesDir, cleanedRel))
	}
	candidates = append(candidates, filepath.Join(execDir, "..", cleanedRel))
	candidates = append(candidates, filepath.Join(execDir, "..", "..", cleanedRel))
	candidates = append(candidates, filepath.Join(execDir, "..", "..", "..", cleanedRel))
	candidates = append(candidates, filepath.Join(execDir, cleanedRel))
	return dedupePaths(candidates)
}

func detectMacOSBundleResourcesDir(execDir string) string {
	cleaned := filepath.Clean(execDir)
	if filepath.Base(cleaned) != "MacOS" {
		return ""
	}
	contentsDir := filepath.Dir(cleaned)
	if filepath.Base(contentsDir) != "Contents" {
		return ""
	}
	return filepath.Join(contentsDir, "Resources")
}

func normalizeLegacyMacOSBundleResourcePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return path
	}
	return filepath.Clean(trimmed)
}

func dedupePaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, item := range paths {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		cleaned := filepath.Clean(trimmed)
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}
	return result
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// expandPaths 展开配置中的所有路径
func (c *Config) expandPaths() {
	execPath, err := os.Executable()
	if err != nil {
		execPath = ""
	}
	pluginRoot := ""
	if resolved, resolveErr := ResolveAutoCartoHome(); resolveErr == nil {
		pluginRoot = resolved
	}
	c.Log.Dir = expandPathFromBase(c.Log.Dir, pluginRoot, execPath)
	c.DataDir = expandPathFromBase(c.DataDir, pluginRoot, execPath)
	if strings.TrimSpace(c.QGIS.ExecutablePath) != "" {
		c.QGIS.ExecutablePath = expandPath(c.QGIS.ExecutablePath)
	}
	if strings.TrimSpace(c.Compute.PythonPath) != "" {
		original := c.Compute.PythonPath
		c.Compute.PythonPath = filepath.Clean(expandPath(c.Compute.PythonPath))
		if c.Compute.PythonPath != original {
			writeConfigNotice("[INFO] compute.python_path 已规范化: %s -> %s", original, c.Compute.PythonPath)
		}
	}
}

// loadSecrets 从加密存储加载 API 密钥
func (c *Config) loadSecrets(configDir string) error {
	mgr, err := NewSecretsManager(configDir)
	if err != nil {
		return fmt.Errorf("create secrets manager: %w", err)
	}

	secrets, err := mgr.LoadSecrets()
	if err != nil {
		return fmt.Errorf("load secrets: %w", err)
	}

	// 将密钥注入到对应的提供商配置
	for i := range c.LLM.Providers {
		name := c.LLM.Providers[i].Name
		if key, ok := secrets.APIKeys[name]; ok {
			c.LLM.Providers[i].APIKey = key
		}
	}

	return nil
}
