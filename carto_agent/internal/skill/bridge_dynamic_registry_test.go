package skill

import (
	"testing"

	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

func TestBridgeDynamicRegistryAppliesMergedDefinitionsToRegistry(t *testing.T) {
	t.Parallel()

	dynamicRegistry := NewBridgeDynamicRegistry()
	dynamicRegistry.RegisterInstanceTools("instance-a", []bridgepkg.ToolDefinition{{
		Name:        "data_load_vector",
		Description: "Load data.",
		Category:    "data",
		InputSchema: map[string]any{"type": "object"},
	}})
	dynamicRegistry.RegisterInstanceTools("instance-b", []bridgepkg.ToolDefinition{{
		Name:        "data_load_vector",
		Description: "Load data.",
		Category:    "data",
		InputSchema: map[string]any{"type": "object"},
	}})

	toolRegistry := toolpkg.NewRegistry()
	dynamicRegistry.ApplyToRegistry(toolRegistry)
	registry := NewRegistry(toolRegistry)

	def, ok := registry.Get("data_load_vector")
	if !ok {
		t.Fatal("expected merged dynamic definition in registry")
	}
	if len(def.Metadata.InstanceIDs) != 2 || def.Metadata.InstanceIDs[0] != "instance-a" || def.Metadata.InstanceIDs[1] != "instance-b" {
		t.Fatalf("unexpected merged instance IDs: %#v", def.Metadata.InstanceIDs)
	}

	dynamicRegistry.UnregisterInstanceTools("instance-a", []string{"data_load_vector"})
	toolRegistry = toolpkg.NewRegistry()
	dynamicRegistry.ApplyToRegistry(toolRegistry)
	registry = NewRegistry(toolRegistry)
	def, ok = registry.Get("data_load_vector")
	if !ok {
		t.Fatal("expected remaining dynamic definition in registry")
	}
	if len(def.Metadata.InstanceIDs) != 1 || def.Metadata.InstanceIDs[0] != "instance-b" {
		t.Fatalf("unexpected remaining instance IDs: %#v", def.Metadata.InstanceIDs)
	}
}

func TestBridgeDynamicRegistryRejectsSchemaConflictsForSameToolName(t *testing.T) {
	t.Parallel()

	dynamicRegistry := NewBridgeDynamicRegistry()
	dynamicRegistry.RegisterInstanceTools("instance-a", []bridgepkg.ToolDefinition{{
		Name:        "flash_feature",
		Description: "Flash feature.",
		Category:    "vector",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"INPUT": map[string]any{"type": "string"}}},
	}})
	dynamicRegistry.RegisterInstanceTools("instance-b", []bridgepkg.ToolDefinition{{
		Name:        "flash_feature",
		Description: "Flash feature.",
		Category:    "vector",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"INPUT": map[string]any{"type": "number"}}},
	}})

	toolRegistry := toolpkg.NewRegistry()
	dynamicRegistry.ApplyToRegistry(toolRegistry)
	registry := NewRegistry(toolRegistry)

	if _, ok := registry.Get("flash_feature"); ok {
		t.Fatal("expected conflicting dynamic tool definition to be rejected")
	}
}
