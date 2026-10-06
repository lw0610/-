package llm

import "fmt"

// Capability 表示模型运行时能力约束。
type Capability string

const (
	CapabilityForcedToolChoice            Capability = "forced_tool_choice"
	CapabilityPlanExecuteForcedToolChoice Capability = "plan_execute_forced_tool_choice"
)

// ValidateCapability 校验模型配置是否满足指定能力要求。
func ValidateCapability(cfg *Config, capability Capability) error {
	if cfg == nil {
		return fmt.Errorf("llm config is nil")
	}

	info, ok := GetProviderInfo(cfg.Provider)
	if !ok {
		return fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}

	switch capability {
	case CapabilityForcedToolChoice:
		if !info.SupportsForcedToolChoice {
			return fmt.Errorf("provider %s does not support forced tool choice", info.ID)
		}
		return nil
	case CapabilityPlanExecuteForcedToolChoice:
		if !info.SupportsPlanExecute {
			return fmt.Errorf("provider %s does not satisfy planexecute forced tool-call protocol", info.ID)
		}
		return nil
	default:
		return fmt.Errorf("unsupported capability: %s", capability)
	}
}

// PrepareConfigForCapability 返回满足指定能力的模型配置副本。
func PrepareConfigForCapability(cfg *Config, capability Capability) (*Config, error) {
	if err := ValidateCapability(cfg, capability); err != nil {
		return nil, err
	}

	prepared := *cfg

	switch capability {
	case CapabilityForcedToolChoice, CapabilityPlanExecuteForcedToolChoice:
		// Planner/Replanner 需要尽量稳定地产出结构化 tool call。
		prepared.Temperature = 0
		prepared.ThinkingEnabled = false
		prepared.ThinkingBudget = 0
		return &prepared, nil
	default:
		return nil, fmt.Errorf("unsupported capability: %s", capability)
	}
}
