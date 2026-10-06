package middlewareclient

import (
	"time"

	"github.com/autocarto/carto_agent/internal/discovery"
)

// InstanceSnapshot 表示 middleware 暴露的实例快照。
type InstanceSnapshot struct {
	Instances      []*discovery.QGISInstance `json:"instances"`
	ActiveID       string                    `json:"active_id,omitempty"`
	QGISAvailable  bool                      `json:"qgis_available"`
	InstanceCount  int                       `json:"instance_count"`
	RunningCount   int                       `json:"running_count,omitempty"`
	MiddlewareMode string                    `json:"middleware_mode,omitempty"`
	Stale          bool                      `json:"stale,omitempty"`
}

// ServiceStatus 表示单个服务运行状态。
type ServiceStatus struct {
	ID             string         `json:"id,omitempty"`
	Status         string         `json:"status"`
	Message        string         `json:"message,omitempty"`
	Port           int            `json:"port,omitempty"`
	Host           string         `json:"host,omitempty"`
	Source         string         `json:"source,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	LastHeartbeat  string         `json:"last_heartbeat,omitempty"`
	LastStartedAt  string         `json:"last_started_at,omitempty"`
	LastUpdatedAt  string         `json:"last_updated_at,omitempty"`
	UpdatedAt      time.Time      `json:"updated_at,omitempty"`
	PID            int            `json:"pid,omitempty"`
	Ready          bool           `json:"ready,omitempty"`
	Enabled        bool           `json:"enabled,omitempty"`
	ProviderCount  int            `json:"provider_count,omitempty"`
	InstanceCount  int            `json:"instance_count,omitempty"`
	RunningCount   int            `json:"running_count,omitempty"`
	RestartAttempt int            `json:"restart_attempt,omitempty"`
	Diagnostics    map[string]any `json:"diagnostics,omitempty"`
}

// RuntimeInstances 表示实例运行时摘要。
type RuntimeInstances struct {
	ActiveID      string `json:"active_id,omitempty"`
	InstanceCount int    `json:"instance_count"`
	RunningCount  int    `json:"running_count"`
}

// RuntimeStatus 表示 middleware 暴露的统一运行状态。
type RuntimeStatus struct {
	Services  map[string]ServiceStatus `json:"services"`
	Instances RuntimeInstances         `json:"instances"`
	Stale     bool                     `json:"stale,omitempty"`
}

// InstancesSnapshot 表示运行时实例完整快照。
type InstancesSnapshot struct {
	Items         []*discovery.QGISInstance `json:"items"`
	ActiveID      string                    `json:"active_id,omitempty"`
	InstanceCount int                       `json:"instance_count"`
	RunningCount  int                       `json:"running_count"`
}

// ModeIssue 表示某个模式的诊断问题。
type ModeIssue struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// ModeReadiness 表示某个模式的就绪状态。
type ModeReadiness struct {
	Enabled bool   `json:"enabled"`
	Ready   bool   `json:"ready"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// ToolRevisions 表示工具目录各类修订号。
type ToolRevisions struct {
	Catalog      string `json:"catalog,omitempty"`
	Schema       string `json:"schema,omitempty"`
	Availability string `json:"availability,omitempty"`
}

// RuntimeSnapshot 表示 middleware 统一运行时快照。
type RuntimeSnapshot struct {
	Version                  uint64                   `json:"version"`
	Timestamp                time.Time                `json:"timestamp"`
	ConfigRevision           string                   `json:"config_revision,omitempty"`
	DiscoveryRevision        string                   `json:"discovery_revision,omitempty"`
	ActiveConfig             string                   `json:"active_config,omitempty"`
	ServicePhase             string                   `json:"service_phase,omitempty"`
	ToolCatalogRevision      string                   `json:"tool_catalog_revision,omitempty"`
	ToolSchemaRevision       string                   `json:"tool_schema_revision,omitempty"`
	ToolAvailabilityRevision string                   `json:"tool_availability_revision,omitempty"`
	ToolRevisions            ToolRevisions            `json:"tool_revisions,omitempty"`
	LastRebuildReason        string                   `json:"last_rebuild_reason,omitempty"`
	LastRebuildAt            time.Time                `json:"last_rebuild_at,omitempty"`
	RuntimeRevision          string                   `json:"runtime_revision,omitempty"`
	ActiveAgentConfig        string                   `json:"active_agent_config,omitempty"`
	RuntimeActiveAgentConfig string                   `json:"runtime_active_agent_config,omitempty"`
	PlanEnabled              bool                     `json:"plan_enabled"`
	PlanReady                bool                     `json:"plan_ready"`
	ModeIssues               map[string]ModeIssue     `json:"mode_issues,omitempty"`
	ModeReadiness            map[string]ModeReadiness `json:"mode_readiness,omitempty"`
	Services                 map[string]ServiceStatus `json:"services"`
	Instances                InstancesSnapshot        `json:"instances"`
	Stale                    bool                     `json:"stale,omitempty"`
}

// RuntimeState 表示 agent 侧缓存的运行时状态。
type RuntimeState struct {
	Snapshot         *RuntimeSnapshot `json:"snapshot,omitempty"`
	Stale            bool             `json:"stale"`
	LastSnapshotTime time.Time        `json:"last_snapshot_time,omitempty"`
}

// ToolCatalogItem 表示 middleware 聚合后的工具目录项。
type ToolCatalogItem struct {
	Name                string         `json:"name"`
	ProviderID          string         `json:"provider_id,omitempty"`
	RawName             string         `json:"raw_name,omitempty"`
	Description         string         `json:"description,omitempty"`
	Category            string         `json:"category,omitempty"`
	Domain              string         `json:"domain,omitempty"`
	Source              string         `json:"source,omitempty"`
	InputSchema         map[string]any `json:"input_schema,omitempty"`
	Backend             string         `json:"backend,omitempty"`
	Visibility          string         `json:"visibility,omitempty"`
	AuthorizationScopes []string       `json:"authorization_scopes,omitempty"`
	Available           bool           `json:"available,omitempty"`
	UnavailableReason   string         `json:"unavailable_reason,omitempty"`
	InstanceIDs         []string       `json:"instance_ids,omitempty"`
}

// RuntimeTool 兼容统一通信架构中的工具目录项命名。
type RuntimeTool = ToolCatalogItem

// ToolCatalogResponse 表示 /api/tools 响应。
type ToolCatalogResponse struct {
	Revision            string            `json:"revision,omitempty"`
	ToolCatalogRevision string            `json:"tool_catalog_revision,omitempty"`
	Tools               []ToolCatalogItem `json:"tools"`
}

// RuntimeEvent 表示 SSE 事件。
type RuntimeEvent struct {
	ID            string
	Event         string
	Data          []byte
	Snapshot      *RuntimeSnapshot
	Service       string
	ServiceStatus *ServiceStatus
	Timestamp     time.Time
}

// SelectInstanceResponse 表示活动实例切换结果。
type SelectInstanceResponse struct {
	ActiveID string                  `json:"active_id,omitempty"`
	Instance *discovery.QGISInstance `json:"instance,omitempty"`
}

// CloseInstanceResponse 表示实例关闭请求结果。
type CloseInstanceResponse struct {
	Status      string                  `json:"status,omitempty"`
	InstanceID  string                  `json:"instance_id,omitempty"`
	RequestedAt string                  `json:"requested_at,omitempty"`
	ActiveID    string                  `json:"active_id,omitempty"`
	Instance    *discovery.QGISInstance `json:"instance,omitempty"`
}

// InstanceProjectActionResponse 表示实例项目操作结果。
type InstanceProjectActionResponse struct {
	Status      string `json:"status,omitempty"`
	InstanceID  string `json:"instance_id,omitempty"`
	RequestedAt string `json:"requested_at,omitempty"`
	Result      any    `json:"result,omitempty"`
}
