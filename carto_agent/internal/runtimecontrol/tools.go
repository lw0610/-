package runtimecontrol

import (
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/hitl"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/autocarto/carto_agent/internal/skill"
)

const (
	ToolHITLRequest   = "hitl_request"
	ToolOutputGetInfo = "output_get_info"
	ToolReferenceRead = "reference_read"
	ToolResourceGet   = "resource_get"
)

func Handles(toolName string) bool {
	if dataservice.Handles(toolName) {
		return true
	}
	switch strings.TrimSpace(toolName) {
	case ToolHITLRequest, ToolOutputGetInfo, ToolReferenceRead, ToolResourceGet, ToolPlanPropose, ToolPlanRevise:
		return true
	default:
		return false
	}
}

func Invoke(ctx context.Context, toolName string, args map[string]any) (any, error) {
	if dataservice.Handles(toolName) {
		return dataservice.Invoke(ctx, toolName, args)
	}
	switch strings.TrimSpace(toolName) {
	case ToolHITLRequest:
		return hitl.Invoke(ctx, args)
	case ToolOutputGetInfo:
		return getOutputInfo(ctx, args)
	case ToolResourceGet:
		return getResource(ctx, args)
	case ToolReferenceRead:
		return readReference(ctx, stringArg(args, "path"))
	case ToolPlanPropose, ToolPlanRevise:
		return invokePlanTool(ctx, toolName, args)
	default:
		return nil, fmt.Errorf("unsupported runtime-control tool: %s", strings.TrimSpace(toolName))
	}
}

func getOutputInfo(ctx context.Context, args map[string]any) (map[string]any, error) {
	// The public tool takes no directory arguments. Resolve the same trusted
	// application-owned session location as the download/file runtimes; the
	// legacy SessionContext.SessionDir() points outside that owner.
	outputDir := ""
	session := skill.SessionContextFromContext(ctx)
	if session.SessionID != "" {
		sessionDir, err := config.ResolveSessionDir(session.ProjectPath, session.SessionID)
		if err != nil {
			return nil, fmt.Errorf("当前会话输出目录不可用: %w", err)
		}
		outputDir = filepath.Join(sessionDir, "output")
	} else {
		// Preserve explicit calls made by older internal integrations only when
		// there is no trusted active session to take precedence.
		outputDir = strings.TrimSpace(stringArg(args, "output_dir"))
	}
	if outputDir == "" {
		sessionDir := strings.TrimSpace(stringArg(args, "session_dir"))
		if sessionDir != "" {
			outputDir = filepath.Join(sessionDir, "output")
		}
	}
	if outputDir == "" {
		return map[string]any{"success": false, "output": "", "info": map[string]any{}, "error": "缺少 output_dir 或 session_dir"}, nil
	}
	info := map[string]any{
		"exists":        false,
		"files_count":   0,
		"files":         []map[string]any{},
		"subdirs_count": 0,
		"subdirs":       []map[string]any{},
	}
	if session.SessionID != "" {
		if store, err := sessionfiles.New(session.ProjectPath, session.SessionID); err == nil {
			if items, err := store.List(false); err == nil {
				groups := map[string][]sessionfiles.Item{}
				for _, item := range items {
					groups[item.Group] = append(groups[item.Group], item)
				}
				info["session_files"] = items
				info["groups"] = groups
				info["session_files_count"] = len(items)
			}
		}
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{"success": true, "output": outputDir, "info": info, "error": ""}, nil
		}
		return nil, err
	}
	files := make([]map[string]any, 0)
	subdirs := make([]map[string]any, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			// Expose nested artifact locations without recursively scanning large
			// download trees or changing the existing direct-file count contract.
			subdirs = append(subdirs, map[string]any{
				"name": entry.Name(),
				"path": filepath.Join(outputDir, entry.Name()),
			})
			continue
		}
		item := map[string]any{"name": entry.Name()}
		if fileInfo, statErr := entry.Info(); statErr == nil {
			item["size"] = fileInfo.Size()
		}
		files = append(files, item)
	}
	info["exists"] = true
	info["files_count"] = len(files)
	info["files"] = files
	info["subdirs_count"] = len(subdirs)
	info["subdirs"] = subdirs
	return map[string]any{"success": true, "output": outputDir, "info": info, "error": ""}, nil
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	raw, _ := args[key].(string)
	return raw
}
