package knowledge

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Workbook provenance belongs to the review, not the contracted graph schema.
// Captions remain source metadata and never become annotation body text.
type CalloutImportSource struct {
	SourcePath string `json:"source_path"`
	Sheet      string `json:"sheet"`
	Row        int    `json:"row"`
	Place      string `json:"place"`
	Caption    string `json:"caption,omitempty"`
	CalloutID  string `json:"callout_id"`
	EntityID   string `json:"entity_id"`
}

type CalloutImportResult struct {
	Review        *Review  `json:"-"`
	ImportedCount int      `json:"imported_count"`
	UpdatedCount  int      `json:"updated_count"`
	ImageCount    int      `json:"image_count"`
	NewPlaceCount int      `json:"new_place_count"`
	Warnings      []string `json:"warnings"`
}

// ImportCallouts parses outside the lock, then commits a graph/asset/provenance
// transaction guarded by the same revision CAS as ordinary draft editing.
// Supplying graph preserves the editor's unsaved changes in that transaction.
func (s *ReviewStore) ImportCallouts(ctx context.Context, sid, rid string, revision int64, sourcePath string, graph Graph, resolve CoordinateResolver) (*CalloutImportResult, error) {
	review, err := s.Get(sid, rid)
	if err != nil {
		return nil, err
	}
	if review.Revision != revision || !reviewEditable(review) {
		return nil, ErrReviewConflict
	}
	if graph != nil {
		if err = ValidateReviewDraft(graph); err != nil {
			return nil, err
		}
	}
	rows, warnings, err := readCalloutWorkbook(sourcePath)
	if err != nil {
		return nil, err
	}
	return s.importCalloutRows(ctx, sid, rid, revision, sourcePath, graph, rows, warnings, resolve)
}

func (s *ReviewStore) importCalloutRows(ctx context.Context, sid, rid string, revision int64, sourcePath string, graph Graph, rows []calloutImportRow, warnings []string, resolve CoordinateResolver) (*CalloutImportResult, error) {
	if len(rows) == 0 || len(rows) > 2000 {
		return nil, fmt.Errorf("%w: Excel须包含1至2000条有效图文注记", ErrReviewInvalid)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := s.Get(sid, rid)
	if err != nil {
		return nil, err
	}
	if r.Revision != revision || !reviewEditable(r) {
		return nil, ErrReviewConflict
	}
	if graph == nil {
		graph = r.Graph
	}
	r.Graph, err = cloneGraph(graph)
	if err != nil {
		return nil, err
	}
	if err = ValidateReviewDraft(r.Graph); err != nil {
		return nil, err
	}
	if r.CalloutImports == nil {
		r.CalloutImports = map[string]CalloutImportSource{}
	}
	result := &CalloutImportResult{Warnings: append([]string{}, warnings...)}
	existingCallouts := map[string]bool{}
	for _, raw := range array(r.Graph["callouts"]) {
		row, _ := raw.(map[string]any)
		existingCallouts[stringValue(row["callout_id"])] = true
	}
	newCallouts, newPlaces := map[string]bool{}, map[string]bool{}
	reserved := calloutImportReservedIDs(r.Graph)
	occurrences := map[string]int{}
	for _, row := range rows {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		baseName, _ := calloutPlaceName(row.Place)
		if baseName == "" {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s第%d行缺少地点，已保留其它有效行", row.Sheet, row.Row))
			continue
		}
		identity := filepath.Clean(sourcePath) + "\x00" + row.Sheet + "\x00" + compactCalloutText(row.Place)
		occurrences[identity]++
		sourceKey := reviewHash([]byte(fmt.Sprintf("%s\x00%d", identity, occurrences[identity])))
		previous := r.CalloutImports[sourceKey]
		entityID, ambiguous := matchCalloutPlace(r, row.Place)
		// Once an ambiguous name was given its own point, subsequent imports
		// retain that explicit association instead of creating another point.
		if previous.EntityID != "" {
			for _, raw := range array(r.Graph["spatial_entities"]) {
				place, _ := raw.(map[string]any)
				if stringValue(place["entity_id"]) == previous.EntityID && place["entity_kind"] != "cartographic_control" {
					entityID, ambiguous = previous.EntityID, false
					break
				}
			}
		}
		if entityID == "" {
			entityID = uniqueCalloutImportID("place_excel_"+reviewHash([]byte(compactCalloutText(row.Place)))[:20], reserved)
			place := map[string]any{
				"entity_id": entityID, "name": compactCalloutText(row.Place), "entity_kind": "place", "map_role": "background",
				"factual": true, "evidence_refs": []any{}, "description": "Excel注记地点：" + strings.TrimSpace(row.Place) + "\n" + row.Body,
			}
			r.Graph["spatial_entities"] = append(array(r.Graph["spatial_entities"]), place)
			newPlaces[entityID] = true
			result.NewPlaceCount++
			if ambiguous {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s第%d行「%s」对应多个已有地点，已创建独立注记地点并自动定位", row.Sheet, row.Row, baseName))
			}
		}
		callouts := array(r.Graph["callouts"])
		var callout map[string]any
		// A stable source identity updates its previous row even if body text changed.
		for _, raw := range callouts {
			candidate, _ := raw.(map[string]any)
			if previous.CalloutID != "" && stringValue(candidate["callout_id"]) == previous.CalloutID {
				callout = candidate
				break
			}
		}
		// Adopt an existing hand-entered annotation at the same place with the
		// same text (including event anchors resolved to their spatial entity).
		if callout == nil {
			for _, raw := range callouts {
				candidate, _ := raw.(map[string]any)
				if calloutAnchorEntity(r.Graph, candidate) == entityID && compactCalloutText(stringValue(candidate["body"])) == compactCalloutText(row.Body) {
					callout = candidate
					break
				}
			}
		}
		if callout == nil {
			callout = map[string]any{"callout_id": uniqueCalloutImportID("callout_excel_"+sourceKey[:20], reserved), "width_mm": 42, "priority": 0, "preferred_side": "right_top"}
			r.Graph["callouts"] = append(callouts, callout)
			newCallouts[stringValue(callout["callout_id"])] = true
			result.ImportedCount++
		} else {
			result.UpdatedCount++
		}
		callout["anchor_ref_type"], callout["anchor_ref"] = "spatial_entity", entityID
		callout["title"], callout["body"] = "", row.Body
		callout["leader_style"], callout["template"] = "double", "text_only"
		callout["image_enabled"], callout["image_path"] = false, ""
		if width, valid := schemaNumber(callout["width_mm"]); !valid || width <= 0 {
			callout["width_mm"] = 42
		}
		if _, valid := schemaNumber(callout["priority"]); !valid {
			callout["priority"] = 0
		}
		image := row.Image
		var imageErr error
		if len(image) == 0 && row.ImagePath != "" {
			imagePath := row.ImagePath
			if !filepath.IsAbs(imagePath) {
				imagePath = filepath.Join(filepath.Dir(sourcePath), imagePath)
			}
			image, imageErr = readReviewImageSource(imagePath)
		}
		if imageErr == nil && len(image) > 0 {
			var asset *ReviewAsset
			s.mu.Lock()
			asset, imageErr = s.importAssetBytesLocked(r, image)
			s.mu.Unlock()
			if imageErr == nil {
				callout["image_enabled"], callout["image_path"], callout["template"] = true, asset.ImagePath, "image_top_text_bottom"
				result.ImageCount++
			}
		}
		if imageErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s第%d行「%s」图片未导入，已保留正文：%v", row.Sheet, row.Row, baseName, imageErr))
		}
		r.CalloutImports[sourceKey] = CalloutImportSource{SourcePath: filepath.Clean(sourcePath), Sheet: row.Sheet, Row: row.Row, Place: row.Place, Caption: row.Caption, CalloutID: stringValue(callout["callout_id"]), EntityID: entityID}
	}
	if result.ImportedCount+result.UpdatedCount == 0 {
		return nil, fmt.Errorf("%w: Excel中没有带地点的有效图文注记", ErrReviewInvalid)
	}
	if err = ValidateReviewDraft(r.Graph); err != nil {
		return nil, err
	}
	if ImportedCalloutsNeedCoordinates(r) {
		if resolve == nil {
			return nil, fmt.Errorf("地点定位服务不可用，Excel尚未保存，请检查模型配置后重试")
		}
		beforeCoordinates, cloneErr := cloneGraph(r.Graph)
		if cloneErr != nil {
			return nil, cloneErr
		}
		lookupGraph, cloneErr := cloneGraph(r.Graph)
		if cloneErr != nil {
			return nil, cloneErr
		}
		// Keep route nodes as geographic context, but do not resolve or write
		// route-only spatial constraints during an annotation import.
		for _, raw := range array(lookupGraph["routes"]) {
			route, _ := raw.(map[string]any)
			for _, pathRaw := range array(route["paths"]) {
				path, _ := pathRaw.(map[string]any)
				delete(path, "spatial_constraints")
			}
		}
		if err = resolve(ctx, lookupGraph, reviewCoordinateSource(r)); err != nil {
			return nil, fmt.Errorf("Excel地点自动定位未完成，草稿未修改：%w", err)
		}
		if err = ValidateResolvedCoordinates(Graph{"spatial_entities": lookupGraph["spatial_entities"]}); err != nil {
			return nil, fmt.Errorf("Excel地点自动定位未完成，草稿未修改：%w", err)
		}
		resolved := map[string]map[string]any{}
		for _, raw := range array(lookupGraph["spatial_entities"]) {
			place, _ := raw.(map[string]any)
			resolved[stringValue(place["entity_id"])] = place
		}
		for _, raw := range array(r.Graph["spatial_entities"]) {
			place, _ := raw.(map[string]any)
			candidate := resolved[stringValue(place["entity_id"])]
			if candidate == nil {
				return nil, fmt.Errorf("Excel地点自动定位未完成，草稿未修改：定位结果缺少地点%s", stringValue(place["name"]))
			}
			for _, key := range coordinateFields {
				if value, present := candidate[key]; present {
					place[key] = value
				} else {
					delete(place, key)
				}
			}
		}
		recordAutomaticCoordinates(r, beforeCoordinates)
	}
	reconcileLocatedCallouts(r, existingCallouts, newCallouts, newPlaces, result)
	// Network work finishes before acquiring the commit lock. A later manual
	// save or confirmation wins; imported rows never overwrite that newer state.
	s.mu.Lock()
	defer s.mu.Unlock()
	latest, err := s.getLocked(sid, rid)
	if err != nil {
		return nil, err
	}
	if latest.Revision != revision || !reviewEditable(latest) {
		return nil, ErrReviewConflict
	}
	// Standalone asset imports do not advance the graph revision. Preserve any
	// registrations that arrived while the workbook was being located.
	assetIDs := map[string]bool{}
	for _, asset := range r.Assets {
		assetIDs[asset.AssetID] = true
	}
	for _, asset := range latest.Assets {
		if !assetIDs[asset.AssetID] {
			r.Assets = append(r.Assets, asset)
		}
	}
	r.Revision++
	r.Resolutions = validReviewIssueResolutions(r, r.Graph)
	r.Status, r.Errors = ReviewAwaiting, []ReviewError{}
	r.ClaimRequestID, r.ClaimedRevision, r.ClaimedAt = "", 0, ""
	reviewTouch(r)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = s.writeLocked(r); err != nil {
		return nil, err
	}
	result.Review = r
	return result, nil
}

// A workbook can use a more specific place name than a hand-entered callout
// (e.g. 独树镇七里岗 versus 独树镇). Only identical text and actually matching
// resolved coordinates justify reuse; a shared name prefix never does.
func reconcileLocatedCallouts(r *Review, existing, created, newPlaces map[string]bool, result *CalloutImportResult) {
	places := map[string]map[string]any{}
	for _, raw := range array(r.Graph["spatial_entities"]) {
		row, _ := raw.(map[string]any)
		places[stringValue(row["entity_id"])] = row
	}
	removed := map[string]bool{}
	for _, raw := range array(r.Graph["callouts"]) {
		imported, _ := raw.(map[string]any)
		id := stringValue(imported["callout_id"])
		if !created[id] || compactCalloutText(stringValue(imported["body"])) == "" {
			continue
		}
		entityID := calloutAnchorEntity(r.Graph, imported)
		for _, candidateRaw := range array(r.Graph["callouts"]) {
			candidate, _ := candidateRaw.(map[string]any)
			candidateID := stringValue(candidate["callout_id"])
			if !existing[candidateID] || compactCalloutText(stringValue(candidate["body"])) != compactCalloutText(stringValue(imported["body"])) {
				continue
			}
			candidateEntity := calloutAnchorEntity(r.Graph, candidate)
			if !sameCalloutPosition(places[entityID], places[candidateEntity]) {
				continue
			}
			for _, field := range []string{"title", "body", "template", "image_enabled", "image_path", "leader_style"} {
				candidate[field] = imported[field]
			}
			if width, valid := schemaNumber(candidate["width_mm"]); !valid || width <= 0 {
				candidate["width_mm"] = 42
			}
			for key, source := range r.CalloutImports {
				if source.CalloutID == id {
					source.CalloutID, source.EntityID = candidateID, candidateEntity
					r.CalloutImports[key] = source
				}
			}
			removed[id] = true
			result.ImportedCount--
			result.UpdatedCount++
			break
		}
	}
	if len(removed) == 0 {
		return
	}
	kept := []any{}
	for _, raw := range array(r.Graph["callouts"]) {
		row, _ := raw.(map[string]any)
		if !removed[stringValue(row["callout_id"])] {
			kept = append(kept, raw)
		}
	}
	r.Graph["callouts"] = kept
	kept = []any{}
	for _, raw := range array(r.Graph["spatial_entities"]) {
		row, _ := raw.(map[string]any)
		id := stringValue(row["entity_id"])
		if newPlaces[id] && !calloutGraphReferencesEntity(r.Graph, id) {
			delete(r.CoordinateLookups, id)
			result.NewPlaceCount--
			continue
		}
		kept = append(kept, raw)
	}
	r.Graph["spatial_entities"] = kept
}

func sameCalloutPosition(a, b map[string]any) bool {
	if a == nil || b == nil || a["entity_kind"] == "cartographic_control" || b["entity_kind"] == "cartographic_control" {
		return false
	}
	for _, field := range []string{"lon", "lat"} {
		left, ok1 := schemaNumber(a[field])
		right, ok2 := schemaNumber(b[field])
		if !ok1 || !ok2 || math.IsNaN(left) || math.IsNaN(right) || math.IsInf(left, 0) || math.IsInf(right, 0) || math.Abs(left-right) > 1e-6 {
			return false
		}
	}
	return true
}

func calloutGraphReferencesEntity(graph Graph, id string) bool {
	var visit func(any) bool
	visit = func(value any) bool {
		switch row := value.(type) {
		case map[string]any:
			for key, child := range row {
				if graphReferenceKeys[key] && stringValue(child) == id {
					return true
				}
				if visit(child) {
					return true
				}
			}
		case []any:
			for _, child := range row {
				if visit(child) {
					return true
				}
			}
		}
		return false
	}
	return visit(map[string]any(graph))
}

func compactCalloutText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}

// Only bracketed administrative qualifiers and whitespace are removed. Compound
// names (e.g. 枣阳—保安寨) and suffixes stay intact; there is no fuzzy matching.
func calloutPlaceName(value string) (string, string) {
	var name, qualifier strings.Builder
	depth := 0
	for _, r := range compactCalloutText(value) {
		switch r {
		case '（', '(', '【', '[':
			depth++
		case '）', ')', '】', ']':
			if depth > 0 {
				depth--
			}
		default:
			if depth > 0 {
				qualifier.WriteRune(r)
			} else {
				name.WriteRune(r)
			}
		}
	}
	return name.String(), qualifier.String()
}

func matchCalloutPlace(r *Review, name string) (string, bool) {
	want, qualifier := calloutPlaceName(name)
	full := compactCalloutText(name)
	byID := map[string]map[string]any{}
	for _, raw := range array(r.Graph["spatial_entities"]) {
		if row, ok := raw.(map[string]any); ok {
			byID[stringValue(row["entity_id"])] = row
		}
	}
	exact, base := map[string]bool{}, map[string]bool{}
	consider := func(id string, value string) {
		if id == "" || byID[id] == nil || stringValue(byID[id]["entity_kind"]) == "cartographic_control" {
			return
		}
		candidate, candidateQualifier := calloutPlaceName(value)
		if candidate != want {
			return
		}
		if qualifier != "" && candidateQualifier != "" && !strings.Contains(qualifier, candidateQualifier) && !strings.Contains(candidateQualifier, qualifier) {
			return
		}
		_, entityQualifier := calloutPlaceName(stringValue(byID[id]["name"]))
		if qualifier != "" && entityQualifier != "" && !strings.Contains(qualifier, entityQualifier) && !strings.Contains(entityQualifier, qualifier) {
			return
		}
		if compactCalloutText(value) == full {
			exact[id] = true
		} else {
			base[id] = true
		}
	}
	knownNames := map[string][]string{}
	for _, source := range r.CalloutImports {
		knownNames[source.EntityID] = append(knownNames[source.EntityID], source.Place)
	}
	for id, row := range byID {
		if names := knownNames[id]; len(names) > 0 {
			// Workbook qualifiers remain authoritative for its newly named points.
			for _, value := range names {
				consider(id, value)
			}
		} else {
			consider(id, stringValue(row["name"]))
		}
		for _, alias := range array(row["aliases"]) {
			consider(id, stringValue(alias))
		}
	}
	for _, raw := range array(r.Graph["events"]) {
		row, _ := raw.(map[string]any)
		id := stringValue(row["location_ref"])
		consider(id, stringValue(row["name"]))
		for _, alias := range array(row["aliases"]) {
			consider(id, stringValue(alias))
		}
	}
	candidates := exact
	if len(candidates) == 0 {
		candidates = base
	}
	if len(candidates) == 1 {
		for id := range candidates {
			return id, false
		}
	}
	return "", len(candidates) > 1
}

func calloutAnchorEntity(g Graph, callout map[string]any) string {
	ref := stringValue(callout["anchor_ref"])
	if callout["anchor_ref_type"] == "spatial_entity" {
		return ref
	}
	if callout["anchor_ref_type"] == "event" {
		for _, raw := range array(g["events"]) {
			event, _ := raw.(map[string]any)
			if stringValue(event["event_id"]) == ref {
				return stringValue(event["location_ref"])
			}
		}
	}
	return ""
}

func calloutImportReservedIDs(g Graph) map[string]bool {
	ids := map[string]bool{}
	for _, collection := range graphCollections {
		for _, raw := range array(g[collection.key]) {
			row, _ := raw.(map[string]any)
			ids[stringValue(row[collection.id])] = true
			if collection.key == "routes" {
				for _, rawPath := range array(row["paths"]) {
					path, _ := rawPath.(map[string]any)
					ids[stringValue(path["path_id"])] = true
				}
			}
		}
	}
	return ids
}

func uniqueCalloutImportID(base string, reserved map[string]bool) string {
	id := base
	for suffix := 2; reserved[id]; suffix++ {
		id = fmt.Sprintf("%s_%d", base, suffix)
	}
	reserved[id] = true
	return id
}

func ImportedCalloutsNeedCoordinates(r *Review) bool {
	if r == nil {
		return false
	}
	used := map[string]bool{}
	for _, source := range r.CalloutImports {
		used[source.EntityID] = true
	}
	for _, raw := range array(r.Graph["spatial_entities"]) {
		row, _ := raw.(map[string]any)
		if used[stringValue(row["entity_id"])] && ValidateResolvedCoordinates(Graph{"spatial_entities": []any{row}}) != nil {
			return true
		}
	}
	return false
}

func reviewCoordinateSource(r *Review) string {
	if len(r.CalloutImports) == 0 {
		return r.SourceText
	}
	keys := make([]string, 0, len(r.CalloutImports))
	for key := range r.CalloutImports {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	bodies := map[string]string{}
	for _, raw := range array(r.Graph["callouts"]) {
		row, _ := raw.(map[string]any)
		bodies[stringValue(row["callout_id"])] = stringValue(row["body"])
	}
	var context strings.Builder
	context.WriteString("用户从Excel补充的图文注记地点及文字（括号内保留省县限定，仅供定位）：\n")
	for _, key := range keys {
		source := r.CalloutImports[key]
		if body, present := bodies[source.CalloutID]; present {
			fmt.Fprintf(&context, "%s：%s\n", compactCalloutText(source.Place), body)
		}
	}
	context.WriteString("\n原始行军文本：\n")
	context.WriteString(r.SourceText)
	return context.String()
}
