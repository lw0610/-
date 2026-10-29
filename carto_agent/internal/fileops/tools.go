package fileops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ToolCreateDirectory = "create_directory"
	ToolReadFile        = "file_read"
	ToolExtractDocument = "document_extract"
	ToolWriteFile       = "file_write"
	ToolRunCommand      = "shell_execute"
)

func Handles(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case ToolCreateDirectory, ToolReadFile, ToolWriteFile, ToolRunCommand, ToolExtractDocument:
		return true
	}
	return false
}

func Invoke(ctx context.Context, toolName string, args map[string]any) (any, error) {
	switch strings.TrimSpace(toolName) {
	case ToolCreateDirectory:
		return createDirectory(args)
	case ToolReadFile, ToolWriteFile, ToolRunCommand, ToolExtractDocument:
		workspace, err := sessionWorkspace(ctx)
		if err != nil {
			return nil, err
		}
		switch strings.TrimSpace(toolName) {
		case ToolExtractDocument:
			return workspace.extract(ctx, args)
		case ToolReadFile:
			return workspace.read(ctx, args)
		case ToolWriteFile:
			return workspace.write(ctx, args)
		default:
			return workspace.run(ctx, args)
		}
	default:
		return nil, fmt.Errorf("unsupported file operation tool: %s", strings.TrimSpace(toolName))
	}
}

func createDirectory(args map[string]any) (map[string]any, error) {
	targetPath := strings.TrimSpace(stringArg(args, "PATH", "path"))
	if targetPath == "" {
		return nil, fmt.Errorf("create_directory 缺少 PATH 参数")
	}
	targetPath = filepath.Clean(targetPath)

	info, err := os.Stat(targetPath)
	switch {
	case err == nil:
		if !info.IsDir() {
			return nil, fmt.Errorf("create_directory 目标已存在且不是目录: %s", targetPath)
		}
		return map[string]any{
			"success": true,
			"output":  targetPath,
			"info": map[string]any{
				"created": false,
				"exists":  true,
			},
			"error": "",
		}, nil
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("create_directory 检查目录状态失败: %w", err)
	}

	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return nil, fmt.Errorf("create_directory 创建目录失败: %w", err)
	}

	return map[string]any{
		"success": true,
		"output":  targetPath,
		"info": map[string]any{
			"created": true,
			"exists":  true,
		},
		"error": "",
	}, nil
}

func stringArg(values map[string]any, names ...string) string {
	for _, name := range names {
		if name == "" || values == nil {
			continue
		}
		raw, _ := values[name].(string)
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
