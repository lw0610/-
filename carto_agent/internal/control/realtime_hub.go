package control

import "github.com/autocarto/carto_agent/internal/middlewareclient"

// RealtimePublisher 表示底层广播传输器。
type RealtimePublisher interface {
	BroadcastRuntimeSnapshot(snapshot middlewareclient.RuntimeSnapshot)
	BroadcastSkillCatalogChanged(action, source, skillName string)
	BroadcastSessionChange(changeType, sessionID string, extra map[string]any)
	BroadcastSessionLogReset(sessionID, projectPath, sessionDir string)
}

type noopRealtimeHub struct{}

type realtimeHubService struct {
	publisher RealtimePublisher
}

// NewRealtimeHub 创建广播服务。
func NewRealtimeHub(publisher RealtimePublisher) RealtimeHub {
	if publisher == nil {
		return noopRealtimeHub{}
	}
	return &realtimeHubService{publisher: publisher}
}

// NewNoopRealtimeHub 创建空广播服务。
func NewNoopRealtimeHub() RealtimeHub {
	return noopRealtimeHub{}
}

func (noopRealtimeHub) BroadcastRuntimeSnapshot(snapshot middlewareclient.RuntimeSnapshot) {
	_ = snapshot
}

func (noopRealtimeHub) BroadcastSkillCatalogChanged(action, source, skillName string) {
	_, _, _ = action, source, skillName
}

func (noopRealtimeHub) BroadcastSessionChange(changeType, sessionID string, extra map[string]any) {
	_, _, _ = changeType, sessionID, extra
}

func (noopRealtimeHub) BroadcastSessionLogReset(sessionID, projectPath, sessionDir string) {
	_, _, _ = sessionID, projectPath, sessionDir
}

func (h *realtimeHubService) BroadcastRuntimeSnapshot(snapshot middlewareclient.RuntimeSnapshot) {
	h.publisher.BroadcastRuntimeSnapshot(snapshot)
}

func (h *realtimeHubService) BroadcastSkillCatalogChanged(action, source, skillName string) {
	h.publisher.BroadcastSkillCatalogChanged(action, source, skillName)
}

func (h *realtimeHubService) BroadcastSessionChange(changeType, sessionID string, extra map[string]any) {
	h.publisher.BroadcastSessionChange(changeType, sessionID, extra)
}

func (h *realtimeHubService) BroadcastSessionLogReset(sessionID, projectPath, sessionDir string) {
	h.publisher.BroadcastSessionLogReset(sessionID, projectPath, sessionDir)
}
