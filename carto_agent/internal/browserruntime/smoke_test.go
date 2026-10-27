package browserruntime

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	const fixtureCSV = "id,name\n1,local browser smoke\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fixture.csv" {
			w.Header().Set("Content-Type", "text/csv")
			w.Header().Set("Content-Disposition", `attachment; filename="fixture.csv"`)
			_, _ = fmt.Fprint(w, fixtureCSV)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<!doctype html><html><title>AutoCarto browser fixture</title><body><h1>Local browser smoke</h1><a href="/fixture.csv" download>Download CSV</a><table id="statistics"><tr><th>地区</th><th>值</th></tr><tr><td>甲,省</td><td>0</td></tr><tr><td>乙&quot;省</td><td></td></tr><tr><td>丙
省</td><td>12</td></tr></table></body></html>`)
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
	for _, name := range []string{"browser_navigate", "browser_snapshot", "browser_click", "browser_evaluate"} {
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
	if err != nil || string(downloaded) != fixtureCSV {
		t.Fatalf("CSV download failed: %v content=%q", err, downloaded)
	}
	// A dynamic portal may expose a real table but no CSV export. Validate the
	// documented browser-only handoff: extract the observed table, escape CSV in
	// the page, and download it via the same MCP context. No file-writing shell,
	// hidden endpoint or additional reader is needed.
	runSchema := known["browser_evaluate"]["inputSchema"].(map[string]any)
	if _, ok := runSchema["properties"].(map[string]any)["function"]; !ok {
		t.Fatalf("unrecognized browser_evaluate schema: %v", runSchema)
	}
	call("browser_evaluate", map[string]any{"function": `() => {
    const rows = Array.from(document.querySelectorAll('#statistics tr'), row =>
      Array.from(row.querySelectorAll('th,td'), cell => cell.textContent));
    const csv = '\uFEFF' + rows.map(row => row.map(value =>
      '"' + value.replace(/"/g, '""') + '"').join(',')).join('\r\n') + '\r\n';
    const link = document.createElement('a');
    link.href = URL.createObjectURL(new Blob([csv], {type: 'text/csv;charset=utf-8'}));
    link.download = 'page-table.csv';
    document.body.appendChild(link);
    link.click();
    link.remove();
    return {rows: rows.length};
}`})
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		downloaded, err = os.ReadFile(filepath.Join(output, "page-table.csv"))
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("page-table CSV was not saved by MCP: %v", err)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(downloaded), "\uFEFF"))).ReadAll()
	wantRows := [][]string{{"地区", "值"}, {"甲,省", "0"}, {"乙\"省", ""}, {"丙\n省", "12"}}
	if err != nil || !reflect.DeepEqual(rows, wantRows) {
		t.Fatalf("page-table CSV changed real cells: %v rows=%q", err, rows)
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
	t.Logf("Verified Playwright MCP %s: navigation, snapshot, file download, page-table Blob CSV (quoted cells, zero and missing values) and process close", PlaywrightVersion)
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
