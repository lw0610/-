package knowledge

import (
	"fmt"
	"strings"
)

// Kept solely for reading historical review records.
type IssueResolution struct {
	IssueID        string   `json:"issue_id"`
	Disposition    string   `json:"disposition"`
	SubjectRefs    []string `json:"subject_refs"`
	EvidenceQuotes []string `json:"evidence_quotes"`
	Explanation    string   `json:"explanation"`
}

func advisoryIssue(i Issue) bool { return i.Reason == "not_stated" || i.Kind == "time" }
func validateIssueSubjects(g Graph, aliases map[string]string, i Issue) error {
	protocol, knownKind := issueProtocolForKind(i.Kind)
	if !knownKind {
		return fmt.Errorf("未知问题类型 %s", i.Kind)
	}
	if i.Reason != "context" && i.Reason != "not_stated" {
		return fmt.Errorf("问题reason须为context或not_stated")
	}
	if i.Field == "" || len(i.SubjectRefs) == 0 {
		return fmt.Errorf("问题 %s 缺少对象和字段绑定", i.ID)
	}
	// An unrecorded full itinerary or connection can be retained as a warning
	// while the separately supported movements remain separate. Missing nodes
	// or attachment points in a claimed movement still require review.
	if i.Reason == "not_stated" && ((i.Kind == "route" && i.Field != "after_last_known" && i.Field != "paths" && i.Field != "continuation") || i.Kind == "place") {
		return fmt.Errorf("原文未记载不能替代已有地点或路线关系的歧义")
	}
	if !containsString(protocol.fields, i.Field) {
		return fmt.Errorf("问题 %s 的字段 %s 与类别 %s 不匹配；该类别 field 只允许：%s", i.ID, i.Field, i.Kind, strings.Join(protocol.fields, "、"))
	}
	index, err := indexGraph(g)
	if err != nil {
		return err
	}
	for _, ref := range i.SubjectRefs {
		id, err := resolveAlias(ref, aliases)
		if err != nil {
			return err
		}
		if obj, ok := index[id]; !ok || obj.kind == "evidence" {
			return fmt.Errorf("问题 %s 引用未知对象 %s", i.ID, ref)
		}
		kind := index[id].kind
		if !containsString(protocol.subjectKinds, kind) {
			return fmt.Errorf("问题 %s 的对象类型 %s 与类别 %s 不匹配", i.ID, kind, i.Kind)
		}
	}
	return nil
}
func validateQuotes(quotes []string, source string) error {
	_, err := anchorEvidenceQuotes(source, quotes)
	return err
}
func validateIssues(issues []Issue, source string) error {
	seen := map[string]bool{}
	for _, i := range issues {
		if i.ID == "" || seen[i.ID] || strings.TrimSpace(i.Message) == "" {
			return fmt.Errorf("问题ID或说明缺失/重复")
		}
		seen[i.ID] = true
		if err := validateQuotes(i.EvidenceQuotes, source); err != nil {
			return err
		}
	}
	return nil
}
