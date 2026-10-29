package flowrun

import (
	"context"
	"reflect"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
)

func TestPlanDelegatesConfiguredRoleWithoutLegacyGrants(t *testing.T) {
	p := testPlan(1)
	p.Roles[0].Provider = "configured-model"
	p.Roles[0].ToolPackages = []string{"data"}
	p.FlowRoles = []flow.FlowRoleSpec{{ID: "worker", DisplayName: "旧临时角色", Kind: "agent", ToolPackages: []string{"extra"}}, {ID: "flow_role_test", Kind: "agent", ToolPackages: []string{"extra"}}}
	p.Steps[0].GrantedPackages = []string{"extra"}
	calls := 0
	c, request, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		calls++
		if r.Step.Role != "worker" || !reflect.DeepEqual(r.Role, p.Roles[0]) {
			t.Fatalf("legacy grants replaced the configured delegate: %+v", r.Role)
		}
		return completed(r.Step), nil
	}, nil)
	run, err := c.Start(context.Background(), request)
	if err != nil || run.Status != "completed" || calls != 1 {
		t.Fatal(run, err)
	}
	if _, err := SelectRole(flow.StepPlan{Kind: "agent", Role: "flow_role_test"}, p.Roles); err == nil {
		t.Fatal("legacy temporary role was available outside the configured scheme")
	}
}
