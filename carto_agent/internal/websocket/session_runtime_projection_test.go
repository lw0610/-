package websocket

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/gorilla/websocket"
)

func TestSaveSessionWritesRuntimeProjectionAndFallbackRestoresIt(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "session-runtime-projection"

	sessionData := map[string]any{
		"session_id": sessionID,
		"name":       "运行态投影测试",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"workflow_blocks": []any{
			map[string]any{
				"id":               "wf-1",
				"agent":            "planner",
				"agentDisplayName": "规划智能体",
				"summary":          "正在生成执行计划",
				"status":           "running",
				"startedAt":        float64(1710000000000),
				"updatedAt":        float64(1710000001000),
				"entries": []any{
					map[string]any{
						"id":        "entry-1",
						"type":      "thinking",
						"content":   "先分析任务边界",
						"phase":     "thinking",
						"timestamp": float64(1710000001000),
					},
				},
			},
		},
		"runtime_status":         sessionRuntimeStatusRunning,
		"run_id":                 "run-1",
		"runtime_phase":          "planning",
		"bound_instance_id":      "inst-a",
		"bound_project_path":     projectPath,
		"runtime_revision":       "rev-projection",
		"active_agent_config":    "default",
		"runtime_summary":        "正在生成执行计划",
		"runtime_failure_code":   "",
		"runtime_failure_reason": "",
		"created_at":             "2026-03-12T10:00:00Z",
		"updated_at":             "2026-03-12T10:01:00Z",
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
	if snapshot.RuntimeProjection == nil {
		t.Fatal("expected runtime projection to be saved into runtime snapshot")
	}
	if snapshot.RuntimeProjection.RuntimeStatus != sessionRuntimeStatusRunning {
		t.Fatalf("unexpected runtime status: %q", snapshot.RuntimeProjection.RuntimeStatus)
	}
	if snapshot.RuntimeProjection.RuntimeSummary != "正在生成执行计划" {
		t.Fatalf("unexpected runtime summary: %q", snapshot.RuntimeProjection.RuntimeSummary)
	}
	if len(snapshot.RuntimeProjection.WorkflowBlocks) != 1 {
		t.Fatalf("expected 1 workflow block, got %d", len(snapshot.RuntimeProjection.WorkflowBlocks))
	}

	sessionDir := filepath.Join(baseDir, sessionID)
	if err := os.Remove(filepath.Join(sessionDir, "session.json")); err != nil {
		t.Fatalf("remove session.json failed: %v", err)
	}

	loaded, err := server.loadSessionFromFile(sessionID, projectPath)
	if err != nil {
		t.Fatalf("loadSessionFromFile failed: %v", err)
	}
	if loaded["runtime_status"] != sessionRuntimeStatusRunning {
		t.Fatalf("unexpected restored runtime_status: %#v", loaded["runtime_status"])
	}
	if loaded["runtime_summary"] != "正在生成执行计划" {
		t.Fatalf("unexpected restored runtime_summary: %#v", loaded["runtime_summary"])
	}
	blocks, ok := loaded["workflow_blocks"].([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("unexpected restored workflow_blocks: %#v", loaded["workflow_blocks"])
	}
}

func TestBuildSessionAttachmentMessagesOrdersSnapshotRuntimeAndReplay(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "demo.qgz")
	sessionID := "session-attach-order"

	if err := server.saveSessionToFile(map[string]any{
		"session_id":      sessionID,
		"name":            "Attach 顺序测试",
		"messages":        []any{map[string]any{"role": "user", "content": "hello attach"}},
		"workflow_blocks": []any{map[string]any{"id": "wf-attach", "agent": "planner", "agentDisplayName": "规划智能体", "summary": "attach snapshot", "status": "running", "startedAt": float64(1710000000000), "updatedAt": float64(1710000001000), "entries": []any{}}},
		"runtime_status":  sessionRuntimeStatusRunning,
		"runtime_summary": "attach snapshot",
		"created_at":      "2026-03-12T10:00:00Z",
		"updated_at":      "2026-03-12T10:01:00Z",
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	server.markSessionRuntimeRunning(sessionID, "run-attach", executionModeBuild, "building", projectPath, "inst-a")
	first, ok := buildRunEventMessageWithOptions(Message{Type: MsgTypeStatus, SessionID: sessionID, Content: "first"}, runEventOptions{Sequence: 1, TimestampMillis: 1000})
	if !ok {
		t.Fatal("expected first run_event")
	}
	second, ok := buildRunEventMessageWithOptions(Message{Type: MsgTypeStatus, SessionID: sessionID, Content: "second"}, runEventOptions{Sequence: 2, TimestampMillis: 2000})
	if !ok {
		t.Fatal("expected second run_event")
	}
	server.recordSessionRunEvent(sessionID, first)
	server.recordSessionRunEvent(sessionID, second)

	messages := server.buildSessionAttachmentMessages(sessionID, 1)
	if len(messages) != 3 {
		t.Fatalf("expected 3 attachment messages, got %d", len(messages))
	}
	if messages[0].Type != MsgTypeRunEvent {
		t.Fatalf("expected first message to be run_event snapshot, got %q", messages[0].Type)
	}
	firstData, _ := messages[0].Data["payload"].(map[string]any)
	if event := messages[0].Data["event"]; event != "session_state" {
		t.Fatalf("expected first event session_state, got %#v", event)
	}
	if firstData["session_id"] != sessionID {
		t.Fatalf("unexpected session snapshot payload: %#v", firstData)
	}
	if _, ok := firstData["workflow_blocks"].([]any); !ok {
		t.Fatalf("expected session snapshot payload to include workflow_blocks, got %#v", firstData["workflow_blocks"])
	}
	if messages[1].Type != MsgTypeSessionRuntimeState {
		t.Fatalf("expected second message to be session_runtime_state, got %q", messages[1].Type)
	}
	if messages[2].Type != MsgTypeRunEvent {
		t.Fatalf("expected third message to be replay run_event, got %q", messages[2].Type)
	}
	if got := parseUint64Like(messages[2].Data["seq"]); got != 2 {
		t.Fatalf("expected replay seq=2, got %d", got)
	}
}

func TestTaskListUpdatePersistsTasksAndCompletesWriteTodosProjection(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "todos.qgz")
	sessionID := "session-task-projection"

	if err := server.saveSessionToFile(map[string]any{
		"session_id":   sessionID,
		"name":         "任务恢复测试",
		"messages":     []any{},
		"created_at":   "2026-03-19T10:00:00Z",
		"updated_at":   "2026-03-19T10:00:00Z",
		"project_path": projectPath,
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	server.markSessionRuntimeRunning(sessionID, "run-task", executionModeBuild, "building", projectPath, "inst-a")
	if !server.applySessionWorkflowMessage(Message{
		Type:      MsgTypeAgentWorkflow,
		SessionID: sessionID,
		Phase:     "tool_call",
		Content:   "调用工具: write_todos",
		Data: map[string]any{
			"tool":   "write_todos",
			"status": "running",
		},
	}) {
		t.Fatal("expected tool_call to update runtime projection")
	}
	if !server.applySessionWorkflowMessage(Message{
		Type:      MsgTypeTaskListUpdate,
		SessionID: sessionID,
		Data: map[string]any{
			"todos": []any{
				map[string]any{"content": "恢复任务列表", "status": "in_progress"},
			},
		},
	}) {
		t.Fatal("expected task_list_update to update runtime projection")
	}

	server.persistSessionRuntimeProjection(sessionID)
	loaded, err := server.loadSessionFromFile(sessionID, projectPath)
	if err != nil {
		t.Fatalf("loadSessionFromFile failed: %v", err)
	}

	tasks, ok := loaded["tasks"].([]any)
	if !ok || len(tasks) != 1 {
		t.Fatalf("expected persisted tasks, got %#v", loaded["tasks"])
	}
	blocks, ok := loaded["workflow_blocks"].([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("expected workflow blocks, got %#v", loaded["workflow_blocks"])
	}
	block := blocks[0].(map[string]any)
	entries := block["entries"].([]any)
	entry := entries[0].(map[string]any)
	toolCard := entry["toolCard"].(map[string]any)
	if toolCard["status"] != "completed" {
		t.Fatalf("expected write_todos status completed, got %#v", toolCard["status"])
	}
	if toolCard["statusSource"] != "task_list_update" {
		t.Fatalf("expected task_list_update status source, got %#v", toolCard["statusSource"])
	}
	success, ok := toolCard["success"].(bool)
	if !ok || !success {
		t.Fatalf("expected write_todos success=true, got %#v", toolCard["success"])
	}
}

func TestTaskListUpdateIgnoresLateUnknownWriteTodosToolResult(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, "todos.qgz")
	sessionID := "session-task-projection-late-tool-result"

	if err := server.saveSessionToFile(map[string]any{
		"session_id":   sessionID,
		"name":         "任务恢复测试",
		"messages":     []any{},
		"created_at":   "2026-03-19T10:00:00Z",
		"updated_at":   "2026-03-19T10:00:00Z",
		"project_path": projectPath,
	}, projectPath, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile failed: %v", err)
	}

	server.markSessionRuntimeRunning(sessionID, "run-task", executionModeBuild, "building", projectPath, "inst-a")
	if !server.applySessionWorkflowMessage(Message{
		Type:      MsgTypeAgentWorkflow,
		SessionID: sessionID,
		Phase:     "tool_call",
		Content:   "调用工具: write_todos",
		Data: map[string]any{
			"tool":   "write_todos",
			"status": "running",
		},
	}) {
		t.Fatal("expected tool_call to update runtime projection")
	}
	if !server.applySessionWorkflowMessage(Message{
		Type:      MsgTypeTaskListUpdate,
		SessionID: sessionID,
		Data: map[string]any{
			"todos": []any{
				map[string]any{"content": "恢复任务列表", "status": "in_progress"},
			},
		},
	}) {
		t.Fatal("expected task_list_update to update runtime projection")
	}
	if !server.applySessionWorkflowMessage(Message{
		Type:      MsgTypeAgentWorkflow,
		SessionID: sessionID,
		Phase:     "tool_result",
		Content:   "工具 write_todos 执行完成（状态未判定）",
		Data: map[string]any{
			"tool":          "write_todos",
			"status":        "unknown",
			"status_source": "unknown",
			"result":        "Updated todo list to [{\"content\":\"恢复任务列表\",\"status\":\"in_progress\"}]",
		},
	}) {
		t.Fatal("expected tool_result to update runtime projection")
	}

	server.persistSessionRuntimeProjection(sessionID)
	loaded, err := server.loadSessionFromFile(sessionID, projectPath)
	if err != nil {
		t.Fatalf("loadSessionFromFile failed: %v", err)
	}

	blocks, ok := loaded["workflow_blocks"].([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("expected workflow blocks, got %#v", loaded["workflow_blocks"])
	}
	block := blocks[0].(map[string]any)
	entries := block["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected single reconciled write_todos entry, got %#v", entries)
	}
	entry := entries[0].(map[string]any)
	toolCard := entry["toolCard"].(map[string]any)
	if toolCard["status"] != "completed" {
		t.Fatalf("expected write_todos status completed, got %#v", toolCard["status"])
	}
	if toolCard["statusSource"] != "task_list_update" {
		t.Fatalf("expected task_list_update status source, got %#v", toolCard["statusSource"])
	}
	success, ok := toolCard["success"].(bool)
	if !ok || !success {
		t.Fatalf("expected write_todos success=true, got %#v", toolCard["success"])
	}
}

func TestCleanupClientConnectionKeepsOtherWindowSubscription(t *testing.T) {
	server := NewDeepServer(0, nil)
	connA := &websocket.Conn{}
	connB := &websocket.Conn{}
	server.connWriteMu[connA] = &sync.Mutex{}
	server.connWriteMu[connB] = &sync.Mutex{}
	server.attachSessionSubscription(connA, "sid-shared")
	server.attachSessionSubscription(connB, "sid-shared")

	server.cleanupClientConnection(connA, false)

	subscribers := server.listSessionSubscribers("sid-shared")
	if len(subscribers) != 1 {
		t.Fatalf("expected exactly 1 subscriber after cleaning one window, got %d", len(subscribers))
	}
	if subscribers[0] != connB {
		t.Fatalf("expected remaining subscriber to be connB")
	}
}

func TestFailSessionsBoundToDisconnectedInstanceOnlyAffectsBoundSession(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPathA := filepath.Join(projectRoot, "a.qgz")
	projectPathB := filepath.Join(projectRoot, "b.qgz")

	for _, item := range []struct {
		sessionID   string
		projectPath string
		instanceID  string
	}{
		{sessionID: "sid-a", projectPath: projectPathA, instanceID: "inst-a"},
		{sessionID: "sid-b", projectPath: projectPathB, instanceID: "inst-b"},
	} {
		if err := server.saveSessionToFile(map[string]any{
			"session_id":   item.sessionID,
			"name":         item.sessionID,
			"messages":     []any{},
			"created_at":   time.Now().Add(-time.Minute).Format(time.RFC3339),
			"updated_at":   time.Now().Format(time.RFC3339),
			"project_path": item.projectPath,
		}, item.projectPath, sessionSaveOperation{}); err != nil {
			t.Fatalf("saveSessionToFile(%s) failed: %v", item.sessionID, err)
		}
	}

	server.markSessionRuntimeRunning("sid-a", "run-a", executionModeBuild, "building", projectPathA, "inst-a")
	server.markSessionRuntimeRunning("sid-b", "run-b", executionModeBuild, "building", projectPathB, "inst-b")
	server.markSessionRuntimeInterrupted("sid-a", "cp-a")
	server.markSessionRuntimeInterrupted("sid-b", "cp-b")

	server.failSessionsBoundToDisconnectedInstance("inst-a", projectPathA)

	if server.hasActiveSessionRuntime("sid-a") {
		t.Fatal("expected sid-a to be failed and removed from active runtimes")
	}
	if !server.hasActiveSessionRuntime("sid-b") {
		t.Fatal("expected sid-b to remain active")
	}

	loadedA, err := server.loadSessionFromFile("sid-a", projectPathA)
	if err != nil {
		t.Fatalf("loadSessionFromFile sid-a failed: %v", err)
	}
	if loadedA["runtime_status"] != sessionRuntimeStatusFailed {
		t.Fatalf("unexpected sid-a runtime_status: %#v", loadedA["runtime_status"])
	}
	if loadedA["runtime_failure_code"] != ErrorCodeBoundInstanceDisconnected {
		t.Fatalf("unexpected sid-a failure code: %#v", loadedA["runtime_failure_code"])
	}

	loadedB, err := server.loadSessionFromFile("sid-b", projectPathB)
	if err != nil {
		t.Fatalf("loadSessionFromFile sid-b failed: %v", err)
	}
	if status := loadedB["runtime_status"]; status == sessionRuntimeStatusFailed {
		t.Fatalf("expected sid-b not to be marked failed, got %#v", status)
	}
}

func TestApplyMiddlewareRuntimeSnapshotOnlyFailsSessionsBoundToRemovedInstance(t *testing.T) {
	server := NewDeepServer(0, nil)
	projectRoot := t.TempDir()
	projectPathA := filepath.Join(projectRoot, "a.qgz")
	projectPathB := filepath.Join(projectRoot, "b.qgz")

	if err := server.saveSessionToFile(map[string]any{
		"session_id":   "sid-a-interrupted",
		"name":         "sid-a-interrupted",
		"messages":     []any{},
		"created_at":   time.Now().Add(-time.Minute).Format(time.RFC3339),
		"updated_at":   time.Now().Format(time.RFC3339),
		"project_path": projectPathA,
	}, projectPathA, sessionSaveOperation{}); err != nil {
		t.Fatalf("saveSessionToFile sid-a-interrupted failed: %v", err)
	}

	server.PrimeRuntimeState(middlewareclient.RuntimeSnapshot{
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "inst-a",
			InstanceCount: 2,
			RunningCount:  2,
			Items: []*discovery.QGISInstance{
				{ID: "inst-a", ProjectPath: projectPathA, Status: "running"},
				{ID: "inst-b", ProjectPath: projectPathB, Status: "running"},
			},
		},
	}, nil)

	cancelRunning := 0
	server.markSessionRuntimeRunning("sid-a-running", "run-a", executionModeBuild, "building", projectPathA, "inst-a")
	server.setSessionTaskCancel(nil, "sid-a-running", func() { cancelRunning++ })

	server.markSessionRuntimeRunning("sid-a-interrupted", "run-a2", executionModeBuild, "building", projectPathA, "inst-a")
	server.markSessionRuntimeInterrupted("sid-a-interrupted", "cp-a")

	server.markSessionRuntimeRunning("sid-b-running", "run-b", executionModeBuild, "building", projectPathB, "inst-b")

	server.PrimeRuntimeState(middlewareclient.RuntimeSnapshot{
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "inst-b",
			InstanceCount: 1,
			RunningCount:  1,
			Items:         []*discovery.QGISInstance{{ID: "inst-b", ProjectPath: projectPathB, Status: "running"}},
		},
	}, nil)

	if cancelRunning != 1 {
		t.Fatalf("expected running sid-a session to be cancelled once, got %d", cancelRunning)
	}
	message, code, failureReason, details := server.resolveSessionRuntimeFailure("sid-a-running", errors.New("fallback"))
	if code != ErrorCodeBoundInstanceDisconnected {
		t.Fatalf("unexpected running failure code: %q", code)
	}
	if message == "" || failureReason == "" {
		t.Fatalf("expected running failure message and reason, got message=%q reason=%q", message, failureReason)
	}
	if details.Code != "" || details.Retryable != nil || len(details.ProviderDiagnostics) != 0 {
		t.Fatalf("expected no tool details for bound instance failure override, got %#v", details)
	}
	if !server.hasActiveSessionRuntime("sid-b-running") {
		t.Fatal("expected inst-b session to remain active")
	}
	if server.hasActiveSessionRuntime("sid-a-interrupted") {
		t.Fatal("expected interrupted inst-a session to be failed and removed")
	}

	loadedA, err := server.loadSessionFromFile("sid-a-interrupted", projectPathA)
	if err != nil {
		t.Fatalf("loadSessionFromFile sid-a-interrupted failed: %v", err)
	}
	if loadedA["runtime_status"] != sessionRuntimeStatusFailed {
		t.Fatalf("unexpected interrupted runtime_status: %#v", loadedA["runtime_status"])
	}
	if loadedA["runtime_failure_code"] != ErrorCodeBoundInstanceDisconnected {
		t.Fatalf("unexpected interrupted failure code: %#v", loadedA["runtime_failure_code"])
	}
}
