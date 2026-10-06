package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDiscoverPluginRootFindsAutocartoFromWorkingDirectory(t *testing.T) {
	rootDir := t.TempDir()
	pluginRoot := filepath.Join(rootDir, "autocarto")
	for _, dir := range []string{
		filepath.Join(pluginRoot, "skills", "system"),
		filepath.Join(pluginRoot, "skills", "user"),
		filepath.Join(pluginRoot, "tools", "system"),
		filepath.Join(pluginRoot, "tools", "user"),
		filepath.Join(rootDir, "nested", "work"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(filepath.Join(rootDir, "nested", "work")); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	if got := DiscoverPluginRoot(); got != pluginRoot {
		t.Fatalf("DiscoverPluginRoot() = %q, want %q", got, pluginRoot)
	}
}

func TestResolveSkillAndToolDirectoriesUsePluginRootLayout(t *testing.T) {
	rootDir := t.TempDir()
	pluginRoot := filepath.Join(rootDir, "autocarto")
	for _, dir := range []string{
		filepath.Join(pluginRoot, "skills", "system"),
		filepath.Join(pluginRoot, "skills", "user"),
		filepath.Join(pluginRoot, "tools", "system"),
		filepath.Join(pluginRoot, "tools", "user"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	if got := ResolveBuiltinSkillsDir(); got != filepath.Join(pluginRoot, "skills", "system") {
		t.Fatalf("ResolveBuiltinSkillsDir() = %q", got)
	}
	if got := ResolveUserSkillsDir(); got != filepath.Join(pluginRoot, "skills", "user") {
		t.Fatalf("ResolveUserSkillsDir() = %q", got)
	}
	if got := ResolveBuiltinToolsDir(); got != filepath.Join(pluginRoot, "tools", "system") {
		t.Fatalf("ResolveBuiltinToolsDir() = %q", got)
	}
	if got := ResolveUserToolsDir(); got != filepath.Join(pluginRoot, "tools", "user") {
		t.Fatalf("ResolveUserToolsDir() = %q", got)
	}
}

func TestResolveConfigAndDataDirectoriesUsePluginRootLayout(t *testing.T) {
	rootDir := t.TempDir()
	pluginRoot := filepath.Join(rootDir, "autocarto")
	runtimeHome := filepath.Join(rootDir, "app-local")
	for _, dir := range []string{
		filepath.Join(pluginRoot, "skills", "system"),
		filepath.Join(pluginRoot, "skills", "user"),
		filepath.Join(pluginRoot, "tools", "system"),
		filepath.Join(pluginRoot, "tools", "user"),
		filepath.Join(runtimeHome, "config"),
		filepath.Join(runtimeHome, "data"),
		filepath.Join(rootDir, "nested", "work"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(filepath.Join(rootDir, "nested", "work")); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	t.Setenv(runtimeHomeEnvVar, runtimeHome)

	homeDir, err := ResolveAutoCartoHome()
	if err != nil {
		t.Fatalf("ResolveAutoCartoHome failed: %v", err)
	}
	if homeDir != runtimeHome {
		t.Fatalf("ResolveAutoCartoHome() = %q, want %q", homeDir, runtimeHome)
	}

	configDir, err := ResolveAutoCartoConfigDir()
	if err != nil {
		t.Fatalf("ResolveAutoCartoConfigDir failed: %v", err)
	}
	if configDir != filepath.Join(runtimeHome, "config") {
		t.Fatalf("ResolveAutoCartoConfigDir() = %q", configDir)
	}

	dataDir, err := ResolvePluginDataDir()
	if err != nil {
		t.Fatalf("ResolvePluginDataDir failed: %v", err)
	}
	if dataDir != filepath.Join(runtimeHome, "data") {
		t.Fatalf("ResolvePluginDataDir() = %q", dataDir)
	}

	secretsDir, err := ResolveSecretsDir()
	if err != nil {
		t.Fatalf("ResolveSecretsDir failed: %v", err)
	}
	if secretsDir != filepath.Join(runtimeHome, "config") {
		t.Fatalf("ResolveSecretsDir() = %q", secretsDir)
	}

	logDir, err := ResolveLogDir()
	if err != nil {
		t.Fatalf("ResolveLogDir failed: %v", err)
	}
	if logDir != filepath.Join(runtimeHome, "data", "logs") {
		t.Fatalf("ResolveLogDir() = %q", logDir)
	}

	userConfigFile, err := ResolveUserConfigFile()
	if err != nil {
		t.Fatalf("ResolveUserConfigFile failed: %v", err)
	}
	if userConfigFile != filepath.Join(runtimeHome, "config", "config.yaml") {
		t.Fatalf("ResolveUserConfigFile() = %q", userConfigFile)
	}

	runtimePortFile, err := ResolveRuntimePortFile()
	if err != nil {
		t.Fatalf("ResolveRuntimePortFile failed: %v", err)
	}
	if runtimePortFile != filepath.Join(runtimeHome, "data", ".port") {
		t.Fatalf("ResolveRuntimePortFile() = %q", runtimePortFile)
	}
}

func TestResolveAutoCartoHomeRespectsEnvOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom-home")
	for _, dir := range []string{
		filepath.Join(want, "config"),
		filepath.Join(want, "data"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
	}
	t.Setenv(runtimeHomeEnvVar, want)

	got, err := ResolveAutoCartoHome()
	if err != nil {
		t.Fatalf("ResolveAutoCartoHome failed: %v", err)
	}
	if got != want {
		t.Fatalf("ResolveAutoCartoHome() = %q, want %q", got, want)
	}

	configDir, err := ResolveAutoCartoConfigDir()
	if err != nil {
		t.Fatalf("ResolveAutoCartoConfigDir failed: %v", err)
	}
	if configDir != filepath.Join(want, "config") {
		t.Fatalf("ResolveAutoCartoConfigDir() = %q", configDir)
	}

	dataDir, err := ResolvePluginDataDir()
	if err != nil {
		t.Fatalf("ResolvePluginDataDir failed: %v", err)
	}
	if dataDir != filepath.Join(want, "data") {
		t.Fatalf("ResolvePluginDataDir() = %q", dataDir)
	}
}

func TestResolveAutoCartoHomeRejectsInvalidEnvOverride(t *testing.T) {
	rootDir := t.TempDir()
	invalidAssetsRoot := filepath.Join(rootDir, "invalid-assets")
	if err := os.MkdirAll(invalidAssetsRoot, 0o755); err != nil {
		t.Fatalf("mkdir invalid assets root failed: %v", err)
	}
	t.Setenv(assetsRootEnvVar, invalidAssetsRoot)

	_, err := ResolveAssetsRoot()
	if err == nil {
		t.Fatalf("ResolveAssetsRoot() error = nil, want invalid AUTOCARTO_ASSETS_ROOT error")
	}
	if !strings.Contains(err.Error(), assetsRootEnvVar) {
		t.Fatalf("ResolveAssetsRoot() error = %q, want mention %s", err, assetsRootEnvVar)
	}
	if !strings.Contains(err.Error(), "skills/system") {
		t.Fatalf("ResolveAssetsRoot() error = %q, want mention skills/system", err)
	}
}

func TestResolveAutoCartoHomeReportsDiscoveryFailureDetails(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	failureRoot := filepath.Join(t.TempDir(), "nowhere")
	if err := os.MkdirAll(failureRoot, 0o755); err != nil {
		t.Fatalf("mkdir failure root failed: %v", err)
	}
	t.Setenv(assetsRootEnvVar, "")
	if err := os.Chdir(failureRoot); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	_, err = ResolveAssetsRoot()
	if err == nil {
		t.Fatalf("ResolveAssetsRoot() error = nil, want discovery failure")
	}
	if !strings.Contains(err.Error(), assetsRootEnvVar) {
		t.Fatalf("ResolveAssetsRoot() error = %q, want mention %s", err, assetsRootEnvVar)
	}
	if !strings.Contains(err.Error(), "当前目录") {
		t.Fatalf("ResolveAssetsRoot() error = %q, want mention current directory search", err)
	}
	if !strings.Contains(err.Error(), "可执行") {
		t.Fatalf("ResolveAssetsRoot() error = %q, want mention executable search", err)
	}
	if !strings.Contains(err.Error(), "8 层") {
		t.Fatalf("ResolveAssetsRoot() error = %q, want mention search depth", err)
	}
	if !strings.Contains(err.Error(), "skills/system") {
		t.Fatalf("ResolveAssetsRoot() error = %q, want mention required marker", err)
	}
}

func TestResolveConfigAndDataDirectoriesPropagateRootResolutionError(t *testing.T) {
	t.Setenv(runtimeHomeEnvVar, "")

	tests := []struct {
		name string
		fn   func() (string, error)
	}{
		{name: "config", fn: ResolveAutoCartoConfigDir},
		{name: "data", fn: ResolvePluginDataDir},
		{name: "runtime-port", fn: ResolveRuntimePortFile},
	}

	if runtime.GOOS == "windows" {
		want := filepath.Clean(defaultWindowsRuntimeHome)
		wantByName := map[string]string{
			"config":       filepath.Join(want, "config"),
			"data":         filepath.Join(want, "data"),
			"runtime-port": filepath.Join(want, "data", ".port"),
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got, err := tc.fn()
				if err != nil {
					t.Fatalf("%s resolver error = %v, want Windows default", tc.name, err)
				}
				if got != wantByName[tc.name] {
					t.Fatalf("%s resolver = %q, want %q", tc.name, got, wantByName[tc.name])
				}
			})
		}
		return
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.fn()
			if err == nil {
				t.Fatalf("%s resolver error = nil, want root resolution failure", tc.name)
			}
			if !strings.Contains(err.Error(), "AUTOCARTO_HOME") {
				t.Fatalf("%s resolver error = %q, want mention AUTOCARTO_HOME", tc.name, err)
			}
			if !strings.Contains(err.Error(), "app-local") {
				t.Fatalf("%s resolver error = %q, want mention app-local runtime home", tc.name, err)
			}
		})
	}
}

func TestRuntimeLocator_UsesBundledAndAppLocalPaths(t *testing.T) {
	rootDir := t.TempDir()
	bundledRoot := filepath.Join(rootDir, "bundle", "autocarto")
	for _, dir := range []string{
		filepath.Join(bundledRoot, "skills", "system"),
		filepath.Join(bundledRoot, "skills", "user"),
		filepath.Join(bundledRoot, "tools", "system"),
		filepath.Join(bundledRoot, "tools", "user"),
		filepath.Join(rootDir, "bundle", "nested", "work"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
	}
	appLocal := filepath.Join(rootDir, "app-local")
	for _, dir := range []string{
		filepath.Join(appLocal, "config"),
		filepath.Join(appLocal, "data"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(filepath.Join(rootDir, "bundle", "nested", "work")); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	t.Setenv("AUTOCARTO_HOME", appLocal)

	if got := ResolveBuiltinSkillsDir(); got != filepath.Join(bundledRoot, "skills", "system") {
		t.Fatalf("ResolveBuiltinSkillsDir() = %q, want %q", got, filepath.Join(bundledRoot, "skills", "system"))
	}
	if got := ResolveBuiltinToolsDir(); got != filepath.Join(bundledRoot, "tools", "system") {
		t.Fatalf("ResolveBuiltinToolsDir() = %q, want %q", got, filepath.Join(bundledRoot, "tools", "system"))
	}

	configDir, err := ResolveAutoCartoConfigDir()
	if err != nil {
		t.Fatalf("ResolveAutoCartoConfigDir failed: %v", err)
	}
	if configDir != filepath.Join(appLocal, "config") {
		t.Fatalf("ResolveAutoCartoConfigDir() = %q, want %q", configDir, filepath.Join(appLocal, "config"))
	}

	dataDir, err := ResolvePluginDataDir()
	if err != nil {
		t.Fatalf("ResolvePluginDataDir failed: %v", err)
	}
	if dataDir != filepath.Join(appLocal, "data") {
		t.Fatalf("ResolvePluginDataDir() = %q, want %q", dataDir, filepath.Join(appLocal, "data"))
	}
}

func TestRuntimeLocator_RejectsInvalidPluginRootFallback(t *testing.T) {
	rootDir := t.TempDir()
	pluginRoot := filepath.Join(rootDir, "autocarto")
	for _, dir := range []string{
		filepath.Join(pluginRoot, "skills", "system"),
		filepath.Join(pluginRoot, "skills", "user"),
		filepath.Join(pluginRoot, "tools", "system"),
		filepath.Join(pluginRoot, "tools", "user"),
		filepath.Join(rootDir, "nested", "work"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(filepath.Join(rootDir, "nested", "work")); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	t.Setenv("AUTOCARTO_HOME", "")

	if runtime.GOOS == "windows" {
		got, err := ResolveRuntimePortFile()
		if err != nil {
			t.Fatalf("ResolveRuntimePortFile() error = %v, want Windows default", err)
		}
		want := filepath.Join(filepath.Clean(defaultWindowsRuntimeHome), "data", ".port")
		if got != want {
			t.Fatalf("ResolveRuntimePortFile() = %q, want %q", got, want)
		}
		return
	}

	_, err = ResolveRuntimePortFile()
	if err == nil {
		t.Fatalf("ResolveRuntimePortFile() error = nil, want invalid plugin root fallback rejection")
	}
	if !strings.Contains(err.Error(), "app-local") {
		t.Fatalf("ResolveRuntimePortFile() error = %q, want mention app-local runtime home", err)
	}
}
