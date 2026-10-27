package flowrun

import (
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/flow"
)

type RoleOverlaySpec struct {
	FlowRole      *flow.FlowRoleSpec `json:"flow_role,omitempty"`
	RoleID        string             `json:"role_id,omitempty"`
	ExtraPackages []string           `json:"extra_packages,omitempty"`
}

func (c *Controller) validateResumeOverlays(ctx context.Context, plan *flow.FlowPlan, scheme string) error {
	ids := []string{}
	for _, role := range plan.FlowRoles {
		ids = append(ids, role.ToolPackages...)
	}
	for _, step := range plan.Steps {
		ids = append(ids, step.GrantedPackages...)
	}
	if len(ids) == 0 {
		return nil
	}
	validator, ok := c.options.Compiler.(interface {
		ValidateOverlayPackages(context.Context, string, []string) error
	})
	if !ok {
		return fmt.Errorf("当前流程服务不能校验恢复所需的临时工具包")
	}
	return validator.ValidateOverlayPackages(ctx, scheme, ids)
}
