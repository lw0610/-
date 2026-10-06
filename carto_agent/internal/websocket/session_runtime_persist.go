package websocket

import (
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/logger"
)

func (s *DeepServer) persistManagedSessionState(sessionID, projectPath string, mode executionMode) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	projection := s.sessionRuntimeProjectionSnapshot(sid, nil)
	if strings.TrimSpace(projectPath) == "" {
		return
	}
	if mode == "" {
		mode = parseExecutionModeOrDefault(projection.RuntimeState.ExecutionMode)
	}

	_, err := s.SaveSessionAtomically(sid, projectPath, func(sessionData map[string]any) error {
		snapshot, err := s.captureRuntimeSessionSnapshot(sessionData)
		if err != nil {
			return err
		}
		sessionData["messages"] = encodeRuntimeSnapshotMessages(snapshot.Messages)
		planContext, err := encodePlanContext(snapshot.PlanContext)
		if err != nil {
			return err
		}
		if planContext != nil {
			sessionData[sessionPlanContextKey] = planContext
		} else {
			delete(sessionData, sessionPlanContextKey)
		}
		if strings.TrimSpace(projectPath) != "" {
			sessionData["project_path"] = strings.TrimSpace(projectPath)
		}
		if strings.TrimSpace(string(mode)) != "" {
			sessionData["execution_mode"] = strings.TrimSpace(string(mode))
			sessionData["run_intent"] = map[executionMode]string{
				executionModePlan:  "plan_only",
				executionModeBuild: "execute",
			}[mode]
		}
		stats := buildContextStatsSnapshot(snapshot.Messages, 0)
		existingContextStats := asMap(sessionData["context_stats"])
		compressedCount := stats.CompressedCount
		if existingContextStats != nil {
			if value, ok := existingContextStats["compressed_count"].(float64); ok {
				compressedCount = int(value)
			}
			if value, ok := existingContextStats["compressed_count"].(int); ok {
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
		applyProjectionToSessionData(sessionData, projection)
		sessionData["updated_at"] = time.Now().Format(time.RFC3339)
		return nil
	})
	if err != nil {
		logger.Warn("同步运行中会话到持久化存储失败: session_id=%s err=%v", sid, err)
	}
}
