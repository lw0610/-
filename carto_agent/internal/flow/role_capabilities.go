package flow

// effectiveStepRole projects an already resolved overlay for diagnostics and
// enrichment only. It never widens another step or the stored scheme.
func effectiveStepRole(plan *FlowPlan, step StepPlan, env Environment) *RoleSpec {
	role := planRole(plan, step.Role)
	if role == nil {
		role = roleByID(step.Role, env.Roles)
	}
	if role == nil {
		return nil
	}
	role.Tools = append([]string{}, role.Tools...)
	packages := append([]string{}, step.GrantedPackages...)
	if role.FlowOnly {
		packages = append(packages, role.ToolPackages...)
	}
	if step.Unresolved != "" {
		packages = nil
	}
	for _, id := range uniqueStrings(packages) {
		if !FlowPackageGrantable(id, env.Packages) {
			continue
		}
		for _, p := range env.Packages {
			if p.ID == id {
				role.Tools = append(role.Tools, p.Tools...)
			}
		}
		// Some catalog projections supply ownership on capabilities rather than packages.
		for _, capability := range env.Capabilities {
			for _, owner := range capability.PackageIDs {
				if owner == id {
					role.Tools = append(role.Tools, capability.Name)
					break
				}
			}
		}
	}
	role.Tools = uniqueStrings(role.Tools)
	return role
}
