package flow

import "testing"

func TestV4RoleResolutionPolicies(t *testing.T) {
	doc := Parse("---\n名称: test\n---\n## 参与角色\n### 工程师\n- 工具包：数据\n## 步骤\n### 加载\n- 负责：工程师\n- 做法：加载数据\n- 完成标准：完成\n")
	base := Environment{MainProvider: "main", Packages: []PackageSpec{{ID: "data", DisplayName: "数据", Kind: "category", Enabled: true}}}
	cases := []struct {
		name, policy   string
		roles          []RoleSpec
		source, reason string
		grants         int
	}{
		{"synthetic", "auto", nil, "flow_role", "", 0},
		{"augment", "auto", []RoleSpec{{ID: "worker", DisplayName: "工程师", Kind: "agent", Enabled: true}}, "scheme_name", "", 1},
		{"strict", "strict", []RoleSpec{{ID: "worker", DisplayName: "工程师", Kind: "agent", Enabled: true}}, "scheme_name", "FLOW_CAPABILITY_MISSING_IN_ROLE", 0},
		{"flow role", "flow_role", []RoleSpec{{ID: "worker", DisplayName: "工程师", Kind: "agent", Enabled: true}}, "flow_role", "", 0},
		{"capability", "auto", []RoleSpec{{ID: "wide", Kind: "agent", Enabled: true, ToolPackages: []string{"data", "extra"}}, {ID: "narrow", Kind: "agent", Enabled: true, ToolPackages: []string{"data"}}}, "scheme_capability", "", 0},
		{"disabled", "auto", []RoleSpec{{ID: "worker", DisplayName: "工程师", Enabled: false}}, "flow_role", "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := base
			env.RoleFill = c.policy
			env.Roles = c.roles
			p := &FlowPlan{FlowID: "test", Roles: env.Roles}
			resolveRoleSlots(p, doc, env)
			r := p.RoleSlots[0].Resolution
			if r.Source != c.source || r.Reason != c.reason || len(r.Granted) != c.grants {
				t.Fatalf("%+v", r)
			}
			if c.name == "capability" && r.RoleID != "narrow" {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestV4VisionRestrictedAndMatching(t *testing.T) {
	env := Environment{MainProvider: "text", VisionProviders: []string{"vision"}, Packages: []PackageSpec{{ID: "gee", DisplayName: "遥感影像（GEE）", Enabled: true, Kind: "category"}, {ID: "pyqgis-scripting", Enabled: true, Kind: "category"}}}
	ids, why := resolvePackages([]string{"遥感影像"}, env)
	if why != "" || ids[0] != "gee" {
		t.Fatal(ids, why)
	}
	p := &FlowPlan{FlowID: "test"}
	r, why := slotSynthetic(p, RoleSlotDoc{Name: "审查", NeedsVision: true}, []string{"gee"}, env)
	if why != "" || r == nil || p.FlowRoles[0].Provider != "vision" {
		t.Fatal(p.FlowRoles, why)
	}
	if _, why = slotSynthetic(p, RoleSlotDoc{Name: "脚本"}, []string{"pyqgis-scripting"}, env); why != "FLOW_ROLE_PACKAGE_RESTRICTED" {
		t.Fatal(why)
	}
	env.VisionProviders = nil
	if _, why = slotSynthetic(p, RoleSlotDoc{Name: "审查2", NeedsVision: true}, nil, env); why != "FLOW_ROLE_VISION_UNAVAILABLE" {
		t.Fatal(why)
	}
	env.Packages = append(env.Packages, PackageSpec{ID: "gee2", DisplayName: "遥感影像（其他）"})
	if _, why = resolvePackages([]string{"遥感影像"}, env); why != "FLOW_ROLE_PACKAGE_AMBIGUOUS" {
		t.Fatal(why)
	}
}
func TestV4Reachability(t *testing.T) {
	p := &FlowPlan{Steps: []StepPlan{{ID: "a"}, {ID: "b", Condition: &ConditionDoc{Rule: &ConditionRule{Op: "provided", Input: "数据"}}}, {ID: "c", Condition: &ConditionDoc{Rule: &ConditionRule{Op: "and", Args: []ConditionRule{{Op: "not_provided", Input: "数据"}, {Op: "step_status", Step: "a", Status: "completed"}}}}}}}
	r := StepReach(p, nil, nil)
	if r["a"] != ReachMust || r["b"] != ReachMaybe {
		t.Fatal(r)
	}
	r = StepReach(p, map[string]any{"数据": "data.shp"}, nil)
	if r["b"] != ReachMust || r["c"] != ReachNever {
		t.Fatal(r)
	}
	r = StepReach(p, map[string]any{}, map[string]string{"数据": "auto"})
	if r["b"] != ReachMust {
		t.Fatal(r)
	}
}
