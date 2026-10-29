package fileops

import (
	"context"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDocumentExtractUsesTrustedSessionOutput(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	home := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", home)
	assets, err := filepath.Abs("../../../autocarto")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOCARTO_ASSETS_ROOT", assets)
	project := filepath.Join(t.TempDir(), "project.qgz")
	os.WriteFile(project, []byte("fixture"), 0600)
	ctx := toolsession.WithContext(context.Background(), "document-extract", project, "")
	ctx = WithDocumentOptions(ctx, DocumentOptions{Config: config.AcquisitionConfig{DocumentPython: python}})
	w, err := sessionWorkspace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(w.output, "table.html")
	os.MkdirAll(w.output, 0700)
	os.WriteFile(source, []byte("<table><tr><th>A</th><th>B</th></tr><tr><td>x</td><td>42</td></tr></table>"), 0600)
	result, err := Invoke(ctx, ToolExtractDocument, map[string]any{"path": source})
	if err != nil {
		t.Fatal(err)
	}
	table := result.(map[string]any)["tables"].([]any)[0].(map[string]any)
	outputRoot, err := canonical(w.output)
	if err != nil {
		t.Fatal(err)
	}
	if !within(outputRoot, table["path"].(string)) {
		t.Fatal("derived document escaped output")
	}
	downloadCalls := 0
	ctx = WithDocumentOptions(ctx, DocumentOptions{Config: config.AcquisitionConfig{DocumentPython: python}, Download: func(_ context.Context, url string) (map[string]any, error) {
		downloadCalls++
		if url != "https://example.org/table" {
			t.Fatal(url)
		}
		return map[string]any{"path": source, "source_url": url}, nil
	}})
	if _, err = Invoke(ctx, ToolExtractDocument, map[string]any{"path": "https://example.org/table"}); err != nil || downloadCalls != 1 {
		t.Fatalf("URL did not reuse download: %v", err)
	}
	if _, err = Invoke(ctx, ToolExtractDocument, map[string]any{"path": source, "output_dir": t.TempDir()}); err == nil {
		t.Fatal("outside write accepted")
	}
}
