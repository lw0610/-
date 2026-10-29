package browserruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This subprocess implements just enough stdio MCP to exercise the real SDK,
// including initialization, notifications, process exit and application errors.
func TestMCPHelper(t *testing.T) {
	helper := false
	stallInit := false
	for _, arg := range os.Args {
		if arg == "autocarto-fake-mcp" {
			helper = true
		}
		if arg == "stall-init" {
			stallInit = true
		}
	}
	if !helper {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	encoder := json.NewEncoder(os.Stdout)
	initialized := false
	page := "about:blank"
	for scanner.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(2)
		}
		if req.Method == "notifications/initialized" {
			initialized = true
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "initialize":
			if stallInit {
				continue
			}
			var params struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(req.Params, &params)
			response["result"] = map[string]any{"protocolVersion": params.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "fake-browser", "version": "1"}}
		case "tools/list":
			if !initialized {
				os.Exit(3)
			}
			response["result"] = map[string]any{"tools": []any{map[string]any{"name": "browser_pid", "description": "Return fake process ID", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			if !initialized {
				os.Exit(4)
			}
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &params)
			var arguments map[string]any
			if json.Unmarshal(params.Arguments, &arguments) != nil || arguments == nil {
				response["error"] = map[string]any{"code": -32602, "message": "arguments: expected record, received null"}
				_ = encoder.Encode(response)
				continue
			}
			text := strconv.Itoa(os.Getpid())
			isError := false
			switch params.Name {
			case "browser_hang":
				continue
			case "browser_exit":
				os.Exit(7)
			case "browser_exit_later":
				time.AfterFunc(50*time.Millisecond, func() { os.Exit(7) })
			case "browser_error":
				text = "fake navigation failed"
				isError = true
			case "browser_invalid_params":
				response["error"] = map[string]any{"code": -32602, "message": "fake invalid tool arguments"}
				_ = encoder.Encode(response)
				continue
			case "browser_internal_error":
				response["error"] = map[string]any{"code": -32603, "message": "fake request failure"}
				_ = encoder.Encode(response)
				continue
			case "browser_server_closing":
				response["error"] = map[string]any{"code": -32004, "message": "server is closing"}
				_ = encoder.Encode(response)
				continue
			case "browser_bad_result":
				response["result"] = map[string]any{"content": "invalid content format"}
				_ = encoder.Encode(response)
				continue
			case "browser_wire_args":
				text = string(params.Arguments)
			case "browser_navigate":
				page, _ = arguments["url"].(string)
				text = page
			case "browser_snapshot":
				text = page
			case "browser_screenshot":
				response["result"] = map[string]any{"content": []any{map[string]any{"type": "image", "mimeType": "image/png", "data": []byte("\x89PNG\r\n\x1a\nMCP-image")}}}
				_ = encoder.Encode(response)
				continue
			case "browser_args":
				text = strings.Join(os.Args, "\n")
			case "browser_large":
				text = strings.Repeat("地图", maxTextBytes)
			case "browser_download":
				if err := os.WriteFile("download.csv", []byte("id,name\n1,test\n"), 0600); err != nil {
					os.Exit(8)
				}
				text = "Downloaded download.csv"
			}
			response["result"] = map[string]any{"isError": isError, "content": []any{map[string]any{"type": "text", "text": text}}}
		default:
			response["result"] = map[string]any{}
		}
		if encoder.Encode(response) != nil {
			os.Exit(9)
		}
	}
	os.Exit(0)
}

func fakeOptions(t *testing.T, id string) Options {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	outputDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Options{Enabled: true, Command: executable, Args: []string{"-test.run=^TestMCPHelper$", "--", "autocarto-fake-mcp"}, Headless: true, Browser: "chrome", TimeoutSeconds: 3, SessionID: id, OutputDir: outputDir}
}

func callFake(t *testing.T, m *Manager, opts Options, name string) map[string]any {
	t.Helper()
	result, err := m.Invoke(context.Background(), "browser_call_tool", map[string]any{"tool_name": name, "args": map[string]any{}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	return result.(map[string]any)
}

func resultText(result map[string]any) string {
	return result["content"].([]map[string]any)[0]["text"].(string)
}

func TestHandshakeReuseAndSessionIsolation(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	a := fakeOptions(t, "a")
	result, err := m.Invoke(context.Background(), "browser_list_tools", nil, a)
	if err != nil {
		t.Fatal(err)
	}
	tools := result.(map[string]any)["tools"].([]map[string]any)
	if len(tools) != 1 || tools[0]["name"] != "browser_pid" {
		t.Fatalf("unexpected tools: %v", tools)
	}
	pidA := resultText(callFake(t, m, a, "browser_pid"))
	if again := resultText(callFake(t, m, a, "browser_pid")); again != pidA {
		t.Fatalf("lost persistent session: %s != %s", again, pidA)
	}
	b := fakeOptions(t, "b")
	if pidB := resultText(callFake(t, m, b, "browser_pid")); pidA == pidB {
		t.Fatal("sessions shared a process")
	}
	argv := resultText(callFake(t, m, a, "browser_args"))
	for _, want := range []string{"--isolated", "--output-dir", a.OutputDir, "--headless"} {
		if !strings.Contains(argv, want) {
			t.Errorf("missing launch argument %s", want)
		}
	}
	if _, err = m.Invoke(context.Background(), "browser_close", nil, a); err != nil {
		t.Fatal(err)
	}
	if next := resultText(callFake(t, m, a, "browser_pid")); next == pidA {
		t.Fatal("close did not reset process")
	}
}

func TestDiscoveredBrowserToolInvokesThroughReturnedWrapper(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	opts := fakeOptions(t, "dispatch-template")
	result, err := m.Invoke(context.Background(), "browser_list_tools", nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	catalog := result.(map[string]any)
	if catalog["invocation_tool"] != "browser_call_tool" || catalog["direct_invocation"] != false {
		t.Fatalf("catalog did not declare its dispatch boundary: %v", catalog)
	}
	entry := catalog["tools"].([]map[string]any)[0]
	if entry["call_via"] != "browser_call_tool" {
		t.Fatalf("discovered tool lost its wrapper: %v", entry)
	}
	// Round-trip the model-visible JSON, then actually execute its template.
	// This fixture's browser_pid takes no args; other tools fill args from schema.
	raw, err := json.Marshal(entry["invocation_template"])
	if err != nil {
		t.Fatal(err)
	}
	var invocation struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &invocation); err != nil {
		t.Fatal(err)
	}
	if invocation.Arguments["tool_name"] != entry["name"] {
		t.Fatalf("template did not preserve actual discovered name: %s", raw)
	}
	called, err := m.Invoke(context.Background(), invocation.Name, invocation.Arguments, opts)
	if err != nil {
		t.Fatalf("returned wrapper template cannot be executed: %v", err)
	}
	if _, err := strconv.Atoi(resultText(called.(map[string]any))); err != nil {
		t.Fatalf("template did not reach internal MCP tool: %v", called)
	}
	if Handles(entry["name"].(string)) {
		t.Fatal("internal MCP name was exposed as a top-level browser entry")
	}
	if _, err := m.Invoke(context.Background(), entry["name"].(string), map[string]any{}, opts); err == nil {
		t.Fatal("direct invocation bypassed browser_call_tool")
	}
}

func TestDisabledCheckDoesNotLaunchAndDisableCloses(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	opts := fakeOptions(t, "disable")
	opts.Enabled = false
	result, err := m.Invoke(context.Background(), "browser_check_environment", nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["server_started"] != false || len(m.sessions) != 0 {
		t.Fatal("environment check started process")
	}
	if _, err = m.Invoke(context.Background(), "browser_list_tools", nil, opts); err == nil {
		t.Fatal("disabled browser was usable")
	}
	opts.Enabled = true
	pid := resultText(callFake(t, m, opts, "browser_pid"))
	opts.Enabled = false
	if _, err = m.Invoke(context.Background(), "browser_list_tools", nil, opts); err == nil {
		t.Fatal("expected disabled error")
	}
	if len(m.sessions) != 0 {
		t.Fatal("disable retained session")
	}
	opts.Enabled = true
	if next := resultText(callFake(t, m, opts, "browser_pid")); next == pid {
		t.Fatal("disable did not close process")
	}
	opts.Enabled = false
	opts.Args = []string{"--config", "bad"}
	if _, err = m.Invoke(context.Background(), "browser_close", nil, opts); err != nil {
		t.Fatal(err)
	}
}

func TestTransportFailuresAndCancellationCleanUp(t *testing.T) {
	for _, tool := range []string{"browser_bad_result", "browser_server_closing", "browser_exit", "browser_hang"} {
		t.Run(tool, func(t *testing.T) {
			m := NewManager()
			defer m.CloseAll()
			opts := fakeOptions(t, tool)
			pid := resultText(callFake(t, m, opts, "browser_pid"))
			ctx := context.Background()
			cancel := func() {}
			if tool == "browser_hang" {
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
			}
			defer cancel()
			_, err := m.Invoke(ctx, "browser_call_tool", map[string]any{"tool_name": tool}, opts)
			if err == nil {
				t.Fatal("expected operation failure")
			}
			if len(m.sessions) != 0 {
				t.Fatal("failed session not cleaned")
			}
			if next := resultText(callFake(t, m, opts, "browser_pid")); next == pid {
				t.Fatal("error retained same process")
			}
		})
	}
}

func TestEmptyArgumentsAreObjectsOnMCPWire(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	opts := fakeOptions(t, "empty-arguments")
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"omitted", map[string]any{"tool_name": "browser_wire_args"}},
		{"null", map[string]any{"tool_name": "browser_wire_args", "args": nil}},
		{"empty-object", map[string]any{"tool_name": "browser_wire_args", "args": map[string]any{}}},
		// The skill argument resolver currently clones empty maps into typed nils.
		{"typed-nil-map", map[string]any{"tool_name": "browser_wire_args", "args": map[string]any(nil)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := m.Invoke(context.Background(), "browser_call_tool", tc.args, opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := resultText(result.(map[string]any)); got != "{}" {
				t.Fatalf("MCP arguments = %s, want {}", got)
			}
		})
	}
	result, err := m.Invoke(context.Background(), "browser_call_tool", map[string]any{
		"tool_name": "browser_wire_args", "args": map[string]any{"nested": map[string]any{"enabled": false}, "count": float64(2)},
	}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := resultText(result.(map[string]any)); got != `{"count":2,"nested":{"enabled":false}}` {
		t.Fatalf("non-empty arguments changed: %s", got)
	}
}

func TestRequestErrorsPreserveOpenBrowserSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"tool-error", map[string]any{"tool_name": "browser_error"}},
		{"invalid-params", map[string]any{"tool_name": "browser_invalid_params"}},
		{"internal-request-error", map[string]any{"tool_name": "browser_internal_error"}},
		{"local-args-type", map[string]any{"tool_name": "browser_snapshot", "args": "not an object"}},
		{"local-path-validation", map[string]any{"tool_name": "browser_download", "args": map[string]any{"filename": "../escape.csv"}}},
		{"local-tool-name", map[string]any{"tool_name": "invalid"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager()
			defer m.CloseAll()
			opts := fakeOptions(t, tc.name)
			pid := resultText(callFake(t, m, opts, "browser_pid"))
			const page = "https://example.com/data-search"
			if _, err := m.Invoke(context.Background(), "browser_call_tool", map[string]any{
				"tool_name": "browser_navigate", "args": map[string]any{"url": page},
			}, opts); err != nil {
				t.Fatal(err)
			}
			result, err := m.Invoke(context.Background(), "browser_call_tool", tc.args, opts)
			if err == nil {
				t.Fatal("expected request error")
			}
			if tc.name == "tool-error" && (result == nil || result.(map[string]any)["is_error"] != true) {
				t.Fatalf("lost structured tool error: %v %v", result, err)
			}
			if strings.Contains(err.Error(), "会话已关闭") {
				t.Fatalf("error incorrectly reports a closed session: %v", err)
			}
			if got := resultText(callFake(t, m, opts, "browser_pid")); got != pid {
				t.Fatalf("request error restarted MCP: old PID %s, new PID %s", pid, got)
			}
			if got := resultText(callFake(t, m, opts, "browser_snapshot")); got != page {
				t.Fatalf("request error lost the open page: %s", got)
			}
		})
	}
}

func TestHandshakeCancellation(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	opts := fakeOptions(t, "handshake")
	opts.Args = append(opts.Args, "stall-init")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := m.Invoke(ctx, "browser_list_tools", nil, opts); err == nil {
		t.Fatal("expected handshake timeout")
	}
	if len(m.sessions) != 0 {
		t.Fatal("handshake failure retained session")
	}
}

func TestOutputFilesAndBoundedContent(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	opts := fakeOptions(t, "output")
	result := callFake(t, m, opts, "browser_download")
	files := result["output_files"].([]map[string]any)
	if len(files) != 1 || files[0]["path"] != filepath.Join(opts.OutputDir, "download.csv") {
		t.Fatalf("download path missing: %v", files)
	}
	result = callFake(t, m, opts, "browser_large")
	if result["truncated"] != true || len(resultText(result)) > maxTextBytes {
		t.Fatal("response was not bounded")
	}
	if _, err := m.Invoke(context.Background(), "browser_call_tool", map[string]any{"tool_name": "browser_download", "args": map[string]any{"filename": "../escape.csv"}}, opts); err == nil {
		t.Fatal("accepted outside output path")
	}
}

func TestControlledOptionsAndEnvironment(t *testing.T) {
	for _, flag := range []string{"--user-data-dir=/tmp/profile", "--config", "--output-dir", "--extension", "--storage-state=/tmp/cookies", "--cdp-endpoint=http://localhost:9222"} {
		if _, err := normalize(Options{Args: []string{"@playwright/mcp@" + PlaywrightVersion, flag}}); err == nil {
			t.Errorf("accepted %s", flag)
		}
	}
	n, err := normalize(Options{})
	if err != nil || n.Enabled || n.Command != "npx" || n.Args[1] != "@playwright/mcp@"+PlaywrightVersion {
		t.Fatalf("bad defaults: %+v %v", n, err)
	}
	t.Setenv("PLAYWRIGHT_MCP_USER_DATA_DIR", "/tmp/private-profile")
	for _, entry := range launchEnv() {
		if strings.HasPrefix(entry, "PLAYWRIGHT_MCP_") {
			t.Fatal("inherited uncontrolled profile setting")
		}
	}
}

func TestSessionLimitAndIdleReclamation(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	opts := fakeOptions(t, "session0")
	for i := 0; i < maxSessions; i++ {
		opts.SessionID = fmt.Sprint("session", i)
		callFake(t, m, opts, "browser_pid")
	}
	opts.SessionID = "overflow"
	if _, err := m.Invoke(context.Background(), "browser_list_tools", nil, opts); err == nil {
		t.Fatal("session limit not enforced")
	}
	m.mu.Lock()
	m.sessions["session0"].lastUsed = time.Now().Add(-idleTimeout - time.Second)
	m.mu.Unlock()
	callFake(t, m, opts, "browser_pid")
	if len(m.sessions) != maxSessions {
		t.Fatal("idle session not reclaimed")
	}
}

func TestUnexpectedExitIsReaped(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	opts := fakeOptions(t, "unexpected-exit")
	pid := resultText(callFake(t, m, opts, "browser_exit_later"))
	m.mu.Lock()
	e := m.sessions[opts.SessionID]
	m.mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for !e.dead.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !e.dead.Load() {
		t.Fatal("unexpected process exit was not cleaned up")
	}
	if next := resultText(callFake(t, m, opts, "browser_pid")); next == pid {
		t.Fatal("dead MCP process reused")
	}
}

func TestCloseInterruptsActiveCall(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	opts := fakeOptions(t, "interrupt-close")
	opts.TimeoutSeconds = 3600
	callFake(t, m, opts, "browser_pid")
	done := make(chan error, 1)
	go func() {
		_, err := m.Invoke(context.Background(), "browser_call_tool", map[string]any{"tool_name": "browser_hang"}, opts)
		done <- err
	}()
	// Wait until the active invocation owns the per-session gate.
	m.mu.Lock()
	e := m.sessions[opts.SessionID]
	m.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for len(e.gate) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := m.Invoke(context.Background(), "browser_close", nil, opts); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("active call should be interrupted")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("close did not cancel active call")
	}
}

func TestNpxShimWithSpaces(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Program Files", "node js")
	cli := filepath.Join(root, "node_modules", "npm", "bin", "npx-cli.js")
	if err := os.MkdirAll(filepath.Dir(cli), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{cli, filepath.Join(root, "node.exe")} {
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	node, resolvedCLI, err := resolveNpxShim(filepath.Join(root, "npx.cmd"))
	if err != nil || node != filepath.Join(root, "node.exe") || resolvedCLI != cli {
		t.Fatalf("shim paths changed: %q %q %v", node, resolvedCLI, err)
	}
	if _, _, err := resolveNpxShim(filepath.Join(t.TempDir(), "npx.cmd")); err == nil {
		t.Fatal("missing shim must not be marked available")
	}
}

func TestEnvironmentRejectsMissingCommands(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	for _, command := range []string{
		filepath.Join(t.TempDir(), "missing-node"),
		"autocarto-browser-command-that-does-not-exist-82da41",
	} {
		result, err := m.Invoke(context.Background(), "browser_check_environment", nil,
			Options{Command: command, Args: []string{"unused-cli.js"}})
		if err != nil {
			t.Fatal(err)
		}
		if result.(map[string]any)["command_available"] != false {
			t.Errorf("missing command was marked available: %q: %v", command, result)
		}
	}
}
