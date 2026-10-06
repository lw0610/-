package compute

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type LocalRuntime struct {
	PythonPath string
	WorkingDir string
	MaxWorkers int
	Env        []string
}

func ResolveLocalRuntime(pythonPath string, maxWorkers int, extraSeeds ...string) (*LocalRuntime, error) {
	trimmedPython := strings.TrimSpace(pythonPath)
	if trimmedPython == "" {
		return nil, fmt.Errorf("compute python_path 为空")
	}

	workingDir := resolveComputeWorkingDir(extraSeeds...)

	if maxWorkers <= 0 {
		maxWorkers = 1
	}

	return &LocalRuntime{
		PythonPath: trimmedPython,
		WorkingDir: workingDir,
		MaxWorkers: maxWorkers,
		Env:        buildComputeCLIEnv(trimmedPython, maxWorkers),
	}, nil
}

func resolveComputeWorkingDir(seedDirs ...string) string {
	for _, seed := range seedDirs {
		trimmed := strings.TrimSpace(seed)
		if trimmed == "" {
			continue
		}
		cleaned := filepath.Clean(trimmed)
		info, err := os.Stat(cleaned)
		if err != nil {
			continue
		}
		if info.IsDir() {
			return cleaned
		}
		return filepath.Dir(cleaned)
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return ""
}

func buildComputeCLIEnv(pythonPath string, maxWorkers int) []string {
	return flattenEnvMap(buildComputeRuntimeEnvMap(pythonPath, maxWorkers))
}

func buildComputeRuntimeEnvMap(pythonPath string, maxWorkers int) map[string]string {
	envMap := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		envMap[key] = value
	}
	if maxWorkers > 0 {
		envMap["AUTOCARTO_COMPUTE_MAX_WORKERS"] = fmt.Sprintf("%d", maxWorkers)
	}
	for key, value := range deriveQGISEnvFromPython(pythonPath) {
		if key == "PYTHONPATH" {
			envMap[key] = prependEnvPath(envMap[key], value)
			continue
		}
		if strings.TrimSpace(value) != "" {
			envMap[key] = value
		}
	}
	for key, value := range deriveHeadlessRuntimeEnv(runtime.GOOS) {
		if strings.TrimSpace(value) != "" {
			envMap[key] = value
		}
	}
	return envMap
}

func flattenEnvMap(envMap map[string]string) []string {
	keys := make([]string, 0, len(envMap))
	for key := range envMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+envMap[key])
	}
	return out
}

func deriveHeadlessRuntimeEnv(goos string) map[string]string {
	if strings.TrimSpace(goos) != "darwin" {
		return nil
	}
	return map[string]string{
		"QT_MAC_DISABLE_FOREGROUND_APPLICATION_TRANSFORM": "1",
	}
}

func deriveQGISEnvFromPython(pythonPath string) map[string]string {
	prefixPath, resourcesDir := inferQGISRuntimeDirsFromPython(pythonPath)
	if prefixPath == "" && runtime.GOOS == "linux" {
		return deriveLinuxQGISEnv()
	}

	env := map[string]string{}
	if prefixPath != "" {
		env["QGIS_PREFIX_PATH"] = prefixPath
		env["AUTOCARTO_QGIS_PREFIX_PATH"] = prefixPath
	}
	if resourcesDir != "" {
		projDir := filepath.Join(resourcesDir, "proj")
		if dirExists(projDir) {
			env["PROJ_LIB"] = projDir
		}
		gdalDir := filepath.Join(resourcesDir, "gdal")
		if dirExists(gdalDir) {
			env["GDAL_DATA"] = gdalDir
		}
		pythonDir := filepath.Join(resourcesDir, "python")
		if dirExists(pythonDir) {
			pythonPaths := []string{pythonDir}
			pluginsDir := filepath.Join(pythonDir, "plugins")
			if dirExists(pluginsDir) {
				pythonPaths = append(pythonPaths, pluginsDir)
			}
			env["PYTHONPATH"] = prependEnvPath("", pythonPaths...)
		}
	}
	return env
}

func deriveLinuxQGISEnv() map[string]string {
	env := map[string]string{}
	for _, prefix := range []string{"/usr", "/usr/local"} {
		pythonDir := filepath.Join(prefix, "share", "qgis", "python")
		pluginsDir := filepath.Join(pythonDir, "plugins")
		if dirExists(pythonDir) {
			paths := []string{pythonDir}
			if dirExists(pluginsDir) {
				paths = append(paths, pluginsDir)
			}
			env["PYTHONPATH"] = prependEnvPath("", paths...)
			env["QGIS_PREFIX_PATH"] = prefix
			env["AUTOCARTO_QGIS_PREFIX_PATH"] = prefix
			break
		}
	}
	return env
}

func inferQGISRuntimeDirsFromPython(pythonPath string) (prefixPath string, resourcesDir string) {
	trimmed := filepath.Clean(strings.TrimSpace(pythonPath))
	if trimmed == "." || trimmed == "" {
		return "", ""
	}

	current := filepath.Dir(trimmed)
	for depth := 0; depth < 6; depth++ {
		base := filepath.Base(current)
		parent := filepath.Dir(current)
		if strings.EqualFold(base, "MacOS") {
			resources := filepath.Join(parent, "Resources")
			if dirExists(resources) {
				return current, resources
			}
		}
		if parent == current {
			break
		}
		current = parent
	}
	return "", ""
}

func prependEnvPath(existing string, values ...string) string {
	seen := map[string]struct{}{}
	parts := make([]string, 0, len(values)+4)
	appendPath := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		for _, item := range filepath.SplitList(trimmed) {
			cleaned := strings.TrimSpace(item)
			if cleaned == "" {
				continue
			}
			if _, ok := seen[cleaned]; ok {
				continue
			}
			seen[cleaned] = struct{}{}
			parts = append(parts, cleaned)
		}
	}
	for _, value := range values {
		appendPath(value)
	}
	appendPath(existing)
	return strings.Join(parts, string(os.PathListSeparator))
}

func dirExists(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	info, err := os.Stat(trimmed)
	return err == nil && info.IsDir()
}
