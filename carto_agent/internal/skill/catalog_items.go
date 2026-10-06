package skill

import (
	"fmt"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/middlewareclient"
)

func BuildCatalogItemsFromRegistry(registry *Registry) ([]middlewareclient.ToolCatalogItem, error) {
	if registry == nil {
		return nil, fmt.Errorf("skill registry is nil")
	}
	return BuildCatalogItemsFromDefinitions(registry.Definitions()), nil
}

func BuildCatalogItemsFromDefinitions(definitions []SkillDefinition) []middlewareclient.ToolCatalogItem {
	items := make([]middlewareclient.ToolCatalogItem, 0, len(definitions))
	for _, def := range definitions {
		def.normalize()
		name := strings.TrimSpace(def.Metadata.Name)
		if name == "" {
			continue
		}
		runtimeID := NormalizeRuntime(firstNonEmpty(def.Metadata.Runtime, def.Metadata.Backend))
		description := strings.TrimSpace(def.Spec.Description)
		if description == "" {
			description = strings.TrimSpace(def.Metadata.DisplayName)
		}
		providerID, source := catalogIdentityForRuntime(runtimeID)
		items = append(items, middlewareclient.ToolCatalogItem{
			Name:        name,
			ProviderID:  providerID,
			Description: description,
			Category:    strings.TrimSpace(def.Metadata.Category),
			Domain:      strings.TrimSpace(def.Metadata.Category),
			Source:      source,
			InputSchema: BuildSkillSchemaMap(def),
			Backend:     runtimeID,
			Available:   true,
			InstanceIDs: append([]string(nil), def.Metadata.InstanceIDs...),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
}

func catalogIdentityForRuntime(runtimeID string) (providerID string, source string) {
	normalized := NormalizeRuntime(runtimeID)
	return normalized, normalized
}
