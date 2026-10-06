package websocket

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
)

func TestProcessTaskUnavailableAgentPersistsFailureWithoutStartingRun(t *testing.T) {
	for _, mode := range []executionMode{executionModeBuild, executionModePlan} {
		t.Run(string(mode), func(t *testing.T) {
			t.Setenv("AUTOCARTO_HOME", t.TempDir())
			server := NewDeepServer(0, nil)
			projectPath := filepath.Join(t.TempDir(), "statistics.qgz")
			sessionID := "preflight-" + string(mode)
			server.PrimeRuntimeState(middlewareclient.RuntimeSnapshot{
				Instances: middlewareclient.InstancesSnapshot{
					ActiveID: "inst-preflight", InstanceCount: 1, RunningCount: 1,
					Items: []*discovery.QGISInstance{{ID: "inst-preflight", ProjectPath: projectPath, Status: "running"}},
				},
			}, nil)
			if err := server.saveSessionToFile(map[string]any{
				"session_id": sessionID, "project_path": projectPath,
				"messages": []any{}, "name": "preflight regression",
				// A new failed request must not inherit the old run identity.
				"runtime_status": sessionRuntimeStatusCompleted, "run_id": "old-run", "checkpoint_id": "old-checkpoint",
			}, projectPath, sessionSaveOperation{}); err != nil {
				t.Fatal(err)
			}

			// No websocket subscriber: reproduces losing the live error frame.
			server.processTask(context.Background(), nil, Message{
				Type: "chat", SessionID: sessionID, Content: "制作统计专题图",
				Data: map[string]any{"execution_mode": string(mode), "project_path": projectPath, "instance_id": "inst-preflight"},
			})

			if _, active := server.activeSessionRuntime(sessionID); active {
				t.Fatal("unavailable agent must not create an active run")
			}
			state := server.sessionRuntimeProjectionSnapshot(sessionID, nil).RuntimeState
			if state.Status != sessionRuntimeStatusFailed || state.FailureCode != ErrorCodeAgentUnavailable || state.BoundProjectPath != projectPath {
				t.Fatalf("missing bound failure state: %+v", state)
			}
			if state.RunID != "" || state.CheckpointID != "" {
				t.Fatalf("preflight failure invented or reused run identity: %+v", state)
			}
			baseDir, err := server.getSessionsBaseDir(projectPath)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(baseDir, sessionID, "session.json"))
			if err != nil {
				t.Fatal(err)
			}
			var persisted map[string]any
			if err := json.Unmarshal(raw, &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted["runtime_status"] != sessionRuntimeStatusFailed || persisted["runtime_failure_code"] != ErrorCodeAgentUnavailable {
				t.Fatalf("failure was not saved to the project session: status=%v code=%v", persisted["runtime_status"], persisted["runtime_failure_code"])
			}
			if _, exists := persisted["run_id"]; exists {
				t.Fatal("old run_id survived the preflight failure")
			}
			if blocks := server.sessionRuntimeProjectionSnapshot(sessionID, nil).WorkflowBlocks; len(blocks) != 1 || blocks[0].Status != "error" {
				t.Fatalf("preflight thinking block must finish as error: %+v", blocks)
			}
			for _, event := range server.replaySessionRunEvents(sessionID, 1) {
				payload, _ := event.Data["payload"].(map[string]any)
				if event.Data["source_type"] == MsgTypeTaskRunningChange && payload["running"] == true {
					t.Fatal("unavailable agent emitted a running=true event")
				}
			}

			// Reconnect with no sequence cursor must recover the failed snapshot;
			// the existing zero-cursor event replay policy remains unchanged.
			bundle := server.buildSessionAttachmentBundle(sessionID, 0)
			if !bundle.SnapshotHit || bundle.RuntimeStatePushed || bundle.ReplayCount != 0 || len(bundle.Messages) != 1 {
				t.Fatalf("expected failed snapshot without an active run or replay: %+v", bundle)
			}
			payload, _ := bundle.Messages[0].Data["payload"].(map[string]any)
			if payload["runtime_status"] != sessionRuntimeStatusFailed || payload["runtime_failure_code"] != ErrorCodeAgentUnavailable {
				t.Fatalf("reattach lost failure status: %+v", payload)
			}
			if !strings.Contains(stringValue(payload["runtime_failure_reason"]), "未初始化") {
				t.Fatalf("reattach lost the human-readable error: %+v", payload)
			}
		})
	}
}
