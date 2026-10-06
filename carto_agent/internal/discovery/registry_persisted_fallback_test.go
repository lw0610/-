package discovery

import (
	"os"
	"testing"
	"time"
)

func TestGetAllAndGetFallbackToPersistedRunningInstances(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())

	originalArg0 := os.Args[0]
	os.Args[0] = "agent"
	defer func() {
		os.Args[0] = originalArg0
	}()

	writer := NewInstanceRegistry()
	defer writer.Close()

	reader := NewInstanceRegistry()
	defer reader.Close()

	request := RegisterRequest{
		InstanceID:  "qgis_persisted_case",
		PID:         os.Getpid(),
		QGISVersion: "3.34",
		ProjectPath: "",
		Source:      "reverse_ws",
		StartTime:   time.Now().Format(time.RFC3339),
	}
	if err := writer.Register(request); err != nil {
		t.Fatalf("register instance failed: %v", err)
	}

	instances := reader.GetAll()
	if len(instances) != 1 {
		t.Fatalf("expected one persisted instance, got=%d", len(instances))
	}
	if instances[0].ID != request.InstanceID {
		t.Fatalf("unexpected instance id: got=%s want=%s", instances[0].ID, request.InstanceID)
	}

	instance := reader.Get(request.InstanceID)
	if instance == nil {
		t.Fatalf("expected persisted instance by id %s", request.InstanceID)
	}
	if instance.ID != request.InstanceID {
		t.Fatalf("unexpected instance from Get: got=%s want=%s", instance.ID, request.InstanceID)
	}
}
