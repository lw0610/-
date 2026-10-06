package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
)

type CoordinateResolver func(context.Context, map[string]any, string) error

// ResolveDraftCoordinates performs slow network work outside the store lock,
// then uses the same revision check as manual edits. A concurrent save or
// confirmation wins; a late lookup must never replace that newer graph.
func (s *ReviewStore) ResolveDraftCoordinates(ctx context.Context, sid, rid string, revision int64, resolve CoordinateResolver) (*Review, error) {
	review, err := s.Get(sid, rid)
	if err != nil {
		return nil, err
	}
	if review.Revision != revision || !reviewEditable(review) {
		return nil, ErrReviewConflict
	}
	if resolve == nil {
		return nil, fmt.Errorf("地点定位服务不可用")
	}
	if err = resolve(ctx, review.Graph, reviewCoordinateSource(review)); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = ValidatePlaceCoordinates(review.Graph); err != nil {
		return nil, err
	}
	return s.saveDraftContext(ctx, sid, rid, revision, review.Graph, true)
}

// Actual places remain required for coordinate completion. Optional route
// references may be unresolved; drawing skips just their shape constraint.
func ValidatePlaceCoordinates(graph Graph) error {
	for index, raw := range array(graph["spatial_entities"]) {
		row, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("地点定位未完成：第 %d 个地点格式无效", index+1)
		}
		if err := validateCoordinateRow(row); err != nil {
			return err
		}
	}
	return nil
}

// ValidateResolvedCoordinates remains the strict completeness check for both
// actual entities and optional references, e.g. deciding whether to retry them.
func ValidateResolvedCoordinates(graph Graph) error {
	if err := ValidatePlaceCoordinates(graph); err != nil {
		return err
	}
	for _, entry := range routeCoordinateReferences(graph) {
		if entry.reference == nil || stringValue(entry.reference["name"]) == "" {
			return fmt.Errorf("路线定位参照缺少名称或格式无效")
		}
		if err := validateCoordinateRow(entry.reference); err != nil {
			return err
		}
	}
	return nil
}

func validateCoordinateRow(row map[string]any) error {
	lon, hasLon := schemaNumber(row["lon"])
	lat, hasLat := schemaNumber(row["lat"])
	if !hasLon || !hasLat || math.IsNaN(lon) || math.IsNaN(lat) || math.IsInf(lon, 0) || math.IsInf(lat, 0) || math.Abs(lon) > 180 || math.Abs(lat) > 90 {
		return fmt.Errorf("地点定位未完成：%s 尚未获得有效坐标，请重试自动定位", stringValue(row["name"]))
	}
	source := stringValue(row["coordinate_source"])
	switch source {
	case "manual", "source_text", "gazetteer", "llm_estimated", "derived_control":
	default:
		return fmt.Errorf("地点定位未完成：%s 的坐标来源无效", stringValue(row["name"]))
	}
	confidence, exists := row["coordinate_confidence"]
	if exists || source == "llm_estimated" {
		value, valid := schemaNumber(confidence)
		if !valid || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return fmt.Errorf("地点定位未完成：%s 的估算置信度无效", stringValue(row["name"]))
		}
	}
	return nil
}

// References live inside their route constraints; they are not visible places.
// Include both IDs and index to distinguish repeated references along one path.
type routeCoordinateReference struct {
	key        string
	reference  map[string]any
	constraint map[string]any
}

func routeCoordinateReferences(graph Graph) []routeCoordinateReference {
	var references []routeCoordinateReference
	for _, rawRoute := range array(graph["routes"]) {
		route, _ := rawRoute.(map[string]any)
		for _, rawPath := range array(route["paths"]) {
			path, _ := rawPath.(map[string]any)
			for index, rawConstraint := range array(path["spatial_constraints"]) {
				constraint, _ := rawConstraint.(map[string]any)
				if stringValue(constraint["kind"]) != "via_relative" {
					continue
				}
				reference, _ := constraint["reference"].(map[string]any)
				key, _ := json.Marshal([]any{route["route_id"], path["path_id"], index})
				references = append(references, routeCoordinateReference{"route_reference:" + string(key), reference, constraint})
			}
		}
	}
	return references
}

func referenceSemantics(constraint map[string]any) map[string]any {
	copy := map[string]any{}
	for key, value := range constraint {
		copy[key] = value
	}
	if reference, ok := constraint["reference"].(map[string]any); ok {
		ref := map[string]any{}
		for key, value := range reference {
			ref[key] = value
		}
		for _, field := range coordinateFields {
			delete(ref, field)
		}
		copy["reference"] = ref
	}
	return copy
}

// Confirmed graphs can gain missing coordinates in a new draft without losing
// their previous human provenance. Every other value, including existing
// coordinates and all route data, must match the stored approved graph exactly.
func isCoordinateCompletion(previous, next Graph) bool {
	if ValidateResolvedCoordinates(next) != nil {
		return false
	}
	left, err := cloneGraph(previous)
	if err != nil {
		return false
	}
	right, err := cloneGraph(next)
	if err != nil {
		return false
	}
	a, b := array(left["spatial_entities"]), array(right["spatial_entities"])
	if len(a) != len(b) {
		return false
	}
	changed := false
	stripCompletion := func(old, fresh map[string]any) bool {
		if old == nil || fresh == nil {
			return false
		}
		if validateCoordinateRow(old) == nil {
			return reflect.DeepEqual(old, fresh)
		}
		// Compatibility is for missing coordinates, never rewriting a prior point.
		if _, present := old["lon"]; present {
			return false
		}
		if _, present := old["lat"]; present {
			return false
		}
		for _, field := range coordinateFields {
			delete(old, field)
			delete(fresh, field)
		}
		changed = true
		return true
	}
	for i := range a {
		old, ok1 := a[i].(map[string]any)
		fresh, ok2 := b[i].(map[string]any)
		if !ok1 || !ok2 || !stripCompletion(old, fresh) {
			return false
		}
	}
	oldRefs, newRefs := routeCoordinateReferences(left), routeCoordinateReferences(right)
	if len(oldRefs) != len(newRefs) {
		return false
	}
	for i, old := range oldRefs {
		fresh := newRefs[i]
		if old.key != fresh.key || !stripCompletion(old.reference, fresh.reference) {
			return false
		}
	}

	return changed && reflect.DeepEqual(left, right)
}

var coordinateFields = []string{"lon", "lat", "coordinate_source", "coordinate_confidence", "geocoding", "generation_reason"}

func coordinateValues(row map[string]any) map[string]any {
	values := map[string]any{}
	for _, key := range coordinateFields {
		if value, exists := row[key]; exists {
			values[key] = value
		}
	}
	return values
}

func recordAutomaticCoordinates(review *Review, before Graph) {
	previous := map[string]map[string]any{}
	for _, raw := range array(before["spatial_entities"]) {
		if row, ok := raw.(map[string]any); ok {
			previous[stringValue(row["entity_id"])] = row
		}
	}
	for _, raw := range array(review.Graph["spatial_entities"]) {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := stringValue(row["entity_id"])
		old, exists := previous[id]
		if !exists || reflect.DeepEqual(coordinateValues(old), coordinateValues(row)) {
			continue
		}
		if review.CoordinateLookups == nil {
			review.CoordinateLookups = map[string]map[string]any{}
		}
		review.CoordinateLookups[id] = coordinateValues(row)
	}
	beforeRefs := map[string]routeCoordinateReference{}
	for _, entry := range routeCoordinateReferences(before) {
		beforeRefs[entry.key] = entry
	}
	for _, entry := range routeCoordinateReferences(review.Graph) {
		old, exists := beforeRefs[entry.key]
		if !exists || !reflect.DeepEqual(referenceSemantics(old.constraint), referenceSemantics(entry.constraint)) || reflect.DeepEqual(coordinateValues(old.reference), coordinateValues(entry.reference)) {
			continue
		}
		if review.CoordinateLookups == nil {
			review.CoordinateLookups = map[string]map[string]any{}
		}
		review.CoordinateLookups[entry.key] = coordinateValues(entry.reference)
	}
}

// Only backend-recorded lookup snapshots can exempt automatic fields from a
// manual edit comparison. Metadata submitted through PUT cannot forge this.
func reviewComparisonRow(review *Review, row map[string]any, current map[string]any) map[string]any {
	if stringValue(current["route_id"]) != "" {
		copy, err := cloneGraph(Graph(row))
		if err != nil {
			return row
		}
		byKey := map[string]routeCoordinateReference{}
		for _, entry := range routeCoordinateReferences(Graph{"routes": []any{map[string]any(copy)}}) {
			byKey[entry.key] = entry
		}
		for _, entry := range routeCoordinateReferences(Graph{"routes": []any{current}}) {
			snapshot, exists := review.CoordinateLookups[entry.key]
			if !exists || !reflect.DeepEqual(snapshot, coordinateValues(entry.reference)) {
				continue
			}
			if target, exists := byKey[entry.key]; exists {
				for _, field := range coordinateFields {
					delete(target.reference, field)
				}
			}
		}
		return copy
	}
	snapshot, exists := review.CoordinateLookups[stringValue(current["entity_id"])]
	if !exists || !reflect.DeepEqual(snapshot, coordinateValues(current)) {
		return row
	}
	copy := make(map[string]any, len(row))
	for key, value := range row {
		copy[key] = value
	}
	for _, key := range coordinateFields {
		delete(copy, key)
	}
	return copy
}

// SyncResolvedCoordinates preserves the semantic snapshot while copying only
// coordinate fields from its matching entity. Names and route semantics stay intact.
func SyncResolvedCoordinates(result *Result) {
	if result == nil || result.SemanticGraph == nil {
		return
	}
	byID := map[string]map[string]any{}
	rows, _ := result.Graph["spatial_entities"].([]any)
	for _, row := range rows {
		if item, ok := row.(map[string]any); ok {
			if id, _ := item["entity_id"].(string); id != "" {
				byID[id] = item
			}
		}
	}
	semantic, _ := result.SemanticGraph["spatial_entities"].([]any)
	for _, row := range semantic {
		item, ok := row.(map[string]any)
		if !ok {
			continue
		}
		id, _ := item["entity_id"].(string)
		matched := byID[id]
		if matched == nil {
			continue
		}
		for _, key := range coordinateFields {
			if value, exists := matched[key]; exists {
				item[key] = value
			} else {
				delete(item, key)
			}
		}
	}
	resolvedReferences := map[string]routeCoordinateReference{}
	for _, entry := range routeCoordinateReferences(result.Graph) {
		resolvedReferences[entry.key] = entry
	}
	for _, entry := range routeCoordinateReferences(result.SemanticGraph) {
		matched, exists := resolvedReferences[entry.key]
		if !exists || entry.reference == nil || !reflect.DeepEqual(referenceSemantics(entry.constraint), referenceSemantics(matched.constraint)) {
			continue
		}
		for _, key := range coordinateFields {
			if value, exists := matched.reference[key]; exists {
				entry.reference[key] = value
			} else {
				delete(entry.reference, key)
			}
		}
	}
}
