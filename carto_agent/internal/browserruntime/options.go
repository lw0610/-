// Package browserruntime runs an isolated Playwright MCP server for each chat.
package browserruntime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const PlaywrightVersion = "0.0.83"

// Options are supplied by application configuration, never by tool arguments.
// OutputDir must be the absolute, application-owned output directory for SessionID.
type Options struct {
	Enabled        bool
	Command        string
	Args           []string
	Headless       bool
	Browser        string
	TimeoutSeconds int
	OutputDir      string
	SessionID      string
}

func Handles(name string) bool {
	switch name {
	case "browser_check_environment", "browser_list_tools", "browser_call_tool", "browser_close":
		return true
	}
	return false
}

func normalize(opts Options) (Options, error) {
	opts.Command = strings.TrimSpace(opts.Command)
	if opts.Command == "" {
		opts.Command = "npx"
	}
	opts.Args = append([]string(nil), opts.Args...)
	if len(opts.Args) == 0 {
		if name := strings.TrimSuffix(strings.ToLower(filepath.Base(opts.Command)), ".cmd"); name != "npx" {
			return opts, fmt.Errorf("自定义浏览器命令必须配置 args（例如 Node 对应的 Playwright MCP cli.js 路径）")
		}
		opts.Args = []string{"-y", "@playwright/mcp@" + PlaywrightVersion}
	}
	// These settings belong to the application. In particular, --config can
	// silently restore cookies, an existing browser, or unrestricted file access.
	for _, arg := range opts.Args {
		flag, _, _ := strings.Cut(arg, "=")
		switch flag {
		case "--output-dir", "--user-data-dir", "--isolated", "--storage-state", "--extension",
			"--cdp-endpoint", "--cdp-header", "--endpoint", "--remote-endpoint", "--config", "-c",
			"--shared-browser-context", "--headless", "--browser", "--port", "--host",
			"--allow-unrestricted-file-access", "--init-page", "--init-script", "--secrets":
			return opts, fmt.Errorf("浏览器 args 不允许覆盖受控启动参数 %s", flag)
		}
	}
	opts.SessionID = strings.TrimSpace(opts.SessionID)
	opts.Browser = strings.TrimSpace(opts.Browser)
	if opts.Browser == "" {
		opts.Browser = "chrome"
	}
	switch opts.Browser {
	case "chrome", "firefox", "webkit", "msedge", "chromium":
	default:
		return opts, fmt.Errorf("不支持的浏览器 %q", opts.Browser)
	}
	if opts.TimeoutSeconds <= 0 {
		opts.TimeoutSeconds = 60
	}
	if opts.TimeoutSeconds > 3600 {
		return opts, fmt.Errorf("浏览器超时不能超过 3600 秒")
	}
	return opts, nil
}

func (o Options) timeout() time.Duration { return time.Duration(o.TimeoutSeconds) * time.Second }

func prepareOutputDir(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" || !filepath.IsAbs(raw) {
		return "", fmt.Errorf("浏览器需要当前会话的绝对 output_dir")
	}
	clean := filepath.Clean(raw)
	if clean == filepath.VolumeName(clean)+string(filepath.Separator) {
		return "", fmt.Errorf("浏览器 output_dir 不能是文件系统根目录")
	}
	if err := os.MkdirAll(clean, 0700); err != nil {
		return "", fmt.Errorf("创建浏览器输出目录: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("解析浏览器输出目录: %w", err)
	}
	return resolved, nil
}

func launchArgs(opts Options) []string {
	args := append([]string(nil), opts.Args...)
	args = append(args, "--isolated", "--output-dir", opts.OutputDir, "--browser", opts.Browser)
	if opts.Headless {
		args = append(args, "--headless")
	}
	return args
}

func launchEnv() []string {
	var env []string
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		// Ignore inherited MCP settings, including browser/CDP/profile settings.
		if !strings.HasPrefix(strings.ToUpper(key), "PLAYWRIGHT_MCP_") {
			env = append(env, item)
		}
	}
	return env
}

func checkEnvironment(opts Options) map[string]any {
	result := map[string]any{"enabled": opts.Enabled, "playwright_mcp_version": PlaywrightVersion,
		"command": opts.Command, "browser": opts.Browser, "headless": opts.Headless,
		"isolated": true, "server_started": false, "browser_verified": false,
		"note": "仅检查本地命令；启动时才验证 MCP 和浏览器。首次使用默认 npx 可能下载固定版本 npm 包，不安装全局依赖。"}
	command, err := newCommand(context.Background(), opts.Command, opts.Args)
	result["command_available"] = err == nil
	if err == nil {
		result["command_path"] = command.Path
	} else {
		result["command_error"] = "无法执行启动命令，请配置 Node.js 18+ 和 npx，或 Node 绝对路径及 MCP cli.js 参数: " + err.Error()
	}
	node, nodeErr := exec.LookPath("node")
	result["node_on_path"] = nodeErr == nil
	if nodeErr == nil {
		result["node_path"] = node
	}
	return result
}

// resolveNpxShim is platform-neutral to test paths containing spaces without
// executing a Windows shell or requiring a Windows host.
func resolveNpxShim(shim string) (node, cli string, err error) {
	cli = filepath.Join(filepath.Dir(shim), "node_modules", "npm", "bin", "npx-cli.js")
	if info, statErr := os.Stat(cli); statErr != nil || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("找不到 npx-cli.js，请配置 node.exe 绝对路径和 Playwright MCP cli.js 参数")
	}
	node = filepath.Join(filepath.Dir(shim), "node.exe")
	if info, statErr := os.Stat(node); statErr != nil || !info.Mode().IsRegular() {
		node, err = exec.LookPath("node")
		if err != nil {
			return "", "", err
		}
	}
	return node, cli, nil
}
