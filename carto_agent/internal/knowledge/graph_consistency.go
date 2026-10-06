package knowledge

import (
	"fmt"
	"sort"
	"strings"
)

// GraphConsistencyDiagnostic addresses graph-internal JSON pointers. ObjectID
// identifies the conflicting entity/reference; RouteID and PathID preserve the
// exact consumer of that object, rather than guessing ownership from names.
type GraphConsistencyDiagnostic struct {
	Code       string   `json:"code"`
	Target     string   `json:"target"`
	Message    string   `json:"message"`
	RouteID    string   `json:"route_id,omitempty"`
	PathID     string   `json:"path_id,omitempty"`
	ObjectID   string   `json:"object_id,omitempty"`
	RelatedIDs []string `json:"related_ids,omitempty"`
}

// Hard errors make references uninterpretable. Semantic conflicts retain a
// structurally readable draft for source-backed review; they are not permission
// to silently change map_role or invent movement facts.
type GraphConsistencyReport struct {
	Hard     []GraphConsistencyDiagnostic `json:"hard"`
	Semantic []GraphConsistencyDiagnostic `json:"semantic"`
}

// InspectGraphConsistency is a read-only check, independent of the renderer's
// single-main/branch-count/minimum-node restrictions on the final six tables.
func InspectGraphConsistency(g Graph) GraphConsistencyReport {
	check := graphConsistencyInspector{report: GraphConsistencyReport{Hard: []GraphConsistencyDiagnostic{}, Semantic: []GraphConsistencyDiagnostic{}}, byID: map[string][]consistencyObject{}}
	if g == nil {
		check.hard("missing_graph", "", "缺少图谱对象", consistencyObject{}, "")
		return check.report
	}
	for _, collection := range graphCollections {
		value, exists := g[collection.key]
		// Optional draft collections need not be declared.
		if !exists {
			continue
		}
		target := "/" + collection.key
		items, ok := value.([]any)
		if !ok {
			check.hard("invalid_collection", target, "图对象集合必须是数组", consistencyObject{}, "")
			continue
		}
		for i, value := range items {
			obj, ok := check.register(collection.key, collection.id, value, fmt.Sprintf("%s/%d", target, i), "")
			if !ok || collection.key != "routes" {
				continue
			}
			paths, ok := obj.data["paths"].([]any)
			if !ok {
				check.hard("invalid_paths", obj.target+"/paths", "路线的 paths 必须是数组", obj, obj.id)
				continue
			}
			for j, path := range paths {
				check.register("paths", "path_id", path, fmt.Sprintf("%s/paths/%d", obj.target, j), obj.id)
			}
		}
	}
	// All declarations are known before references are checked. Declaration
	// order, names, shared quotes, and numeric proximity never resolve identity.
	for _, obj := range check.objects {
		if obj.kind == "events" {
			if _, exists := obj.data["location_ref"]; !exists {
				check.hard("missing_reference", obj.target+"/location_ref", "事件缺少 location_ref", obj, obj.id)
			}
		}
		if obj.kind == "paths" {
			if _, exists := obj.data["nodes"]; !exists {
				check.hard("missing_nodes", obj.target+"/nodes", "路径缺少 nodes 数组", obj, obj.id)
			}
		}
		check.visit(obj.data, obj.target, obj)
		if obj.kind == "paths" {
			check.spatialConstraints(obj)
		}
	}
	return check.report
}

type consistencyObject struct {
	id, kind, target, routeID, pathID string
	data                              map[string]any
}

type graphConsistencyInspector struct {
	report  GraphConsistencyReport
	byID    map[string][]consistencyObject
	objects []consistencyObject
}

func (c *graphConsistencyInspector) hard(code, target, message string, owner consistencyObject, id string) {
	c.report.Hard = append(c.report.Hard, GraphConsistencyDiagnostic{Code: code, Target: target, Message: message,
		RouteID: owner.routeID, PathID: owner.pathID, ObjectID: id, RelatedIDs: consistencyRelatedIDs(owner.routeID, owner.pathID, owner.id, id)})
}

func (c *graphConsistencyInspector) semantic(code, target, message string, owner consistencyObject, id string) {
	c.report.Semantic = append(c.report.Semantic, GraphConsistencyDiagnostic{Code: code, Target: target, Message: message,
		RouteID: owner.routeID, PathID: owner.pathID, ObjectID: id, RelatedIDs: consistencyRelatedIDs(owner.routeID, owner.pathID, owner.id, id)})
}

func (c *graphConsistencyInspector) register(kind, idKey string, value any, target, routeID string) (consistencyObject, bool) {
	obj := consistencyObject{kind: kind, target: target, routeID: routeID}
	data, ok := value.(map[string]any)
	if !ok {
		c.hard("invalid_object", target, "图集合元素必须是对象", obj, "")
		return obj, false
	}
	obj.data, obj.id = data, stringValue(data[idKey])
	if kind == "routes" {
		obj.routeID = obj.id
	}
	if kind == "paths" {
		obj.pathID = obj.id
	}
	c.objects = append(c.objects, obj)
	if strings.TrimSpace(obj.id) == "" {
		c.hard("missing_id", target+"/"+idKey, "图对象缺少非空 "+idKey, obj, "")
		return obj, true
	}
	if previous := c.byID[obj.id]; len(previous) > 0 {
		c.hard("duplicate_id", target+"/"+idKey, fmt.Sprintf("图对象 ID %q 重复；首次声明位于 %s", obj.id, previous[0].target), obj, obj.id)
	}
	c.byID[obj.id] = append(c.byID[obj.id], obj)
	return obj, true
}

func (c *graphConsistencyInspector) visit(value any, target string, owner consistencyObject) {
	switch node := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(node))
		for key := range node {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child, pointer := node[key], target+"/"+strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
			// Each indexed path owns its diagnostics and is visited once.
			if owner.kind == "routes" && target == owner.target && key == "paths" {
				continue
			}
			if owner.kind == "paths" && target == owner.target && key == "nodes" {
				nodes, ok := child.([]any)
				if !ok {
					c.hard("invalid_nodes", pointer, "路径 nodes 必须是数组", owner, owner.id)
					continue
				}
				for i, raw := range nodes {
					place := fmt.Sprintf("%s/%d", pointer, i)
					n, ok := raw.(map[string]any)
					if !ok {
						c.hard("invalid_node", place, "路径节点必须是对象", owner, owner.id)
						continue
					}
					if _, exists := n["ref_id"]; !exists {
						c.hard("missing_reference", place+"/ref_id", "路径节点缺少 ref_id", owner, owner.id)
					}
					c.visit(n, place, owner)
				}
				continue
			}
			if !graphReferenceKeys[key] {
				c.visit(child, pointer, owner)
				continue
			}
			if key == "actor_refs" || key == "evidence_refs" {
				refs, ok := child.([]any)
				if !ok {
					c.hard("invalid_reference_shape", pointer, key+" 必须为对象 ID 数组", owner, owner.id)
					continue
				}
				for i, ref := range refs {
					c.reference(ref, key, node, fmt.Sprintf("%s/%d", pointer, i), owner)
				}
			} else {
				c.reference(child, key, node, pointer, owner)
			}
		}
	case []any:
		for i, child := range node {
			c.visit(child, fmt.Sprintf("%s/%d", target, i), owner)
		}
	}
}

func (c *graphConsistencyInspector) reference(value any, key string, parent map[string]any, target string, owner consistencyObject) {
	id, ok := value.(string)
	if !ok || strings.TrimSpace(id) == "" {
		c.hard("invalid_reference", target, key+" 必须包含非空对象 ID", owner, "")
		return
	}
	kind, typed := consistencyReferenceKind(key, parent)
	if !typed {
		c.hard("invalid_reference_type", target, key+" 的引用类型必须明确为 spatial_entity 或 event", owner, id)
	}
	matches := c.byID[id]
	if len(matches) == 0 {
		c.hard("dangling_reference", target, fmt.Sprintf("图谱引用对象 %q 不存在", id), owner, id)
		return
	}
	if len(matches) > 1 {
		c.hard("ambiguous_reference", target, fmt.Sprintf("图谱引用对象 %q 存在重复声明，不能选择其中一个", id), owner, id)
		return
	}
	if !typed {
		return
	}
	object := matches[0]
	if object.kind != kind {
		if owner.kind == "paths" && key == "attach_ref" {
			c.semantic("attach_reference_kind", target, fmt.Sprintf("路径 %s 的 attach_ref %q 必须指向主路上的空间实体，当前却指向 %s；须在路线复核中依原文修正", owner.pathID, id, object.kind), owner, id)
			return
		}
		c.hard("reference_kind_mismatch", target, fmt.Sprintf("引用 %q 必须指向 %s，实际为 %s", id, kind, object.kind), owner, id)
		return
	}
	if owner.kind != "paths" || key != "ref_id" && key != "attach_ref" {
		return
	}
	entity := object
	if object.kind == "events" {
		locations := c.byID[stringValue(object.data["location_ref"])]
		if len(locations) != 1 || locations[0].kind != "spatial_entities" {
			return
		}
		entity = locations[0]
	}
	if entity.kind == "spatial_entities" && entity.data["map_role"] == "background" {
		c.report.Semantic = append(c.report.Semantic, GraphConsistencyDiagnostic{
			Code: "background_in_route", Target: target, ObjectID: entity.id, RouteID: owner.routeID, PathID: owner.pathID,
			RelatedIDs: consistencyRelatedIDs(owner.routeID, owner.pathID, object.id, entity.id),
			Message:    fmt.Sprintf("路径 %s 的 %s 引用了仅作背景的地点 %s；须依据原文核对地点角色或路径用途，不能自动将 background 提升为 route_node", owner.pathID, key, entity.id),
		})
	}
}

// Only schema reference fields have type semantics. Free text, dates, names,
// style IDs, source_refs and quotations are never treated as graph references.
func consistencyReferenceKind(key string, parent map[string]any) (string, bool) {
	switch key {
	case "actor_refs":
		return "actors", true
	case "evidence_refs":
		return "evidence", true
	case "location_ref", "attach_ref", "target_ref", "after_ref", "before_ref":
		return "spatial_entities", true
	case "ref_id", "anchor_ref":
		typeKey := "ref_type"
		if key == "anchor_ref" {
			typeKey = "anchor_ref_type"
		}
		switch parent[typeKey] {
		case "spatial_entity":
			return "spatial_entities", true
		case "event":
			return "events", true
		}
	}
	return "", false
}

func consistencyRelatedIDs(ids ...string) []string {
	out, seen := []string{}, map[string]bool{}
	for _, id := range ids {
		if id != "" && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out
}

// Constraint anchors are actual route places. The drawing copy may insert
// controls between them, so geometric helper nodes do not affect adjacency.
func (c *graphConsistencyInspector) spatialConstraints(owner consistencyObject) {
	value, exists := owner.data["spatial_constraints"]
	if !exists {
		return
	}
	constraints, ok := value.([]any)
	if !ok {
		c.hard("invalid_spatial_constraints", owner.target+"/spatial_constraints", "方位约束必须是数组", owner, owner.id)
		return
	}
	factual := []string{}
	add := func(id string) {
		matches := c.byID[id]
		if len(matches) != 1 {
			return
		}
		entity := matches[0]
		if entity.kind == "events" {
			matches = c.byID[stringValue(entity.data["location_ref"])]
			if len(matches) != 1 {
				return
			}
			entity = matches[0]
		}
		if entity.kind == "spatial_entities" && entity.data["entity_kind"] != "cartographic_control" {
			factual = append(factual, entity.id)
		}
	}
	for _, raw := range array(owner.data["nodes"]) {
		node, _ := raw.(map[string]any)
		add(stringValue(node["ref_id"]))
	}
	attach := stringValue(owner.data["attach_ref"])
	if owner.data["role"] == "branch" && (len(factual) == 0 || factual[0] != attach) {
		nodes := factual
		factual = nil
		add(attach)
		factual = append(factual, nodes...)
	}
	if owner.data["role"] == "incoming" && (len(factual) == 0 || factual[len(factual)-1] != attach) {
		add(attach)
	}
	for i, raw := range constraints {
		target := fmt.Sprintf("%s/spatial_constraints/%d", owner.target, i)
		constraint, ok := raw.(map[string]any)
		if !ok {
			c.hard("invalid_spatial_constraint", target, "方位约束必须为对象", owner, owner.id)
			continue
		}
		require := func(key string) {
			if strings.TrimSpace(stringValue(constraint[key])) == "" {
				c.hard("invalid_spatial_constraint", target+"/"+key, "方位约束缺少 "+key, owner, owner.id)
			}
		}
		if !map[string]bool{"N": true, "NE": true, "E": true, "SE": true, "S": true, "SW": true, "W": true, "NW": true}[stringValue(constraint["bearing"])] {
			c.hard("invalid_spatial_constraint", target+"/bearing", "方位必须为八方位 N/NE/E/SE/S/SW/W/NW", owner, owner.id)
		}
		switch constraint["kind"] {
		case "approach_from":
			require("target_ref")
			if owner.data["role"] == "branch" {
				c.semantic("spatial_constraint_role", target, "来向约束用于 main 或 incoming，已知连接点出发的 branch 不添加示意箭尾", owner, owner.id)
			}
			for _, key := range []string{"after_ref", "before_ref", "reference", "relation", "occurrence"} {
				if _, exists := constraint[key]; exists {
					c.hard("invalid_spatial_constraint", target+"/"+key, "来向约束不使用 "+key, owner, owner.id)
				}
			}
			if len(factual) == 0 || factual[0] != stringValue(constraint["target_ref"]) {
				c.semantic("spatial_constraint_target", target+"/target_ref", "来向约束必须指向本路径第一个实际地点", owner, stringValue(constraint["target_ref"]))
			}
		case "via_relative":
			require("after_ref")
			require("before_ref")
			if _, exists := constraint["target_ref"]; exists {
				c.hard("invalid_spatial_constraint", target+"/target_ref", "途经方位约束不使用 target_ref", owner, owner.id)
			}
			if constraint["relation"] != "within" && constraint["relation"] != "outside" {
				c.hard("invalid_spatial_constraint", target+"/relation", "途经方位约束须区分 within（区域内）和 outside（区域外）", owner, owner.id)
			}
			reference, ok := constraint["reference"].(map[string]any)
			if !ok || strings.TrimSpace(stringValue(reference["name"])) == "" {
				c.hard("invalid_spatial_constraint", target+"/reference", "途经方位约束需要 reference.name 定位参照", owner, owner.id)
			}
			_, hasLon := reference["lon"]
			_, hasLat := reference["lat"]
			if hasLon != hasLat {
				c.hard("invalid_spatial_constraint", target+"/reference", "定位参照的经纬度必须成对提供", owner, owner.id)
			}
			occurrence := 1
			if rawOccurrence, exists := constraint["occurrence"]; exists {
				value, ok := schemaNumber(rawOccurrence)
				if !ok || value < 1 || value != float64(int(value)) {
					c.hard("invalid_spatial_constraint", target+"/occurrence", "路段出现次数 occurrence 必须为正整数", owner, owner.id)
				} else {
					occurrence = int(value)
				}
			}
			matches := 0
			for n := 1; n < len(factual); n++ {
				if factual[n-1] == stringValue(constraint["after_ref"]) && factual[n] == stringValue(constraint["before_ref"]) {
					matches++
				}
			}
			if occurrence > matches {
				c.semantic("spatial_constraint_segment", target, "途经方位约束须引用本路径实际存在的相邻路段及出现次数", owner, owner.id)
			}
		default:
			c.hard("invalid_spatial_constraint", target+"/kind", "未知方位约束类型", owner, owner.id)
		}
	}
}
