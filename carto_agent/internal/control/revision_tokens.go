package control

import (
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
)

func ResolveDiscoveryRevision(snapshot middlewareclient.RuntimeSnapshot) string {
	if revision := strings.TrimSpace(snapshot.DiscoveryRevision); revision != "" {
		return revision
	}
	if revision := strings.TrimSpace(snapshot.RuntimeRevision); revision != "" {
		return revision
	}
	if revision := strings.TrimSpace(snapshot.ConfigRevision); revision != "" {
		return revision
	}
	if snapshot.Version > 0 {
		return fmt.Sprintf("discovery-v%d", snapshot.Version)
	}
	return ""
}

func JoinToolCatalogRevision(schemaRevision, availabilityRevision string) string {
	schemaRevision = strings.TrimSpace(schemaRevision)
	availabilityRevision = strings.TrimSpace(availabilityRevision)
	if schemaRevision == "" {
		return availabilityRevision
	}
	if availabilityRevision == "" {
		return schemaRevision
	}
	return schemaRevision + "/" + availabilityRevision
}

func ResolvePublicToolRevisionsFromCatalog(contract toolcatalog.Contract) middlewareclient.ToolRevisions {
	schemaRevision := strings.TrimSpace(contract.SchemaRevision)
	availabilityRevision := strings.TrimSpace(contract.AvailabilityRevision)
	return middlewareclient.ToolRevisions{
		Catalog:      JoinToolCatalogRevision(schemaRevision, availabilityRevision),
		Schema:       schemaRevision,
		Availability: availabilityRevision,
	}
}
