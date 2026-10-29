package flow

import (
	"strings"
	"testing"
)

func TestPlanReviewRequiresVisionAndUsesConfiguredProvider(t *testing.T) {
	env := Environment{MainProvider: "text", Packages: []PackageSpec{{ID: "quality-feedback", DisplayName: "质量审查", Kind: "category", Enabled: true, Tools: []string{"review"}}}, Roles: []RoleSpec{{ID: "text-reviewer", Enabled: true, Kind: "agent", Provider: "text", ToolPackages: []string{"quality-feedback"}}}}
	p := &FlowPlan{}
	s := &StepPlan{ID: "review", Title: "审图"}
	if err := AssignPlanRole(p, s, []string{"quality-feedback"}, "text-reviewer", env); err == nil || !strings.Contains(err.Error(), "读图") {
		t.Fatal("non-vision review accepted", err)
	}
	env.VisionProviders = []string{"vision"}
	if err := AssignPlanRole(p, s, []string{"quality-feedback"}, "text-reviewer", env); err != nil {
		t.Fatal(err)
	}
	if s.Role == "text-reviewer" || len(p.FlowRoles) != 1 || p.FlowRoles[0].Provider != "vision" {
		t.Fatal(s, p.FlowRoles)
	}
	env.Roles = append(env.Roles, RoleSpec{ID: "vision-reviewer", Enabled: true, Kind: "agent", Provider: "vision", ToolPackages: []string{"quality-feedback"}})
	if err := AssignPlanRole(p, s, []string{"quality-feedback"}, "", env); err != nil || s.Role != "vision-reviewer" {
		t.Fatal(s, err)
	}
}
func TestPlanRestrictedPackagesAreNotImplicitlyGranted(t *testing.T) {
	env := Environment{Packages: []PackageSpec{{ID: "browser", Kind: "category", Enabled: true, Hidden: true, Tools: []string{"browse"}}}}
	if err := AssignPlanRole(&FlowPlan{}, &StepPlan{ID: "browse"}, []string{"browser"}, "", env); err == nil {
		t.Fatal("restricted package granted")
	}
}
