package knowledge

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// RouteReview records a source-based decision independently of renderer fields.
// Retained path IDs are stable. An explicitly rejected interpretation remains
// in Before/Decisions; withdrawing it never removes entities, events or evidence.
type RouteReview struct {
	ChunkID           string          `json:"chunk_id"`
	RouteIDs          []string        `json:"route_ids"`
	Before            []any           `json:"before"`
	Decisions         []ReviewedRoute `json:"decisions,omitempty"`
	Resolved          []string        `json:"resolved,omitempty"`
	Continuations     []Continuation  `json:"continuations,omitempty"`
	WithdrawnRouteIDs []string        `json:"withdrawn_route_ids,omitempty"`
	Applied           bool            `json:"applied"`
	Error             string          `json:"error,omitempty"`
}

type ReviewedRoute struct {
	RouteID        string                 `json:"route_id"`
	Status         string                 `json:"status"`
	Disposition    RouteDisposition       `json:"disposition,omitempty"`
	Explanation    string                 `json:"explanation"`
	SourceRefs     []string               `json:"source_refs"`
	Paths          []ReviewedPath         `json:"paths"`
	ResolvedIssues []string               `json:"resolved_issues,omitempty"`
	Continuations  []ReviewedContinuation `json:"continuations,omitempty"`
}

type ReviewedPath struct {
	PathID    string         `json:"path_id"`
	Role      string         `json:"role"`
	AttachRef string         `json:"attach_ref,omitempty"`
	Nodes     []ReviewedNode `json:"nodes"`
}

type ReviewedNode struct {
	RefType string `json:"ref_type"`
	RefID   string `json:"ref_id"`
}

type RouteDisposition string

const (
	RouteRetain    RouteDisposition = "retain"
	RouteNotARoute RouteDisposition = "not_a_route"
)

type ReviewedContinuation struct {
	EarlierPathID string   `json:"earlier_path_id"`
	LaterPathID   string   `json:"later_path_id"`
	SourceRefs    []string `json:"source_refs"`
}

// SemanticReview is the only current model review record. Legacy route and
// boundary shapes above remain data-only so saved histories stay readable.
type SemanticChange struct {
	SubjectRefs []string `json:"subject_refs"`
	SourceRefs  []string `json:"source_refs"`
	Explanation string   `json:"explanation"`
}
type SemanticReview struct {
	ID          string           `json:"id"`
	ChunkIDs    []string         `json:"chunk_ids"`
	SubjectRefs []string         `json:"subject_refs"`
	SourceRefs  []string         `json:"source_refs"`
	Applied     bool             `json:"applied"`
	Error       string           `json:"error,omitempty"`
	Changes     []SemanticChange `json:"changes,omitempty"`
}
type semanticScope struct {
	graph   Graph
	chunks  []Chunk
	sources []EvidenceSource
	ids     map[string]bool
	issues  []Issue
	input   map[string]any
}

func hasFailedSemanticReviews(r *Result) bool {
	for _, record := range r.SemanticReviews {
		if !record.Applied {
			return true
		}
	}
	return len(r.SemanticReviews) == 0
}

func semanticIssueView(issues []Issue, sources []EvidenceSource) []modelIssue {
	out := []modelIssue{}
	for _, i := range issues {
		refs := []string{}
		seen := map[string]bool{}
		for _, q := range i.EvidenceQuotes {
			for _, s := range sources {
				if strings.Contains(s.Text, q) || strings.Contains(q, s.Text) {
					if !seen[s.Ref] {
						refs = append(refs, s.Ref)
						seen[s.Ref] = true
					}
				}
			}
		}
		out = append(out, modelIssue{ID: i.ID, Kind: i.Kind, Reason: i.Reason, Field: i.Field, SubjectRefs: i.SubjectRefs, Message: i.Message, SourceRefs: refs})
	}
	return out
}

func validateSemanticChange(before Graph, out modelResponse, oldIssues []Issue, sources []EvidenceSource) ([]Issue, error) {
	issues, err := decodeModelIssues(out.Unresolved, out.Graph, sources)
	if err != nil {
		return nil, err
	}
	old, err := indexGraph(before)
	if err != nil {
		return nil, err
	}
	next, err := indexGraph(out.Graph)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for id := range old {
		known[id] = true
	}
	for id := range next {
		known[id] = true
	}
	for _, i := range oldIssues {
		known[i.ID] = true
	}
	explained := map[string]bool{}
	changeSources := map[string]map[string]bool{}
	for _, change := range out.Changes {
		if strings.TrimSpace(change.Explanation) == "" || len(change.SubjectRefs) == 0 {
			return nil, fmt.Errorf("每项changes须说明修改对象和理由")
		}
		if _, err := sourceQuotes(change.SourceRefs, sources); err != nil {
			return nil, err
		}
		for _, id := range change.SubjectRefs {
			if !known[id] {
				return nil, fmt.Errorf("changes引用未知对象 %s", id)
			}
			explained[id] = true
			if changeSources[id] == nil {
				changeSources[id] = map[string]bool{}
			}
			for _, ref := range change.SourceRefs {
				changeSources[id][ref] = true
			}
		}
	}
	if !reflect.DeepEqual(before["task"], out.Graph["task"]) || !reflect.DeepEqual(before["output_constraints"], out.Graph["output_constraints"]) {
		return nil, fmt.Errorf("语义核对不能改变任务元数据")
	}
	for from, to := range out.Aliases {
		source, ok := old[from]
		target, exists := next[to]
		if !ok || !exists || from == to || source.kind != target.kind || source.kind == "evidence" {
			return nil, fmt.Errorf("无效同类对象归并 %s → %s", from, to)
		}
		if _, still := next[from]; still {
			return nil, fmt.Errorf("归并后的旧对象 %s 仍重复存在", from)
		}
		if !explained[from] {
			return nil, fmt.Errorf("归并 %s 缺少修改依据", from)
		}
		// Both sides retain their exact source occurrences, even for repeated text.
		oldEvidence := map[string]bool{}
		for _, id := range []string{from, to} {
			if previous, exists := old[id]; exists {
				for _, ref := range array(previous.data["evidence_refs"]) {
					refs, e := evidenceSourceRefs(old[stringValue(ref)].data, sources)
					if e != nil {
						return nil, e
					}
					for _, r := range refs {
						oldEvidence[r] = true
					}
				}
			}
		}
		for _, ref := range array(target.data["evidence_refs"]) {
			refs, e := evidenceSourceRefs(next[stringValue(ref)].data, sources)
			if e != nil {
				return nil, e
			}
			for _, r := range refs {
				delete(oldEvidence, r)
			}
		}
		if len(oldEvidence) > 0 {
			return nil, fmt.Errorf("归并 %s 未保留双方完整原文证据", from)
		}
	}
	beforeView, err := graphModelView(before, sources)
	if err != nil {
		return nil, err
	}
	afterView, err := graphModelView(out.Graph, sources)
	if err != nil {
		return nil, err
	}
	// Alias identity/evidence has already been validated above. Updating a
	// reference to that retained identity is mechanical and requires no second
	// explanation for each affected event, path and containing route. Only
	// reference fields are rewritten; names, evidence and other facts stay exact.
	if err := rewriteGraphReferences(beforeView, func(id string) (string, error) {
		return resolveAlias(id, out.Aliases)
	}); err != nil {
		return nil, err
	}
	normalizeSemanticComparison(beforeView)
	normalizeSemanticComparison(afterView)
	oldWire, _ := indexGraph(beforeView)
	newWire, _ := indexGraph(afterView)
	var coverageErrors []error
	for _, id := range sortedGraphObjectIDs(old) {
		obj := old[id]
		if obj.kind == "evidence" {
			continue
		}
		newer, exists := next[id]
		if !exists && obj.kind != "routes" && obj.kind != "paths" && out.Aliases[id] == "" {
			return nil, fmt.Errorf("核对不能丢弃事实对象 %s；同一实体须明确aliases归并", id)
		}
		if !exists || newer.kind != obj.kind || !reflect.DeepEqual(semanticObjectComparison(oldWire[id]), semanticObjectComparison(newWire[id])) {
			if !explained[id] {
				coverageErrors = append(coverageErrors, fmt.Errorf("对象 %s 修改或移除缺少changes依据", id))
			}
		}
	}
	for _, id := range sortedGraphObjectIDs(next) {
		obj := next[id]
		if obj.kind != "evidence" {
			if _, exists := old[id]; !exists && !explained[id] && !newPathExplainedByNewRoute(id, obj, old, newWire, changeSources) {
				coverageErrors = append(coverageErrors, fmt.Errorf("新增对象 %s 缺少changes依据", id))
			}
		}
	}
	retained := map[string]bool{}
	for _, i := range issues {
		retained[i.ID] = true
	}
	for _, i := range oldIssues {
		if !retained[i.ID] && !explained[i.ID] {
			coverageErrors = append(coverageErrors, fmt.Errorf("疑问 %s 被省略但没有解决依据", i.ID))
		}
	}
	if err := errors.Join(coverageErrors...); err != nil {
		return nil, err
	}
	if err := validatePathActorIssues(out.Graph, issues); err != nil {
		return nil, err
	}
	return issues, nil
}

func applySemanticScope(r *Result, scope semanticScope, out modelResponse, issues []Issue) error {
	next, err := cloneGraph(r.Graph)
	if err != nil {
		return err
	}
	previous, err := indexGraph(next)
	if err != nil {
		return err
	}
	for _, col := range graphCollections {
		kept := []any{}
		seen := map[string]bool{}
		for _, raw := range array(next[col.key]) {
			obj := raw.(map[string]any)
			id := stringValue(obj[col.id])
			if scope.ids[id] && col.key != "evidence" {
				continue
			}
			kept = append(kept, obj)
			seen[id] = true
		}
		for _, raw := range array(out.Graph[col.key]) {
			obj := raw.(map[string]any)
			id := stringValue(obj[col.id])
			if seen[id] {
				if col.key == "evidence" {
					continue
				}
				return fmt.Errorf("核对新增ID与范围外对象冲突: %s", id)
			}
			kept = append(kept, obj)
			seen[id] = true
		}
		next[col.key] = kept
	}
	aliases := map[string]string{}
	for from, to := range r.Aliases {
		aliases[from] = to
	}
	for from, to := range out.Aliases {
		aliases[from] = to
	}
	if err := rewriteGraphReferences(next, func(id string) (string, error) { return resolveAlias(id, aliases) }); err != nil {
		return err
	}
	if report := InspectGraphConsistency(next); len(report.Hard) > 0 {
		return fmt.Errorf("候选影响范围外引用: %s", report.Hard[0].Message)
	}
	preserveConstraintRuntimeMetadata(next, previous)

	remaining := []Issue{}
	handled := map[string]bool{}
	for _, i := range scope.issues {
		handled[i.ID] = true
	}
	for _, i := range r.Issues {
		if !handled[i.ID] {
			remaining = append(remaining, i)
		}
	}
	remaining = append(remaining, issues...)
	for i := range remaining {
		for j, ref := range remaining[i].SubjectRefs {
			id, err := resolveAlias(ref, aliases)
			if err != nil {
				return err
			}
			remaining[i].SubjectRefs[j] = id
		}
		if err := validateIssueSubjects(next, nil, remaining[i]); err != nil {
			return err
		}
	}
	r.Graph, r.Aliases, r.Issues = next, aliases, remaining
	return nil
}

// No old automatic review can satisfy the new runtime contract. Historical
// records remain readable; explicitly handled manual drafts use their own gate.
func ValidateSemanticReviewCoverage(r *Result, _ ...map[string]bool) error {
	if r == nil || r.Completion == nil || r.Completion.Version != 2 || !r.Completion.ExtractionComplete || !r.Completion.SemanticComplete || hasFailedSemanticReviews(r) {
		return fmt.Errorf("缺少完整、已应用的统一语义核对")
	}
	covered := map[string]bool{}
	for _, review := range r.SemanticReviews {
		for _, id := range review.ChunkIDs {
			covered[id] = true
		}
	}
	for _, chunk := range r.Chunks {
		if !covered[chunk.Chunk.ID] {
			return fmt.Errorf("片段 %s 未经过语义核对", chunk.Chunk.ID)
		}
	}
	return nil
}

func validatePathActorIssues(g Graph, issues []Issue) error {
	index, err := indexGraph(g)
	if err != nil {
		return err
	}
	for id, obj := range index {
		if obj.kind != "paths" || len(array(obj.data["actor_refs"])) > 0 {
			continue
		}
		bound := false
		for _, i := range issues {
			if i.Kind != "actor" || i.Reason != "context" || (i.Field != "actor_refs" && i.Field != "identity") {
				continue
			}
			for _, ref := range i.SubjectRefs {
				bound = bound || ref == id
			}
		}
		if !bound {
			return fmt.Errorf("路径 %s 缺少移动主体，须绑定actor/context问题", id)
		}
	}
	return nil
}

// Review models own the relation, not display dimensions or lookup output.
// Retain program metadata only while the semantic constraint is unchanged;
// reordering constraints is harmless, while editing an anchor invalidates it.
func preserveConstraintRuntimeMetadata(next Graph, previous map[string]graphObject) {
	index, _ := indexGraph(next)
	for id, object := range index {
		old, exists := previous[id]
		if !exists || object.kind != "paths" || old.kind != "paths" {
			continue
		}
		if options, exists := old.data["control_options"]; exists {
			object.data["control_options"] = options
		}
		for _, raw := range array(object.data["spatial_constraints"]) {
			constraint, _ := raw.(map[string]any)
			reference, ok := constraint["reference"].(map[string]any)
			if !ok {
				continue
			}
			for _, previousRaw := range array(old.data["spatial_constraints"]) {
				prior, _ := previousRaw.(map[string]any)
				priorReference, _ := prior["reference"].(map[string]any)
				same := reference["name"] == priorReference["name"]
				occurrence, exists := schemaNumber(constraint["occurrence"])
				if !exists {
					occurrence = 1
				}
				previousOccurrence, exists := schemaNumber(prior["occurrence"])
				if !exists {
					previousOccurrence = 1
				}
				same = same && occurrence == previousOccurrence
				for _, field := range []string{"kind", "bearing", "relation", "target_ref", "after_ref", "before_ref"} {
					same = same && constraint[field] == prior[field]
				}
				if !same {
					continue
				}
				for _, field := range []string{"lon", "lat", "coordinate_source", "coordinate_confidence", "geocoding"} {
					if value, exists := priorReference[field]; exists {
						reference[field] = value
					}
				}
				break
			}
		}
	}
}
