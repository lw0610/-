package browserruntime

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPWireImagePersistsDecodedBytes(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	opts := fakeOptions(t, "media-wire")
	r := callFake(t, m, opts, "browser_screenshot")
	items := r["_agent_media"].([]any)
	if len(items) != 1 {
		t.Fatal("MCP image did not reach media envelope")
	}
	p := items[0].(map[string]any)["path"].(string)
	data, err := os.ReadFile(p)
	if err != nil || string(data) != "\x89PNG\r\n\x1a\nMCP-image" {
		t.Fatalf("SDK image was not decoded once: %q %v", data, err)
	}
}

func TestMCPMediaSurvivesTextTruncationWithoutBase64JSON(t *testing.T) {
	image := []byte("\x89PNG\r\n\x1a\nreal-image-bytes")
	pdf := []byte("%PDF-1.7\nreal-pdf-bytes\n%%EOF")
	dir := t.TempDir()
	result, err := readableResult(&mcp.CallToolResult{Content: []mcp.Content{
		&mcp.TextContent{Text: strings.Repeat("a", maxTextBytes+10)},
		&mcp.ImageContent{Data: image, MIMEType: "image/png"},
		&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: "https://example.org/table.pdf", MIMEType: "application/pdf", Blob: pdf}},
	}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	r := result.(map[string]any)
	media := r["_agent_media"].([]any)
	if len(media) != 2 || r["truncated"] != true {
		t.Fatalf("missing media/truncation: %#v", r)
	}
	for i, want := range [][]byte{image, pdf} {
		item := media[i].(map[string]any)
		path := item["path"].(string)
		if filepath.Dir(path) != dir {
			t.Fatalf("media escaped: %s", path)
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("media bytes changed: %v", err)
		}
		if (i == 0 && item["type"] != "image") || (i == 1 && item["type"] != "file") {
			t.Fatalf("incorrect media kind: %#v", item)
		}
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{image, pdf} {
		if bytes.Contains(raw, []byte(base64.StdEncoding.EncodeToString(data))) {
			t.Fatal("binary media leaked into text JSON")
		}
	}
}

func TestMCPInvalidMediaDoesNotLeavePartialAttachments(t *testing.T) {
	dir := t.TempDir()
	_, err := readableResult(&mcp.CallToolResult{Content: []mcp.Content{
		&mcp.ImageContent{Data: []byte("\x89PNG\r\n\x1a\nvalid"), MIMEType: "image/png"},
		&mcp.ImageContent{Data: []byte("not an image"), MIMEType: "image/png"},
	}}, dir)
	if err == nil {
		t.Fatal("invalid image was accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial media persisted: %v %v", entries, err)
	}
	if _, err := saveMedia([]byte("%PDF-1.7\n"), "image/png", "bad", dir); err == nil {
		t.Fatal("MIME mismatch accepted")
	}
}
