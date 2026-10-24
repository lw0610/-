package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRuntimeDirectoryDiagnosticsDoesNotRewriteConfig(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "enio2/runtime")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, path string
		warning    bool
	}{{"inside", filepath.Join(home, "data"), false}, {"relative", "data", false}, {"sibling-prefix", home + "-old/data", true}, {"outside", filepath.Join(root, "Eino/runtime/data"), true}} {
		t.Run(test.name, func(t *testing.T) {
			cfg := Config{DataDir: test.path, Log: LogConfig{Dir: filepath.Join(root, "actual-logs")}}
			before := cfg
			result := cfg.DirectoryDiagnostics(home, cfg.Log.Dir)
			if (len(result.Warnings) > 0) != test.warning || result.LogFile != filepath.Join(root, "actual-logs/agent.log") {
				t.Fatalf("wrong diagnostic %+v", result)
			}
			if !reflect.DeepEqual(cfg, before) {
				t.Fatal("diagnostic rewrote configuration")
			}
			if test.warning && result.Warnings[0].Code != "DATA_DIR_OUTSIDE_RUNTIME_HOME" {
				t.Fatalf("wrong code %+v", result.Warnings)
			}
		})
	}
}
func TestRuntimeDirectoryDiagnosticsFindsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "runtime")
	outside := filepath.Join(root, "old-project")
	for _, dir := range []string{home, outside} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(home, "linked")); err != nil {
		t.Fatal(err)
	}
	cfg := Config{DataDir: filepath.Join(home, "linked/not-yet-created")}
	if result := cfg.DirectoryDiagnostics(home, ""); len(result.Warnings) != 1 {
		t.Fatalf("symlink escape missed %+v", result)
	}
}
func TestRuntimeDirectoryDiagnosticsUsesActualStartupLogPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_HOME", filepath.Join(root, "runtime"))
	t.Setenv("AUTOCARTO_DISABLE_KEYRING", "1")
	cfg := &Config{DataDir: filepath.Join(root, "external"), Log: LogConfig{Dir: filepath.Join(root, "actual-logs")}}
	service := NewConfigService(nil, cfg, "", filepath.Join(root, "runtime/config/config.yaml"))
	cfg.Log.Dir = filepath.Join(root, "new-setting-for-next-launch")
	if value := service.GetRuntimeDirectoryDiagnostics(); value.LogFile != filepath.Join(root, "actual-logs/agent.log") || len(value.Warnings) != 1 {
		t.Fatalf("reported configured rather than actual logger path %+v", value)
	}
}
