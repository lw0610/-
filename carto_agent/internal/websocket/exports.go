package websocket

import (
	"context"
	"fmt"
)

// SaveSession 导出的会话保存方法（供 REST API 调用）
func (s *DeepServer) SaveSession(sessionData map[string]any, projectPath string) error {
	return s.saveSessionToFile(sessionData, projectPath, sessionSaveOperation{})
}

// SaveSessionWithOperation 导出的会话保存方法（包含可选操作）
func (s *DeepServer) SaveSessionWithOperation(sessionData map[string]any, projectPath string, operation map[string]any) error {
	return s.saveSessionToFile(sessionData, projectPath, parseSessionSaveOperation(operation))
}

// LoadSession 导出的会话加载方法
func (s *DeepServer) LoadSession(sessionID, projectPath string) (map[string]any, error) {
	data, err := s.loadSessionFromFile(sessionID, projectPath)
	if err != nil {
		return nil, err
	}
	flow, flowErr := s.flowSnapshot(sessionID, projectPath)
	if flowErr != nil {
		return nil, flowErr
	}
	plans, planErr := s.ListPlans(context.Background(), sessionID, projectPath)
	if planErr != nil {
		return nil, planErr
	}
	data["carto_plans"] = plans
	data["flow_run"] = flow
	if flow != nil {
		data["flow_run_event_seq"] = flow.EventSeq
	}
	return data, nil
}

// ListSessions 导出的会话列表方法
func (s *DeepServer) ListSessions(projectPath string) ([]map[string]any, error) {
	return s.listSessions(projectPath)
}

// ListProjects 导出的项目索引列表方法
func (s *DeepServer) ListProjects() ([]map[string]any, error) {
	return s.listProjects()
}

// DeleteSession 导出的会话删除方法
func (s *DeepServer) DeleteSession(sessionID, projectPath string) error {
	return s.deleteSessionFile(sessionID, projectPath)
}

// NotifySessionChange 导出会话变更通知方法（供 REST API 调用）
func (s *DeepServer) NotifySessionChange(changeType, sessionID string, extra map[string]any) {
	s.realtimeService().BroadcastSessionChange(changeType, sessionID, extra)
}

// InitSessionDir 导出的会话目录初始化方法
func (s *DeepServer) InitSessionDir(projectPath, sessionID string) (map[string]string, error) {
	return s.initSessionDir(projectPath, sessionID)
}

// GetSessionDir 导出的会话目录获取方法
func (s *DeepServer) GetSessionDir(projectPath, sessionID string) (string, error) {
	return s.getSessionDir(projectPath, sessionID)
}

// SaveSessionAtomically 原子化读取-更新-保存会话，避免并发覆盖。
func (s *DeepServer) SaveSessionAtomically(sessionID, projectPath string, updater func(map[string]any) error) (map[string]any, error) {
	normalizedSessionID, err := validateSessionID(sessionID)
	if err != nil {
		return nil, err
	}
	normalizedProjectPath, err := normalizeProjectPath(projectPath)
	if err != nil {
		return nil, err
	}

	lock := s.getOrCreateSessionFileLock(sessionLockKey(normalizedProjectPath, normalizedSessionID))
	lock.Lock()
	defer lock.Unlock()

	sessionData, err := s.loadSessionFromFileUnlocked(normalizedSessionID, normalizedProjectPath)
	if err != nil {
		return nil, err
	}

	if updater != nil {
		if err := updater(sessionData); err != nil {
			return nil, err
		}
	}

	sessionData["session_id"] = normalizedSessionID
	if normalizedProjectPath == "" {
		return nil, fmt.Errorf("project_path 不能为空")
	}
	sessionData["project_path"] = normalizedProjectPath

	if err := s.saveSessionToFileUnlocked(sessionData, normalizedProjectPath, sessionSaveOperation{}); err != nil {
		return nil, err
	}
	return sessionData, nil
}

// ListSkills 导出的技能列表方法
func (s *DeepServer) ListSkills() ([]map[string]any, error) {
	return s.listSkills()
}

// GetSkillContent 导出的技能内容获取方法
func (s *DeepServer) GetSkillContent(name string) (map[string]any, error) {
	return s.getSkillContent(name)
}

// SaveSkill 导出的技能保存方法
func (s *DeepServer) SaveSkill(name, description, content string) error {
	return s.saveSkill(name, description, content)
}

// DeleteSkill 导出的技能删除方法
func (s *DeepServer) DeleteSkill(name string) error {
	return s.deleteSkill(name)
}

// ImportSkill 导出的技能导入方法
func (s *DeepServer) ImportSkill(sourcePath string, overwrite bool) (map[string]any, error) {
	return s.importSkill(sourcePath, overwrite)
}
