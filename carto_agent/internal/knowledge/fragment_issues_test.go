package knowledge

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecodeModelIssuesReportsEveryInvalidQuestion(t *testing.T) {
	sources := []EvidenceSource{{Ref: "c0001:s011", Text: "部队分别在甲地、乙地活动。"}}
	rows := []modelIssue{
		{ID: "u1", Kind: "route", Reason: "not_stated", Field: "paths", Message: "原文未描述连续路线。", SourceRefs: []string{"c0001:s011"}},
		{ID: "u2", Kind: "route", Reason: "not_stated", Field: "nodes", SubjectRefs: []string{"r1"}, Message: "无法确定节点顺序。", SourceRefs: []string{"c0001:s011"}},
		{ID: "u3", Kind: "actor", Reason: "context", Field: "identity", SubjectRefs: []string{"a1"}, Message: "主体身份有歧义。", SourceRefs: []string{"nonexistent"}},
	}
	issues, err := decodeModelIssues(rows, issueProtocolGraph(), sources)
	if err == nil || issues != nil {
		t.Fatalf("invalid response partially accepted: issues=%v err=%v", issues, err)
	}
	for _, want := range []string{"u2", "原文未记载不能替代", "u3", "原文引用无效"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("retry feedback missing %q: %v", want, err)
		}
	}
}

func TestGlobalSourceIssueDoesNotRequireInventedObject(t *testing.T) {
	message := "原文未提供任何行军路线、地点或主体信息，无法构建知识图谱。"
	rows := []modelIssue{{ID: "no_source", Kind: "route", Reason: "not_stated", Field: "paths", Message: message, SourceRefs: []string{"s1"}}}
	issues, err := decodeModelIssues(rows, Graph{}, []EvidenceSource{{Ref: "s1", Text: "读取史料文件内容"}})
	if err != nil || len(issues) != 1 || issues[0].Message != message || issues[0].Kind != "source" || len(issues[0].SubjectRefs) != 0 {
		t.Fatalf("global source explanation was lost: %#v %v", issues, err)
	}
}

func TestDecodeModelIssuesAcceptsEquivalentFieldSpellingsWithoutChangingMeaning(t *testing.T) {
	source := EvidenceSource{Ref: "c0001:s011", Text: "部队分别在甲地、乙地活动。"}
	rows := []modelIssue{
		{ID: "u1", Kind: "route", Reason: "context", Field: "paths.nodes", SubjectRefs: []string{"r1"}, Message: "原文不能确定途经次序。", SourceRefs: []string{source.Ref}},
		{ID: "u2", Kind: "actor", Reason: "not_stated", Field: "name", SubjectRefs: []string{"a1"}, Message: "原文未写具体番号。", SourceRefs: []string{source.Ref}},
	}
	issues, err := decodeModelIssues(rows, issueProtocolGraph(), []EvidenceSource{source})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != len(rows) || issues[0].Field != "nodes" || issues[1].Field != "identity" {
		t.Fatalf("equivalent fields not normalized: %#v", issues)
	}
	for i, issue := range issues {
		if issue.ID != rows[i].ID || issue.Kind != rows[i].Kind || issue.Reason != rows[i].Reason || issue.Message != rows[i].Message || !reflect.DeepEqual(issue.SubjectRefs, rows[i].SubjectRefs) || !reflect.DeepEqual(issue.EvidenceQuotes, []string{source.Text}) {
			t.Fatalf("issue meaning or evidence changed: %#v", issue)
		}
	}
	if advisoryIssue(issues[0]) {
		t.Fatal("node ambiguity was downgraded to an advisory")
	}
}

func TestDecodeModelIssuesStillRejectsNotStatedNodeAmbiguity(t *testing.T) {
	rows := []modelIssue{{ID: "u3", Kind: "route", Reason: "not_stated", Field: "paths.nodes", SubjectRefs: []string{"r1"}, Message: "起点和先后顺序不明确。", SourceRefs: []string{"s1"}}}
	issues, err := decodeModelIssues(rows, issueProtocolGraph(), []EvidenceSource{{Ref: "s1", Text: "向甲地、乙地进发。"}})
	if err == nil || issues != nil || !strings.Contains(err.Error(), "u3") || !strings.Contains(err.Error(), "原文未记载不能替代") {
		t.Fatalf("ambiguous nodes escaped validation: issues=%v err=%v", issues, err)
	}
}
