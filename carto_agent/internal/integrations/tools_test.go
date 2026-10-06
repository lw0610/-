package integrations

import (
	"context"
	"github.com/autocarto/carto_agent/internal/geeruntime"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/skill"
)

type fixedConfig struct{ config.IntegrationsConfig }

func (c fixedConfig) GetIntegrationsConfig() (config.IntegrationsConfig, error) {
	v := c.IntegrationsConfig
	v.SetDefaults()
	return v, nil
}

func TestFailureSummaryPreservesCloudTaskAfterVerboseLogs(t *testing.T) {
	summary := failureSummary(&geeruntime.Result{Status: "timed_out", Stdout: strings.Repeat("x", 64000), ScriptPath: "/output/run/script.py", OutputDir: "/output/run", Tasks: []map[string]any{{"id": "KEEP_THIS_TASK_ID", "state": "SUBMITTED"}}})
	if !strings.Contains(summary, "KEEP_THIS_TASK_ID") || !strings.Contains(summary, "/output/run/script.py") || len(summary) > 8000 {
		t.Fatalf("missing recovery data or unbounded summary: %s", summary)
	}
}

func TestIntegrationToolsUseSessionContextAndRespectDisabled(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	for _, name := range []string{"gee_run_script", "browser_call_tool", "web_download_file"} {
		_, err := Invoke(context.Background(), name, map[string]any{"session_id": "forged", "output_dir": t.TempDir()}, fixedConfig{})
		if err == nil {
			t.Fatalf("%s accepted model session override", name)
		}
		ctx := skill.WithSessionContext(context.Background(), skill.SessionContext{SessionID: "test", ProjectPath: filepath.Join(t.TempDir(), "project.qgz")})
		_, err = Invoke(ctx, name, map[string]any{}, fixedConfig{})
		if err == nil {
			t.Fatalf("%s ran while disabled", name)
		}
	}
}

func TestIntegrationToolsRejectInvalidSessionStorageIdentity(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "project.qgz")
	for _, session := range []skill.SessionContext{
		{SessionID: "test"},
		{SessionID: "test", ProjectPath: "relative.qgz"},
		{SessionID: "../escape", ProjectPath: project},
		{SessionID: "with spaces", ProjectPath: project},
	} {
		ctx := skill.WithSessionContext(context.Background(), session)
		for _, name := range []string{"gee_run_script", "browser_list_tools", "web_download_file"} {
			_, err := Invoke(ctx, name, map[string]any{}, fixedConfig{})
			if err == nil || !strings.Contains(err.Error(), "当前会话输出目录不可用") {
				t.Errorf("%s did not reject invalid storage identity %+v: %v", name, session, err)
			}
		}
	}
}
