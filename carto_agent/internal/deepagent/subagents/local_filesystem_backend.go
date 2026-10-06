package subagents

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/cloudwego/eino/adk/filesystem"
)

// LocalFilesystemBackend 基于本地文件系统实现 Eino filesystem.Backend。
// 当前主要用于 skill middleware 的文件枚举与读取。
type LocalFilesystemBackend struct{}

// NewLocalFilesystemBackend 创建本地文件系统 backend。
func NewLocalFilesystemBackend() *LocalFilesystemBackend {
	return &LocalFilesystemBackend{}
}

// LsInfo 列出目录内容。
func (b *LocalFilesystemBackend) LsInfo(_ context.Context, req *filesystem.LsInfoRequest) ([]filesystem.FileInfo, error) {
	if req == nil {
		return nil, fmt.Errorf("ls request is required")
	}

	entries, err := os.ReadDir(req.Path)
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
			Path:       filepath.Join(req.Path, entry.Name()),
			IsDir:      entry.IsDir(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	return result, nil
}

// Read 读取文件内容，支持按行偏移和限制。
func (b *LocalFilesystemBackend) Read(_ context.Context, req *filesystem.ReadRequest) (*filesystem.FileContent, error) {
	if req == nil {
		return nil, fmt.Errorf("read request is required")
	}

	data, err := os.ReadFile(req.FilePath)
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

// GrepRaw 在文件中执行简单正则匹配。
func (b *LocalFilesystemBackend) GrepRaw(ctx context.Context, req *filesystem.GrepRequest) ([]filesystem.GrepMatch, error) {
	if req == nil {
		return nil, fmt.Errorf("grep request is required")
	}

	pattern := req.Pattern
	if req.CaseInsensitive {
		pattern = "(?i)" + pattern
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	root := req.Path
	if strings.TrimSpace(root) == "" {
		root = "."
	}

	var matches []filesystem.GrepMatch
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if req.Glob != "" {
			ok, err := doublestar.PathMatch(req.Glob, path)
			if err != nil {
				return err
			}
			if !ok {
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					return relErr
				}
				ok, err = doublestar.PathMatch(req.Glob, rel)
				if err != nil {
					return err
				}
				if !ok {
					return nil
				}
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		for idx, line := range lines {
			if re.MatchString(line) {
				matches = append(matches, filesystem.GrepMatch{
					Content: line,
					Path:    path,
					Line:    idx + 1,
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return matches, nil
}

// GlobInfo 返回匹配 glob 的文件列表。
func (b *LocalFilesystemBackend) GlobInfo(_ context.Context, req *filesystem.GlobInfoRequest) ([]filesystem.FileInfo, error) {
	if req == nil {
		return nil, fmt.Errorf("glob request is required")
	}

	root := req.Path
	if strings.TrimSpace(root) == "" {
		root = "."
	}

	pattern := filepath.Join(root, req.Pattern)
	matches, err := doublestar.FilepathGlob(pattern)
	if err != nil {
		return nil, err
	}

	result := make([]filesystem.FileInfo, 0, len(matches))
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			return nil, err
		}
		result = append(result, filesystem.FileInfo{
			Path:       match,
			IsDir:      info.IsDir(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	return result, nil
}

// Write 写入文件内容。
func (b *LocalFilesystemBackend) Write(_ context.Context, req *filesystem.WriteRequest) error {
	if req == nil {
		return fmt.Errorf("write request is required")
	}

	if err := os.MkdirAll(filepath.Dir(req.FilePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(req.FilePath, []byte(req.Content), 0o644)
}

// Edit 执行简单文本替换。
func (b *LocalFilesystemBackend) Edit(ctx context.Context, req *filesystem.EditRequest) error {
	if req == nil {
		return fmt.Errorf("edit request is required")
	}
	if req.OldString == "" {
		return fmt.Errorf("oldString is required")
	}

	content, err := b.Read(ctx, &filesystem.ReadRequest{FilePath: req.FilePath})
	if err != nil {
		return err
	}

	if !strings.Contains(content.Content, req.OldString) {
		return fmt.Errorf("oldString not found")
	}

	updated := strings.Replace(content.Content, req.OldString, req.NewString, 1)
	if req.ReplaceAll {
		updated = strings.ReplaceAll(content.Content, req.OldString, req.NewString)
	}

	return b.Write(ctx, &filesystem.WriteRequest{
		FilePath: req.FilePath,
		Content:  updated,
	})
}
