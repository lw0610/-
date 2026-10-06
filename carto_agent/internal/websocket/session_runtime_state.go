package websocket

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/logger"
)

const (
	sessionRuntimeStatusRunning     = "running"
	sessionRuntimeStatusInterrupted = "interrupted"
	sessionRuntimeStatusCompleted   = "completed"
	sessionRuntimeStatusFailed      = "failed"
	sessionRuntimeStatusCancelled   = "cancelled"
)

type sessionBindingResolution struct {
	InstanceID  string
	ProjectPath string
	Reason      string
}

const (
	bindingReasonExplicitInstance = "explicit_instance"
	bindingReasonProjectMatch     = "project_match"
	bindingReasonActiveFallback   = "active_fallback"
	bindingReasonFirstRunning     = "first_running"
	bindingReasonUnavailable      = "unavailable"
)

// SessionRuntimeState 表示会话级运行态摘要。
type SessionRuntimeState struct {
	SessionID         string `json:"session_id"`
	RunID             string `json:"run_id,omitempty"`
	Status            string `json:"status"`
	ExecutionMode     string `json:"execution_mode,omitempty"`
	Phase             string `json:"phase,omitempty"`
	BoundInstanceID   string `json:"bound_instance_id,omitempty"`
	BoundProjectPath  string `json:"bound_project_path,omitempty"`
	CheckpointID      string `json:"checkpoint_id,omitempty"`
	RuntimeRevision   string `json:"runtime_revision,omitempty"`
	ActiveAgentConfig string `json:"active_agent_config,omitempty"`
	LastError         string `json:"last_error,omitempty"`
	FailureCode       string `json:"failure_code,omitempty"`
	FailureReason     string `json:"failure_reason,omitempty"`
	UpdatedAt         string `json:"updated_at,omitempty"`
}

func (s *DeepServer) activeSessionRuntime(sessionID string) (SessionRuntimeState, bool) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return SessionRuntimeState{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.activeSessionRuns[sid]
	return state, ok
}

func (s *DeepServer) listActiveSessionRuntimes() []SessionRuntimeState {
	s.mu.Lock()
	defer s.mu.Unlock()
	states := make([]SessionRuntimeState, 0, len(s.activeSessionRuns))
	for _, state := range s.activeSessionRuns {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].SessionID < states[j].SessionID
	})
	return states
}

// ListActiveRuns 返回当前活跃运行摘要，供控制层守卫使用。
func (s *DeepServer) ListActiveRuns() []control.ActiveRunInfo {
	states := s.listActiveSessionRuntimes()
	result := make([]control.ActiveRunInfo, 0, len(states))
	for _, state := range states {
		result = append(result, control.ActiveRunInfo{
			SessionID:        strings.TrimSpace(state.SessionID),
			Status:           strings.TrimSpace(state.Status),
			BoundInstanceID:  strings.TrimSpace(state.BoundInstanceID),
			BoundProjectPath: strings.TrimSpace(state.BoundProjectPath),
		})
	}
	return result
}

func (s *DeepServer) hasActiveSessionRuntime(sessionID string) bool {
	state, ok := s.activeSessionRuntime(sessionID)
	if !ok {
		return false
	}
	return state.Status == sessionRuntimeStatusRunning || state.Status == sessionRuntimeStatusInterrupted
}

func (s *DeepServer) markSessionRuntimeRunning(sessionID, runID string, mode executionMode, phase, projectPath, instanceID string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	state := SessionRuntimeState{
		SessionID:         sid,
		RunID:             strings.TrimSpace(runID),
		Status:            sessionRuntimeStatusRunning,
		ExecutionMode:     strings.TrimSpace(string(mode)),
		Phase:             strings.TrimSpace(phase),
		BoundInstanceID:   strings.TrimSpace(instanceID),
		BoundProjectPath:  strings.TrimSpace(projectPath),
		CheckpointID:      strings.TrimSpace(runID),
		RuntimeRevision:   strings.TrimSpace(s.GetRuntimeRevision()),
		ActiveAgentConfig: strings.TrimSpace(s.GetRuntimeActiveConfigName()),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	s.mu.Lock()
	s.activeSessionRuns[sid] = state
	delete(s.sessionFailureOverrides, sid)
	s.mu.Unlock()
	s.updateSessionRuntimeProjectionState(state)
	s.broadcastSessionRuntimeState(state)
}

func (s *DeepServer) markSessionRuntimeInterrupted(sessionID, checkpointID string) {
	s.updateSessionRuntimeState(sessionID, func(state *SessionRuntimeState) {
		state.Status = sessionRuntimeStatusInterrupted
		state.FailureCode = ""
		state.FailureReason = ""
		if strings.TrimSpace(checkpointID) != "" {
			state.CheckpointID = strings.TrimSpace(checkpointID)
		}
	})
}

func (s *DeepServer) markSessionRuntimeResumed(sessionID string) {
	s.updateSessionRuntimeState(sessionID, func(state *SessionRuntimeState) {
		state.Status = sessionRuntimeStatusRunning
		state.FailureCode = ""
		state.FailureReason = ""
	})
}

func (s *DeepServer) markSessionRuntimeFinished(sessionID, status, lastError string) {
	s.markSessionRuntimeFinishedDetailed(sessionID, status, lastError, "", "")
}

// Finish cancellation for both a running runner and an already-paused runner.
// Only the executable checkpoint is removed; extracted data and drafts remain.
// The caller holds knowledgeConfirmMu to serialize pause/confirm/cancel transitions.
func (s *DeepServer) markSessionRuntimeCancelled(sessionID, runID string) {
	state, active := s.activeSessionRuntime(sessionID)
	if !active || state.RunID != runID {
		return // An old runner must not finish a newer run in the same session.
	}
	s.markSessionRuntimeFinished(sessionID, sessionRuntimeStatusCancelled, "")
	s.persistSessionRuntimeProjection(sessionID)
	if state.CheckpointID != "" && s.checkpointStore != nil {
		if err := s.checkpointStore.Delete(context.Background(), state.CheckpointID); err != nil {
			logger.Warn("删除已取消任务 checkpoint 失败: checkpoint=%s err=%v", state.CheckpointID, err)
		}
	}
}

func (s *DeepServer) markSessionRuntimeFailed(sessionID, lastError, failureCode, failureReason string) {
	s.markSessionRuntimeFinishedDetailed(sessionID, sessionRuntimeStatusFailed, lastError, failureCode, failureReason)
}

func (s *DeepServer) markSessionRuntimeFinishedDetailed(sessionID, status, lastError, failureCode, failureReason string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	state, ok := s.activeSessionRuntime(sid)
	if !ok {
		state = s.sessionRuntimeProjectionSnapshot(sid, nil).RuntimeState
		state.SessionID = sid
	}
	state.Status = strings.TrimSpace(status)
	state.LastError = strings.TrimSpace(lastError)
	if state.Status == sessionRuntimeStatusFailed {
		state.FailureCode = strings.TrimSpace(failureCode)
		if strings.TrimSpace(failureReason) != "" {
			state.FailureReason = strings.TrimSpace(failureReason)
		} else {
			state.FailureReason = strings.TrimSpace(lastError)
		}
	} else {
		state.FailureCode = ""
		state.FailureReason = ""
	}
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.updateSessionRuntimeProjectionState(state)
	s.broadcastSessionRuntimeState(state)
	s.mu.Lock()
	delete(s.activeSessionRuns, sid)
	delete(s.sessionFailureOverrides, sid)
	delete(s.sessionPendingInterrupts, sid)
	delete(s.sessionTaskCancels, sid)
	delete(s.sessionCurrentAgent, sid)
	for conn, connPending := range s.pendingInterrupts {
		delete(connPending, sid)
		if len(connPending) == 0 {
			delete(s.pendingInterrupts, conn)
		}
	}
	for conn, connCancels := range s.taskCancels {
		delete(connCancels, sid)
		if len(connCancels) == 0 {
			delete(s.taskCancels, conn)
		}
	}
	for conn, connAgents := range s.currentAgent {
		delete(connAgents, sid)
		if len(connAgents) == 0 {
			delete(s.currentAgent, conn)
		}
	}
	s.mu.Unlock()
}

func (s *DeepServer) updateSessionRuntimeState(sessionID string, apply func(*SessionRuntimeState)) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || apply == nil {
		return
	}
	runtimeRevision := strings.TrimSpace(s.GetRuntimeRevision())
	activeAgentConfig := strings.TrimSpace(s.GetRuntimeActiveConfigName())
	s.mu.Lock()
	state, ok := s.activeSessionRuns[sid]
	if !ok {
		s.mu.Unlock()
		return
	}
	apply(&state)
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	state.RuntimeRevision = runtimeRevision
	state.ActiveAgentConfig = activeAgentConfig
	s.activeSessionRuns[sid] = state
	s.mu.Unlock()
	s.updateSessionRuntimeProjectionState(state)
	s.broadcastSessionRuntimeState(state)
}

func (s *DeepServer) sendCurrentSessionRuntimeStates(conn *websocket.Conn) {
	if conn == nil {
		return
	}
	states := s.listActiveSessionRuntimes()
	logger.Debug("发送当前会话运行态快照: count=%d", len(states))
	for _, state := range states {
		s.sendSessionRuntimeState(conn, state)
	}
}

func (s *DeepServer) sendSessionRuntimeState(conn *websocket.Conn, state SessionRuntimeState) {
	if conn == nil {
		return
	}
	if err := s.sendMessage(conn, Message{
		Type:      MsgTypeSessionRuntimeState,
		SessionID: strings.TrimSpace(state.SessionID),
		Data: map[string]interface{}{
			"session_id":          strings.TrimSpace(state.SessionID),
			"run_id":              strings.TrimSpace(state.RunID),
			"status":              strings.TrimSpace(state.Status),
			"execution_mode":      strings.TrimSpace(state.ExecutionMode),
			"phase":               strings.TrimSpace(state.Phase),
			"bound_instance_id":   strings.TrimSpace(state.BoundInstanceID),
			"bound_project_path":  strings.TrimSpace(state.BoundProjectPath),
			"checkpoint_id":       strings.TrimSpace(state.CheckpointID),
			"runtime_revision":    strings.TrimSpace(state.RuntimeRevision),
			"active_agent_config": strings.TrimSpace(state.ActiveAgentConfig),
			"last_error":          strings.TrimSpace(state.LastError),
			"failure_code":        strings.TrimSpace(state.FailureCode),
			"failure_reason":      strings.TrimSpace(state.FailureReason),
			"updated_at":          strings.TrimSpace(state.UpdatedAt),
		},
	}); err != nil {
		logger.Debug("发送 session_runtime_state 失败: session_id=%s err=%v", state.SessionID, err)
	}
}

func (s *DeepServer) broadcastSessionRuntimeState(state SessionRuntimeState) {
	for _, conn := range s.listClientConnections() {
		s.sendSessionRuntimeState(conn, state)
	}
}

func (s *DeepServer) getPendingInterrupt(sessionID string) (*PendingInterrupt, bool) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.sessionPendingInterrupts[sid]
	return pending, ok
}

func (s *DeepServer) sendPendingInterruptSnapshot(conn *websocket.Conn, pending *PendingInterrupt) {
	if conn == nil || pending == nil {
		return
	}
	contexts := make([]map[string]interface{}, 0, len(pending.Contexts))
	for _, ic := range pending.Contexts {
		ictx := map[string]interface{}{
			"id":            ic.ID,
			"info":          ic.Info,
			"is_root_cause": ic.IsRootCause,
		}
		addrParts := make([]string, 0, len(ic.Address))
		for _, seg := range ic.Address {
			addrParts = append(addrParts, strings.TrimSpace(strings.Join([]string{string(seg.Type), seg.ID}, ":")))
		}
		ictx["address"] = strings.Join(addrParts, " → ")
		contexts = append(contexts, ictx)
	}
	_ = s.sendMessage(conn, Message{
		Type:      MsgTypeInterruptRequest,
		SessionID: strings.TrimSpace(pending.SessionID),
		Content:   "任务需要您的确认才能继续",
		Data: withModePhaseData(map[string]interface{}{
			"checkpoint_id":      strings.TrimSpace(pending.CheckpointID),
			"interrupt_contexts": contexts,
		}, pending.ModePhase),
	})
}

func (s *DeepServer) resolveSessionBoundInstanceID(projectPath string) string {
	binding, err := s.resolveSessionBinding("", projectPath)
	if err != nil {
		return ""
	}
	return binding.InstanceID
}

func (s *DeepServer) resolveSessionBinding(explicitInstanceID, projectPath string) (sessionBindingResolution, error) {
	requestedInstanceID := strings.TrimSpace(explicitInstanceID)
	requestedProjectPath := strings.TrimSpace(projectPath)
	snapshot := s.GetRuntimeSnapshot()

	if requestedInstanceID != "" {
		instance, ok := findRunningInstanceByID(snapshot.Instances.Items, requestedInstanceID)
		if !ok {
			logger.Warn("会话绑定实例解析失败: explicit_instance_id=%s requested_project_path=%s reason=%s", requestedInstanceID, requestedProjectPath, bindingReasonUnavailable)
			return sessionBindingResolution{}, fmt.Errorf("指定实例不可用或未在运行：%s", requestedInstanceID)
		}
		binding := sessionBindingResolution{
			InstanceID:  strings.TrimSpace(instance.ID),
			ProjectPath: resolveBoundProjectPath(strings.TrimSpace(instance.ProjectPath), requestedProjectPath),
			Reason:      bindingReasonExplicitInstance,
		}
		logger.Debug("会话绑定实例解析完成: explicit_instance_id=%s requested_project_path=%s bound_instance_id=%s bound_project_path=%s reason=%s", requestedInstanceID, requestedProjectPath, binding.InstanceID, binding.ProjectPath, binding.Reason)
		return binding, nil
	}

	if requestedProjectPath != "" {
		if instance, ok := findPreferredRunningInstanceByProject(snapshot.Instances.Items, snapshot.Instances.ActiveID, requestedProjectPath); ok {
			binding := sessionBindingResolution{
				InstanceID:  strings.TrimSpace(instance.ID),
				ProjectPath: resolveBoundProjectPath(strings.TrimSpace(instance.ProjectPath), requestedProjectPath),
				Reason:      bindingReasonProjectMatch,
			}
			logger.Debug("会话绑定实例解析完成: explicit_instance_id=%s requested_project_path=%s bound_instance_id=%s bound_project_path=%s reason=%s", requestedInstanceID, requestedProjectPath, binding.InstanceID, binding.ProjectPath, binding.Reason)
			return binding, nil
		}
	}

	if activeID := strings.TrimSpace(snapshot.Instances.ActiveID); activeID != "" {
		if instance, ok := findRunningInstanceByID(snapshot.Instances.Items, activeID); ok {
			binding := sessionBindingResolution{
				InstanceID:  activeID,
				ProjectPath: resolveBoundProjectPath(strings.TrimSpace(instance.ProjectPath), requestedProjectPath),
				Reason:      bindingReasonActiveFallback,
			}
			logger.Debug("会话绑定实例解析完成: explicit_instance_id=%s requested_project_path=%s bound_instance_id=%s bound_project_path=%s reason=%s", requestedInstanceID, requestedProjectPath, binding.InstanceID, binding.ProjectPath, binding.Reason)
			return binding, nil
		}
	}

	if instance, ok := findFirstRunningInstance(snapshot.Instances.Items); ok {
		binding := sessionBindingResolution{
			InstanceID:  strings.TrimSpace(instance.ID),
			ProjectPath: resolveBoundProjectPath(strings.TrimSpace(instance.ProjectPath), requestedProjectPath),
			Reason:      bindingReasonFirstRunning,
		}
		logger.Debug("会话绑定实例解析完成: explicit_instance_id=%s requested_project_path=%s bound_instance_id=%s bound_project_path=%s reason=%s", requestedInstanceID, requestedProjectPath, binding.InstanceID, binding.ProjectPath, binding.Reason)
		return binding, nil
	}

	logger.Warn("会话绑定实例解析失败: explicit_instance_id=%s requested_project_path=%s reason=%s", requestedInstanceID, requestedProjectPath, bindingReasonUnavailable)
	return sessionBindingResolution{}, fmt.Errorf("当前没有可用的 QGIS 实例，无法启动运行")
}

func findRunningInstanceByID(items []*discovery.QGISInstance, instanceID string) (*discovery.QGISInstance, bool) {
	target := strings.TrimSpace(instanceID)
	if target == "" {
		return nil, false
	}
	for _, instance := range items {
		if instance == nil || !strings.EqualFold(strings.TrimSpace(instance.Status), "running") {
			continue
		}
		if strings.TrimSpace(instance.ID) == target {
			return instance, true
		}
	}
	return nil, false
}

func findPreferredRunningInstanceByProject(items []*discovery.QGISInstance, activeID, projectPath string) (*discovery.QGISInstance, bool) {
	normalizedProjectPath := strings.TrimSpace(projectPath)
	if normalizedProjectPath == "" {
		return nil, false
	}
	trimmedActiveID := strings.TrimSpace(activeID)
	var fallback *discovery.QGISInstance
	for _, instance := range items {
		if instance == nil || !strings.EqualFold(strings.TrimSpace(instance.Status), "running") {
			continue
		}
		if strings.TrimSpace(instance.ProjectPath) != normalizedProjectPath {
			continue
		}
		if strings.TrimSpace(instance.ID) == trimmedActiveID {
			return instance, true
		}
		if fallback == nil && strings.TrimSpace(instance.ID) != "" {
			fallback = instance
		}
	}
	if fallback == nil {
		return nil, false
	}
	return fallback, true
}

func findFirstRunningInstance(items []*discovery.QGISInstance) (*discovery.QGISInstance, bool) {
	for _, instance := range items {
		if instance == nil || !strings.EqualFold(strings.TrimSpace(instance.Status), "running") {
			continue
		}
		if strings.TrimSpace(instance.ID) == "" {
			continue
		}
		return instance, true
	}
	return nil, false
}

func resolveBoundProjectPath(instanceProjectPath, requestedProjectPath string) string {
	if strings.TrimSpace(instanceProjectPath) != "" {
		return strings.TrimSpace(instanceProjectPath)
	}
	return strings.TrimSpace(requestedProjectPath)
}
