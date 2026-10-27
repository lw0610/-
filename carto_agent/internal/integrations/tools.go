// Package integrations connects optional data runtimes to the existing internal
// tool backend. Output locations come from trusted session context, not tool args.
package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/osmdata"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/browserruntime"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/geeruntime"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/autocarto/carto_agent/internal/webdata"
)

type ConfigProvider interface {
	GetIntegrationsConfig() (config.IntegrationsConfig, error)
}

func Handles(name string) bool {
	return osmdata.Handles(name) || geeruntime.Handles(name) || browserruntime.Handles(name) || webdata.Handles(name)
}

func Invoke(ctx context.Context, name string, args map[string]any, provider ConfigProvider, gisInvokers ...GISInvoker) (any, error) {
	if provider == nil {
		return nil, fmt.Errorf("扩展服务配置不可用")
	}
	c, err := provider.GetIntegrationsConfig()
	if err != nil {
		return nil, err
	}
	if name == "osm_check_environment" {
		return diagnosticResult(Check(ctx, "osm", c))
	}
	if name == "gee_check_environment" {
		return diagnosticResult(Check(ctx, "gee", c))
	}
	if name == "browser_check_environment" {
		return diagnosticResult(Check(ctx, "browser", c))
	}
	session := skill.SessionContextFromContext(ctx)
	if session.SessionID == "" {
		return nil, fmt.Errorf("此工具需要当前聊天会话，不能由模型提供 session_id 替代")
	}
	// Use the same app-local owner as persisted sessions. SessionContext's
	// legacy project-local path can be moved away by normal session reads.
	root, err := config.ResolveSessionDir(session.ProjectPath, session.SessionID)
	if err != nil {
		return nil, fmt.Errorf("当前会话输出目录不可用: %w", err)
	}
	sessionRoot := root
	root = filepath.Join(root, "output", "integrations")
	var result any
	switch {
	case osmdata.Handles(name):
		options := osmOptions(c, filepath.Join(sessionRoot, "output", "osm"), session.ProjectPath)
		if name == "osm_get_objects" || name == "osm_get_features" {
			if !options.Enabled {
				return nil, fmt.Errorf("请先在设置 → 扩展服务启用 OSM")
			}
			// Enabling the integration authorizes downloads through this trusted
			// session entry point; do not interrupt each new chat for confirmation.
			options.Authorized = true
		}
		var osmResult *osmdata.Result
		var gis GISInvoker
		if len(gisInvokers) > 0 {
			gis = gisInvokers[0]
		}
		osmResult, err = invokeOSM(ctx, name, args, options, gis)
		result = osmResult
		if osmResult != nil {
			if store, e := sessionfiles.New(session.ProjectPath, session.SessionID); e == nil {
				for _, path := range osmResult.ArtifactPaths {
					if isOSMStagingPath(options.OutputDir, path) {
						continue // Recovery files remain diagnostic, not delivered layers.
					}
					if _, e = store.Register(path, sessionfiles.Origin{Type: "osm", Tool: name, Label: "OSM 下载", License: "ODbL 1.0"}, sessionfiles.WithGroup("data")); e != nil {
						osmResult.Warnings = append(osmResult.Warnings, "文件登记失败: "+e.Error())
					}
				}
			}
		}
	case geeruntime.Handles(name):
		result, err = geeruntime.Invoke(ctx, name, args, geeOptions(c, filepath.Join(root, "gee")))
	case browserruntime.Handles(name):
		result, err = browserruntime.Invoke(ctx, name, args, browserOptions(c, session.SessionID, filepath.Join(root, "browser")))
	case webdata.Handles(name):
		result, err = webdata.Invoke(ctx, name, args, webdata.Options{Enabled: c.Web.Enabled, TimeoutSeconds: c.Web.TimeoutSeconds, MaxDownloadMB: c.Web.MaxDownloadMB, OutputDir: filepath.Join(root, "downloads")})
	default:
		return nil, fmt.Errorf("未知扩展工具: %s", name)
	}
	if err != nil && result != nil {
		// The existing engine renders execution errors as text. Retain bounded
		// diagnostic output and task/artifact IDs without changing that contract.
		return result, fmt.Errorf("%w; 诊断: %s", err, failureSummary(result))
	}
	return result, err
}

func failureSummary(result any) string {
	// Keep cloud task identities before considering log size. Truncating a
	// serialized Result would cut off tasks after a verbose stdout field.
	if r, ok := result.(*geeruntime.Result); ok {
		tasks := make([]map[string]any, 0, len(r.Tasks))
		for _, task := range r.Tasks {
			tasks = append(tasks, map[string]any{"id": task["id"], "name": task["name"], "state": task["state"]})
		}
		paths := r.ArtifactPaths
		if len(paths) > 20 {
			paths = paths[:20]
		}
		data, _ := json.Marshal(map[string]any{
			"success": false, "status": r.Status, "error_code": r.ErrorCode,
			"script_path": r.ScriptPath, "output_dir": r.OutputDir,
			"artifact_paths": paths, "artifact_count": len(r.ArtifactPaths),
			"tasks": tasks, "task": r.Task,
			"downloads": r.Downloads,
			"stdout":    clipDiagnostic(r.Stdout, 1500), "stderr": clipDiagnostic(r.Stderr, 1500),
		})
		return string(data)
	}
	data, _ := json.Marshal(result)
	return clipDiagnostic(string(data), 12000)
}

func clipDiagnostic(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…[诊断已截断，完整日志见输出目录]"
}

func diagnosticResult(result map[string]any) (any, error) {
	// A diagnostic can succeed as an operation while reporting a runtime as
	// unavailable. Both statuses are intentionally explicit in its payload.
	return result, nil
}

func Check(ctx context.Context, service string, c config.IntegrationsConfig) map[string]any {
	var result any
	var err error
	switch strings.TrimSpace(service) {
	case "osm":
		result, err = osmdata.Invoke(ctx, "osm_check_environment", map[string]any{}, osmOptions(c, "", ""))
	case "gee":
		var dir string
		dir, err = os.MkdirTemp("", "autocarto-gee-check-")
		if err == nil {
			defer os.RemoveAll(dir)
			result, err = geeruntime.Invoke(ctx, "gee_check_environment", map[string]any{}, geeOptions(c, dir))
		}
	case "browser":
		result, err = browserruntime.Invoke(ctx, "browser_check_environment", map[string]any{}, browserOptions(c, "", ""))
	default:
		err = fmt.Errorf("未知服务: %s", service)
	}
	out := map[string]any{"service": service, "success": err == nil, "result": result}
	// Runtime packages may represent a failed check as a successful protocol
	// call with success:false. Preserve that distinction in the UI envelope.
	if data, e := json.Marshal(result); e == nil {
		var status map[string]any
		if json.Unmarshal(data, &status) == nil {
			if ok, exists := status["success"].(bool); exists && !ok {
				out["success"] = false
			}
			if service == "gee" {
				if env, ok := status["environment"].(map[string]any); ok {
					if ready, _ := env["ready"].(bool); !ready {
						out["success"] = false
					}
				}
			}
			if service == "browser" {
				if available, _ := status["command_available"].(bool); !available {
					out["success"] = false
				}
				out["note"] = "只核实了本地启动命令；首次实际浏览时才验证 MCP 与浏览器。"
			}
		}
	}
	if err != nil {
		out["error"] = err.Error()
	}
	return out
}

func geeOptions(c config.IntegrationsConfig, output string) geeruntime.Options {
	return geeruntime.Options{Enabled: c.GEE.Enabled, Python: c.GEE.Python, Project: c.GEE.Project, ProxyURL: c.GEE.ProxyURL, TimeoutSeconds: c.GEE.TimeoutSeconds, OutputDir: output}
}

func browserOptions(c config.IntegrationsConfig, session, output string) browserruntime.Options {
	return browserruntime.Options{Enabled: c.Browser.Enabled, Command: c.Browser.Command, Args: c.Browser.Args, Headless: c.Browser.Headless, Browser: c.Browser.Browser, TimeoutSeconds: c.Browser.TimeoutSeconds, OutputDir: output, SessionID: session}
}

func osmOptions(c config.IntegrationsConfig, output, project string) osmdata.Options {
	return osmdata.Options{Enabled: c.OSM.Enabled, Endpoints: c.OSM.Endpoints, NominatimURL: c.OSM.NominatimURL, ProxyURL: c.OSM.ProxyURL, TimeoutSeconds: c.OSM.TimeoutSeconds, TotalTimeoutSeconds: c.OSM.TotalTimeoutSeconds, MaxAttempts: c.OSM.MaxAttempts, MaxResponseMB: c.OSM.MaxResponseMB, OutputDir: output, ProjectDir: filepath.Dir(project)}
}
