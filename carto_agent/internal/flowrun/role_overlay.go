package flowrun

import (
	"github.com/autocarto/carto_agent/internal/flow"
)

type RoleOverlaySpec struct {
	FlowRole      *flow.FlowRoleSpec `json:"flow_role,omitempty"`
	RoleID        string             `json:"role_id,omitempty"`
	ExtraPackages []string           `json:"extra_packages,omitempty"`
}
