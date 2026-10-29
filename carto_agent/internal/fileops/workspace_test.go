package fileops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/schema"
)

func testWorkspace(t *testing.T) workspace {
	t.Helper()
	root := t.TempDir()
	output := filepath.Join(root, "output")
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	return workspace{root: root, output: output}
}
func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestReadWritePreservesCSVAndReturnsRealFile(t *testing.T) {
	w := testWorkspace(t)
	ctx := context.Background()
	content := "\ufeff城市,人口,说明\r\n甲市,42,\"含,逗号\"\r\n乙市,31,\"保留 空格\"\r\n"
	r, err := w.write(ctx, map[string]any{"path": "tables/population.csv", "content": content})
	if err != nil {
		t.Fatal(err)
	}
	p := r.(map[string]any)["path"].(string)
	raw, err := os.ReadFile(p)
	if err != nil || string(raw) != content {
		t.Fatalf("CSV bytes changed: %q %v", raw, err)
	}
	if _, err = w.write(ctx, map[string]any{"path": p, "content": "changed", "overwrite": false}); err == nil {
		t.Fatal("overwrite=false replaced file")
	}
	r, err = w.read(ctx, map[string]any{"path": p, "offset": 2, "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	got := r.(map[string]any)
	if !strings.Contains(got["content"].(string), "甲市,42") || got["next_offset"] != 3 || got["truncated"] != true {
		t.Fatalf("bad page: %#v", got)
	}
	if _, err = w.write(ctx, map[string]any{"path": p, "content": "\n  exact\n"}); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(p)
	if string(raw) != "\n  exact\n" {
		t.Fatalf("write trimmed text: %q", raw)
	}
}

func TestFileReadMediaEnvelopeAndBinaryRejection(t *testing.T) {
	w := testWorkspace(t)
	for _, tc := range []struct{ name, body, kind, mime string }{{"figure.png", "\x89PNG\r\n\x1a\nrest", "image", "image/png"}, {"source.pdf", "%PDF-1.7\nfixture", "file", "application/pdf"}} {
		p := filepath.Join(w.output, tc.name)
		writeFixture(t, p, tc.body)
		r, err := w.read(context.Background(), map[string]any{"path": p})
		if err != nil {
			t.Fatal(err)
		}
		media := r.(map[string]any)["_agent_media"].([]any)
		expected, _ := canonical(p)
		attachment := media[0].(map[string]any)
		if len(media) != 1 || attachment["path"] != expected || attachment["type"] != tc.kind || attachment["mime_type"] != tc.mime {
			t.Fatalf("missing real media: %#v", media)
		}
	}
	p := filepath.Join(w.output, "book.xlsx")
	writeFixture(t, p, "PK\x00\x00binary")
	if _, err := w.read(context.Background(), map[string]any{"path": p}); err == nil {
		t.Fatal("binary treated as text")
	}
}

func TestFileReadsLocalPathsAndKeepsWritesInOutput(t *testing.T) {
	w := testWorkspace(t)
	outside := t.TempDir()
	p := filepath.Join(outside, "data.csv")
	writeFixture(t, p, "real")
	if _, err := w.read(context.Background(), map[string]any{"path": p}); err != nil {
		t.Fatalf("local file could not be read: %v", err)
	}
	if _, err := w.read(context.Background(), map[string]any{"path": outside}); err != nil {
		t.Fatalf("local directory could not be read: %v", err)
	}
	if _, err := w.write(context.Background(), map[string]any{"path": p, "content": "changed"}); err == nil {
		t.Fatal("write escaped output")
	}
	if _, err := w.resolve("../outside.csv", true); err == nil {
		t.Fatal("relative traversal accepted")
	}
	secret := filepath.Join(outside, ".env")
	writeFixture(t, secret, "test fixture only")
	if _, err := w.read(context.Background(), map[string]any{"path": secret}); err != nil {
		t.Fatalf("local text file could not be read: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(w.output, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := w.read(context.Background(), map[string]any{"path": filepath.Join(w.output, "link", "data.csv")}); err != nil {
		t.Fatalf("local file symlink could not be read: %v", err)
	}
	if _, err := w.write(context.Background(), map[string]any{"path": "link/new/deeper.csv", "content": "bad"}); err == nil {
		t.Fatal("symlink write escaped")
	}
	if err := os.RemoveAll(w.output); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, w.output); err != nil {
		t.Fatal(err)
	}
	if _, err := w.write(context.Background(), map[string]any{"path": "escaped.csv", "content": "bad"}); err == nil {
		t.Fatal("output symlink escaped session")
	}
}

func TestFileToolsUseTrustedSessionNotArguments(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "project.qgz")
	writeFixture(t, project, "fixture")
	if _, err := Invoke(context.Background(), ToolReadFile, map[string]any{"session_id": "fake", "session_dir": filepath.Dir(project), "path": project}); err == nil {
		t.Fatal("model selected session")
	}
	ctx := toolsession.WithContext(context.Background(), "file-tools", project, "")
	r, err := Invoke(ctx, ToolWriteFile, map[string]any{"path": "result.csv", "content": "a,b\n1,2\n", "session_dir": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	root, err := config.ResolveSessionDir(project, "file-tools")
	if err != nil {
		t.Fatal(err)
	}
	path := r.(map[string]any)["path"].(string)
	expected, _ := canonical(filepath.Join(root, "output", "result.csv"))
	if path != expected {
		t.Fatalf("wrong session root: %s != %s", path, expected)
	}
}

func TestFileReadAllowsSharedDatastoreReadOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", home)
	project := filepath.Join(t.TempDir(), "project.qgz")
	writeFixture(t, project, "fixture")
	ctx := toolsession.WithContext(context.Background(), "registered-data", project, "")
	store, err := sessionfiles.New(project, "registered-data")
	if err != nil {
		t.Fatal(err)
	}
	registered := filepath.Join(home, "data", "acquired", "source", "query", "out", "population.csv")
	other := filepath.Join(filepath.Dir(registered), "unregistered.csv")
	writeFixture(t, registered, "city,population\n甲,42\n")
	writeFixture(t, other, "unrelated")
	if _, err = store.Register(registered, sessionfiles.Origin{Type: "data_service", Tool: "data_get", SourceID: "source"}); err != nil {
		t.Fatal(err)
	}
	result, err := Invoke(ctx, ToolReadFile, map[string]any{"path": registered})
	if err != nil || !strings.Contains(result.(map[string]any)["content"].(string), "甲,42") {
		t.Fatalf("registered acquired input denied: %v", err)
	}
	if _, err = Invoke(ctx, ToolReadFile, map[string]any{"path": other}); err != nil {
		t.Fatal("datastore should be readable", err)
	}
	otherCtx := toolsession.WithContext(context.Background(), "other-session", project, "")
	if _, err = Invoke(otherCtx, ToolReadFile, map[string]any{"path": registered}); err != nil {
		t.Fatal("verified datastore is shared across sessions", err)
	}
	if _, err = Invoke(ctx, ToolWriteFile, map[string]any{"path": registered, "content": "changed"}); err == nil {
		t.Fatal("read registration granted write access")
	}
	provided := filepath.Join(t.TempDir(), "provided.csv")
	writeFixture(t, provided, "user input")
	if _, err = store.RegisterProvided(provided, sessionfiles.Origin{Type: "provided"}); err != nil {
		t.Fatal(err)
	}
	if _, err = Invoke(ctx, ToolReadFile, map[string]any{"path": provided}); err != nil {
		t.Fatalf("explicitly provided input denied: %v", err)
	}
	if err = os.Remove(registered); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.csv")
	writeFixture(t, outside, "local source")
	if err = os.Symlink(outside, registered); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err = Invoke(ctx, ToolReadFile, map[string]any{"path": registered}); err != nil {
		t.Fatalf("local file symlink could not be read: %v", err)
	}
}

func TestShellExecutionFailureTimeoutAndOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	w := testWorkspace(t)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("FILEOPS_TEST_API_KEY", "not-a-real-key")
	r, err := w.run(context.Background(), map[string]any{"command": "printf '%s\\n' \"$PWD\"; printf '%s' \"${FILEOPS_TEST_API_KEY-unset}\"; exit 7"})
	if err != nil {
		t.Fatal(err)
	}
	got := r.(map[string]any)
	if got["success"] != false || got["exit_code"] != 7 || !strings.Contains(got["output"].(string), "unset") || got["sandboxed"] != false {
		t.Fatalf("bad command result: %#v", got)
	}
	start := time.Now()
	r, err = w.run(context.Background(), map[string]any{"command": "sleep 20 & wait", "timeout_seconds": 1})
	if err != nil {
		t.Fatal(err)
	}
	if r.(map[string]any)["timed_out"] != true || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout did not stop process group: %#v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err = w.run(ctx, map[string]any{"command": "sleep 20"})
	if err != nil {
		t.Fatal(err)
	}
	if r.(map[string]any)["cancelled"] != true {
		t.Fatalf("cancel not reported: %#v", r)
	}
	if _, err = w.run(context.Background(), map[string]any{"command": "true", "workdir": t.TempDir()}); err == nil {
		t.Fatal("unscoped working dir accepted")
	}
}

func TestShellCanUseInstalledPythonAndWriteTable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX quoting fixture")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no installed Python; tool does not install dependencies")
	}
	w := testWorkspace(t)
	script := "import csv\nwith open('actual.csv', 'w', encoding='utf-8', newline='') as f:\n csv.writer(f).writerows([['city','value'],['甲',42]])\nprint('x'*100000)\nprint('finished-table')\n"
	if _, err = w.write(context.Background(), map[string]any{"path": "table.py", "content": script}); err != nil {
		t.Fatal(err)
	}
	r, err := w.run(context.Background(), map[string]any{"command": "'" + strings.ReplaceAll(python, "'", "'\\''") + "' table.py"})
	if err != nil {
		t.Fatal(err)
	}
	got := r.(map[string]any)
	if got["success"] != true || got["truncated"] != true || !strings.Contains(got["output"].(string), "finished-table") {
		t.Fatalf("Python result missing: %#v", got)
	}
	raw, err := os.ReadFile(filepath.Join(w.output, "actual.csv"))
	if err != nil || !strings.Contains(string(raw), "甲,42") {
		t.Fatalf("real CSV not written: %q %v", raw, err)
	}
}

func TestShippedFileToolDefinitionsExecuteThroughInternalBackend(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	tools := toolpkg.NewRegistry()
	if err := tools.LoadDirectories(filepath.Join(root, "autocarto", "tools", "system", "file-operations")); err != nil {
		t.Fatal(err)
	}
	registry := skill.NewRegistry(tools)
	if err := registry.LoadDirectories(filepath.Join(root, "autocarto", "skills", "system", "file-operations")); err != nil {
		t.Fatal(err)
	}
	engine := skill.NewEngine(registry, skill.NewInternalBackend(Invoke))
	ctx := toolsession.WithContext(context.Background(), "file-schema", filepath.Join(t.TempDir(), "project.qgz"), "")
	for _, name := range []string{ToolReadFile, ToolWriteFile, ToolRunCommand} {
		def, ok := registry.Get(name)
		if !ok || def.Metadata.Runtime != "internal" {
			t.Fatalf("tool not routed internally: %s", name)
		}
	}
	content := "城市,数值\n甲,42\n"
	result, err := engine.Execute(ctx, ToolWriteFile, map[string]any{"path": "from-schema.csv", "content": content})
	if err != nil {
		t.Fatal(err)
	}
	out := result.Output.(map[string]any)
	raw, err := os.ReadFile(out["path"].(string))
	if err != nil || string(raw) != content {
		t.Fatalf("mapping corrupted text: %q %v", raw, err)
	}
	result, err = engine.Execute(ctx, ToolReadFile, map[string]any{"path": out["path"]})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output.(map[string]any)["content"] != content {
		t.Fatalf("read mapping failed: %#v", result.Output)
	}
	if _, err = integer(map[string]any{"limit": json.Number("1.5")}, "limit", 1, 1, 20000); err == nil {
		t.Fatal("fractional limit accepted")
	}
	// Exercise the native Go result through the actual adapter, not a JSON
	// round trip that would silently convert []map into the []any envelope.
	def, _ := registry.Get(ToolReadFile)
	adapter := skill.NewEnhancedSkillAdapter(def, engine)
	sessionDir, err := config.ResolveSessionDir(toolsession.ProjectPathFromContext(ctx), "file-schema")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body string
		part       schema.ToolPartType
	}{
		{"table.png", "\x89PNG\r\n\x1a\nfixture", schema.ToolPartTypeImage},
		{"table.pdf", "%PDF-1.7\nfixture\n%%EOF", schema.ToolPartTypeFile},
	} {
		path := filepath.Join(sessionDir, "output", tc.name)
		writeFixture(t, path, tc.body)
		raw, _ := json.Marshal(map[string]any{"path": path})
		mediaResult, err := adapter.InvokableRun(ctx, &schema.ToolArgument{Text: string(raw)})
		if err != nil {
			t.Fatalf("native file_read media bridge failed: %v", err)
		}
		found := false
		for _, part := range mediaResult.Parts {
			if part.Type == tc.part {
				var data *string
				if part.Image != nil {
					data = part.Image.Base64Data
				}
				if part.File != nil {
					data = part.File.Base64Data
				}
				found = data != nil && *data == base64.StdEncoding.EncodeToString([]byte(tc.body))
			}
		}
		if !found {
			t.Fatalf("actual bytes missing from tool attachment: %#v", mediaResult)
		}
	}
}

func TestReadFileLongLineDoesNotReturnRepeatingCursor(t *testing.T) {
	w := testWorkspace(t)
	longLine := strings.Repeat("统计", maxTextBytes/3)
	writeFixture(t, filepath.Join(w.output, "large.html"), "header\n"+longLine+"\nend\n")
	first, err := w.read(context.Background(), map[string]any{"path": "large.html"})
	if err != nil {
		t.Fatal(err)
	}
	page := first.(map[string]any)
	if page["content"] != "header\n" || page["next_offset"] != 2 {
		t.Fatalf("partial line leaked into regular page: %#v", page)
	}
	second, err := w.read(context.Background(), map[string]any{"path": "large.html", "offset": page["next_offset"]})
	if err != nil {
		t.Fatal(err)
	}
	page = second.(map[string]any)
	if _, exists := page["next_offset"]; exists {
		t.Fatal("overlong line returned an unusable repeating cursor")
	}
	if page["continuation_requires_processing"] != true || page["truncated"] != true || !utf8.ValidString(page["content"].(string)) {
		t.Fatalf("unmarked or invalid UTF-8 preview: %#v", page)
	}
}

func TestBoundedCommandOutputPreservesHeadAndTail(t *testing.T) {
	var output boundedOutput
	data := "first\n" + strings.Repeat("x", 8*1024*1024) + "\nlast"
	if n, err := output.Write([]byte(data)); err != nil || n != len(data) {
		t.Fatal(n, err)
	}
	got, truncated, total := output.result()
	if !truncated || total != len(data) || !strings.HasPrefix(got, "first\n") || !strings.HasSuffix(got, "\nlast") || len(output.head)+len(output.tail) > maxTextBytes {
		t.Fatalf("invalid bounded output: truncated=%v bytes=%d retained=%d", truncated, total, len(got))
	}
	output.Write([]byte("final"))
	got, _, _ = output.result()
	if !strings.HasSuffix(got, "lastfinal") {
		t.Fatal("later output lost")
	}
}
