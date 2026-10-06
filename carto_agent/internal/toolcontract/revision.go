package toolcontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

func CatalogSchemaRevision(entries []CatalogEntry) string {
	return revisionFromEntries(entries, catalogSchemaSignature)
}

func CatalogAvailabilityRevision(entries []CatalogEntry) string {
	return revisionFromEntries(entries, catalogAvailabilitySignature)
}

func revisionFromEntries(entries []CatalogEntry, signature func(CatalogEntry) string) string {
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		parts = append(parts, name+"="+signature(entry))
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:8])
}

func catalogSchemaSignature(entry CatalogEntry) string {
	rawSchema, _ := json.Marshal(entry.InputSchema)
	return strings.Join([]string{
		strings.TrimSpace(entry.Name),
		strings.TrimSpace(entry.Runtime),
		strings.TrimSpace(entry.Backend),
		strings.TrimSpace(entry.Domain),
		strings.TrimSpace(entry.Description),
		string(rawSchema),
	}, "|")
}

func catalogAvailabilitySignature(entry CatalogEntry) string {
	instanceIDs := append([]string(nil), entry.InstanceIDs...)
	sort.Strings(instanceIDs)
	rawInstanceIDs, _ := json.Marshal(instanceIDs)
	available := "false"
	if entry.Available {
		available = "true"
	}
	return strings.Join([]string{
		strings.TrimSpace(entry.Name),
		available,
		strings.TrimSpace(entry.UnavailableReason),
		string(rawInstanceIDs),
	}, "|")
}
