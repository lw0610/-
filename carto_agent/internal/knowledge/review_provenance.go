package knowledge

import (
	"encoding/json"
	"fmt"
	"sort"
)

func reviewManualRecords(r *Review, prepared Graph) []ReviewManualRecord {
	available := map[string]bool{}
	for _, collection := range graphCollections {
		for _, item := range array(prepared[collection.key]) {
			if row, ok := item.(map[string]any); ok {
				available[collection.key+"/"+stringValue(row[collection.id])] = true
			}
		}
	}
	// Compare accepted source rows before projecting their IDs. Automatic split
	// routes inherit only an existing human edit, never a manufactured one.
	adaptations := reviewRenderingAdaptations(r.Graph, prepared)
	for _, adaptation := range adaptations {
		available["routes/"+adaptation.SourceRouteID] = true
	}
	records := map[string]ReviewManualRecord{}
	for _, provenance := range r.InheritedProvenance {
		for _, record := range provenance.ManualRecords {
			key := record.Collection + "/" + record.ID
			if available[key] && records[key].Origin != "manual_added" {
				records[key] = record
			}
		}
	}
	for _, collection := range graphCollections {
		if collection.key == "evidence" {
			continue
		}
		baseline := map[string]string{}
		baselineRows := map[string]map[string]any{}
		for _, item := range array(reviewInitialGraph(r)[collection.key]) {
			if row, ok := item.(map[string]any); ok {
				raw, _ := json.Marshal(row)
				baseline[stringValue(row[collection.id])] = string(raw)
				baselineRows[stringValue(row[collection.id])] = row
			}
		}
		for _, item := range array(r.Graph[collection.key]) {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id := stringValue(row[collection.id])
			key := collection.key + "/" + id
			if id == "" || !available[key] {
				continue
			}
			raw, _ := json.Marshal(row)
			before, exists := baseline[id]
			if exists && (collection.key == "spatial_entities" || collection.key == "routes") {
				beforeJSON, _ := json.Marshal(reviewComparisonRow(r, baselineRows[id], row))
				before = string(beforeJSON)
				raw, _ = json.Marshal(reviewComparisonRow(r, row, row))
			}
			if !exists {
				records[key] = ReviewManualRecord{collection.key, id, "manual_added"}
			} else if before != string(raw) && records[key].Origin != "manual_added" {
				records[key] = ReviewManualRecord{collection.key, id, "manual_edited"}
			}
		}
	}
	for _, adaptation := range adaptations {
		if adaptation.SourceRouteID == adaptation.RenderedRouteID || adaptation.RenderedRouteID == "" {
			continue
		}
		if original, exists := records["routes/"+adaptation.SourceRouteID]; exists {
			original.ID = adaptation.RenderedRouteID
			records["routes/"+original.ID] = original
		}
	}
	for _, adaptation := range adaptations {
		if adaptation.SourceRouteID != adaptation.RenderedRouteID {
			delete(records, "routes/"+adaptation.SourceRouteID)
		}
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ReviewManualRecord, 0, len(keys))
	for _, key := range keys {
		out = append(out, records[key])
	}
	return out
}

func (s *ReviewStore) inheritReviewProvenanceLocked(r, selector *Review) error {
	// The caller's object is a selector only. No supplied origin, decision or
	// graph is trusted as a substitute for the stored published snapshot.
	previous, err := s.getLocked(selector.SessionID, selector.ReviewID)
	if err != nil {
		return err
	}
	// The accepted graph is the reusable edition. ApprovedGraph is only its
	// rendering projection, which can omit isolated paths and split route IDs.
	acceptedJSON, err := json.Marshal(previous.Graph)
	if err != nil {
		return err
	}
	initialJSON, err := json.Marshal(r.Graph)
	if err != nil {
		return err
	}
	compatibleGraph := string(acceptedJSON) == string(initialJSON) || isCoordinateCompletion(previous.Graph, r.Graph)
	if previous.Status != ReviewCompiled || previous.ApprovedRevision < 1 || previous.ApprovedRevision != selector.ApprovedRevision || previous.SourceSHA256 != r.SourceSHA256 || previous.CompatibilityKey != r.CompatibilityKey || previous.ProjectPath != r.ProjectPath || !compatibleGraph {
		return fmt.Errorf("%w: 复用来源必须是相同项目、原文与契约的已发布版本，且新草稿须保留其已确认图谱", ErrReviewInvalid)
	}
	if errors := ValidateReviewPending(previous, previous.Graph); len(errors) > 0 {
		return fmt.Errorf("%w: 已发布来源仍含未解决问题", ErrReviewInvalid)
	}
	// Preserve the actual source extraction, not an altered caller copy passed
	// alongside an otherwise valid reuse selector.
	r.OriginalResult = previous.OriginalResult
	r.CalloutImports = previous.CalloutImports
	history := append([]ReviewProvenance{}, previous.InheritedProvenance...)
	ownChanges := *previous
	ownChanges.InheritedProvenance = nil
	// Carry source IDs through reuse; signing projects these real human edits
	// onto the new drawing IDs without losing edits on point-only paths.
	current := ReviewProvenance{ReviewID: previous.ReviewID, Revision: previous.ApprovedRevision, ManualRecords: reviewManualRecords(&ownChanges, previous.Graph), IssueResolutions: validReviewIssueResolutions(previous, previous.Graph)}
	if len(current.ManualRecords) > 0 || len(current.IssueResolutions) > 0 {
		history = append(history, current)
	}
	seen := map[string]bool{}
	for _, entry := range history {
		key := fmt.Sprintf("%s/%d", entry.ReviewID, entry.Revision)
		if seen[key] || (len(entry.ManualRecords) == 0 && len(entry.IssueResolutions) == 0) {
			continue
		}
		seen[key] = true
		r.InheritedProvenance = append(r.InheritedProvenance, entry)
	}
	return nil
}
