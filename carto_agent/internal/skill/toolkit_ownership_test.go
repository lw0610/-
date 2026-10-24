package skill

import (
	"sort"
	"testing"

	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

func TestToolsBelongToToolkits(t *testing.T) {
	tools, err := toolpkg.LoadDefinitionsFromDirectories(repoSamplePath(t, "autocarto/tools/system"))
	if err != nil {
		t.Fatal(err)
	}
	packages, err := loadPackagesFromDirectory(repoSamplePath(t, "autocarto/skills/system"))
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string][]string{}
	hidden := map[string][]string{}
	known := map[string]bool{}
	for _, d := range tools {
		known[d.Metadata.Name] = true
	}
	for _, p := range packages {
		if p.Kind != SkillPackageKindCategory {
			continue
		}
		for _, name := range p.ToolRefs {
			if !known[name] {
				t.Errorf("%s references missing tool %s", p.Name, name)
			}
			if p.Hidden {
				hidden[name] = append(hidden[name], p.Name)
			} else {
				owners[name] = append(owners[name], p.Name)
			}
		}
	}
	for _, d := range tools {
		name := d.Metadata.Name
		sort.Strings(owners[name])
		sort.Strings(hidden[name])
		if len(owners[name]) > 1 {
			t.Errorf("tool %s belongs to multiple visible toolkits: %v", name, owners[name])
		}
		if d.EffectiveVisibility() == toolpkg.VisibilityPublic && len(owners[name]) == 0 && len(hidden[name]) == 0 {
			t.Errorf("public tool %s has no toolkit", name)
		}
		if len(owners[name]) > 0 && len(hidden[name]) > 0 {
			t.Errorf("tool %s is both visible and hidden: %v / %v", name, owners[name], hidden[name])
		}
	}
}
