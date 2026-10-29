package browserruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/autocarto/carto_agent/internal/toolresult"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxTextBytes = 48 * 1024

type boundedBuffer struct {
	mu    sync.Mutex
	data  []byte
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	length := len(p)
	if len(p) > b.limit {
		p = p[len(p)-b.limit:]
	}
	if excess := len(b.data) + len(p) - b.limit; excess > 0 {
		b.data = b.data[excess:]
	}
	b.data = append(b.data, p...)
	return length, nil
}

func (b *boundedBuffer) String() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.ToValidUTF8(string(b.data), "")
}

func clip(s string, size int) (string, bool) {
	if len(s) <= size {
		return s, false
	}
	if size <= 0 {
		return "", true
	}
	for size > 0 && !utf8.RuneStart(s[size]) {
		size--
	}
	return s[:size], true
}

func listTools(ctx context.Context, session *mcp.ClientSession, outputDir string) (any, error) {
	tools := make([]map[string]any, 0)
	total := 0
	truncated := false
	for t, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		if len(tools) >= 128 {
			truncated = true
			break
		}
		schema, err := json.Marshal(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("读取浏览器工具参数: %w", err)
		}
		description, shortened := clip(t.Description, 1500)
		if len(schema) > 16*1024 || total+len(schema)+len(description) > 96*1024 {
			truncated = true
			break // Never return an incomplete argument schema as if valid.
		}
		truncated = truncated || shortened
		total += len(schema) + len(description)
		tools = append(tools, map[string]any{
			"name": t.Name, "description": description, "inputSchema": t.InputSchema,
			"call_via": "browser_call_tool",
			"invocation_template": map[string]any{
				"name":      "browser_call_tool",
				"arguments": map[string]any{"tool_name": t.Name, "args": map[string]any{}},
			},
		})
	}
	return map[string]any{
		"tools": tools, "truncated": truncated, "output_dir": outputDir,
		"invocation_tool": "browser_call_tool", "direct_invocation": false,
		"invocation_instructions": "tools[].name 是浏览器 MCP 内部操作，不能直接作为智能体工具调用。必须使用 browser_call_tool(tool_name, args)。每条 invocation_template 的 name 是实际调用工具，arguments 是外层参数；其中 args={} 是待填写模板，须按对应 inputSchema 填入必填字段，无参数时才保留 {}。",
	}, nil
}

func validateToolArguments(args map[string]any, root string) error {
	encoded, err := json.Marshal(args)
	if err != nil || len(encoded) > 256*1024 {
		return errors.New("浏览器工具参数无效或超过 256 KiB")
	}
	for key, value := range args {
		switch key {
		case "filename":
			if path, ok := value.(string); ok && path != "" {
				if err := pathInOutput(path, root); err != nil {
					return err
				}
			}
		case "paths":
			paths, ok := value.([]any)
			if typed, yes := value.([]string); yes {
				for _, path := range typed {
					if err := pathInOutput(path, root); err != nil {
						return err
					}
				}
			} else if ok {
				for _, v := range paths {
					if path, ok := v.(string); ok {
						if err := pathInOutput(path, root); err != nil {
							return err
						}
					}
				}
			}
		case "url":
			if text, ok := value.(string); ok {
				u, err := url.Parse(text)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https" && text != "about:blank") {
					return errors.New("在线取数只允许 http/https 地址或 about:blank")
				}
			}
		}
	}
	return nil
}

func pathInOutput(path, root string) error {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("浏览器文件路径必须位于当前会话输出目录内")
	}
	// Resolve the existing portion, so an output symlink cannot redirect writes.
	parent := filepath.Clean(path)
	for {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			rel, err = filepath.Rel(root, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return errors.New("浏览器文件路径不能通过符号链接离开当前会话目录")
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("检查浏览器文件路径: %w", err)
		}
		next := filepath.Dir(parent)
		if next == parent {
			return errors.New("无法解析浏览器文件路径")
		}
		parent = next
	}
}

func readableResult(result *mcp.CallToolResult, outputDir string) (any, error) {
	if result == nil {
		return nil, errors.New("浏览器返回空结果")
	}
	blocks := make([]map[string]any, 0)
	media := make([]any, 0)
	mediaBytes := 0
	mediaPaths := []string{}
	complete := false
	defer func() {
		if !complete {
			for _, path := range mediaPaths {
				_ = os.Remove(path)
			}
		}
	}()
	attach := func(data []byte, declared, label string) (map[string]any, error) {
		mediaBytes += len(data)
		if mediaBytes > toolresult.MaxInlineMediaBytes {
			return nil, fmt.Errorf("浏览器媒体合计超过 %d MiB", toolresult.MaxInlineMediaBytes>>20)
		}
		item, err := saveMedia(data, declared, label, outputDir)
		if err != nil {
			return nil, err
		}
		media = append(media, item)
		mediaPaths = append(mediaPaths, item["path"].(string))
		return map[string]any{"type": item["type"], "mime_type": item["mime_type"], "bytes": len(data), "path": item["path"]}, nil
	}
	remaining := maxTextBytes
	truncated := false
	for i, content := range result.Content {
		if i >= 32 {
			truncated = true
			break
		}
		switch c := content.(type) {
		case *mcp.TextContent:
			text, cut := clip(c.Text, remaining)
			remaining -= len(text)
			truncated = truncated || cut
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		case *mcp.ImageContent:
			block, err := attach(c.Data, c.MIMEType, "浏览器图片")
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		case *mcp.EmbeddedResource:
			if c.Resource == nil {
				continue
			}
			if c.Resource.Blob != nil {
				block, err := attach(c.Resource.Blob, c.Resource.MIMEType, c.Resource.URI)
				if err != nil {
					return nil, err
				}
				block["uri"] = c.Resource.URI
				blocks = append(blocks, block)
			} else {
				text, cut := clip(c.Resource.Text, remaining)
				remaining -= len(text)
				truncated = truncated || cut
				blocks = append(blocks, map[string]any{"type": "text", "text": text, "uri": c.Resource.URI})
			}
		case *mcp.AudioContent:
			blocks = append(blocks, map[string]any{"type": "audio", "mime_type": c.MIMEType, "bytes": len(c.Data)})
		default:
			data, err := content.MarshalJSON()
			if err != nil {
				continue
			}
			text, cut := clip(string(data), remaining)
			remaining -= len(text)
			truncated = truncated || cut
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		}
	}
	out := map[string]any{"is_error": result.IsError, "content": blocks, "truncated": truncated, "output_dir": outputDir}
	if len(media) > 0 {
		out["_agent_media"] = media
	}
	if result.StructuredContent != nil {
		data, err := json.Marshal(result.StructuredContent)
		if err == nil && len(data) <= 16*1024 {
			out["structured_content"] = result.StructuredContent
		} else {
			out["structured_content_omitted"] = true
			out["truncated"] = true
		}
	}
	files, limited := outputFiles(outputDir)
	out["output_files"] = files
	out["output_files_truncated"] = limited
	complete = true
	return out, nil
}

// MCP binary content is stored under the trusted session directory and passed
// through the existing media envelope, never serialized as base64 tool text.
func saveMedia(data []byte, declared, label, outputDir string) (map[string]any, error) {
	if len(data) == 0 || len(data) > toolresult.MaxInlineMediaBytes {
		return nil, fmt.Errorf("浏览器媒体为空或超过大小上限")
	}
	mimeType := http.DetectContentType(data)
	ext, kind := "", "image"
	switch mimeType {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	case "application/pdf":
		ext, kind = ".pdf", "file"
	default:
		return nil, fmt.Errorf("浏览器返回不支持的媒体类型 %q", mimeType)
	}
	if declared = strings.TrimSpace(declared); declared != "" && declared != mimeType {
		return nil, fmt.Errorf("浏览器媒体声明 %q 与实际内容 %q 不符", declared, mimeType)
	}
	if !filepath.IsAbs(outputDir) {
		return nil, fmt.Errorf("浏览器媒体需要当前会话的绝对输出目录")
	}
	f, err := os.CreateTemp(outputDir, "attachment-*"+ext)
	if err != nil {
		return nil, fmt.Errorf("保存浏览器媒体: %w", err)
	}
	path := f.Name()
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return nil, errors.Join(writeErr, closeErr)
	}
	return map[string]any{"type": kind, "path": path, "mime_type": mimeType, "label": label, "filename": filepath.Base(path)}, nil
}

func outputFiles(root string) ([]map[string]any, bool) {
	type record struct {
		path string
		info fs.FileInfo
	}
	var records []record
	visited := 0
	limited := false
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		visited++
		if visited > 2000 {
			limited = true
			return fs.SkipAll
		}
		if walkErr != nil {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() {
			records = append(records, record{path, info})
		}
		return nil
	})
	sort.Slice(records, func(i, j int) bool { return records[i].info.ModTime().After(records[j].info.ModTime()) })
	if len(records) > 40 {
		records = records[:40]
		limited = true
	}
	files := make([]map[string]any, 0, len(records))
	for _, r := range records {
		files = append(files, map[string]any{"path": r.path, "size": r.info.Size(), "modified_at": r.info.ModTime().UTC().Format("2006-01-02T15:04:05Z")})
	}
	return files, limited
}
