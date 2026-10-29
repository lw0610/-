package fileops

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/autocarto/carto_agent/internal/toolsession"
)

func TestReadTextProvidedDirectoryAndCompleteSource(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "project.qgz")
	writeFixture(t, project, "fixture")
	ctx := toolsession.WithContext(context.Background(), "document-input", project, "")
	store, err := sessionfiles.New(project, "document-input")
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "selected")
	path := filepath.Join(dir, "nested", "史料.txt")
	body := strings.Repeat("真实原文，保留全部内容。\r\n", 4000) + "最后一行"
	writeFixture(t, path, body)
	neighbor := filepath.Join(parent, "other.txt")
	writeFixture(t, neighbor, "unselected")
	if _, err = ReadText(ctx, path); err == nil {
		t.Fatal("unselected text readable")
	}
	if err = store.RegisterProvidedDirectory(dir); err != nil {
		t.Fatal(err)
	}
	text, err := ReadText(ctx, path)
	if err != nil || text != body {
		t.Fatalf("full source lost: bytes=%d expected=%d err=%v", len(text), len(body), err)
	}
	for _, forbidden := range []string{parent, neighbor} {
		if _, err = ReadText(ctx, forbidden); err == nil {
			t.Fatalf("unauthorized input read: %s", forbidden)
		}
	}
	listing, err := Invoke(ctx, ToolReadFile, map[string]any{"path": dir})
	if err != nil || listing.(map[string]any)["kind"] != "directory" {
		t.Fatalf("cannot list selected directory: %v", err)
	}
	page, err := Invoke(ctx, ToolReadFile, map[string]any{"path": path, "limit": 1})
	if err != nil || page.(map[string]any)["next_offset"] != 2 {
		t.Fatalf("cannot read child page: %v", err)
	}
	if _, err = Invoke(ctx, ToolWriteFile, map[string]any{"path": path, "content": "changed"}); err == nil {
		t.Fatal("read grant authorized a write")
	}
	if err = os.Symlink(neighbor, filepath.Join(dir, "escape.txt")); err == nil {
		if _, err = ReadText(ctx, filepath.Join(dir, "escape.txt")); err == nil {
			t.Fatal("symlink escaped selected directory")
		}
	}
}

func documentZIPFixture(t *testing.T, path string, members map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, text := range members {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileReadDOCXAndXLSXRealText(t *testing.T) {
	w := testWorkspace(t)
	docx := filepath.Join(w.output, "source.docx")
	documentZIPFixture(t, docx, map[string]string{"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>红二十五军</w:t></w:r></w:p><w:p><w:del><w:r><w:t>已删除文字</w:t></w:r></w:del><w:r><w:t>1934 年出发</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>地点</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>湖北</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body></w:document>`})
	result, err := w.read(context.Background(), map[string]any{"path": docx, "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	page := result.(map[string]any)
	if page["content"] != "红二十五军\n" || page["next_offset"] != 2 {
		t.Fatalf("DOCX paging failed: %#v", page)
	}
	text, err := readDocumentText(context.Background(), docx)
	if err != nil || !strings.Contains(text, "湖北") || strings.Contains(text, "已删除文字") {
		t.Fatalf("DOCX content wrong: %q %v", text, err)
	}
	xlsxPath := filepath.Join(w.output, "source.xlsx")
	documentZIPFixture(t, xlsxPath, map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="人口" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>城市</t></is></c><c r="C1" t="inlineStr"><is><t>人数</t></is></c></row><row r="3"><c r="A3" t="inlineStr"><is><t>甲市</t></is></c><c r="C3"><f>20+22</f><v>42</v></c></row></sheetData></worksheet>`,
	})
	result, err = w.read(context.Background(), map[string]any{"path": xlsxPath})
	if err != nil {
		t.Fatal(err)
	}
	text = result.(map[string]any)["content"].(string)
	if !strings.Contains(text, "工作表：人口") || !strings.Contains(text, "行 3\t甲市\t\t42") {
		t.Fatalf("XLSX values/coordinates lost: %q", text)
	}
	broken := filepath.Join(w.output, "broken.docx")
	writeFixture(t, broken, "not a document")
	if _, err = readDocumentText(context.Background(), broken); err == nil {
		t.Fatal("malformed DOCX accepted")
	}
}

func TestPDFTextCapabilityAndMediaFallback(t *testing.T) {
	w := testWorkspace(t)
	path := filepath.Join(w.output, "source.pdf")
	writeFixture(t, path, "%PDF-1.7\nfixture")
	t.Setenv("PATH", t.TempDir())
	if _, err := w.read(context.Background(), map[string]any{"path": path, "as_text": true}); err == nil || !strings.Contains(err.Error(), "pdftotext") {
		t.Fatalf("missing PDF parser not reported: %v", err)
	}
	media, err := w.read(context.Background(), map[string]any{"path": path})
	if err != nil || media.(map[string]any)["_agent_media"] == nil {
		t.Fatalf("default PDF media path broken: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	program := filepath.Join(t.TempDir(), "pdftotext")
	if err = os.WriteFile(program, []byte("#!/bin/sh\nprintf 'PDF extracted text\\nsecond line\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(program))
	text, err := readDocumentText(context.Background(), path)
	if err != nil || text != "PDF extracted text\nsecond line\n" {
		t.Fatalf("PDF parser output not used: %q %v", text, err)
	}
	page, err := w.read(context.Background(), map[string]any{"path": path, "as_text": true, "offset": 2, "limit": 1})
	if err != nil || page.(map[string]any)["content"] != "second line\n" {
		t.Fatalf("PDF text pagination failed: %v", err)
	}
}
