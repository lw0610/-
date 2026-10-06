package runtimepersist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/autocarto/carto_agent/internal/session"
	"github.com/cloudwego/eino/schema"
)

const sessionSnapshotVersion = 1

// SessionRuntimeProjection 运行时投影视图，用于会话恢复与 UI 重连接管。
type SessionRuntimeProjection struct {
	RuntimeStatus     string           `json:"runtime_status,omitempty"`
	RunID             string           `json:"run_id,omitempty"`
	ExecutionMode     string           `json:"execution_mode,omitempty"`
	Phase             string           `json:"phase,omitempty"`
	BoundInstanceID   string           `json:"bound_instance_id,omitempty"`
	BoundProjectPath  string           `json:"bound_project_path,omitempty"`
	CheckpointID      string           `json:"checkpoint_id,omitempty"`
	RuntimeRevision   string           `json:"runtime_revision,omitempty"`
	ActiveAgentConfig string           `json:"active_agent_config,omitempty"`
	LastError         string           `json:"last_error,omitempty"`
	FailureCode       string           `json:"failure_code,omitempty"`
	FailureReason     string           `json:"failure_reason,omitempty"`
	UpdatedAt         string           `json:"updated_at,omitempty"`
	RuntimeSummary    string           `json:"runtime_summary,omitempty"`
	WorkflowBlocks    []map[string]any `json:"workflow_blocks,omitempty"`
	Tasks             []map[string]any `json:"tasks,omitempty"`
}

// SessionSnapshot 可序列化的会话快照。
type SessionSnapshot struct {
	Version           int                       `json:"version"`
	SessionID         string                    `json:"session_id"`
	ActiveConfigName  string                    `json:"active_config_name,omitempty"`
	RuntimeRevision   string                    `json:"runtime_revision,omitempty"`
	Messages          []*schema.Message         `json:"messages,omitempty"`
	PlanContext       *session.PlanContext      `json:"plan_context,omitempty"`
	RuntimeProjection *SessionRuntimeProjection `json:"runtime_projection,omitempty"`
	CreatedAt         time.Time                 `json:"created_at"`
	LastActiveAt      time.Time                 `json:"last_active_at"`
}

// CaptureSessionSnapshot 捕获当前会话状态。
func CaptureSessionSnapshot(sess *session.Session, activeConfigName, runtimeRevision string) *SessionSnapshot {
	if sess == nil {
		return nil
	}

	return &SessionSnapshot{
		Version:          sessionSnapshotVersion,
		SessionID:        sess.ID,
		ActiveConfigName: activeConfigName,
		RuntimeRevision:  runtimeRevision,
		Messages:         cloneMessages(sess.GetMessages()),
		PlanContext:      sess.GetPlanContext(),
		CreatedAt:        sess.CreatedAt,
		LastActiveAt:     sess.LastActiveAt,
	}
}

// RestoreSession 用快照恢复出新的 Session。
func (s *SessionSnapshot) RestoreSession(maxMessages int) *session.Session {
	if s == nil {
		return nil
	}

	restored := session.NewSession(s.SessionID, maxMessages)
	if len(s.Messages) > 0 {
		restored.AddMessages(cloneMessages(s.Messages))
	}
	if s.PlanContext != nil {
		restored.SetPlanContext(s.PlanContext)
	}
	restored.CreatedAt = s.CreatedAt
	restored.LastActiveAt = s.LastActiveAt
	return restored
}

// SaveJSON 保存快照到文件。
func (s *SessionSnapshot) SaveJSON(path string) error {
	if s == nil {
		return fmt.Errorf("session snapshot is nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0o644)
}

// LoadSessionSnapshotJSON 读取会话快照。
func LoadSessionSnapshotJSON(path string) (*SessionSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var snapshot SessionSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func cloneMessages(messages []*schema.Message) []*schema.Message {
	if len(messages) == 0 {
		return nil
	}

	raw, err := json.Marshal(messages)
	if err != nil {
		out := make([]*schema.Message, len(messages))
		copy(out, messages)
		return out
	}

	var cloned []*schema.Message
	if err := json.Unmarshal(raw, &cloned); err != nil {
		out := make([]*schema.Message, len(messages))
		copy(out, messages)
		return out
	}
	return cloned
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
