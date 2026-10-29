package flowrun

import (
	"context"
	"github.com/autocarto/carto_agent/internal/flow"
	"testing"
)

func TestPlanFlowRoleOverlay(t *testing.T) {
	p := testPlan(1)
	p.FlowRoles = []flow.FlowRoleSpec{{ID: "flow_role_test", DisplayName: "临时角色", Kind: "agent", ToolPackages: []string{"data"}}}
	p.Steps[0].Role = "flow_role_test"
	p.Steps[0].GrantedPackages = []string{"extra"}
	c, request, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Overlay == nil || r.Overlay.FlowRole == nil || r.Role.ID != "flow_role_test" || r.Overlay.ExtraPackages[0] != "extra" {
			t.Fatalf("%+v", r)
		}
		return completed(r.Step), nil
	}, nil)
	run, err := c.Start(context.Background(), request)
	if err != nil || run.Status != "completed" {
		t.Fatal(run, err)
	}
}
