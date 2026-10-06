package knowledge

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

func validateReviewPendingIssues(issues []ReviewPendingIssue) error {
	if len(issues) > 2000 {
		return fmt.Errorf("%w: 待核对问题过多", ErrReviewInvalid)
	}
	seen := map[string]bool{}
	for _, issue := range issues {
		if strings.TrimSpace(issue.ID) == "" || seen[issue.ID] || len(issue.ID) > 300 || len(issue.Message) > 4000 || strings.TrimSpace(issue.Message) == "" || len(issue.SubjectRefs) > 1000 || len(issue.EvidenceQuotes) > 50 {
			return fmt.Errorf("%w: 待核对问题标识、内容或范围不合法", ErrReviewInvalid)
		}
		seen[issue.ID] = true
		refs := map[string]bool{}
		for _, id := range issue.SubjectRefs {
			if strings.TrimSpace(id) == "" || refs[id] || len(id) > 300 {
				return fmt.Errorf("%w: 待核对问题 %s 的对象范围不合法", ErrReviewInvalid, issue.ID)
			}
			refs[id] = true
		}
		for _, quote := range issue.EvidenceQuotes {
			if len(quote) > 8000 {
				return fmt.Errorf("%w: 待核对问题原文片段过长", ErrReviewInvalid)
			}
		}
	}
	return nil
}

func reviewInitialGraph(r *Review) Graph {
	if r.InitialGraph != nil {
		return r.InitialGraph
	}
	if r.OriginalResult != nil {
		return r.OriginalResult.Graph
	}
	return nil
}

// Only fields addressing the question count as a correction. Renaming an
// unrelated route, changing its color, or editing another entity never counts.
func reviewSubjectFields(issue ReviewPendingIssue, obj graphObject) []string {
	if issue.Phase == "semantic_review_failure" {
		switch obj.kind {
		case "actors":
			return []string{"name", "aliases", "evidence_refs"}
		case "spatial_entities":
			return []string{"name", "aliases", "map_role", "lon", "lat", "evidence_refs"}
		case "events":
			return []string{"name", "event_type", "location_ref", "actor_refs", "time_text", "description", "evidence_refs"}
		case "routes":
			return []string{"paths", "evidence_refs"}
		case "paths":
			return []string{"nodes", "role", "attach_ref", "actor_refs", "evidence_refs"}
		case "point_presentations":
			return []string{"anchor_ref", "anchor_ref_type", "symbol_type", "visible"}
		case "callouts":
			return []string{"anchor_ref", "anchor_ref_type", "text", "image_path"}
		}
	}
	// Route-pass questions use the legacy "nodes" field for the whole path
	// relationship. Correcting only a junction or branch/merge role counts too.
	// Persisted decision snapshots already bind these fields independently.
	if issue.Phase == "route_review" && issue.Field == "nodes" && (obj.kind == "routes" || obj.kind == "paths") {
		return []string{"nodes", "role", "attach_ref"}
	}
	switch issue.Field {
	case "map_role":
		switch obj.kind {
		case "spatial_entities":
			return []string{"map_role"}
		case "events":
			return []string{"location_ref"}
		case "routes":
			return []string{"paths"}
		case "paths":
			return []string{"nodes", "attach_ref"}
		}
	case "time_text", "actor_refs", "location_ref", "nodes", "attach_ref":
		return []string{issue.Field}
	case "continuation", "after_last_known":
		if obj.kind == "routes" {
			return []string{"paths", "actor_refs", "time_text"}
		}
		return []string{"nodes", "actor_refs", "attach_ref"}
	case "identity":
		switch obj.kind {
		case "actors":
			return []string{"name", "aliases"}
		case "spatial_entities":
			return []string{"name", "aliases", "lon", "lat"}
		case "events":
			if issue.Kind == "actor" {
				return []string{"actor_refs"}
			}
			if issue.Kind == "place" {
				return []string{"location_ref"}
			}
		case "routes", "paths":
			if issue.Kind == "actor" {
				return []string{"actor_refs"}
			}
			return []string{"paths", "nodes", "actor_refs", "attach_ref"}
		}
	}
	// A typed object with no field still has a limited semantic surface. Do not
	// include descriptions, evidence or presentation fields in the correction.
	switch obj.kind {
	case "actors":
		return []string{"name", "aliases"}
	case "spatial_entities":
		return []string{"name", "aliases", "lon", "lat"}
	case "events":
		return []string{"location_ref", "actor_refs", "time_text"}
	case "routes":
		return []string{"paths", "actor_refs"}
	case "paths":
		return []string{"nodes", "actor_refs", "attach_ref"}
	}
	return nil
}

func reviewSubjectValue(obj graphObject, field string) any {
	if obj.kind == "routes" && (field == "paths" || field == "nodes" || field == "attach_ref" || field == "role") {
		paths := []any{}
		for _, item := range array(obj.data["paths"]) {
			path, ok := item.(map[string]any)
			if !ok {
				continue
			}
			projection := map[string]any{"path_id": path["path_id"]}
			if field == "paths" {
				for _, key := range []string{"nodes", "actor_refs", "attach_ref", "role"} {
					projection[key] = path[key]
				}
			} else {
				projection[field] = path[field]
			}
			paths = append(paths, projection)
		}
		return paths
	}
	return obj.data[field]
}

// The correction proof is narrowly scoped to the question, while the persisted
// decision snapshot binds a route's entire semantic shape. A later branch or
// attachment edit must not inherit a previous node-order approval.
func reviewCorrectionHash(r *Review, graph Graph, issue ReviewPendingIssue) (string, error) {
	return reviewIssueHash(r, graph, issue, false, true)
}

func reviewSubjectHash(r *Review, graph Graph, issue ReviewPendingIssue) (string, error) {
	return reviewIssueHash(r, graph, issue, true, true)
}

// Node semantics include their referenced location's role. Editing a place in
// the place tab is a real correction even when route node IDs stay unchanged;
// subsequently changing that role must invalidate the old human approval.
func reviewRouteReferenceRoles(index map[string]graphObject, obj graphObject) map[string]any {
	out := map[string]any{}
	var add func(string)
	add = func(id string) {
		if _, exists := out[id]; exists || id == "" {
			return
		}
		target := index[id]
		switch target.kind {
		case "spatial_entities":
			out[id] = map[string]any{"kind": target.kind, "map_role": target.data["map_role"]}
		case "events":
			out[id] = map[string]any{"kind": target.kind, "location_ref": target.data["location_ref"]}
			add(stringValue(target.data["location_ref"]))
		default:
			out[id] = nil
		}
	}
	paths := []any{obj.data}
	if obj.kind == "routes" {
		paths = array(obj.data["paths"])
	}
	for _, raw := range paths {
		path, _ := raw.(map[string]any)
		add(stringValue(path["attach_ref"]))
		for _, node := range array(path["nodes"]) {
			ref, _ := node.(map[string]any)
			add(stringValue(ref["ref_id"]))
		}
	}
	return out
}

func reviewIssueHash(r *Review, graph Graph, issue ReviewPendingIssue, decisionSnapshot, referenceRoles bool) (string, error) {
	if len(issue.SubjectRefs) == 0 {
		return "", fmt.Errorf("问题尚未绑定可安全修改的对象，需要重新抽取或补齐模型核对")
	}
	base, err := indexGraph(reviewInitialGraph(r))
	if err != nil {
		return "", err
	}
	current, err := indexGraph(graph)
	if err != nil {
		return "", err
	}
	projection := map[string]any{}
	for _, id := range issue.SubjectRefs {
		original, exists := base[id]
		if !exists {
			return "", fmt.Errorf("原始问题对象 %s 不存在，不能声明已处理", id)
		}
		row, exists := current[id]
		if decisionSnapshot && (original.kind == "routes" || original.kind == "paths") {
			ownerID := id
			if original.kind == "paths" {
				ownerID = stringValue(original.owner["route_id"])
			}
			owner, ownerExists := current[ownerID]
			if !ownerExists {
				projection[id] = nil
				continue
			}
			actualOwner := ownerID
			if original.kind == "paths" && exists {
				actualOwner = stringValue(row.owner["route_id"])
			}
			selected := map[string]any{"kind": original.kind, "owner_id": ownerID, "current_owner_id": actualOwner, "subject_present": exists,
				"paths": reviewSubjectValue(owner, "paths"), "actor_refs": owner.data["actor_refs"], "time_text": owner.data["time_text"]}
			if referenceRoles {
				selected["reference_roles"] = reviewRouteReferenceRoles(current, owner)
			}
			projection[id] = selected
			continue
		}
		if !exists {
			projection[id] = nil
			continue
		}
		if row.kind != original.kind {
			return "", fmt.Errorf("问题对象 %s 类型已变化", id)
		}
		fields := reviewSubjectFields(issue, original)
		if len(fields) == 0 {
			return "", fmt.Errorf("问题对象 %s 没有可核对字段", id)
		}
		selected := map[string]any{"kind": row.kind}
		for _, field := range fields {
			selected[field] = reviewSubjectValue(row, field)
		}
		if referenceRoles && issue.Kind == "route" && (row.kind == "routes" || row.kind == "paths") {
			switch issue.Field {
			case "", "nodes", "attach_ref", "map_role", "continuation", "after_last_known":
				selected["reference_roles"] = reviewRouteReferenceRoles(current, row)
			}
		}
		projection[id] = selected
	}
	raw, err := json.Marshal(projection)
	if err != nil {
		return "", err
	}
	return reviewHash(raw), nil
}

func validateReviewResolution(r *Review, graph Graph, issue ReviewPendingIssue, decision ReviewIssueResolution) error {
	if decision.Source != "human_review" || strings.TrimSpace(decision.Explanation) == "" || len(decision.Explanation) > 2000 || decision.Revision < 2 || decision.Revision > r.Revision || decision.ResolvedAt == "" {
		return fmt.Errorf("缺少有效的人工逐项处理记录")
	}
	if !sameStringSet(decision.SubjectRefs, issue.SubjectRefs) {
		return fmt.Errorf("处理范围与问题对象不一致")
	}
	if decision.SnapshotVersion != 0 && decision.SnapshotVersion != 2 {
		return fmt.Errorf("不支持的逐项核对快照版本")
	}
	// Historical approvals predate reference-role binding. They remain readable
	// and reusable with their exact original hash; all newly saved decisions use
	// v2. The independent final graph gate still rejects existing contradictions.
	hash, err := reviewIssueHash(r, graph, issue, true, decision.SnapshotVersion >= 2)
	if err != nil {
		return err
	}
	if hash != decision.SubjectSHA256 {
		return fmt.Errorf("问题对象已再次修改，请重新逐项核对")
	}
	correctionHash, err := reviewCorrectionHash(r, graph, issue)
	if err != nil {
		return err
	}
	initialHash, err := reviewCorrectionHash(r, reviewInitialGraph(r), issue)
	if err != nil {
		return err
	}
	if decision.Action != "confirmed" && correctionHash == initialHash {
		return fmt.Errorf("相关对象尚无有效修正，不能仅勾选问题已处理")
	}
	switch decision.Action {
	case "confirmed":
		if issue.Phase == "graph_consistency" || issue.Phase == "adapter" {
			return fmt.Errorf("已定位的图谱语义矛盾必须实际修正或明确排除相关路线，不能原样确认")
		}
		// Confirmation is a per-question human judgment, not a fabricated
		// model conclusion and never a substitute for completing extraction.
		completion := (*ExtractionCompletion)(nil)
		if r.OriginalResult != nil {
			completion = r.OriginalResult.Completion
		}
		if !extractionProcessingComplete(completion) {
			return fmt.Errorf("抽取处理尚未完成，不能用人工原样确认跳过")
		}
		initialSnapshot, err := reviewIssueHash(r, reviewInitialGraph(r), issue, true, decision.SnapshotVersion >= 2)
		if err != nil {
			return err
		}
		if hash != initialSnapshot {
			return fmt.Errorf("本项关联语义已改动，请选择已修正并说明修改，不能记为原样确认")
		}
		if len(issue.EvidenceQuotes) == 0 {
			return fmt.Errorf("本项缺少可核对的原文片段，不能直接确认原结果")
		}
		if err := validateQuotes(issue.EvidenceQuotes, r.SourceText); err != nil {
			return fmt.Errorf("本项原文依据无效，不能确认原结果: %w", err)
		}
		checked, err := cloneGraph(graph)
		if err != nil {
			return err
		}
		if err = checkReviewReferences(checked); err != nil {
			return err
		}
		index, err := indexGraph(checked)
		if err != nil {
			return err
		}
		for _, id := range issue.SubjectRefs {
			obj := index[id]
			if obj.data == nil {
				return fmt.Errorf("原样确认对象 %s 已缺失", id)
			}
			if obj.kind == "routes" {
				if err := checkRouteStructure(checked, obj.data, false); err != nil {
					return err
				}
			}
			if obj.kind == "paths" {
				if err := checkRouteStructure(checked, obj.owner, false); err != nil {
					return err
				}
			}
		}
	case "edited":
		// Deletion of an uncertain route must be disclosed as exclusion, not
		// disguised as a correction to facts that remain present in the source.
		base, _ := indexGraph(reviewInitialGraph(r))
		current, _ := indexGraph(graph)
		for _, id := range issue.SubjectRefs {
			if (base[id].kind == "routes" || base[id].kind == "paths") && current[id].data == nil {
				return fmt.Errorf("移除路线须明确选择“不纳入制图”，不能记为已修正")
			}
		}
		if issue.Phase == "graph_consistency" {
			for _, remaining := range graphConsistencyPendingIssues(graph) {
				if remaining.ID == issue.ID {
					return fmt.Errorf("本项图谱语义矛盾仍然存在，请修正相关地图用途或引用关系")
				}
			}
		}
	case "excluded":
		if issue.Kind != "route" {
			return fmt.Errorf("只有路线问题可以明确选择不纳入制图")
		}
		base, _ := indexGraph(reviewInitialGraph(r))
		current, _ := indexGraph(graph)
		routes := 0
		for _, id := range issue.SubjectRefs {
			obj := base[id]
			routeID := id
			if obj.kind == "paths" {
				routeID = stringValue(obj.owner["route_id"])
			} else if obj.kind != "routes" {
				// A consistency question also points at the contradictory place
				// so the user can correct it. Excluding its owner route does not
				// authorize deleting or modifying that underlying source fact.
				if issue.Phase != "graph_consistency" || !reflect.DeepEqual(obj.data, current[id].data) {
					return fmt.Errorf("路线排除范围包含非路线事实，必须完整保留该事实，不能整项跳过")
				}
				continue
			}
			routes++
			if _, exists := current[routeID]; exists {
				return fmt.Errorf("必须先从草稿移除该问题涉及的整条路线，不能仅删路径或忽略疑问")
			}
		}
		if routes == 0 {
			return fmt.Errorf("本项未绑定可排除的路线，必须修正相关事实")
		}
	default:
		return fmt.Errorf("不支持该处理方式，请逐项确认、修正或明确排除路线")
	}
	return nil
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa, bb := append([]string{}, a...), append([]string{}, b...)
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func applyReviewIssueResolutions(r *Review, inputs []ReviewIssueResolutionInput) error {
	issues := map[string]ReviewPendingIssue{}
	for _, issue := range r.PendingIssues {
		issues[issue.ID] = issue
	}
	decisions := map[string]ReviewIssueResolution{}
	for _, decision := range r.Resolutions {
		decisions[decision.IssueID] = decision
	}
	seen := map[string]bool{}
	for _, input := range inputs {
		issue, exists := issues[input.IssueID]
		if !exists || seen[input.IssueID] {
			return fmt.Errorf("%w: 未知或重复的问题处理 ID", ErrReviewInvalid)
		}
		seen[input.IssueID] = true
		hash, err := reviewSubjectHash(r, r.Graph, issue)
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrReviewInvalid, issue.Message, err)
		}
		decision := ReviewIssueResolution{IssueID: issue.ID, Action: input.Action, Explanation: strings.TrimSpace(input.Explanation), SubjectRefs: append([]string{}, issue.SubjectRefs...), SubjectSHA256: hash, SnapshotVersion: 2, Revision: r.Revision, ResolvedAt: time.Now().UTC().Format(time.RFC3339Nano), Source: "human_review"}
		if err := validateReviewResolution(r, r.Graph, issue, decision); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrReviewInvalid, issue.Message, err)
		}
		decisions[issue.ID] = decision
	}
	r.Resolutions = nil
	for _, issue := range r.PendingIssues {
		if decision, ok := decisions[issue.ID]; ok {
			r.Resolutions = append(r.Resolutions, decision)
		}
	}
	return nil
}

func validReviewIssueResolutions(r *Review, graph Graph) []ReviewIssueResolution {
	out := []ReviewIssueResolution{}
	issues := map[string]ReviewPendingIssue{}
	for _, issue := range r.PendingIssues {
		issues[issue.ID] = issue
	}
	for _, decision := range r.Resolutions {
		issue, exists := issues[decision.IssueID]
		if exists && validateReviewResolution(r, graph, issue, decision) == nil {
			out = append(out, decision)
		}
	}
	return out
}

// Pending model questions are audit information. Clicking Start Mapping accepts
// the saved extraction without requiring individual semantic resolutions. The
// compiler-copy preparation separately checks concrete graph references.
func ValidateReviewPending(r *Review, graph Graph) []ReviewError {
	if r == nil {
		return []ReviewError{{Code: "pending_review_missing", Message: "缺少待核对记录"}}
	}
	return nil
}
