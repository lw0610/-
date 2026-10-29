package planexec

import (
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
)

func TestV2DeclaredExportCapabilityDoesNotAcquireVisionRequirement(t *testing.T) {
	// The isolated model submitted exactly this export responsibility after
	// receiving an erroneous vision requirement from the earlier inference.
	p, err := plan.Decode([]byte(`{"goal":"导出 PDF","steps":[{"role_hint":"cartographer","capabilities":["print-layout"],"id":"export","instructions":"不做质量审查，直接从版面导出 PDF 文件，文件名用地图标题。","produces":[{"format":"pdf","id":"final_pdf","kind":"file"}],"title":"导出 PDF"}],"deliverables":["final_pdf"]}`))
	if err != nil {
		t.Fatal(err)
	}
	available := map[string]bool{"print-layout": true, "quality-feedback": true, "file-operations": true}
	env := flow.Environment{MainProvider: "text", Roles: []flow.RoleSpec{{ID: "cartographer", Kind: "agent", Enabled: true, ToolPackages: []string{"print-layout", "file-operations"}}, {ID: "text-reviewer", Kind: "agent", Enabled: true, Provider: "text", ToolPackages: []string{"quality-feedback"}}, {ID: "vision-reviewer", Kind: "agent", Enabled: true, Provider: "vision", ToolPackages: []string{"quality-feedback"}}}, Packages: []flow.PackageSpec{{ID: "print-layout", Kind: "category", Enabled: true}, {ID: "quality-feedback", Kind: "category", Enabled: true}, {ID: "file-operations", Kind: "category", Enabled: true}}}
	for _, declared := range [][]string{{"print-layout"}, nil} {
		p.Steps[0].Capabilities = declared
		plan.NormalizeV2(p, available)
		executable, _, err := Build(p, BuildOptions{Environment: env})
		if err != nil {
			t.Fatal(err)
		}
		if step := executable.Steps[0]; step.Role != "cartographer" || !reflect.DeepEqual(step.RequiredPackages, env.Roles[0].ToolPackages) || len(executable.FlowRoles) != 0 {
			t.Fatalf("export became a review responsibility: %+v", step)
		}
		if len(p.Steps[0].Capabilities) != len(declared) {
			t.Fatal("export prose added capabilities", p.Steps[0].Capabilities)
		}
	}
	// A declared review capability or a review output is still subject to the
	// same vision gate, without relying on review-related words in the prose.
	for _, kind := range []string{"review_initial", "review_final"} {
		for _, declared := range [][]string{{"quality-feedback"}, nil} {
			p.Steps[0].Title = "评价地图"
			p.Steps[0].Instructions = "评价图片中的遮挡与配色"
			p.Steps[0].RoleHint = "text-reviewer"
			p.Steps[0].Capabilities = declared
			p.Steps[0].Produces = []plan.Product{{ID: "review", Kind: kind}}
			p.Deliverables = nil
			plan.NormalizeV2(p, available)
			env.VisionProviders = nil
			if _, _, err := Build(p, BuildOptions{Environment: env}); err == nil || !strings.Contains(err.Error(), "读图") {
				t.Fatalf("review bypassed its vision requirement: %v", err)
			}
			env.VisionProviders = []string{"vision"}
			if _, _, err := Build(p, BuildOptions{Environment: env}); err == nil || !strings.Contains(err.Error(), "读图") {
				t.Fatalf("review silently replaced the selected role's model: %v", err)
			}
			p.Steps[0].RoleHint = "vision-reviewer"
			if executable, _, err := Build(p, BuildOptions{Environment: env}); err != nil || executable.Steps[0].Role != "vision-reviewer" || len(executable.FlowRoles) != 0 {
				t.Fatalf("review did not use the selected scheme role: %+v %v", executable, err)
			}
		}
	}
}
