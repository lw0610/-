package flow

import "testing"

func TestV4RestrictedPackagesNeverEnterNewRoleGrants(t *testing.T) {
	for _, blocked := range []PackageSpec{
		{ID: "pyqgis-scripting", Enabled: true, Kind: "category"},
		{ID: "creator-tools", Enabled: true, Kind: "category"},
		{ID: "hidden-fixture", Enabled: true, Kind: "category", Hidden: true},
	} {
		for _, location := range []string{"slot", "step", "optional"} {
			t.Run(blocked.ID+"/"+location, func(t *testing.T) {
				env := Environment{MainProvider: "fixture", Packages: []PackageSpec{{ID: "data", Enabled: true, Kind: "category"}, blocked}, Roles: []RoleSpec{{ID: "worker", DisplayName: "工程师", Kind: "agent", Enabled: true, ToolPackages: []string{"data"}}}}
				doc := &FlowDoc{RoleSlots: []RoleSlotDoc{{Name: "工程师", Packages: []string{"data"}}}}
				step := StepDoc{ID: "s1", Role: "工程师"}
				switch location {
				case "slot":
					doc.RoleSlots[0].Packages = []string{blocked.ID}
				case "step":
					step.Capabilities = []string{blocked.ID}
				case "optional":
					step.OptionalCapabilities = []string{blocked.ID}
				}
				plan := &FlowPlan{FlowID: "guard", Roles: env.Roles}
				resolveRoleSlots(plan, doc, env)
				compiled := StepPlan{ID: "s1"}
				resolveStepRole(plan, &compiled, step, doc, env)
				for _, grant := range compiled.GrantedPackages {
					if grant == blocked.ID {
						t.Fatal("restricted package entered step grants")
					}
				}
				for _, role := range plan.FlowRoles {
					for _, grant := range role.ToolPackages {
						if grant == blocked.ID {
							t.Fatal("restricted package entered synthetic role")
						}
					}
				}
				if location != "optional" && compiled.Unresolved != "FLOW_ROLE_PACKAGE_RESTRICTED" {
					t.Fatalf("required restricted capability did not block: %+v", compiled)
				}
				if location == "optional" && compiled.Unresolved != "" {
					t.Fatalf("optional capability blocked safe work: %+v", compiled)
				}
				if len(env.Roles[0].ToolPackages) != 1 {
					t.Fatal("stored role mutated")
				}
			})
		}
	}
}
