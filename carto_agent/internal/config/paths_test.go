package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverPluginRootFindsAutocartoFromWorkingDirectory(t *testing.T) {
	t.Setenv(assetsRootEnvVar, "")
	rootDir := physicalPath(t, t.TempDir())
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
	t.Setenv(assetsRootEnvVar, "")
	rootDir := physicalPath(t, t.TempDir())
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
	rootDir := physicalPath(t, t.TempDir())
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
	rootDir := physicalPath(t, t.TempDir())
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

func TestPluginRootDiscoveryDoesNotInventMissingAssets(t *testing.T) {
	if got := discoverPluginRootFromSeed(t.TempDir()); got != "" {
		t.Fatalf("found nonexistent assets: %q", got)
	}
}

func TestResolveConfigAndDataDirectoriesPropagateRootResolutionError(t *testing.T) {
	t.Setenv(runtimeHomeEnvVar, "")
	t.Setenv(projectRootEnvVar, "relative-root")
	for _, resolve := range []func() (string, error){ResolveAutoCartoConfigDir, ResolvePluginDataDir, ResolveRuntimePortFile} {
		if _, err := resolve(); err == nil || !strings.Contains(err.Error(), projectRootEnvVar) {
			t.Fatalf("want explicit project root error, got %v", err)
		}
	}
}

func TestRuntimeLocator_UsesBundledAndAppLocalPaths(t *testing.T) {
	t.Setenv(assetsRootEnvVar, "")
	rootDir := physicalPath(t, t.TempDir())
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

func TestRuntimeHomeDefaultsToProjectDataOnEveryPlatform(t *testing.T) {
	root := t.TempDir()
	t.Setenv(projectRootEnvVar, root)
	t.Setenv(runtimeHomeEnvVar, "")
	got, err := ResolveRuntimePortFile()
	want := filepath.Join(root, "autocarto_data", "data", ".port")
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
	if _, err := os.Stat(filepath.Join(root, "autocarto_data")); !os.IsNotExist(err) {
		t.Fatalf("resolving paths created runtime data: %v", err)
	}
}

func TestProjectRelativePathsSurviveWorkingDirectoryChanges(t *testing.T) {
	root := t.TempDir()
	t.Setenv(projectRootEnvVar, root)
	t.Setenv(runtimeHomeEnvVar, "local/runtime")
	want := filepath.Join(root, "local", "runtime")
	original, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(original) })
	for _, cwd := range []string{t.TempDir(), t.TempDir()} {
		if err := os.Chdir(cwd); err != nil {
			t.Fatal(err)
		}
		got, err := ResolveAutoCartoHome()
		if err != nil || got != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	}
	internal := filepath.Join(root, "resources", "数据.csv")
	got, err := PreferProjectRelativePath(internal)
	if err != nil || got != "resources/数据.csv" {
		t.Fatalf("portable: %q, %v", got, err)
	}
	roundtrip, err := ResolveProjectPath(got)
	if err != nil || roundtrip != internal {
		t.Fatalf("roundtrip: %q, %v", roundtrip, err)
	}
	external := filepath.Join(t.TempDir(), "outside.csv")
	got, err = PreferProjectRelativePath(external)
	if err != nil || got != external {
		t.Fatalf("external: %q, %v", got, err)
	}
	for _, invalid := range []string{"", "  ", "bad\x00name"} {
		if _, err := ResolveProjectPath(invalid); err == nil {
			t.Fatalf("accepted invalid path")
		}
	}
}

func TestRuntimeRootFollowsExplicitAssetsLocation(t *testing.T) {
	t.Setenv(projectRootEnvVar, "")
	t.Setenv(runtimeHomeEnvVar, "")
	root := t.TempDir()
	assets := filepath.Join(root, "autocarto")
	if err := os.MkdirAll(filepath.Join(assets, "skills", "system"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(assetsRootEnvVar, assets)
	got, err := ResolveAutoCartoHome()
	if err != nil || got != filepath.Join(root, "autocarto_data") {
		t.Fatalf("got %q, %v", got, err)
	}
}

func physicalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
