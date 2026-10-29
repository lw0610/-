package websocket

import (
	"context"
	"strings"

	"github.com/autocarto/carto_agent/internal/toolsession"
)

// InjectToolRuntimeArgs 为工具调用补充会话上下文参数。
func (s *DeepServer) InjectToolRuntimeArgs(ctx context.Context, args map[string]any) map[string]any {
	if args == nil {
		args = make(map[string]any)
	}
	delete(args, "_autocarto_output_owner")
	if owner := toolsession.OutputOwnerFromContext(ctx); owner != nil {
		args["_autocarto_output_owner"] = owner
	}

	if sessionID := toolsession.SessionIDFromContext(ctx); sessionID != "" {
		if _, exists := args["session_id"]; !exists {
			args["session_id"] = sessionID
		}

		if instanceID := toolsession.InstanceIDFromContext(ctx); instanceID != "" {
			if _, exists := args["instance_id"]; !exists {
				args["instance_id"] = instanceID
			}
			if _, exists := args["target_instance_id"]; !exists {
				args["target_instance_id"] = instanceID
			}
		}

		projectPath := toolsession.ProjectPathFromContext(ctx)
		if projectPath != "" {
			if _, exists := args["project_path"]; !exists {
				args["project_path"] = projectPath
			}
		}

		if s != nil {
			if dirs, err := s.InitSessionDir(projectPath, sessionID); err == nil {
				if _, exists := args["session_dir"]; !exists {
					if root := strings.TrimSpace(dirs["root"]); root != "" {
						args["session_dir"] = root
					}
				}
				if _, exists := args["session_subdirs"]; !exists {
					subdirs := make(map[string]any, len(dirs))
					for key, value := range dirs {
						if key == "root" {
							continue
						}
						subdirs[key] = value
					}
					args["session_subdirs"] = subdirs
				}
			}
		}
	}

	if projectPath := toolsession.ProjectPathFromContext(ctx); projectPath != "" {
		if _, exists := args["project_path"]; !exists {
			args["project_path"] = projectPath
		}
	}
	return args
}
