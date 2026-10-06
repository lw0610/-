package websocket

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
)

type sessionFailureOverride struct {
	Code   string
	Reason string
}

type toolErrorDetails struct {
	Code                string
	Retryable           *bool
	ProviderDiagnostics map[string]any
}

type boundInstanceImpactSummary struct {
	InstanceID           string
	ProjectPath          string
	AffectedSessionCount int
	RunningCount         int
	InterruptedCount     int
	AffectedSessionIDs   []string
}

func (s *DeepServer) setSessionFailureOverride(sessionID, code, reason string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionFailureOverrides == nil {
		s.sessionFailureOverrides = make(map[string]sessionFailureOverride)
	}
	s.sessionFailureOverrides[sid] = sessionFailureOverride{
		Code:   strings.TrimSpace(code),
		Reason: strings.TrimSpace(reason),
	}
}

func (s *DeepServer) consumeSessionFailureOverride(sessionID string) (sessionFailureOverride, bool) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return sessionFailureOverride{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	override, ok := s.sessionFailureOverrides[sid]
	if !ok {
		return sessionFailureOverride{}, false
	}
	delete(s.sessionFailureOverrides, sid)
	return override, true
}

func (s *DeepServer) resolveSessionRuntimeFailure(sessionID string, err error) (message, code, failureReason string, details toolErrorDetails) {
	if override, ok := s.consumeSessionFailureOverride(sessionID); ok {
		message = strings.TrimSpace(override.Reason)
		if message == "" && err != nil {
			message = strings.TrimSpace(err.Error())
		}
		code = strings.TrimSpace(override.Code)
		failureReason = strings.TrimSpace(override.Reason)
		if failureReason == "" {
			failureReason = message
		}
		return message, code, failureReason, toolErrorDetails{}
	}
	if err == nil {
		return "运行失败", "", "运行失败", toolErrorDetails{}
	}
	message = strings.TrimSpace(err.Error())
	if message == "" {
		message = "运行失败"
	}
	details = extractToolErrorDetails(err)
	return message, strings.TrimSpace(details.Code), message, details
}

func extractToolErrorDetails(err error) toolErrorDetails {
	if err == nil {
		return toolErrorDetails{}
	}
	var coded interface {
		ToolErrorCode() string
		ToolRetryable() bool
		ToolProviderDiagnostics() map[string]any
	}
	if !errors.As(err, &coded) {
		return toolErrorDetails{}
	}
	retryable := coded.ToolRetryable()
	return toolErrorDetails{
		Code:                strings.TrimSpace(coded.ToolErrorCode()),
		Retryable:           &retryable,
		ProviderDiagnostics: cloneFailureDiagnostics(coded.ToolProviderDiagnostics()),
	}
}

func cloneFailureDiagnostics(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func (s *DeepServer) handleBoundInstanceDisconnects(prev *middlewareclient.RuntimeSnapshot, next middlewareclient.RuntimeSnapshot) {
	if prev == nil {
		return
	}
	prevRunning := collectRunningInstances(*prev)
	if len(prevRunning) == 0 {
		return
	}
	nextRunning := collectRunningInstances(next)
	for instanceID, projectPath := range prevRunning {
		if _, ok := nextRunning[instanceID]; ok {
			continue
		}
		s.failSessionsBoundToDisconnectedInstance(instanceID, projectPath)
	}
}

func collectRunningInstances(snapshot middlewareclient.RuntimeSnapshot) map[string]string {
	result := make(map[string]string)
	for _, instance := range snapshot.Instances.Items {
		if instance == nil || !strings.EqualFold(strings.TrimSpace(instance.Status), "running") {
			continue
		}
		instanceID := strings.TrimSpace(instance.ID)
		if instanceID == "" {
			continue
		}
		result[instanceID] = strings.TrimSpace(instance.ProjectPath)
	}
	return result
}

func (s *DeepServer) failSessionsBoundToDisconnectedInstance(instanceID, projectPath string) {
	trimmedInstanceID := strings.TrimSpace(instanceID)
	if trimmedInstanceID == "" {
		return
	}
	states := s.listActiveSessionRuntimes()
	if summary, ok := summarizeBoundInstanceImpact(states, trimmedInstanceID, projectPath); ok {
		logger.Warn("绑定实例断开影响摘要: instance_id=%s bound_project_path=%s affected_sessions=%d running=%d interrupted=%d session_ids=%v", summary.InstanceID, summary.ProjectPath, summary.AffectedSessionCount, summary.RunningCount, summary.InterruptedCount, summary.AffectedSessionIDs)
	}
	for _, state := range states {
		if strings.TrimSpace(state.BoundInstanceID) != trimmedInstanceID {
			continue
		}
		reason := buildBoundInstanceDisconnectReason(trimmedInstanceID, state.BoundProjectPath, projectPath)
		sessionID := strings.TrimSpace(state.SessionID)
		if sessionID == "" {
			continue
		}

		switch strings.ToLower(strings.TrimSpace(state.Status)) {
		case sessionRuntimeStatusRunning:
			s.setSessionFailureOverride(sessionID, ErrorCodeBoundInstanceDisconnected, reason)
			s.cancelSessionTask(nil, sessionID)
			logger.Warn("绑定实例断开，取消运行中会话: session_id=%s instance_id=%s prior_status=%s bound_project_path=%s", sessionID, trimmedInstanceID, sessionRuntimeStatusRunning, strings.TrimSpace(state.BoundProjectPath))
		case sessionRuntimeStatusInterrupted:
			s.cancelSessionTask(nil, sessionID)
			s.sendSessionErrorWithCode(nil, sessionID, reason, ErrorCodeBoundInstanceDisconnected)
			s.markSessionRuntimeFailed(sessionID, reason, ErrorCodeBoundInstanceDisconnected, reason)
			s.persistSessionRuntimeFailure(sessionID, state.BoundProjectPath, state.BoundInstanceID, parseExecutionModeOrDefault(state.ExecutionMode), reason, ErrorCodeBoundInstanceDisconnected, reason)
			s.sendTaskRunningChanged(nil, sessionID, false)
			logger.Warn("绑定实例断开，失败结束已中断会话: session_id=%s instance_id=%s prior_status=%s bound_project_path=%s", sessionID, trimmedInstanceID, sessionRuntimeStatusInterrupted, strings.TrimSpace(state.BoundProjectPath))
		}
	}
}

func summarizeBoundInstanceImpact(states []SessionRuntimeState, instanceID, projectPath string) (boundInstanceImpactSummary, bool) {
	trimmedInstanceID := strings.TrimSpace(instanceID)
	if trimmedInstanceID == "" {
		return boundInstanceImpactSummary{}, false
	}
	summary := boundInstanceImpactSummary{
		InstanceID:  trimmedInstanceID,
		ProjectPath: strings.TrimSpace(projectPath),
	}
	for _, state := range states {
		if strings.TrimSpace(state.BoundInstanceID) != trimmedInstanceID {
			continue
		}
		sessionID := strings.TrimSpace(state.SessionID)
		if sessionID == "" {
			continue
		}
		if summary.ProjectPath == "" {
			summary.ProjectPath = strings.TrimSpace(state.BoundProjectPath)
		}
		summary.AffectedSessionIDs = append(summary.AffectedSessionIDs, sessionID)
		summary.AffectedSessionCount++
		switch strings.ToLower(strings.TrimSpace(state.Status)) {
		case sessionRuntimeStatusRunning:
			summary.RunningCount++
		case sessionRuntimeStatusInterrupted:
			summary.InterruptedCount++
		}
	}
	if summary.AffectedSessionCount == 0 {
		return boundInstanceImpactSummary{}, false
	}
	sort.Strings(summary.AffectedSessionIDs)
	return summary, true
}

func buildBoundInstanceDisconnectReason(instanceID string, paths ...string) string {
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		return fmt.Sprintf("绑定实例断开：%s（%s）", strings.TrimSpace(instanceID), trimmed)
	}
	return fmt.Sprintf("绑定实例断开：%s", strings.TrimSpace(instanceID))
}

func parseExecutionModeOrDefault(raw string) executionMode {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case string(executionModePlan):
		return executionModePlan
	default:
		return executionModeBuild
	}
}

func (s *DeepServer) persistSessionRuntimeFailure(sessionID, projectPath, instanceID string, mode executionMode, lastError, failureCode, failureReason string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	state, _ := s.activeSessionRuntime(sid)
	boundProjectPath := strings.TrimSpace(projectPath)
	boundInstanceID := strings.TrimSpace(instanceID)
	if boundInstanceID == "" {
		boundInstanceID = strings.TrimSpace(state.BoundInstanceID)
	}
	if strings.TrimSpace(failureReason) == "" {
		failureReason = strings.TrimSpace(lastError)
	}

	projection := s.sessionRuntimeProjectionSnapshot(sid, nil)
	if strings.TrimSpace(projection.RuntimeState.SessionID) == "" {
		projection.RuntimeState.SessionID = sid
	}
	projection.RuntimeState.Status = sessionRuntimeStatusFailed
	projection.RuntimeState.LastError = strings.TrimSpace(lastError)
	projection.RuntimeState.FailureCode = strings.TrimSpace(failureCode)
	projection.RuntimeState.FailureReason = strings.TrimSpace(failureReason)
	projection.RuntimeState.BoundInstanceID = boundInstanceID
	projection.RuntimeState.BoundProjectPath = boundProjectPath
	projection.RuntimeState.ExecutionMode = strings.TrimSpace(string(mode))
	projection.RuntimeState.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(projection.RuntimeSummary) == "" {
		projection.RuntimeSummary = strings.TrimSpace(failureReason)
	}
	s.mu.Lock()
	if s.sessionRuntimeProjections == nil {
		s.sessionRuntimeProjections = make(map[string]*sessionRuntimeProjection)
	}
	cloned := cloneSessionRuntimeProjection(&projection)
	s.sessionRuntimeProjections[sid] = &cloned
	s.mu.Unlock()

	if strings.TrimSpace(boundProjectPath) == "" {
		return
	}

	_, err := s.SaveSessionAtomically(sid, boundProjectPath, func(sessionData map[string]any) error {
		if boundProjectPath != "" {
			sessionData["project_path"] = boundProjectPath
		}
		if strings.TrimSpace(string(mode)) != "" {
			sessionData["execution_mode"] = strings.TrimSpace(string(mode))
			sessionData["run_intent"] = map[executionMode]string{
				executionModePlan:  "plan_only",
				executionModeBuild: "execute",
			}[mode]
		}
		applyProjectionToSessionData(sessionData, projection)
		sessionData["updated_at"] = time.Now().Format(time.RFC3339)
		return nil
	})
	if err != nil {
		logger.Warn("持久化会话失败态失败: session_id=%s err=%v", sid, err)
	}
}
