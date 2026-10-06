package discovery

import "testing"

func TestHandleEventUpdatesProjectPathOnProjectSaved(t *testing.T) {
	registry := NewInstanceRegistry()
	t.Cleanup(func() {
		registry.Close()
	})

	if err := registry.Register(RegisterRequest{
		InstanceID:  "inst-1",
		PID:         1234,
		QGISVersion: "3.40",
		Source:      "reverse_ws",
	}); err != nil {
		t.Fatalf("register instance: %v", err)
	}

	const projectPath = "/tmp/demo.qgz"
	if err := registry.HandleEvent("inst-1", EventRequest{
		EventType: "project_saved",
		Data: map[string]interface{}{
			"project_path": projectPath,
		},
	}); err != nil {
		t.Fatalf("handle project_saved event: %v", err)
	}

	inst := registry.Get("inst-1")
	if inst == nil {
		t.Fatal("expected instance to exist")
	}
	if inst.ProjectPath != projectPath {
		t.Fatalf("project path mismatch: got=%q want=%q", inst.ProjectPath, projectPath)
	}
}
