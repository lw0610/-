package skill

import (
	"testing"

	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

func TestThematicWorkflowToolsReachRuntimeCatalog(t *testing.T) {
	toolRegistry := toolpkg.NewRegistry()
	if err := toolRegistry.LoadDirectories(repoSamplePath(t, "autocarto/tools/system")); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(toolRegistry)
	if err := registry.LoadDirectories(repoPluginSkillPackagesPath(t), repoSkillPackagesPath(t)); err != nil {
		t.Fatal(err)
	}
	items, err := BuildCatalogItemsFromRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	catalog := make(map[string]bool)
	for _, item := range items {
		catalog[item.Name] = true
	}
	for packageName, names := range map[string][]string{
		"cartography-styling": {"cartography_graduated_style", "cartography_add_labels", "layer_get_style_info"},
		"print-layout":        {"layout_set_map_extent", "layout_set_map_layers"},
	} {
		assigned := make(map[string]bool)
		for _, definition := range registry.ToolsForPackages([]string{packageName}) {
			assigned[definition.Metadata.Name] = true
		}
		for _, name := range names {
			if !assigned[name] || !catalog[name] {
				t.Fatalf("tool YAML %s must be referenced by %s and included in the runtime catalog", name, packageName)
			}
		}
	}
	workflow, ok := registry.GetPackage("thematic-map-styling")
	if !ok || len(workflow.RequiresTools) == 0 {
		t.Fatal("thematic workflow must declare its actual renderer verification dependencies")
	}
	for _, name := range workflow.RequiresTools {
		if !catalog[name] {
			t.Fatalf("thematic workflow requires inaccessible tool %s", name)
		}
	}
}
