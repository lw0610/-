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
	if err := AssignPlanRole(p, s, []string{"quality-feedback"}, "text-reviewer", env); err == nil || !strings.Contains(err.Error(), "读图") {
		t.Fatal("unrelated vision provider replaced the selected role's model", err)
	}
	env.Roles = append(env.Roles, RoleSpec{ID: "vision-reviewer", Enabled: true, Kind: "agent", Provider: "vision", ToolPackages: []string{"quality-feedback"}})
	if err := AssignPlanRole(p, s, []string{"quality-feedback"}, "vision-reviewer", env); err != nil || s.Role != "vision-reviewer" || len(p.FlowRoles) != 0 {
		t.Fatal(s, err)
	}
	if err := AssignPlanRole(p, &StepPlan{ID: "ambiguous", Title: "审图"}, []string{"quality-feedback"}, "", env); err == nil || !strings.Contains(err.Error(), "多个") {
		t.Fatal("ambiguous role selection accepted", err)
	}
}
func TestPlanRestrictedPackagesAreNotImplicitlyGranted(t *testing.T) {
	env := Environment{Packages: []PackageSpec{{ID: "browser", Kind: "category", Enabled: true, Hidden: true, Tools: []string{"browse"}}}}
	if err := AssignPlanRole(&FlowPlan{}, &StepPlan{ID: "browse"}, []string{"browser"}, "", env); err == nil {
		t.Fatal("restricted package granted")
	}
}
