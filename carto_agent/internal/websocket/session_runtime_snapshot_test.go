package websocket

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/autocarto/carto_agent/internal/session"
	"github.com/cloudwego/eino/schema"
)

func TestSaveSessionWritesRuntimeSnapshot(t *testing.T) {
	server := &DeepServer{}
	server.SetRuntime(&agentruntime.Runtime{
		ActiveConfigName: "experts-demo",
		Revision:         "rev-20260310",
	})

	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "session-runtime-snapshot"

	sessionData := map[string]any{
		"session_id": sessionID,
		"name":       "测试会话",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
			map[string]any{"role": "assistant", "content": "world"},
		},
		"plan_context": map[string]any{
			"raw_output": "plan-v1",
			"status":     "approved",
		},
		"created_at": "2026-03-10T09:00:00Z",
		"updated_at": "2026-03-10T09:05:00Z",
	}
	if err := server.saveSessionToFile(sessionData, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	baseDir, err := server.getSessionsBaseDir(projectPath)
	if err != nil {
		t.Fatalf("getSessionsBaseDir failed: %v", err)
	}
	snapshotPath := filepath.Join(baseDir, sessionID, runtimeSessionSnapshotFileName)
	snapshot, err := runtimepersist.LoadSessionSnapshotJSON(snapshotPath)
	if err != nil {
		t.Fatalf("LoadSessionSnapshotJSON failed: %v", err)
	}
	if snapshot.SessionID != sessionID {
		t.Fatalf("unexpected session id: %s", snapshot.SessionID)
	}
	if snapshot.ActiveConfigName != "experts-demo" {
		t.Fatalf("unexpected active config: %s", snapshot.ActiveConfigName)
	}
	if snapshot.RuntimeRevision != "rev-20260310" {
		t.Fatalf("unexpected runtime revision: %s", snapshot.RuntimeRevision)
	}
	if len(snapshot.Messages) != 2 {
		t.Fatalf("unexpected message count: %d", len(snapshot.Messages))
	}
	if snapshot.PlanContext == nil || snapshot.PlanContext.RawOutput != "plan-v1" {
		t.Fatalf("unexpected plan context: %#v", snapshot.PlanContext)
	}
}

func TestSaveSessionWritesRuntimeSnapshotPrefersManagedSessionState(t *testing.T) {
	server := &DeepServer{
		sessionManager: session.NewManager(50, time.Hour),
	}
	server.SetRuntime(&agentruntime.Runtime{
		ActiveConfigName: "experts-live",
		Revision:         "rev-live",
	})

	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "session-runtime-live"
	createdAt := time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 3, 10, 10, 5, 0, 0, time.UTC)

	managed := server.sessionManager.GetOrCreate(sessionID)
	managed.Restore([]*schema.Message{
		{
			Role:             schema.Assistant,
			ReasoningContent: "优先保留内存态",
			ToolCalls: []schema.ToolCall{
				{
					ID:   "call-1",
					Type: "function",
					Function: schema.FunctionCall{
						Name:      "write_todos",
						Arguments: `{"items":[{"content":"补持久化测试"}]}`,
					},
				},
			},
		},
	}, &session.PlanContext{RawOutput: "live-plan"}, createdAt, updatedAt)

	sessionData := map[string]any{
		"session_id": sessionID,
		"name":       "测试会话",
		"messages": []any{
			map[string]any{"role": "assistant", "content": "only-session-json"},
		},
		"created_at": createdAt.Format(time.RFC3339),
		"updated_at": updatedAt.Format(time.RFC3339),
	}
	if err := server.saveSessionToFile(sessionData, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	baseDir, err := server.getSessionsBaseDir(projectPath)
	if err != nil {
		t.Fatalf("getSessionsBaseDir failed: %v", err)
	}
	snapshotPath := filepath.Join(baseDir, sessionID, runtimeSessionSnapshotFileName)
	snapshot, err := runtimepersist.LoadSessionSnapshotJSON(snapshotPath)
	if err != nil {
		t.Fatalf("LoadSessionSnapshotJSON failed: %v", err)
	}
	if len(snapshot.Messages) != 1 {
		t.Fatalf("unexpected message count: %d", len(snapshot.Messages))
	}
	if snapshot.Messages[0].ReasoningContent != "优先保留内存态" {
		t.Fatalf("unexpected reasoning content: %q", snapshot.Messages[0].ReasoningContent)
	}
	if len(snapshot.Messages[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(snapshot.Messages[0].ToolCalls))
	}
	if snapshot.Messages[0].ToolCalls[0].Function.Name != "write_todos" {
		t.Fatalf("unexpected tool call name: %s", snapshot.Messages[0].ToolCalls[0].Function.Name)
	}
	if snapshot.PlanContext == nil || snapshot.PlanContext.RawOutput != "live-plan" {
		t.Fatalf("unexpected plan context: %#v", snapshot.PlanContext)
	}
}

func TestLoadSessionFallsBackToRuntimeSnapshotWhenSessionFileMissing(t *testing.T) {
	server := &DeepServer{}
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "session-runtime-fallback-missing"

	sessionData := map[string]any{
		"session_id": sessionID,
		"messages": []any{
			map[string]any{"role": "user", "content": "recover-me"},
			map[string]any{"role": "assistant", "content": "done"},
		},
		"plan_context": map[string]any{
			"raw_output": "plan-missing",
			"status":     "draft",
		},
		"snapshots": []any{
			map[string]any{"snapshot_id": "snap-a", "created_at": "2026-03-10T10:00:00Z"},
		},
		"created_at": "2026-03-10T09:00:00Z",
		"updated_at": "2026-03-10T09:05:00Z",
	}
	if err := server.saveSessionToFile(sessionData, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	baseDir, err := server.getSessionsBaseDir(projectPath)
	if err != nil {
		t.Fatalf("getSessionsBaseDir failed: %v", err)
	}
	sessionDir := filepath.Join(baseDir, sessionID)
	if err := os.Remove(filepath.Join(sessionDir, "session.json")); err != nil {
		t.Fatalf("remove session.json failed: %v", err)
	}

	loaded, err := server.loadSessionFromFile(sessionID, projectPath)
	if err != nil {
		t.Fatalf("loadSessionFromFile failed: %v", err)
	}
	messages, ok := loaded["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("unexpected messages payload: %#v", loaded["messages"])
	}
	planContext, ok := loaded["plan_context"].(map[string]any)
	if !ok || planContext["raw_output"] != "plan-missing" {
		t.Fatalf("unexpected plan context: %#v", loaded["plan_context"])
	}
	snapshots, ok := loaded["snapshots"].([]any)
	if !ok || len(snapshots) != 1 {
		t.Fatalf("unexpected snapshots payload: %#v", loaded["snapshots"])
	}
}

func TestLoadSessionFallsBackToRuntimeSnapshotWhenSessionFileCorrupted(t *testing.T) {
	server := &DeepServer{}
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "session-runtime-fallback-corrupted"

	sessionData := map[string]any{
		"session_id": sessionID,
		"messages": []any{
			map[string]any{"role": "user", "content": "broken-json"},
		},
		"created_at": "2026-03-10T09:00:00Z",
		"updated_at": "2026-03-10T09:05:00Z",
	}
	if err := server.saveSessionToFile(sessionData, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	baseDir, err := server.getSessionsBaseDir(projectPath)
	if err != nil {
		t.Fatalf("getSessionsBaseDir failed: %v", err)
	}
	sessionDir := filepath.Join(baseDir, sessionID)
	if err := os.WriteFile(filepath.Join(sessionDir, "session.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatalf("corrupt session.json failed: %v", err)
	}

	loaded, err := server.loadSessionFromFile(sessionID, projectPath)
	if err != nil {
		t.Fatalf("loadSessionFromFile failed: %v", err)
	}
	messages, ok := loaded["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("unexpected messages payload: %#v", loaded["messages"])
	}
}

func TestSaveSessionAtomicallyFallsBackToRuntimeSnapshot(t *testing.T) {
	server := &DeepServer{}
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "session-runtime-atomic"

	sessionData := map[string]any{
		"session_id": sessionID,
		"name":       "原始名称",
		"messages": []any{
			map[string]any{"role": "user", "content": "persist"},
		},
		"created_at": "2026-03-10T09:00:00Z",
		"updated_at": "2026-03-10T09:05:00Z",
	}
	if err := server.saveSessionToFile(sessionData, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	baseDir, err := server.getSessionsBaseDir(projectPath)
	if err != nil {
		t.Fatalf("getSessionsBaseDir failed: %v", err)
	}
	sessionDir := filepath.Join(baseDir, sessionID)
	if err := os.Remove(filepath.Join(sessionDir, "session.json")); err != nil {
		t.Fatalf("remove session.json failed: %v", err)
	}

	updatedAt := time.Date(2026, 3, 10, 9, 10, 0, 0, time.UTC).Format(time.RFC3339)
	updated, err := server.SaveSessionAtomically(sessionID, projectPath, func(sessionData map[string]any) error {
		sessionData["name"] = "恢复后名称"
		sessionData["updated_at"] = updatedAt
		return nil
	})
	if err != nil {
		t.Fatalf("SaveSessionAtomically failed: %v", err)
	}
	if updated["name"] != "恢复后名称" {
		t.Fatalf("unexpected updated name: %#v", updated["name"])
	}

	restored, err := server.loadSessionFromFile(sessionID, projectPath)
	if err != nil {
		t.Fatalf("loadSessionFromFile after atomic save failed: %v", err)
	}
	if restored["name"] != "恢复后名称" {
		t.Fatalf("unexpected restored name: %#v", restored["name"])
	}
	if restored["updated_at"] != updatedAt {
		t.Fatalf("unexpected restored updated_at: %#v", restored["updated_at"])
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "session.json")); err != nil {
		t.Fatalf("session.json should be recreated: %v", err)
	}
}

func TestSaveSessionSynchronizesManagedSessionAfterClearHistory(t *testing.T) {
	server := &DeepServer{
		sessionManager: session.NewManager(50, time.Hour),
	}

	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "session-runtime-sync"

	managed := server.sessionManager.GetOrCreate(sessionID)
	managed.AddMessage(schema.UserMessage("stale-user"))
	managed.AddMessage(schema.AssistantMessage("stale-assistant", nil))
	managed.SetPlanContext(&session.PlanContext{RawOutput: "stale-plan"})

	sessionData := map[string]any{
		"session_id": sessionID,
		"messages":   []any{},
		"created_at": "2026-03-10T09:00:00Z",
		"updated_at": "2026-03-10T09:20:00Z",
	}
	if err := server.saveSessionToFile(sessionData, projectPath, sessionSaveOperation{Type: sessionSaveOpClearHistory}); err != nil {
		t.Fatalf("saveSessionToFile clear_history failed: %v", err)
	}

	got := server.sessionManager.Get(sessionID)
	if got == nil {
		t.Fatal("expected managed session")
	}
	if got.MessageCount() != 0 {
		t.Fatalf("expected managed session messages cleared, got=%d", got.MessageCount())
	}
	if got.GetPlanContext() != nil {
		t.Fatalf("expected managed session plan context cleared, got=%#v", got.GetPlanContext())
	}
}

func TestRestoreManagedSessionFromSessionDataReplacesExistingState(t *testing.T) {
	server := &DeepServer{
		sessionManager: session.NewManager(50, time.Hour),
	}

	sessionID := "session-runtime-restore"
	managed := server.sessionManager.GetOrCreate(sessionID)
	managed.AddMessage(schema.UserMessage("stale"))
	managed.SetPlanContext(&session.PlanContext{RawOutput: "stale-plan"})

	sessionData := map[string]any{
		"session_id": sessionID,
		"messages": []any{
			map[string]any{
				"role":              "assistant",
				"reasoning_content": "从磁盘恢复",
				"tool_calls": []any{
					map[string]any{
						"id":   "call-restore",
						"type": "function",
						"function": map[string]any{
							"name":      "task",
							"arguments": `{"prompt":"恢复任务"}`,
						},
					},
				},
			},
		},
		"plan_context": map[string]any{
			"raw_output": "restored-plan",
			"status":     "approved",
		},
		"created_at": "2026-03-10T11:00:00Z",
		"updated_at": "2026-03-10T11:10:00Z",
	}

	server.restoreManagedSessionFromSessionData(sessionData)

	got := server.sessionManager.Get(sessionID)
	if got == nil {
		t.Fatal("expected managed session")
	}
	msgs := got.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ReasoningContent != "从磁盘恢复" {
		t.Fatalf("unexpected reasoning content: %q", msgs[0].ReasoningContent)
	}
	if len(msgs[0].ToolCalls) != 1 || msgs[0].ToolCalls[0].Function.Name != "task" {
		t.Fatalf("unexpected restored tool calls: %#v", msgs[0].ToolCalls)
	}
	if got.GetPlanContext() == nil || got.GetPlanContext().RawOutput != "restored-plan" {
		t.Fatalf("unexpected restored plan context: %#v", got.GetPlanContext())
	}
}

func TestDescribeSessionDataRestoreSource(t *testing.T) {
	cases := []struct {
		name              string
		sessionFileLoaded bool
		projectionMerged  bool
		want              string
	}{
		{name: "session file", sessionFileLoaded: true, projectionMerged: false, want: "session_file"},
		{name: "runtime snapshot fallback", sessionFileLoaded: false, projectionMerged: false, want: "runtime_snapshot_fallback"},
		{name: "runtime snapshot projection merge", sessionFileLoaded: false, projectionMerged: true, want: "runtime_snapshot_projection_merge"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeSessionDataRestoreSource(tc.sessionFileLoaded, tc.projectionMerged); got != tc.want {
				t.Fatalf("unexpected source: got=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestBuildSessionDataRestoreObservationUsesStableInfoLevel(t *testing.T) {
	cases := []struct {
		name              string
		sessionFileLoaded bool
		projectionMerged  bool
		wantSource        string
	}{
		{name: "session file", sessionFileLoaded: true, projectionMerged: false, wantSource: "session_file"},
		{name: "runtime snapshot fallback", sessionFileLoaded: false, projectionMerged: false, wantSource: "runtime_snapshot_fallback"},
		{name: "runtime snapshot projection merge", sessionFileLoaded: false, projectionMerged: true, wantSource: "runtime_snapshot_projection_merge"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs := buildSessionDataRestoreObservation("/tmp/session", "/tmp/session/runtime_snapshot.json", tc.sessionFileLoaded, tc.projectionMerged, errors.New("primary"))
			if obs.Source != tc.wantSource {
				t.Fatalf("unexpected source: got=%s want=%s", obs.Source, tc.wantSource)
			}
			if obs.LogLevel != logger.INFO {
				t.Fatalf("expected info level, got=%v", obs.LogLevel)
			}
			if obs.SessionDir == "" {
				t.Fatal("expected session dir")
			}
		})
	}
}
