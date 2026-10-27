package api

import (
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/gorilla/mux"
	"net/http"
	"strings"
)

func (a *API) handleFlowRoles(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flow_roles") {
		return
	}
	if a.assetCatalogService == nil {
		ErrorWithCode(w, http.StatusServiceUnavailable, "FLOW_ROLE_SERVICE_UNAVAILABLE", "方案服务尚未就绪")
		return
	}
	compiler, err := a.FlowCompiler()
	if err != nil {
		flowError(w, err)
		return
	}
	id := mux.Vars(r)["id"]
	plan, err := compiler.PlanReadOnly(r.Context(), id, r.URL.Query().Get("scheme"))
	if err != nil {
		flowError(w, err)
		return
	}
	profile, err := a.assetCatalogService.GetAgentProfile(plan.Scheme)
	if err != nil {
		flowError(w, err)
		return
	}
	var mutation *settings.SettingsMutationResult
	if r.Method == http.MethodPut {
		if a.settingsMutationService == nil {
			ErrorWithCode(w, http.StatusServiceUnavailable, "FLOW_ROLE_SERVICE_UNAVAILABLE", "设置变更服务尚未就绪")
			return
		}
		var request struct {
			Mapping map[string]string `json:"mapping"`
		}
		if !decodeFlowRequest(w, r, &request) {
			return
		}
		if request.Mapping == nil {
			ErrorWithCode(w, http.StatusBadRequest, "FLOW_REQUEST_INVALID", "请提供角色对应 mapping；空对象恢复全部自动对应。")
			return
		}
		if profile.FlowPolicy == nil {
			profile.FlowPolicy = &config.FlowPolicyConfig{}
		}
		if profile.FlowPolicy.RoleMapping == nil {
			profile.FlowPolicy.RoleMapping = map[string]map[string]string{}
		}
		mapping := map[string]string{}
		for slot, target := range request.Mapping {
			slot = strings.TrimSpace(slot)
			target = strings.TrimSpace(target)
			if slot == "" {
				ErrorWithCode(w, http.StatusBadRequest, "FLOW_REQUEST_INVALID", "角色位名称不能为空")
				return
			}
			if target != "" {
				mapping[slot] = target
			}
		}
		if len(mapping) == 0 {
			delete(profile.FlowPolicy.RoleMapping, id)
		} else {
			profile.FlowPolicy.RoleMapping[id] = mapping
		}
		saved, saveErr := a.settingsMutationService.SaveAgentConfig("", plan.Scheme, profile)
		if saveErr != nil {
			if writeSettingsMutationError(w, r, saveErr) {
				return
			}
			flowError(w, saveErr)
			return
		}
		mutation = &saved
		a.InvalidateFlowEnvironment()
		plan, err = compiler.PlanReadOnly(r.Context(), id, plan.Scheme)
		if err != nil {
			flowError(w, err)
			return
		}
	}
	policy := "auto"
	mapping := map[string]string{}
	if profile.FlowPolicy != nil {
		if profile.FlowPolicy.RoleFill != "" {
			policy = profile.FlowPolicy.RoleFill
		}
		for slot, target := range profile.FlowPolicy.RoleMapping[id] {
			mapping[slot] = target
		}
	}
	slots := append([]flow.RoleSlotPlan{}, plan.RoleSlots...)
	originalRoles := map[string]string{}
	if store, storeErr := a.flowStore(); storeErr == nil {
		if stored, getErr := store.Get(id); getErr == nil {
			for _, step := range flow.Parse(stored.FlowMD).Steps {
				originalRoles[step.ID] = step.Role
			}
		}
	}

	// Old FLOW.md files expose their responsible names as mapping targets too.
	seen := map[string]bool{}
	for _, slot := range slots {
		seen[slot.Name] = true
	}
	for _, step := range plan.Steps {
		label := step.RoleSlot
		if label == "" {
			label = originalRoles[step.ID]
		}
		if label == "" || label == "系统自动" || step.Kind == "confirm" || seen[label] {
			continue
		}
		seen[label] = true
		source := step.RoleSource
		if source == "" {
			source = "scheme_name"
		}
		if step.Unresolved != "" {
			source = "unresolved"
		}
		slots = append(slots, flow.RoleSlotPlan{Name: label, Packages: step.RequiredPackages, Resolution: flow.RoleResolution{Source: source, RoleID: step.Role, RoleLabel: step.RoleLabel, Granted: step.GrantedPackages, Reason: step.Unresolved}})
	}
	Success(w, map[string]any{"flow_id": id, "scheme": plan.Scheme, "role_fill": policy, "role_mapping": mapping, "role_slots": slots, "scheme_roles": plan.Roles, "flow_roles": plan.FlowRoles, "mutation": mutation})
}

func summarizeFlowRoles(plan *flow.FlowPlan) map[string]int {
	counts := map[string]int{"scheme": 0, "augmented": 0, "flow_role": 0, "executor_default": 0, "unresolved": 0, "conditional_unresolved": 0}
	seen := map[string]map[string]bool{}
	reach := flow.StepReach(plan, nil, nil)
	for _, step := range plan.Steps {
		if step.Kind == "confirm" {
			continue
		}
		category := "scheme"
		switch {
		case step.Unresolved != "":
			category = "unresolved"
			if reach[step.ID] == flow.ReachMaybe {
				category = "conditional_unresolved"
			}
		case step.RoleSource == "executor_default":
			category = "executor_default"
		case step.RoleSource == "flow_role":
			category = "flow_role"
		case len(step.GrantedPackages) > 0:
			category = "augmented"
		case step.Kind == "auto":
			continue
		}
		key := step.RoleSlot
		if key == "" {
			key = step.RoleLabel
		}
		if key == "" {
			key = step.Role
		}
		if key == "" {
			key = step.ID
		}
		if seen[category] == nil {
			seen[category] = map[string]bool{}
		}
		if !seen[category][key] {
			counts[category]++
			seen[category][key] = true
		}
	}
	return counts
}
