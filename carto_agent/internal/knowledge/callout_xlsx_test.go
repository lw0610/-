package knowledge

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func calloutTestWorkbook(t *testing.T, entries map[string][]byte) string {
	t.Helper()
	var raw bytes.Buffer
	w := zip.NewWriter(&raw)
	var names []string
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		file, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(entries[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "注记.xlsx")
	if err := os.WriteFile(source, raw.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func calloutTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func calloutTestEntries() map[string][]byte {
	return map[string][]byte{
		"xl/workbook.xml":                     []byte(`<workbook xmlns:r="urn:relationships"><sheets><sheet name="图文标注" r:id="sheet"/></sheets></workbook>`),
		"xl/_rels/workbook.xml.rels":          []byte(`<Relationships><Relationship Id="sheet" Type="urn:relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="strings" Type="urn:relationships/sharedStrings" Target="sharedStrings.xml"/></Relationships>`),
		"xl/sharedStrings.xml":                []byte(`<sst><si><t>地点</t></si><si><r><t>正</t></r><r><t>文</t></r></si><si><t>图片</t></si><si><r><t>何家</t></r><r><t>冲</t></r></si><si><t>正文第一行&#10;正文第二行</t></si><si><t>纪念设施照片</t></si></sst>`),
		"xl/worksheets/sheet1.xml":            []byte(`<worksheet xmlns:r="urn:relationships"><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c></row><row r="2"><c r="A2" t="s"><v>3</v></c><c r="B2" t="s"><v>4</v></c><c r="C2" t="s"><v>5</v></c></row><row r="4"><c r="A4" t="inlineStr"><is><r><t>枣阳</t></r><r><t>保安寨</t></r></is></c><c r="B4" t="inlineStr"><is><t>第二段正文</t></is></c><c r="C4" t="inlineStr"><is><t>images/第二张.png</t></is></c></row></sheetData><drawing r:id="drawing"/></worksheet>`),
		"xl/worksheets/_rels/sheet1.xml.rels": []byte(`<Relationships><Relationship Id="drawing" Type="urn:relationships/drawing" Target="../drawings/drawing1.xml"/></Relationships>`),
		"xl/drawings/_rels/drawing1.xml.rels": []byte(`<Relationships><Relationship Id="pic1" Type="urn:relationships/image" Target="../media/photo.png"/><Relationship Id="pic2" Type="urn:relationships/image" Target="../media/second.png"/></Relationships>`),
		"xl/drawings/drawing1.xml":            []byte(`<xdr:wsDr xmlns:xdr="urn:drawing" xmlns:a="urn:main" xmlns:r="urn:relationships"><xdr:oneCellAnchor><xdr:from><xdr:col>2</xdr:col><xdr:row>1</xdr:row></xdr:from><xdr:pic><xdr:blipFill><a:blip r:embed="pic1"/></xdr:blipFill></xdr:pic></xdr:oneCellAnchor></xdr:wsDr>`),
	}
}

func TestReadCalloutWorkbookTextAndEmbeddedImages(t *testing.T) {
	entries := calloutTestEntries()
	photo := calloutTestPNG(t)
	entries["xl/media/photo.png"] = photo
	rows, warnings, err := readCalloutWorkbook(calloutTestWorkbook(t, entries))
	if err != nil || len(warnings) != 0 || len(rows) != 2 {
		t.Fatalf("rows=%d warnings=%v err=%v", len(rows), warnings, err)
	}
	first := rows[0]
	if first.Sheet != "图文标注" || first.Row != 2 || first.Place != "何家冲" || first.Body != "正文第一行\n正文第二行" || first.Caption != "纪念设施照片" || !bytes.Equal(first.Image, photo) {
		t.Fatalf("unexpected first row: %+v", first)
	}
	second := rows[1]
	if second.Row != 4 || second.Place != "枣阳保安寨" || second.ImagePath != "images/第二张.png" || second.Caption != "" || len(second.Image) != 0 {
		t.Fatalf("unexpected sparse inline row: %+v", second)
	}
}

func TestReadCalloutWorkbookPrefersNamedSheet(t *testing.T) {
	entries := calloutTestEntries()
	entries["xl/media/photo.png"] = calloutTestPNG(t)
	entries["xl/workbook.xml"] = []byte(`<workbook xmlns:r="urn:relationships"><sheets><sheet name="说明页" r:id="first"/><sheet name="图文标注" r:id="sheet"/></sheets></workbook>`)
	entries["xl/_rels/workbook.xml.rels"] = bytes.Replace(entries["xl/_rels/workbook.xml.rels"], []byte(`</Relationships>`), []byte(`<Relationship Id="first" Type="urn:relationships/worksheet" Target="worksheets/first.xml"/></Relationships>`), 1)
	entries["xl/worksheets/first.xml"] = bytes.Replace(entries["xl/worksheets/sheet1.xml"], []byte(`<drawing r:id="drawing"/>`), nil, 1)
	rows, _, err := readCalloutWorkbook(calloutTestWorkbook(t, entries))
	if err != nil || len(rows) != 2 || rows[0].Sheet != "图文标注" || len(rows[0].Image) == 0 {
		t.Fatalf("named sheet not selected: rows=%v err=%v", rows, err)
	}
}

func TestReadCalloutWorkbookImageProblemsPreserveText(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt", "multiple", "two-cell", "missing-drawing", "broken-drawing"} {
		t.Run(mode, func(t *testing.T) {
			entries := calloutTestEntries()
			entries["xl/media/photo.png"] = calloutTestPNG(t)
			switch mode {
			case "missing":
				delete(entries, "xl/media/photo.png")
			case "corrupt":
				entries["xl/media/photo.png"] = []byte("broken png")
			case "multiple":
				entries["xl/drawings/drawing1.xml"] = bytes.Replace(entries["xl/drawings/drawing1.xml"], []byte(`</xdr:wsDr>`), []byte(`<xdr:oneCellAnchor><xdr:from><xdr:row>1</xdr:row></xdr:from><xdr:pic><xdr:blipFill><a:blip r:embed="pic2"/></xdr:blipFill></xdr:pic></xdr:oneCellAnchor></xdr:wsDr>`), 1)
			case "two-cell":
				entries["xl/drawings/drawing1.xml"] = bytes.ReplaceAll(entries["xl/drawings/drawing1.xml"], []byte("oneCellAnchor"), []byte("twoCellAnchor"))
				entries["xl/drawings/drawing1.xml"] = bytes.Replace(entries["xl/drawings/drawing1.xml"], []byte(`</xdr:from>`), []byte(`</xdr:from><xdr:to><xdr:row>4</xdr:row></xdr:to>`), 1)
			case "missing-drawing":
				delete(entries, "xl/drawings/drawing1.xml")
			case "broken-drawing":
				entries["xl/drawings/drawing1.xml"] = []byte("<wsDr><")
			}
			rows, warnings, err := readCalloutWorkbook(calloutTestWorkbook(t, entries))
			if err != nil || len(rows) != 2 || rows[0].Body == "" || len(warnings) == 0 {
				t.Fatalf("text not preserved: rows=%v warnings=%v err=%v", rows, warnings, err)
			}
			if mode == "multiple" || mode == "two-cell" {
				if len(rows[0].Image) == 0 {
					t.Fatal("first image lost")
				}
			} else if len(rows[0].Image) != 0 {
				t.Fatal("invalid image accepted")
			}
		})
	}
}

func TestReadCalloutWorkbookRejectsUnsafeOrInvalidInput(t *testing.T) {
	for _, mode := range []string{"zip-traversal", "relationship-traversal", "external", "encoded-traversal", "invalid-xml", "invalid-string", "missing-headers", "xls", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			entries := calloutTestEntries()
			switch mode {
			case "zip-traversal":
				entries["../outside"] = []byte("outside")
			case "relationship-traversal", "external", "encoded-traversal":
				target := "../../../outside.png"
				if mode == "external" {
					target = "https://example.invalid/image.png"
				} else if mode == "encoded-traversal" {
					target = "%2e%2e/%2e%2e/%2e%2e/outside.png"
				}
				entries["xl/drawings/_rels/drawing1.xml.rels"] = []byte(fmt.Sprintf(`<Relationships><Relationship Id="pic1" Type="urn:image" Target="%s"/></Relationships>`, target))
			case "invalid-xml":
				entries["xl/workbook.xml"] = []byte("<broken>")
			case "invalid-string":
				entries["xl/worksheets/sheet1.xml"] = bytes.Replace(entries["xl/worksheets/sheet1.xml"], []byte("<v>3</v>"), []byte("<v>99999</v>"), 1)
			case "missing-headers":
				entries["xl/sharedStrings.xml"] = bytes.Replace(entries["xl/sharedStrings.xml"], []byte("地点"), []byte("未知列"), 1)
			}
			source := calloutTestWorkbook(t, entries)
			if mode == "xls" {
				old := source
				source += ".xls"
				if err := os.Rename(old, source); err != nil {
					t.Fatal(err)
				}
			} else if mode == "symlink" {
				link := filepath.Join(t.TempDir(), "link.xlsx")
				if err := os.Symlink(source, link); err != nil {
					t.Fatal(err)
				}
				source = link
			}
			_, _, err := readCalloutWorkbook(source)
			if !errors.Is(err, ErrReviewInvalid) {
				t.Fatalf("expected ErrReviewInvalid, got %v", err)
			}
		})
	}
}

func TestReadCalloutWorkbookRejectsDecompressionLimit(t *testing.T) {
	var raw bytes.Buffer
	w := zip.NewWriter(&raw)
	_, err := w.CreateRaw(&zip.FileHeader{Name: "large.xml", Method: zip.Store, UncompressedSize64: maxCalloutWorkbookBytes + 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "large.xlsx")
	if err = os.WriteFile(source, raw.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = readCalloutWorkbook(source)
	if !errors.Is(err, ErrReviewInvalid) || !strings.Contains(err.Error(), "100 MiB") {
		t.Fatalf("unexpected size result: %v", err)
	}
}

func TestReadCalloutWorkbookActualExample(t *testing.T) {
	source := os.Getenv("CALLOUT_XLSX_TEST_FILE")
	if source == "" {
		t.Skip("设置 CALLOUT_XLSX_TEST_FILE 后验证固定原始工作簿；合成夹具已覆盖读取器")
	}
	rows, warnings, err := readCalloutWorkbook(source)
	if err != nil {
		t.Fatal(err)
	}
	images := 0
	for _, row := range rows {
		if len(row.Image) > 0 {
			images++
		}
		if row.Sheet != "图文标注" || row.Place == "" || row.Body == "" {
			t.Fatalf("incomplete row %d", row.Row)
		}
	}
	if len(rows) != 29 || images != 27 || len(warnings) != 0 {
		t.Fatalf("actual workbook: rows=%d images=%d warnings=%v", len(rows), images, warnings)
	}
}
