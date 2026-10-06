package browserruntime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestPlaywrightSmoke is deliberately opt-in: the normal suite never downloads
// npm packages, launches a browser, or touches a browser profile. This smoke uses
// only a local fixture and the same isolated default configuration as production.
func TestPlaywrightSmoke(t *testing.T) {
	if os.Getenv("AUTOCARTO_BROWSER_SMOKE") != "1" {
		t.Skip("set AUTOCARTO_BROWSER_SMOKE=1 to test installed Chrome with fixed Playwright MCP")
	}
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("npx unavailable; no software installed")
	}
	if !chromeAvailable() {
		t.Skip("Chrome unavailable; browser installation is intentionally skipped")
	}
	const csv = "id,name\n1,local browser smoke\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fixture.csv" {
			w.Header().Set("Content-Type", "text/csv")
			w.Header().Set("Content-Disposition", `attachment; filename="fixture.csv"`)
			_, _ = fmt.Fprint(w, csv)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<!doctype html><html><title>AutoCarto browser fixture</title><body><h1>Local browser smoke</h1><a href="/fixture.csv" download>Download CSV</a></body></html>`)
	}))
	defer server.Close()
	output, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Enabled: true, Headless: true, Browser: "chrome", TimeoutSeconds: 120, SessionID: "local-playwright-smoke", OutputDir: output}
	m := NewManager()
	defer m.CloseAll()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	catalog, err := m.Invoke(ctx, "browser_list_tools", nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]map[string]any{}
	for _, tool := range catalog.(map[string]any)["tools"].([]map[string]any) {
		known[tool["name"].(string)] = tool
	}
	for _, name := range []string{"browser_navigate", "browser_snapshot", "browser_click"} {
		if known[name] == nil {
			t.Fatalf("missing live tool %s", name)
		}
	}
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		value, err := m.Invoke(ctx, "browser_call_tool", map[string]any{"tool_name": name, "args": args}, opts)
		if err != nil {
			t.Fatalf("%s: %v; result=%v", name, err, value)
		}
		return value.(map[string]any)
	}
	call("browser_navigate", map[string]any{"url": server.URL})
	snapshot := call("browser_snapshot", map[string]any{})
	var text strings.Builder
	for _, block := range snapshot["content"].([]map[string]any) {
		if s, ok := block["text"].(string); ok {
			text.WriteString(s)
		}
	}
	match := regexp.MustCompile(`link "Download CSV"[^\n]*\[ref=([^\]]+)\]`).FindStringSubmatch(text.String())
	if len(match) != 2 {
		t.Fatalf("download link missing from accessibility snapshot: %s", text.String())
	}
	clickArgs := map[string]any{}
	schema := known["browser_click"]["inputSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	if _, ok := properties["target"]; ok {
		clickArgs["target"] = match[1]
	} else if _, ok := properties["ref"]; ok {
		clickArgs["ref"] = match[1]
	} else {
		t.Fatalf("unrecognized click target schema: %v", schema)
	}
	if _, ok := properties["element"]; ok {
		clickArgs["element"] = "Download CSV link"
	}
	call("browser_click", clickArgs)
	deadline := time.Now().Add(5 * time.Second)
	var downloaded []byte
	for time.Now().Before(deadline) {
		downloaded, err = os.ReadFile(filepath.Join(output, "fixture.csv"))
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || string(downloaded) != csv {
		t.Fatalf("CSV download failed: %v content=%q", err, downloaded)
	}
	m.mu.Lock()
	command := m.sessions[opts.SessionID].command
	m.mu.Unlock()
	if _, err = m.Invoke(ctx, "browser_close", nil, opts); err != nil {
		t.Fatal(err)
	}
	if len(m.sessions) != 0 || command.ProcessState == nil || !command.ProcessState.Exited() {
		t.Fatal("MCP process did not exit on close")
	}
	t.Logf("Verified Playwright MCP %s: local navigation, accessibility snapshot, click, CSV download and process close", PlaywrightVersion)
}

func chromeAvailable() bool {
	if runtime.GOOS == "darwin" {
		_, err := os.Stat("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
		return err == nil
	}
	if runtime.GOOS == "windows" {
		for _, key := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
			if root := os.Getenv(key); root != "" {
				if _, err := os.Stat(filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe")); err == nil {
					return true
				}
			}
		}
		return false
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable"} {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}
