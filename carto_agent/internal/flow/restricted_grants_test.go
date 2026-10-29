package flow

import "testing"

func TestRestrictedPackagesNeverEnterPlanRoleGrants(t *testing.T) {
	for _, blocked := range []PackageSpec{{ID: "pyqgis-scripting", Enabled: true, Kind: "category"}, {ID: "creator-tools", Enabled: true, Kind: "category"}, {ID: "hidden-fixture", Enabled: true, Kind: "category", Hidden: true}} {
		t.Run(blocked.ID, func(t *testing.T) {
			env := Environment{MainProvider: "fixture", Packages: []PackageSpec{{ID: "data", Enabled: true, Kind: "category"}, blocked}, Roles: []RoleSpec{{ID: "worker", Kind: "agent", Enabled: true, ToolPackages: []string{"data"}}}}
			plan := &FlowPlan{Roles: env.Roles}
			step := StepPlan{ID: "s1"}
			if err := AssignPlanRole(plan, &step, []string{blocked.ID}, "worker", env); err != nil {
				t.Fatal(err)
			}
			if step.Role != "worker" || len(step.RequiredPackages) != 1 || step.RequiredPackages[0] != "data" || len(step.GrantedPackages) > 0 || len(plan.FlowRoles) > 0 || len(env.Roles[0].ToolPackages) != 1 {
				t.Fatal("capability declaration changed scheme authorization")
			}
		})
	}
}
func TestPlanCapabilityNamesDetectAmbiguity(t *testing.T) {
	env := Environment{Packages: []PackageSpec{{ID: "gee", DisplayName: "遥感影像（GEE）", Enabled: true, Kind: "category"}}}
	ids, err := ResolvePlanCapabilities([]string{"遥感影像"}, env)
	if err != nil || len(ids) != 1 || ids[0] != "gee" {
		t.Fatal(ids, err)
	}
	env.Packages = append(env.Packages, PackageSpec{ID: "gee2", DisplayName: "遥感影像（其他）"})
	if _, err = ResolvePlanCapabilities([]string{"遥感影像"}, env); err == nil {
		t.Fatal("ambiguity accepted")
	}
}
