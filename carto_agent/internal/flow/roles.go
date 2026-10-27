package flow

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var restrictedFlowPackages = map[string]bool{"pyqgis-scripting": true, "creator-tools": true}
var packageParentheses = regexp.MustCompile(`（[^）]*）|\([^)]*\)`)

// FlowPackageGrantable applies equally to new and restored runtime overlays.
func FlowPackageGrantable(id string, packages []PackageSpec) bool {
	if restrictedFlowPackages[id] {
		return false
	}
	for _, p := range packages {
		if p.ID == id {
			return p.Enabled && !p.Hidden && (p.Kind == "category" || p.Kind == "")
		}
	}
	return false
}
func resolvePackages(names []string, env Environment) ([]string, string) {
	ids := []string{}
	for _, name := range names {
		matches := []string{}
		for stage := 0; stage < 3; stage++ {
			for _, p := range env.Packages {
				match := p.ID == name
				if stage == 1 {
					match = p.DisplayName == name
				}
				if stage == 2 {
					match = strings.TrimSpace(packageParentheses.ReplaceAllString(p.DisplayName, "")) == name
				}
				if match {
					matches = append(matches, p.ID)
				}
			}
			if len(matches) > 0 {
				break
			}
		}
		if len(matches) > 1 {
			return ids, "FLOW_ROLE_PACKAGE_AMBIGUOUS"
		}
		if len(matches) == 0 {
			return ids, "FLOW_ROLE_PACKAGE_UNKNOWN"
		}
		ids = append(ids, matches[0])
	}
	return uniqueStrings(ids), ""
}
func missingPackages(required, owned []string) []string {
	have := map[string]bool{}
	for _, id := range owned {
		have[id] = true
	}
	result := []string{}
	for _, id := range required {
		if !have[id] {
			result = append(result, id)
		}
	}
	return uniqueStrings(result)
}
func grantReason(ids []string, env Environment) string {
	for _, id := range ids {
		exists := false
		for _, p := range env.Packages {
			if p.ID == id {
				exists = true
				break
			}
		}
		if !exists {
			return "FLOW_ROLE_PACKAGE_UNKNOWN"
		}
		if !FlowPackageGrantable(id, env.Packages) {
			return "FLOW_ROLE_PACKAGE_RESTRICTED"
		}
	}
	return ""
}
func policy(env Environment) string {
	if env.RoleFill == "" {
		return "auto"
	}
	return env.RoleFill
}
func roleByID(id string, roles []RoleSpec) *RoleSpec {
	for _, r := range roles {
		if r.ID == id {
			copy := r
			return &copy
		}
	}
	return nil
}
func planRole(plan *FlowPlan, id string) *RoleSpec {
	if r := roleByID(id, plan.Roles); r != nil {
		return r
	}
	for _, r := range plan.FlowRoles {
		if r.ID == id {
			copy := r.RoleSpec()
			return &copy
		}
	}
	return nil
}
func slotExecutor(raw string) string {
	for _, s := range ExecutorSpecs() {
		if raw == s.Type || raw == s.DisplayName {
			return s.Type
		}
	}
	return raw
}
func slotSynthetic(plan *FlowPlan, slot RoleSlotDoc, packages []string, env Environment) (*RoleSpec, string) {
	if policy(env) == "strict" || slot.SchemeOnly {
		return nil, "FLOW_ROLE_NOT_FOUND"
	}
	if reason := grantReason(packages, env); reason != "" {
		return nil, reason
	}
	provider := env.MainProvider
	if slot.NeedsVision {
		found := false
		for _, p := range env.VisionProviders {
			if p == provider {
				found = true
			}
		}
		if !found {
			if len(env.VisionProviders) == 0 {
				return nil, "FLOW_ROLE_VISION_UNAVAILABLE"
			}
			provider = env.VisionProviders[0]
		}
	}
	digest := sha256.Sum256([]byte(plan.FlowID + "\x00" + slot.Name))
	id := "flow_role_" + hex.EncodeToString(digest[:])[:12]
	for _, r := range plan.FlowRoles {
		if r.ID == id {
			copy := r.RoleSpec()
			return &copy, ""
		}
	}
	kind := "agent"
	executor := ""
	if slot.Executor != "" {
		kind = "executor"
		executor = slotExecutor(slot.Executor)
	}
	r := FlowRoleSpec{ID: id, Slot: slot.Name, DisplayName: slot.Name, Description: slot.Description, Kind: kind, Executor: executor, ToolPackages: append([]string{}, packages...), Provider: provider}
	plan.FlowRoles = append(plan.FlowRoles, r)
	copy := r.RoleSpec()
	return &copy, ""
}
func resolveRoleSlots(plan *FlowPlan, doc *FlowDoc, env Environment) {
	for _, slot := range doc.RoleSlots {
		entry := RoleSlotPlan{Name: slot.Name, Description: slot.Description, NeedsVision: slot.NeedsVision, Executor: slotExecutor(slot.Executor), Resolution: RoleResolution{Source: "unresolved", Granted: []string{}}}
		packages, reason := resolvePackages(slot.Packages, env)
		entry.Packages = packages
		if reason != "" {
			if reason == "FLOW_ROLE_PACKAGE_AMBIGUOUS" {
				plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "error", Code: reason, Message: "参与角色工具包名称不唯一：" + slot.Name})
			}
			entry.Resolution.Reason = reason
			plan.RoleSlots = append(plan.RoleSlots, entry)
			continue
		}
		for _, r := range env.Roles {
			if !r.Enabled && (r.ID == slot.Name || r.DisplayName == slot.Name) {
				entry.Resolution.Note = "方案中的同名角色已禁用"
			}
		}
		mapped := env.RoleMapping[plan.FlowID][slot.Name]
		forceFlow := mapped == "flow_role"
		accept := func(role *RoleSpec, source string) bool {
			if role == nil || !role.Enabled {
				return false
			}
			missing := missingPackages(packages, role.ToolPackages)
			if len(missing) > 0 {
				switch policy(env) {
				case "flow_role":
					return false
				case "strict":
					reason = "FLOW_CAPABILITY_MISSING_IN_ROLE"
				default:
					reason = grantReason(missing, env)
					if reason == "" {
						entry.Resolution.Granted = missing
					}
				}
			}
			entry.Resolution.Source = source
			entry.Resolution.RoleID = role.ID
			entry.Resolution.RoleLabel = role.DisplayName
			entry.Resolution.Reason = reason
			return true
		}
		if slot.Executor != "" {
			if mapped != "" && !forceFlow {
				r := roleByID(mapped, env.Roles)
				if r != nil && r.Kind == "executor" && r.Executor == entry.Executor && accept(r, "mapping") {
					plan.RoleSlots = append(plan.RoleSlots, entry)
					continue
				}
				plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "warning", Code: "FLOW_ROLE_MAPPING_INVALID", Message: "角色对应已失效：" + slot.Name})
			}
			var candidate *RoleSpec
			if !forceFlow {
				for _, r := range env.Roles {
					if r.Enabled && r.Kind == "executor" && r.Executor == entry.Executor {
						copy := r
						if candidate == nil || r.ID == slot.Name || r.DisplayName == slot.Name {
							candidate = &copy
						}
					}
				}
			}
			if candidate != nil {
				accept(candidate, "scheme_executor")
			} else if r, why := slotSynthetic(plan, slot, packages, env); r != nil {
				accept(r, "executor_default")
			} else {
				entry.Resolution.Reason = why
				if why == "FLOW_ROLE_NOT_FOUND" {
					entry.Resolution.Reason = "FLOW_EXECUTOR_ROLE_MISSING"
				}
			}
			plan.RoleSlots = append(plan.RoleSlots, entry)
			continue
		}
		matched := false
		if mapped != "" && !forceFlow {
			r := roleByID(mapped, env.Roles)
			if r != nil && (r.Kind == "agent" || r.Kind == "") && r.Enabled {
				matched = accept(r, "mapping")
			} else {
				plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "warning", Code: "FLOW_ROLE_MAPPING_INVALID", Message: "角色对应已失效：" + slot.Name})
			}
		}
		if !matched && !forceFlow {
			for _, r := range env.Roles {
				if r.Enabled && (r.Kind == "agent" || r.Kind == "") && (r.ID == slot.Name || r.DisplayName == slot.Name) {
					copy := r
					if accept(&copy, "scheme_name") {
						matched = true
						break
					}
				}
			}
		}
		if !matched && !forceFlow && len(packages) > 0 {
			var best *RoleSpec
			surplus := int(^uint(0) >> 1)
			for _, r := range env.Roles {
				if r.Enabled && !r.IsMain && (r.Kind == "agent" || r.Kind == "") && len(missingPackages(packages, r.ToolPackages)) == 0 && len(r.ToolPackages)-len(packages) < surplus {
					copy := r
					best = &copy
					surplus = len(r.ToolPackages) - len(packages)
				}
			}
			if best != nil {
				matched = accept(best, "scheme_capability")
			}
		}
		if !matched {
			if r, why := slotSynthetic(plan, slot, packages, env); r != nil {
				accept(r, "flow_role")
			} else {
				entry.Resolution.Reason = why
			}
		}
		plan.RoleSlots = append(plan.RoleSlots, entry)
	}
}
func resolveStepRole(plan *FlowPlan, compiled *StepPlan, step StepDoc, doc *FlowDoc, env Environment) {
	if step.Role == "用户" || step.Role == "人工" {
		compiled.Kind = "confirm"
		return
	}
	var slotDoc *RoleSlotDoc
	var role *RoleSpec
	for i := range doc.RoleSlots {
		if doc.RoleSlots[i].Name == step.Role {
			slotDoc = &doc.RoleSlots[i]
			break
		}
	}
	if slotDoc != nil {
		compiled.RoleSlot = slotDoc.Name
		for _, slot := range plan.RoleSlots {
			if slot.Name != slotDoc.Name {
				continue
			}
			compiled.RequiredPackages = append([]string{}, slot.Packages...)
			compiled.GrantedPackages = append([]string{}, slot.Resolution.Granted...)
			compiled.RoleSource = slot.Resolution.Source
			compiled.Unresolved = slot.Resolution.Reason
			role = planRole(plan, slot.Resolution.RoleID)
			break
		}
	} else {
		mapped := env.RoleMapping[plan.FlowID][step.Role]
		if mapped != "" && mapped != "flow_role" {
			r := roleByID(mapped, env.Roles)
			if r != nil && r.Enabled {
				role = r
				compiled.RoleSource = "mapping"
			} else {
				plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "warning", Code: "FLOW_ROLE_MAPPING_INVALID", Step: step.ID, Message: "角色对应已失效：" + step.Role})
			}
		}
		if role == nil {
			var ds []Diagnostic
			role, ds = bindRole(step, env.Roles)
			plan.Diagnostics = append(plan.Diagnostics, ds...)
			if len(ds) > 0 {
				compiled.Unresolved = ds[0].Code
			}
		}
	}
	if role != nil {
		compiled.Role = role.ID
		compiled.RoleLabel = role.DisplayName
		compiled.Executor = role.Executor
		compiled.Kind = "agent"
		if role.Kind == "executor" {
			compiled.Kind = "executor"
		}
	}
	required, reason := resolvePackages(step.Capabilities, env)
	compiled.RequiredPackages = uniqueStrings(append(compiled.RequiredPackages, required...))
	if reason != "" {
		compiled.Unresolved = reason
	}
	if role != nil && compiled.Unresolved == "" {
		missing := missingPackages(compiled.RequiredPackages, append(append([]string{}, role.ToolPackages...), compiled.GrantedPackages...))
		if len(missing) > 0 {
			switch policy(env) {
			case "strict":
				compiled.Unresolved = "FLOW_CAPABILITY_MISSING_IN_ROLE"
			case "flow_role":
				slot := RoleSlotDoc{Name: step.Role, Packages: step.Capabilities}
				if slotDoc != nil {
					slot = *slotDoc
				}
				synthetic, why := slotSynthetic(plan, slot, func() []string {
					if slotDoc != nil {
						for _, s := range plan.RoleSlots {
							if s.Name == slot.Name {
								return s.Packages
							}
						}
					}
					return nil
				}(), env)
				if why != "" {
					compiled.Unresolved = why
				} else if why = grantReason(missingPackages(compiled.RequiredPackages, synthetic.ToolPackages), env); why != "" {
					compiled.Unresolved = why
				} else {
					role = synthetic
					compiled.Role = role.ID
					compiled.RoleLabel = role.DisplayName
					compiled.RoleSource = "flow_role"
					compiled.GrantedPackages = missingPackages(compiled.RequiredPackages, role.ToolPackages)
				}
			default:
				if why := grantReason(missing, env); why != "" {
					compiled.Unresolved = why
				} else {
					compiled.GrantedPackages = uniqueStrings(append(compiled.GrantedPackages, missing...))
				}
			}
		}
	}
	if role == nil && len(step.Capabilities) > 0 && compiled.Unresolved == "" {
		if step.Role == "" || step.Role == "系统自动" {
			compiled.Unresolved = grantReason(required, env)
			if compiled.Unresolved == "" {
				compiled.GrantedPackages = append(compiled.GrantedPackages, required...)
			}
		} else {
			compiled.Unresolved = "FLOW_ROLE_NOT_FOUND"
		}
	}
	for _, name := range step.OptionalCapabilities {
		ids, why := resolvePackages([]string{name}, env)
		if why == "" {
			why = grantReason(ids, env)
		}
		if why == "" && env.PackageReady != nil && !env.PackageReady(ids[0]) {
			why = "FLOW_ROLE_PACKAGE_UNAVAILABLE"
		}
		if why != "" {
			plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "info", Code: why, Step: step.ID, Message: "可选能力暂不可用：" + name})
			continue
		}
		compiled.GrantedPackages = uniqueStrings(append(compiled.GrantedPackages, ids...))
	}
	if compiled.Unresolved == "" && len(compiled.GrantedPackages) > 0 {
		plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "info", Code: "FLOW_ROLE_AUGMENTED", Step: step.ID, Message: "本步将临时补齐工具包：" + strings.Join(compiled.GrantedPackages, "、"), Fix: map[string]any{"action": "add_tool_package", "role": compiled.Role}})
	}
	if compiled.Unresolved == "" && (compiled.RoleSource == "flow_role" || compiled.RoleSource == "executor_default") {
		plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "info", Code: "FLOW_ROLE_FLOW_ROLE", Step: step.ID, Message: "本步使用流程自带角色：" + compiled.RoleLabel, Fix: map[string]any{"action": "map_role", "slot": compiled.RoleSlot}})
	}
	if compiled.Unresolved != "" {
		exists := false
		for _, d := range plan.Diagnostics {
			if d.Step == step.ID && d.Code == compiled.Unresolved {
				exists = true
			}
		}
		if !exists {
			fix := map[string]any{"action": "set_role", "step": step.ID}
			if compiled.Unresolved == "FLOW_CAPABILITY_MISSING_IN_ROLE" {
				fix["action"] = "add_tool_package"
				fix["role"] = compiled.Role
			}
			plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "error", Code: compiled.Unresolved, Step: step.ID, Message: "负责角色“" + step.Role + "”无法满足本步需求（" + compiled.Unresolved + "）。", Fix: fix})
		}
	}
}
