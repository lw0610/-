package config

import "fmt"

// FlowPolicyConfig is independent of enabled-flow selection: absent flows still means all.
type FlowPolicyConfig struct {
	RoleFill    string                       `yaml:"role_fill,omitempty" json:"role_fill,omitempty"`
	AutoAcquire string                       `yaml:"auto_acquire,omitempty" json:"auto_acquire,omitempty"`
	RoleMapping map[string]map[string]string `yaml:"role_mapping,omitempty" json:"role_mapping,omitempty"`
}

func CloneFlowPolicy(policy *FlowPolicyConfig) *FlowPolicyConfig {
	if policy == nil {
		return nil
	}
	result := *policy
	if policy.RoleMapping != nil {
		result.RoleMapping = map[string]map[string]string{}
		for id, mapping := range policy.RoleMapping {
			result.RoleMapping[id] = map[string]string{}
			for slot, role := range mapping {
				result.RoleMapping[id][slot] = role
			}
		}
	}
	return &result
}
func ValidateFlowPolicy(policy *FlowPolicyConfig) error {
	if policy == nil {
		return nil
	}
	switch policy.RoleFill {
	case "", "auto", "flow_role", "strict":
	default:
		return fmt.Errorf("flow_policy.role_fill 必须为 auto、flow_role 或 strict")
	}
	switch policy.AutoAcquire {
	case "", "ask", "allow", "deny":
	default:
		return fmt.Errorf("flow_policy.auto_acquire 必须为 ask、allow 或 deny")
	}
	return nil
}
