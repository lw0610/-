package fileops

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/autocarto/carto_agent/internal/shared/xlsx"
)

const maxDocumentTextBytes = 16 << 20

// ReadText reads the complete, authorized source text. It never substitutes a
// task description for file contents or returns a silently truncated document.
// Images and scanned PDFs require a separate media/OCR-capable reader.
func ReadText(ctx context.Context, path string) (string, error) {
	w, err := sessionWorkspace(ctx)
	if err != nil {
		return "", err
	}
	p, err := w.resolve(path, false)
	if err != nil {
		return "", err
	}
	return readDocumentText(ctx, p)
}

func readDocumentText(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("正文读取需要普通文件；目录请先用 file_read 列举，再选择其中的文件")
	}
	if info.Size() > maxMediaBytes {
		return "", fmt.Errorf("文档超过 50 MiB，未读取；请先分割或使用专用解析工具")
	}
	var text string
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx":
		text, err = readDOCXText(ctx, path)
	case ".xlsx":
		text, err = readXLSXText(ctx, path)
	case ".pdf":
		text, err = readPDFText(ctx, path)
	default:
		var f *os.File
		f, err = os.Open(path)
		if err == nil {
			var raw []byte
			raw, err = io.ReadAll(io.LimitReader(f, maxDocumentTextBytes+1))
			f.Close()
			if bytes.ContainsRune(raw, '\x00') {
				err = fmt.Errorf("该文件不是 UTF-8 文本或可解析的 DOCX/XLSX/PDF；请使用相应格式解析器，图片需要视觉或 OCR 能力")
			} else {
				text = string(raw)
			}
		}
	}
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if len(text) > maxDocumentTextBytes {
		return "", fmt.Errorf("文档正文超过 16 MiB，未返回不完整内容；请先分割文档")
	}
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("解析结果不是 UTF-8 文本")
	}
	if strings.TrimSpace(strings.TrimPrefix(text, "\ufeff")) == "" {
		return "", fmt.Errorf("文件没有可提取的正文；扫描件或仅含图片的文档需要视觉或 OCR 能力")
	}
	return text, nil
}

func readDOCXText(ctx context.Context, path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("DOCX 文件无效：%w", err)
	}
	defer zr.Close()
	r, err := zr.Open("word/document.xml")
	if err != nil {
		return "", fmt.Errorf("DOCX 正文读取失败：%w", err)
	}
	defer r.Close()
	raw, err := io.ReadAll(io.LimitReader(r, maxDocumentTextBytes+1))
	if err != nil || len(raw) > maxDocumentTextBytes {
		return "", fmt.Errorf("DOCX 正文 XML 无法完整读取或超过 16 MiB：%v", err)
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var out strings.Builder
	textDepth, deletedDepth, bodyDepth := 0, 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("DOCX 正文 XML 无效：%w", err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			if !wordElement(value.Name) {
				continue
			}
			switch value.Name.Local {
			case "body":
				bodyDepth++
			case "del":
				deletedDepth++
			case "t":
				textDepth++
			case "tab":
				if bodyDepth > 0 && deletedDepth == 0 {
					out.WriteByte('\t')
				}
			case "br", "cr":
				if bodyDepth > 0 && deletedDepth == 0 {
					out.WriteByte('\n')
				}
			}
		case xml.CharData:
			if bodyDepth > 0 && textDepth > 0 && deletedDepth == 0 {
				out.Write(value)
			}
		case xml.EndElement:
			if !wordElement(value.Name) {
				continue
			}
			switch value.Name.Local {
			case "body":
				bodyDepth--
			case "del":
				deletedDepth--
			case "t":
				textDepth--
			case "p", "tr":
				if bodyDepth > 0 && deletedDepth == 0 {
					out.WriteByte('\n')
				}
			case "tc":
				if bodyDepth > 0 && deletedDepth == 0 {
					out.WriteByte('\t')
				}
			}
		}
	}
	return out.String(), nil
}

func wordElement(name xml.Name) bool {
	return name.Space == "http://schemas.openxmlformats.org/wordprocessingml/2006/main" || name.Space == "http://purl.oclc.org/ooxml/wordprocessingml/main"
}

func readXLSXText(ctx context.Context, path string) (string, error) {
	book, err := xlsx.Open(path)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	hasValue := false
	for _, sheet := range book.Sheets {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		_, cells, err := book.ReadSheet(sheet)
		if err != nil {
			return "", fmt.Errorf("工作表 %s 无法读取：%w", sheet.Name, err)
		}
		out.WriteString("工作表：" + sheet.Name + "\n")
		rows := make([]int, 0, len(cells))
		for row := range cells {
			rows = append(rows, row)
		}
		sort.Ints(rows)
		writer := csv.NewWriter(&out)
		writer.Comma = '\t'
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			maxColumn := 0
			for column := range cells[row] {
				maxColumn = max(maxColumn, column)
			}
			values := make([]string, maxColumn+1)
			values[0] = "行 " + strconv.Itoa(row)
			for column, value := range cells[row] {
				values[column] = value
				hasValue = hasValue || strings.TrimSpace(value) != ""
			}
			if err := writer.Write(values); err != nil {
				return "", err
			}
			writer.Flush()
			if err := writer.Error(); err != nil {
				return "", err
			}
			if out.Len() > maxDocumentTextBytes {
				return "", fmt.Errorf("Excel 正文超过 16 MiB，请按工作表或区域分段读取")
			}
		}
		out.WriteByte('\n')
	}
	if !hasValue {
		return "", fmt.Errorf("Excel 没有可读取的单元格值；公式需先在表格软件中计算并保存缓存值")
	}
	return out.String(), nil
}

type documentOutput struct {
	bytes.Buffer
	exceeded bool
}

func (b *documentOutput) Write(p []byte) (int, error) {
	if len(p) > maxDocumentTextBytes-b.Len() {
		b.exceeded = true
		return 0, fmt.Errorf("PDF 正文超过 16 MiB")
	}
	return b.Buffer.Write(p)
}

func readPDFText(ctx context.Context, path string) (string, error) {
	program, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", fmt.Errorf("本机未安装 pdftotext，不能提取 PDF 正文；请使用支持 PDF 的媒体读取能力或安装本机解析工具")
	}
	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(readCtx, program, "-layout", "-enc", "UTF-8", path, "-")
	var output documentOutput
	var stderr boundedOutput
	command.Stdout, command.Stderr = &output, &stderr
	err = command.Run()
	if readCtx.Err() != nil {
		return "", fmt.Errorf("PDF 正文提取取消或超时：%w", readCtx.Err())
	}
	if output.exceeded {
		return "", fmt.Errorf("PDF 正文超过 16 MiB，未返回不完整内容；请先分割 PDF")
	}
	if err != nil {
		detail, _, _ := stderr.result()
		return "", fmt.Errorf("PDF 正文提取失败：%w；%s", err, strings.TrimSpace(detail))
	}
	return output.String(), nil
}
