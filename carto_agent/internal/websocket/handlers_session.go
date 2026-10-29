package websocket

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/logger"
)

var sessionSubdirs = []string{"logs", "intermediate", "output", "temp", "snapshots", "exports"}

const (
	sessionSaveOpClearHistory    = "clear_history"
	sessionSaveOpCreateSnapshot  = "create_snapshot"
	sessionSaveOpRestoreSnapshot = "restore_snapshot"
	sessionRestoreModeStrict     = "strict"
	snapshotManifestVersion      = 1
	sessionMaintenanceInterval   = 10 * time.Minute
	sessionFileLockTTL           = 30 * time.Minute
	sessionTempCleanupMaxAge     = 7 * 24 * time.Hour
	projectRegistryFileName      = "projects.yaml"
)

type sessionSaveOperation struct {
	Type       string
	SnapshotID string
	Mode       string
	Reason     string
}

type sessionSnapshotMeta struct {
	ID        string
	CreatedAt string
}

type sessionRuntimePolicy struct {
	MaxSnapshotsPerSession int
	AutoArchiveEnabled     bool
	AutoArchiveDays        int
	AutoCleanupEnabled     bool
	AutoCleanupDays        int
	IdleTimeoutSeconds     int
	MaxDurationSeconds     int
	MaxActiveSessions      int
	MaxMessagesPerSession  int
	AutoSaveInterval       int
	CrashRecoveryEnabled   bool
}

type sessionFileLock struct {
	sync.Mutex
	lastUsed time.Time
}

type projectRegistryEntry struct {
	Path         string `yaml:"path"`
	LastAccessed string `yaml:"last_accessed"`
}

type projectRegistryPayload struct {
	Projects []projectRegistryEntry `yaml:"projects"`
}

func (s *DeepServer) handleSessionMessage(conn *websocket.Conn, msg Message) bool {
	requestID, _ := msg.Data["request_id"].(string)

	switch msg.Type {
	case MsgTypeSessionSave:
		// 保存会话到本地文件
		sessionData, ok := msg.Data["session"].(map[string]any)
		if !ok {
			s.sendSessionResponse(conn, requestID, "save", false, nil, fmt.Errorf("无效的会话数据格式"), newWebSocketRequestEnvelope(
				"INVALID_SESSION_REQUEST",
				"无效的会话数据格式",
				"websocket",
				"session_save",
				"请检查 session 请求体后重试。",
				map[string]any{"action": "save"},
			))
			return true
		}

		sessionID, _ := sessionData["session_id"].(string)
		projectPath, _ := msg.Data["project_path"].(string)
		operation := parseSessionSaveOperation(msg.Data["operation"])
		err := s.saveSessionToFile(sessionData, projectPath, operation)
		if err != nil {
			s.sendSessionResponse(conn, requestID, "save", false, map[string]any{
				"session_id":   sessionID,
				"project_path": projectPath,
			}, err, newWebSocketRequestEnvelope(
				"SESSION_SAVE_FAILED",
				"会话保存失败",
				"websocket",
				"session_save",
				"请检查会话参数或磁盘状态后重试。",
				map[string]any{"action": "save"},
			))
		} else {
			s.sendSessionResponse(conn, requestID, "save", true, map[string]any{
				"session_id":   sessionID,
				"project_path": projectPath,
			}, nil, nil)
			s.realtimeService().BroadcastSessionChange("updated", strings.TrimSpace(sessionID), map[string]any{
				"project_path": strings.TrimSpace(projectPath),
			})
		}
		return true

	case MsgTypeSessionLoad:
		// 加载会话
		sessionID, _ := msg.Data["session_id"].(string)
		projectPath, _ := msg.Data["project_path"].(string)

		sessionData, err := s.loadSessionFromFile(sessionID, projectPath)
		if err != nil {
			s.sendSessionResponse(conn, requestID, "load", false, map[string]any{
				"session_id":   sessionID,
				"project_path": projectPath,
			}, err, newWebSocketRequestEnvelope(
				"SESSION_LOAD_FAILED",
				"会话加载失败",
				"websocket",
				"session_load",
				"请检查会话参数或会话文件后重试。",
				map[string]any{"action": "load"},
			))
		} else {
			s.restoreManagedSessionFromSessionData(sessionData)
			s.sendSessionResponse(conn, requestID, "load", true, map[string]any{
				"session":      sessionData,
				"session_id":   sessionID,
				"project_path": projectPath,
			}, nil, nil)
		}
		return true

	case MsgTypeSessionList:
		// 获取会话列表
		projectPath, _ := msg.Data["project_path"].(string)
		sessions, err := s.listSessions(projectPath)
		if err != nil {
			s.sendSessionResponse(conn, requestID, "list", false, map[string]any{
				"project_path": projectPath,
			}, err, newWebSocketRequestEnvelope(
				"SESSION_LIST_FAILED",
				"会话列表读取失败",
				"websocket",
				"session_list",
				"请检查 project_path 或会话目录后重试。",
				map[string]any{"action": "list"},
			))
		} else {
			s.sendSessionResponse(conn, requestID, "list", true, map[string]any{
				"sessions":     sessions,
				"project_path": projectPath,
			}, nil, nil)
		}
		return true

	case MsgTypeSessionDelete:
		// 删除会话
		sessionID, _ := msg.Data["session_id"].(string)
		projectPath, _ := msg.Data["project_path"].(string)

		err := s.deleteSessionFile(sessionID, projectPath)
		if err != nil {
			s.sendSessionResponse(conn, requestID, "delete", false, map[string]any{
				"session_id":   sessionID,
				"project_path": projectPath,
			}, err, newWebSocketRequestEnvelope(
				"SESSION_DELETE_FAILED",
				"会话删除失败",
				"websocket",
				"session_delete",
				"请检查会话参数或会话文件后重试。",
				map[string]any{"action": "delete"},
			))
		} else {
			s.sendSessionResponse(conn, requestID, "delete", true, map[string]any{
				"session_id":   sessionID,
				"project_path": projectPath,
			}, nil, nil)
			s.realtimeService().BroadcastSessionChange("deleted", strings.TrimSpace(sessionID), nil)
		}
		return true

	case MsgTypeSessionDirInit:
		// 初始化会话目录
		sessionID, _ := msg.Data["session_id"].(string)
		projectPath, _ := msg.Data["project_path"].(string)

		dirs, err := s.initSessionDir(projectPath, sessionID)
		if err != nil {
			s.sendSessionResponse(conn, requestID, "dir_init", false, map[string]any{
				"session_id":   sessionID,
				"project_path": projectPath,
			}, err, newWebSocketRequestEnvelope(
				"INVALID_SESSION_REQUEST",
				"会话目录初始化失败",
				"websocket",
				"session_dir_init",
				"请检查 session_id 与 project_path 后重试。",
				map[string]any{"action": "dir_init"},
			))
		} else {
			// 将 map[string]string 转为 map[string]any
			dirsAny := make(map[string]any, len(dirs))
			for k, v := range dirs {
				dirsAny[k] = v
			}
			s.sendSessionResponse(conn, requestID, "dir_init", true, map[string]any{
				"session_id":   sessionID,
				"project_path": projectPath,
				"dirs":         dirsAny,
			}, nil, nil)
		}
		return true

	case MsgTypeSessionDirInfo:
		// 获取会话目录信息
		sessionID, _ := msg.Data["session_id"].(string)
		projectPath, _ := msg.Data["project_path"].(string)

		sessionDir, err := s.getSessionDir(projectPath, sessionID)
		if err != nil {
			s.sendSessionResponse(conn, requestID, "dir_info", false, map[string]any{
				"session_id":   sessionID,
				"project_path": projectPath,
			}, err, newWebSocketRequestEnvelope(
				"INVALID_SESSION_REQUEST",
				"会话目录请求无效",
				"websocket",
				"session_dir_info",
				"请检查 session_id 与 project_path 后重试。",
				map[string]any{"action": "dir_info"},
			))
		} else {
			dirsAny := map[string]any{"root": sessionDir}
			for _, sub := range sessionSubdirs {
				dirsAny[sub] = filepath.Join(sessionDir, sub)
			}
			s.sendSessionResponse(conn, requestID, "dir_info", true, map[string]any{
				"session_id":   sessionID,
				"project_path": projectPath,
				"dirs":         dirsAny,
			}, nil, nil)
		}
		return true

	case MsgTypeSessionAttach:
		sessionID, _ := msg.Data["session_id"].(string)
		if strings.TrimSpace(sessionID) == "" {
			sessionID = msg.SessionID
		}
		if _, err := resolveMessageProtocolVersion(msg); err != nil {
			s.sendSessionResponse(conn, requestID, "attach", false, map[string]any{
				"session_id":       strings.TrimSpace(sessionID),
				"protocol_version": websocketProtocolVersion,
			}, err, newWebSocketRequestEnvelope(
				ErrorCodeUnsupportedProtocolVersion,
				"WebSocket 协议版本不受支持。",
				"websocket",
				"session_attach",
				"请升级客户端后重试。",
				map[string]any{"action": "attach", "session_id": strings.TrimSpace(sessionID)},
			))
			return true
		}
		lastSeenSeq, err := parseSessionAttachCursor(msg.Data)
		if err != nil {
			s.sendSessionResponse(conn, requestID, "attach", false, map[string]any{
				"session_id":       strings.TrimSpace(sessionID),
				"protocol_version": websocketProtocolVersion,
			}, err, newWebSocketRequestEnvelope(
				ErrorCodeInvalidCursor,
				"会话回放游标无效。",
				"websocket",
				"session_attach",
				"请清空游标或使用有效的 last_seen_seq 后重试。",
				map[string]any{"action": "attach", "session_id": strings.TrimSpace(sessionID)},
			))
			return true
		}
		s.syncSessionAttachment(conn, requestID, sessionID, lastSeenSeq)
		return true

	case MsgTypeSessionDetach:
		sessionID, _ := msg.Data["session_id"].(string)
		if strings.TrimSpace(sessionID) == "" {
			sessionID = msg.SessionID
		}
		s.detachSessionSubscription(conn, sessionID)
		return true
	}

	return false
}

func parseSessionAttachCursor(data map[string]any) (uint64, error) {
	if data == nil {
		return 0, nil
	}
	raw, exists := data["last_seen_seq"]
	if !exists {
		return 0, nil
	}
	seq, ok := parseStrictAttachCursor(raw)
	if !ok {
		return 0, fmt.Errorf("invalid last_seen_seq")
	}
	return seq, nil
}

func parseStrictAttachCursor(raw any) (uint64, bool) {
	switch value := raw.(type) {
	case nil:
		return 0, false
	case uint64:
		return value, true
	case uint32:
		return uint64(value), true
	case uint:
		return uint64(value), true
	case int64:
		if value < 0 {
			return 0, false
		}
		return uint64(value), true
	case int:
		if value < 0 {
			return 0, false
		}
		return uint64(value), true
	case float64:
		if value < 0 || math.Trunc(value) != value {
			return 0, false
		}
		return uint64(value), true
	case float32:
		if value < 0 || float32(uint64(value)) != value {
			return 0, false
		}
		return uint64(value), true
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, false
		}
		return parseStrictUint64(trimmed)
	default:
		return 0, false
	}
}

func (s *DeepServer) sendSessionResponse(conn *websocket.Conn, requestID, action string, success bool, data map[string]any, err error, fallback *control.ErrorEnvelope) {
	respData := map[string]any{
		"request_id": requestID,
		"action":     action,
		"success":    success,
	}
	if data != nil {
		for k, v := range data {
			respData[k] = v
		}
	}
	applyWebSocketErrorPayload(respData, err, fallback, map[string]any{
		"message_type": MsgTypeSessionResponse,
		"request_id":   strings.TrimSpace(requestID),
		"action":       strings.TrimSpace(action),
	})
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	logger.Debug(
		"发送会话响应: request_id=%s, action=%s, success=%v, error=%s",
		requestID, action, success, errMsg,
	)
	s.sendMessage(conn, Message{
		Type: MsgTypeSessionResponse,
		Data: respData,
	})
}

func parseSessionSaveOperation(raw any) sessionSaveOperation {
	op := sessionSaveOperation{}
	data, ok := raw.(map[string]any)
	if !ok {
		return op
	}

	opType, _ := data["type"].(string)
	op.Type = strings.ToLower(strings.TrimSpace(opType))

	snapshotID, _ := data["snapshot_id"].(string)
	op.SnapshotID = strings.TrimSpace(snapshotID)

	mode, _ := data["mode"].(string)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = sessionRestoreModeStrict
	}
	op.Mode = mode

	reason, _ := data["reason"].(string)
	op.Reason = strings.TrimSpace(reason)
	return op
}

func validateSessionID(raw string) (string, error) {
	return config.ValidateSessionID(raw)
}

func normalizeProjectPath(raw string) (string, error) {
	return config.NormalizeSessionProjectPath(raw)
}

func sessionLockKey(projectPath, sessionID string) string {
	return fmt.Sprintf("%s::%s", strings.TrimSpace(projectPath), strings.TrimSpace(sessionID))
}

func (s *DeepServer) getOrCreateSessionFileLock(key string) *sessionFileLock {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionFileLocks == nil {
		s.sessionFileLocks = make(map[string]*sessionFileLock)
	}
	if lock, ok := s.sessionFileLocks[key]; ok {
		lock.lastUsed = time.Now()
		return lock
	}
	lock := &sessionFileLock{lastUsed: time.Now()}
	s.sessionFileLocks[key] = lock
	return lock
}

func (s *DeepServer) removeSessionFileLock(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessionFileLocks, key)
}

func resolveCanonicalSessionsBaseDir(projectPath string) (string, error) {
	return config.ResolveSessionsBaseDir(projectPath)
}

func (s *DeepServer) getSessionsBaseDir(projectPath string) (string, error) {
	normalizedProjectPath, err := normalizeProjectPath(projectPath)
	if err != nil {
		return "", err
	}
	if normalizedProjectPath == "" {
		return "", fmt.Errorf("project_path 不能为空")
	}

	sessionsDir, err := resolveCanonicalSessionsBaseDir(normalizedProjectPath)
	if err != nil {
		return "", err
	}
	s.trackSessionBaseDir(sessionsDir)
	return sessionsDir, nil
}

func (s *DeepServer) getSessionDir(projectPath, sessionID string) (string, error) {
	normalizedSessionID, err := validateSessionID(sessionID)
	if err != nil {
		return "", err
	}

	baseDir, err := s.getSessionsBaseDir(projectPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, normalizedSessionID), nil
}

func (s *DeepServer) initSessionDir(projectPath, sessionID string) (map[string]string, error) {
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
	return s.initSessionDirUnlocked(normalizedProjectPath, normalizedSessionID)
}

func (s *DeepServer) initSessionDirUnlocked(projectPath, sessionID string) (map[string]string, error) {
	if s.sessionWasDeleted(sessionID) {
		return nil, fmt.Errorf("会话已删除: %s", sessionID)
	}
	sessionDir, err := s.getSessionDir(projectPath, sessionID)
	if err != nil {
		return nil, err
	}

	dirs := make(map[string]string, len(sessionSubdirs)+1)
	dirs["root"] = sessionDir

	for _, sub := range sessionSubdirs {
		subPath := filepath.Join(sessionDir, sub)
		if err := os.MkdirAll(subPath, 0755); err != nil {
			return nil, fmt.Errorf("创建会话子目录 %s 失败: %w", sub, err)
		}
		dirs[sub] = subPath
	}

	if err := s.registerProjectPath(projectPath); err != nil {
		logger.Warn("记录项目索引失败: project_path=%s, err=%v", projectPath, err)
	}
	projectDir, projectFileName := filepath.Dir(projectPath), filepath.Base(projectPath)
	if err := registerProjectInWorkspace(projectDir, projectFileName); err != nil {
		logger.Warn("注册工程到 workspace.yaml 失败: project_dir=%s, file=%s, err=%v", projectDir, projectFileName, err)
	}

	logger.Debug("会话目录已初始化: %s", sessionDir)
	return dirs, nil
}

func (s *DeepServer) saveSessionToFile(sessionData map[string]any, projectPath string, operation sessionSaveOperation) error {
	rawSessionID, _ := sessionData["session_id"].(string)
	sessionID, err := validateSessionID(rawSessionID)
	if err != nil {
		return err
	}
	sessionData["session_id"] = sessionID

	normalizedProjectPath, err := normalizeProjectPath(projectPath)
	if err != nil {
		return err
	}
	if normalizedProjectPath == "" {
		return fmt.Errorf("project_path 不能为空")
	}
	sessionData["project_path"] = normalizedProjectPath

	lock := s.getOrCreateSessionFileLock(sessionLockKey(normalizedProjectPath, sessionID))
	lock.Lock()
	defer lock.Unlock()

	return s.saveSessionToFileUnlocked(sessionData, normalizedProjectPath, operation)
}

func (s *DeepServer) saveSessionToFileUnlocked(sessionData map[string]any, projectPath string, operation sessionSaveOperation) error {
	sessionID, _ := sessionData["session_id"].(string)
	if sessionID == "" {
		return fmt.Errorf("会话ID无效")
	}
	needSessionLogReset := operation.Type == sessionSaveOpClearHistory

	// 确保会话目录及子目录存在
	if _, err := s.initSessionDirUnlocked(projectPath, sessionID); err != nil {
		return err
	}

	sessionDir, err := s.getSessionDir(projectPath, sessionID)
	if err != nil {
		return err
	}

	if operation.Type == sessionSaveOpCreateSnapshot {
		if _, err := appendSessionSnapshot(sessionData, operation.Reason); err != nil {
			return err
		}
	}

	policy := s.getSessionRuntimePolicy()
	if err := s.enforceSessionActiveLimit(projectPath, sessionDir, policy.MaxActiveSessions); err != nil {
		return err
	}
	trimmedMessages := applyMessageLimit(sessionData, policy.MaxMessagesPerSession)
	if trimmedMessages > 0 {
		logger.Warn("会话消息超过上限，已裁剪: session_id=%s, removed=%d", sessionID, trimmedMessages)
	}

	trimmed := applySnapshotLimit(sessionData, policy.MaxSnapshotsPerSession)
	if trimmed > 0 {
		logger.Info("会话快照超过上限，已裁剪: session_id=%s, removed=%d", sessionID, trimmed)
	}

	snapshots := extractSessionSnapshots(sessionData)
	allowCreateMissingBackup := operation.Type != sessionSaveOpRestoreSnapshot
	if err := s.syncSessionSnapshotBackups(sessionDir, snapshots, allowCreateMissingBackup); err != nil {
		return err
	}

	switch operation.Type {
	case "":
		// no-op
	case sessionSaveOpClearHistory:
		if err := clearSessionRuntimeContent(sessionDir); err != nil {
			return err
		}
	case sessionSaveOpCreateSnapshot:
		// 快照已在前置步骤创建，后续仅写入 session.json 与同步快照备份。
	case sessionSaveOpRestoreSnapshot:
		if operation.SnapshotID == "" {
			return fmt.Errorf("恢复快照失败: snapshot_id 不能为空")
		}
		if operation.Mode != "" && operation.Mode != sessionRestoreModeStrict {
			return fmt.Errorf("恢复快照失败: 暂不支持模式 %s", operation.Mode)
		}
		if err := restoreSessionSnapshotStrict(sessionDir, operation.SnapshotID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("未知会话保存操作: %s", operation.Type)
	}

	sessionPath := filepath.Join(sessionDir, "session.json")
	data, err := json.MarshalIndent(sessionData, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化会话数据失败: %w", err)
	}

	if err := s.writeSessionRuntimeSnapshot(sessionDir, sessionData); err != nil {
		return err
	}

	if err := atomicWriteFile(sessionPath, data, 0644); err != nil {
		return fmt.Errorf("写入会话文件失败: %w", err)
	}
	s.syncManagedSessionWithSessionData(sessionData)

	if needSessionLogReset {
		s.broadcastSessionLogReset(sessionID, projectPath, sessionDir)
	}

	logger.Debug("会话已保存: %s", sessionPath)
	return nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	cleanup = false
	return nil
}

func extractSessionSnapshots(sessionData map[string]any) []sessionSnapshotMeta {
	rawSnapshots, ok := sessionData["snapshots"]
	if !ok {
		return nil
	}

	asList, ok := rawSnapshots.([]any)
	if !ok {
		return nil
	}

	seen := make(map[string]bool, len(asList))
	snapshots := make([]sessionSnapshotMeta, 0, len(asList))
	for _, item := range asList {
		snapshotData, ok := item.(map[string]any)
		if !ok {
			continue
		}
		snapshotID, _ := snapshotData["snapshot_id"].(string)
		snapshotID = strings.TrimSpace(snapshotID)
		if snapshotID == "" || seen[snapshotID] {
			continue
		}
		createdAt, _ := snapshotData["created_at"].(string)
		snapshots = append(snapshots, sessionSnapshotMeta{
			ID:        snapshotID,
			CreatedAt: strings.TrimSpace(createdAt),
		})
		seen[snapshotID] = true
	}
	return snapshots
}

func appendSessionSnapshot(sessionData map[string]any, reason string) (string, error) {
	now := time.Now().UTC()
	snapshotID := fmt.Sprintf("%d-%06d", now.UnixMilli(), now.UnixNano()%1_000_000)

	rawSnapshots, _ := sessionData["snapshots"].([]any)
	if rawSnapshots == nil {
		rawSnapshots = []any{}
	}

	messagesBackup := cloneAnySlice(sessionData["messages"])
	toolCallsBackup := cloneAnySlice(sessionData["tool_calls"])

	snapshot := map[string]any{
		"snapshot_id":       snapshotID,
		"created_at":        now.Format(time.RFC3339Nano),
		"reason":            normalizeSnapshotReason(reason),
		"messages_count":    len(messagesBackup),
		"token_count":       toSnapshotInt(sessionData["total_tokens_used"]),
		"messages_backup":   messagesBackup,
		"tool_calls_backup": toolCallsBackup,
	}

	sessionData["snapshots"] = append(rawSnapshots, snapshot)
	return snapshotID, nil
}

func normalizeSnapshotReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return "manual"
	}
	return trimmed
}

func cloneAnySlice(raw any) []any {
	items, ok := raw.([]any)
	if !ok || items == nil {
		return []any{}
	}
	cloned := make([]any, len(items))
	copy(cloned, items)
	return cloned
}

func toSnapshotInt(raw any) int {
	switch v := raw.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func (s *DeepServer) syncSessionSnapshotBackups(sessionDir string, snapshots []sessionSnapshotMeta, createMissing bool) error {
	snapshotsDir := filepath.Join(sessionDir, "snapshots")
	if err := os.MkdirAll(snapshotsDir, 0755); err != nil {
		return fmt.Errorf("创建快照目录失败: %w", err)
	}

	keep := make(map[string]bool, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.ID == "" {
			continue
		}
		keep[snapshot.ID] = true
		if !createMissing {
			continue
		}
		if err := ensureSnapshotBackup(sessionDir, snapshotsDir, snapshot); err != nil {
			return err
		}
	}

	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(snapshotsDir, name)
		if strings.HasPrefix(name, ".tmp-") {
			_ = os.RemoveAll(path)
			continue
		}
		if keep[name] {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("清理孤儿快照目录失败 %s: %w", path, err)
		}
		logger.Info("已清理孤儿快照目录: %s", path)
	}
	return nil
}

func ensureSnapshotBackup(sessionDir, snapshotsDir string, snapshot sessionSnapshotMeta) error {
	targetDir := filepath.Join(snapshotsDir, snapshot.ID)
	ready, err := isSnapshotBackupReady(targetDir)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}

	if _, err := os.Stat(targetDir); err == nil {
		if removeErr := os.RemoveAll(targetDir); removeErr != nil {
			return fmt.Errorf("移除损坏快照目录失败 %s: %w", targetDir, removeErr)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	tmpDir := filepath.Join(snapshotsDir, fmt.Sprintf(".tmp-%s-%d", snapshot.ID, time.Now().UnixNano()))
	stateDir := filepath.Join(tmpDir, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("创建快照临时目录失败: %w", err)
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	if err := copySessionStateForSnapshot(sessionDir, stateDir); err != nil {
		return err
	}

	manifest := map[string]any{
		"version":                    snapshotManifestVersion,
		"snapshot_id":                snapshot.ID,
		"snapshot_created_at":        snapshot.CreatedAt,
		"file_snapshot_created_at":   time.Now().UTC().Format(time.RFC3339Nano),
		"scope":                      "session_root_exclude[snapshots,session.json]",
		"session_relative_state_dir": "state",
	}
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化快照 manifest 失败: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "manifest.json"), manifestRaw, 0644); err != nil {
		return fmt.Errorf("写入快照 manifest 失败: %w", err)
	}

	if err := os.Rename(tmpDir, targetDir); err != nil {
		if _, statErr := os.Stat(targetDir); statErr == nil {
			return nil
		}
		return fmt.Errorf("提交快照目录失败: %w", err)
	}
	cleanupTmp = false
	logger.Info("会话快照文件备份已创建: session_dir=%s, snapshot_id=%s", sessionDir, snapshot.ID)
	return nil
}

func isSnapshotBackupReady(snapshotDir string) (bool, error) {
	stateDir := filepath.Join(snapshotDir, "state")
	stateInfo, err := os.Stat(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !stateInfo.IsDir() {
		return false, nil
	}
	manifestInfo, err := os.Stat(filepath.Join(snapshotDir, "manifest.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !manifestInfo.IsDir(), nil
}

func copySessionStateForSnapshot(sessionDir, dstRoot string) error {
	snapshotsDir := filepath.Clean(filepath.Join(sessionDir, "snapshots"))
	return filepath.Walk(sessionDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil {
			return nil
		}

		cleanPath := filepath.Clean(path)
		if cleanPath == snapshotsDir {
			return filepath.SkipDir
		}

		relPath, err := filepath.Rel(sessionDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}
		if relPath == "session.json" {
			return nil
		}
		if relPath == runtimeSessionSnapshotFileName {
			return nil
		}

		// 跳过符号链接，避免越界复制
		if info.Mode()&os.ModeSymlink != 0 {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dstPath := filepath.Join(dstRoot, relPath)
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyRegularFile(path, dstPath, info.Mode().Perm())
	})
}

func copyRegularFile(srcPath, dstPath string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return nil
}

func clearSessionRuntimeContent(sessionDir string) error {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(sessionDir, name)
		switch name {
		case "snapshots", "session.json":
			continue
		case "logs":
			if entry.IsDir() {
				if err := truncateSessionLogs(path); err != nil {
					return err
				}
				continue
			}
		}

		if entry.IsDir() {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		} else {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}

	return ensureSessionSubdirs(sessionDir)
}

func ensureSessionSubdirs(sessionDir string) error {
	for _, sub := range sessionSubdirs {
		if err := os.MkdirAll(filepath.Join(sessionDir, sub), 0755); err != nil {
			return err
		}
	}
	return nil
}

func truncateSessionLogs(logsDir string) error {
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(logsDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		for _, entry := range entries {
			path := filepath.Join(logsDir, entry.Name())
			if entry.IsDir() {
				if err := os.RemoveAll(path); err != nil {
					return err
				}
				continue
			}
			if entry.Name() == "session.log" {
				file, openErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
				if openErr != nil {
					return openErr
				}
				if closeErr := file.Close(); closeErr != nil {
					return closeErr
				}
				continue
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}

	// 保留文件句柄稳定：会话日志文件始终存在并就地截断。
	logFile := filepath.Join(logsDir, "session.log")
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	return file.Close()
}

func restoreSessionSnapshotStrict(sessionDir, snapshotID string) error {
	if snapshotID == "" {
		return fmt.Errorf("恢复快照失败: snapshot_id 不能为空")
	}

	snapshotDir := filepath.Join(sessionDir, "snapshots", snapshotID)
	ready, err := isSnapshotBackupReady(snapshotDir)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("恢复快照失败: 快照备份不存在 %s", snapshotID)
	}

	if err := clearSessionRuntimeContent(sessionDir); err != nil {
		return err
	}

	stateDir := filepath.Join(snapshotDir, "state")
	if err := copySnapshotStateToSession(stateDir, sessionDir); err != nil {
		return err
	}
	return ensureSessionSubdirs(sessionDir)
}

func copySnapshotStateToSession(stateDir, sessionDir string) error {
	return filepath.Walk(stateDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil {
			return nil
		}

		relPath, err := filepath.Rel(stateDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		if info.Mode()&os.ModeSymlink != 0 {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dstPath := filepath.Join(sessionDir, relPath)
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyRegularFile(path, dstPath, info.Mode().Perm())
	})
}

func (s *DeepServer) loadSessionFromFile(sessionID, projectPath string) (map[string]any, error) {
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

	return s.loadSessionFromFileUnlocked(normalizedSessionID, normalizedProjectPath)
}

func (s *DeepServer) loadSessionFromFileUnlocked(sessionID, projectPath string) (map[string]any, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("会话ID不能为空")
	}

	baseDir, err := s.getSessionsBaseDir(projectPath)
	if err != nil {
		return nil, err
	}

	sessionData, err := s.loadSessionFromBaseDirUnlocked(baseDir, sessionID)
	if err == nil {
		normalizeSessionContextStats(sessionData)
		return sessionData, nil
	}

	return nil, err
}

func (s *DeepServer) loadSessionFromBaseDirUnlocked(baseDir, sessionID string) (map[string]any, error) {
	sessionDir := filepath.Join(baseDir, sessionID)
	return loadSessionDataFromDirWithFallback(sessionDir)
}

func (s *DeepServer) listSessions(projectPath string) ([]map[string]any, error) {
	baseDir, err := s.getSessionsBaseDir(projectPath)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(baseDir)
	if os.IsNotExist(err) {
		return []map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取会话目录失败: %w", err)
	}
	var sessions []map[string]any

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		lock := s.getOrCreateSessionFileLock(sessionLockKey(projectPath, entry.Name()))
		lock.Lock()
		if s.sessionWasDeleted(entry.Name()) {
			lock.Unlock()
			continue
		}
		sessionDir := filepath.Join(baseDir, entry.Name())
		sessionData, err := loadSessionDataFromDirWithFallback(sessionDir)
		lock.Unlock()
		if err != nil {
			continue
		}
		normalizeSessionContextStats(sessionData)
		sessions = append(sessions, sessionData)
	}

	return sessions, nil
}

func normalizeSessionContextStats(sessionData map[string]any) {
	if sessionData == nil {
		return
	}
	messages := decodeSessionMessages(sessionData["messages"], -1)
	stats := buildContextStatsSnapshot(messages, 0)
	existing := asMap(sessionData["context_stats"])
	compressedCount := 0
	if existing != nil {
		if value, ok := existing["compressed_count"].(float64); ok {
			compressedCount = int(value)
		}
		if value, ok := existing["compressed_count"].(int); ok {
			compressedCount = value
		}
	}
	sessionData["context_stats"] = map[string]any{
		"message_count":    stats.MessageCount,
		"total_chars":      stats.TotalChars,
		"estimated_tokens": stats.EstimatedTokens,
		"total_tokens":     stats.EstimatedTokens,
		"compressed_count": compressedCount,
	}
	sessionData["total_tokens_used"] = stats.EstimatedTokens
}

func defaultSessionRuntimePolicy() sessionRuntimePolicy {
	return sessionRuntimePolicy{
		MaxSnapshotsPerSession: 10,
		AutoArchiveEnabled:     false,
		AutoArchiveDays:        30,
		AutoCleanupEnabled:     false,
		AutoCleanupDays:        90,
		IdleTimeoutSeconds:     0,
		MaxDurationSeconds:     0,
		MaxActiveSessions:      0,
		MaxMessagesPerSession:  0,
		AutoSaveInterval:       60,
		CrashRecoveryEnabled:   true,
	}
}

func (s *DeepServer) getSessionRuntimePolicy() sessionRuntimePolicy {
	policy := defaultSessionRuntimePolicy()
	if s.configService == nil {
		return policy
	}

	cfg, err := s.configService.GetFullConfig()
	if err != nil || cfg == nil {
		return policy
	}

	if cfg.Session.MaxSnapshotsPerSession > 0 {
		policy.MaxSnapshotsPerSession = cfg.Session.MaxSnapshotsPerSession
	}
	if cfg.Session.AutoArchive.Enabled != nil {
		policy.AutoArchiveEnabled = *cfg.Session.AutoArchive.Enabled
	}
	if cfg.Session.AutoArchive.Days > 0 {
		policy.AutoArchiveDays = cfg.Session.AutoArchive.Days
	}
	if cfg.Session.AutoCleanup.Enabled != nil {
		policy.AutoCleanupEnabled = *cfg.Session.AutoCleanup.Enabled
	}
	if cfg.Session.AutoCleanup.ArchiveRetentionDays > 0 {
		policy.AutoCleanupDays = cfg.Session.AutoCleanup.ArchiveRetentionDays
	}
	if cfg.Session.Timeout.IdleTimeoutSeconds > 0 {
		policy.IdleTimeoutSeconds = cfg.Session.Timeout.IdleTimeoutSeconds
	}
	if cfg.Session.Timeout.MaxDurationSeconds > 0 {
		policy.MaxDurationSeconds = cfg.Session.Timeout.MaxDurationSeconds
	}
	if cfg.Session.Concurrency.MaxActiveSessions > 0 {
		policy.MaxActiveSessions = cfg.Session.Concurrency.MaxActiveSessions
	}
	if cfg.Session.Concurrency.MaxMessagesPerSession > 0 {
		policy.MaxMessagesPerSession = cfg.Session.Concurrency.MaxMessagesPerSession
	}
	if cfg.Session.Recovery.AutoSaveIntervalSeconds > 0 {
		policy.AutoSaveInterval = cfg.Session.Recovery.AutoSaveIntervalSeconds
	}
	policy.CrashRecoveryEnabled = cfg.Session.Recovery.IsCrashRecoveryEnabled()

	return policy
}

func applySnapshotLimit(sessionData map[string]any, maxSnapshots int) int {
	if maxSnapshots <= 0 {
		maxSnapshots = 10
	}

	rawSnapshots, ok := sessionData["snapshots"]
	if !ok {
		return 0
	}

	snapshots, ok := rawSnapshots.([]any)
	if !ok {
		return 0
	}

	if len(snapshots) <= maxSnapshots {
		return 0
	}

	trimmed := len(snapshots) - maxSnapshots
	sessionData["snapshots"] = append([]any(nil), snapshots[trimmed:]...)
	return trimmed
}

func applyMessageLimit(sessionData map[string]any, maxMessages int) int {
	if maxMessages <= 0 {
		return 0
	}

	rawMessages, ok := sessionData["messages"]
	if !ok {
		return 0
	}
	messages, ok := rawMessages.([]any)
	if !ok {
		return 0
	}
	if len(messages) <= maxMessages {
		return 0
	}

	trimmed := len(messages) - maxMessages
	sessionData["messages"] = append([]any(nil), messages[trimmed:]...)
	return trimmed
}

func (s *DeepServer) enforceSessionActiveLimit(projectPath, sessionDir string, maxActive int) error {
	if maxActive <= 0 {
		return nil
	}

	currentSessionPath := filepath.Join(sessionDir, "session.json")
	if _, err := os.Stat(currentSessionPath); err == nil {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("检查会话状态失败: %w", err)
	}
	if sessionPersistenceExists(sessionDir) {
		return nil
	}

	sessions, err := s.listSessions(projectPath)
	if err != nil {
		return fmt.Errorf("检查会话并发上限失败: %w", err)
	}

	activeCount := 0
	for _, item := range sessions {
		archived, _ := item["archived"].(bool)
		if !archived {
			activeCount++
		}
	}
	if activeCount >= maxActive {
		return fmt.Errorf("活跃会话数量达到上限（%d），请归档或删除旧会话后重试", maxActive)
	}

	return nil
}

func parseSessionTime(values ...any) time.Time {
	for _, raw := range values {
		text, ok := raw.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return ts
		}
		if ts, err := time.Parse(time.RFC3339, text); err == nil {
			return ts
		}
	}
	return time.Time{}
}

func (s *DeepServer) trackSessionBaseDir(baseDir string) {
	cleaned := strings.TrimSpace(baseDir)
	if cleaned == "" {
		return
	}

	cleaned = filepath.Clean(cleaned)
	s.mu.Lock()
	if s.knownSessionBaseDirs == nil {
		s.knownSessionBaseDirs = make(map[string]time.Time)
	}
	s.knownSessionBaseDirs[cleaned] = time.Now()
	s.mu.Unlock()
}

func (s *DeepServer) listKnownSessionBaseDirs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	dirs := make([]string, 0, len(s.knownSessionBaseDirs))
	for dir := range s.knownSessionBaseDirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs
}

func (s *DeepServer) cleanupStaleSessionFileLocks(ttl time.Duration) int {
	if ttl <= 0 {
		ttl = sessionFileLockTTL
	}
	cutoff := time.Now().Add(-ttl)

	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for key, lock := range s.sessionFileLocks {
		if lock == nil {
			delete(s.sessionFileLocks, key)
			removed++
			continue
		}
		if lock.lastUsed.Before(cutoff) {
			if !lock.TryLock() {
				continue
			}
			lock.Unlock()
			delete(s.sessionFileLocks, key)
			removed++
		}
	}
	return removed
}

func (s *DeepServer) pruneInvalidSessionBaseDirs() int {
	dirs := s.listKnownSessionBaseDirs()
	if len(dirs) == 0 {
		return 0
	}

	removed := 0
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			logger.Warn("检查会话基目录失败，跳过清理: base_dir=%s, err=%v", dir, err)
			continue
		}

		s.mu.Lock()
		if _, ok := s.knownSessionBaseDirs[dir]; ok {
			delete(s.knownSessionBaseDirs, dir)
			removed++
		}
		s.mu.Unlock()
	}

	return removed
}

func resolveProjectRegistryPath() (string, error) {
	dataDir, err := config.ResolvePluginDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, projectRegistryFileName), nil
}

func loadProjectRegistry(path string) (projectRegistryPayload, error) {
	payload := projectRegistryPayload{Projects: []projectRegistryEntry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return payload, nil
		}
		return payload, err
	}
	if len(data) == 0 {
		return payload, nil
	}
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return payload, err
	}
	if payload.Projects == nil {
		payload.Projects = []projectRegistryEntry{}
	}
	return payload, nil
}

func saveProjectRegistry(path string, payload projectRegistryPayload) error {
	if payload.Projects == nil {
		payload.Projects = []projectRegistryEntry{}
	}
	encoded, err := yaml.Marshal(payload)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, encoded, 0644)
}

func parseProjectRegistryTime(raw string) time.Time {
	text := strings.TrimSpace(raw)
	if text == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, text); err == nil {
		return ts
	}
	return time.Time{}
}

func sortProjectRegistryEntries(entries []projectRegistryEntry) {
	sort.Slice(entries, func(i, j int) bool {
		left := parseProjectRegistryTime(entries[i].LastAccessed)
		right := parseProjectRegistryTime(entries[j].LastAccessed)
		if left.Equal(right) {
			return entries[i].Path < entries[j].Path
		}
		return left.After(right)
	})
}

func (s *DeepServer) registerProjectPath(projectPath string) error {
	normalizedProjectPath, err := normalizeProjectPath(projectPath)
	if err != nil || normalizedProjectPath == "" {
		return err
	}

	registryPath, err := resolveProjectRegistryPath()
	if err != nil {
		return err
	}

	s.projectRegistryMu.Lock()
	defer s.projectRegistryMu.Unlock()

	payload, err := loadProjectRegistry(registryPath)
	if err != nil {
		return err
	}

	nowText := time.Now().UTC().Format(time.RFC3339Nano)
	replaced := false
	for i := range payload.Projects {
		if filepath.Clean(strings.TrimSpace(payload.Projects[i].Path)) == normalizedProjectPath {
			payload.Projects[i].Path = normalizedProjectPath
			payload.Projects[i].LastAccessed = nowText
			replaced = true
			break
		}
	}
	if !replaced {
		payload.Projects = append(payload.Projects, projectRegistryEntry{
			Path:         normalizedProjectPath,
			LastAccessed: nowText,
		})
	}

	sortProjectRegistryEntries(payload.Projects)
	return saveProjectRegistry(registryPath, payload)
}

func (s *DeepServer) listProjectRegistryEntries() ([]projectRegistryEntry, error) {
	registryPath, err := resolveProjectRegistryPath()
	if err != nil {
		return nil, err
	}

	s.projectRegistryMu.Lock()
	defer s.projectRegistryMu.Unlock()

	payload, err := loadProjectRegistry(registryPath)
	if err != nil {
		return nil, err
	}

	entries := make([]projectRegistryEntry, 0, len(payload.Projects))
	seen := make(map[string]bool, len(payload.Projects))
	for _, item := range payload.Projects {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if !filepath.IsAbs(path) || seen[path] {
			continue
		}
		seen[path] = true
		entries = append(entries, projectRegistryEntry{
			Path:         path,
			LastAccessed: strings.TrimSpace(item.LastAccessed),
		})
	}
	sortProjectRegistryEntries(entries)
	return entries, nil
}

func (s *DeepServer) listProjects() ([]map[string]any, error) {
	if _, err := s.pruneProjectRegistryMissingPaths(); err != nil {
		logger.Warn("读取项目索引前清理失效路径失败: %v", err)
	}

	entries, err := s.listProjectRegistryEntries()
	if err != nil {
		return nil, err
	}

	projects := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		projects = append(projects, map[string]any{
			"path":          entry.Path,
			"last_accessed": entry.LastAccessed,
		})
	}
	return projects, nil
}

func (s *DeepServer) pruneProjectRegistryMissingPaths() (int, error) {
	registryPath, err := resolveProjectRegistryPath()
	if err != nil {
		return 0, err
	}

	s.projectRegistryMu.Lock()
	defer s.projectRegistryMu.Unlock()

	payload, err := loadProjectRegistry(registryPath)
	if err != nil {
		return 0, err
	}

	kept := make([]projectRegistryEntry, 0, len(payload.Projects))
	seen := make(map[string]bool, len(payload.Projects))
	removed := 0
	for _, item := range payload.Projects {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			removed++
			continue
		}
		path = filepath.Clean(path)
		if !filepath.IsAbs(path) || seen[path] {
			removed++
			continue
		}
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				removed++
				continue
			}
			logger.Warn("检查项目索引路径失败，保留原记录: path=%s, err=%v", path, err)
		}
		seen[path] = true
		kept = append(kept, projectRegistryEntry{
			Path:         path,
			LastAccessed: strings.TrimSpace(item.LastAccessed),
		})
	}

	if removed == 0 {
		return 0, nil
	}

	payload.Projects = kept
	sortProjectRegistryEntries(payload.Projects)
	if err := saveProjectRegistry(registryPath, payload); err != nil {
		return 0, err
	}
	return removed, nil
}

func (s *DeepServer) restoreSessionBaseDirsFromProjectRegistry() {
	entries, err := s.listProjectRegistryEntries()
	if err != nil {
		logger.Warn("加载项目索引失败，跳过会话基目录恢复: %v", err)
		return
	}
	for _, entry := range entries {
		projectDir := filepath.Dir(entry.Path)
		s.trackSessionBaseDir(filepath.Join(projectDir, "autoc", "sessions"))
	}
}

func (s *DeepServer) startSessionMaintenance() {
	if s.configService == nil {
		return
	}

	s.mu.Lock()
	if s.sessionMaintenanceStarted {
		s.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	s.sessionMaintenanceStopCh = stopCh
	s.sessionMaintenanceStarted = true
	s.mu.Unlock()

	s.restoreSessionBaseDirsFromProjectRegistry()

	go s.runSessionMaintenanceLoop(stopCh)
	logger.Info("会话策略维护任务已启动")
}

func (s *DeepServer) stopSessionMaintenance() {
	s.mu.Lock()
	if !s.sessionMaintenanceStarted {
		s.mu.Unlock()
		return
	}
	stopCh := s.sessionMaintenanceStopCh
	s.sessionMaintenanceStopCh = nil
	s.sessionMaintenanceStarted = false
	s.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
}

func (s *DeepServer) runSessionMaintenanceLoop(stopCh <-chan struct{}) {
	ticker := time.NewTicker(sessionMaintenanceInterval)
	defer ticker.Stop()

	s.runSessionMaintenance()

	for {
		select {
		case <-stopCh:
			logger.Info("会话策略维护任务已停止")
			return
		case <-ticker.C:
			s.runSessionMaintenance()
		}
	}
}

func (s *DeepServer) runSessionMaintenance() {
	if removedLocks := s.cleanupStaleSessionFileLocks(sessionFileLockTTL); removedLocks > 0 {
		logger.Debug("已回收过期会话文件锁: count=%d", removedLocks)
	}
	if removedDirs := s.pruneInvalidSessionBaseDirs(); removedDirs > 0 {
		logger.Info("已清理失效会话基目录: count=%d", removedDirs)
	}
	if removedProjects, err := s.pruneProjectRegistryMissingPaths(); err != nil {
		logger.Warn("清理项目索引失败: %v", err)
	} else if removedProjects > 0 {
		logger.Info("已清理失效项目索引记录: count=%d", removedProjects)
	}

	policy := s.getSessionRuntimePolicy()
	if !policy.AutoArchiveEnabled && !policy.AutoCleanupEnabled {
		return
	}

	baseDirs := s.listKnownSessionBaseDirs()
	if len(baseDirs) == 0 {
		return
	}

	totalArchived := 0
	totalCleaned := 0
	for _, baseDir := range baseDirs {
		archived, cleaned, err := s.applySessionPolicyInBaseDir(baseDir, policy)
		if err != nil {
			logger.Warn("会话策略维护失败: base_dir=%s, err=%v", baseDir, err)
			continue
		}
		totalArchived += archived
		totalCleaned += cleaned
	}

	if totalArchived > 0 || totalCleaned > 0 {
		logger.Info("会话策略维护完成: archived=%d, cleaned=%d", totalArchived, totalCleaned)
		s.broadcastSessionListChanged(totalArchived, totalCleaned)
	}
}

func (s *DeepServer) broadcastSessionListChanged(archivedCount, cleanedCount int) {
	s.realtimeService().BroadcastSessionChange("maintenance", "", map[string]any{
		"archived_count": archivedCount,
		"cleaned_count":  cleanedCount,
	})
}

func (s *DeepServer) broadcastSessionChange(changeType, sessionID string, extra map[string]any) {
	msg := Message{
		Type: MsgTypeSessionListChanged,
		Data: map[string]any{
			"change_type": strings.TrimSpace(changeType),
			"session_id":  strings.TrimSpace(sessionID),
			"updated_at":  time.Now().Format(time.RFC3339),
		},
	}

	for key, value := range extra {
		msg.Data[key] = value
	}

	connections := s.listClientConnections()
	for _, conn := range connections {
		if err := s.sendMessage(conn, msg); err != nil {
			logger.Warn("推送 session_list_changed 失败: %v", err)
		}
	}
}

func (s *DeepServer) broadcastSessionLogReset(sessionID, projectPath, sessionDir string) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return
	}
	if s != nil && s.hasLocalBridgeRunningInstances() {
		logsDir := filepath.Join(strings.TrimSpace(sessionDir), "logs")
		s.mu.Lock()
		localBridgeProvider := s.localBridgeProvider
		s.mu.Unlock()
		if localBridgeProvider != nil {
			if err := localBridgeProvider.BroadcastSessionLogReset(normalizedSessionID, strings.TrimSpace(projectPath), strings.TrimSpace(sessionDir), logsDir); err != nil {
				logger.Warn("通过本地 bridge 转发 session_log_reset 失败: session_id=%s err=%v", normalizedSessionID, err)
			}
			return
		}
	}
}

func (s *DeepServer) applySessionPolicyInBaseDir(baseDir string, policy sessionRuntimePolicy) (int, int, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}

	now := time.Now()
	archiveThreshold := time.Duration(policy.AutoArchiveDays) * 24 * time.Hour
	cleanupThreshold := time.Duration(policy.AutoCleanupDays) * 24 * time.Hour

	archivedCount := 0
	cleanedCount := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		sessionDir := filepath.Join(baseDir, entry.Name())
		sessionPath := filepath.Join(sessionDir, "session.json")
		raw, readErr := os.ReadFile(sessionPath)
		if readErr != nil {
			continue
		}

		var sessionData map[string]any
		if err := json.Unmarshal(raw, &sessionData); err != nil {
			continue
		}

		sessionID, _ := sessionData["session_id"].(string)
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			sessionID = entry.Name()
		}

		lastActive := parseSessionTime(sessionData["updated_at"], sessionData["created_at"])
		if lastActive.IsZero() {
			if info, statErr := os.Stat(sessionPath); statErr == nil {
				lastActive = info.ModTime()
			}
		}

		archived, _ := sessionData["archived"].(bool)
		changed := false

		if policy.AutoArchiveEnabled && !archived && !lastActive.IsZero() && now.Sub(lastActive) >= archiveThreshold {
			sessionData["archived"] = true
			sessionData["updated_at"] = now.Format(time.RFC3339)
			archived = true
			changed = true
			archivedCount++
			lastActive = now
		}

		if policy.AutoCleanupEnabled && archived && !lastActive.IsZero() && now.Sub(lastActive) >= cleanupThreshold {
			if err := os.RemoveAll(sessionDir); err != nil {
				logger.Warn("会话策略维护清理失败: session_id=%s, dir=%s, err=%v", sessionID, sessionDir, err)
				continue
			}
			cleanedCount++
			continue
		}

		if err := cleanSessionTempFiles(sessionDir, sessionTempCleanupMaxAge); err != nil {
			logger.Warn("清理会话临时目录失败: session_id=%s, dir=%s, err=%v", sessionID, sessionDir, err)
		}

		if changed {
			payload, err := json.MarshalIndent(sessionData, "", "  ")
			if err != nil {
				logger.Warn("会话策略维护序列化失败: session_id=%s, err=%v", sessionID, err)
				continue
			}
			if err := atomicWriteFile(sessionPath, payload, 0644); err != nil {
				logger.Warn("会话策略维护写入失败: session_id=%s, path=%s, err=%v", sessionID, sessionPath, err)
				continue
			}
		}
	}

	return archivedCount, cleanedCount, nil
}

func cleanSessionTempFiles(sessionDir string, maxAge time.Duration) error {
	if maxAge <= 0 {
		return nil
	}
	tempDir := filepath.Join(sessionDir, "temp")
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		path := filepath.Join(tempDir, entry.Name())
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if entry.IsDir() {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

func (s *DeepServer) deleteSessionFile(sessionID, projectPath string) error {
	normalizedSessionID, err := validateSessionID(sessionID)
	if err != nil {
		return err
	}
	normalizedProjectPath, err := normalizeProjectPath(projectPath)
	if err != nil {
		return err
	}

	lockKey := sessionLockKey(normalizedProjectPath, normalizedSessionID)
	baseDir, err := s.getSessionsBaseDir(normalizedProjectPath)
	if err != nil {
		return err
	}

	sessionDir := filepath.Join(baseDir, normalizedSessionID)
	info, statErr := os.Stat(sessionDir)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return fmt.Errorf("会话不存在: %s", normalizedSessionID)
		}
		return fmt.Errorf("检查会话目录失败: %w", statErr)
	}
	if !info.IsDir() {
		return fmt.Errorf("会话目录异常: %s", sessionDir)
	}

	workers, err := s.beginSessionDeletion(normalizedSessionID)
	if err != nil {
		return err
	}
	deleted := false
	defer func() {
		if !deleted {
			s.abortSessionDeletion(normalizedSessionID)
		}
	}()
	// Cancellation can persist interrupted state: never call it with the file
	// lock held. Wait for local workers before removing files they may write.
	for _, worker := range workers {
		worker.cancel()
	}
	s.cancelSessionTasksForSession(normalizedSessionID)
	if err := waitSessionWorkers(workers, sessionDeleteWaitTimeout); err != nil {
		return err
	}
	lock := s.getOrCreateSessionFileLock(lockKey)
	lock.Lock()
	defer lock.Unlock()
	if err := os.RemoveAll(sessionDir); err != nil {
		return fmt.Errorf("删除会话目录失败: %w", err)
	}
	deleted = true
	if s.sessionManager != nil {
		s.sessionManager.Delete(normalizedSessionID)
	}
	logger.Debug("会话目录已删除: %s", sessionDir)

	return nil
}
