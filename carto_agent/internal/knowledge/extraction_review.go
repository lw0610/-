package knowledge

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Version 3 records extraction without the former full-graph model review.
const ExtractionProtocolVersion = 3

type ExtractionCompletion struct {
	Version            int  `json:"version"`
	AllRoutesRequired  bool `json:"all_routes_required"`
	ExtractionComplete bool `json:"extraction_complete"`
	BoundaryComplete   bool `json:"boundary_complete"`
	RoutePassComplete  bool `json:"route_pass_complete"`
	SemanticAttempted  bool `json:"semantic_attempted"`
	SemanticComplete   bool `json:"semantic_complete"`
	SemanticSkipped    bool `json:"semantic_skipped,omitempty"`
	AdaptationComplete bool `json:"adaptation_complete"`
}

// ValidateExtractionCompletion checks the actual protocol used, without
// claiming that an intentionally skipped model review was performed. Legacy
// completed results remain readable under their original coverage rules.
func ValidateExtractionCompletion(r *Result) error {
	if r == nil || r.Completion == nil || !r.Completion.ExtractionComplete || !r.Completion.AdaptationComplete {
		return fmt.Errorf("知识抽取或本地制图整理尚未完成")
	}
	switch r.Completion.Version {
	case ExtractionProtocolVersion:
		if !extractionProcessingComplete(r.Completion) || len(r.SemanticReviews) != 0 {
			return fmt.Errorf("抽取完成状态与已跳过语义核对的协议不一致")
		}
		return nil
	case 2:
		return ValidateSemanticReviewCoverage(r)
	default:
		return fmt.Errorf("不支持的知识抽取完成协议")
	}
}

func extractionProcessingComplete(c *ExtractionCompletion) bool {
	if c == nil || !c.ExtractionComplete {
		return false
	}
	switch c.Version {
	case ExtractionProtocolVersion:
		return c.SemanticSkipped && !c.SemanticAttempted && !c.SemanticComplete
	case 2:
		return c.SemanticAttempted && !c.SemanticSkipped
	default:
		return false
	}
}

type ExtractionNeedsReviewError struct{ Message string }

const NoExtractedContentMessage = "未提取到可制图的内容，请检查原文是否正确读取，或补充材料后重新抽取。"

// Do not require routes or any particular entity type: partial extracted facts
// remain editable and usable. Only a wholly empty graph is not a drawing result.
func HasExtractedContent(g Graph) bool {
	for _, collection := range graphCollections {
		if collection.key == "evidence" || collection.key == "point_presentations" {
			continue
		}
		for _, raw := range array(g[collection.key]) {
			if item, ok := raw.(map[string]any); ok && (collection.key != "spatial_entities" || item["entity_kind"] != "cartographic_control") {
				return true
			}
		}
	}
	return false
}

func emptyExtractionError(review *Review) ReviewError {
	message := NoExtractedContentMessage
	if review != nil {
		for _, issue := range review.PendingIssues {
			if issue.Kind == "source" && strings.TrimSpace(issue.Message) != "" {
				message += " 原因：" + issue.Message
				break
			}
		}
	}
	return ReviewError{Code: "empty_extraction", Path: "graph", Message: message}
}

func (e *ExtractionNeedsReviewError) Error() string { return e.Message }
func hasBlockingExtractionIssues(issues []Issue) bool {
	for _, i := range issues {
		if !advisoryIssue(i) {
			return true
		}
	}
	return false
}
func boundedReviewMessage(value string) string {
	r := []rune(strings.TrimSpace(value))
	if len(r) > 1000 {
		return string(r[:1000]) + "……"
	}
	return string(r)
}
func checkReviewReferences(g Graph) error {
	index, err := indexGraph(g)
	if err != nil {
		return err
	}
	return rewriteGraphReferences(g, func(id string) (string, error) {
		if _, ok := index[id]; !ok {
			return "", fmt.Errorf("图谱引用对象不存在: %s", id)
		}
		return id, nil
	})
}

func ExtractionPendingIssues(r *Result) []ReviewPendingIssue {
	if r == nil {
		return nil
	}
	out := []ReviewPendingIssue{}
	for _, i := range r.Issues {
		phase := "extraction"
		if r.Completion != nil && r.Completion.Version == 2 {
			phase = "semantic_review"
		}
		if strings.HasPrefix(i.ID, "adapter:") {
			phase = "adapter"
		}
		out = append(out, ReviewPendingIssue{ID: i.ID, Kind: i.Kind, Phase: phase, Message: i.Message, SubjectRefs: i.SubjectRefs, Field: i.Field, EvidenceQuotes: i.EvidenceQuotes, Warning: advisoryIssue(i)})
	}
	index, _ := indexGraph(r.Graph)
	for _, record := range r.SemanticReviews {
		if record.Applied {
			continue
		}
		refs := []string{}
		quotes := []string{}
		for _, id := range record.SubjectRefs {
			if obj, ok := index[id]; ok && obj.kind != "evidence" {
				refs = append(refs, id)
			}
		}
		if len(refs) == 0 {
			for _, f := range r.Chunks {
				for _, c := range record.ChunkIDs {
					if f.Chunk.ID != c {
						continue
					}
					for id, obj := range index {
						if obj.kind != "evidence" && strings.HasPrefix(id, c+"__") {
							refs = append(refs, id)
						}
					}
				}
			}
		}
		for _, f := range r.Chunks {
			for _, c := range record.ChunkIDs {
				if f.Chunk.ID == c {
					for _, s := range sourceSentences(f.Chunk) {
						if len(quotes) < 50 {
							quotes = append(quotes, s.Text)
						}
					}
				}
			}
		}
		sort.Strings(refs)
		out = append(out, ReviewPendingIssue{ID: "semantic_failure:" + record.ID, Kind: "route", Phase: "semantic_review_failure", Message: boundedReviewMessage("本组语义自动核对未通过，草稿尚未确认：" + record.Error), SubjectRefs: refs, Field: "nodes", EvidenceQuotes: quotes})
	}
	out = append(out, graphConsistencyPendingIssues(r.Graph)...)
	return out
}

// One current contradiction per route and code. Individual object references
// remain in the group for precise editable-field provenance and evidence.
func graphConsistencyPendingIssues(g Graph) []ReviewPendingIssue {
	report := InspectGraphConsistency(g)
	index, err := indexGraph(g)
	if err != nil {
		return nil
	}
	groups := map[string]*ReviewPendingIssue{}
	order := []string{}
	for _, d := range report.Semantic {
		raw, _ := json.Marshal([]string{d.Code, d.RouteID})
		id := "graph_consistency:" + reviewHash(raw)[:24]
		item := groups[id]
		if item == nil {
			kind, field := "route", "map_role"
			if d.RouteID == "" {
				kind = "place"
			}
			if d.Code == "attach_reference_kind" {
				field = "attach_ref"
			}
			item = &ReviewPendingIssue{ID: id, Kind: kind, Phase: "graph_consistency", Field: field, Message: d.Message + "；请依据原文修正。"}
			groups[id] = item
			order = append(order, id)
		}
		refs := append([]string{d.RouteID, d.PathID, d.ObjectID}, d.RelatedIDs...)
		for _, ref := range refs {
			obj, ok := index[ref]
			if !ok || obj.kind == "evidence" {
				continue
			}
			if !containsString(item.SubjectRefs, ref) {
				item.SubjectRefs = append(item.SubjectRefs, ref)
			}
			for _, e := range array(obj.data["evidence_refs"]) {
				q := stringValue(index[stringValue(e)].data["quote"])
				if q != "" && !containsString(item.EvidenceQuotes, q) && len(item.EvidenceQuotes) < 50 {
					item.EvidenceQuotes = append(item.EvidenceQuotes, q)
				}
			}
		}
	}
	out := []ReviewPendingIssue{}
	for _, id := range order {
		out = append(out, *groups[id])
	}
	return out
}
func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
func activeConsistencyIssues(g Graph) map[string]bool {
	out := map[string]bool{}
	for _, i := range graphConsistencyPendingIssues(g) {
		out[i.ID] = true
	}
	return out
}
func visiblePendingIssues(r *Review) []ReviewPendingIssue {
	active := activeConsistencyIssues(r.Graph)
	out := []ReviewPendingIssue{}
	for _, i := range r.PendingIssues {
		if i.Phase == "graph_consistency" && !active[i.ID] {
			continue
		}
		out = append(out, i)
	}
	return out
}

func ReviewableExtraction(r *Result, source string) error {
	if r == nil || r.Status != "needs_review" || r.Completion == nil || (r.Completion.Version != 2 && r.Completion.Version != ExtractionProtocolVersion) || !r.Completion.ExtractionComplete {
		return fmt.Errorf("抽取尚未完整结束或抽取协议不受支持，不能提交结果草稿")
	}
	if HasPartialExtraction(r) {
		return validatePartialGraph(r.Graph)
	}
	if !extractionProcessingComplete(r.Completion) {
		return fmt.Errorf("抽取处理尚未结束")
	}
	if err := validateFragment(r.Graph, Chunk{Text: source}); err != nil {
		return err
	}
	if len(ExtractionPendingIssues(r)) == 0 {
		return fmt.Errorf("待核对草稿缺少具体问题")
	}
	return nil
}

func PrepareReviewCompilation(review *Review) (Graph, []ReviewError) {
	fail := func(err error) (Graph, []ReviewError) {
		return nil, []ReviewError{{Code: "invalid_graph", Path: "$", Message: err.Error()}}
	}
	if review == nil {
		return fail(fmt.Errorf("知识核对记录不存在"))
	}
	if !HasExtractedContent(review.Graph) {
		return nil, []ReviewError{emptyExtractionError(review)}
	}
	g, err := cloneGraph(review.Graph)
	if err != nil {
		return fail(err)
	}
	// Confirmation accepts the saved extraction. Automatic semantic findings and
	// historical completion metadata remain audit information, not a second
	// approval gate. Only an uninterpretable graph can prevent compilation.
	if report := InspectGraphConsistency(drawingReferenceGraph(g)); len(report.Hard) > 0 {
		return fail(fmt.Errorf("%s", report.Hard[0].Message))
	}
	if err = adaptAcceptedRoutes(g); err != nil {
		return fail(err)
	}
	return g, nil
}
