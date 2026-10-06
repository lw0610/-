package websocket

import "testing"

func TestSummarizeBoundInstanceImpactCountsAffectedStatuses(t *testing.T) {
	states := []SessionRuntimeState{
		{SessionID: "sid-a", Status: sessionRuntimeStatusRunning, BoundInstanceID: "inst-a", BoundProjectPath: "/tmp/a.qgz"},
		{SessionID: "sid-b", Status: sessionRuntimeStatusInterrupted, BoundInstanceID: "inst-a", BoundProjectPath: "/tmp/a.qgz"},
		{SessionID: "sid-c", Status: sessionRuntimeStatusRunning, BoundInstanceID: "inst-b", BoundProjectPath: "/tmp/b.qgz"},
	}

	summary, ok := summarizeBoundInstanceImpact(states, "inst-a", "/tmp/a.qgz")
	if !ok {
		t.Fatal("expected bound instance impact summary")
	}
	if summary.InstanceID != "inst-a" {
		t.Fatalf("unexpected instance id: %q", summary.InstanceID)
	}
	if summary.ProjectPath != "/tmp/a.qgz" {
		t.Fatalf("unexpected project path: %q", summary.ProjectPath)
	}
	if summary.AffectedSessionCount != 2 || summary.RunningCount != 1 || summary.InterruptedCount != 1 {
		t.Fatalf("unexpected summary counts: %+v", summary)
	}
	if len(summary.AffectedSessionIDs) != 2 || summary.AffectedSessionIDs[0] != "sid-a" || summary.AffectedSessionIDs[1] != "sid-b" {
		t.Fatalf("unexpected affected session ids: %v", summary.AffectedSessionIDs)
	}
}

func TestSummarizeBoundInstanceImpactSkipsUnboundSessions(t *testing.T) {
	states := []SessionRuntimeState{{SessionID: "sid-a", Status: sessionRuntimeStatusRunning, BoundInstanceID: "inst-b"}}
	if _, ok := summarizeBoundInstanceImpact(states, "inst-a", ""); ok {
		t.Fatal("expected no summary for unrelated bound sessions")
	}
}
