package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	pluginRootRequiredMarker  = "skills/system"
	pluginRootSearchDepth     = 8
	runtimeHomeEnvVar         = "AUTOCARTO_HOME"
	assetsRootEnvVar          = "AUTOCARTO_ASSETS_ROOT"
	defaultWindowsRuntimeHome = `F:\AutoCarto`
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

func resolveDefaultAutoCartoHome() (string, bool) {
	if runtime.GOOS == "windows" {
		return filepath.Clean(defaultWindowsRuntimeHome), true
	}
	return "", false
}

func ResolveAutoCartoHome() (string, error) {
	if envHome, ok, err := resolveAutoCartoHomeFromEnv(); ok {
		if err != nil {
			return "", err
		}
		return envHome, nil
	}
	if defaultHome, ok := resolveDefaultAutoCartoHome(); ok {
		return defaultHome, nil
	}
	return "", fmt.Errorf("未配置桌面 app-local runtime home：请通过 %s 指向可写目录（config/data 由后端维护），不再接受 plugin root fallback", runtimeHomeEnvVar)
}

func resolveAssetsRootFromEnv() (string, bool, error) {
	assetsRoot := strings.TrimSpace(os.Getenv(assetsRootEnvVar))
	if assetsRoot == "" {
		return "", false, nil
	}
	cleaned := filepath.Clean(assetsRoot)
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
