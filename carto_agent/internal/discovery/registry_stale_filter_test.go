package discovery

import (
	"os"
	"testing"
	"time"
)

func TestShouldDemotePersistedRunningInstance(t *testing.T) {
	now := time.Now()

	if shouldDemotePersistedRunningInstance(nil, now) {
		t.Fatalf("nil instance should not be demoted")
	}

	healthy := &QGISInstance{
		Status:    "running",
		PID:       os.Getpid(),
		LastHeart: now,
	}
	if shouldDemotePersistedRunningInstance(healthy, now) {
		t.Fatalf("healthy running instance should not be demoted")
	}

	staleHeartbeat := &QGISInstance{
		Status:    "running",
		PID:       os.Getpid(),
		LastHeart: now.Add(-defaultHeartbeatTTL - time.Second),
	}
	if !shouldDemotePersistedRunningInstance(staleHeartbeat, now) {
		t.Fatalf("stale running instance should be demoted")
	}

	invalidPID := &QGISInstance{
		Status:    "running",
		PID:       0,
		LastHeart: now,
	}
	if !shouldDemotePersistedRunningInstance(invalidPID, now) {
		t.Fatalf("running instance with invalid pid should be demoted")
	}
}

func TestLoadPersistedInstancesDemotesStaleRunningRecord(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())

	originalArg0 := os.Args[0]
	os.Args[0] = "agent"
	defer func() {
		os.Args[0] = originalArg0
	}()

	writer := NewInstanceRegistry()
	if writer.db == nil {
		t.Fatalf("expected sqlite db to be initialized")
	}

	staleHeartbeat := time.Now().Add(-2 * defaultHeartbeatTTL).Format(time.RFC3339Nano)
	nowText := time.Now().Format(time.RFC3339Nano)
	_, err := writer.db.Exec(`
INSERT INTO qgis_instances (
	instance_id,
	machine_fingerprint,
	start_time,
	pid,
	qgis_version,
	project_path,
	status,
	source,
	last_heartbeat_at,
	connected_at,
	disconnected_at,
	created_at,
	updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		"stale_running_case",
		"machine-test",
		nowText,
		os.Getpid(),
		"3.34",
		"",
		"running",
		"reverse_ws",
		staleHeartbeat,
		nowText,
		"",
		nowText,
		nowText,
	)
	if err != nil {
		writer.Close()
		t.Fatalf("insert stale running record failed: %v", err)
	}
	writer.Close()

	reader := NewInstanceRegistry()
	defer reader.Close()

	inst := reader.Get("stale_running_case")
	if inst != nil {
		t.Fatalf("stale instance should not be exposed after demotion")
	}

	for _, item := range reader.GetAll() {
		if item != nil && item.ID == "stale_running_case" {
			t.Fatalf("stale instance should not be exposed in runtime snapshot")
		}
	}

	var status string
	if err := reader.db.QueryRow(`SELECT status FROM qgis_instances WHERE instance_id = ?`, "stale_running_case").Scan(&status); err != nil {
		t.Fatalf("query demoted stale instance failed: %v", err)
	}
	if status != "stopped" {
		t.Fatalf("stale instance should be persisted as stopped, got=%s", status)
	}
}

func TestLoadPersistedInstancesSkipsStoppedRecordExposure(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())

	originalArg0 := os.Args[0]
	os.Args[0] = "agent"
	defer func() {
		os.Args[0] = originalArg0
	}()

	writer := NewInstanceRegistry()
	if writer.db == nil {
		t.Fatalf("expected sqlite db to be initialized")
	}

	nowText := time.Now().Format(time.RFC3339Nano)
	_, err := writer.db.Exec(`
INSERT INTO qgis_instances (
	instance_id,
	machine_fingerprint,
	start_time,
	pid,
	qgis_version,
	project_path,
	status,
	source,
	last_heartbeat_at,
	connected_at,
	disconnected_at,
	created_at,
	updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		"stopped_case",
		"machine-test",
		nowText,
		os.Getpid(),
		"3.34",
		"",
		"stopped",
		"reverse_ws",
		nowText,
		nowText,
		nowText,
		nowText,
		nowText,
	)
	if err != nil {
		writer.Close()
		t.Fatalf("insert stopped record failed: %v", err)
	}
	writer.Close()

	reader := NewInstanceRegistry()
	defer reader.Close()

	if inst := reader.Get("stopped_case"); inst != nil {
		t.Fatalf("stopped instance should not be exposed by id lookup")
	}

	for _, item := range reader.GetAll() {
		if item != nil && item.ID == "stopped_case" {
			t.Fatalf("stopped instance should not be exposed in runtime snapshot")
		}
	}
}
