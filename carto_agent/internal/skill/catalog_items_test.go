package skill

import "testing"

func TestBuildCatalogItemsFromDefinitionsUsesCanonicalRuntimeIdentity(t *testing.T) {
	t.Parallel()

	items := BuildCatalogItemsFromDefinitions([]SkillDefinition{
		{
			Metadata: SkillMetadata{
				Name:        "open_project",
				DisplayName: "打开工程",
				Runtime:     RuntimeIDQGIS,
			},
			Spec: SkillSpec{
				Description: "打开 QGIS 工程",
			},
		},
		{
			Metadata: SkillMetadata{
				Name:        "buffer",
				DisplayName: "缓冲区",
				Backend:     RuntimeIDCLI,
			},
			Spec: SkillSpec{
				Description: "计算缓冲区",
			},
		},
	})

	if len(items) != 2 {
		t.Fatalf("expected 2 catalog items, got %#v", items)
	}
	byName := map[string]string{}
	bySource := map[string]string{}
	byBackend := map[string]string{}
	for _, item := range items {
		byName[item.Name] = item.ProviderID
		bySource[item.Name] = item.Source
		byBackend[item.Name] = item.Backend
	}
	if got := byName["open_project"]; got != RuntimeIDQGIS {
		t.Fatalf("expected qgis provider identity, got %q", got)
	}
	if got := bySource["open_project"]; got != RuntimeIDQGIS {
		t.Fatalf("expected qgis source identity, got %q", got)
	}
	if got := byBackend["open_project"]; got != RuntimeIDQGIS {
		t.Fatalf("expected qgis backend identity, got %q", got)
	}
	if got := byName["buffer"]; got != RuntimeIDCLI {
		t.Fatalf("expected cli provider identity, got %q", got)
	}
	if got := bySource["buffer"]; got != RuntimeIDCLI {
		t.Fatalf("expected cli source identity, got %q", got)
	}
	if got := byBackend["buffer"]; got != RuntimeIDCLI {
		t.Fatalf("expected cli backend identity, got %q", got)
	}
}

func TestBuildCatalogItemsFromDefinitionsExposeDomainAndCategory(t *testing.T) {
	t.Parallel()

	items := BuildCatalogItemsFromDefinitions([]SkillDefinition{{
		Metadata: SkillMetadata{
			Name:     "buffer",
			Category: "vector-geometry",
			Backend:  RuntimeIDCLI,
		},
		Spec: SkillSpec{Description: "计算缓冲区"},
	}})

	if len(items) != 1 {
		t.Fatalf("expected 1 catalog item, got %#v", items)
	}
	if got := items[0].Domain; got != "vector-geometry" {
		t.Fatalf("expected domain propagated, got %q", got)
	}
	if got := items[0].Category; got != "vector-geometry" {
		t.Fatalf("expected category compatibility preserved, got %q", got)
	}
}
