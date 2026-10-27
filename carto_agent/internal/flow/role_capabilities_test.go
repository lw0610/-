package flow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestV4SuggestedToolsRespectPerStepRoleOverlays(t *testing.T) {
	raw := "---\n名称: 能力投影\n---\n## 参与角色\n### 数据工程师\n- 工具包：data\n## 步骤\n### 处理\n- 负责：数据工程师\n- 做法：处理数据\n- 完成标准：完成\n"
	for _, synthetic := range []bool{false, true} {
		t.Run(map[bool]string{false: "augmented", true: "synthetic"}[synthetic], func(t *testing.T) {
			env := Environment{Scheme: "fixture", MainProvider: "fixture", Packages: []PackageSpec{{ID: "data", Kind: "category", Enabled: true, Tools: []string{"prepare_data"}}}, Capabilities: []CapabilitySpec{{Name: "prepare_data", DisplayName: "处理数据", PackageIDs: []string{"data"}}}}
			if !synthetic {
				env.Roles = []RoleSpec{{ID: "worker", DisplayName: "数据工程师", Kind: "agent", Enabled: true, ToolPackages: []string{}, Tools: []string{}}}
			}
			compiler, store, _ := compilerFixture(t, "", &env)
			item, err := store.Create(raw, "overlay-diagnostics")
			if err != nil {
				t.Fatal(err)
			}
			plan, err := compiler.PlanReadOnly(context.Background(), item.Meta.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			plan.Steps[0].SuggestedTools = []string{"prepare_data"}
			completeDiagnostics(Parse(raw), plan, env)
			for _, d := range plan.Diagnostics {
				if d.Code == "FLOW_CAPABILITY_MISSING_IN_ROLE" {
					t.Fatalf("overlay ignored: %+v", d)
				}
			}
			source, err := compiler.source(context.Background(), item.Meta.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			units := compiler.enrichmentUnits(source)
			if len(units) != 2 || !strings.Contains(units[1].Request.Prompt, `"tools":["prepare_data"]`) {
				t.Fatalf("enrichment ignored role overlay: %+v", units)
			}
			if len(env.Roles) > 0 && len(env.Roles[0].Tools) != 0 {
				t.Fatal("mutated persistent role")
			}
			before, _ := json.Marshal(plan.FlowRoles)
			other := plan.Steps[0]
			other.GrantedPackages = nil
			if !synthetic && len(effectiveStepRole(plan, other, env).Tools) > 0 {
				t.Fatal("per-step grant leaked")
			}
			after, _ := json.Marshal(plan.FlowRoles)
			if string(before) != string(after) {
				t.Fatal("mutated flow role")
			}
		})
	}
}
