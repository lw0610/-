package flow

import (
	"fmt"
	"sort"
)

// ResolvePlanCapabilities uses the same package catalog as runtime role overlays.
func ResolvePlanCapabilities(names []string, env Environment) ([]string, error) {
	ids, reason := resolvePackages(names, env)
	if reason != "" {
		return nil, fmt.Errorf("%s", reason)
	}
	return ids, nil
}

// AssignPlanRole selects by actual capabilities; a guide never names a role.
func AssignPlanRole(plan *FlowPlan, step *StepPlan, names []string, hint string, env Environment) error {
	for _, name := range names {
		if name == "路线知识抽取" || name == "route_knowledge" {
			if len(names) != 1 {
				return fmt.Errorf("系统专家应单独作为一个步骤")
			}
			role := FlowRoleSpec{ID: "system.route_knowledge", DisplayName: "路线知识抽取", Kind: "executor", Executor: "route_knowledge", Provider: env.MainProvider}
			exists := false
			for _, r := range plan.FlowRoles {
				if r.ID == role.ID {
					exists = true
				}
			}
			if !exists {
				plan.FlowRoles = append(plan.FlowRoles, role)
			}
			step.Kind = "executor"
			step.Executor = "route_knowledge"
			step.Role = role.ID
			step.RoleLabel = role.DisplayName
			return nil
		}
	}
	ids, err := ResolvePlanCapabilities(names, env)
	if err != nil {
		return err
	}
	step.RequiredPackages = ids
	needsVision := false
	for _, id := range ids {
		if id == "quality-feedback" {
			needsVision = true
		}
	}
	isVision := func(provider string) bool {
		if provider == "" {
			provider = env.MainProvider
		}
		for _, p := range env.VisionProviders {
			if provider == p {
				return true
			}
		}
		return false
	}
	provider := env.MainProvider
	if needsVision && !isVision(provider) {
		if len(env.VisionProviders) == 0 {
			return fmt.Errorf("步骤 %s 需要支持读图的模型，请先在模型设置中配置", step.Title)
		}
		provider = env.VisionProviders[0]
	}
	candidates := append([]RoleSpec{}, env.Roles...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if len(candidates[i].ToolPackages) != len(candidates[j].ToolPackages) {
			return len(candidates[i].ToolPackages) < len(candidates[j].ToolPackages)
		}
		return candidates[i].ID < candidates[j].ID
	})
	eligible := func(r RoleSpec) bool {
		return r.Enabled && !r.FlowOnly && (r.Kind == "" || r.Kind == "agent") && (!needsVision || isVision(r.Provider))
	}
	selectRole := func(r RoleSpec, extra []string) {
		step.Kind = "agent"
		step.Role = r.ID
		step.RoleLabel = r.DisplayName
		step.GrantedPackages = extra
		step.RoleSource = "scheme"
	}
	if hint != "" {
		for _, r := range candidates {
			if r.ID == hint && eligible(r) {
				missing := missingPackages(ids, r.ToolPackages)
				if grantReason(missing, env) == "" {
					selectRole(r, missing)
					return nil
				}
			}
		}
	}
	for _, r := range candidates {
		if eligible(r) && len(missingPackages(ids, r.ToolPackages)) == 0 {
			selectRole(r, nil)
			return nil
		}
	}
	if reason := grantReason(ids, env); reason != "" {
		return fmt.Errorf("%s：步骤 %s 无法临时补齐所需能力", reason, step.Title)
	}
	role := FlowRoleSpec{ID: "plan_role_" + step.ID, DisplayName: step.Title, Description: step.Instructions, Kind: "agent", ToolPackages: ids, Provider: provider}
	plan.FlowRoles = append(plan.FlowRoles, role)
	step.Kind = "agent"
	step.Role = role.ID
	step.RoleLabel = role.DisplayName
	step.RoleSource = "plan_role"
	return nil
}
