package websocket

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/integrations"
	"github.com/autocarto/carto_agent/internal/toolsession"
)

type browserSessionTestConfig struct{ config.IntegrationsConfig }

func (c browserSessionTestConfig) GetIntegrationsConfig() (config.IntegrationsConfig, error) {
	return c.IntegrationsConfig, nil
}

// Real subprocess and MCP transport; pause before the first cwd lookup so a UI
// session refresh deterministically exercises legacy migration during startup.
func TestIntegrationBrowserMCPHelper(t *testing.T) {
	if !strings.Contains(strings.Join(os.Args, " "), "autocarto-integration-probe") {
		return
	}
	gate := os.Getenv("AUTOCARTO_TEST_INTEGRATION_MCP_GATE")
	if err := os.WriteFile(gate+".ready", []byte("ready"), 0600); err != nil {
		os.Exit(2)
	}
	for {
		if _, err := os.Stat(gate + ".release"); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || len(request.ID) == 0 {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": request.Params["protocolVersion"], "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "session-browser-probe", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "browser_probe", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": cwd}}}
		default:
			result = map[string]any{}
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}
	os.Exit(0)
}

func integrationSessionFixture(t *testing.T, id string) (*DeepServer, context.Context, string, string, browserSessionTestConfig) {
	t.Helper()
	runtimeHome, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOCARTO_HOME", runtimeHome)
	project := filepath.Join(t.TempDir(), "测试工程.qgz")
	server := NewDeepServer(0, nil)
	if err := server.saveSessionToFile(map[string]any{"session_id": id, "project_path": project, "messages": []any{}, "tool_calls": []any{}, "snapshots": []any{}}, project, sessionSaveOperation{}); err != nil {
		t.Fatal(err)
	}
	root, err := server.getSessionDir(project, id)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "output", "integrations", "browser")
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	c := browserSessionTestConfig{}
	c.SetDefaults()
	c.Browser.Enabled = true
	c.Browser.Headless = true
	c.Browser.TimeoutSeconds = 30
	return server, toolsession.WithContext(context.Background(), id, project, "test-instance"), project, output, c
}

func TestIntegrationBrowserUsesCanonicalSessionDuringRefresh(t *testing.T) {
	server, ctx, project, output, c := integrationSessionFixture(t, "browser-canonical-startup")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c.Browser.Command = executable
	c.Browser.Args = []string{"-test.run=^TestIntegrationBrowserMCPHelper$", "--", "autocarto-integration-probe"}
	gate := filepath.Join(t.TempDir(), "startup")
	t.Setenv("AUTOCARTO_TEST_INTEGRATION_MCP_GATE", gate)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer func() {
		_ = os.WriteFile(gate+".release", []byte("release"), 0600)
		cancel()
		_, _ = integrations.Invoke(ctx, "browser_close", nil, c)
	}()
	started := make(chan error, 1)
	go func() {
		_, err := integrations.Invoke(ctx, "browser_list_tools", nil, c)
		started <- err
	}()
	for {
		if _, err := os.Stat(gate + ".ready"); err == nil {
			break
		}
		select {
		case err := <-started:
			t.Fatalf("browser ended before startup barrier: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	// This is the actual UI read path, including migration of legacy directories
	// beside the project. The original integration used that legacy source path.
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := server.listSessions(project); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(gate+".release", []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	value, err := integrations.Invoke(ctx, "browser_call_tool", map[string]any{"tool_name": "browser_probe"}, c)
	if err != nil {
		t.Fatal(err)
	}
	content := value.(map[string]any)["content"].([]map[string]any)
	if len(content) != 1 || content[0]["text"] != output {
		t.Fatalf("browser did not use canonical session workspace: %v, expected %s", content, output)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(project), "autoc")); !os.IsNotExist(err) {
		t.Fatalf("integration recreated a legacy migration source: %v", err)
	}
}

// Opt-in production chain: integration dispatcher -> real Node/MCP/Chrome while
// the same session-list operation used by the UI keeps refreshing concurrently.
func TestIntegrationBrowserLiveWithSessionRefresh(t *testing.T) {
	if os.Getenv("AUTOCARTO_BROWSER_SMOKE") != "1" {
		t.Skip("opt-in real Chrome integration test")
	}
	server, ctx, project, output, c := integrationSessionFixture(t, "browser-live-session-refresh")
	c.Browser.TimeoutSeconds = 90
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	defer func() { _, _ = integrations.Invoke(ctx, "browser_close", nil, c) }()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "<!doctype html><title>Session refresh fixture</title><h1>Browser stayed in its session workspace</h1>")
	}))
	defer page.Close()
	refreshCtx, stopRefresh := context.WithCancel(ctx)
	refreshDone := make(chan error, 1)
	go func() {
		for {
			if _, err := server.listSessions(project); err != nil {
				refreshDone <- err
				return
			}
			select {
			case <-refreshCtx.Done():
				refreshDone <- nil
				return
			case <-time.After(15 * time.Millisecond):
			}
		}
	}()
	defer func() {
		stopRefresh()
		if err := <-refreshDone; err != nil {
			t.Error(err)
		}
	}()
	if _, err := integrations.Invoke(ctx, "browser_list_tools", nil, c); err != nil {
		t.Fatal(err)
	}
	if _, err := integrations.Invoke(ctx, "browser_call_tool", map[string]any{"tool_name": "browser_navigate", "args": map[string]any{"url": page.URL}}, c); err != nil {
		t.Fatal(err)
	}
	value, err := integrations.Invoke(ctx, "browser_call_tool", map[string]any{"tool_name": "browser_snapshot", "args": map[string]any{}}, c)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(value)
	if !strings.Contains(string(data), "Browser stayed in its session workspace") {
		t.Fatalf("real browser did not read the fixture: %s", data)
	}
	if os.Getenv("AUTOCARTO_BROWSER_SMOKE_EXTERNAL") == "1" {
		if _, err := integrations.Invoke(ctx, "browser_call_tool", map[string]any{"tool_name": "browser_navigate", "args": map[string]any{"url": "https://example.com"}}, c); err != nil {
			t.Fatal(err)
		}
		value, err := integrations.Invoke(ctx, "browser_call_tool", map[string]any{"tool_name": "browser_snapshot", "args": map[string]any{}}, c)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(value)
		if !strings.Contains(string(data), "Example Domain") {
			t.Fatalf("real browser did not read example.com: %s", data)
		}
		t.Log("real Chrome also opened example.com and read its Example Domain heading")
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(project), "autoc")); !os.IsNotExist(err) {
		t.Fatalf("browser recreated legacy output: %v", err)
	}
	t.Log("real Chrome navigation and snapshot passed through the integration dispatcher during repeated UI session refreshes")
}
