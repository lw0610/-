package flowrun

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/planexec"
)

func TestV2LayoutProductNormalizesThroughExecutionVerification(t *testing.T) {
	for _, kind := range []string{"file", ""} {
		t.Run("kind="+kind, func(t *testing.T) {
			// This declaration was emitted by the real model when the public
			// schema omitted layout from the output-kind enumeration.
			p, err := plan.Decode([]byte(fmt.Sprintf(`{"goal":"制作版面并导出","steps":[{"id":"compose","role_hint":"cartographer","title":"版面与导出","instructions":"在工程中创建版面并导出 PDF 和 QPT","produces":[{"id":"layout_done","kind":%q,"format":"layout"},{"id":"map_pdf","kind":"file","format":"pdf"},{"id":"map_qpt","kind":"file","format":"qpt"}]}],"deliverables":["layout_done","map_pdf","map_qpt"]}`, kind)))
			if err != nil {
				t.Fatal(err)
			}
			plan.NormalizeV2(p, map[string]bool{"print-layout": true})
			if diagnostics := plan.ValidateV2(p); len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			env := flow.Environment{Roles: []flow.RoleSpec{{ID: "cartographer", Enabled: true, Kind: "agent", ToolPackages: []string{"print-layout"}}}, Packages: []flow.PackageSpec{{ID: "print-layout", Enabled: true, Kind: "category", Tools: []string{"layout_info"}}}}
			executable, _, err := planexec.Build(p, planexec.BuildOptions{Environment: env})
			if err != nil {
				t.Fatal(err)
			}
			step := executable.Steps[0]
			if output := step.ExpectedOutputs[0]; output.Type != "layout" || output.Format != "" || p.Deliverables[0].Type != "layout" {
				t.Fatalf("layout declaration did not reach the executor and deliverable: %+v / %+v", output, p.Deliverables)
			}
			root := t.TempDir()
			for i, ext := range []string{"pdf", "qpt"} {
				if output := step.ExpectedOutputs[i+1]; output.Type != "file" || output.Format != ext {
					t.Fatalf("ordinary file output changed: %+v", output)
				}
				if err := os.WriteFile(filepath.Join(root, "map."+ext), []byte("exported file"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			tools := toolFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
				calls++
				if name != "layout_info" || args["name"] != "成图版面" {
					t.Fatalf("unexpected readback: %s %v", name, args)
				}
				return map[string]any{"success": true, "data": map[string]any{"name": "成图版面"}}, nil
			})
			receipt := Receipt{Outputs: map[string]any{"layout_done": "成图版面", "map_pdf": "map.pdf", "map_qpt": "map.qpt"}}
			if verification := VerifyOutputs(context.Background(), step, receipt, root, tools); !verification.OK || calls != 1 {
				t.Fatalf("layout name was treated as a file or not read back: %+v, calls=%d", verification, calls)
			}
			if err := os.Remove(filepath.Join(root, "map.qpt")); err != nil {
				t.Fatal(err)
			}
			if verification := VerifyOutputs(context.Background(), step, receipt, root, tools); verification.OK {
				t.Fatal("ordinary QPT file existence validation was weakened")
			}
		})
	}
}
