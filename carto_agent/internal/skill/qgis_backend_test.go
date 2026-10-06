package skill

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/compute"
	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/toolsession"
)

type stubQGISRuntimeInspector struct {
	providerID   string
	activeID     string
	connectedIDs []string
}

func (s stubQGISRuntimeInspector) ProviderID() string {
	return s.providerID
}

func (s stubQGISRuntimeInspector) ActiveInstance() string {
	return s.activeID
}

func (s stubQGISRuntimeInspector) HasInstanceConnected(instanceID string) bool {
	for _, item := range s.connectedIDs {
		if item == instanceID {
			return true
		}
	}
	return false
}

func (s stubQGISRuntimeInspector) ConnectedInstanceIDs() []string {
	return append([]string(nil), s.connectedIDs...)
}

func TestQGISBackendReturnsNotReadyWithoutConnectedInstance(t *testing.T) {
	t.Parallel()

	callCount := 0
	backend := NewQGISBackend(func(context.Context, string, map[string]any) (*compute.InvokeResponse, error) {
		callCount++
		return &compute.InvokeResponse{Success: true}, nil
	}, nil, stubQGISRuntimeInspector{providerID: "qgis_bridge"})

	_, err := backend.Execute(context.Background(), "open_project", map[string]any{})
	if err == nil {
		t.Fatal("expected not ready error")
	}
	typed, ok := err.(*bridgepkg.ToolInvokeError)
	if !ok {
		t.Fatalf("expected ToolInvokeError, got %T", err)
	}
	if typed.Code != bridgepkg.ToolErrorCodeNotReady {
		t.Fatalf("unexpected error code: %#v", typed)
	}
	if callCount != 0 {
		t.Fatalf("expected response invoker to be skipped when not ready, got %d", callCount)
	}
}

func TestEngineExecutePrefersQGISBackendAndPreservesOutputLayers(t *testing.T) {
	t.Parallel()

	registry := loadRegistryForTest(t, map[string]string{
		"qgis/open_project.yaml": `
apiVersion: tool/v1
kind: Tool
metadata:
  name: open_project
  domain: qgis
  runtime: qgis
spec:
  description: Open a project.
  parameters:
    - name: path
      type: string
      required: true
execution:
  backend_tool: open_project
  param_mapping:
    path: "{{ path }}"
`,
	})

	qgisCalls := 0
	engine := NewEngine(
		registry,
		NewComputeBackend(&fakeComputeInvoker{resp: &compute.InvokeResponse{Success: true, Output: map[string]any{"legacy": true}}}),
		NewQGISBackend(func(ctx context.Context, toolName string, args map[string]any) (*compute.InvokeResponse, error) {
			qgisCalls++
			return &compute.InvokeResponse{
				Success: true,
				Output:  map[string]any{"opened": true},
				OutputLayers: []compute.OutputLayer{{
					Path:     "/tmp/demo.qgz",
					AutoLoad: true,
				}},
			}, nil
		}, nil, stubQGISRuntimeInspector{providerID: "qgis_bridge", activeID: "inst-1", connectedIDs: []string{"inst-1"}}),
	)
	ctx := toolsession.WithContext(context.Background(), "session-qgis", filepath.Join(t.TempDir(), "demo.qgz"), "inst-1")

	result, err := engine.Execute(ctx, "open_project", map[string]any{"path": "/tmp/demo.qgz"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.BackendID != RuntimeIDQGIS {
		t.Fatalf("expected qgis backend id, got %s", result.BackendID)
	}
	if qgisCalls != 1 {
		t.Fatalf("expected qgis backend to be called once, got %d", qgisCalls)
	}
	if len(result.OutputLayers) != 1 || result.OutputLayers[0].Path != "/tmp/demo.qgz" {
		t.Fatalf("expected qgis output layers to be preserved, got %#v", result.OutputLayers)
	}
}

func TestQGISBackendAppliesArgsMutatorBeforePreflightAndInvoke(t *testing.T) {
	t.Parallel()

	original := map[string]any{"source_text": "测试文本"}
	var invoked map[string]any
	backend := NewQGISBackend(
		func(_ context.Context, _ string, args map[string]any) (*compute.InvokeResponse, error) {
			invoked = cloneMapAny(args)
			return &compute.InvokeResponse{Success: true, Output: map[string]any{"ok": true}}, nil
		},
		nil,
		stubQGISRuntimeInspector{providerID: "qgis_bridge"},
		func(_ context.Context, args map[string]any) {
			args["instance_id"] = "qgis-instance-1"
			args["session_dir"] = "/tmp/autocarto/session-1"
			args["session_subdirs"] = map[string]any{
				"intermediate": "/tmp/autocarto/session-1/intermediate",
			}
		},
	)

	_, err := backend.Execute(
		context.Background(),
		"marching_route_compile_dataset",
		original,
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := invoked["session_dir"]; got != "/tmp/autocarto/session-1" {
		t.Fatalf("session_dir = %v, want injected session directory", got)
	}
	if got := invoked["instance_id"]; got != "qgis-instance-1" {
		t.Fatalf("instance_id = %v, want injected QGIS instance", got)
	}
	if _, mutated := original["session_dir"]; mutated {
		t.Fatal("Execute mutated the caller's original args")
	}
}
