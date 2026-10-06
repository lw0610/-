package knowledge

import (
	"strings"
	"testing"
)

func issueProtocolGraph() Graph {
	return Graph{
		"actors":           []any{map[string]any{"actor_id": "a1", "name": "红军"}},
		"spatial_entities": []any{map[string]any{"entity_id": "s1", "name": "甲地"}},
		"events":           []any{map[string]any{"event_id": "e1", "name": "到达"}},
		"routes": []any{map[string]any{"route_id": "r1", "paths": []any{
			map[string]any{"path_id": "p1", "actor_refs": []any{"a1"}},
		}}},
		"evidence": []any{map[string]any{"evidence_id": "ev1"}},
	}
}

func TestCanonicalModelIssueFieldAcceptsOnlyExplicitAliases(t *testing.T) {
	for _, tc := range []struct{ kind, input, want, subject string }{
		{"route", "paths.nodes", "nodes", "p1"},
		{"route", "routes.paths.nodes", "nodes", "r1"},
		{"route", "paths.attach_ref", "attach_ref", "p1"},
		{"route", "routes.paths.attach_ref", "attach_ref", "r1"},
		{"route", "paths.continuation", "continuation", "p1"},
		{"route", "routes.paths.continuation", "continuation", "r1"},
		{"actor", "paths.actor_refs", "actor_refs", "p1"},
		{"actor", "routes.paths.actor_refs", "actor_refs", "r1"},
		{"actor", "routes.actor_refs", "actor_refs", "r1"},
		{"actor", "events.actor_refs", "actor_refs", "e1"},
		{"actor", "name", "identity", "a1"},
		{"actor", "actors.name", "identity", "a1"},
		{"route", " nodes ", "nodes", "p1"},
	} {
		t.Run(tc.kind+"/"+tc.input, func(t *testing.T) {
			got := canonicalModelIssueField(tc.kind, tc.input)
			if got != tc.want {
				t.Fatalf("field = %q, want %q", got, tc.want)
			}
			issue := Issue{ID: "i1", Kind: tc.kind, Reason: "context", Field: got, SubjectRefs: []string{tc.subject}}
			if err := validateIssueSubjects(issueProtocolGraph(), nil, issue); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, tc := range []struct{ kind, field string }{
		{"actor", "paths.nodes"}, {"route", "foo.nodes"}, {"route", "graph.routes.paths.nodes"},
		{"route", "paths[0].nodes"}, {"route", "nodes.ref_id"}, {"place", "name"},
		{"place", "paths.attach_ref"}, {"route", "paths.actor_refs"}, {"actor", "foos.actor_refs"},
	} {
		if got := canonicalModelIssueField(tc.kind, tc.field); got != tc.field {
			t.Fatalf("unrecognized field changed: %s/%s -> %s", tc.kind, tc.field, got)
		}
		issue := Issue{ID: "i1", Kind: tc.kind, Reason: "context", Field: tc.field, SubjectRefs: []string{"e1"}}
		if err := validateIssueSubjects(issueProtocolGraph(), nil, issue); err == nil {
			t.Fatalf("unrecognized field accepted: %s/%s", tc.kind, tc.field)
		}
	}
}

func TestModelIssueAliasesDoNotDowngradeAmbiguityOrChangeBindings(t *testing.T) {
	for _, issue := range []Issue{
		{ID: "i1", Kind: "route", Reason: "not_stated", Field: "paths.nodes", SubjectRefs: []string{"p1"}},
		{ID: "i1", Kind: "route", Reason: "not_stated", Field: "paths.attach_ref", SubjectRefs: []string{"p1"}},
		{ID: "i1", Kind: "place", Reason: "not_stated", Field: "identity", SubjectRefs: []string{"s1"}},
		{ID: "i1", Kind: "actor", Reason: "context", Field: "paths.actor_refs", SubjectRefs: []string{"s1"}},
		{ID: "i1", Kind: "route", Reason: "context", Field: "paths.nodes", SubjectRefs: []string{"unknown"}},
		{ID: "i1", Kind: "route", Reason: "context", Field: "paths.nodes", SubjectRefs: []string{"ev1"}},
		{ID: "i1", Kind: "route", Reason: "context", Field: "paths.nodes"},
	} {
		issue.Field = canonicalModelIssueField(issue.Kind, issue.Field)
		if err := validateIssueSubjects(issueProtocolGraph(), nil, issue); err == nil {
			t.Fatalf("invalid issue accepted after normalization: %#v", issue)
		}
	}
	// Canonicalization belongs only at the model boundary, not in the validator.
	issue := Issue{ID: "i1", Kind: "route", Reason: "context", Field: "paths.nodes", SubjectRefs: []string{"p1"}}
	if err := validateIssueSubjects(issueProtocolGraph(), nil, issue); err == nil {
		t.Fatal("validator silently changed a stored issue field")
	}
}

func TestIssueProtocolPromptAndValidationShareVocabulary(t *testing.T) {
	graph := issueProtocolGraph()
	refs := map[string]string{"actors": "a1", "spatial_entities": "s1", "events": "e1", "routes": "r1", "paths": "p1"}
	for _, protocol := range issueProtocols {
		line := "kind=" + protocol.kind + "：field 只能是 " + strings.Join(protocol.fields, "、") + "；subject_refs 对象只能来自 " + strings.Join(protocol.subjectKinds, "、")
		for _, prompt := range []string{fragmentInstructions, semanticReviewInstructions} {
			if !strings.Contains(prompt, line) {
				t.Fatalf("%s vocabulary is missing from a model stage", protocol.kind)
			}
		}
		for _, field := range allowedIssueFields(protocol.kind) {
			for _, kind := range protocol.subjectKinds {
				issue := Issue{ID: "i1", Kind: protocol.kind, Reason: "context", Field: field, SubjectRefs: []string{refs[kind]}}
				if err := validateIssueSubjects(graph, nil, issue); err != nil {
					t.Fatalf("documented issue rejected: %#v: %v", issue, err)
				}
			}
		}
		issue := Issue{ID: "i1", Kind: protocol.kind, Reason: "context", Field: "unknown_field", SubjectRefs: []string{"e1"}}
		err := validateIssueSubjects(graph, nil, issue)
		if err == nil || !strings.Contains(err.Error(), strings.Join(protocol.fields, "、")) {
			t.Fatalf("validation feedback omits accepted vocabulary: %v", err)
		}
	}
}
