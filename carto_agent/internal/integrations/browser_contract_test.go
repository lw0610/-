package integrations

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"gopkg.in/yaml.v3"
)

func TestBrowserDispatchGuidanceReachesActualModelToolInfo(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	skillsRoot := filepath.Join(root, "autocarto", "skills", "system")
	registry := toolpkg.NewRegistry()
	if err := registry.LoadDirectories(filepath.Join(root, "autocarto", "tools", "system", "browser-data-acquisition")); err != nil {
		t.Fatal(err)
	}
	skills := skill.NewRegistry(registry)
	if err := skills.LoadDirectories(skillsRoot); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "carto_agent", "defaults", "agent_configs", "default", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var profile config.AgentConfigFile
	if err := yaml.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	runtimeConfig := config.ConvertToRuntimeSkillConfig(&profile, skillsRoot, "", t.TempDir(), "default")
	if !slices.Contains(runtimeConfig.GetAgentMiddlewareSkills("data_engineer"), "browser-data-acquisition") {
		t.Fatal("assigned browser tool package methods must be readable")
	}
	// Tool descriptions must preserve dispatch guidance through the real
	// YAML -> registry -> model-facing adapter path, as well as package methods.
	for _, name := range []string{"browser_list_tools", "browser_call_tool"} {
		if !slices.Contains(runtimeConfig.GetAgentAllowedTools("data_engineer"), name) {
			t.Fatalf("data_engineer lacks %s", name)
		}
		definition, ok := skills.GetTool(name)
		if !ok {
			t.Fatalf("actual browser contract missing: %s", name)
		}
		info, err := skill.NewSkillAdapter(definition, nil).Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"不能直接", "browser_call_tool", `"tool_name":"browser_navigate"`, `"args":{"url"`} {
			if !strings.Contains(info.Desc, text) {
				t.Errorf("%s model-visible description lost wrapper guidance %q: %s", name, text, info.Desc)
			}
		}
	}
	for _, internalName := range []string{"browser_navigate", "browser_snapshot", "browser_click"} {
		if _, ok := registry.Get(internalName); ok || slices.Contains(runtimeConfig.GetAgentAllowedTools("data_engineer"), internalName) {
			t.Errorf("internal MCP operation %s must be routed through browser_call_tool", internalName)
		}
	}
}
