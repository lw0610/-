package settings

import (
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
)

const (
	// MutationDomainSystem 表示系统域。
	MutationDomainSystem = "system"
	// MutationDomainServices 表示服务域。
	MutationDomainServices = "services"
	// MutationDomainModels 表示模型与推理域。
	MutationDomainModels = "models"
	// MutationDomainAgents 表示智能体与策略域。
	MutationDomainAgents = "agents"
	// MutationDomainTools 表示工具域。
	MutationDomainTools = "tools"
	// MutationDomainSkills 表示技能域。
	MutationDomainSkills = "skills"
)

const (
	// MutationRequiredActionNone 表示无需额外动作。
	MutationRequiredActionNone = "none"
	// MutationRequiredActionRuntimeRebuild 表示需要重建运行态。
	MutationRequiredActionRuntimeRebuild = "runtime_rebuild"
	// MutationRequiredActionDesktopRestart 表示需要重启桌面端。
	MutationRequiredActionDesktopRestart = "desktop_restart"
	// MutationRequiredActionStopActiveRuns 表示需要先停止活跃运行。
	MutationRequiredActionStopActiveRuns = "stop_active_runs"
)

// SettingsModeIssue 表示某个运行模式当前不可用的原因。
type SettingsModeIssue struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// SettingsMutationResult 描述设置写入后的持久化与运行态应用结果。
type SettingsMutationResult struct {
	Scope             string                       `json:"scope,omitempty"`
	Persisted         bool                         `json:"persisted"`
	Idempotent        bool                         `json:"idempotent,omitempty"`
	ConfigLoaded      bool                         `json:"config_loaded"`
	RuntimeApplied    bool                         `json:"runtime_applied"`
	AffectedDomain    string                       `json:"affected_domain,omitempty"`
	RequiredAction    string                       `json:"required_action,omitempty"`
	NextStep          string                       `json:"next_step,omitempty"`
	ConfigRevision    string                       `json:"config_revision,omitempty"`
	ActiveAgentConfig string                       `json:"active_agent_config,omitempty"`
	RuntimeRevision   string                       `json:"runtime_revision,omitempty"`
	Warnings          []string                     `json:"warnings,omitempty"`
	AffectedPaths     []string                     `json:"affected_paths,omitempty"`
	BlockingRuns      []string                     `json:"blocking_runs,omitempty"`
	BlockingInstances []string                     `json:"blocking_instances,omitempty"`
	ModeIssues        map[string]SettingsModeIssue `json:"mode_issues,omitempty"`
}

// RuntimeApplyProvider 定义设置写入后的运行态应用接口。
type RuntimeApplyProvider interface {
	ApplyPlanModeConfig() error
	ApplyLLMConfig() error
	ApplyAgentConfig(configName string) (bool, error)
	ApplyActiveRuntime() error
}

// RuntimeDiagnosticsProvider 定义运行态诊断读取接口。
type RuntimeDiagnosticsProvider interface {
	GetRuntimeRevision() string
	GetRuntimeActiveConfigName() string
	GetRuntimeModeIssues() map[string]SettingsModeIssue
}

// RuntimeController 组合运行态应用与诊断能力。
type RuntimeController interface {
	RuntimeApplyProvider
	RuntimeDiagnosticsProvider
}

// RuntimeControlService 定义运行时控制接口。
type RuntimeControlService interface {
	RuntimeController
}

// RuntimeDiagnosticsService 定义扩展运行时诊断接口。
type RuntimeDiagnosticsService interface {
	RuntimeDiagnosticsProvider
	GetToolSchemaRevision() string
	GetToolAvailabilityRevision() string
	GetLastRuntimeRebuildReason() string
	GetLastRuntimeRebuildAt() time.Time
	GetPlannerReadyReason() string
	GetActiveInstanceID() string
}

// MutationBuilder 构造统一的设置变更反馈。
type MutationBuilder struct {
	diagnostics RuntimeDiagnosticsProvider
}

// NewMutationBuilder 创建 mutation 构造器。
func NewMutationBuilder(diagnostics RuntimeDiagnosticsProvider) MutationBuilder {
	return MutationBuilder{diagnostics: diagnostics}
}

// New 创建持久化写入 mutation 结果。
func (b MutationBuilder) New(scope string) SettingsMutationResult {
	result := SettingsMutationResult{
		Scope:        strings.TrimSpace(scope),
		Persisted:    true,
		ConfigLoaded: true,
	}
	return b.Attach(result)
}

// NewTransient 创建非持久化切换类 mutation 结果。
func (b MutationBuilder) NewTransient(scope string) SettingsMutationResult {
	result := SettingsMutationResult{
		Scope:        strings.TrimSpace(scope),
		ConfigLoaded: true,
	}
	return b.Attach(result)
}

// Attach 为 mutation 附加运行态诊断。
func (b MutationBuilder) Attach(result SettingsMutationResult) SettingsMutationResult {
	result = FinalizeMutationResult(result)
	if b.diagnostics == nil {
		return result
	}
	result.ActiveAgentConfig = strings.TrimSpace(b.diagnostics.GetRuntimeActiveConfigName())
	result.RuntimeRevision = strings.TrimSpace(b.diagnostics.GetRuntimeRevision())
	if issues := b.diagnostics.GetRuntimeModeIssues(); len(issues) > 0 {
		result.ModeIssues = issues
	}
	return FinalizeMutationResult(result)
}

// FinalizeMutationResult 统一收敛影响域、所需动作与下一步提示。
func FinalizeMutationResult(result SettingsMutationResult) SettingsMutationResult {
	result.Scope = strings.TrimSpace(result.Scope)
	result.AffectedDomain = strings.TrimSpace(result.AffectedDomain)
	result.RequiredAction = strings.TrimSpace(result.RequiredAction)
	result.NextStep = strings.TrimSpace(result.NextStep)
	result.ConfigRevision = strings.TrimSpace(result.ConfigRevision)
	result.ActiveAgentConfig = strings.TrimSpace(result.ActiveAgentConfig)
	result.RuntimeRevision = strings.TrimSpace(result.RuntimeRevision)
	result.Warnings = normalizeMutationStringList(result.Warnings)
	result.AffectedPaths = normalizeMutationStringList(result.AffectedPaths)
	result.BlockingRuns = normalizeMutationStringList(result.BlockingRuns)
	result.BlockingInstances = normalizeMutationStringList(result.BlockingInstances)

	if result.AffectedDomain == "" {
		result.AffectedDomain = inferMutationDomain(result.Scope, result.AffectedPaths)
	}
	if result.RuntimeApplied {
		result.RequiredAction = MutationRequiredActionNone
	} else if result.RequiredAction == "" {
		result.RequiredAction = inferRequiredAction(result.Scope, result.AffectedDomain, result.AffectedPaths)
	}
	if result.NextStep == "" {
		result.NextStep = inferNextStep(result.Scope, result.AffectedDomain, result.RequiredAction)
	}
	return result
}

func normalizeMutationStringList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func inferMutationDomain(scope string, affectedPaths []string) string {
	if domain := inferDomainFromPaths(affectedPaths); domain != "" {
		return domain
	}
	switch strings.TrimSpace(scope) {
	case "llm":
		return MutationDomainModels
	case "agent_config", "agent_config_delete", "agent_config_switch":
		return MutationDomainAgents
	case "skill", "skill_delete", "skill_import", "skill_package_toggle":
		return MutationDomainSkills
	default:
		return ""
	}
}

func inferDomainFromPaths(affectedPaths []string) string {
	for _, path := range affectedPaths {
		switch classifyConfigPath(path) {
		case MutationDomainSystem:
			return MutationDomainSystem
		case MutationDomainServices:
			return MutationDomainServices
		case MutationDomainModels:
			return MutationDomainModels
		case MutationDomainAgents:
			return MutationDomainAgents
		case MutationDomainTools:
			return MutationDomainTools
		case MutationDomainSkills:
			return MutationDomainSkills
		}
	}
	return ""
}

func classifyConfigPath(path string) string {
	trimmed := strings.ToLower(strings.TrimSpace(path))
	if trimmed == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(trimmed, "llm"):
		return MutationDomainModels
	case strings.HasPrefix(trimmed, "plan_mode"),
		strings.HasPrefix(trimmed, "planner"),
		strings.HasPrefix(trimmed, "execution_mode"):
		return MutationDomainAgents
	case trimmed == "agent.loop_max_iterations":
		return MutationDomainSystem
	case strings.HasPrefix(trimmed, "tools"):
		return MutationDomainTools
	case strings.HasPrefix(trimmed, "skills"):
		return MutationDomainSkills
	case strings.HasPrefix(trimmed, "ports"),
		strings.HasPrefix(trimmed, "compute"),
		strings.HasPrefix(trimmed, "middleware"),
		strings.HasPrefix(trimmed, "backend"),
		strings.HasPrefix(trimmed, "services.lifecycle"),
		strings.HasPrefix(trimmed, "service_lifecycle"):
		return MutationDomainServices
	case strings.HasPrefix(trimmed, "qgis"),
		strings.HasPrefix(trimmed, "session"),
		strings.HasPrefix(trimmed, "performance"),
		strings.HasPrefix(trimmed, "log."),
		strings.HasPrefix(trimmed, "logging"),
		strings.HasPrefix(trimmed, "data_dir"):
		return MutationDomainSystem
	default:
		return ""
	}
}

func inferRequiredAction(scope, affectedDomain string, affectedPaths []string) string {
	switch strings.TrimSpace(scope) {
	case "skill", "skill_delete", "skill_import":
		return MutationRequiredActionNone
	case "llm", "agent_config", "agent_config_delete", "agent_config_switch":
		return MutationRequiredActionRuntimeRebuild
	}

	if affectedDomain == MutationDomainSystem || affectedDomain == MutationDomainServices {
		if requiresDesktopRestart(affectedPaths) {
			return MutationRequiredActionDesktopRestart
		}
		return MutationRequiredActionNone
	}
	if affectedDomain == MutationDomainModels || affectedDomain == MutationDomainAgents || affectedDomain == MutationDomainTools {
		return MutationRequiredActionRuntimeRebuild
	}
	return MutationRequiredActionNone
}

func requiresDesktopRestart(affectedPaths []string) bool {
	for _, path := range affectedPaths {
		trimmed := strings.ToLower(strings.TrimSpace(path))
		if trimmed == "" {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "ports"),
			strings.HasPrefix(trimmed, "qgis"),
			strings.HasPrefix(trimmed, "compute"),
			strings.HasPrefix(trimmed, "middleware"),
			strings.HasPrefix(trimmed, "backend"),
			strings.HasPrefix(trimmed, "services.lifecycle"),
			strings.HasPrefix(trimmed, "service_lifecycle"):
			return true
		}
	}
	return false
}

func inferNextStep(scope, affectedDomain, requiredAction string) string {
	switch requiredAction {
	case MutationRequiredActionDesktopRestart:
		return "请重启桌面端，使新的端口、QGIS 与服务托管配置完全生效。"
	case MutationRequiredActionStopActiveRuns:
		return "请先停止受影响的运行中会话，再重新保存该设置。"
	case MutationRequiredActionRuntimeRebuild:
		switch affectedDomain {
		case MutationDomainModels:
			return "请重建当前运行时，使新的模型与推理参数立即生效。"
		case MutationDomainAgents:
			return "请重新激活当前方案或执行运行时重建，使新的智能体编排策略生效。"
		case MutationDomainTools:
			return "请重建当前运行时，使工具目录变更重新装载。"
		case MutationDomainSkills:
			return "请新建会话或手动重建运行时后加载最新技能资产。"
		default:
			return "请执行运行时重建以应用最新配置。"
		}
	case MutationRequiredActionNone:
		if strings.HasPrefix(strings.TrimSpace(scope), "skill") {
			return "技能变更将在下次会话创建或下一次运行时重建后生效。"
		}
	}
	return ""
}

// AppendMutationWarning 为 mutation 追加 warning，并去重空值。
func AppendMutationWarning(result *SettingsMutationResult, warning string) {
	if result == nil {
		return
	}
	trimmed := strings.TrimSpace(warning)
	if trimmed == "" {
		return
	}
	for _, item := range result.Warnings {
		if item == trimmed {
			return
		}
	}
	result.Warnings = append(result.Warnings, trimmed)
}

// MutationEnvelope 为业务返回值统一附加 mutation 字段。
func MutationEnvelope(payload map[string]any, mutation SettingsMutationResult) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["mutation"] = FinalizeMutationResult(mutation)
	return payload
}

// IsPlanModeConfigPath 判断配置路径是否属于 plan_mode。
func IsPlanModeConfigPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	return trimmed == "plan_mode" || strings.HasPrefix(trimmed, "plan_mode.")
}

// HasPlanModeConfigItem 判断批量配置中是否包含 plan_mode 项。
func HasPlanModeConfigItem(items []config.ConfigItem) bool {
	for _, item := range items {
		if IsPlanModeConfigPath(item.Path) {
			return true
		}
	}
	return false
}

// ApplyPlanModeMutation 对 plan_mode 变更执行运行态刷新。
func ApplyPlanModeMutation(builder MutationBuilder, runtime RuntimeApplyProvider, result *SettingsMutationResult, warningPrefix string) {
	if result == nil || runtime == nil {
		return
	}
	if err := runtime.ApplyPlanModeConfig(); err != nil {
		AppendMutationWarning(result, strings.TrimSpace(warningPrefix)+err.Error())
		*result = FinalizeMutationResult(*result)
		return
	}
	result.RuntimeApplied = true
	*result = builder.Attach(*result)
}

// ApplyLLMMutation 对 LLM 配置变更执行运行态刷新。
func ApplyLLMMutation(builder MutationBuilder, runtime RuntimeApplyProvider, result *SettingsMutationResult, warningPrefix string) {
	if result == nil || runtime == nil {
		return
	}
	if err := runtime.ApplyLLMConfig(); err != nil {
		AppendMutationWarning(result, strings.TrimSpace(warningPrefix)+err.Error())
		*result = FinalizeMutationResult(*result)
		return
	}
	result.RuntimeApplied = true
	*result = builder.Attach(*result)
}

// ApplyAgentMutation 对指定 agent config 变更执行运行态刷新。
func ApplyAgentMutation(builder MutationBuilder, runtime RuntimeApplyProvider, result *SettingsMutationResult, configName, warningPrefix string) {
	if result == nil || runtime == nil {
		return
	}
	applied, err := runtime.ApplyAgentConfig(strings.TrimSpace(configName))
	result.RuntimeApplied = applied
	if err != nil {
		AppendMutationWarning(result, strings.TrimSpace(warningPrefix)+err.Error())
		*result = FinalizeMutationResult(*result)
		return
	}
	if !applied {
		result.RequiredAction = MutationRequiredActionNone
		result.NextStep = ""
	}
	*result = builder.Attach(*result)
}

// ApplyActiveRuntimeMutation 对当前激活运行态执行重建。
func ApplyActiveRuntimeMutation(builder MutationBuilder, runtime RuntimeApplyProvider, result *SettingsMutationResult, warningPrefix string) {
	if result == nil || runtime == nil {
		return
	}
	if err := runtime.ApplyActiveRuntime(); err != nil {
		AppendMutationWarning(result, strings.TrimSpace(warningPrefix)+err.Error())
		*result = FinalizeMutationResult(*result)
		return
	}
	result.RuntimeApplied = true
	*result = builder.Attach(*result)
}
