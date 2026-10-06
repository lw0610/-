package knowledge

import "sort"

func sortedGraphObjectIDs(index map[string]graphObject) []string {
	ids := make([]string, 0, len(index))
	for id := range index {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// normalizeSemanticComparison operates only on graphModelView copies. It
// recognizes representation-only differences without changing persisted facts.
func normalizeSemanticComparison(g Graph) {
	// These optional arrays may be absent or empty in the graph schema. Required
	// paths/nodes/evidence and the extraction protocol's path actor_refs are not
	// interchangeable with an omitted field.
	for _, raw := range array(g["actors"]) {
		actor := raw.(map[string]any)
		if refs, ok := actor["aliases"].([]any); ok && len(refs) == 0 {
			delete(actor, "aliases")
		}
	}
	for _, raw := range array(g["routes"]) {
		for _, rawPath := range array(raw.(map[string]any)["paths"]) {
			path := rawPath.(map[string]any)
			if constraints, ok := path["spatial_constraints"].([]any); ok && len(constraints) == 0 {
				delete(path, "spatial_constraints")
			}
			for _, rawConstraint := range array(path["spatial_constraints"]) {
				constraint, _ := rawConstraint.(map[string]any)
				if occurrence, ok := schemaNumber(constraint["occurrence"]); ok && occurrence == 1 {
					delete(constraint, "occurrence")
				}
			}
		}
	}
	for _, raw := range array(g["events"]) {
		event := raw.(map[string]any)
		if refs, ok := event["actor_refs"].([]any); ok && len(refs) == 0 {
			delete(event, "actor_refs")
		}
	}
}

// A route owns its metadata and ordered membership of paths. Each nested path
// is compared independently by its stable ID, so a documented path edit does
// not need a duplicate explanation for its otherwise unchanged container.
func semanticObjectComparison(obj graphObject) map[string]any {
	if obj.kind != "routes" {
		return obj.data
	}
	value := make(map[string]any, len(obj.data))
	for key, child := range obj.data {
		value[key] = child
	}
	pathIDs := []any{}
	for _, raw := range array(obj.data["paths"]) {
		pathIDs = append(pathIDs, raw.(map[string]any)["path_id"])
	}
	value["paths"] = pathIDs
	return value
}

// A new route's evidenced explanation may describe its new nested paths. This
// does not authorize changes to existing paths or paths added to an old route.
func newPathExplainedByNewRoute(id string, obj graphObject, old map[string]graphObject, nextWire map[string]graphObject, changeSources map[string]map[string]bool) bool {
	if obj.kind != "paths" || obj.owner == nil {
		return false
	}
	if _, exists := old[id]; exists {
		return false
	}
	ownerID := stringValue(obj.owner["route_id"])
	if _, exists := old[ownerID]; exists {
		return false
	}
	proof := changeSources[ownerID]
	refs := array(nextWire[id].data["evidence_refs"])
	if len(proof) == 0 || len(refs) == 0 {
		return false
	}
	for _, ref := range refs {
		if !proof[stringValue(ref)] {
			return false
		}
	}
	return true
}
