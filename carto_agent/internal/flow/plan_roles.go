package flow

import (
	"fmt"
	"strings"
)

// ResolvePlanCapabilities resolves package names in saved capability-based plans.
func ResolvePlanCapabilities(names []string, env Environment) ([]string, error) {
	ids, reason := resolvePackages(names, env)
	if reason != "" {
		return nil, fmt.Errorf("%s", reason)
	}
	return ids, nil
}

// AssignPlanRole binds a step to an enabled child in the selected scheme.
// A selected role retains its own tools, model and instructions.
func AssignPlanRole(_ *FlowPlan, step *StepPlan, names []string, hint string, env Environment) error {
	executor := ""
	for _, name := range names {
		if name == "路线知识抽取" || name == "route_knowledge" {
			executor = "route_knowledge"
		}
	}
	var selected *RoleSpec
	if hint = strings.TrimSpace(hint); hint != "" {
		selected = roleByID(hint, env.Roles)
		if selected == nil || !selected.Enabled || selected.IsMain {
			return fmt.Errorf("步骤「%s」指定的子智能体 %s 未在当前方案中启用，请从方案角色目录选择 role_hint", step.Title, hint)
		}
	} else {
		// Older saved plans declared capabilities instead of a role. Only an
		// unambiguous existing child can satisfy that contract; never invent one.
		if len(names) == 0 {
			return fmt.Errorf("步骤「%s」未选择执行子智能体，请用 role_hint 指定当前方案中的角色", step.Title)
		}
		var ids []string
		if executor == "" {
			var err error
			ids, err = ResolvePlanCapabilities(names, env)
			if err != nil {
				return err
			}
		}
		for _, role := range env.Roles {
			if !role.Enabled || role.IsMain {
				continue
			}
			matches := role.Kind == "executor" && role.Executor == executor
			if executor == "" {
				matches = (role.Kind == "" || role.Kind == "agent") && len(missingPackages(ids, role.ToolPackages)) == 0
			}
			if !matches {
				continue
			}
			if selected != nil {
				return fmt.Errorf("步骤「%s」有多个可用子智能体，请用 role_hint 从当前方案中明确选择", step.Title)
			}
			copy := role
			selected = &copy
		}
		if selected == nil {
			return fmt.Errorf("步骤「%s」在当前方案中没有匹配的子智能体，请按已有角色职责调整任务", step.Title)
		}
	}
	if executor != "" && (selected.Kind != "executor" || selected.Executor != executor) {
		return fmt.Errorf("步骤「%s」需要路线知识抽取，请选择方案中使用 %s 执行器的子智能体", step.Title, executor)
	}
	needsVision := false
	for _, id := range selected.ToolPackages {
		needsVision = needsVision || id == "quality-feedback"
	}
	for _, output := range step.ExpectedOutputs {
		needsVision = needsVision || output.Type == "review_initial" || output.Type == "review_final"
	}
	if needsVision {
		provider := selected.Provider
		if provider == "" {
			provider = env.MainProvider
		}
		vision := false
		for _, id := range env.VisionProviders {
			vision = vision || id == provider
		}
		if !vision {
			return fmt.Errorf("子智能体「%s」配置的模型不支持读图，请选择方案中支持读图的子智能体", selected.DisplayName)
		}
	}
	step.Kind = selected.Kind
	if step.Kind == "" {
		step.Kind = "agent"
	}
	step.Executor = selected.Executor
	step.Role, step.RoleLabel = selected.ID, selected.DisplayName
	step.RoleSource = "scheme"
	step.RequiredPackages = append([]string{}, selected.ToolPackages...)
	step.GrantedPackages = nil
	return nil
}
