package websocket

import (
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/api"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
)

// SetRuntimeBuildError 保留启动或重建失败的具体原因；不影响仍然可用的旧 Runtime。
// 成功安装 Runtime 时由 SetRuntime 清除。
func (s *DeepServer) SetRuntimeBuildError(err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRuntimeBuildError = ""
	if err != nil {
		s.lastRuntimeBuildError = strings.TrimSpace(err.Error())
	}
}

func (s *DeepServer) runtimeBuildError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRuntimeBuildError
}

func (s *DeepServer) setPendingRuntimeRebuildReason(reason string) {
	if s == nil {
		return
	}
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return
	}
	s.mu.Lock()
	s.pendingRuntimeRebuildReason = trimmed
	s.mu.Unlock()
}

func (s *DeepServer) peekRuntimeRebuildReason() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.pendingRuntimeRebuildReason)
}

func (s *DeepServer) markRuntimeRebuildSuccess(reason string, rebuiltAt time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingRuntimeRebuildReason = ""
	s.lastRuntimeRebuildReason = strings.TrimSpace(reason)
	s.lastRuntimeRebuildAt = rebuiltAt
	if strings.TrimSpace(s.toolSchemaRevision) != "" {
		s.lastAppliedToolSchemaRev = strings.TrimSpace(s.toolSchemaRevision)
	}
}

func (s *DeepServer) setToolCatalogDiagnostics(schemaRevision, availabilityRevision string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolSchemaRevision = strings.TrimSpace(schemaRevision)
	s.toolAvailabilityRevision = strings.TrimSpace(availabilityRevision)
}

func (s *DeepServer) lastAppliedToolSchemaRevision() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.lastAppliedToolSchemaRev)
}

func (s *DeepServer) GetToolSchemaRevision() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.toolSchemaRevision)
}

func (s *DeepServer) GetToolAvailabilityRevision() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.toolAvailabilityRevision)
}

func (s *DeepServer) GetLastRuntimeRebuildReason() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.lastRuntimeRebuildReason)
}

func (s *DeepServer) GetLastRuntimeRebuildAt() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRuntimeRebuildAt
}

func (s *DeepServer) GetPlannerReadyReason() string {
	issues := s.GetRuntimeModeIssues()
	if len(issues) == 0 {
		return ""
	}
	issue, ok := issues["plan"]
	if !ok {
		return ""
	}
	return strings.TrimSpace(issue.Message)
}

func (s *DeepServer) GetActiveInstanceID() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.activeInstanceID)
}

func (s *DeepServer) activeConfigNameForSnapshot() string {
	if s == nil {
		return ""
	}
	runtimeName := strings.TrimSpace(s.GetRuntimeActiveConfigName())
	if runtimeName != "" {
		return runtimeName
	}
	if s.agentConfigManager == nil {
		return ""
	}
	return strings.TrimSpace(s.agentConfigManager.GetActiveConfigName())
}

func (s *DeepServer) planEnabledForSnapshot() bool {
	if s == nil || s.agentConfigManager == nil {
		return false
	}
	active := s.agentConfigManager.GetActiveConfig()
	if active == nil {
		return false
	}
	return active.Meta.PlanEnabled
}

func (s *DeepServer) runtimeModeIssuesSnapshot() map[string]middlewareclient.ModeIssue {
	issues := s.GetRuntimeModeIssues()
	if len(issues) == 0 {
		return nil
	}
	result := make(map[string]middlewareclient.ModeIssue, len(issues))
	for mode, issue := range issues {
		trimmedMode := strings.TrimSpace(mode)
		if trimmedMode == "" {
			continue
		}
		result[trimmedMode] = middlewareclient.ModeIssue{
			Code:    strings.TrimSpace(issue.Code),
			Message: strings.TrimSpace(issue.Message),
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func (s *DeepServer) runtimeModeReadinessSnapshot(planEnabled bool) map[string]middlewareclient.ModeReadiness {
	issues := s.runtimeModeIssuesSnapshot()
	result := map[string]middlewareclient.ModeReadiness{
		"build": {
			Enabled: true,
			Ready:   s != nil && s.GetAgent(executionModeBuild) != nil,
		},
		"plan": {
			Enabled: planEnabled,
			Ready:   planEnabled && s != nil && s.GetAgent(executionModePlan) != nil,
		},
	}
	if issue, ok := issues["build"]; ok {
		result["build"] = middlewareclient.ModeReadiness{
			Enabled: true,
			Ready:   false,
			Code:    issue.Code,
			Message: issue.Message,
		}
	}
	if issue, ok := issues["plan"]; ok {
		result["plan"] = middlewareclient.ModeReadiness{
			Enabled: planEnabled,
			Ready:   false,
			Code:    issue.Code,
			Message: issue.Message,
		}
	}
	return result
}

func (s *DeepServer) runtimeServicePhase(snapshot middlewareclient.RuntimeSnapshot) string {
	if snapshot.Services == nil || len(snapshot.Services) == 0 {
		return "bootstrap"
	}
	failed := false
	starting := false
	for _, service := range snapshot.Services {
		switch strings.ToLower(strings.TrimSpace(service.Status)) {
		case "failed":
			failed = true
		case "starting", "restarting", "syncing", "unknown":
			starting = true
		}
	}
	if failed {
		return "degraded"
	}
	if issues := s.GetRuntimeModeIssues(); len(issues) > 0 {
		return "degraded"
	}
	if starting {
		return "bootstrap"
	}
	return "ready"
}

var _ interface {
	GetToolSchemaRevision() string
	GetToolAvailabilityRevision() string
	GetLastRuntimeRebuildReason() string
	GetLastRuntimeRebuildAt() time.Time
	GetPlannerReadyReason() string
	GetActiveInstanceID() string
	GetRuntimeRevision() string
	GetRuntimeActiveConfigName() string
	GetRuntimeModeIssues() map[string]api.SettingsModeIssue
} = (*DeepServer)(nil)
