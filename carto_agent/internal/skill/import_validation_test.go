package skill

import "testing"

func TestToolPackageImportRequiresExplicitCategoryToolList(t *testing.T) {
	for _, tc := range []struct {
		name, kind            string
		tools, refs, requires []string
		allowed               bool
	}{
		{name: "tools", kind: "category", tools: []string{"open_project"}, allowed: true},
		{name: "tool_refs", kind: "category", refs: []string{"open_project"}, allowed: true},
		{name: "empty_category", kind: "category"},
		{name: "whitespace_only", kind: "category", tools: []string{" "}},
		{name: "requires_only", kind: "category", requires: []string{"open_project"}},
		{name: "knowledge", kind: "knowledge", tools: []string{"open_project"}},
		{name: "workflow", kind: "workflow", refs: []string{"open_project"}},
		{name: "markdown_without_metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := SkillPackage{Kind: tc.kind, FrontMatter: SkillFrontMatter{Kind: tc.kind, Tools: tc.tools, ToolRefs: tc.refs, RequiresTools: tc.requires}}
			if err := ValidateToolPackageImport(pkg); (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v error=%v", tc.allowed, err)
			}
		})
	}
}
