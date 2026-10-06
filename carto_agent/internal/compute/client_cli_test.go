package compute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWSClientListsToolsAndInvokes(t *testing.T) {
	invoker := &fakeToolInvoker{
		listTools: []map[string]any{{
			"name":        "buffer",
			"description": "buffer",
			"category":    "vector",
			"inputSchema": map[string]any{"type": "object"},
			"available":   true,
			"backend":     "cli",
		}},
		invokeResult: &InvokeResponse{
			Success: true,
			Output:  map[string]any{"result": "ok"},
			Error:   "",
		},
	}

	client := NewWSClient(invoker, "inst-1", 2*time.Second)

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "buffer" || !tools[0].Available {
		t.Fatalf("unexpected ws tools: %#v", tools)
	}

	resp, err := client.Invoke(context.Background(), InvokeRequest{Tool: "buffer", Params: map[string]any{"DISTANCE": 10}})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected invoke success, got %#v", resp)
	}
	output, ok := resp.Output.(map[string]any)
	if !ok || output["result"] != "ok" {
		t.Fatalf("unexpected invoke output: %#v", resp.Output)
	}
	if invoker.listInstanceID != "inst-1" {
		t.Fatalf("unexpected list instance id: %q", invoker.listInstanceID)
	}
	if invoker.invokeInstanceID != "inst-1" {
		t.Fatalf("unexpected invoke instance id: %q", invoker.invokeInstanceID)
	}
	if invoker.invokeToolName != "buffer" {
		t.Fatalf("unexpected invoke tool name: %q", invoker.invokeToolName)
	}
	if got, _ := invoker.invokeParams["DISTANCE"].(int); got != 10 {
		t.Fatalf("unexpected invoke params: %#v", invoker.invokeParams)
	}
}

func TestWSClientInvokePropagatesInvokerError(t *testing.T) {
	client := NewWSClient(&fakeToolInvoker{invokeErr: errors.New("boom")}, "inst-9", 2*time.Second)
	if _, err := client.Invoke(context.Background(), InvokeRequest{Tool: "buffer", Params: map[string]any{"DISTANCE": 10}}); err == nil {
		t.Fatal("expected invoker error")
	}
}

func TestResolveLocalRuntimeUsesSeedWorkingDirWithoutSocketPath(t *testing.T) {
	root := t.TempDir()
	pythonPath := filepath.Join(root, "python")
	if err := os.WriteFile(pythonPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake python failed: %v", err)
	}

	runtime, err := ResolveLocalRuntime(pythonPath, 3, root)
	if err != nil {
		t.Fatalf("ResolveLocalRuntime failed: %v", err)
	}
	if runtime.WorkingDir != root {
		t.Fatalf("unexpected working dir: got=%q want=%q", runtime.WorkingDir, root)
	}
	if !containsEnv(runtime.Env, "AUTOCARTO_COMPUTE_MAX_WORKERS=3") {
		t.Fatalf("expected max workers env in %v", runtime.Env)
	}
}

func TestClientSourceDoesNotExposeLegacySocketTransport(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file failed")
	}

	clientSourcePath := filepath.Join(filepath.Dir(currentFile), "client.go")
	data, err := os.ReadFile(clientSourcePath)
	if err != nil {
		t.Fatalf("read client source failed: %v", err)
	}

	source := string(data)
	for _, forbidden := range []string{
		`DialContext(ctx, "unix"`,
		"type cliTransport struct",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("client.go should not contain %q", forbidden)
		}
	}
}

type fakeToolInvoker struct {
	listTools        []map[string]any
	listErr          error
	invokeResult     *InvokeResponse
	invokeErr        error
	listInstanceID   string
	invokeInstanceID string
	invokeToolName   string
	invokeParams     map[string]any
}

func (f *fakeToolInvoker) ListInstanceTools(ctx context.Context, instanceID string) ([]map[string]any, error) {
	_ = ctx
	f.listInstanceID = instanceID
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listTools, nil
}

func (f *fakeToolInvoker) InvokeTool(ctx context.Context, toolName string, params map[string]any) (*InvokeResponse, error) {
	_ = ctx
	f.invokeToolName = toolName
	f.invokeParams = params
	if rawID, ok := params["instance_id"].(string); ok {
		f.invokeInstanceID = rawID
	}
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	return f.invokeResult, nil
}

func containsEnv(env []string, target string) bool {
	for _, item := range env {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}
