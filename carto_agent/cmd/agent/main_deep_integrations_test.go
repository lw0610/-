package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/integrations"
	"github.com/autocarto/carto_agent/internal/skill"
)

func TestOnlineDataToolsAreRegisteredAndDispatchInternally(t *testing.T) {
	registry, err := loadRuntimeSkillRegistry(config.ResolveBuiltinSkillsDir(), "", "", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gee_check_environment", "gee_run_script", "gee_task_status", "gee_cancel_task", "browser_check_environment", "browser_list_tools", "browser_call_tool", "browser_close", "web_read_page", "web_download_file"}
	for _, name := range want {
		definition, ok := registry.Get(name)
		if !ok || definition.Metadata.Runtime != skill.RuntimeIDInternal {
			t.Fatalf("%s missing or wrong runtime", name)
		}
	}
	cs := config.NewConfigService(nil, &config.Config{}, "", filepath.Join(t.TempDir(), "config.yaml"))
	engine := skill.NewEngine(registry, skill.NewInternalBackend(func(ctx context.Context, name string, args map[string]any) (any, error) {
		return integrations.Invoke(ctx, name, args, cs)
	}))
	ctx := skill.WithSessionContext(context.Background(), skill.SessionContext{SessionID: "integration-test", ProjectPath: filepath.Join(t.TempDir(), "project.qgz")})
	if _, err := engine.Execute(ctx, "browser_close", nil); err != nil {
		t.Fatalf("disabled close should still release resources: %v", err)
	}
	if _, err := engine.Execute(ctx, "gee_run_script", map[string]any{"code": "print('test')"}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
	var mapped map[string]any
	engine = skill.NewEngine(registry, skill.NewInternalBackend(func(_ context.Context, _ string, args map[string]any) (any, error) {
		mapped = args
		return map[string]any{"success": true}, nil
	}))
	_, err = engine.Execute(ctx, "browser_call_tool", map[string]any{"tool_name": "browser_navigate", "args": map[string]any{"url": "https://example.org"}})
	if err != nil {
		t.Fatal(err)
	}
	args, ok := mapped["args"].(map[string]any)
	if !ok || args["url"] != "https://example.org" {
		t.Fatalf("MCP argument object lost during mapping: %#v", mapped)
	}
}
