package fileops

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxTextBytes = 64 * 1024
const maxMediaBytes = 50 * 1024 * 1024

func integer(args map[string]any, name string, fallback, min, max int) (int, error) {
	v, ok := args[name]
	if !ok || v == nil || v == "" {
		return fallback, nil
	}
	var n float64
	switch x := v.(type) {
	case int:
		n = float64(x)
	case float64:
		n = x
	case json.Number:
		var err error
		n, err = x.Float64()
		if err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("%s 必须是整数", name)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) || n < float64(min) || n > float64(max) {
		return 0, fmt.Errorf("%s 必须为 %d–%d 的整数", name, min, max)
	}
	return int(n), nil
}

func (w workspace) read(ctx context.Context, args map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := w.resolve(stringArg(args, "path"), false)
	if err != nil {
		return nil, err
	}
	offset, err := integer(args, "offset", 1, 1, 10000000)
	if err != nil {
		return nil, err
	}
	limit, err := integer(args, "limit", 2000, 1, 20000)
	if err != nil {
		return nil, err
	}
	asText := false
	if value, exists := args["as_text"]; exists && value != nil && value != "" {
		var ok bool
		asText, ok = value.(bool)
		if !ok {
			return nil, fmt.Errorf("as_text 必须为布尔值")
		}
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(p)
		if err != nil {
			return nil, err
		}
		start := min(offset-1, len(entries))
		end := min(start+limit, len(entries))
		out := []map[string]any{}
		for _, e := range entries[start:end] {
			out = append(out, map[string]any{"name": e.Name(), "is_directory": e.IsDir(), "path": filepath.Join(p, e.Name())})
		}
		return map[string]any{"success": true, "path": p, "kind": "directory", "entries": out, "truncated": end < len(entries), "next_offset": end + 1}, nil
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("只能读取普通文件或目录")
	}
	sample := make([]byte, 512)
	n, err := f.Read(sample)
	if err != nil && err != io.EOF {
		return nil, err
	}
	sample = sample[:n]
	mime := http.DetectContentType(sample)
	mediaType := ""
	if bytes.HasPrefix(sample, []byte("%PDF-")) {
		mime = "application/pdf"
		mediaType = "file"
	}
	if mime == "image/png" || mime == "image/jpeg" || mime == "image/gif" || mime == "image/webp" {
		mediaType = "image"
	}
	if mediaType != "" && !asText {
		if info.Size() > maxMediaBytes {
			return nil, fmt.Errorf("图片/PDF 超过 50 MiB，请使用本机处理工具分割后再读取")
		}
		return map[string]any{"success": true, "path": p, "kind": mediaType, "mime_type": mime, "bytes": info.Size(), "_agent_media": []any{map[string]any{"type": mediaType, "path": p, "mime_type": mime, "label": filepath.Base(p), "filename": filepath.Base(p)}}}, nil
	}
	var reader io.Reader = f
	ext := strings.ToLower(filepath.Ext(p))
	if ext == ".docx" || ext == ".xlsx" || ext == ".pdf" && asText {
		text, err := readDocumentText(ctx, p)
		if err != nil {
			return nil, err
		}
		reader = strings.NewReader(text)
	} else {
		if mediaType != "" && asText {
			return nil, fmt.Errorf("该媒体不能直接转换为文本；请使用支持此媒体的模型或 OCR 工具")
		}
		if bytes.ContainsRune(sample, '\x00') {
			return nil, fmt.Errorf("这是二进制文件，不能当文本读取；请用本机可用的解析工具处理")
		}
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var out strings.Builder
	line, count, truncated := 0, 0, false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line++
		if line < offset {
			continue
		}
		if count >= limit {
			truncated = true
			break
		}
		value := scanner.Text()
		if !utf8.ValidString(value) {
			return nil, fmt.Errorf("文件不是 UTF-8 文本，请用本机解析工具按原编码读取")
		}
		remaining := maxTextBytes - out.Len()
		if len(value)+1 > remaining {
			// Return complete lines whenever possible. A single overlong line
			// has no usable line cursor, so expose only a clearly marked preview.
			if count == 0 {
				cut := max(remaining-1, 0)
				for cut > 0 && !utf8.ValidString(value[:cut]) {
					cut--
				}
				out.WriteString(value[:cut])
			}
			truncated = true
			break
		}
		out.WriteString(value)
		out.WriteByte('\n')
		count++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("文本读取失败（单行最大 1 MiB；更大文件可用本机处理工具分页）: %w", err)
	}
	result := map[string]any{"success": true, "path": p, "kind": "text", "content": out.String(), "offset": offset, "lines": count, "truncated": truncated}
	if truncated {
		if count > 0 {
			result["next_offset"] = offset + count
			result["message"] = "已达到行数或 64 KiB 输出上限；可按 next_offset 继续"
		} else {
			result["continuation_requires_processing"] = true
			result["message"] = "当前单行超过 64 KiB，仅返回开头预览；没有可继续的行游标，请用本机处理工具分段或解析后读取"
		}
	}
	return result, nil
}

func (w workspace) write(ctx context.Context, args map[string]any) (any, error) {
	p, err := w.resolve(stringArg(args, "path"), true)
	if err != nil {
		return nil, err
	}
	content, ok := args["content"].(string)
	if !ok {
		return nil, fmt.Errorf("content 必须是文本字符串")
	}
	if len(content) > 16*1024*1024 {
		return nil, fmt.Errorf("单次写入文本超过 16 MiB")
	}
	if !utf8.ValidString(content) {
		return nil, fmt.Errorf("content 必须是 UTF-8 文本")
	}
	overwrite := true
	if value, exists := args["overwrite"]; exists && value != nil && value != "" {
		var valid bool
		overwrite, valid = value.(bool)
		if !valid {
			return nil, fmt.Errorf("overwrite 必须为布尔值")
		}
	}
	_, statErr := os.Stat(p)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}
	if exists && !overwrite {
		return nil, fmt.Errorf("目标文件已存在，overwrite=false")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return nil, err
	}
	// Recheck after creating parents before opening anything for writing.
	if p, err = w.resolve(p, true); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".autocarto-write-*")
	if err != nil {
		return nil, err
	}
	tempPath := tmp.Name()
	defer os.Remove(tempPath)
	if _, err = tmp.WriteString(content); err != nil {
		tmp.Close()
		return nil, err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return nil, err
	}
	if err = tmp.Close(); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if !overwrite {
		// Link creates a destination only if it is still absent, avoiding a race
		// where a new file appears between the existence check and publication.
		err = os.Link(tempPath, p)
	} else {
		err = os.Rename(tempPath, p)
	}
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(content))
	return map[string]any{"success": true, "path": p, "bytes": len(content), "sha256": hex.EncodeToString(sum[:]), "overwritten": exists, "artifact_paths": []string{p}}, nil
}
