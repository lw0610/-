package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/defaults"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/logger"
	"gopkg.in/yaml.v3"
)

const (
	AgentConfigVersionV10 = "1.0"

	PlanToolStrategyReadonlyOnly = "readonly_only"
	PlanToolStrategyReadonlyPlus = "readonly_plus"
	PlanToolStrategyAll          = "all"
)

// StoredAgentConfig 存储的智能体配置信息。
type StoredAgentConfig struct {
	ID         int64     `json:"id"`
	UserID     string    `json:"user_id"`
	ConfigName string    `json:"config_name"`
	IsDefault  bool      `json:"is_default"`
	IsActive   bool      `json:"is_active"`
	ConfigData string    `json:"config_data"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// AgentConfigMeta 智能体方案级别元配置。
type AgentConfigMeta struct {
	DefaultExecutionMode string `yaml:"default_execution_mode" json:"default_execution_mode"`
	Description          string `yaml:"description" json:"description"`
	PlanEnabled          bool   `yaml:"plan_enabled" json:"plan_enabled"`
}

// AgentConfigFile YAML 配置文件结构。
type AgentConfigFile struct {
	Version string          `yaml:"version"`
	Meta    AgentConfigMeta `yaml:"meta"`
	Runtime *RuntimeConfig  `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Plan    *PlanConfig     `yaml:"plan,omitempty" json:"plan,omitempty"`
}

// RuntimeConfig 统一运行配置。
type RuntimeConfig struct {
	Global GlobalAgentConfig       `yaml:"global" json:"global"`
	Agents map[string]AgentDefData `yaml:"agents" json:"agents"`
}

// PlanConfig Plan 模式配置。
type PlanConfig struct {
	MaxIterations int                         `yaml:"max_iterations" json:"max_iterations"`
	Global        GlobalAgentConfig           `yaml:"global" json:"global"`
	Agents        map[string]PlanAgentDefData `yaml:"agents" json:"agents"`
}

// UnmarshalYAML 兼容历史 plan.behavior.max_iterations 写法。
func (p *PlanConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawPlanConfig struct {
		MaxIterations int                         `yaml:"max_iterations"`
		Global        GlobalAgentConfig           `yaml:"global"`
		Agents        map[string]PlanAgentDefData `yaml:"agents"`
		Behavior      struct {
			MaxIterations int `yaml:"max_iterations"`
		} `yaml:"behavior"`
	}

	var raw rawPlanConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}

	maxIterations := raw.MaxIterations
	if maxIterations <= 0 && raw.Behavior.MaxIterations > 0 {
		maxIterations = raw.Behavior.MaxIterations
	}

	*p = PlanConfig{
		MaxIterations: maxIterations,
		Global:        raw.Global,
		Agents:        raw.Agents,
	}
	return nil
}

// GlobalAgentConfig 全局智能体配置。
type GlobalAgentConfig struct {
	SkillPackages []string `yaml:"skill_packages,omitempty" json:"skill_packages"`
}

func (c *GlobalAgentConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawGlobalAgentConfig struct {
		SkillPackages        []string `yaml:"skill_packages,omitempty"`
		CommonSkills         []string `yaml:"common_skills,omitempty"`
		CommonCategorySkills []string `yaml:"common_category_skills,omitempty"`
		CommonWorkflowSkills []string `yaml:"common_workflow_skills,omitempty"`
	}
	var raw rawGlobalAgentConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*c = GlobalAgentConfig{
		SkillPackages: mergeSkillPackageLists(
			raw.SkillPackages,
			raw.CommonSkills,
			raw.CommonCategorySkills,
			raw.CommonWorkflowSkills,
		),
	}
	return nil
}

// AgentDefData 单个 Runtime 子智能体定义。
type AgentDefData struct {
	DisplayName     string            `yaml:"display_name" json:"display_name"`
	Description     string            `yaml:"description" json:"description"`
	IsMain          bool              `yaml:"is_main,omitempty" json:"is_main"`
	Deletable       bool              `yaml:"deletable,omitempty" json:"deletable"`
	Enabled         bool              `yaml:"enabled" json:"enabled"`
	LLM             AgentLLMConfig    `yaml:"llm,omitempty" json:"llm,omitempty"`
	WorkflowPackage string            `yaml:"workflow_package,omitempty" json:"workflow_package,omitempty"`
	SkillPackages   []string          `yaml:"skill_packages,omitempty" json:"skill_packages,omitempty"`
	Prompts         map[string]string `yaml:"prompts" json:"prompts"`
}

func (a *AgentDefData) UnmarshalYAML(value *yaml.Node) error {
	type rawAgentDefData struct {
		DisplayName     string            `yaml:"display_name"`
		Description     string            `yaml:"description"`
		IsMain          bool              `yaml:"is_main,omitempty"`
		Deletable       bool              `yaml:"deletable,omitempty"`
		Enabled         bool              `yaml:"enabled"`
		LLM             AgentLLMConfig    `yaml:"llm,omitempty"`
		WorkflowPackage string            `yaml:"workflow_package,omitempty"`
		SkillPackages   []string          `yaml:"skill_packages,omitempty"`
		SkillPolicy     SkillPolicyData   `yaml:"skill_policy,omitempty"`
		Prompts         map[string]string `yaml:"prompts"`
	}
	var raw rawAgentDefData
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*a = AgentDefData{
		DisplayName:     raw.DisplayName,
		Description:     raw.Description,
		IsMain:          raw.IsMain,
		Deletable:       raw.Deletable,
		Enabled:         raw.Enabled,
		LLM:             raw.LLM,
		WorkflowPackage: strings.TrimSpace(raw.WorkflowPackage),
		SkillPackages:   mergeSkillPackageLists(raw.SkillPackages, skillPackagesFromPolicy(raw.SkillPolicy)),
		Prompts:         raw.Prompts,
	}
	return nil
}

// AgentLLMConfig 智能体独立 LLM 配置。
type AgentLLMConfig struct {
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
}

// PlanAgentDefData Plan 子智能体定义。
type PlanAgentDefData struct {
	DisplayName   string            `yaml:"display_name" json:"display_name"`
	IsMain        bool              `yaml:"is_main,omitempty" json:"is_main"`
	Mode          string            `yaml:"mode,omitempty" json:"mode,omitempty"`
	LLM           AgentLLMConfig    `yaml:"llm,omitempty" json:"llm,omitempty"`
	SkillPackages []string          `yaml:"skill_packages,omitempty" json:"skill_packages,omitempty"`
	Prompts       map[string]string `yaml:"prompts" json:"prompts"`
}

func (p *PlanAgentDefData) UnmarshalYAML(value *yaml.Node) error {
	type rawPlanAgentDefData struct {
		DisplayName   string            `yaml:"display_name"`
		IsMain        bool              `yaml:"is_main,omitempty"`
		Mode          string            `yaml:"mode,omitempty"`
		LLM           AgentLLMConfig    `yaml:"llm,omitempty"`
		SkillPackages []string          `yaml:"skill_packages,omitempty"`
		SkillPolicy   SkillPolicyData   `yaml:"skill_policy,omitempty"`
		Prompts       map[string]string `yaml:"prompts"`
	}
	var raw rawPlanAgentDefData
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*p = PlanAgentDefData{
		DisplayName:   raw.DisplayName,
		IsMain:        raw.IsMain,
		Mode:          strings.TrimSpace(raw.Mode),
		LLM:           raw.LLM,
		SkillPackages: mergeSkillPackageLists(raw.SkillPackages, skillPackagesFromPolicy(raw.SkillPolicy)),
		Prompts:       raw.Prompts,
	}
	return nil
}

type SkillLayerData struct {
	Skills         []string `yaml:"skills,omitempty" json:"skills,omitempty"`
	CategorySkills []string `yaml:"category_skills,omitempty" json:"category_skills,omitempty"`
	WorkflowSkills []string `yaml:"workflow_skills,omitempty" json:"workflow_skills,omitempty"`
}

func (l *SkillLayerData) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		*l = SkillLayerData{}
		return nil
	}

	switch value.Kind {
	case yaml.SequenceNode:
		var skills []string
		if err := value.Decode(&skills); err != nil {
			return err
		}
		*l = SkillLayerData{Skills: skills}
		return nil
	case yaml.MappingNode:
		type raw SkillLayerData
		var out raw
		if err := value.Decode(&out); err != nil {
			return err
		}
		*l = SkillLayerData(out)
		return nil
	case 0:
		*l = SkillLayerData{}
		return nil
	default:
		return fmt.Errorf("无效的 skill 层配置格式")
	}
}

type SkillPolicyData struct {
	Private SkillLayerData `yaml:"private,omitempty" json:"private"`
	Share   SkillLayerData `yaml:"share,omitempty" json:"share"`
}

// AgentConfigData 智能体配置数据（用于 JSON 传输）。
type AgentConfigData struct {
	Meta    AgentConfigMeta `yaml:"meta" json:"meta"`
	Runtime *RuntimeConfig  `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Plan    *PlanConfig     `yaml:"plan,omitempty" json:"plan,omitempty"`
}

// SkillReference Skill 引用信息。
type SkillReference struct {
	ConfigName string `json:"config_name"`
	AgentName  string `json:"agent_name"`
	Layer      string `json:"layer"`
	Source     string `json:"source"`
}

// AgentConfigManager 智能体配置管理器（基于 YAML 文件）。
type AgentConfigManager struct {
	configDir    string
	configs      map[string]*AgentConfigFile
	activeConfig string
	mu           sync.RWMutex
}

type ExclusiveSkillMigrationItem struct {
	PackageID   string `json:"package_id"`
	OwnerConfig string `json:"owner_config,omitempty"`
	SourcePath  string `json:"source_path,omitempty"`
	TargetPath  string `json:"target_path,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type ExclusiveSkillMigrationResult struct {
	Migrated []ExclusiveSkillMigrationItem `json:"migrated"`
	Skipped  []ExclusiveSkillMigrationItem `json:"skipped"`
}

const (
	activeConfigStateFileName = ".active_agent_config"
	agentConfigFileName       = "agent.yaml"
)

// NewAgentConfigManager 创建智能体配置管理器。
func NewAgentConfigManager(configDir string) *AgentConfigManager {
	acm := &AgentConfigManager{
		configDir:    configDir,
		configs:      make(map[string]*AgentConfigFile),
		activeConfig: "",
	}
	acm.migrateLegacyFlatConfigFiles()
	acm.loadAllConfigs()
	acm.loadActiveConfigState()
	acm.ensureBootstrapConfig()
	return acm
}

func (acm *AgentConfigManager) activeConfigStatePath() string {
	return filepath.Join(acm.configDir, activeConfigStateFileName)
}

func resolveAgentConfigDir(configDir, configName string) string {
	trimmedDir := strings.TrimSpace(configDir)
	trimmedName := strings.TrimSpace(configName)
	if trimmedDir == "" || trimmedName == "" {
		return ""
	}
	return filepath.Join(trimmedDir, trimmedName)
}

func resolveAgentConfigFilePath(configDir, configName string) string {
	configPath := resolveAgentConfigDir(configDir, configName)
	if configPath == "" {
		return ""
	}
	return filepath.Join(configPath, agentConfigFileName)
}

func resolveExclusiveSkillsDirFromConfigDir(configDir, configName string) string {
	configPath := resolveAgentConfigDir(configDir, configName)
	if configPath == "" {
		return ""
	}
	return filepath.Join(configPath, "skills")
}

func resolveExclusiveToolsDirFromConfigDir(configDir, configName string) string {
	configPath := resolveAgentConfigDir(configDir, configName)
	if configPath == "" {
		return ""
	}
	return filepath.Join(configPath, "tools")
}

func ResolveGlobalSkillsDir(configBaseDir string) string {
	trimmedBase := strings.TrimSpace(configBaseDir)
	if trimmedBase == "" {
		return ""
	}
	return filepath.Join(trimmedBase, "skills", "global")
}

func ResolveGlobalToolsDir(configBaseDir string) string {
	trimmedBase := strings.TrimSpace(configBaseDir)
	if trimmedBase == "" {
		return ""
	}
	return filepath.Join(trimmedBase, "tools", "global")
}

func ResolveExclusiveSkillsDir(configBaseDir, configName string) string {
	trimmedName := strings.TrimSpace(configName)
	if trimmedName == "" {
		return ""
	}
	trimmedBase := strings.TrimSpace(configBaseDir)
	if trimmedBase == "" {
		return ""
	}
	configDir := trimmedBase
	if !strings.EqualFold(filepath.Base(filepath.Clean(trimmedBase)), "agent_configs") {
		configDir = filepath.Join(trimmedBase, "agent_configs")
	}
	return resolveExclusiveSkillsDirFromConfigDir(configDir, trimmedName)
}

func ResolveExclusiveToolsDir(configBaseDir, configName string) string {
	trimmedName := strings.TrimSpace(configName)
	if trimmedName == "" {
		return ""
	}
	trimmedBase := strings.TrimSpace(configBaseDir)
	if trimmedBase == "" {
		return ""
	}
	configDir := trimmedBase
	if !strings.EqualFold(filepath.Base(filepath.Clean(trimmedBase)), "agent_configs") {
		configDir = filepath.Join(trimmedBase, "agent_configs")
	}
	return resolveExclusiveToolsDirFromConfigDir(configDir, trimmedName)
}

func normalizePathList(paths ...string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		cleaned := filepath.Clean(trimmed)
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func ResolveRuntimeSkillDirs(builtinSkillsDir, userSkillsDir, configBaseDir, activeConfigName string) []string {
	return ResolveRuntimeSkillDirsWithPlugin(ResolveSystemSkillsDir(), builtinSkillsDir, userSkillsDir, configBaseDir, activeConfigName)
}

func ResolveRuntimeSkillDirsWithPlugin(pluginSkillsDir, builtinSkillsDir, userSkillsDir, configBaseDir, activeConfigName string) []string {
	return normalizePathList(
		pluginSkillsDir,
		builtinSkillsDir,
		userSkillsDir,
		ResolveGlobalSkillsDir(configBaseDir),
		ResolveExclusiveSkillsDir(configBaseDir, activeConfigName),
	)
}

func ResolveRuntimeToolDirs(builtinToolsDir, userToolsDir, configBaseDir, activeConfigName string) []string {
	return ResolveRuntimeToolDirsWithPlugin(ResolveSystemToolsDir(), builtinToolsDir, userToolsDir, configBaseDir, activeConfigName)
}

func ResolveRuntimeToolDirsWithPlugin(pluginToolsDir, builtinToolsDir, userToolsDir, configBaseDir, activeConfigName string) []string {
	return normalizePathList(
		pluginToolsDir,
		builtinToolsDir,
		userToolsDir,
		ResolveGlobalToolsDir(configBaseDir),
		ResolveExclusiveToolsDir(configBaseDir, activeConfigName),
	)
}

func (acm *AgentConfigManager) configDirectory(configName string) string {
	return resolveAgentConfigDir(acm.configDir, configName)
}

func (acm *AgentConfigManager) configFilePath(configName string) string {
	return resolveAgentConfigFilePath(acm.configDir, configName)
}

func (acm *AgentConfigManager) ExclusiveSkillsDir(configName string) string {
	return resolveExclusiveSkillsDirFromConfigDir(acm.configDir, configName)
}

func (acm *AgentConfigManager) ExclusiveToolsDir(configName string) string {
	return resolveExclusiveToolsDirFromConfigDir(acm.configDir, configName)
}

func (acm *AgentConfigManager) ConfigBaseDir() string {
	if acm == nil {
		return ""
	}
	return filepath.Dir(strings.TrimSpace(acm.configDir))
}

func (acm *AgentConfigManager) ListExclusiveSkillDirs() map[string]string {
	acm.mu.RLock()
	defer acm.mu.RUnlock()

	out := make(map[string]string, len(acm.configs))
	for configName := range acm.configs {
		dir := acm.ExclusiveSkillsDir(configName)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		out[configName] = dir
	}
	return out
}

func (acm *AgentConfigManager) migrateLegacyFlatConfigFiles() {
	entries, err := os.ReadDir(acm.configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		logger.Warn("AgentConfigManager: 读取配置目录失败: %v", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		configName := strings.TrimSpace(strings.TrimSuffix(entry.Name(), ".yaml"))
		if configName == "" {
			continue
		}
		legacyPath := filepath.Join(acm.configDir, entry.Name())
		targetPath := acm.configFilePath(configName)
		if targetPath == "" {
			continue
		}
		if _, err := os.Stat(targetPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			logger.Warn("AgentConfigManager: 检查迁移目标失败 %s: %v", targetPath, err)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			logger.Warn("AgentConfigManager: 创建迁移目录失败 %s: %v", targetPath, err)
			continue
		}
		if err := os.Rename(legacyPath, targetPath); err != nil {
			logger.Warn("AgentConfigManager: 迁移旧配置失败 %s -> %s: %v", legacyPath, targetPath, err)
			continue
		}
		logger.Info("AgentConfigManager: 已迁移旧配置 %s -> %s", legacyPath, targetPath)
	}
}

func (acm *AgentConfigManager) loadActiveConfigState() {
	raw, err := os.ReadFile(acm.activeConfigStatePath())
	if err != nil {
		return
	}
	name := strings.TrimSpace(string(raw))
	if name == "" {
		return
	}
	if _, ok := acm.configs[name]; ok {
		acm.activeConfig = name
	}
}

func (acm *AgentConfigManager) saveActiveConfigStateLocked() error {
	if err := os.MkdirAll(acm.configDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(acm.activeConfigStatePath(), []byte(strings.TrimSpace(acm.activeConfig)), 0o644)
}

func builtInDefaultAgentConfig() *AgentConfigFile {
	raw, err := defaults.FS.ReadFile("agent_configs/default/agent.yaml")
	if err != nil {
		panic(fmt.Sprintf("AgentConfigManager: 读取内置默认智能体方案失败: %v", err))
	}

	var embedded AgentConfigFile
	if err := yaml.Unmarshal(raw, &embedded); err != nil {
		panic(fmt.Sprintf("AgentConfigManager: 解析内置默认智能体方案失败: %v", err))
	}
	return normalizeAgentConfigFile(&embedded)
}

var (
	embeddedSkillPackagesOnce sync.Once
	embeddedSkillPackagesSet  map[string]struct{}
)

func builtInEmbeddedSkillPackages() map[string]struct{} {
	embeddedSkillPackagesOnce.Do(func() {
		embeddedSkillPackagesSet = make(map[string]struct{})
		for _, root := range []string{ResolveSystemSkillsDir(), ResolvePluginUserSkillsDir()} {
			entries, err := os.ReadDir(strings.TrimSpace(root))
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				packageID := strings.TrimSpace(entry.Name())
				if packageID == "" {
					continue
				}
				embeddedSkillPackagesSet[packageID] = struct{}{}
			}
		}
	})
	return embeddedSkillPackagesSet
}

func mergeUnknownSkillPackages(embeddedPackages, currentPackages []string) []string {
	merged := append([]string{}, normalizeStringSlice(embeddedPackages)...)
	known := make(map[string]struct{}, len(merged))
	builtInPackages := builtInEmbeddedSkillPackages()
	for _, packageID := range merged {
		known[packageID] = struct{}{}
	}
	for _, packageID := range normalizeStringSlice(currentPackages) {
		if _, exists := known[packageID]; exists {
			continue
		}
		if _, builtIn := builtInPackages[packageID]; builtIn {
			continue
		}
		merged = append(merged, packageID)
		known[packageID] = struct{}{}
	}
	return merged
}

func mergeDefaultConfigPreservingUnknownPackages(embedded, current *AgentConfigFile) *AgentConfigFile {
	if embedded == nil {
		return normalizeAgentConfigFile(current)
	}
	merged, err := cloneConfigFile(embedded)
	if err != nil {
		return normalizeAgentConfigFile(embedded)
	}
	merged = normalizeAgentConfigFile(merged)
	current = normalizeAgentConfigFile(current)
	if current == nil {
		return merged
	}

	if merged.Runtime != nil && current.Runtime != nil {
		merged.Runtime.Global.SkillPackages = mergeUnknownSkillPackages(
			merged.Runtime.Global.SkillPackages,
			current.Runtime.Global.SkillPackages,
		)
		for agentName, embeddedAgent := range merged.Runtime.Agents {
			currentAgent, ok := current.Runtime.Agents[agentName]
			if !ok {
				continue
			}
			embeddedAgent.SkillPackages = mergeUnknownSkillPackages(embeddedAgent.SkillPackages, currentAgent.SkillPackages)
			merged.Runtime.Agents[agentName] = embeddedAgent
		}
	}

	if merged.Plan != nil && current.Plan != nil {
		if current.Plan.MaxIterations > 0 {
			merged.Plan.MaxIterations = current.Plan.MaxIterations
		}
		merged.Plan.Global.SkillPackages = mergeUnknownSkillPackages(
			merged.Plan.Global.SkillPackages,
			current.Plan.Global.SkillPackages,
		)
		for agentName, embeddedAgent := range merged.Plan.Agents {
			currentAgent, ok := current.Plan.Agents[agentName]
			if !ok {
				continue
			}
			embeddedAgent.SkillPackages = mergeUnknownSkillPackages(embeddedAgent.SkillPackages, currentAgent.SkillPackages)
			merged.Plan.Agents[agentName] = embeddedAgent
		}
	}

	return normalizeAgentConfigFile(merged)
}

func (acm *AgentConfigManager) refreshManagedDefaultConfigLocked() error {
	current, exists := acm.configs["default"]
	if !exists || current == nil {
		return nil
	}
	merged := mergeDefaultConfigPreservingUnknownPackages(builtInDefaultAgentConfig(), current)
	return acm.saveConfigFile("default", merged)
}

func (acm *AgentConfigManager) ensureBootstrapConfig() {
	if len(acm.configs) > 0 {
		if _, ok := acm.configs["default"]; ok {
			if err := acm.refreshManagedDefaultConfigLocked(); err != nil {
				logger.Warn("AgentConfigManager: 刷新受管默认配置失败: %v", err)
			}
		}
		if _, ok := acm.configs[acm.activeConfig]; !ok {
			names := make([]string, 0, len(acm.configs))
			for name := range acm.configs {
				names = append(names, name)
			}
			sort.Strings(names)
			if len(names) > 0 {
				acm.activeConfig = names[0]
			}
			_ = acm.saveActiveConfigStateLocked()
		}
		return
	}

	defaultConfig := builtInDefaultAgentConfig()
	acm.configs["default"] = defaultConfig
	acm.activeConfig = "default"
	if err := acm.saveConfigFile("default", defaultConfig); err != nil {
		logger.Warn("AgentConfigManager: 写入默认配置失败: %v", err)
		return
	}
	if err := acm.saveActiveConfigStateLocked(); err != nil {
		logger.Warn("AgentConfigManager: 写入活动配置状态失败: %v", err)
	}
	logger.Info("AgentConfigManager: 已初始化默认配置 %s", acm.configFilePath("default"))
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func mergeSkillPackageLists(groups ...[]string) []string {
	merged := make([]string, 0)
	for _, group := range groups {
		merged = append(merged, group...)
	}
	return normalizeStringSlice(merged)
}

func skillPackagesFromPolicy(policy SkillPolicyData) []string {
	return mergeSkillPackageLists(
		policy.Private.Skills,
		policy.Private.CategorySkills,
		policy.Private.WorkflowSkills,
		policy.Share.Skills,
		policy.Share.CategorySkills,
		policy.Share.WorkflowSkills,
	)
}

func normalizeStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		out[trimmedKey] = value
	}
	if len(out) == 0 {
		return map[string]string{}
	}
	return out
}

func normalizeSkillPolicyData(policy SkillPolicyData) SkillPolicyData {
	return SkillPolicyData{
		Private: normalizeSkillLayerData(policy.Private),
		Share:   normalizeSkillLayerData(policy.Share),
	}
}

func normalizeSkillLayerData(layer SkillLayerData) SkillLayerData {
	return SkillLayerData{
		Skills:         normalizeStringSlice(layer.Skills),
		CategorySkills: normalizeStringSlice(layer.CategorySkills),
		WorkflowSkills: normalizeStringSlice(layer.WorkflowSkills),
	}
}

func normalizeAgentLLMConfig(cfg AgentLLMConfig) AgentLLMConfig {
	return AgentLLMConfig{Provider: strings.TrimSpace(cfg.Provider)}
}

func normalizeGlobalAgentConfig(cfg GlobalAgentConfig) GlobalAgentConfig {
	return GlobalAgentConfig{
		SkillPackages: normalizeStringSlice(cfg.SkillPackages),
	}
}

func addCategorySkillTools(addTool func(string), packageTools map[string][]string, packageNames []string) {
	for _, packageName := range packageNames {
		for _, toolName := range packageTools[strings.TrimSpace(packageName)] {
			addTool(toolName)
		}
	}
}

func skillPackagesDirFromConfigDir(configDir string) string {
	return ResolveSystemSkillsDir()
}

func normalizePlanMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = PlanToolStrategyReadonlyPlus
	}
	return mode
}

func normalizeAgentDefData(agentName string, agent AgentDefData) AgentDefData {
	displayName := strings.TrimSpace(agent.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(agentName)
	}

	normalized := AgentDefData{
		DisplayName:     displayName,
		Description:     strings.TrimSpace(agent.Description),
		IsMain:          agent.IsMain,
		Deletable:       agent.Deletable,
		Enabled:         agent.Enabled,
		LLM:             normalizeAgentLLMConfig(agent.LLM),
		WorkflowPackage: strings.TrimSpace(agent.WorkflowPackage),
		SkillPackages:   normalizeStringSlice(agent.SkillPackages),
		Prompts:         normalizeStringMap(agent.Prompts),
	}

	if normalized.Enabled == false && normalized.IsMain {
		normalized.Enabled = true
	}
	if normalized.Prompts == nil {
		normalized.Prompts = map[string]string{}
	}
	if agentName == "knowledge_engineer" {
		// Business rules now come from this agent's editable instructions. Remove
		// the retired duplicate package from previously saved configurations.
		packages := normalized.SkillPackages[:0]
		for _, packageID := range normalized.SkillPackages {
			if packageID != "marching-route-knowledge-extraction" {
				packages = append(packages, packageID)
			}
		}
		normalized.SkillPackages = packages
	}
	return normalized
}

func normalizePlanAgentDefData(agentName string, agent PlanAgentDefData) PlanAgentDefData {
	displayName := strings.TrimSpace(agent.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(agentName)
	}

	normalized := PlanAgentDefData{
		DisplayName:   displayName,
		IsMain:        agent.IsMain,
		Mode:          normalizePlanMode(agent.Mode),
		LLM:           normalizeAgentLLMConfig(agent.LLM),
		SkillPackages: normalizeStringSlice(agent.SkillPackages),
		Prompts:       normalizeStringMap(agent.Prompts),
	}
	if normalized.Prompts == nil {
		normalized.Prompts = map[string]string{}
	}
	if strings.TrimSpace(normalized.Prompts["user_prompt_template"]) == "" {
		normalized.Prompts["user_prompt_template"] = "{{ user_input }}"
	}
	return normalized
}

func normalizeRuntimeConfig(runtimeCfg *RuntimeConfig) *RuntimeConfig {
	if runtimeCfg == nil {
		return nil
	}
	normalized := &RuntimeConfig{
		Global: normalizeGlobalAgentConfig(runtimeCfg.Global),
		Agents: map[string]AgentDefData{},
	}
	for name, agent := range runtimeCfg.Agents {
		normalized.Agents[name] = normalizeAgentDefData(name, agent)
	}
	return normalized
}

func normalizePlanConfig(planCfg *PlanConfig) *PlanConfig {
	if planCfg == nil {
		return nil
	}
	normalized := &PlanConfig{
		MaxIterations: planCfg.MaxIterations,
		Global:        normalizeGlobalAgentConfig(planCfg.Global),
		Agents:        map[string]PlanAgentDefData{},
	}
	if normalized.MaxIterations <= 0 {
		normalized.MaxIterations = 100
	}

	for name, agent := range planCfg.Agents {
		normalized.Agents[name] = normalizePlanAgentDefData(name, agent)
	}
	return normalized
}

func normalizeAgentConfigMeta(meta AgentConfigMeta) AgentConfigMeta {
	mode := strings.ToLower(strings.TrimSpace(meta.DefaultExecutionMode))
	switch mode {
	case "plan", "build":
	default:
		mode = "build"
	}
	return AgentConfigMeta{
		DefaultExecutionMode: mode,
		Description:          strings.TrimSpace(meta.Description),
		PlanEnabled:          meta.PlanEnabled,
	}
}

func runtimeConfig(cfg *AgentConfigFile) *RuntimeConfig {
	if cfg == nil {
		return nil
	}
	return cfg.Runtime
}

func runtimeConfigData(data *AgentConfigData) *RuntimeConfig {
	if data == nil {
		return nil
	}
	return data.Runtime
}

func normalizeAgentConfigFile(cfg *AgentConfigFile) *AgentConfigFile {
	if cfg == nil {
		return builtInDefaultAgentConfig()
	}
	normalized := &AgentConfigFile{
		Version: AgentConfigVersionV10,
		Meta:    normalizeAgentConfigMeta(cfg.Meta),
		Runtime: normalizeRuntimeConfig(cfg.Runtime),
		Plan:    normalizePlanConfig(cfg.Plan),
	}
	return normalized
}

func validatePlanToolMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case PlanToolStrategyReadonlyOnly, PlanToolStrategyReadonlyPlus, PlanToolStrategyAll:
		return true
	default:
		return false
	}
}

func validatePlanConfig(planCfg *PlanConfig) error {
	if planCfg == nil {
		return nil
	}
	if planCfg.MaxIterations <= 0 {
		return fmt.Errorf("plan.max_iterations 必须大于 0")
	}
	if len(planCfg.Agents) == 0 {
		return fmt.Errorf("plan.agents 不能为空")
	}
	for name, agent := range planCfg.Agents {
		if !validatePlanToolMode(agent.Mode) {
			return fmt.Errorf("plan.agents.%s.mode 无效: %s", name, agent.Mode)
		}
		if strings.TrimSpace(agent.Prompts["user_prompt_template"]) == "" {
			return fmt.Errorf("plan.agents.%s.prompts.user_prompt_template 不能为空", name)
		}
	}
	return nil
}

func validateAgentConfig(cfg *AgentConfigFile) error {
	if cfg == nil {
		return fmt.Errorf("配置为空")
	}
	if cfg.Runtime == nil {
		return fmt.Errorf("runtime 配置不能为空")
	}
	if len(cfg.Runtime.Agents) == 0 {
		return fmt.Errorf("runtime.agents 不能为空")
	}
	if err := validatePlanConfig(cfg.Plan); err != nil {
		return err
	}
	return nil
}

func cloneConfigFile(cfg *AgentConfigFile) (*AgentConfigFile, error) {
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var cloned AgentConfigFile
	if err := yaml.Unmarshal(raw, &cloned); err != nil {
		return nil, err
	}
	return &cloned, nil
}

func cloneConfigData(data *AgentConfigData) (*AgentConfigData, error) {
	raw, err := yaml.Marshal(data)
	if err != nil {
		return nil, err
	}
	var out AgentConfigData
	if err := yaml.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// loadAllConfigs 加载目录下所有配置文件。
func (acm *AgentConfigManager) loadAllConfigs() {
	entries, err := os.ReadDir(acm.configDir)
	if err != nil {
		logger.Warn("AgentConfigManager: 读取配置目录失败: %v", err)
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		configName := strings.TrimSpace(entry.Name())
		if configName == "" || strings.HasPrefix(configName, ".") {
			continue
		}
		configPath := acm.configFilePath(configName)
		if _, err := os.Stat(configPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			logger.Warn("AgentConfigManager: 检查配置 %s 失败: %v", configName, err)
			continue
		}

		config, err := acm.loadConfigFile(configPath)
		if err != nil {
			logger.Warn("AgentConfigManager: 加载配置 %s 失败: %v", configName, err)
			continue
		}
		acm.configs[configName] = config
		logger.Info("AgentConfigManager: 加载配置 %s", configName)
	}
}

// loadConfigFile 加载单个配置文件。
func (acm *AgentConfigManager) loadConfigFile(path string) (*AgentConfigFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	var config AgentConfigFile
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析 YAML 失败: %w", err)
	}

	normalized := normalizeAgentConfigFile(&config)
	if err := validateAgentConfig(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

// ListConfigs 列出所有配置。
func (acm *AgentConfigManager) ListConfigs(userID string) ([]StoredAgentConfig, error) {
	acm.mu.RLock()
	defer acm.mu.RUnlock()

	configs := make([]StoredAgentConfig, 0, len(acm.configs))
	id := int64(1)
	for name, cfg := range acm.configs {
		configs = append(configs, StoredAgentConfig{
			ID:         id,
			UserID:     userID,
			ConfigName: name,
			IsDefault:  name == "default",
			IsActive:   name == acm.activeConfig,
			ConfigData: acm.configToYAMLString(cfg),
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		})
		id++
	}
	return configs, nil
}

// GetConfigData 获取指定配置的结构化数据。
func (acm *AgentConfigManager) GetConfigData(configName string) (*AgentConfigData, error) {
	acm.mu.RLock()
	defer acm.mu.RUnlock()

	if strings.TrimSpace(configName) == "" {
		configName = acm.activeConfig
	}

	cfg, exists := acm.configs[configName]
	if !exists || cfg == nil {
		return nil, fmt.Errorf("配置不存在: %s", configName)
	}

	cloned, err := cloneConfigFile(cfg)
	if err != nil {
		return nil, fmt.Errorf("克隆配置失败: %w", err)
	}
	out, err := cloneConfigData(&AgentConfigData{
		Meta:    cloned.Meta,
		Runtime: cloned.Runtime,
		Plan:    cloned.Plan,
	})
	if err != nil {
		return nil, fmt.Errorf("序列化配置失败: %w", err)
	}
	return out, nil
}

// CreateConfig 创建新的智能体配置（使用内置默认模板）。
func (acm *AgentConfigManager) CreateConfig(configName string) error {
	acm.mu.Lock()
	defer acm.mu.Unlock()

	configName = strings.TrimSpace(configName)
	if configName == "" {
		return fmt.Errorf("配置名不能为空")
	}
	if _, exists := acm.configs[configName]; exists {
		return fmt.Errorf("配置已存在: %s", configName)
	}

	cfg := builtInDefaultAgentConfig()
	acm.configs[configName] = cfg
	return acm.saveConfigFile(configName, cfg)
}

// CloneConfig 克隆已有配置为新配置。
func (acm *AgentConfigManager) CloneConfig(sourceName, targetName string) error {
	acm.mu.Lock()
	defer acm.mu.Unlock()

	sourceName = strings.TrimSpace(sourceName)
	targetName = strings.TrimSpace(targetName)
	if sourceName == "" || targetName == "" {
		return fmt.Errorf("源配置和目标配置名不能为空")
	}

	sourceCfg, exists := acm.configs[sourceName]
	if !exists || sourceCfg == nil {
		return fmt.Errorf("源配置不存在: %s", sourceName)
	}
	if _, exists := acm.configs[targetName]; exists {
		return fmt.Errorf("目标配置已存在: %s", targetName)
	}

	cloned, err := cloneConfigFile(sourceCfg)
	if err != nil {
		return fmt.Errorf("克隆配置失败: %w", err)
	}
	acm.configs[targetName] = cloned
	if err := acm.saveConfigFile(targetName, cloned); err != nil {
		delete(acm.configs, targetName)
		return err
	}

	sourceSkillsDir := acm.ExclusiveSkillsDir(sourceName)
	info, err := os.Stat(sourceSkillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		delete(acm.configs, targetName)
		_ = os.RemoveAll(acm.configDirectory(targetName))
		return fmt.Errorf("读取源方案专属技能目录失败: %w", err)
	}
	if !info.IsDir() {
		return nil
	}
	if err := copyDirectory(sourceSkillsDir, acm.ExclusiveSkillsDir(targetName)); err != nil {
		delete(acm.configs, targetName)
		_ = os.RemoveAll(acm.configDirectory(targetName))
		return fmt.Errorf("复制方案专属技能目录失败: %w", err)
	}
	return nil
}

// configToYAMLString 将配置转换为 YAML 字符串。
func (acm *AgentConfigManager) configToYAMLString(cfg *AgentConfigFile) string {
	if cfg == nil {
		return ""
	}
	cloned, err := cloneConfigFile(cfg)
	if err != nil {
		return ""
	}

	data := AgentConfigData{Meta: cloned.Meta, Runtime: cloned.Runtime, Plan: cloned.Plan}
	jsonBytes, _ := yaml.Marshal(data)
	return string(jsonBytes)
}

// configToJSON 保留历史函数名，实际返回 YAML 字符串。
func (acm *AgentConfigManager) configToJSON(cfg *AgentConfigFile) string {
	return acm.configToYAMLString(cfg)
}

// SaveConfig 保存配置。
func (acm *AgentConfigManager) SaveConfig(userID, configName string, data *AgentConfigData) error {
	_ = userID
	acm.mu.Lock()
	defer acm.mu.Unlock()

	if data == nil {
		return fmt.Errorf("配置数据不能为空")
	}

	configName = strings.TrimSpace(configName)
	if configName == "" {
		return fmt.Errorf("配置名不能为空")
	}

	requestedRuntime := runtimeConfigData(data)
	if requestedRuntime == nil {
		return fmt.Errorf("runtime 配置不能为空")
	}
	candidate := normalizeAgentConfigFile(&AgentConfigFile{
		Version: AgentConfigVersionV10,
		Meta:    data.Meta,
		Runtime: requestedRuntime,
		Plan:    data.Plan,
	})
	if err := validateAgentConfig(candidate); err != nil {
		return err
	}

	acm.configs[configName] = candidate
	return acm.saveConfigFile(configName, candidate)
}

// GetAgent 获取指定配置中的单个 Runtime 智能体定义。
func (acm *AgentConfigManager) GetAgent(configName, agentName string) (*AgentDefData, error) {
	acm.mu.RLock()
	defer acm.mu.RUnlock()

	configName = strings.TrimSpace(configName)
	if configName == "" {
		configName = acm.activeConfig
	}
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return nil, fmt.Errorf("智能体名称不能为空")
	}

	cfg, exists := acm.configs[configName]
	if !exists || runtimeConfig(cfg) == nil {
		return nil, fmt.Errorf("配置不存在: %s", configName)
	}

	agent, exists := cfg.Runtime.Agents[agentName]
	if !exists {
		return nil, fmt.Errorf("智能体不存在: %s", agentName)
	}

	cloned := cloneAgentDefData(agent)
	return &cloned, nil
}

// AddAgent 在配置中新增单个 Runtime 智能体。
func (acm *AgentConfigManager) AddAgent(configName, agentName string, agent AgentDefData) error {
	acm.mu.Lock()
	defer acm.mu.Unlock()

	configName = strings.TrimSpace(configName)
	if configName == "" {
		configName = acm.activeConfig
	}
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return fmt.Errorf("智能体名称不能为空")
	}

	cfg, exists := acm.configs[configName]
	if !exists || runtimeConfig(cfg) == nil {
		return fmt.Errorf("配置不存在: %s", configName)
	}
	if cfg.Runtime.Agents == nil {
		cfg.Runtime.Agents = make(map[string]AgentDefData)
	}
	if _, exists := cfg.Runtime.Agents[agentName]; exists {
		return fmt.Errorf("智能体已存在: %s", agentName)
	}
	if agent.IsMain {
		return fmt.Errorf("新增智能体不能设置为主控智能体")
	}

	normalized := normalizeAgentDefData(agentName, agent)
	normalized.IsMain = false
	normalized.Deletable = true

	cfg.Runtime.Agents[agentName] = normalized
	return acm.saveConfigFile(configName, cfg)
}

// UpdateAgent 更新配置中的单个 Runtime 智能体。
func (acm *AgentConfigManager) UpdateAgent(configName, agentName string, agent AgentDefData) error {
	acm.mu.Lock()
	defer acm.mu.Unlock()

	configName = strings.TrimSpace(configName)
	if configName == "" {
		configName = acm.activeConfig
	}
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return fmt.Errorf("智能体名称不能为空")
	}

	cfg, exists := acm.configs[configName]
	if !exists || runtimeConfig(cfg) == nil {
		return fmt.Errorf("配置不存在: %s", configName)
	}
	original, exists := cfg.Runtime.Agents[agentName]
	if !exists {
		return fmt.Errorf("智能体不存在: %s", agentName)
	}

	normalized := normalizeAgentDefData(agentName, agent)
	normalized.IsMain = original.IsMain
	normalized.Deletable = original.Deletable
	if original.IsMain {
		normalized.Enabled = true
	}

	cfg.Runtime.Agents[agentName] = normalized
	return acm.saveConfigFile(configName, cfg)
}

// RemoveAgent 删除配置中的单个 Runtime 智能体。
func (acm *AgentConfigManager) RemoveAgent(configName, agentName string) error {
	acm.mu.Lock()
	defer acm.mu.Unlock()

	configName = strings.TrimSpace(configName)
	if configName == "" {
		configName = acm.activeConfig
	}
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return fmt.Errorf("智能体名称不能为空")
	}

	cfg, exists := acm.configs[configName]
	if !exists || runtimeConfig(cfg) == nil {
		return fmt.Errorf("配置不存在: %s", configName)
	}
	original, exists := cfg.Runtime.Agents[agentName]
	if !exists {
		return fmt.Errorf("智能体不存在: %s", agentName)
	}
	if original.IsMain {
		return fmt.Errorf("主控智能体不能删除: %s", agentName)
	}

	delete(cfg.Runtime.Agents, agentName)
	if err := acm.saveConfigFile(configName, cfg); err != nil {
		cfg.Runtime.Agents[agentName] = original
		return err
	}
	return nil
}

// saveConfigFile 保存配置到文件。
func (acm *AgentConfigManager) saveConfigFile(name string, cfg *AgentConfigFile) error {
	normalized := normalizeAgentConfigFile(cfg)
	if err := validateAgentConfig(normalized); err != nil {
		return err
	}

	path := acm.configFilePath(name)
	if path == "" {
		return fmt.Errorf("配置路径不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	persisted := struct {
		Version string          `yaml:"version"`
		Meta    AgentConfigMeta `yaml:"meta"`
		Runtime *RuntimeConfig  `yaml:"runtime,omitempty"`
		Plan    *PlanConfig     `yaml:"plan,omitempty"`
	}{
		Version: normalized.Version,
		Meta:    normalized.Meta,
		Runtime: normalized.Runtime,
		Plan:    normalized.Plan,
	}
	data, err := yaml.Marshal(persisted)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	acm.configs[name] = normalized
	return nil
}

// SwitchConfig 切换活动配置。
func (acm *AgentConfigManager) SwitchConfig(userID, configName string) error {
	_ = userID
	acm.mu.Lock()
	defer acm.mu.Unlock()

	if _, exists := acm.configs[configName]; !exists {
		return fmt.Errorf("配置不存在: %s", configName)
	}
	acm.activeConfig = configName
	return acm.saveActiveConfigStateLocked()
}

// DeleteConfig 删除配置。
func (acm *AgentConfigManager) DeleteConfig(userID, configName string) error {
	_ = userID
	acm.mu.Lock()
	defer acm.mu.Unlock()

	configName = strings.TrimSpace(configName)

	if configName == "default" {
		return fmt.Errorf("默认配置不能删除")
	}

	if _, exists := acm.configs[configName]; !exists {
		return fmt.Errorf("配置不存在: %s", configName)
	}

	path := acm.configDirectory(configName)
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("删除配置目录失败: %w", err)
	}

	delete(acm.configs, configName)
	if acm.activeConfig == configName {
		acm.activeConfig = ""
		names := make([]string, 0, len(acm.configs))
		for name := range acm.configs {
			names = append(names, name)
		}
		sort.Strings(names)
		if len(names) > 0 {
			acm.activeConfig = names[0]
		}
	}
	return acm.saveActiveConfigStateLocked()
}

func (acm *AgentConfigManager) persistAllConfigsLocked() error {
	names := make([]string, 0, len(acm.configs))
	for name := range acm.configs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cfg := acm.configs[name]
		if cfg == nil {
			continue
		}
		if err := acm.saveConfigFile(name, cfg); err != nil {
			return err
		}
	}
	return nil
}

func (acm *AgentConfigManager) GetActiveToolCatalog() ([]string, error) {
	acm.mu.RLock()
	defer acm.mu.RUnlock()

	cfg := acm.configs[acm.activeConfig]
	if runtimeConfig(cfg) == nil {
		return nil, fmt.Errorf("没有激活的配置")
	}

	packageTools, err := subagents.LoadPackageToolsMultiDir(
		skillPackagesDirFromConfigDir(acm.configDir),
		ResolveUserSkillsDir(),
		acm.ExclusiveSkillsDir(acm.activeConfig),
	)
	if err != nil {
		return nil, err
	}
	toolSet := make(map[string]struct{})

	addTool := func(tool string) {
		name := strings.TrimSpace(tool)
		if name == "" {
			return
		}
		toolSet[name] = struct{}{}
	}

	addGlobalSkillTools := func(global GlobalAgentConfig) {
		addCategorySkillTools(addTool, packageTools, global.SkillPackages)
	}

	addGlobalSkillTools(cfg.Runtime.Global)
	for _, agent := range cfg.Runtime.Agents {
		addCategorySkillTools(addTool, packageTools, agent.SkillPackages)
	}

	if cfg.Plan != nil {
		addGlobalSkillTools(cfg.Plan.Global)
		for _, agent := range cfg.Plan.Agents {
			addCategorySkillTools(addTool, packageTools, agent.SkillPackages)
		}
	}

	tools := make([]string, 0, len(toolSet))
	for tool := range toolSet {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	return tools, nil
}

func (acm *AgentConfigManager) GetGlobalToolCatalog() ([]string, error) {
	acm.mu.RLock()
	defer acm.mu.RUnlock()
	toolSet := make(map[string]struct{})

	addTool := func(tool string) {
		name := strings.TrimSpace(tool)
		if name == "" {
			return
		}
		toolSet[name] = struct{}{}
	}
	addGlobalSkillTools := func(packageTools map[string][]string, global GlobalAgentConfig) {
		addCategorySkillTools(addTool, packageTools, global.SkillPackages)
	}

	for _, cfg := range acm.configs {
		if runtimeConfig(cfg) == nil {
			continue
		}
		packageTools, err := subagents.LoadPackageToolsMultiDir(
			skillPackagesDirFromConfigDir(acm.configDir),
			ResolveUserSkillsDir(),
			acm.ExclusiveSkillsDir(acm.configNameForConfigLocked(cfg)),
		)
		if err != nil {
			return nil, err
		}
		addGlobalSkillTools(packageTools, cfg.Runtime.Global)
		for _, agent := range cfg.Runtime.Agents {
			addCategorySkillTools(addTool, packageTools, agent.SkillPackages)
		}
		if cfg.Plan != nil {
			addGlobalSkillTools(packageTools, cfg.Plan.Global)
			for _, agent := range cfg.Plan.Agents {
				addCategorySkillTools(addTool, packageTools, agent.SkillPackages)
			}
		}
	}

	tools := make([]string, 0, len(toolSet))
	for tool := range toolSet {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	return tools, nil
}

// GetActiveConfig 获取当前激活的配置。
func (acm *AgentConfigManager) GetActiveConfig() *AgentConfigFile {
	acm.mu.RLock()
	defer acm.mu.RUnlock()

	cfg := acm.configs[acm.activeConfig]
	if cfg == nil {
		return nil
	}
	cloned, err := cloneConfigFile(cfg)
	if err != nil {
		return nil
	}
	return cloned
}

// GetActiveConfigName 获取当前激活的配置名称。
func (acm *AgentConfigManager) GetActiveConfigName() string {
	acm.mu.RLock()
	defer acm.mu.RUnlock()
	return acm.activeConfig
}

// FindSkillReferences 查找指定 skill 在所有配置中的引用位置。
func (acm *AgentConfigManager) FindSkillReferences(skillName string) []SkillReference {
	target := strings.TrimSpace(skillName)
	if target == "" {
		return nil
	}

	acm.mu.RLock()
	defer acm.mu.RUnlock()

	var refs []SkillReference
	seen := make(map[string]bool)
	addRef := func(configName, agentName, layer, source string) {
		key := fmt.Sprintf("%s|%s|%s|%s", configName, agentName, layer, source)
		if seen[key] {
			return
		}
		seen[key] = true
		refs = append(refs, SkillReference{
			ConfigName: configName,
			AgentName:  agentName,
			Layer:      layer,
			Source:     source,
		})
	}
	checkSlice := func(configName, agentName, layerName, source string, values []string) {
		for _, skill := range values {
			if strings.TrimSpace(skill) == target {
				addRef(configName, agentName, layerName, source)
				break
			}
		}
	}
	for configName, cfg := range acm.configs {
		if runtimeConfig(cfg) == nil {
			continue
		}
		checkSlice(configName, "global", "runtime/global", "skill_packages", cfg.Runtime.Global.SkillPackages)
		for agentName, agent := range cfg.Runtime.Agents {
			checkSlice(configName, agentName, "runtime/agent", "skill_packages", agent.SkillPackages)
		}
		if cfg.Plan != nil {
			checkSlice(configName, "global", "plan/global", "skill_packages", cfg.Plan.Global.SkillPackages)
			for agentName, agent := range cfg.Plan.Agents {
				checkSlice(configName, agentName, "plan/agent", "skill_packages", agent.SkillPackages)
			}
		}
	}

	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ConfigName != refs[j].ConfigName {
			return refs[i].ConfigName < refs[j].ConfigName
		}
		if refs[i].AgentName != refs[j].AgentName {
			return refs[i].AgentName < refs[j].AgentName
		}
		if refs[i].Layer != refs[j].Layer {
			return refs[i].Layer < refs[j].Layer
		}
		return refs[i].Source < refs[j].Source
	})

	return refs
}

func (acm *AgentConfigManager) configNameForConfigLocked(target *AgentConfigFile) string {
	for name, cfg := range acm.configs {
		if cfg == target {
			return name
		}
	}
	return ""
}

func (acm *AgentConfigManager) MigrateExclusiveSkills() (ExclusiveSkillMigrationResult, error) {
	acm.mu.Lock()
	defer acm.mu.Unlock()

	result := ExclusiveSkillMigrationResult{
		Migrated: []ExclusiveSkillMigrationItem{},
		Skipped:  []ExclusiveSkillMigrationItem{},
	}
	globalSkillsDir := skillPackagesDirFromConfigDir(acm.configDir)
	entries, err := os.ReadDir(globalSkillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, fmt.Errorf("读取全局技能目录失败: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		packageID := strings.TrimSpace(entry.Name())
		if packageID == "" {
			continue
		}
		sourcePath := filepath.Join(globalSkillsDir, packageID)
		owners := acm.packageOwnersLocked(packageID)
		if len(owners) != 1 {
			reason := ""
			switch len(owners) {
			case 0:
				reason = "未被任何方案引用"
			default:
				reason = "被多个方案共同引用"
			}
			result.Skipped = append(result.Skipped, ExclusiveSkillMigrationItem{
				PackageID:  packageID,
				SourcePath: sourcePath,
				Reason:     reason,
			})
			continue
		}
		ownerConfig := owners[0]
		targetPath := filepath.Join(acm.ExclusiveSkillsDir(ownerConfig), packageID)
		if _, err := os.Stat(targetPath); err == nil {
			result.Skipped = append(result.Skipped, ExclusiveSkillMigrationItem{
				PackageID:   packageID,
				OwnerConfig: ownerConfig,
				SourcePath:  sourcePath,
				TargetPath:  targetPath,
				Reason:      "目标方案已存在同名专属技能包",
			})
			continue
		} else if !os.IsNotExist(err) {
			return result, fmt.Errorf("检查专属技能目标失败: %w", err)
		}
		if err := moveDirectory(sourcePath, targetPath); err != nil {
			return result, fmt.Errorf("迁移技能包 %s 失败: %w", packageID, err)
		}
		result.Migrated = append(result.Migrated, ExclusiveSkillMigrationItem{
			PackageID:   packageID,
			OwnerConfig: ownerConfig,
			SourcePath:  sourcePath,
			TargetPath:  targetPath,
		})
	}

	return result, nil
}

func (acm *AgentConfigManager) packageOwnersLocked(packageID string) []string {
	target := strings.TrimSpace(packageID)
	if target == "" {
		return nil
	}
	owners := make([]string, 0)
	for configName, cfg := range acm.configs {
		if runtimeConfig(cfg) == nil {
			continue
		}
		if configUsesPackage(cfg, target) {
			owners = append(owners, configName)
		}
	}
	sort.Strings(owners)
	return owners
}

func configUsesPackage(cfg *AgentConfigFile, packageID string) bool {
	target := strings.TrimSpace(packageID)
	if target == "" || cfg == nil || runtimeConfig(cfg) == nil {
		return false
	}
	if containsNormalized(cfg.Runtime.Global.SkillPackages, target) {
		return true
	}
	for _, agent := range cfg.Runtime.Agents {
		if containsNormalized(agent.SkillPackages, target) {
			return true
		}
	}
	if cfg.Plan == nil {
		return false
	}
	if containsNormalized(cfg.Plan.Global.SkillPackages, target) {
		return true
	}
	for _, agent := range cfg.Plan.Agents {
		if containsNormalized(agent.SkillPackages, target) {
			return true
		}
	}
	return false
}

func containsNormalized(values []string, target string) bool {
	trimmedTarget := strings.TrimSpace(target)
	if trimmedTarget == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == trimmedTarget {
			return true
		}
	}
	return false
}

func copyDirectory(sourceDir, targetDir string) error {
	info, err := os.Stat(sourceDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("源路径不是目录: %s", sourceDir)
	}
	if err := os.MkdirAll(targetDir, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(sourceDir, entry.Name())
		targetPath := filepath.Join(targetDir, entry.Name())
		if entry.IsDir() {
			if err := copyDirectory(sourcePath, targetPath); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(sourcePath, targetPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(sourcePath, targetPath string) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	info, err := sourceFile.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	targetFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer targetFile.Close()
	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		return err
	}
	return nil
}

func moveDirectory(sourceDir, targetDir string) error {
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return err
	}
	if err := os.Rename(sourceDir, targetDir); err == nil {
		return nil
	}
	if err := copyDirectory(sourceDir, targetDir); err != nil {
		return err
	}
	return os.RemoveAll(sourceDir)
}

func cloneAgentDefData(agent AgentDefData) AgentDefData {
	cloned := normalizeAgentDefData(agent.DisplayName, agent)
	cloned.IsMain = agent.IsMain
	cloned.Deletable = agent.Deletable
	cloned.Enabled = agent.Enabled
	return cloned
}

// contains 检查切片是否包含指定元素。
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func removeStringValues(values []string, target string) ([]string, int) {
	trimmedTarget := strings.TrimSpace(target)
	if trimmedTarget == "" || len(values) == 0 {
		copied := append([]string(nil), values...)
		return copied, 0
	}

	out := make([]string, 0, len(values))
	removed := 0
	for _, value := range values {
		if strings.TrimSpace(value) == trimmedTarget {
			removed++
			continue
		}
		out = append(out, value)
	}
	return out, removed
}

func ConvertToRuntimeSkillConfig(cfg *AgentConfigFile, builtinSkillsDir, userSkillsDir, configBaseDir, activeConfigName string) *subagents.RuntimeSkillConfig {
	return ConvertToRuntimeSkillConfigWithPlugin(cfg, "", builtinSkillsDir, userSkillsDir, configBaseDir, activeConfigName)
}

func ConvertToRuntimeSkillConfigWithPlugin(cfg *AgentConfigFile, pluginSkillsDir, builtinSkillsDir, userSkillsDir, configBaseDir, activeConfigName string) *subagents.RuntimeSkillConfig {
	if runtimeConfig(cfg) == nil {
		return nil
	}
	packageTools, err := subagents.LoadPackageToolsMultiDir(ResolveRuntimeSkillDirsWithPlugin(pluginSkillsDir, builtinSkillsDir, userSkillsDir, configBaseDir, activeConfigName)...)
	if err != nil {
		logger.Warn("加载技能包工具映射失败: %v", err)
		packageTools = map[string][]string{}
	}

	agents := make(map[string]subagents.RuntimeSkillAgentConfig)
	for name, agent := range cfg.Runtime.Agents {
		agents[name] = subagents.RuntimeSkillAgentConfig{
			DisplayName:     agent.DisplayName,
			Description:     agent.Description,
			IsMain:          agent.IsMain,
			Enabled:         agent.Enabled,
			WorkflowPackage: strings.TrimSpace(agent.WorkflowPackage),
			SkillPackages:   append([]string(nil), agent.SkillPackages...),
			Prompts: subagents.AgentPrompts{
				StaticInstructions:     strings.TrimSpace(agent.Prompts["static_instructions"]),
				UserPromptTemplate:     strings.TrimSpace(agent.Prompts["user_prompt_template"]),
				RuntimeModeInstruction: strings.TrimSpace(agent.Prompts["runtime_mode_instruction"]),
			},
		}
	}

	return &subagents.RuntimeSkillConfig{
		Version: cfg.Version,
		Global: subagents.RuntimeSkillGlobalConfig{
			SkillPackages: append([]string(nil), cfg.Runtime.Global.SkillPackages...),
			PackageTools:  packageTools,
		},
		Agents: agents,
	}
}
