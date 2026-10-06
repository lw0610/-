package discovery

import "time"

const (
	ServiceNameQGISBridge      = "qgis_bridge"
	ServiceNameAgentQGISBridge = "agent_qgis_bridge"

	SourceRegister       = "register"
	SourceScan           = "scan"
	SourceReverseWS      = "reverse_ws"
	SourceAgentReverseWS = "agent_reverse_ws"
)

// QGISInstance 表示一个 QGIS 实例
type QGISInstance struct {
	ID                 string                 `json:"id"`
	MachineFingerprint string                 `json:"machine_fingerprint,omitempty"`
	StartTime          string                 `json:"start_time,omitempty"`
	PID                int                    `json:"pid"`
	QGISVersion        string                 `json:"qgis_version"`
	ProjectPath        string                 `json:"project_path"`
	Status             string                 `json:"status"` // running, stopped, dead
	StatusReason       string                 `json:"status_reason,omitempty"`
	StatusDetail       string                 `json:"status_detail,omitempty"`
	Services           map[string]ServiceInfo `json:"services"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
	LastHeart          time.Time              `json:"last_heartbeat"`
	Source             string                 `json:"source"`
}

// ServiceInfo 服务信息
type ServiceInfo struct {
	Port          int       `json:"port"`
	Status        string    `json:"status"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// ServicePortEntry 服务端口注册条目
type ServicePortEntry struct {
	ServiceName  string `json:"service_name"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Protocol     string `json:"protocol"`
	PID          int    `json:"pid"`
	Status       string `json:"status"`
	RegisteredAt string `json:"registered_at"`
	LastSeen     string `json:"last_seen"`
}

// InstanceEvent 实例变化事件
type InstanceEvent struct {
	Type     string        `json:"type"` // added, removed, changed
	Instance *QGISInstance `json:"instance"`
}

// RegisterRequest Python 端注册实例请求
type RegisterRequest struct {
	InstanceID         string `json:"instance_id"`
	MachineFingerprint string `json:"machine_fingerprint,omitempty"`
	StartTime          string `json:"start_time,omitempty"`
	PID                int    `json:"pid"`
	QGISVersion        string `json:"qgis_version"`
	ProjectPath        string `json:"project_path"`
	Source             string `json:"source,omitempty"`
}

// EventRequest Python 端上报事件请求
type EventRequest struct {
	EventType string                 `json:"event_type"` // project_opened, project_saved, project_closed
	Data      map[string]interface{} `json:"data,omitempty"`
}
