package websocket

import (
	"sync"
	"testing"

	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/gorilla/websocket"
)

func TestCleanupClientConnectionKeepsSessionScopedRuntimeState(t *testing.T) {
	server := NewDeepServer(0, nil)
	conn := &websocket.Conn{}
	server.connWriteMu[conn] = &sync.Mutex{}
	server.attachSessionSubscription(conn, "sid-1")

	cancelCalls := 0
	server.setSessionTaskCancel(conn, "sid-1", func() {
		cancelCalls++
	})
	server.setPendingInterrupt(conn, "sid-1", &PendingInterrupt{
		CheckpointID: "cp-1",
		SessionID:    "sid-1",
		ProjectPath:  "/tmp/demo.qgz",
	})
	server.markSessionRuntimeRunning("sid-1", "run-1", executionModeBuild, "building", "/tmp/demo.qgz", "inst-1")

	server.cleanupClientConnection(conn, false)

	if cancelCalls != 0 {
		t.Fatalf("cleanup should not cancel active task, got %d", cancelCalls)
	}
	if !server.hasActiveSessionRuntime("sid-1") {
		t.Fatal("expected active session runtime state to remain after connection cleanup")
	}
	if _, ok := server.getPendingInterrupt("sid-1"); !ok {
		t.Fatal("expected pending interrupt to remain after connection cleanup")
	}
	if subscribers := server.listSessionSubscribers("sid-1"); len(subscribers) != 0 {
		t.Fatalf("expected cleanup to remove session subscriptions, got %d", len(subscribers))
	}
}

func TestCancelSessionTaskUsesSessionScopedCancel(t *testing.T) {
	server := NewDeepServer(0, nil)
	conn := &websocket.Conn{}

	cancelCalls := 0
	server.setSessionTaskCancel(conn, "sid-2", func() {
		cancelCalls++
	})

	server.cancelSessionTask(nil, "sid-2")

	if cancelCalls != 1 {
		t.Fatalf("expected cancel to be invoked once, got %d", cancelCalls)
	}
}

func TestResolveSessionBindingReportsReasonChain(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.PrimeRuntimeState(middlewareclient.RuntimeSnapshot{
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "inst-b",
			InstanceCount: 4,
			RunningCount:  3,
			Items: []*discovery.QGISInstance{
				{ID: "inst-a", ProjectPath: "/tmp/project-a.qgz", Status: "running"},
				{ID: "inst-b", ProjectPath: "/tmp/shared.qgz", Status: "running"},
				{ID: "inst-c", ProjectPath: "/tmp/shared.qgz", Status: "running"},
				{ID: "inst-d", ProjectPath: "/tmp/stopped.qgz", Status: "stopped"},
			},
		},
	}, nil)

	tests := []struct {
		name               string
		explicitInstanceID string
		projectPath        string
		wantInstanceID     string
		wantProjectPath    string
		wantReason         string
	}{
		{
			name:               "explicit instance",
			explicitInstanceID: "inst-a",
			projectPath:        "/tmp/requested.qgz",
			wantInstanceID:     "inst-a",
			wantProjectPath:    "/tmp/project-a.qgz",
			wantReason:         "explicit_instance",
		},
		{
			name:            "project match prefers active instance",
			projectPath:     "/tmp/shared.qgz",
			wantInstanceID:  "inst-b",
			wantProjectPath: "/tmp/shared.qgz",
			wantReason:      "project_match",
		},
		{
			name:            "active fallback",
			projectPath:     "/tmp/unknown.qgz",
			wantInstanceID:  "inst-b",
			wantProjectPath: "/tmp/shared.qgz",
			wantReason:      "active_fallback",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			binding, err := server.resolveSessionBinding(tc.explicitInstanceID, tc.projectPath)
			if err != nil {
				t.Fatalf("resolveSessionBinding failed: %v", err)
			}
			if binding.InstanceID != tc.wantInstanceID {
				t.Fatalf("unexpected instance id: got %q want %q", binding.InstanceID, tc.wantInstanceID)
			}
			if binding.ProjectPath != tc.wantProjectPath {
				t.Fatalf("unexpected project path: got %q want %q", binding.ProjectPath, tc.wantProjectPath)
			}
			if binding.Reason != tc.wantReason {
				t.Fatalf("unexpected reason: got %q want %q", binding.Reason, tc.wantReason)
			}
		})
	}

	server.PrimeRuntimeState(middlewareclient.RuntimeSnapshot{
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "inst-missing",
			InstanceCount: 2,
			RunningCount:  1,
			Items: []*discovery.QGISInstance{
				{ID: "inst-z", ProjectPath: "/tmp/project-z.qgz", Status: "running"},
				{ID: "inst-y", ProjectPath: "/tmp/project-y.qgz", Status: "stopped"},
			},
		},
	}, nil)

	binding, err := server.resolveSessionBinding("", "")
	if err != nil {
		t.Fatalf("resolveSessionBinding first running fallback failed: %v", err)
	}
	if binding.InstanceID != "inst-z" {
		t.Fatalf("unexpected first running instance id: %q", binding.InstanceID)
	}
	if binding.Reason != "first_running" {
		t.Fatalf("unexpected first running reason: %q", binding.Reason)
	}

	server.PrimeRuntimeState(middlewareclient.RuntimeSnapshot{}, nil)
	if _, err := server.resolveSessionBinding("", ""); err == nil {
		t.Fatal("expected unavailable error when no running instances exist")
	}
}
