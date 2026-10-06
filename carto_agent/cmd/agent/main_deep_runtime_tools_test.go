package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/skill"
)

type fakeRuntimeBackend struct {
	id string
}

func (b fakeRuntimeBackend) ID() string { return b.id }

func (b fakeRuntimeBackend) Execute(context.Context, string, map[string]any) (*skill.BackendResult, error) {
	return &skill.BackendResult{Success: true, Output: map[string]any{"ok": true}}, nil
}

func TestPreparedBaseToolsStayUnique(t *testing.T) {
	builtinSkillsDir := config.ResolveBuiltinSkillsDir()
	if builtinSkillsDir == "" {
		t.Fatal("expected builtin skills dir from plugin root discovery")
	}
	registry, err := loadRuntimeSkillRegistry(builtinSkillsDir, "", "", filepath.Join(".."), "")
	if err != nil {
		t.Fatalf("loadRuntimeSkillRegistry failed: %v", err)
	}
	engine := skill.NewEngine(registry,
		fakeRuntimeBackend{id: skill.RuntimeIDCLI},
		fakeRuntimeBackend{id: skill.RuntimeIDInternal},
	)
	baseTools, err := skill.BuildToolsFromRegistry(registry, engine)
	if err != nil {
		t.Fatalf("BuildToolsFromRegistry failed: %v", err)
	}
	prepared := prepareAgentTools(baseTools)

	seen := make(map[string]struct{})
	for _, item := range prepared {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info failed: %v", err)
		}
		if _, exists := seen[info.Name]; exists {
			t.Fatalf("duplicate prepared tool name: %s", info.Name)
		}
		seen[info.Name] = struct{}{}
	}
	if len(prepared) == 0 {
		t.Fatal("expected prepared runtime tools")
	}
}

func TestLoadRuntimeSkillRegistryUsesConfigResolver(t *testing.T) {
	pluginRoot := t.TempDir()
	pluginDir := filepath.Join(pluginRoot, "skills")
	writeRuntimeSkillPackage(t, pluginDir, "plugin-registry", "plugin_registry_tool")
	registry, err := loadRuntimeSkillRegistry(filepath.Join(t.TempDir(), "builtin"), filepath.Join(t.TempDir(), "user"), pluginDir, t.TempDir(), "")
	if err != nil {
		t.Fatalf("loadRuntimeSkillRegistry failed: %v", err)
	}
	if _, ok := registry.Get("plugin_registry_tool"); !ok {
		t.Fatal("expected plugin_registry_tool to be loaded via config resolver")
	}
}

func TestLoadRuntimeSkillRegistryKeepsDynamicToolsWithoutVirtualPackage(t *testing.T) {
	bridgeDynamicRegistry := skill.NewBridgeDynamicRegistry()
	bridgeDynamicRegistry.RegisterInstanceTools("instance-a", []bridgepkg.ToolDefinition{{
		Name:        "flash_feature",
		Description: "Flash feature.",
		Category:    "vector",
		InputSchema: map[string]any{"type": "object"},
	}})

	registry, err := loadRuntimeSkillRegistry(t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), "", bridgeDynamicRegistry)
	if err != nil {
		t.Fatalf("loadRuntimeSkillRegistry failed: %v", err)
	}
	if _, ok := registry.Get("flash_feature"); !ok {
		t.Fatal("expected dynamic bridge tool to stay available in runtime registry")
	}
	if _, ok := registry.GetPackage("_bridge-dynamic"); ok {
		t.Fatal("expected _bridge-dynamic virtual package to be removed")
	}
}
