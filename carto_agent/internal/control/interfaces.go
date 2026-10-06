package control

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
	"github.com/autocarto/carto_agent/internal/toolrouter"
)

// RuntimeCoordinator 统一提供运行态快照视图。
type RuntimeCoordinator interface {
	Snapshot(ctx context.Context) middlewareclient.RuntimeSnapshot
	Services(ctx context.Context) RuntimeServicesView
}

// RuntimeServicesView 表示精简的服务视图。
type RuntimeServicesView struct {
	Services                 map[string]middlewareclient.ServiceStatus `json:"services"`
	Instances                RuntimeInstancesView                      `json:"instances"`
	ConfigRevision           string                                    `json:"config_revision,omitempty"`
	DiscoveryRevision        string                                    `json:"discovery_revision,omitempty"`
	ActiveConfig             string                                    `json:"active_config,omitempty"`
	ServicePhase             string                                    `json:"service_phase,omitempty"`
	RuntimeRevision          string                                    `json:"runtime_revision,omitempty"`
	RuntimeActiveAgentConfig string                                    `json:"runtime_active_agent_config,omitempty"`
	ActiveInstanceID         string                                    `json:"active_instance_id,omitempty"`
	ModeIssues               map[string]middlewareclient.ModeIssue     `json:"mode_issues,omitempty"`
	ModeReadiness            map[string]middlewareclient.ModeReadiness `json:"mode_readiness,omitempty"`
	ToolCatalogRevision      string                                    `json:"tool_catalog_revision,omitempty"`
	ToolSchemaRevision       string                                    `json:"tool_schema_revision,omitempty"`
	ToolAvailabilityRevision string                                    `json:"tool_availability_revision,omitempty"`
	ToolRevisions            middlewareclient.ToolRevisions            `json:"tool_revisions,omitempty"`
	LastRebuildReason        string                                    `json:"last_rebuild_reason,omitempty"`
	LastRebuildAt            string                                    `json:"last_rebuild_at,omitempty"`
	PlannerReadyReason       string                                    `json:"planner_ready_reason,omitempty"`
	Stale                    bool                                      `json:"stale,omitempty"`
	Version                  uint64                                    `json:"version,omitempty"`
	Timestamp                string                                    `json:"timestamp,omitempty"`
}

// RuntimeInstancesView 表示服务视图中的实例摘要。
type RuntimeInstancesView struct {
	ActiveID      string `json:"active_id,omitempty"`
	InstanceCount int    `json:"instance_count,omitempty"`
	RunningCount  int    `json:"running_count,omitempty"`
}

// ActiveRunInfo 表示活跃运行会话摘要。
type ActiveRunInfo struct {
	SessionID        string `json:"session_id,omitempty"`
	Status           string `json:"status,omitempty"`
	BoundInstanceID  string `json:"bound_instance_id,omitempty"`
	BoundProjectPath string `json:"bound_project_path,omitempty"`
}

// ActiveRunInspector 提供当前活跃运行视图。
type ActiveRunInspector interface {
	ListActiveRuns() []ActiveRunInfo
}

// SettingsMutationService 统一处理设置写入与运行态应用。
type SettingsMutationService interface {
	GetAllConfig() (map[string]any, error)
	GetConfig(path, userID string) (any, error)
	GetLLMConfig() (any, error)
	SaveConfig(path string, value any, userID string) (settings.SettingsMutationResult, error)
	BatchSetConfig(items []config.ConfigItem) (settings.SettingsMutationResult, error)
	SaveLLMProviders(llmConfig map[string]any) (settings.SettingsMutationResult, error)
	SwitchActiveLLMProvider(providerName string) (settings.SettingsMutationResult, error)
	CreateAgentConfig(userID, name, cloneFrom string) (map[string]any, error)
	CloneAgentConfig(userID, sourceName, targetName string) (map[string]any, error)
	SaveAgentConfig(userID, name string, profile settings.AgentProfileView) (settings.SettingsMutationResult, error)
	SaveAgentConfigData(userID, name string, data *config.AgentConfigData) (settings.SettingsMutationResult, error)
	DeleteAgentConfig(userID, name string) (map[string]any, error)
	ActivateAgentConfig(userID, name string) (settings.SettingsMutationResult, error)
	AddAgentToConfig(configName, agentName string, agent config.AgentDefData) (map[string]any, error)
	UpdateAgentInConfig(configName, agentName string, agent config.AgentDefData) (settings.SettingsMutationResult, error)
	RemoveAgentFromConfig(configName, agentName string) (settings.SettingsMutationResult, error)
	ToggleSkillPackage(name string, enabled bool) (settings.SettingsMutationResult, error)
	SaveSkillWithScope(name, scope, configName, description, content string) (settings.SettingsMutationResult, error)
	SaveSkill(name, description, content string) (settings.SettingsMutationResult, error)
	DeleteSkillWithScope(name, scope, configName string) (settings.SettingsMutationResult, error)
	DeleteSkill(name string) (settings.SettingsMutationResult, error)
	ImportSkillWithScope(sourcePath string, overwrite bool, scope, configName string) (map[string]any, error)
	ImportSkill(sourcePath string, overwrite bool) (map[string]any, error)
}

// AssetCatalogService 统一提供资产域视图。
type AssetCatalogService interface {
	SettingsWorkspace() map[string]any
	ListAgentConfigs(userID string) ([]config.StoredAgentConfig, error)
	GetAgentConfig(userID, name string) (*config.StoredAgentConfig, error)
	GetAgentProfile(name string) (settings.AgentProfileView, error)
	GetAgent(configName, agentName string) (config.AgentDefData, error)
	GetToolCatalog(ctx context.Context, scope string) (ToolCatalogView, error)
	ListSkillPackages() ([]settings.SkillPackageSummary, error)
	GetSkillPackage(name string) (settings.SkillPackageDetail, error)
	GetSkillPackageReferences(name string) []config.SkillReference
	ListSkills() ([]map[string]any, error)
	GetSkillContent(name string) (map[string]any, error)
	GetSkillReferences(name string) []config.SkillReference
}

// ToolCatalogView 表示工具目录响应视图。
type ToolCatalogView struct {
	Scope             string               `json:"scope"`
	ConfigName        string               `json:"config_name,omitempty"`
	RuntimeRevision   string               `json:"runtime_revision,omitempty"`
	Catalog           toolcatalog.Contract `json:"catalog"`
	AvailableCount    int                  `json:"available_count,omitempty"`
	ConfiguredCount   int                  `json:"configured_count,omitempty"`
	ToolCatalogStatus map[string]any       `json:"tool_catalog_status,omitempty"`
	ToolInfos         []toolcatalog.Item   `json:"tool_infos"`
	Tools             []string             `json:"tools"`
}

type toolCatalogPublicProjection struct {
	Scope           string               `json:"scope"`
	ConfigName      string               `json:"config_name,omitempty"`
	RuntimeRevision string               `json:"runtime_revision,omitempty"`
	Catalog         toolcatalog.Contract `json:"catalog"`
}

type ToolCatalogFailureMode string

const (
	ToolCatalogFailureModeNone               ToolCatalogFailureMode = ""
	ToolCatalogFailureModeUnsupportedRuntime ToolCatalogFailureMode = ToolCatalogFailureMode(toolcatalog.DegradationStatusUnsupportedRuntime)
	ToolCatalogFailureModeAbnormalEmpty      ToolCatalogFailureMode = ToolCatalogFailureMode(toolcatalog.DegradationStatusAbnormalEmpty)
)

type ToolCatalogResponseProjection struct {
	Payload     map[string]any
	SharedError SharedErrorDictionary
	FailureMode ToolCatalogFailureMode
	CatalogID   string
	Scope       string
	ConfigName  string
	RuntimeRev  string
}

func (v ToolCatalogView) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.publicProjection())
}

func (v ToolCatalogView) PublicPayload() map[string]any {
	projection := v.publicProjection()
	return map[string]any{
		"scope":            projection.Scope,
		"config_name":      projection.ConfigName,
		"runtime_revision": projection.RuntimeRevision,
		"catalog":          projection.Catalog,
	}
}

func (v ToolCatalogView) SharedErrorDictionary() SharedErrorDictionary {
	return SharedErrorDictionaryFromToolCatalogContract(v.Catalog, "")
}

func (v ToolCatalogView) ResponseProjection() ToolCatalogResponseProjection {
	payload := v.PublicPayload()
	sharedError := v.SharedErrorDictionary()
	if sharedError.Code != "" {
		payload["shared_error"] = sharedError
	}
	return ToolCatalogResponseProjection{
		Payload:     payload,
		SharedError: sharedError,
		FailureMode: classifyToolCatalogFailureMode(v.Catalog.DegradationStatus),
		CatalogID:   strings.TrimSpace(v.Catalog.ID),
		Scope:       strings.TrimSpace(v.Scope),
		ConfigName:  strings.TrimSpace(v.ConfigName),
		RuntimeRev:  strings.TrimSpace(v.RuntimeRevision),
	}
}

func classifyToolCatalogFailureMode(status string) ToolCatalogFailureMode {
	switch strings.TrimSpace(status) {
	case toolcatalog.DegradationStatusUnsupportedRuntime:
		return ToolCatalogFailureModeUnsupportedRuntime
	case toolcatalog.DegradationStatusAbnormalEmpty:
		return ToolCatalogFailureModeAbnormalEmpty
	default:
		return ToolCatalogFailureModeNone
	}
}

func (v ToolCatalogView) publicProjection() toolCatalogPublicProjection {
	projection := toolCatalogPublicProjection{
		Scope:           strings.TrimSpace(v.Scope),
		ConfigName:      strings.TrimSpace(v.ConfigName),
		RuntimeRevision: strings.TrimSpace(v.RuntimeRevision),
		Catalog:         v.Catalog,
	}
	if projection.Catalog.Policy != nil {
		if scope := strings.TrimSpace(projection.Catalog.Policy.Scope); scope != "" {
			projection.Scope = scope
		}
		if configName := strings.TrimSpace(projection.Catalog.Policy.ConfigName); configName != "" {
			projection.ConfigName = configName
		}
	}
	return projection
}

// RealtimeHub 统一承接控制面广播。
type RealtimeHub interface {
	BroadcastRuntimeSnapshot(snapshot middlewareclient.RuntimeSnapshot)
	BroadcastSkillCatalogChanged(action, source, skillName string)
	BroadcastSessionChange(changeType, sessionID string, extra map[string]any)
	BroadcastSessionLogReset(sessionID, projectPath, sessionDir string)
}

// RuntimeSource 提供运行时快照读取能力。
type RuntimeSource interface {
	GetRuntimeSnapshot() middlewareclient.RuntimeSnapshot
	GetRuntimeState() middlewareclient.RuntimeState
}

// ToolAggregatorSource 提供聚合工具目录快照。
type ToolAggregatorSource interface {
	GetToolAggregatorSnapshot() toolrouter.Snapshot
}

// SkillCatalogStore 提供 skill 读写能力。
type SkillCatalogStore interface {
	ListSkillPackages() ([]settings.SkillPackageSummary, error)
	GetSkillPackage(name string) (settings.SkillPackageDetail, error)
	GetSkillPackageReferences(name string) []config.SkillReference
	SetSkillPackageEnabled(name string, enabled bool) error
	ListSkills() ([]map[string]any, error)
	GetSkillContent(name string) (map[string]any, error)
	SaveSkill(name, description, content string) error
	DeleteSkill(name string) error
	ImportSkill(sourcePath string, overwrite bool) (map[string]any, error)
}
