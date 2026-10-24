package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	pluginRootRequiredMarker = "skills/system"
	pluginRootSearchDepth    = 8
	runtimeHomeEnvVar        = "AUTOCARTO_HOME"
	assetsRootEnvVar         = "AUTOCARTO_ASSETS_ROOT"
	projectRootEnvVar        = "AUTOCARTO_PROJECT_ROOT"
	defaultRuntimeDir        = "autocarto_data"
)

func DiscoverPluginRoot() string {
	seeds := make([]string, 0, 2)
	if cwd, err := os.Getwd(); err == nil {
		seeds = append(seeds, cwd)
	}
	if execPath, err := os.Executable(); err == nil {
		seeds = append(seeds, filepath.Dir(execPath))
	}
	for _, seed := range seeds {
		if root := discoverPluginRootFromSeed(seed); root != "" {
			return root
		}
	}
	return ""
}

func discoverPluginRootFromSeed(seed string) string {
	trimmedSeed := strings.TrimSpace(seed)
	if trimmedSeed == "" {
		return ""
	}
	current := filepath.Clean(trimmedSeed)
	for depth := 0; depth <= pluginRootSearchDepth; depth++ {
		for _, candidate := range []string{filepath.Join(current, "autocarto"), current} {
			if !isPluginRoot(candidate) {
				continue
			}
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return ""
}

func isPluginRoot(path string) bool {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(trimmedPath, filepath.FromSlash(pluginRootRequiredMarker)))
	if err != nil || !info.IsDir() {
		return false
	}
	return true
}

func validateAutoCartoHome(path string, source string) (string, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return "", fmt.Errorf("%s 为空，要求指向桌面 app-local runtime home", source)
	}
	return filepath.Clean(trimmedPath), nil
}

func resolveAutoCartoHomeFromEnv() (string, bool, error) {
	envHome := strings.TrimSpace(os.Getenv(runtimeHomeEnvVar))
	if envHome == "" {
		return "", false, nil
	}

	homePath := envHome
	if strings.HasPrefix(homePath, "~/") || strings.HasPrefix(homePath, "~\\") || homePath == "~" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", true, fmt.Errorf("获取用户目录失败: %w", err)
		}
		if homePath == "~" {
			homePath = homeDir
		} else {
			homePath = filepath.Join(homeDir, strings.TrimPrefix(strings.TrimPrefix(homePath, "~/"), "~\\"))
		}
	}
	validatedPath, err := validateAutoCartoHome(homePath, runtimeHomeEnvVar+" 指向的目录")
	if err != nil {
		return "", true, err
	}
	return validatedPath, true, nil
}

// ResolveProjectRoot anchors relative configuration paths independently of the
// process working directory. A project is the directory containing autocarto/.
func ResolveProjectRoot() (string, error) {
	if raw := strings.TrimSpace(os.Getenv(projectRootEnvVar)); raw != "" {
		if !filepath.IsAbs(raw) {
			return "", fmt.Errorf("%s 必须为绝对路径", projectRootEnvVar)
		}
		return filepath.Clean(raw), nil
	}
	if raw := strings.TrimSpace(os.Getenv(assetsRootEnvVar)); filepath.IsAbs(raw) && isPluginRoot(raw) {
		return filepath.Dir(filepath.Clean(raw)), nil
	}
	seeds := []string{}
	if executable, err := os.Executable(); err == nil {
		seeds = append(seeds, filepath.Dir(executable))
	}
	// During development binaries and tests may live in an external cache.
	if _, source, _, ok := runtime.Caller(0); ok && filepath.IsAbs(source) {
		seeds = append(seeds, filepath.Dir(source))
	}
	if cwd, err := os.Getwd(); err == nil {
		seeds = append(seeds, cwd)
	}
	for _, seed := range seeds {
		if pluginRoot := discoverPluginRootFromSeed(seed); pluginRoot != "" {
			return filepath.Dir(pluginRoot), nil
		}
	}
	return "", fmt.Errorf("未发现项目根目录，请设置 %s", projectRootEnvVar)
}

// ResolveProjectPath expands a saved path against the fixed project root. It
// resolves names only; callers still enforce their own resource boundaries.
func ResolveProjectPath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" || strings.ContainsRune(path, '\x00') {
		return "", fmt.Errorf("路径为空或包含非法字符")
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	root, err := ResolveProjectRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(path)), nil
}

// PreferProjectRelativePath keeps files inside a movable project portable.
// External datasets and application installations retain their absolute paths.
func PreferProjectRelativePath(raw string) (string, error) {
	absolute, err := ResolveProjectPath(raw)
	if err != nil {
		return "", err
	}
	root, err := ResolveProjectRoot()
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, absolute)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
		return filepath.ToSlash(rel), nil
	}
	return absolute, nil
}

func ResolveAutoCartoHome() (string, error) {
	if envHome, ok, err := resolveAutoCartoHomeFromEnv(); ok {
		if err != nil {
			return "", err
		}
		return ResolveProjectPath(envHome)
	}
	return ResolveProjectPath(defaultRuntimeDir)
}

func resolveAssetsRootFromEnv() (string, bool, error) {
	assetsRoot := strings.TrimSpace(os.Getenv(assetsRootEnvVar))
	if assetsRoot == "" {
		return "", false, nil
	}
	cleaned, err := ResolveProjectPath(assetsRoot)
	if err != nil {
		return "", true, err
	}
	if !isPluginRoot(cleaned) {
		return "", true, fmt.Errorf("%s 指向的目录无效: %s，要求根目录包含 %s", assetsRootEnvVar, cleaned, pluginRootRequiredMarker)
	}
	return cleaned, true, nil
}

func ResolveAssetsRoot() (string, error) {
	if envAssetsRoot, ok, err := resolveAssetsRootFromEnv(); ok {
		if err != nil {
			return "", err
		}
		return envAssetsRoot, nil
	}

	pluginRoot := strings.TrimSpace(DiscoverPluginRoot())
	if pluginRoot == "" {
		return "", fmt.Errorf("未发现 AutoCarto 运行资产根目录：已检查 %s，并已从当前目录和可执行目录向上最多 %d 层查找，要求根目录包含 %s", assetsRootEnvVar, pluginRootSearchDepth, pluginRootRequiredMarker)
	}
	return pluginRoot, nil
}

func ResolveAutoCartoConfigDir() (string, error) {
	autocartoHome, err := ResolveAutoCartoHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(autocartoHome, "config"), nil
}

func ResolvePluginDataDir() (string, error) {
	autocartoHome, err := ResolveAutoCartoHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(autocartoHome, "data"), nil
}

func ResolveSecretsDir() (string, error) {
	return ResolveAutoCartoConfigDir()
}

func ResolveLogDir() (string, error) {
	dataDir, err := ResolvePluginDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "logs"), nil
}

func ResolveSystemSkillsDir() string {
	assetsRoot, err := ResolveAssetsRoot()
	if err != nil {
		return ""
	}
	return filepath.Join(assetsRoot, "skills", "system")
}

func ResolvePluginUserSkillsDir() string {
	assetsRoot, err := ResolveAssetsRoot()
	if err != nil {
		return ""
	}
	return filepath.Join(assetsRoot, "skills", "user")
}

func ResolveSystemToolsDir() string {
	assetsRoot, err := ResolveAssetsRoot()
	if err != nil {
		return ""
	}
	return filepath.Join(assetsRoot, "tools", "system")
}

func ResolvePluginUserToolsDir() string {
	assetsRoot, err := ResolveAssetsRoot()
	if err != nil {
		return ""
	}
	return filepath.Join(assetsRoot, "tools", "user")
}

func ResolveBuiltinSkillsDir() string {
	return ResolveSystemSkillsDir()
}

func ResolveUserSkillsDir() string {
	return ResolvePluginUserSkillsDir()
}

func ResolveBuiltinToolsDir() string {
	return ResolveSystemToolsDir()
}

func ResolveUserToolsDir() string {
	return ResolvePluginUserToolsDir()
}

func ResolveUserConfigFile() (string, error) {
	configDir, err := ResolveAutoCartoConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "config.yaml"), nil
}

func ResolveRuntimePortFile() (string, error) {
	dataDir, err := ResolvePluginDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, ".port"), nil
}

func ResolveBuiltinFlowsDir() string {
	root, err := ResolveAssetsRoot()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "flows", "system")
}

func ResolveUserFlowsDir() (string, error) {
	root, err := ResolveAutoCartoHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "flows"), nil
}

func ResolveResourceBindingsFile() (string, error) {
	root, err := ResolveAutoCartoConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "resource_bindings.yaml"), nil
}
