package runtimepersist

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/adk/filesystem"
)

// ScopedFilesystemBackend 将文件访问限制在指定 root 下，供 reduction/read_file 使用。
type ScopedFilesystemBackend struct {
	rootDir string
}

// NewScopedFilesystemBackend 创建受限文件系统 backend。
func NewScopedFilesystemBackend(rootDir string) (*ScopedFilesystemBackend, error) {
	cleanRoot := filepath.Clean(strings.TrimSpace(rootDir))
	if cleanRoot == "" || cleanRoot == "." {
		return nil, fmt.Errorf("scoped filesystem root is required")
	}
	return &ScopedFilesystemBackend{rootDir: cleanRoot}, nil
}

// RootDir 返回 backend 根目录。
func (b *ScopedFilesystemBackend) RootDir() string {
	if b == nil {
		return ""
	}
	return b.rootDir
}

// ResolveReductionOffloadRoot 计算 reduction offload 根目录。
func ResolveReductionOffloadRoot(configBaseDir, activeConfigName, agentName string) string {
	return filepath.Join(
		strings.TrimSpace(configBaseDir),
		"runtime",
		"reduction",
		sanitizePathSegment(activeConfigName, "default"),
		sanitizePathSegment(agentName, "agent"),
	)
}

func sanitizePathSegment(raw string, fallback string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}

	var b strings.Builder
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}

	value := strings.Trim(b.String(), "._-")
	if value == "" {
		return fallback
	}
	return value
}

func (b *ScopedFilesystemBackend) resolvePath(inputPath string) (string, error) {
	if b == nil {
		return "", fmt.Errorf("scoped filesystem backend is nil")
	}

	trimmed := strings.TrimSpace(inputPath)
	if trimmed == "" {
		return "", fmt.Errorf("file path is required")
	}

	var resolved string
	if filepath.IsAbs(trimmed) {
		resolved = filepath.Clean(trimmed)
	} else {
		resolved = filepath.Clean(filepath.Join(b.rootDir, trimmed))
	}

	rel, err := filepath.Rel(b.rootDir, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve relative path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes scoped root", inputPath)
	}
	return resolved, nil
}

// LsInfo 列出目录内容。
func (b *ScopedFilesystemBackend) LsInfo(_ context.Context, req *filesystem.LsInfoRequest) ([]filesystem.FileInfo, error) {
	if req == nil {
		return nil, fmt.Errorf("ls request is required")
	}

	targetPath := req.Path
	if strings.TrimSpace(targetPath) == "" {
		targetPath = b.rootDir
	}

	resolved, err := b.resolvePath(targetPath)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}

	result := make([]filesystem.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		result = append(result, filesystem.FileInfo{
			Path:       filepath.Join(resolved, entry.Name()),
			IsDir:      entry.IsDir(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	return result, nil
}

// Read 读取文件内容，支持按行偏移和限制。
func (b *ScopedFilesystemBackend) Read(_ context.Context, req *filesystem.ReadRequest) (*filesystem.FileContent, error) {
	if req == nil {
		return nil, fmt.Errorf("read request is required")
	}

	resolved, err := b.resolvePath(req.FilePath)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}

	content := string(data)
	offset := req.Offset
	limit := req.Limit
	if offset <= 1 && limit <= 0 {
		return &filesystem.FileContent{Content: content}, nil
	}

	lines := strings.Split(content, "\n")
	start := offset - 1
	if start < 0 {
		start = 0
	}
	if start > len(lines) {
		start = len(lines)
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}

	return &filesystem.FileContent{
		Content: strings.Join(lines[start:end], "\n"),
	}, nil
}

// GrepRaw 当前不对 reduction offload 暴露 grep 能力。
func (b *ScopedFilesystemBackend) GrepRaw(context.Context, *filesystem.GrepRequest) ([]filesystem.GrepMatch, error) {
	return nil, fmt.Errorf("grep is not supported by scoped filesystem backend")
}

// GlobInfo 当前不对 reduction offload 暴露 glob 能力。
func (b *ScopedFilesystemBackend) GlobInfo(context.Context, *filesystem.GlobInfoRequest) ([]filesystem.FileInfo, error) {
	return nil, fmt.Errorf("glob is not supported by scoped filesystem backend")
}

// Write 写入文件内容。
func (b *ScopedFilesystemBackend) Write(_ context.Context, req *filesystem.WriteRequest) error {
	if req == nil {
		return fmt.Errorf("write request is required")
	}

	resolved, err := b.resolvePath(req.FilePath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return err
	}
	return os.WriteFile(resolved, []byte(req.Content), 0o644)
}

// Edit 当前不对 reduction offload 暴露 edit 能力。
func (b *ScopedFilesystemBackend) Edit(context.Context, *filesystem.EditRequest) error {
	return fmt.Errorf("edit is not supported by scoped filesystem backend")
}
