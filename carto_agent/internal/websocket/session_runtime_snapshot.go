package websocket

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/autocarto/carto_agent/internal/session"
	"github.com/autocarto/carto_agent/internal/sessionmeta"
)

const runtimeSessionSnapshotFileName = "runtime_snapshot.json"

func describeSessionDataRestoreSource(sessionFileLoaded, projectionMerged bool) string {
	if sessionFileLoaded {
		return "session_file"
	}
	if projectionMerged {
		return "runtime_snapshot_projection_merge"
	}
	return "runtime_snapshot_fallback"
}

type sessionDataRestoreObservation struct {
	SessionDir   string
	SnapshotPath string
	Source       string
	PrimaryError string
	LogLevel     logger.LogLevel
}

func buildSessionDataRestoreObservation(sessionDir, snapshotPath string, sessionFileLoaded, projectionMerged bool, primaryErr error) sessionDataRestoreObservation {
	observation := sessionDataRestoreObservation{
		SessionDir:   strings.TrimSpace(sessionDir),
		SnapshotPath: strings.TrimSpace(snapshotPath),
		Source:       describeSessionDataRestoreSource(sessionFileLoaded, projectionMerged),
		LogLevel:     logger.INFO,
	}
	if primaryErr != nil {
		observation.PrimaryError = primaryErr.Error()
	}
	return observation
}

func logSessionDataRestoreObservation(observation sessionDataRestoreObservation) {
	if observation.LogLevel >= logger.WARN {
		logger.Warn("会话数据恢复来源: session_dir=%s snapshot=%s source=%s primary_err=%s", observation.SessionDir, observation.SnapshotPath, observation.Source, observation.PrimaryError)
		return
	}
	logger.Info("会话数据恢复来源: session_dir=%s snapshot=%s source=%s primary_err=%s", observation.SessionDir, observation.SnapshotPath, observation.Source, observation.PrimaryError)
}

func (s *DeepServer) writeSessionRuntimeSnapshot(sessionDir string, sessionData map[string]any) error {
	snapshot, err := s.captureRuntimeSessionSnapshot(sessionData)
	if err != nil {
		return err
	}
	path := filepath.Join(sessionDir, runtimeSessionSnapshotFileName)
	if err := snapshot.SaveJSON(path); err != nil {
		return fmt.Errorf("保存运行时会话快照失败: %w", err)
	}
	return nil
}

func (s *DeepServer) syncManagedSessionWithSessionData(sessionData map[string]any) {
	if s == nil || s.sessionManager == nil || sessionData == nil {
		return
	}

	sessionID, _ := sessionData["session_id"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	managed := s.sessionManager.Get(sessionID)
	if managed == nil {
		return
	}

	createdAt := parseSessionTime(sessionData["created_at"])
	lastActiveAt := parseSessionTime(sessionData["updated_at"], sessionData["last_active_at"], sessionData["created_at"])
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	if lastActiveAt.IsZero() {
		lastActiveAt = createdAt
	}

	managed.Restore(
		decodeSessionMessages(sessionData["messages"], -1),
		decodePlanContext(sessionData[sessionPlanContextKey]),
		createdAt,
		lastActiveAt,
	)
	logger.Info("内存会话状态已同步到持久化版本: session_id=%s", sessionID)
}

func (s *DeepServer) restoreManagedSessionFromSessionData(sessionData map[string]any) {
	if s == nil || s.sessionManager == nil || sessionData == nil {
		return
	}

	sessionID, _ := sessionData["session_id"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	createdAt := parseSessionTime(sessionData["created_at"])
	lastActiveAt := parseSessionTime(sessionData["updated_at"], sessionData["last_active_at"], sessionData["created_at"])
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	if lastActiveAt.IsZero() {
		lastActiveAt = createdAt
	}

	managed := s.sessionManager.GetOrCreate(sessionID)
	managed.Restore(
		decodeSessionMessages(sessionData["messages"], -1),
		decodePlanContext(sessionData[sessionPlanContextKey]),
		createdAt,
		lastActiveAt,
	)
	logger.Info("内存会话状态已从持久化载入恢复: session_id=%s", sessionID)
}

func (s *DeepServer) captureRuntimeSessionSnapshot(sessionData map[string]any) (*runtimepersist.SessionSnapshot, error) {
	if sessionData == nil {
		return nil, fmt.Errorf("会话数据不能为空")
	}

	sessionID, _ := sessionData["session_id"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("会话ID不能为空")
	}

	createdAt := parseSessionTime(sessionData["created_at"])
	lastActiveAt := parseSessionTime(sessionData["updated_at"], sessionData["last_active_at"], sessionData["created_at"])
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	if lastActiveAt.IsZero() {
		lastActiveAt = createdAt
	}

	fallbackSession := &session.Session{
		ID:           sessionID,
		Messages:     decodeSessionMessages(sessionData["messages"], -1),
		PlanContext:  decodePlanContext(sessionData[sessionPlanContextKey]),
		CreatedAt:    createdAt,
		LastActiveAt: lastActiveAt,
	}

	meta := s.currentRuntimeMetadata()
	snapshot := runtimepersist.CaptureSessionSnapshot(fallbackSession, meta.ActiveConfig, meta.RuntimeRevision)
	if s.sessionManager != nil {
		if managed := s.sessionManager.Get(sessionID); managed != nil {
			if liveSnapshot := runtimepersist.CaptureSessionSnapshot(managed, meta.ActiveConfig, meta.RuntimeRevision); liveSnapshot != nil {
				snapshot = mergeRuntimeSessionSnapshot(liveSnapshot, snapshot)
			}
		}
	}
	if snapshot == nil {
		return nil, fmt.Errorf("捕获运行时会话快照失败")
	}
	projection := s.sessionRuntimeProjectionSnapshot(sessionID, sessionData)
	if runtimeProjection := projectionToSnapshot(projection); runtimeProjection != nil {
		snapshot.RuntimeProjection = runtimeProjection
	}
	return snapshot, nil
}

func mergeRuntimeSessionSnapshot(primary, fallback *runtimepersist.SessionSnapshot) *runtimepersist.SessionSnapshot {
	if primary == nil {
		return fallback
	}
	if fallback == nil {
		return primary
	}

	merged := *fallback
	if strings.TrimSpace(primary.SessionID) != "" {
		merged.SessionID = primary.SessionID
	}
	if strings.TrimSpace(primary.ActiveConfigName) != "" {
		merged.ActiveConfigName = primary.ActiveConfigName
	}
	if strings.TrimSpace(primary.RuntimeRevision) != "" {
		merged.RuntimeRevision = primary.RuntimeRevision
	}
	if len(primary.Messages) > 0 {
		merged.Messages = primary.Messages
	}
	if primary.PlanContext != nil {
		merged.PlanContext = primary.PlanContext
	}
	if !primary.CreatedAt.IsZero() {
		merged.CreatedAt = primary.CreatedAt
	}
	if !primary.LastActiveAt.IsZero() {
		merged.LastActiveAt = primary.LastActiveAt
	}
	if primary.RuntimeProjection != nil {
		merged.RuntimeProjection = primary.RuntimeProjection
	} else if fallback.RuntimeProjection != nil {
		merged.RuntimeProjection = fallback.RuntimeProjection
	}
	return &merged
}

func loadSessionDataFromDirWithFallback(sessionDir string) (map[string]any, error) {
	sessionPath := filepath.Join(sessionDir, "session.json")
	var primaryErr error
	data, err := os.ReadFile(sessionPath)
	if err == nil {
		var sessionData map[string]any
		decodeErr := json.Unmarshal(data, &sessionData)
		if decodeErr == nil {
			logSessionDataRestoreObservation(buildSessionDataRestoreObservation(sessionDir, sessionPath, true, false, nil))
			return sessionData, nil
		}
		primaryErr = fmt.Errorf("解析会话数据失败: %w", decodeErr)
		logger.Warn("解析会话文件失败，尝试运行时快照回退: path=%s, err=%v", sessionPath, decodeErr)
	} else {
		primaryErr = fmt.Errorf("读取会话文件失败: %w", err)
		logger.Warn("读取会话文件失败，尝试运行时快照回退: path=%s, err=%v", sessionPath, err)
	}

	snapshotPath := filepath.Join(sessionDir, runtimeSessionSnapshotFileName)
	snapshot, snapshotErr := runtimepersist.LoadSessionSnapshotJSON(snapshotPath)
	if snapshotErr != nil {
		if primaryErr != nil {
			return nil, fmt.Errorf("%v; 读取运行时会话快照失败: %w", primaryErr, snapshotErr)
		}
		return nil, fmt.Errorf("读取运行时会话快照失败: %w", snapshotErr)
	}

	sessionData, buildErr := rebuildSessionDataFromRuntimeSnapshot(sessionDir, snapshot)
	if buildErr != nil {
		return nil, buildErr
	}

	observation := buildSessionDataRestoreObservation(sessionDir, snapshotPath, false, snapshot.RuntimeProjection != nil, primaryErr)
	logSessionDataRestoreObservation(observation)
	logger.Info("运行时会话快照回退命中: session_dir=%s snapshot=%s source=%s primary_err=%v", sessionDir, snapshotPath, observation.Source, primaryErr)
	return sessionData, nil
}

func rebuildSessionDataFromRuntimeSnapshot(sessionDir string, snapshot *runtimepersist.SessionSnapshot) (map[string]any, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("运行时会话快照不能为空")
	}

	sessionID := strings.TrimSpace(snapshot.SessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("运行时会话快照缺少 session_id")
	}

	createdAt := snapshot.CreatedAt
	if createdAt.IsZero() {
		createdAt = snapshot.LastActiveAt
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	lastActiveAt := snapshot.LastActiveAt
	if lastActiveAt.IsZero() {
		lastActiveAt = createdAt
	}

	stats := buildContextStatsSnapshot(snapshot.Messages, 0)

	data := map[string]any{
		"session_id":          sessionID,
		"name":                "会话 " + createdAt.Format("01-02 15:04"),
		"name_auto_generated": true,
		"title_source":        sessionmeta.TitleSourceDefault,
		"messages":            encodeRuntimeSnapshotMessages(snapshot.Messages),
		"execution_mode":      string(executionModeBuild),
		"run_intent":          "execute",
		"created_at":          createdAt.Format(time.RFC3339),
		"updated_at":          lastActiveAt.Format(time.RFC3339),
		"total_tokens_used":   stats.EstimatedTokens,
		"context_stats": map[string]any{
			"message_count":    stats.MessageCount,
			"total_chars":      stats.TotalChars,
			"estimated_tokens": stats.EstimatedTokens,
			"total_tokens":     stats.EstimatedTokens,
			"compressed_count": 0,
		},
		"snapshots": discoverRuntimeSnapshotBackups(sessionDir),
	}

	planContext, err := encodePlanContext(snapshot.PlanContext)
	if err != nil {
		return nil, fmt.Errorf("编码运行时会话规划上下文失败: %w", err)
	}
	if planContext != nil {
		data[sessionPlanContextKey] = planContext
	}

	projection := projectionFromSnapshot(sessionID, snapshot.RuntimeProjection)
	applyProjectionToSessionData(data, projection)

	return data, nil
}

func encodeRuntimeSnapshotMessages(messages []*schema.Message) []any {
	if len(messages) == 0 {
		return []any{}
	}

	raw, err := json.Marshal(messages)
	if err == nil {
		var encoded []any
		if err := json.Unmarshal(raw, &encoded); err == nil {
			for i, item := range encoded {
				if payload, ok := item.(map[string]any); ok {
					persistKnowledgeReviewCard(payload, messages[i])
					if msg := messages[i]; msg != nil && msg.Extra != nil {
						if id, ok := msg.Extra["flow_run_id"].(string); ok && id != "" {
							data := asMap(payload["data"])
							if data == nil {
								data = map[string]any{}
							}
							data["flow_run_id"] = id
							if step, ok := msg.Extra["step_id"].(string); ok {
								data["step_id"] = step
							}
							payload["data"] = data
						}
					}
				}
			}
			return encoded
		}
	}

	encoded := make([]any, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		encoded = append(encoded, map[string]any{
			"role":    string(msg.Role),
			"content": msg.Content,
		})
	}
	return encoded
}

func discoverRuntimeSnapshotBackups(sessionDir string) []any {
	snapshotsDir := filepath.Join(sessionDir, "snapshots")
	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		return []any{}
	}

	type snapshotMeta struct {
		id        string
		createdAt time.Time
		rawTime   string
	}

	metas := make([]snapshotMeta, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		snapshotID := strings.TrimSpace(entry.Name())
		if snapshotID == "" || strings.HasPrefix(snapshotID, ".tmp-") {
			continue
		}

		rawTime := readRuntimeSnapshotCreatedAt(filepath.Join(snapshotsDir, snapshotID, "manifest.json"))
		metas = append(metas, snapshotMeta{
			id:        snapshotID,
			createdAt: parseSessionTime(rawTime),
			rawTime:   rawTime,
		})
	}

	sort.Slice(metas, func(i, j int) bool {
		if !metas[i].createdAt.Equal(metas[j].createdAt) {
			if metas[i].createdAt.IsZero() {
				return true
			}
			if metas[j].createdAt.IsZero() {
				return false
			}
			return metas[i].createdAt.Before(metas[j].createdAt)
		}
		return metas[i].id < metas[j].id
	})

	result := make([]any, 0, len(metas))
	for _, meta := range metas {
		item := map[string]any{
			"snapshot_id": meta.id,
		}
		if strings.TrimSpace(meta.rawTime) != "" {
			item["created_at"] = meta.rawTime
		}
		result = append(result, item)
	}
	return result
}

func readRuntimeSnapshotCreatedAt(manifestPath string) string {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return ""
	}

	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		logger.Warn("解析会话快照 manifest 失败: path=%s, err=%v", manifestPath, err)
		return ""
	}

	createdAt, _ := manifest["snapshot_created_at"].(string)
	return strings.TrimSpace(createdAt)
}

func sessionPersistenceExists(sessionDir string) bool {
	for _, name := range []string{"session.json", runtimeSessionSnapshotFileName} {
		path := filepath.Join(sessionDir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}
