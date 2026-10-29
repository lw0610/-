package skill

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestMediaEnvelopeCarriesPDFBytesAndProvenance(t *testing.T) {
	data := []byte("%PDF-1.7\nreal-statistical-table\n%%EOF")
	path := filepath.Join(t.TempDir(), "table.pdf")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := toolResultWithMedia(map[string]any{"source_url": "https://example.org/table.pdf", agentMediaKey: []any{map[string]any{"type": "file", "path": path, "mime_type": "application/pdf", "filename": "人口表.pdf", "label": "统计表原件"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Parts) != 3 || r.Parts[2].Type != schema.ToolPartTypeFile || r.Parts[2].File == nil {
		t.Fatalf("PDF was not attached: %#v", r)
	}
	file := r.Parts[2].File
	if file.MIMEType != "application/pdf" || file.Base64Data == nil || *file.Base64Data != base64.StdEncoding.EncodeToString(data) || file.Extra["filename"] != "人口表.pdf" {
		t.Fatalf("wrong file metadata: %#v", file)
	}
	if strings.Contains(r.Parts[0].Text, agentMediaKey) || strings.Contains(r.Parts[0].Text, *file.Base64Data) || !strings.Contains(r.Parts[0].Text, "source_url") {
		t.Fatal("lost source or included binary in JSON")
	}
}

func TestMediaEnvelopeRejectsMislabelledAndUnsupportedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "table.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.7\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, item := range []map[string]any{
		{"type": "image", "path": path},
		{"type": "file", "path": path, "mime_type": "image/png"},
		{"type": "file", "path": "relative.pdf"},
	} {
		if _, err := toolResultWithMedia(map[string]any{agentMediaKey: []any{item}}); err == nil {
			t.Fatalf("invalid media accepted: %#v", item)
		}
	}
	if err := os.WriteFile(path, []byte("not a PDF"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := toolResultWithMedia(map[string]any{agentMediaKey: []any{map[string]any{"type": "file", "path": path}}}); err == nil {
		t.Fatal("text accepted as PDF")
	}
}
