package websocket

import (
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
)

// SetControlServices 注入共享的 control 层服务。
func (s *DeepServer) SetControlServices(settingsService control.SettingsMutationService, assetCatalog control.AssetCatalogService, realtimeHub control.RealtimeHub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settingsMutationService = settingsService
	s.assetCatalogService = assetCatalog
	if realtimeHub == nil {
		s.realtimeHub = control.NewRealtimeHub(s)
	} else {
		s.realtimeHub = realtimeHub
	}
}

func (s *DeepServer) resetControlServices() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settingsMutationService = nil
	s.assetCatalogService = nil
	s.skillCatalogStore = nil
	if s.realtimeHub == nil {
		s.realtimeHub = control.NewRealtimeHub(s)
	}
}

func (s *DeepServer) settingsService() control.SettingsMutationService {
	s.mu.Lock()
	if s.settingsMutationService != nil {
		service := s.settingsMutationService
		s.mu.Unlock()
		return service
	}
	s.mu.Unlock()
	skillCatalog := s.sharedSkillCatalog()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settingsMutationService != nil {
		return s.settingsMutationService
	}
	s.settingsMutationService = control.NewSettingsMutationService(control.SettingsMutationServiceDeps{
		ConfigService:      s.configService,
		AgentConfigManager: s.agentConfigManager,
		RuntimeController:  s,
		SkillCatalog:       skillCatalog,
		ActiveRuns:         s,
		SnapshotRefresher:  s.capabilityRefresher,
	})
	if s.realtimeHub == nil {
		s.realtimeHub = control.NewRealtimeHub(s)
	}
	return s.settingsMutationService
}

func (s *DeepServer) assetService() control.AssetCatalogService {
	s.mu.Lock()
	if s.assetCatalogService != nil {
		service := s.assetCatalogService
		s.mu.Unlock()
		return service
	}
	s.mu.Unlock()
	skillCatalog := s.sharedSkillCatalog()
	runtimeCoordinator := control.NewRuntimeCoordinator(control.RuntimeCoordinatorDeps{
		AgentConfigManager: s.agentConfigManager,
		RuntimeSource:      s,
		RuntimeDiagnostics: s,
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.assetCatalogService != nil {
		return s.assetCatalogService
	}
	s.assetCatalogService = control.NewAssetCatalogService(control.AssetCatalogServiceDeps{
		AgentConfigManager:   s.agentConfigManager,
		SkillCatalog:         skillCatalog,
		RuntimeCoordinator:   runtimeCoordinator,
		RuntimeDiagnostics:   s,
		ToolAggregatorSource: s,
		SnapshotStore:        s.capabilitySnapshotStore,
		SnapshotMode:         s.capabilitySnapshotMode,
	})
	if s.realtimeHub == nil {
		s.realtimeHub = control.NewRealtimeHub(s)
	}
	return s.assetCatalogService
}

func (s *DeepServer) realtimeService() control.RealtimeHub {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.realtimeHub == nil {
		s.realtimeHub = control.NewRealtimeHub(s)
	}
	return s.realtimeHub
}

// BroadcastRuntimeSnapshot 对外暴露 runtime snapshot 广播能力，供 RealtimeHub 调用。
func (s *DeepServer) BroadcastRuntimeSnapshot(snapshot middlewareclient.RuntimeSnapshot) {
	s.broadcastRuntimeSnapshot(snapshot)
}

// BroadcastSkillCatalogChanged 对外暴露技能目录广播能力，供 RealtimeHub 调用。
func (s *DeepServer) BroadcastSkillCatalogChanged(action, source, skillName string) {
	s.broadcastSkillCatalogChanged(action, source, skillName)
}

// BroadcastSessionChange 对外暴露会话广播能力，供 RealtimeHub 调用。
func (s *DeepServer) BroadcastSessionChange(changeType, sessionID string, extra map[string]any) {
	s.broadcastSessionChange(changeType, sessionID, extra)
}

// BroadcastSessionLogReset 对外暴露 session_log_reset 转发能力，供 RealtimeHub 调用。
func (s *DeepServer) BroadcastSessionLogReset(sessionID, projectPath, sessionDir string) {
	s.broadcastSessionLogReset(sessionID, projectPath, sessionDir)
}
