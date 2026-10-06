package knowledge

// This file applies explicit, evidence-backed model decisions. It deliberately
// does not infer identity from names or connect routes by sorting events.
import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Graph map[string]any

type BoundaryPlan struct {
	EntityLinks    []EntityLink      `json:"entity_links"`
	Continuations  []Continuation    `json:"continuations,omitempty"`
	RouteRelations []RouteRelation   `json:"route_relations,omitempty"`
	PathLinks      []PathLink        `json:"path_links"` // Legacy saved responses only.
	Updates        []ObjectUpdate    `json:"updates"`
	Resolved       []string          `json:"resolved"`
	Resolutions    []IssueResolution `json:"resolutions,omitempty"`
	Unresolved     []Issue           `json:"unresolved"`
}

// Continuation records a language-level decision: the later passage continues
// the earlier movement. It deliberately has no cartographic attachment point.
type Continuation struct {
	EarlierPathID  string   `json:"earlier_path_id"`
	LaterPathID    string   `json:"later_path_id"`
	EvidenceQuotes []string `json:"evidence_quotes"`
}

// RouteRelation preserves explicitly stated splitting, merging or independent
// movements. LocationRef is evidence-backed when supplied, not a drawing rule.
type RouteRelation struct {
	FromPathID     string   `json:"from_path_id"`
	ToPathID       string   `json:"to_path_id"`
	Relation       string   `json:"relation"`
	LocationRef    string   `json:"location_ref,omitempty"`
	EvidenceQuotes []string `json:"evidence_quotes"`
}

type EntityLink struct {
	FromID         string   `json:"from_id"`
	ToID           string   `json:"to_id"`
	Relation       string   `json:"relation"`
	EvidenceQuotes []string `json:"evidence_quotes"`
}

type PathLink struct {
	FromPathID     string   `json:"from_path_id"`
	ToPathID       string   `json:"to_path_id"`
	Kind           string   `json:"kind"`
	AttachRef      string   `json:"attach_ref,omitempty"`
	EvidenceQuotes []string `json:"evidence_quotes"`
}

type ObjectUpdate struct {
	ID             string         `json:"id"`
	Fields         map[string]any `json:"fields"`
	EvidenceQuotes []string       `json:"evidence_quotes"`
}

type Issue struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	Reason         string   `json:"reason,omitempty"`
	Message        string   `json:"message"`
	SubjectRefs    []string `json:"subject_refs,omitempty"`
	Field          string   `json:"field,omitempty"`
	EvidenceQuotes []string `json:"evidence_quotes"`
}

var graphCollections = []struct{ key, id string }{
	{"actors", "actor_id"}, {"spatial_entities", "entity_id"},
	{"events", "event_id"}, {"routes", "route_id"},
	{"point_presentations", "presentation_id"}, {"callouts", "callout_id"},
	{"evidence", "evidence_id"},
}

var graphReferenceKeys = map[string]bool{
	"location_ref": true, "actor_refs": true, "evidence_refs": true,
	"ref_id": true, "attach_ref": true, "anchor_ref": true,
	"target_ref": true, "after_ref": true, "before_ref": true,
}

type graphObject struct {
	kind string
	data map[string]any
	// owner is populated only for nested path objects.
	owner map[string]any
}

func cloneGraph(g Graph) (Graph, error) {
	b, err := json.Marshal(g)
	if err != nil {
		return nil, err
	}
	var copied Graph
	if err = json.Unmarshal(b, &copied); err != nil {
		return nil, err
	}
	if copied == nil {
		return nil, fmt.Errorf("graph must be an object")
	}
	return copied, nil
}

func indexGraph(g Graph) (map[string]graphObject, error) {
	index := map[string]graphObject{}
	add := func(kind, key string, value any, owner map[string]any) error {
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s item must be an object", kind)
		}
		id, ok := obj[key].(string)
		if !ok || strings.TrimSpace(id) == "" {
			return fmt.Errorf("%s requires a nonempty %s", kind, key)
		}
		if _, exists := index[id]; exists {
			return fmt.Errorf("duplicate graph ID %q", id)
		}
		index[id] = graphObject{kind: kind, data: obj, owner: owner}
		return nil
	}
	for _, collection := range graphCollections {
		value, exists := g[collection.key]
		if !exists {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an array", collection.key)
		}
		for _, item := range items {
			if err := add(collection.key, collection.id, item, nil); err != nil {
				return nil, err
			}
			if collection.key != "routes" {
				continue
			}
			route := item.(map[string]any)
			paths, ok := route["paths"].([]any)
			if !ok {
				return nil, fmt.Errorf("route %s paths must be an array", route["route_id"])
			}
			for _, path := range paths {
				if err := add("paths", "path_id", path, route); err != nil {
					return nil, err
				}
			}
		}
	}
	return index, nil
}

// GraphIDs returns only object IDs, not task IDs, enum values or style presets.
func GraphIDs(g Graph) map[string]bool {
	out := map[string]bool{}
	copied, err := cloneGraph(g)
	if err != nil {
		return out
	}
	index, err := indexGraph(copied)
	if err != nil {
		return out
	}
	for id := range index {
		out[id] = true
	}
	return out
}

// NamespaceGraph rewrites every object ID and schema reference, but never free
// text, the task ID, or style_ref (which is a preset, not an object reference).
func NamespaceGraph(g Graph, prefix string) (Graph, error) {
	if strings.TrimSpace(prefix) == "" {
		return nil, fmt.Errorf("namespace prefix is required")
	}
	copied, err := cloneGraph(g)
	if err != nil {
		return nil, err
	}
	index, err := indexGraph(copied)
	if err != nil {
		return nil, err
	}
	replacements := map[string]string{}
	for id := range index {
		replacements[id] = prefix + id
	}
	for _, collection := range graphCollections {
		for _, item := range array(copied[collection.key]) {
			obj := item.(map[string]any)
			obj[collection.id] = replacements[stringValue(obj[collection.id])]
			if collection.key == "routes" {
				for _, path := range array(obj["paths"]) {
					p := path.(map[string]any)
					p["path_id"] = replacements[stringValue(p["path_id"])]
				}
			}
		}
	}
	if err := rewriteGraphReferences(copied, func(id string) (string, error) {
		value, exists := replacements[id]
		if !exists {
			return "", fmt.Errorf("unknown graph reference %q", id)
		}
		return value, nil
	}); err != nil {
		return nil, err
	}
	return copied, nil
}

// MergeGraphs concatenates objects with globally unique IDs. Identity and route
// order are never guessed here; the first fragment owns task/display metadata.
func MergeGraphs(fragments []Graph) (Graph, error) {
	if len(fragments) == 0 {
		return nil, fmt.Errorf("at least one graph fragment is required")
	}
	merged, err := cloneGraph(fragments[0])
	if err != nil {
		return nil, err
	}
	for _, collection := range graphCollections {
		merged[collection.key] = []any{}
	}
	for i, fragment := range fragments {
		copied, err := cloneGraph(fragment)
		if err != nil {
			return nil, fmt.Errorf("fragment %d: %w", i, err)
		}
		if _, err = indexGraph(copied); err != nil {
			return nil, fmt.Errorf("fragment %d: %w", i, err)
		}
		if copied["schema_version"] != merged["schema_version"] {
			return nil, fmt.Errorf("fragment %d schema version differs", i)
		}
		firstTask, _ := merged["task"].(map[string]any)
		thisTask, _ := copied["task"].(map[string]any)
		if firstTask["input_crs"] != thisTask["input_crs"] {
			return nil, fmt.Errorf("fragment %d input CRS differs", i)
		}
		for _, collection := range graphCollections {
			merged[collection.key] = append(array(merged[collection.key]), array(copied[collection.key])...)
		}
	}
	if _, err = indexGraph(merged); err != nil {
		return nil, err
	}
	return merged, nil
}

func resolveAlias(id string, aliases map[string]string) (string, error) {
	seen := map[string]bool{}
	for aliases[id] != "" {
		if seen[id] {
			return "", fmt.Errorf("alias cycle at %q", id)
		}
		seen[id] = true
		id = aliases[id]
	}
	return id, nil
}

func resolvedNode(g Graph, node any) string {
	obj, ok := node.(map[string]any)
	if !ok {
		return ""
	}
	id := stringValue(obj["ref_id"])
	if obj["ref_type"] == "event" {
		for _, event := range array(g["events"]) {
			e := event.(map[string]any)
			if e["event_id"] == id {
				return stringValue(e["location_ref"])
			}
		}
	}
	return id
}

func rewriteGraphReferences(g Graph, resolve func(string) (string, error)) error {
	var visit func(any) error
	visit = func(value any) error {
		switch obj := value.(type) {
		case map[string]any:
			for key, child := range obj {
				if graphReferenceKeys[key] {
					switch refs := child.(type) {
					case string:
						id, err := resolve(refs)
						if err != nil {
							return err
						}
						obj[key] = id
					case []any:
						values := []any{}
						for _, item := range refs {
							id, ok := item.(string)
							if !ok {
								return fmt.Errorf("%s must contain string references", key)
							}
							id, err := resolve(id)
							if err != nil {
								return err
							}
							values = append(values, id)
						}
						obj[key] = unionStrings(nil, values)
					default:
						return fmt.Errorf("invalid reference field %s", key)
					}
				} else if err := visit(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range obj {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(map[string]any(g))
}

func unorderedPair(a, b string) string {
	items := []string{a, b}
	sort.Strings(items)
	return items[0] + "\x00" + items[1]
}

func array(value any) []any {
	values, _ := value.([]any)
	return values
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func unionStrings(left, right []any) []any {
	out := []any{}
	seen := map[string]bool{}
	for _, value := range append(append([]any{}, left...), right...) {
		text, ok := value.(string)
		if ok && text != "" && !seen[text] {
			out, seen[text] = append(out, text), true
		}
	}
	return out
}
