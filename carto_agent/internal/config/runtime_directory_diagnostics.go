package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

type RuntimeDirectoryWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type RuntimeDirectoryDiagnostics struct {
	RuntimeHome string                    `json:"runtime_home"`
	DataDir     string                    `json:"data_dir"`
	LogFile     string                    `json:"log_file"`
	Warnings    []RuntimeDirectoryWarning `json:"warnings"`
}

// DirectoryDiagnostics reports effective local paths without modifying config
// or creating directories. logDir is the process's actual startup logger path.
func (c *Config) DirectoryDiagnostics(home, logDir string) RuntimeDirectoryDiagnostics {
	resolve := func(path, fallback string) string {
		if strings.TrimSpace(path) == "" {
			path = fallback
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(home, path)
		}
		return filepath.Clean(path)
	}
	result := RuntimeDirectoryDiagnostics{RuntimeHome: filepath.Clean(home), DataDir: resolve(c.DataDir, "data"), LogFile: filepath.Join(resolve(logDir, "data/logs"), "agent.log"), Warnings: []RuntimeDirectoryWarning{}}
	if home != "" {
		relative, err := filepath.Rel(canonicalDirectoryPath(home), canonicalDirectoryPath(result.DataDir))
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			result.Warnings = append(result.Warnings, RuntimeDirectoryWarning{Code: "DATA_DIR_OUTSIDE_RUNTIME_HOME", Message: fmt.Sprintf("数据目录位于当前运行目录之外：%s。当前实际日志文件：%s。请核对配置中的 data_dir；系统不会自动改写。", result.DataDir, result.LogFile)})
		}
	}
	return result
}

// Resolve an existing ancestor so /var versus /private/var and symlinked
// directories are handled consistently even before their children are created.
func canonicalDirectoryPath(path string) string {
	path = filepath.Clean(path)
	suffix := []string{}
	current := path
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}
func (cs *ConfigService) GetRuntimeDirectoryDiagnostics() RuntimeDirectoryDiagnostics {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	home, _ := ResolveAutoCartoHome()
	if cs.config == nil {
		return RuntimeDirectoryDiagnostics{RuntimeHome: home, Warnings: []RuntimeDirectoryWarning{}}
	}
	return cs.config.DirectoryDiagnostics(home, cs.runtimeLogDir)
}
