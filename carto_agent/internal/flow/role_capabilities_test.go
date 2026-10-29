package flow

import (
	"encoding/json"
	"testing"
)

func TestPlanRoleOverlayIsPerStep(t *testing.T) {
	env := Environment{MainProvider: "fixture", Packages: []PackageSpec{{ID: "data", Enabled: true, Kind: "category", Tools: []string{"prepare_data"}}}, Roles: []RoleSpec{{ID: "worker", Kind: "agent", Enabled: true}}, Capabilities: []CapabilitySpec{{Name: "prepare_data", PackageIDs: []string{"data"}}}}
	plan := &FlowPlan{}
	step := StepPlan{ID: "s1"}
	if err := AssignPlanRole(plan, &step, []string{"data"}, "worker", env); err != nil {
		t.Fatal(err)
	}
	if len(effectiveStepRole(plan, step, env).Tools) != 1 {
		t.Fatal("grant missing")
	}
	before, _ := json.Marshal(plan)
	step.GrantedPackages = nil
	if len(effectiveStepRole(plan, step, env).Tools) != 0 {
		t.Fatal("grant leaked to another step")
	}
	after, _ := json.Marshal(plan)
	if string(before) != string(after) || len(env.Roles[0].Tools) != 0 {
		t.Fatal("persistent role mutated")
	}
}
