package flow

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPlanRoleKeepsConfiguredPackages(t *testing.T) {
	env := Environment{MainProvider: "fixture", Packages: []PackageSpec{{ID: "data", Enabled: true, Kind: "category", Tools: []string{"prepare_data"}}, {ID: "extra", Enabled: true, Kind: "category"}}, Roles: []RoleSpec{{ID: "worker", Kind: "agent", Enabled: true, ToolPackages: []string{"data", "shared"}, Tools: []string{"prepare_data"}}}}
	plan := &FlowPlan{Roles: env.Roles}
	before, _ := json.Marshal(env)
	step := StepPlan{ID: "s1"}
	if err := AssignPlanRole(plan, &step, []string{"extra"}, "worker", env); err != nil {
		t.Fatal(err)
	}
	if step.Role != "worker" || step.RoleSource != "scheme" || !reflect.DeepEqual(step.RequiredPackages, []string{"data", "shared"}) || len(step.GrantedPackages) != 0 || len(plan.FlowRoles) != 0 {
		t.Fatalf("selected scheme role was replaced or granted extra packages: %+v", step)
	}
	after, _ := json.Marshal(env)
	if string(before) != string(after) {
		t.Fatal("scheme role mutated")
	}
}
